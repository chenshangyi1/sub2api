import { toast } from "sonner"
import { Copy, Plus, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { fullDateTime, money, relativeTime } from "@/lib/format"
import { copyText } from "@/lib/clipboard"
import { cn } from "@/lib/utils"
import type { GatewayKey } from "@/lib/api-types"

type KeysPanelProps = {
  keys: GatewayKey[]
  keySecrets: Record<number, string>
  busy: boolean
  refreshing?: boolean
  onRefresh: () => void
  onCreate: () => void
  onEdit: (k: GatewayKey) => void
  onToggle: (k: GatewayKey) => void
  onDelete: (id: number) => void
}

export function KeysPanel({
  keys,
  keySecrets,
  busy,
  refreshing = false,
  onRefresh,
  onCreate,
  onEdit,
  onToggle,
  onDelete,
}: KeysPanelProps) {
  return (
    <TooltipProvider delayDuration={200}>
      <div className="space-y-4">
        <Card className="overflow-hidden border-border shadow-none">
          <CardContent className="space-y-4 p-4 sm:p-5">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <div className="text-sm font-medium">API 密钥</div>
                <p className="text-xs text-muted-foreground">
                  客户端使用组内密钥访问 /v1/*
                </p>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={busy || refreshing}
                  title="刷新密钥列表"
                  onClick={() => onRefresh()}
                >
                  <RefreshCw
                    className={cn("size-3.5", refreshing && "animate-spin")}
                  />
                  刷新
                </Button>
                <Button size="sm" onClick={onCreate} disabled={busy || refreshing}>
                  <Plus className="size-3.5" /> 创建密钥
                </Button>
              </div>
            </div>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>密钥</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>配额</TableHead>
                  <TableHead>最近使用</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {keys.map((k) => {
                  const secret = keySecrets[k.id] ?? ""
                  return (
                    <TableRow key={k.id}>
                      <TableCell className="font-medium">{k.name}</TableCell>
                      <TableCell>
                        <div className="flex min-w-[16rem] max-w-md items-center gap-1.5">
                          <Input
                            className="h-8 font-mono text-xs"
                            readOnly
                            value={secret || k.key_prefix || ""}
                            placeholder="加载中…"
                          />
                          <Button
                            size="icon"
                            variant="outline"
                            className="size-8 shrink-0"
                            disabled={!secret}
                            title="复制密钥"
                            onClick={() => {
                              if (!secret) return
                              void copyText(secret)
                                .then(() => toast.success("已复制密钥"))
                                .catch(() => toast.error("复制失败"))
                            }}
                          >
                            <Copy className="size-3.5" />
                          </Button>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Switch
                          checked={k.status === "active"}
                          onCheckedChange={() => void onToggle(k)}
                        />
                      </TableCell>
                      <TableCell className="text-sm">
                        {k.quota > 0
                          ? `${money(k.quota_used)} / ${money(k.quota)}`
                          : `${money(k.quota_used)} / ∞`}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {k.last_used_at ? (
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <span className="cursor-default underline decoration-dotted decoration-muted-foreground/50 underline-offset-2">
                                {relativeTime(k.last_used_at)}
                              </span>
                            </TooltipTrigger>
                            <TooltipContent side="top" className="text-xs">
                              最后使用：{fullDateTime(k.last_used_at)}
                            </TooltipContent>
                          </Tooltip>
                        ) : (
                          "—"
                        )}
                      </TableCell>
                      <TableCell className="space-x-1 text-right">
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => onEdit(k)}
                        >
                          编辑
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => void onDelete(k.id)}
                        >
                          删除
                        </Button>
                      </TableCell>
                    </TableRow>
                  )
                })}
                {keys.length === 0 && (
                  <TableRow>
                    <TableCell
                      colSpan={6}
                      className="text-center text-muted-foreground"
                    >
                      组内暂无密钥，创建后客户端用该密钥访问 /v1/*
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      </div>
    </TooltipProvider>
  )
}
