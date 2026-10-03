import { useEffect, useState } from "react"
import {
  Activity,
  Banknote,
  ChartNoAxesCombined,
  Clock3,
  Coins,
  Users,
} from "lucide-react"
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip as ChartTooltip,
  XAxis,
  YAxis,
} from "recharts"
import {
  OperationsEmpty,
  OperationsError,
  OperationsLoading,
  OperationsMetricStrip,
  OperationsPageHeader,
  OperationsStatusBadge,
} from "@/components/operations/operations-layout"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useQueryTab } from "@/hooks/use-query-tab"
import {
  dateTime,
  decimal,
  formatCompactNumber,
  formatDurationMS,
  money,
} from "@/lib/format"
import { getOperationsAnalytics, type TargetAnalytics } from "@/lib/operations-api"
import { cn } from "@/lib/utils"

const analyticsRanges = ["day", "week", "month"] as const

export default function TargetAnalyticsPage() {
  const [range, setRange] = useQueryTab("range", analyticsRanges, "day")
  const [data, setData] = useState<TargetAnalytics | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reload, setReload] = useState(0)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    getOperationsAnalytics(range)
      .then((result) => {
        if (!cancelled) setData(result)
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : "目标分析加载失败")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [range, reload])

  const summary = data?.summary

  return (
    <section className="space-y-5">
      <OperationsPageHeader
        icon={ChartNoAxesCombined}
        title="主服务分析"
        description="主 Sub2API 服务的用户实扣、全部账号成本、管理员成本、利润和请求质量；上游网关自身用量继续保留在请求网关页面。"
        refreshing={loading}
        onRefresh={() => setReload((value) => value + 1)}
        actions={
          <div className="flex rounded-md border border-border bg-background p-0.5" aria-label="统计周期">
            {analyticsRanges.map((item) => (
              <Button
                key={item}
                variant="ghost"
                size="sm"
                className={cn("h-7 px-2.5", range === item && "bg-muted")}
                onClick={() => setRange(item)}
              >
                {item === "day" ? "今日" : item === "week" ? "本周" : "本月"}
              </Button>
            ))}
          </div>
        }
      />

      {error ? <OperationsError message={error} /> : null}
      {loading && !data ? <OperationsLoading rows={6} /> : null}

      {summary ? (
        <>
          <OperationsMetricStrip
            items={[
              { label: "用户实扣", value: money(summary.user_cost), detail: "排除管理员后的实际用户成本" },
              { label: "用户利润", value: money(summary.profit), detail: `全部账号成本 ${money(summary.upstream_cost)}` },
              { label: "管理员成本", value: money(summary.administrator_cost), detail: "管理员自用成本单独核算" },
              { label: "利润率", value: `${decimal(summary.profit_margin * 100, 2)}%`, detail: `${formatCompactNumber(summary.requests)} 次请求` },
            ]}
          />

          <div className="grid gap-4 xl:grid-cols-[minmax(0,1.45fr)_minmax(19rem,0.55fr)]">
            <section className="min-w-0 rounded-md border border-border bg-background p-4">
              <div className="mb-4 flex items-start justify-between gap-3">
                <div>
                  <h3 className="text-sm font-semibold text-foreground">利润与成本趋势</h3>
                  <p className="mt-1 text-xs text-muted-foreground">按自然日聚合，利润等于用户实扣减去全部账号成本。</p>
                </div>
                <Badge variant="outline" className="font-normal">{summary.label}</Badge>
              </div>
              {data?.daily.length ? (
                <div className="h-72 w-full">
                  <ResponsiveContainer width="100%" height="100%">
                    <AreaChart data={data.daily} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                      <defs>
                        <linearGradient id="profit-fill" x1="0" y1="0" x2="0" y2="1">
                          <stop offset="0%" stopColor="var(--chart-2)" stopOpacity={0.3} />
                          <stop offset="100%" stopColor="var(--chart-2)" stopOpacity={0.02} />
                        </linearGradient>
                      </defs>
                      <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="var(--border)" />
                      <XAxis dataKey="date" tickLine={false} axisLine={false} tick={{ fontSize: 11, fill: "var(--muted-foreground)" }} />
                      <YAxis tickLine={false} axisLine={false} width={54} tick={{ fontSize: 11, fill: "var(--muted-foreground)" }} tickFormatter={(value) => `$${value}`} />
                      <ChartTooltip
                        formatter={(value, name) => [money(Number(value)), name === "profit" ? "利润" : "账号成本"]}
                        labelFormatter={(label) => String(label)}
                        contentStyle={{ borderRadius: 6, borderColor: "var(--border)", background: "var(--background)" }}
                      />
                      <Area type="monotone" dataKey="profit" name="profit" stroke="var(--chart-2)" fill="url(#profit-fill)" strokeWidth={2} />
                      <Area type="monotone" dataKey="upstream_cost" name="upstream_cost" stroke="var(--chart-4)" fill="transparent" strokeWidth={1.5} />
                    </AreaChart>
                  </ResponsiveContainer>
                </div>
              ) : (
                <OperationsEmpty title="当前周期没有趋势数据" description="产生目标服务用量后会在这里显示。" />
              )}
            </section>

            <section className="rounded-md border border-border bg-background p-4">
              <h3 className="text-sm font-semibold text-foreground">请求质量</h3>
              <p className="mt-1 text-xs text-muted-foreground">目标服务的吞吐、缓存和流式首字表现。</p>
              <div className="mt-4 divide-y divide-border">
                <QualityRow icon={Activity} label="请求" value={formatCompactNumber(summary.requests)} detail={`${formatCompactNumber(summary.active_users)} 位活跃用户`} />
                <QualityRow icon={Users} label="流式请求" value={formatCompactNumber(summary.stream_requests)} detail={`${decimal((summary.stream_requests / Math.max(summary.requests, 1)) * 100, 1)}% 占比`} />
                <QualityRow icon={Coins} label="Token" value={formatCompactNumber(summary.total_tokens)} detail={`输入 ${formatCompactNumber(summary.input_tokens)} / 输出 ${formatCompactNumber(summary.output_tokens)} / 缓存 ${formatCompactNumber(summary.cache_read_tokens + summary.cache_creation_tokens)}`} />
                <QualityRow icon={Banknote} label="缓存命中" value={`${decimal(summary.cache_hit_rate * 100, 1)}%`} detail={`读取 ${formatCompactNumber(summary.cache_read_tokens)}`} />
                <QualityRow icon={Clock3} label="首字 P95" value={formatDurationMS(summary.p95_first_token_ms)} detail={`平均 ${formatDurationMS(summary.average_first_token_ms)}`} />
              </div>
            </section>
          </div>

          <UsageHeatmap cells={data?.heatmap ?? []} />
          <SlowRequestsTable items={data?.slow_requests ?? []} />
        </>
      ) : null}

      {!loading && !error && !summary ? (
        <OperationsEmpty title="暂无主服务分析数据" description="确认主服务数据库连接和用量数据已迁移。" />
      ) : null}
    </section>
  )
}

