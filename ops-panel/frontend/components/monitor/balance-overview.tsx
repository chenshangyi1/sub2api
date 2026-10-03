"use client"

import { lazy, Suspense } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useIsMobile } from "@/hooks/use-mobile"
import { useBalanceTrend, useCostTrend, useDashboardSummary } from "@/lib/queries"
import { money } from "@/lib/format"
import { cn } from "@/lib/utils"

const BalanceTrendChart = lazy(() =>
  import("@/components/monitor/balance-trend-chart").then((m) => ({ default: m.BalanceTrendChart })),
)

function niceCeil(n: number): number {
  if (!Number.isFinite(n) || n <= 0) return 10
  const padded = n * 1.15
  const mag = Math.pow(10, Math.floor(Math.log10(padded)))
  const norm = padded / mag
  const step = norm <= 1 ? 1 : norm <= 2 ? 2 : norm <= 5 ? 5 : 10
  return step * mag
}

function formatDay(iso: string) {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return `${d.getMonth() + 1}月${d.getDate()}日`
}

interface ChartPoint {
  day: string
  balance: number | null
  cost: number | null
}

export function BalanceOverview() {
  const isMobile = useIsMobile()
  const trend = useBalanceTrend(7)
  const costTrend = useCostTrend(7)
  const summary = useDashboardSummary()

  const channels = summary.data?.channels ?? []
  const trendMap = new Map<string, ChartPoint>()

  for (const point of trend.data ?? []) {
    const key = point.day
    const existing = trendMap.get(key)
    trendMap.set(key, {
      day: formatDay(point.day),
      balance: point.balance,
      cost: existing?.cost ?? null,
    })
  }
  for (const point of costTrend.data ?? []) {
    const key = point.day
    const existing = trendMap.get(key)
    trendMap.set(key, {
      day: existing?.day ?? formatDay(point.day),
      balance: existing?.balance ?? null,
      cost: point.cost,
    })
  }

  const data = Array.from(trendMap.entries())
    .sort(([a], [b]) => new Date(a).getTime() - new Date(b).getTime())
    .map(([, value]) => value)
  const balanceValues = data.map((d) => d.balance ?? 0)
  const costValues = data.map((d) => d.cost ?? 0)
  const yMax = data.length > 0 ? niceCeil(Math.max(...balanceValues)) : 10
  const costMax = data.length > 0 ? niceCeil(Math.max(...costValues)) : 10
  const isLoading = trend.loading || costTrend.loading

  return (
    <Card className="border border-border py-4 shadow-none lg:h-100 sm:py-6">
      <CardHeader className="flex shrink-0 flex-row items-center justify-between px-4 pb-2 sm:px-6">
        <CardTitle className="text-base font-semibold">{"余额概览"}</CardTitle>
        <span className="text-xs text-muted-foreground">{"最近 7 天"}</span>
      </CardHeader>
      <CardContent className="flex min-h-0 flex-1 flex-col px-4 sm:px-6">
        <div className="mb-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
          <span className="inline-flex items-center gap-1.5">
            <span className="size-2 rounded-full bg-brand" />
            <span className="text-muted-foreground">{"余额"}</span>
          </span>
          <span className="inline-flex items-center gap-1.5">
            <span className="size-2 rounded-full bg-warning" />
            <span className="text-muted-foreground">
              {"消费趋势"}
            </span>
          </span>
        </div>
        <div className="h-64 min-h-0 w-full sm:h-72 lg:h-auto lg:flex-1">
          <Suspense
            fallback={
              <div className="flex h-full items-center justify-center text-xs text-muted-foreground">{"图表加载中…"}</div>
            }
          >
            <BalanceTrendChart
              isMobile={isMobile}
              data={data}
              yMax={yMax}
              costMax={costMax}
              isLoading={isLoading}
            />
          </Suspense>
        </div>

        {/* per-channel chips */}
        {channels.length > 0 ? (
          <div className="mt-3 flex shrink-0 flex-wrap items-center gap-x-5 gap-y-2 border-t border-border pt-3">
            {channels.map((c) => {
              const isFailed = !!c.last_error
              const isUnknown = c.last_balance == null
              return (
                <span key={c.id} className="inline-flex max-w-full items-center gap-1.5 text-xs">
                  <span
                    className={cn(
                      "size-2 rounded-full",
                      isFailed ? "bg-danger" : isUnknown ? "bg-muted-foreground/40" : "bg-success",
                    )}
                  />
                  <span className="max-w-32 truncate font-medium text-foreground sm:max-w-none">{c.name}</span>
                  <span className="min-w-0 tabular-nums text-muted-foreground">
                    {money(c.last_balance)}
                    {" · 今日 "}
                    {money(c.today_cost)}
                  </span>
                </span>
              )
            })}
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}
