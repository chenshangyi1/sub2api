import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { apiFetch } from "@/lib/api"
import type { ModelPriceOverride } from "@/lib/api-types"
import { emptyPriceForm, perTokenToMTok, type PriceFormState } from "./gateway-utils"

type PricesPanelProps = {
  busy: boolean
  prices: ModelPriceOverride[]
  priceForm: PriceFormState
  setPriceForm: (form: PriceFormState) => void
  onSavePrice: () => void
  onOpenDefaults: () => void
  onFillFromOverride: (p: ModelPriceOverride) => void
  onLoadPrices: () => void
}

export function PricesPanel({
  busy,
  prices,
  priceForm,
  setPriceForm,
  onSavePrice,
  onOpenDefaults,
  onFillFromOverride,
  onLoadPrices,
}: PricesPanelProps) {
  return (
    <div className="space-y-4">
<Card className="overflow-hidden border-border shadow-none">
  <CardContent className="space-y-4 p-4 sm:p-5">
    <p className="text-sm leading-6 text-muted-foreground">
      覆盖表优先于系统内置价。输入单位为 <strong>$/MTok</strong>
      （每百万 token 美元），保存时自动换算为 per-token 存储。未覆盖且不在默认表中的模型费用为 0。
    </p>
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            <div className="space-y-1">
              <Label className="text-xs">模型名</Label>
              <Input
                placeholder="claude-sonnet-4"
                value={priceForm.model_name}
                onChange={(e) =>
                  setPriceForm({ ...priceForm, model_name: e.target.value })
                }
              />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">Input $/MTok</Label>
              <Input
                type="number"
                step="0.01"
                value={priceForm.input_mtok}
                onChange={(e) =>
                  setPriceForm({ ...priceForm, input_mtok: e.target.value })
                }
              />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">Output $/MTok</Label>
              <Input
                type="number"
                step="0.01"
                value={priceForm.output_mtok}
                onChange={(e) =>
                  setPriceForm({ ...priceForm, output_mtok: e.target.value })
                }
              />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">Cache create $/MTok</Label>
              <Input
                type="number"
                step="0.01"
                value={priceForm.cache_create_mtok}
                onChange={(e) =>
                  setPriceForm({ ...priceForm, cache_create_mtok: e.target.value })
                }
              />
            </div>
            <div className="space-y-1">
              <Label className="text-xs">Cache read $/MTok</Label>
              <Input
                type="number"
                step="0.01"
                value={priceForm.cache_read_mtok}
                onChange={(e) =>
                  setPriceForm({ ...priceForm, cache_read_mtok: e.target.value })
                }
              />
            </div>
            <div className="flex items-end gap-2 pb-1">
              <div className="flex items-center gap-2">
                <Switch
                  checked={priceForm.enabled}
                  onCheckedChange={(v) => setPriceForm({ ...priceForm, enabled: v })}
                />
                <Label className="text-sm">启用覆盖</Label>
              </div>
            </div>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button onClick={() => void onSavePrice()} disabled={busy}>
              保存价格覆盖
            </Button>
            <Button
              variant="outline"
              onClick={() => void onOpenDefaults()}
              disabled={busy}
            >
              查看系统默认价
            </Button>
            <Button
              variant="ghost"
              onClick={() => setPriceForm(emptyPriceForm())}
              disabled={busy}
            >
              清空表单
            </Button>
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>模型</TableHead>
                <TableHead>Input $/MTok</TableHead>
                <TableHead>Output $/MTok</TableHead>
                <TableHead>Cache C/R</TableHead>
                <TableHead>状态</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {prices.map((p) => (
                <TableRow
                  key={p.id}
                  className="cursor-pointer"
                  onClick={() => onFillFromOverride(p)}
                >
                  <TableCell>{p.model_name}</TableCell>
                  <TableCell className="text-xs">
                    {perTokenToMTok(p.input_price_per_token)}
                  </TableCell>
                  <TableCell className="text-xs">
                    {perTokenToMTok(p.output_price_per_token)}
                  </TableCell>
                  <TableCell className="text-xs">
                    {perTokenToMTok(p.cache_creation_price_per_token)} /{" "}
                    {perTokenToMTok(p.cache_read_price_per_token)}
                  </TableCell>
                  <TableCell>
                    <Badge variant={p.enabled ? "default" : "secondary"}>
                      {p.enabled ? "启用" : "禁用"}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={async (e) => {
                        e.stopPropagation()
                        try {
                          await apiFetch(`/gateway/prices/${p.id}`, {
                            method: "DELETE",
                          })
                          toast.success("已删除覆盖")
                          await onLoadPrices()
                        } catch (err) {
                          toast.error(
                            err instanceof Error ? err.message : "删除失败",
                          )
                        }
                      }}
                    >
                      删除
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {prices.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="text-center text-muted-foreground">
                    暂无价格覆盖，计费使用系统内置默认价
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

    </div>
  )
}
