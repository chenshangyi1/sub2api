import { useEffect, useState } from "react"
import { BrushCleaning, Cable, CheckCircle2, CircleX, Database, ServerCog, Wrench, Workflow } from "lucide-react"
import { toast } from "sonner"
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
import { useConfirm } from "@/components/ui/confirm-dialog"
import { dateTime, decimal, relativeTime } from "@/lib/format"
import {
  cleanupOperationsInvalidData,
  getOperationsDiagnostics,
  type OperationsDiagnostics,
} from "@/lib/operations-api"

export default function DiagnosticsPage() {
  const [data, setData] = useState<OperationsDiagnostics | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reload, setReload] = useState(0)
  const [cleaning, setCleaning] = useState(false)
  const { confirm, dialog: confirmDialog } = useConfirm()

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    getOperationsDiagnostics()
      .then((result) => {
        if (!cancelled) setData(result)
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : "诊断数据加载失败")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [reload])

  async function cleanup() {
    const invalid = data?.invalid_data
    const count = (invalid?.bindings ?? 0) + (invalid?.managed_accounts ?? 0) + (invalid?.probe_rules ?? 0)
    const accepted = await confirm({
      title: "清理失效运营数据",
      description: `将清理 ${count} 条无法解析到现有目标或账号的记录。同步器和目标业务数据不会被删除。`,
      confirmLabel: "执行清理",
      destructive: true,
    })
    if (!accepted) return
    setCleaning(true)
    try {
      const result = await cleanupOperationsInvalidData()
      const removed = result.bindings + result.managed_accounts + result.probe_rules
      toast.success(`已清理 ${removed} 条失效记录`)
      setReload((value) => value + 1)
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "清理失败")
    } finally {
      setCleaning(false)
    }
  }

  const healthyServices = data?.services.filter((item) => item.status === "healthy").length ?? 0
  const failedTasks = data?.tasks.filter((item) => item.status === "failed").length ?? 0
  const invalidCount = data
    ? data.invalid_data.bindings + data.invalid_data.managed_accounts + data.invalid_data.probe_rules
    : 0

  return (
    <section className="space-y-5">
      <OperationsPageHeader
        icon={Wrench}
        title="诊断中心"
        description="集中查看运营面板、数据库、扩展 Worker、目标连接和最近任务；上游监控、通知、同步与网关日志仍留在各自功能页。"
        refreshing={loading}
        onRefresh={() => setReload((value) => value + 1)}
        actions={
          <Button variant="outline" size="sm" onClick={() => void cleanup()} disabled={cleaning || invalidCount === 0}>
            <BrushCleaning className="size-3.5" />
            清理失效数据
          </Button>
        }
      />

      <OperationsMetricStrip
        items={[
          { label: "健康服务", value: `${healthyServices}/${data?.services.length ?? 0}`, detail: "当前检查通过" },
          { label: "Worker", value: data?.worker.status ?? "unknown", detail: `心跳 ${relativeTime(data?.worker.heartbeat_at)}` },
          { label: "失败任务", value: decimal(failedTasks, 0), detail: "最近任务窗口" },
          { label: "失效记录", value: decimal(invalidCount, 0), detail: "可安全清理的扩展引用" },
        ]}
      />

      {error ? <OperationsError message={error} /> : null}
      {loading && !data ? <OperationsLoading rows={6} /> : null}

      {data ? (
        <>
          <section className="rounded-md border border-border bg-background">
            <div className="border-b border-border px-4 py-3">
              <h3 className="text-sm font-semibold text-foreground">服务状态</h3>
              <p className="mt-1 text-xs text-muted-foreground">仅展示，不在此页面重启数据库或主服务。</p>
            </div>
            <div className="divide-y divide-border">
              {data.services.map((service) => (
                <div key={service.id} className="grid gap-2 px-4 py-3 sm:grid-cols-[minmax(10rem,0.8fr)_7rem_minmax(0,1.7fr)_10rem] sm:items-center">
                  <div className="flex min-w-0 items-center gap-2">
                    <ServerCog className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate text-sm font-medium text-foreground">{service.name}</span>
                  </div>
                  <OperationsStatusBadge status={service.status} />
                  <p className="min-w-0 truncate text-xs text-muted-foreground" title={service.detail}>{service.detail || "—"}</p>
                  <p className="text-xs text-muted-foreground sm:text-right">
                    {service.restart_count != null ? `重启 ${service.restart_count} · ` : ""}{relativeTime(service.checked_at)}
                  </p>
                </div>
              ))}
            </div>
          </section>

          <section className="rounded-md border border-border bg-background">
            <div className="border-b border-border px-4 py-3">
              <h3 className="text-sm font-semibold text-foreground">连接状态</h3>
              <p className="mt-1 text-xs text-muted-foreground">
                面板连接 sub2api 的接入面自检。缺配置时按诊断中心提示补环境变量后重启面板。
              </p>
            </div>
            <div className="divide-y divide-border">
              {(data.connections ?? []).map((connection) => (
                <div key={connection.key} className="flex items-center gap-3 px-4 py-3">
                  {connection.ok ? (
                    <CheckCircle2 className="size-4 shrink-0 text-emerald-600" />
                  ) : (
                    <CircleX className="size-4 shrink-0 text-red-600" />
                  )}
                  <Cable className="size-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate text-sm font-medium text-foreground">
                    {connection.name}
                  </span>
                  <span
                    className="min-w-0 truncate text-xs text-muted-foreground"
                    title={connection.detail}
                  >
                    {connection.detail}
                  </span>
                  <Badge variant={connection.ok ? "default" : "destructive"} className="shrink-0 font-normal">
                    {connection.ok ? "正常" : "异常"}
                  </Badge>
                </div>
              ))}
            </div>
          </section>

          <div className="grid gap-4 lg:grid-cols-2">
            <section className="rounded-md border border-border bg-background p-4">
              <div className="flex items-start gap-3">
                <div className="flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                  <Workflow className="size-4" />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h3 className="text-sm font-semibold text-foreground">扩展 Worker</h3>
                    <OperationsStatusBadge status={data.worker.status} />
                  </div>
                  <p className="mt-1 text-xs text-muted-foreground">最后运行 {relativeTime(data.worker.last_run_at)}</p>
                  <p className="mt-3 text-sm text-foreground">{data.worker.last_run_status || "尚无运行状态"}</p>
                  <p className="mt-1 line-clamp-3 text-xs leading-5 text-muted-foreground">{data.worker.last_run_message || "—"}</p>
                </div>
              </div>
            </section>

            <section className="rounded-md border border-border bg-background p-4">
              <div className="flex items-start gap-3">
                <div className="flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                  <Database className="size-4" />
                </div>
                <div className="min-w-0 flex-1">
                  <h3 className="text-sm font-semibold text-foreground">扩展数据完整性</h3>
                  <p className="mt-1 text-xs text-muted-foreground">迁移账本之外的业务引用检查。</p>
                  <dl className="mt-3 grid grid-cols-3 gap-3 text-center">
                    <div><dt className="text-[11px] text-muted-foreground">绑定</dt><dd className="mt-1 font-mono text-sm">{data.invalid_data.bindings}</dd></div>
                    <div><dt className="text-[11px] text-muted-foreground">托管账号</dt><dd className="mt-1 font-mono text-sm">{data.invalid_data.managed_accounts}</dd></div>
                    <div><dt className="text-[11px] text-muted-foreground">探测规则</dt><dd className="mt-1 font-mono text-sm">{data.invalid_data.probe_rules}</dd></div>
                  </dl>
                </div>
              </div>
            </section>
          </div>

          <section className="rounded-md border border-border bg-background">
            <div className="border-b border-border px-4 py-3">
              <h3 className="text-sm font-semibold text-foreground">最近任务</h3>
            </div>
            {data.tasks.length ? (
              <div className="divide-y divide-border">
                {data.tasks.map((task) => (
                  <div key={task.id} className="grid gap-2 px-4 py-3 sm:grid-cols-[minmax(10rem,1fr)_7rem_minmax(0,1.5fr)_10rem] sm:items-center">
                    <p className="truncate text-sm font-medium text-foreground">{task.name}</p>
                    <OperationsStatusBadge status={task.status} />
                    <p className="truncate text-xs text-muted-foreground" title={task.message}>{task.message || "—"}</p>
                    <p className="text-xs text-muted-foreground sm:text-right">{relativeTime(task.finished_at ?? task.started_at)}</p>
                  </div>
                ))}
              </div>
            ) : (
              <div className="p-4"><OperationsEmpty title="暂无任务记录" description="扩展任务运行后会在这里显示。" /></div>
            )}
          </section>

          <section className="rounded-md border border-border bg-background">
            <div className="border-b border-border px-4 py-3">
              <h3 className="text-sm font-semibold text-foreground">扩展操作日志</h3>
            </div>
            {data.recent_logs.length ? (
              <div className="overflow-x-auto">
                <table className="w-full min-w-[680px] text-left text-sm">
                  <thead className="bg-muted/35 text-xs text-muted-foreground"><tr><th className="px-4 py-2 font-medium">时间</th><th className="px-4 py-2 font-medium">操作</th><th className="px-4 py-2 font-medium">目标</th><th className="px-4 py-2 font-medium">结果</th><th className="px-4 py-2 font-medium">详情</th></tr></thead>
                  <tbody className="divide-y divide-border">
                    {data.recent_logs.map((log) => (
                      <tr key={log.id}>
                        <td className="whitespace-nowrap px-4 py-3 text-xs text-muted-foreground">{dateTime(log.created_at)}</td>
                        <td className="px-4 py-3 font-mono text-xs">{log.action}</td>
                        <td className="px-4 py-3 text-xs">{log.target || "—"}</td>
                        <td className="px-4 py-3"><OperationsStatusBadge status={log.status} /></td>
                        <td className="max-w-96 truncate px-4 py-3 text-xs text-muted-foreground" title={log.message}>{log.message || "—"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="p-4"><OperationsEmpty title="暂无扩展日志" description="运营扩展执行写操作后会在这里显示。" /></div>
            )}
          </section>
        </>
      ) : null}

      {confirmDialog}
    </section>
  )
}
