import { useEffect, useState } from "react"
import { Activity, Gauge, HeartPulse, ShieldAlert, ShieldCheck } from "lucide-react"
import {
  OperationsEmpty,
  OperationsError,
  OperationsLoading,
  OperationsMetricStrip,
  OperationsPageHeader,
  OperationsStatusBadge,
} from "@/components/operations/operations-layout"
import { formatDurationMS } from "@/lib/format"
import { getGroupMonitorOverview, type GroupMonitorOverview } from "@/lib/operations-api"
import { cn } from "@/lib/utils"

function monitorState(m: GroupMonitorOverview["monitors"][number]): "healthy" | "warning" | "failed" {
  const total = m.healthy_count + m.failed_count + m.unknown_count
  if (total === 0) return "warning"
  const ratio = m.healthy_count / total
  if (ratio >= 0.9) return "healthy"
  if (ratio >= 0.5) return "warning"
  return "failed"
}

function pctOf(m: GroupMonitorOverview["monitors"][number], kind: "healthy" | "failed" | "unknown"): number {
  const total = m.healthy_count + m.failed_count + m.unknown_count
  if (total === 0) return 0
  const value = kind === "healthy" ? m.healthy_count : kind === "failed" ? m.failed_count : m.unknown_count
  return (value / total) * 100
}

function healthPct(m: GroupMonitorOverview["monitors"][number]): number {
  return pctOf(m, "healthy")
}
function failedPct(m: GroupMonitorOverview["monitors"][number]): number {
  return pctOf(m, "failed")
}
function unknownPct(m: GroupMonitorOverview["monitors"][number]): number {
  return pctOf(m, "unknown")
}

function stateLabel(s: "healthy" | "warning" | "failed"): string {
  return s === "healthy" ? "正常" : s === "warning" ? "预警" : "异常"
}

