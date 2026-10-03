"use client"

import { Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis, CartesianGrid } from "recharts"
import { money } from "@/lib/format"

interface TooltipPayloadItem {
  dataKey?: string
  value: number
}

function ChartTooltip({ active, payload, label }: { active?: boolean; payload?: TooltipPayloadItem[]; label?: string }) {
  if (!active || !payload?.length) return null
  const balance = payload.find((p) => p.dataKey === "balance")?.value
  const cost = payload.find((p) => p.dataKey === "cost")?.value
  return (
    <div className="rounded-lg border border-border bg-popover px-3 py-2 shadow-md">
      <p className="text-xs text-muted-foreground">{label}</p>
      {balance != null ? (
        <p className="text-sm font-semibold text-brand">
          {"余额："}{money(balance)}
        </p>
      ) : null}
      {cost != null ? (
        <p className="mt-1 text-sm font-semibold text-warning">
          {"消费："}{money(cost)}
        </p>
      ) : null}
    </div>
  )
}

function formatY(n: number) {
  if (n === 0) return "$0"
  if (n >= 1000) return `$${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}K`
  if (n >= 100) return `$${n.toFixed(0)}`
  return `$${n.toFixed(n >= 10 ? 1 : 2)}`
}

interface ChartPoint {
  day: string
  balance: number | null
  cost: number | null
}

interface BalanceTrendChartProps {
  isMobile: boolean
  data: ChartPoint[]
  yMax: number
  costMax: number
  isLoading: boolean
}

export function BalanceTrendChart({ isMobile, data, yMax, costMax, isLoading }: BalanceTrendChartProps) {
  const chartMargin = isMobile
    ? { top: 6, right: 4, left: -18, bottom: 0 }
    : { top: 8, right: 12, left: 0, bottom: 0 }
  const dot = isMobile ? false : { r: 4, fill: "var(--background)", strokeWidth: 2 }
  const activeDot = isMobile ? { r: 4, strokeWidth: 0 } : { r: 5, strokeWidth: 0 }

  if (isLoading) {
    return <div className="flex h-full items-center justify-center text-xs text-muted-foreground">{"加载中…"}</div>
  }
  if (data.length === 0) {
    return (
      <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
        {"暂无趋势采样，等待下次扫描或手动刷新"}
      </div>
    )
  }
  return (
    <ResponsiveContainer width="100%" height="100%">
      <LineChart data={data} margin={chartMargin}>
        <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" vertical={false} />
        <XAxis
          dataKey="day"
          tickLine={false}
          axisLine={false}
          interval={isMobile ? 1 : 0}
          minTickGap={isMobile ? 8 : 5}
          tick={{ fill: "var(--muted-foreground)", fontSize: isMobile ? 10 : 11 }}
          dy={isMobile ? 6 : 8}
        />
        <YAxis
          tickLine={false}
          axisLine={false}
          width={isMobile ? 40 : 48}
          tick={{ fill: "var(--muted-foreground)", fontSize: isMobile ? 10 : 11 }}
          tickFormatter={formatY}
          domain={[0, yMax]}
        />
        <YAxis
          yAxisId="cost"
          orientation="right"
          tickLine={false}
          axisLine={false}
          width={isMobile ? 0 : 52}
          tick={isMobile ? false : { fill: "var(--muted-foreground)", fontSize: 11 }}
          tickFormatter={formatY}
          domain={[0, costMax]}
        />
        <Tooltip content={<ChartTooltip />} cursor={{ stroke: "var(--border)", strokeDasharray: "4 4" }} />
        <Line
          type="monotone"
          dataKey="balance"
          stroke="var(--brand)"
          strokeWidth={2}
          dot={dot}
          activeDot={{ ...activeDot, fill: "var(--brand)" }}
        />
        <Line
          yAxisId="cost"
          type="monotone"
          dataKey="cost"
          stroke="var(--warning)"
          strokeWidth={2}
          connectNulls={false}
          dot={dot}
          activeDot={{ ...activeDot, fill: "var(--warning)" }}
        />
      </LineChart>
    </ResponsiveContainer>
  )
}