function QualityRow({
  icon: Icon,
  label,
  value,
  detail,
}: {
  icon: typeof Activity
  label: string
  value: string
  detail: string
}) {
  return (
    <div className="flex items-center gap-3 py-3 first:pt-0 last:pb-0">
      <div className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
        <Icon className="size-4" />
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-xs text-muted-foreground">{label}</p>
        <p className="truncate text-[11px] text-muted-foreground">{detail}</p>
      </div>
      <p className="font-mono text-sm font-semibold text-foreground">{value}</p>
    </div>
  )
}

function UsageHeatmap({ cells }: { cells: TargetAnalytics["heatmap"] }) {
  const dates = Array.from(new Set(cells.map((cell) => cell.date))).sort()
  const byKey = new Map(cells.map((cell) => [`${cell.date}:${cell.hour}`, cell]))
  const maxRequests = Math.max(1, ...cells.map((cell) => cell.requests))

  return (
    <section className="rounded-md border border-border bg-background p-4">
      <h3 className="text-sm font-semibold text-foreground">7 x 24 请求热力图</h3>
      <p className="mt-1 text-xs text-muted-foreground">不包含管理员请求；颜色越深表示请求越集中。</p>
      {dates.length ? (
        <div className="mt-4 overflow-x-auto pb-1">
          <div className="min-w-[760px] space-y-1">
            <div className="grid grid-cols-[5rem_repeat(24,minmax(1rem,1fr))] gap-1 text-[10px] text-muted-foreground">
              <span />
              {Array.from({ length: 24 }, (_, hour) => <span key={hour} className="text-center">{hour % 3 === 0 ? hour : ""}</span>)}
            </div>
            {dates.map((date) => (
              <div key={date} className="grid grid-cols-[5rem_repeat(24,minmax(1rem,1fr))] gap-1">
                <span className="truncate pr-2 text-[11px] text-muted-foreground">{date.slice(5)}</span>
                {Array.from({ length: 24 }, (_, hour) => {
                  const cell = byKey.get(`${date}:${hour}`)
                  const level = cell ? Math.max(1, Math.ceil((cell.requests / maxRequests) * 5)) : 0
                  return (
                    <div
                      key={hour}
                      className={cn(
                        "aspect-square min-h-4 rounded-[3px] border border-transparent",
                        level === 0 && "bg-muted/40",
                        level === 1 && "bg-emerald-100 dark:bg-emerald-950/60",
                        level === 2 && "bg-emerald-200 dark:bg-emerald-900/70",
                        level === 3 && "bg-emerald-300 dark:bg-emerald-800/80",
                        level === 4 && "bg-emerald-500 dark:bg-emerald-700",
                        level === 5 && "bg-emerald-700 dark:bg-emerald-500",
                      )}
                      title={`${date} ${hour}:00 · ${cell?.requests ?? 0} 请求 · ${cell?.failures ?? 0} 失败`}
                    />
                  )
                })}
              </div>
            ))}
          </div>
        </div>
      ) : (
        <div className="mt-4"><OperationsEmpty title="暂无热力图数据" description="当前周期没有用户请求。" /></div>
      )}
    </section>
  )
}

