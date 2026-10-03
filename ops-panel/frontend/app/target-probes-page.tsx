import { useEffect, useState } from "react"
import { FlaskConical, Gauge, Play, Plus, Radar, Trash2, TimerReset } from "lucide-react"
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
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { useOperationsTarget } from "@/hooks/use-operations-target"
import { decimal, formatDurationMS, relativeTime } from "@/lib/format"
import {
  listTargetProbes,
  listTargetAccounts,
  createTargetProbe,
  deleteTargetProbe,
  runTargetProbe,
  runTargetProbeBatch,
  setTargetProbeEnabled,
  type OperationsStatus,
  type TargetProbePage,
  type TargetProbeRunMode,
  type TargetAccount,
} from "@/lib/operations-api"

export default function TargetProbesPage() {
  const target = useOperationsTarget()
  const [data, setData] = useState<TargetProbePage | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reload, setReload] = useState(0)
  const [mode, setMode] = useState<TargetProbeRunMode | "all">("all")
  const [status, setStatus] = useState<OperationsStatus | "all">("all")
  const [busyProbe, setBusyProbe] = useState<number | "batch" | null>(null)
  const [accounts, setAccounts] = useState<TargetAccount[]>([])
  const [accountsLoading, setAccountsLoading] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [deleteProbeID, setDeleteProbeID] = useState<number | null>(null)
  const [createAccountID, setCreateAccountID] = useState("")
  const [createModel, setCreateModel] = useState("")
  const [createGroup, setCreateGroup] = useState("")
  const [createInterval, setCreateInterval] = useState("10")
  const [createFailureThreshold, setCreateFailureThreshold] = useState("3")
  const [createPauseMinutes, setCreatePauseMinutes] = useState("30")
  const [createEnabled, setCreateEnabled] = useState(true)
  const [createSaving, setCreateSaving] = useState(false)

  useEffect(() => {
    if (target.selectedTargetID == null) {
      setData(null)
      return
    }
    let cancelled = false
    setLoading(true)
    setError(null)
    listTargetProbes(target.selectedTargetID)
      .then((result) => {
        if (!cancelled) setData(result)
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : "增强探测加载失败")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [reload, target.selectedTargetID])

  useEffect(() => {
    if (!createOpen || target.selectedTargetID == null) return
    let cancelled = false
    setAccountsLoading(true)
    listTargetAccounts(target.selectedTargetID, { page: 1, pageSize: 200, schedule: "all" })
      .then((result) => {
        if (!cancelled) {
          setAccounts(result.items)
          setCreateAccountID((current) => current || (result.items[0] ? String(result.items[0].id) : ""))
        }
      })
      .catch((reason: unknown) => {
        if (!cancelled) toast.error(reason instanceof Error ? reason.message : "目标账号加载失败")
      })
      .finally(() => {
        if (!cancelled) setAccountsLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [createOpen, target.selectedTargetID])

  async function runProbe(probeID: number) {
    if (target.selectedTargetID == null) return
    setBusyProbe(probeID)
    try {
      await runTargetProbe(target.selectedTargetID, probeID)
      toast.success("探测已提交")
      setReload((value) => value + 1)
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "探测执行失败")
    } finally {
      setBusyProbe(null)
    }
  }

  async function runBatch() {
    if (target.selectedTargetID == null || items.length === 0) return
    setBusyProbe("batch")
    try {
      const result = await runTargetProbeBatch(target.selectedTargetID, {
        mode,
        probeIDs: items.map((item) => item.id),
        accountIDs: Array.from(
          new Set(items.flatMap((item) => (item.account_id == null ? [] : [item.account_id]))),
        ),
      })
      toast.success(`已提交 ${result.queued} 项探测`)
      setReload((value) => value + 1)
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "批量探测提交失败")
    } finally {
      setBusyProbe(null)
    }
  }

  async function toggleProbe(probeID: number, enabled: boolean) {
    if (target.selectedTargetID == null) return
    setBusyProbe(probeID)
    try {
      await setTargetProbeEnabled(target.selectedTargetID, probeID, enabled)
      setData((current) =>
        current
          ? {
              ...current,
              items: current.items.map((item) => (item.id === probeID ? { ...item, enabled } : item)),
            }
          : current,
      )
      toast.success(enabled ? "探测已启用" : "探测已停用")
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "探测状态保存失败")
    } finally {
      setBusyProbe(null)
    }
  }

  async function createProbe() {
    if (target.selectedTargetID == null || !createAccountID) {
      toast.error("请选择目标账号")
      return
    }
    setCreateSaving(true)
    try {
      await createTargetProbe(target.selectedTargetID, {
        account_id: Number(createAccountID),
        model_id: createModel.trim() || undefined,
        group_name: createGroup.trim() || undefined,
        enabled: createEnabled,
        check_interval_minutes: Number(createInterval),
        failure_threshold: Number(createFailureThreshold),
        pause_minutes: Number(createPauseMinutes),
      })
      toast.success("增强探测已添加")
      setCreateOpen(false)
      setReload((value) => value + 1)
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "增强探测添加失败")
    } finally {
      setCreateSaving(false)
    }
  }

  async function removeProbe() {
    if (target.selectedTargetID == null || deleteProbeID == null) return
    setBusyProbe(deleteProbeID)
    try {
      await deleteTargetProbe(target.selectedTargetID, deleteProbeID)
      toast.success("增强探测已删除")
      setDeleteProbeID(null)
      setReload((value) => value + 1)
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "增强探测删除失败")
    } finally {
      setBusyProbe(null)
    }
  }

  const items = (data?.items ?? []).filter(
    (item) => (mode === "all" || item.mode === mode) && (status === "all" || item.status === status),
  )
  const summary = data?.summary ?? { total: 0, healthy: 0, warning: 0, error: 0 }

  return (
    <section className="space-y-5">
      <OperationsPageHeader
        icon={Radar}
        title="增强探测"
        description="保留目标账号的凭据轻检、流式重量检测、TTFT/TPS 采样和历史能力结果；源渠道登录与倍率采集仍由上游监控负责。"
        targets={target.targets}
        selectedTargetID={target.selectedTargetID}
        targetLoading={target.loading}
        onTargetChange={target.selectTarget}
        refreshing={loading}
        onRefresh={() => setReload((value) => value + 1)}
        actions={
          <>
            <Button size="sm" variant="outline" onClick={() => setCreateOpen(true)} disabled={target.selectedTargetID == null}>
              <Plus className="size-3.5" />
              添加探测
            </Button>
            <Button size="sm" onClick={() => void runBatch()} disabled={busyProbe === "batch" || target.selectedTargetID == null || items.length === 0}>
              <Play className="size-3.5" />
              执行当前范围
            </Button>
          </>
        }
      />

      {target.error ? <OperationsError title="目标站点加载失败" message={target.error} /> : null}

      <OperationsMetricStrip
        items={[
          { label: "探测规则", value: decimal(summary.total, 0) },
          { label: "健康", value: decimal(summary.healthy, 0), detail: "最近检测通过" },
          { label: "需关注", value: decimal(summary.warning, 0), detail: "慢响应或部分能力缺失" },
          { label: "失败", value: decimal(summary.error, 0), detail: "最近检测不可用" },
        ]}
      />

      <div className="flex flex-col gap-2 border-b border-border pb-4 sm:flex-row sm:items-center">
        <Select value={mode} onValueChange={(value) => setMode(value as TargetProbeRunMode | "all")}>
          <SelectTrigger className="w-full bg-background sm:w-44" aria-label="探测模式">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部模式</SelectItem>
            <SelectItem value="light">轻量检测</SelectItem>
            <SelectItem value="heavy">重量检测</SelectItem>
          </SelectContent>
        </Select>
        <Select value={status} onValueChange={(value) => setStatus(value as OperationsStatus | "all")}>
          <SelectTrigger className="w-full bg-background sm:w-40" aria-label="探测状态">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            <SelectItem value="healthy">健康</SelectItem>
            <SelectItem value="warning">需关注</SelectItem>
            <SelectItem value="error">失败</SelectItem>
            <SelectItem value="unknown">未检测</SelectItem>
          </SelectContent>
        </Select>
        <p className="text-xs text-muted-foreground sm:ml-auto">显示 {items.length} 项</p>
      </div>

      {error ? <OperationsError message={error} /> : null}
      {loading && !data ? <OperationsLoading rows={5} /> : null}
      {!loading && !error && items.length === 0 ? (
        <OperationsEmpty title="没有匹配的探测规则" description="调整模式或状态筛选后重试。" />
      ) : null}

      {items.length ? (
        <div className="overflow-hidden rounded-md border border-border">
          <div className="hidden grid-cols-[minmax(0,1.35fr)_minmax(10rem,1fr)_9rem_10rem_9rem] gap-3 border-b border-border bg-muted/35 px-4 py-2 text-xs font-medium text-muted-foreground md:grid">
            <span>目标</span>
            <span>检测模型</span>
            <span>质量</span>
            <span>最近结果</span>
            <span className="text-right">控制</span>
          </div>
          <div className="divide-y divide-border">
            {items.map((probe) => {
              const pending = busyProbe === probe.id
              return (
                <div
                  key={probe.id}
                  className="grid gap-3 px-4 py-3 md:grid-cols-[minmax(0,1.35fr)_minmax(10rem,1fr)_9rem_10rem_9rem] md:items-center"
                >
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="truncate text-sm font-medium text-foreground">{probe.name}</span>
                      <Badge variant="outline" className="font-normal">{probe.mode}</Badge>
                    </div>
                    <p className="mt-1 truncate text-xs text-muted-foreground">
                      {probe.group_name || "未绑定分组"} · {probe.provider || "未知平台"}
                      {probe.account_id ? ` · 账号 #${probe.account_id}` : ""}
                    </p>
                    <p className="mt-1 truncate font-mono text-[11px] text-muted-foreground" title={probe.endpoint}>
                      {probe.endpoint}
                    </p>
                  </div>

                  <div className="min-w-0">
                    <p className="truncate font-mono text-xs text-foreground">{probe.model || "自动选择"}</p>
                    <p className="mt-1 truncate text-[11px] text-muted-foreground">
                      {(probe.candidate_models?.length ?? 0) > 0
                        ? `${probe.candidate_models?.length} 个候选模型`
                        : "固定检测模型"}
                    </p>
                    {probe.capability_total ? (
                      <div className="mt-1 flex items-center gap-1 text-[11px] text-muted-foreground">
                        <FlaskConical className="size-3" />
                        能力 {probe.capability_passed ?? 0}/{probe.capability_total}
                      </div>
                    ) : null}
                  </div>

                  <div className="space-y-1 text-xs">
                    <p className="flex items-center gap-1.5"><Gauge className="size-3.5 text-muted-foreground" />{formatDurationMS(probe.first_token_ms)} TTFT</p>
                    <p className="text-muted-foreground">
                      {probe.tokens_per_second == null ? "—" : `${decimal(probe.tokens_per_second, 1)} tok/s`}
                    </p>
                    <p className="text-muted-foreground">
                      7 日 {probe.availability_7d == null ? "—" : `${decimal(probe.availability_7d, 2)}%`}
                    </p>
                  </div>

                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <OperationsStatusBadge status={probe.status} />
                      <span className="text-xs text-muted-foreground">{formatDurationMS(probe.latency_ms)}</span>
                    </div>
                    <p className="mt-1 text-xs text-muted-foreground">{relativeTime(probe.last_checked_at)}</p>
                    {probe.last_error ? (
                      <p className="mt-1 line-clamp-2 text-[11px] text-red-600 dark:text-red-400">{probe.last_error}</p>
                    ) : (
                      <p className="mt-1 flex items-center gap-1 text-[11px] text-muted-foreground">
                        <TimerReset className="size-3" />下次 {relativeTime(probe.next_run_at)}
                      </p>
                    )}
                  </div>

                  <div className="flex items-center justify-between gap-2 md:justify-end">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => void runProbe(probe.id)}
                      disabled={pending}
                      title="立即检测"
                      aria-label="立即检测"
                    >
                      <Play className="size-4" />
                    </Button>
                    <span className="text-xs text-muted-foreground">{probe.enabled ? "运行中" : "已停用"}</span>
                    <Switch
                      checked={probe.enabled}
                      disabled={pending}
                      onCheckedChange={(enabled) => void toggleProbe(probe.id, enabled)}
                      aria-label={`${probe.name} 启用状态`}
                    />
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => setDeleteProbeID(probe.id)}
                      disabled={pending}
                      title="删除探测"
                      aria-label="删除探测"
                    >
                      <Trash2 className="size-4 text-red-600" />
                    </Button>
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      ) : null}
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-h-[calc(100vh-2rem)] overflow-y-auto sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>添加增强探测</DialogTitle>
            <DialogDescription>为目标账号创建一条独立探测规则，默认使用轻量检测。</DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-2">
            <div className="space-y-2">
              <Label>目标账号</Label>
              <Select value={createAccountID} onValueChange={setCreateAccountID} disabled={accountsLoading || accounts.length === 0}>
                <SelectTrigger><SelectValue placeholder={accountsLoading ? "加载账号" : "选择账号"} /></SelectTrigger>
                <SelectContent>
                  {accounts.map((account) => <SelectItem key={account.id} value={String(account.id)}>{account.name} · #{account.id}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-2"><Label htmlFor="probe-model">检测模型（可选）</Label><Input id="probe-model" value={createModel} onChange={(event) => setCreateModel(event.target.value)} placeholder="自动选择" /></div>
              <div className="space-y-2"><Label htmlFor="probe-group">目标分组（可选）</Label><Input id="probe-group" value={createGroup} onChange={(event) => setCreateGroup(event.target.value)} placeholder="默认分组" /></div>
            </div>
            <div className="grid gap-3 sm:grid-cols-3">
              <div className="space-y-2"><Label htmlFor="probe-interval">间隔（分钟）</Label><Input id="probe-interval" type="number" min="1" max="1440" value={createInterval} onChange={(event) => setCreateInterval(event.target.value)} /></div>
              <div className="space-y-2"><Label htmlFor="probe-threshold">失败次数</Label><Input id="probe-threshold" type="number" min="1" max="100" value={createFailureThreshold} onChange={(event) => setCreateFailureThreshold(event.target.value)} /></div>
              <div className="space-y-2"><Label htmlFor="probe-pause">暂停（分钟）</Label><Input id="probe-pause" type="number" min="0" max="10080" value={createPauseMinutes} onChange={(event) => setCreatePauseMinutes(event.target.value)} /></div>
            </div>
            <div className="flex items-center justify-between rounded-md border border-border px-3 py-2">
              <div><Label htmlFor="probe-enabled">创建后启用</Label><p className="text-xs text-muted-foreground">启用后由 worker 按间隔自动检测</p></div>
              <Switch id="probe-enabled" checked={createEnabled} onCheckedChange={setCreateEnabled} />
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setCreateOpen(false)}>取消</Button>
            <Button type="button" onClick={() => void createProbe()} disabled={createSaving || accountsLoading || !createAccountID}>{createSaving ? "保存中" : "保存"}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <AlertDialog open={deleteProbeID != null} onOpenChange={(open) => { if (!open && busyProbe == null) setDeleteProbeID(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader><AlertDialogTitle>删除增强探测？</AlertDialogTitle><AlertDialogDescription>该规则和关联的原生监控会一并移除，删除后需要重新添加。</AlertDialogDescription></AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={busyProbe != null}>取消</AlertDialogCancel>
            <AlertDialogAction onClick={(event) => { event.preventDefault(); void removeProbe() }} disabled={busyProbe != null}>{busyProbe != null ? "删除中" : "确认删除"}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  )
}