export default function GroupMonitorPage() {
  const [data, setData] = useState<GroupMonitorOverview | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reload, setReload] = useState(0)
  const [autoRefresh, setAutoRefresh] = useState(true)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    getGroupMonitorOverview()
      .then((result) => {
        if (!cancelled) setData(result)
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : "分组监控加载失败")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [reload])

  useEffect(() => {
    if (!autoRefresh) return
    const timer = setInterval(() => setReload((value) => value + 1), 30_000)
    return () => clearInterval(timer)
  }, [autoRefresh])

  return (
    <section className="space-y-5">
      <OperationsPageHeader
        icon={HeartPulse}
        title="分组监控"
        description="Sub2API 分组级渠道健康聚合：按分组统计账号可用率、TTFT、缓存率与最近检测状态。"
        refreshing={loading}
        onRefresh={() => setReload((value) => value + 1)}
        actions={
          <button
            type="button"
            onClick={() => setAutoRefresh((value) => !value)}
            className={cn(
              "flex items-center gap-2 rounded-md border border-border bg-background px-3 py-1.5 text-xs font-medium",
              autoRefresh ? "text-emerald-600 dark:text-emerald-400" : "text-muted-foreground",
            )}
          >
            <span className={cn("h-1.5 w-1.5 rounded-full", autoRefresh ? "animate-pulse bg-emerald-500" : "bg-muted-foreground")} />
            {autoRefresh ? "自动刷新 30s" : "自动刷新关"}
          </button>
        }
      />

      {error ? (
        <OperationsError message={error} />
      ) : loading && !data ? (
        <OperationsLoading rows={5} />
      ) : !data || data.monitors.length === 0 ? (
        <OperationsEmpty title="暂无分组监控" description="请在 Sub2API 管理后台「渠道管理 → 分组监控」中创建分组监控。" />
      ) : (
        <>
          <OperationsMetricStrip
            items={[
              { label: "分组监控", value: data.total_monitors, detail: `启用中` },
              { label: "健康分组", value: data.healthy_monitors, detail: `异常 ${data.failed_monitors}` },
              { label: "账号", value: `${data.healthy_accounts}/${data.total_accounts}`, detail: `异常 ${data.failed_accounts}` },
              { label: "平均可用率(7天)", value: `${data.avg_availability.toFixed(1)}%` },
            ]}
          />

          <div className="grid gap-4 lg:grid-cols-2">
            {data.monitors.map((m) => {
              const state = monitorState(m)
              return (
                <div
                  key={m.monitor_id}
                  className={cn(
                    "space-y-4 rounded-lg border border-border bg-card p-5",
                    state === "failed" && "border-red-300/60 dark:border-red-900/60",
                    state === "warning" && "border-amber-300/60 dark:border-amber-900/60",
                  )}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-semibold text-foreground">{m.group_name}</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        分组 #{m.group_id} · 间隔 {m.interval_minutes}m · 账号 {m.account_count}
                      </p>
                    </div>
                    <OperationsStatusBadge status={state === "healthy" ? "healthy" : state === "warning" ? "warning" : "failed"} />
                  </div>

                  <div className="grid grid-cols-4 gap-2">
                    <div className="rounded-md bg-muted/40 px-3 py-2">
                      <p className="text-[10px] text-muted-foreground">可用率7d</p>
                      <p className={cn("mt-0.5 text-base font-bold tabular-nums", m.availability_7d >= 95 ? "text-emerald-600 dark:text-emerald-400" : m.availability_7d >= 80 ? "text-amber-600 dark:text-amber-400" : "text-red-600 dark:text-red-400")}>
                        {m.probes_7d > 0 ? `${m.availability_7d.toFixed(1)}%` : "--"}
                      </p>
                    </div>
                    <div className="rounded-md bg-muted/40 px-3 py-2">
                      <p className="text-[10px] text-muted-foreground">TTFT</p>
                      <p className="mt-0.5 text-base font-bold tabular-nums text-foreground">
                        {m.avg_ttft_ms_7d > 0 ? formatDurationMS(m.avg_ttft_ms_7d) : "--"}
                      </p>
                    </div>
                    <div className="rounded-md bg-muted/40 px-3 py-2">
                      <p className="text-[10px] text-muted-foreground">缓存率</p>
                      <p className="mt-0.5 text-base font-bold tabular-nums text-foreground">{m.cache_rate_7d.toFixed(0)}%</p>
                    </div>
                    <div className="rounded-md bg-muted/40 px-3 py-2">
                      <p className="text-[10px] text-muted-foreground">探测7d</p>
                      <p className="mt-0.5 text-base font-bold tabular-nums text-foreground">{m.probes_7d}</p>
                    </div>
                  </div>

                  <div className="flex items-center justify-between text-[11px] text-muted-foreground">
                    <span className="flex items-center gap-1">
                      <ShieldCheck className="h-3.5 w-3.5 text-emerald-500" /> 健康 {m.healthy_count}
                    </span>
                    <span className="flex items-center gap-1">
                      <ShieldAlert className="h-3.5 w-3.5 text-red-500" /> 异常 {m.failed_count}
                    </span>
                    <span className="flex items-center gap-1">
                      <Activity className="h-3.5 w-3.5 text-muted-foreground" /> {stateLabel(state)}
                    </span>
                    <span className="flex items-center gap-1">
                      <Gauge className="h-3.5 w-3.5 text-muted-foreground" /> {m.last_run_at ? new Date(m.last_run_at).toLocaleTimeString() : "--"}
                    </span>
                  </div>

                  {/* 健康比例进度条 */}
                  <div className="flex h-2 w-full overflow-hidden rounded-full bg-muted/60">
                    <div className="bg-emerald-500" style={{ width: `${healthPct(m)}%` }} />
                    <div className="bg-red-400" style={{ width: `${failedPct(m)}%` }} />
                    <div className="bg-amber-300" style={{ width: `${unknownPct(m)}%` }} />
                  </div>
                </div>
              )
            })}
          </div>
        </>
      )}
    </section>
  )
}
