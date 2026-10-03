import type { ReactNode } from "react"
import { AlertCircle, RefreshCw, type LucideIcon } from "lucide-react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import type { UpstreamSyncTarget } from "@/lib/api-types"
import type { OperationsStatus } from "@/lib/operations-api"
import { cn } from "@/lib/utils"

export function OperationsPageHeader({
  icon: Icon,
  title,
  description,
  targets,
  selectedTargetID,
  targetLoading,
  onTargetChange,
  refreshing,
  onRefresh,
  actions,
}: {
  icon: LucideIcon
  title: string
  description: string
  targets?: UpstreamSyncTarget[]
  selectedTargetID?: number | null
  targetLoading?: boolean
  onTargetChange?: (targetID: number) => void
  refreshing?: boolean
  onRefresh?: () => void
  actions?: ReactNode
}) {
  return (
    <header className="flex flex-col gap-4 border-b border-border pb-4 lg:flex-row lg:items-end lg:justify-between">
      <div className="min-w-0 space-y-1.5">
        <div className="flex items-center gap-2">
          <Icon className="size-5 text-foreground" />
          <h2 className="text-xl font-semibold text-foreground">{title}</h2>
        </div>
        <p className="max-w-3xl text-sm leading-6 text-muted-foreground">{description}</p>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {targets && onTargetChange ? (
          <Select
            value={selectedTargetID == null ? "" : String(selectedTargetID)}
            onValueChange={(value) => {
              const targetID = Number(value)
              if (targetID !== selectedTargetID) onTargetChange(targetID)
            }}
            disabled={targetLoading || targets.length === 0}
          >
            <SelectTrigger className="w-full min-w-48 bg-background sm:w-auto" aria-label="目标站点">
              <SelectValue placeholder={targetLoading ? "加载目标站点" : "选择目标站点"} />
            </SelectTrigger>
            <SelectContent>
              {targets.map((target) => (
                <SelectItem key={target.id} value={String(target.id)}>
                  {target.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ) : null}
        {actions}
        {onRefresh ? (
          <Button
            variant="outline"
            size="icon"
            onClick={onRefresh}
            disabled={refreshing}
            aria-label="刷新"
            title="刷新"
          >
            <RefreshCw className={cn("size-4", refreshing && "animate-spin")} />
          </Button>
        ) : null}
      </div>
    </header>
  )
}

export function OperationsError({ title = "数据加载失败", message }: { title?: string; message: string }) {
  return (
    <Alert variant="destructive">
      <AlertCircle className="size-4" />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  )
}

export function OperationsLoading({ rows = 4 }: { rows?: number }) {
  return (
    <div className="space-y-2" aria-label="加载中">
      {Array.from({ length: rows }, (_, index) => (
        <Skeleton key={index} className="h-16 w-full rounded-md" />
      ))}
    </div>
  )
}

export function OperationsEmpty({ title, description }: { title: string; description: string }) {
  return (
    <div className="border-y border-dashed border-border py-12 text-center">
      <p className="text-sm font-medium text-foreground">{title}</p>
      <p className="mt-1 text-xs text-muted-foreground">{description}</p>
    </div>
  )
}

export function OperationsStatusBadge({ status }: { status: OperationsStatus | string }) {
  const displayStatus = typeof status === "string" && status.trim() ? status : "unknown"
  const normalized = displayStatus.toLowerCase()
  const tone =
    normalized === "healthy" || normalized === "success" || normalized === "active"
      ? "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/40 dark:text-emerald-300"
      : normalized === "warning" || normalized === "running" || normalized === "temporary_unavailable"
        ? "border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-300"
        : normalized === "error" || normalized === "failed"
          ? "border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-950/40 dark:text-red-300"
          : "border-border bg-muted/40 text-muted-foreground"
  return (
    <Badge variant="outline" className={cn("font-normal", tone)}>
      {normalized === "temporary_unavailable" ? "临时不可用" : displayStatus}
    </Badge>
  )
}

export function OperationsMetricStrip({
  items,
}: {
  items: Array<{ label: string; value: ReactNode; detail?: string }>
}) {
  return (
    <div className="grid overflow-hidden rounded-md border border-border bg-background sm:grid-cols-2 lg:grid-cols-4">
      {items.map((item, index) => (
        <div
          key={item.label}
          className={cn(
            "min-w-0 px-4 py-3",
            index > 0 && "border-t border-border sm:border-t-0 sm:border-l",
            index === 2 && "sm:border-l-0 lg:border-l",
          )}
        >
          <p className="text-xs text-muted-foreground">{item.label}</p>
          <p className="mt-1 truncate text-lg font-semibold text-foreground">{item.value}</p>
          {item.detail ? <p className="mt-0.5 truncate text-[11px] text-muted-foreground">{item.detail}</p> : null}
        </div>
      ))}
    </div>
  )
}
