import { useEffect, useState } from "react"
import { Bot, Play, Save } from "lucide-react"
import { toast } from "sonner"
import {
  OperationsError,
  OperationsLoading,
  OperationsPageHeader,
  OperationsStatusBadge,
} from "@/components/operations/operations-layout"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { useOperationsTarget } from "@/hooks/use-operations-target"
import { relativeTime } from "@/lib/format"
import {
  applyTargetAutomation,
  getPoolExecutionMode,
  getTargetAutomationSettings,
  saveTargetAutomationSettings,
  setPoolExecutionMode,
  type TargetAutomationSettings,
} from "@/lib/operations-api"

export function AutomationSettings() {
  const target = useOperationsTarget()
  const [form, setForm] = useState<TargetAutomationSettings | null>(null)
  const [priorityGroupIDs, setPriorityGroupIDs] = useState("")
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [applying, setApplying] = useState(false)
  const [reload, setReload] = useState(0)
  const [executionMode, setExecutionMode] = useState<"local" | "delegated">("local")

  useEffect(() => {
    let cancelled = false
    getPoolExecutionMode()
      .then((result) => {
        if (!cancelled) setExecutionMode(result.mode === "delegated" ? "delegated" : "local")
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [])

  async function switchExecutionMode(mode: "local" | "delegated") {
    try {
      const result = await setPoolExecutionMode(mode)
      setExecutionMode(result.mode === "delegated" ? "delegated" : "local")
      toast.success(mode === "delegated" ? "执行权已移交 sub2api 内置调度器" : "执行权已回到面板 worker")
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "执行权切换失败")
    }
  }

  useEffect(() => {
    if (target.selectedTargetID == null) {
      setForm(null)
      setPriorityGroupIDs("")
      return
    }
    let cancelled = false
    setForm(null)
    setPriorityGroupIDs("")
    setLoading(true)
    setError(null)
    getTargetAutomationSettings(target.selectedTargetID)
      .then((result) => {
        if (!cancelled) {
          setForm(result)
          setPriorityGroupIDs(result.account_priority.target_group_ids.join(", "))
        }
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : "自动化设置加载失败")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [reload, target.selectedTargetID])

  function patchPool<K extends keyof TargetAutomationSettings["account_pool"]>(
    key: K,
    value: TargetAutomationSettings["account_pool"][K],
  ) {
    setForm((current) =>
      current ? { ...current, account_pool: { ...current.account_pool, [key]: value } } : current,
    )
  }

  function patchPriority<K extends keyof TargetAutomationSettings["account_priority"]>(
    key: K,
    value: TargetAutomationSettings["account_priority"][K],
  ) {
    setForm((current) =>
      current ? { ...current, account_priority: { ...current.account_priority, [key]: value } } : current,
    )
  }

  async function save() {
    const input = formWithPriorityGroups(form, priorityGroupIDs)
    if (!input || target.selectedTargetID == null) return
    setSaving(true)
    try {
      const saved = await saveTargetAutomationSettings(target.selectedTargetID, input)
      setForm(saved)
      setPriorityGroupIDs(saved.account_priority.target_group_ids.join(", "))
      toast.success("自动化策略已保存")
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "自动化策略保存失败")
    } finally {
      setSaving(false)
    }
  }

  async function apply() {
    const input = formWithPriorityGroups(form, priorityGroupIDs)
    if (!input || target.selectedTargetID == null) return
    setApplying(true)
    try {
      const saved = await saveTargetAutomationSettings(target.selectedTargetID, input)
      setForm(saved)
      setPriorityGroupIDs(saved.account_priority.target_group_ids.join(", "))
      const result = await applyTargetAutomation(target.selectedTargetID)
      setForm(result)
      setPriorityGroupIDs(result.account_priority.target_group_ids.join(", "))
      toast.success("自动化策略已提交执行")
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "自动化策略执行失败")
    } finally {
      setApplying(false)
    }
  }

  return (
    <div className="space-y-5">
      <OperationsPageHeader
        icon={Bot}
        title="目标自动化"
        description="账号池健康回归、并发与负载分配、价格保护和账号优先级统一在此配置；倍率计算仍由上游同步器执行。"
        targets={target.targets}
        selectedTargetID={target.selectedTargetID}
        targetLoading={target.loading}
        onTargetChange={target.selectTarget}
        refreshing={loading}
        onRefresh={() => setReload((value) => value + 1)}
        actions={
          <>
            <Button variant="outline" size="sm" onClick={() => void save()} disabled={!form || saving || applying}>
              <Save className="size-3.5" />保存
            </Button>
            <Button size="sm" onClick={() => void apply()} disabled={!form || applying || saving}>
              <Play className="size-3.5" />立即应用
            </Button>
          </>
        }
      />

      {target.error ? <OperationsError title="目标站点加载失败" message={target.error} /> : null}
      {error ? <OperationsError message={error} /> : null}

      <section className="space-y-2 rounded-md border border-border bg-background p-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold text-foreground">池策略执行权</h3>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              delegated：由 sub2api 内置健康调度器执行（被动信号、零额外探测、429 不摘除）；local：由本面板 worker 周期执行。
            </p>
          </div>
          <Select value={executionMode} onValueChange={(value) => void switchExecutionMode(value === "delegated" ? "delegated" : "local")}>
            <SelectTrigger className="w-44"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="delegated">sub2api 内置</SelectItem>
              <SelectItem value="local">面板 worker</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </section>

      {loading && !form ? <OperationsLoading rows={5} /> : null}

      {form ? (
        <>
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <OperationsStatusBadge status={form.last_apply_status || "unknown"} />
            <span>最近应用 {relativeTime(form.last_applied_at)}</span>
            {form.last_apply_message ? <span className="min-w-0 truncate">· {form.last_apply_message}</span> : null}
          </div>

          <div className="grid gap-5 xl:grid-cols-2">
            <section className="space-y-4 rounded-md border border-border bg-background p-4">
              <div><h3 className="text-sm font-semibold text-foreground">账号池策略</h3><p className="mt-1 text-xs leading-5 text-muted-foreground">按健康、负载和价格边界维护可用账号池。</p></div>
              <PolicySwitch id="health-return" label="健康账号回池" checked={form.account_pool.health_return_enabled} onCheckedChange={(value) => patchPool("health_return_enabled", value)} />
              <PolicyNumber label="健康阈值" value={form.account_pool.health_return_threshold} onChange={(value) => patchPool("health_return_threshold", value)} suffix="%" min={1} max={100} />
              <PolicySwitch id="smart-expansion" label="智能并发扩容" checked={form.account_pool.smart_expansion_enabled} onCheckedChange={(value) => patchPool("smart_expansion_enabled", value)} />
              <div className="grid gap-3 sm:grid-cols-3">
                <PolicyNumber label="总并发" value={form.account_pool.total_concurrency} onChange={(value) => patchPool("total_concurrency", value)} min={1} />
                <PolicyNumber label="账号下限" value={form.account_pool.min_account_concurrency} onChange={(value) => patchPool("min_account_concurrency", value)} min={1} />
                <PolicyNumber label="账号上限" value={form.account_pool.max_account_concurrency} onChange={(value) => patchPool("max_account_concurrency", value)} min={1} />
              </div>
              <PolicySwitch id="load-factor" label="负载因子分配" checked={form.account_pool.load_factor_enabled} onCheckedChange={(value) => patchPool("load_factor_enabled", value)} />
              <PolicyNumber label="总负载因子" value={form.account_pool.total_load_factor} onChange={(value) => patchPool("total_load_factor", value)} min={1} />
              <div className="grid gap-3 sm:grid-cols-2">
                <PolicySwitch id="price-protection" label="价格保护" checked={form.account_pool.price_protection_enabled} onCheckedChange={(value) => patchPool("price_protection_enabled", value)} />
                <PolicySwitch id="failure-disable" label="失败自动暂停" checked={form.account_pool.failure_disable_enabled} onCheckedChange={(value) => patchPool("failure_disable_enabled", value)} />
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <PolicyNumber label="最少可用账号" value={form.account_pool.min_available_accounts} onChange={(value) => patchPool("min_available_accounts", value)} min={1} />
                <PolicyNumber label="目标健康账号" value={form.account_pool.target_healthy_accounts} onChange={(value) => patchPool("target_healthy_accounts", value)} min={1} />
              </div>
            </section>

            <section className="space-y-4 rounded-md border border-border bg-background p-4">
              <div><h3 className="text-sm font-semibold text-foreground">账号优先级</h3><p className="mt-1 text-xs leading-5 text-muted-foreground">基于倍率或 TTFT 与倍率组合计算目标账号优先级。</p></div>
              <PolicySwitch id="priority-enabled" label="启用优先级策略" checked={form.account_priority.enabled} onCheckedChange={(value) => patchPriority("enabled", value)} />
              <div className="space-y-2">
                <Label htmlFor="priority-strategy">策略</Label>
                <Select value={form.account_priority.strategy} onValueChange={(value) => patchPriority("strategy", value as "rate" | "latency_rate")}>
                  <SelectTrigger id="priority-strategy"><SelectValue /></SelectTrigger>
                  <SelectContent><SelectItem value="rate">倍率优先</SelectItem><SelectItem value="latency_rate">TTFT + 倍率</SelectItem></SelectContent>
                </Select>
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <PolicyNumber label="样本数" value={form.account_priority.sample_size} onChange={(value) => patchPriority("sample_size", value)} min={1} max={200} />
                <PolicyNumber label="回看窗口" value={form.account_priority.lookback_minutes} onChange={(value) => patchPriority("lookback_minutes", value)} suffix="分钟" min={1} />
                <PolicyNumber label="首字系数" value={form.account_priority.first_token_coefficient} onChange={(value) => patchPriority("first_token_coefficient", value)} min={0} step="0.01" />
                <PolicyNumber label="倍率系数" value={form.account_priority.rate_coefficient} onChange={(value) => patchPriority("rate_coefficient", value)} min={0} step="0.01" />
              </div>
              <PolicyNumber label="缺失样本惩罚" value={form.account_priority.missing_sample_penalty_ms} onChange={(value) => patchPriority("missing_sample_penalty_ms", value)} suffix="ms" min={0} />
              <div className="space-y-2">
                <Label htmlFor="priority-groups">目标分组 ID</Label>
                <Input
                  id="priority-groups"
                  value={priorityGroupIDs}
                  onChange={(event) => setPriorityGroupIDs(event.target.value)}
                  placeholder="留空表示所有分组"
                />
              </div>
            </section>
          </div>
        </>
      ) : null}
    </div>
  )
}

function PolicySwitch({ id, label, checked, onCheckedChange }: { id: string; label: string; checked: boolean; onCheckedChange: (checked: boolean) => void }) {
  return <div className="flex items-center justify-between gap-4 border-y border-border py-3"><Label htmlFor={id}>{label}</Label><Switch id={id} checked={checked} onCheckedChange={onCheckedChange} /></div>
}

function PolicyNumber({ label, value, onChange, suffix, min, max, step = "1" }: { label: string; value: number; onChange: (value: number) => void; suffix?: string; min?: number; max?: number; step?: string }) {
  const id = `policy-${label}`
  return <div className="space-y-2"><Label htmlFor={id}>{label}</Label><div className="relative"><Input id={id} type="number" value={value} min={min} max={max} step={step} onChange={(event) => onChange(Number(event.target.value))} className={suffix ? "pr-14" : undefined} />{suffix ? <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-xs text-muted-foreground">{suffix}</span> : null}</div></div>
}

function formWithPriorityGroups(
  form: TargetAutomationSettings | null,
  rawGroupIDs: string,
): TargetAutomationSettings | null {
  if (!form) return null
  const targetGroupIDs = parseIDs(rawGroupIDs)
  if (targetGroupIDs == null) {
    toast.error("目标分组 ID 只能填写正整数")
    return null
  }
  return {
    ...form,
    account_priority: {
      ...form.account_priority,
      target_group_ids: targetGroupIDs,
    },
  }
}

function parseIDs(raw: string) {
  if (!raw.trim()) return []
  const values = raw.split(/[，,\s]+/).filter(Boolean).map(Number)
  if (values.some((value) => !Number.isSafeInteger(value) || value <= 0)) return null
  return Array.from(new Set(values))
}