function SlowRequestsTable({ items }: { items: TargetAnalytics["slow_requests"] }) {
  return (
    <section className="rounded-md border border-border bg-background">
      <div className="border-b border-border px-4 py-3">
        <h3 className="text-sm font-semibold text-foreground">慢请求明细</h3>
        <p className="mt-1 text-xs text-muted-foreground">按首字或总耗时排序的目标服务请求。</p>
      </div>
      {items.length ? (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] text-left text-sm">
            <thead className="bg-muted/35 text-xs text-muted-foreground">
              <tr>
                <th className="px-4 py-2 font-medium">时间</th>
                <th className="px-4 py-2 font-medium">用户</th>
                <th className="px-4 py-2 font-medium">模型 / 账号</th>
                <th className="px-4 py-2 font-medium">总耗时</th>
                <th className="px-4 py-2 font-medium">首字</th>
                <th className="px-4 py-2 font-medium">结果</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {items.map((item) => (
                <tr key={item.id}>
                  <td className="whitespace-nowrap px-4 py-3 text-xs text-muted-foreground">{dateTime(item.created_at)}</td>
                  <td className="px-4 py-3"><p>{item.user_name || "—"}</p><p className="text-[11px] text-muted-foreground">{item.user_id ? `#${item.user_id}` : ""}</p></td>
                  <td className="px-4 py-3"><p className="max-w-56 truncate font-mono text-xs">{item.model}</p><p className="text-[11px] text-muted-foreground">{item.account_id ? `账号 #${item.account_id}` : "—"}{item.stream ? " · 流式" : ""}</p></td>
                  <td className="px-4 py-3 font-mono text-xs">{formatDurationMS(item.duration_ms)}</td>
                  <td className="px-4 py-3 font-mono text-xs">{formatDurationMS(item.first_token_ms)}</td>
                  <td className="px-4 py-3"><OperationsStatusBadge status={item.status_code >= 200 && item.status_code < 400 ? "success" : "failed"} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="p-4"><OperationsEmpty title="没有慢请求" description="当前周期内没有达到慢请求阈值的记录。" /></div>
      )}
    </section>
  )
}
