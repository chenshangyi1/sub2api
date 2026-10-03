import { useDeferredValue, useEffect, useRef, useState } from "react"
import {
  AlertTriangle,
  CircleCheck,
  Eraser,
  Link2,
  Loader2,
  Pencil,
  Plus,
  Search,
  Trash2,
  UsersRound,
  WalletCards,
} from "lucide-react"
import { toast } from "sonner"
import { ChannelAPIKeysDialog } from "@/components/monitor/channel-api-keys-dialog"
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
import { Checkbox } from "@/components/ui/checkbox"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { useOperationsTarget } from "@/hooks/use-operations-target"
import type { Channel } from "@/lib/api-types"
import { decimal, money, relativeTime } from "@/lib/format"
import { useChannels } from "@/lib/queries"
import {
  applyTargetRatePolicies,
  deleteTargetAccount,
  getTargetRatePolicies,
  listTargetAccounts,
  saveTargetRatePolicy,
  runTargetAccountAction,
  type RatePolicyMode,
  type RatePolicySource,
  type RatePolicyTarget,
  type RatePolicyWorkspace,
  type TargetAccountPage,
  type TargetAccountScheduleFilter,
} from "@/lib/operations-api"
import { cn } from "@/lib/utils"

function isUsableRateSource(source: RatePolicySource) {
  return source.enabled
    && source.fresh
    && (source.last_status ?? "").toLowerCase() === "online"
    && Number.isFinite(source.rate)
    && source.rate > 0
}

function calculateRatePreview(mode: RatePolicyMode, rates: number[], offset: number) {
  if (mode === "locked") return Number.isFinite(offset) && offset > 0 ? offset : null
  if (rates.length === 0 || !Number.isFinite(offset)) return null

  let base: number
  switch (mode) {
    case "max":
      base = Math.max(...rates)
      break
    case "min":
      base = Math.min(...rates)
      break
    case "average":
      base = rates.reduce((sum, rate) => sum + rate, 0) / rates.length
      break
    case "first":
      base = rates[0]
      break
    default:
      return null
  }
  return Math.round((base + offset + Number.EPSILON) * 10_000) / 10_000
}

export default function TargetAccountsPage() {
  const target = useOperationsTarget()
  const channels = useChannels()
  const [search, setSearch] = useState("")
  const deferredSearch = useDeferredValue(search)
  const [schedule, setSchedule] = useState<TargetAccountScheduleFilter>("all")
  const [page, setPage] = useState(1)
  const [data, setData] = useState<TargetAccountPage | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reload, setReload] = useState(0)
  const [busyAccount, setBusyAccount] = useState<number | null>(null)
  const [rateWorkspace, setRateWorkspace] = useState<RatePolicyWorkspace | null>(null)
  const [rateLoading, setRateLoading] = useState(false)
  const [rateError, setRateError] = useState<string | null>(null)
  const [rateEditor, setRateEditor] = useState<RatePolicyTarget | null>(null)
  const [rateEditorOpen, setRateEditorOpen] = useState(false)
  const [draftEnabled, setDraftEnabled] = useState(false)
  const [draftMode, setDraftMode] = useState<RatePolicyMode>("max")
  const [draftOffset, setDraftOffset] = useState("0")
  const [draftSourceKeys, setDraftSourceKeys] = useState<string[]>([])
  const [draftSourceChannelID, setDraftSourceChannelID] = useState("all")
  const [rateSourceSearch, setRateSourceSearch] = useState("")
  const [rateSaving, setRateSaving] = useState(false)
  const rateDialogScrollPosition = useRef({ left: 0, top: 0 })
  const [selectedImportChannelID, setSelectedImportChannelID] = useState("")
  const [apiKeyChannel, setAPIKeyChannel] = useState<Channel | null>(null)
  const { confirm, dialog: confirmDialog } = useConfirm()

  useEffect(() => {
    const available = channels.data ?? []
    setSelectedImportChannelID((current) =>
      available.some((channel) => String(channel.id) === current)
        ? current
        : available[0]
          ? String(available[0].id)
          : "",
    )
  }, [channels.data])

  useEffect(() => {
    setPage(1)
  }, [deferredSearch, schedule, target.selectedTargetID])

  useEffect(() => {
    if (target.selectedTargetID == null) {
      setData(null)
      return
    }
    let cancelled = false
    setLoading(true)
    setError(null)
    listTargetAccounts(target.selectedTargetID, {
      page,
      pageSize: 50,
      search: deferredSearch,
      schedule,
    })
      .then((result) => {
        if (!cancelled) setData(result)
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : "目标账号加载失败")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [deferredSearch, page, reload, schedule, target.selectedTargetID])

  useEffect(() => {
    if (target.selectedTargetID == null) {
      setRateWorkspace(null)
      return
    }
    let cancelled = false
    setRateLoading(true)
    setRateError(null)
    getTargetRatePolicies(target.selectedTargetID)
      .then((result) => {
        if (!cancelled) setRateWorkspace(result)
      })
      .catch((reason: unknown) => {
        if (!cancelled) setRateError(reason instanceof Error ? reason.message : "倍率同步规则加载失败")
      })
      .finally(() => {
        if (!cancelled) setRateLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [reload, target.selectedTargetID])

  function sourceKey(source: RatePolicySource) {
    return `${source.channel_id}:${source.group_id}`
  }

  function openRateEditor(targetType: "group" | "account", objectID: number) {
    const item = (targetType === "account" ? rateWorkspace?.accounts : rateWorkspace?.groups)
      ?.find((targetItem) => targetItem.id === objectID)
    if (!item) return
    const bindings = item.bindings ?? []
    const mode = item.rule.mode ?? "first"
    setRateEditor(item)
    setDraftEnabled(item.rule.enabled)
    setDraftMode(targetType === "group" && !item.rule.enabled && mode === "first" ? "max" : mode)
    setDraftOffset(String(item.rule.offset ?? 0))
    const enabledSourceKeys = new Set(
      (rateWorkspace?.sources ?? [])
        .filter((source) => source.enabled)
        .map(sourceKey),
    )
    const editableBindings = (targetType === "group" ? bindings : bindings.slice(0, 1))
      .filter((binding) => enabledSourceKeys.has(`${binding.channel_id}:${binding.group_id}`))
    setDraftSourceKeys(editableBindings.map((binding) => `${binding.channel_id}:${binding.group_id}`))
    setDraftSourceChannelID(editableBindings.length === 1 ? String(editableBindings[0].channel_id) : "all")
    setRateSourceSearch("")
    rateDialogScrollPosition.current = { left: window.scrollX, top: window.scrollY }
    setRateEditorOpen(true)
  }

  function closeRateEditor() {
    setRateEditorOpen(false)
    requestAnimationFrame(() => {
      window.scrollTo(rateDialogScrollPosition.current)
    })
  }

  function toggleDraftSource(key: string, checked: boolean) {
    setDraftSourceKeys((current) => checked
      ? current.includes(key) ? current : [...current, key]
      : current.filter((item) => item !== key))
  }

  async function saveRateEditor() {
    if (!rateEditor || target.selectedTargetID == null) return
    const sources = (rateWorkspace?.sources ?? []).filter(
      (item) => item.enabled && draftSourceKeys.includes(sourceKey(item)),
    )
    const needsSource = draftMode !== "locked" && draftMode !== "manual_source"
    if (draftEnabled && needsSource && sources.length === 0) {
      toast.error(`启用同步前请选择${rateEditor.target_type === "group" ? "至少一个" : "一个"}采集源`)
      return
    }
    if (draftEnabled && draftMode === "locked" && Number(draftOffset) <= 0) {
      toast.error("手动锁定倍率必须大于 0")
      return
    }
    setRateSaving(true)
    try {
      await saveTargetRatePolicy(target.selectedTargetID, rateEditor.target_type, rateEditor.id, {
        enabled: draftEnabled,
        mode: draftMode,
        offset: Number(draftOffset || 0),
        bindings: sources.map((source) => ({ channel_id: source.channel_id, group_id: source.group_id })),
      })
      await applyTargetRatePolicies(target.selectedTargetID)
      closeRateEditor()
      toast.success(`${rateEditor.target_type === "account" ? "账号" : "分组"}倍率同步规则已保存，worker 已排队应用`)
      setReload((value) => value + 1)
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "倍率同步规则保存失败")
    } finally {
      setRateSaving(false)
    }
  }

  async function runAction(
    accountID: number,
    action: "enable" | "disable" | "clear_error" | "refresh" | "check_balance",
  ) {
    if (target.selectedTargetID == null) return
    setBusyAccount(accountID)
    try {
      await runTargetAccountAction(target.selectedTargetID, accountID, action)
      toast.success(
        action === "enable"
          ? "账号已加入调度"
          : action === "disable"
            ? "账号已暂停调度"
            : action === "clear_error"
              ? "账号错误已清除"
              : "账号状态已刷新",
      )
      setReload((value) => value + 1)
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "账号操作失败")
    } finally {
      setBusyAccount(null)
    }
  }

  async function deleteAccount(accountID: number, accountName: string) {
    if (target.selectedTargetID == null) return
    const ok = await confirm({
      title: `删除账号 ${accountName}？`,
      description: "删除后该目标账号不可恢复，倍率同步绑定将一并移除。",
      confirmLabel: "删除",
      destructive: true,
    })
    if (!ok) return
    setBusyAccount(accountID)
    try {
      await deleteTargetAccount(target.selectedTargetID, accountID)
      toast.success("账号已删除")
      setReload((value) => value + 1)
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "删除账号失败")
    } finally {
      setBusyAccount(null)
    }
  }

  const summary = data?.summary ?? { total: 0, schedulable: 0, errors: 0, balance_low: 0, temporary_unavailable: 0 }
  const enabledRateSources = (rateWorkspace?.sources ?? []).filter((source) => source.enabled)
  const rateSourceChannels = Array.from(
    new Map(enabledRateSources.map((source) => [source.channel_id, source])).values(),
  )
  const normalizedRateSourceSearch = rateSourceSearch.trim().toLowerCase()
  const filteredRateSources = enabledRateSources.filter((source) => {
    if (draftSourceChannelID !== "all" && String(source.channel_id) !== draftSourceChannelID) return false
    if (!normalizedRateSourceSearch) return true
    return [source.channel_name, source.channel_type, source.group_name, source.group_id, source.last_status]
      .some((value) => String(value ?? "").toLowerCase().includes(normalizedRateSourceSearch))
  })
  const sortedFilteredRateSources = filteredRateSources
    .map((source, index) => ({ source, index }))
    .sort(({ source: left, index: leftIndex }, { source: right, index: rightIndex }) => {
      const selectedDifference = Number(draftSourceKeys.includes(sourceKey(right)))
        - Number(draftSourceKeys.includes(sourceKey(left)))
      if (selectedDifference !== 0) return selectedDifference
      return leftIndex - rightIndex
    })
    .map(({ source }) => source)
  const selectedRateSources = enabledRateSources
    .filter((source) => draftSourceKeys.includes(sourceKey(source)))
    .sort((left, right) => left.channel_id - right.channel_id
      || (left.group_id < right.group_id ? -1 : left.group_id > right.group_id ? 1 : 0))
  const selectedRateExclusionKeys = new Set(
    (rateWorkspace?.exclusions ?? [])
      .filter((exclusion) => exclusion.target_type === rateEditor?.target_type && exclusion.target_id === rateEditor?.id)
      .map((exclusion) => `${exclusion.channel_id}:${exclusion.group_id}`),
  )
  const usableSelectedRateSources = selectedRateSources.filter(
    (source) => isUsableRateSource(source) && !selectedRateExclusionKeys.has(sourceKey(source)),
  )
  const selectedRates = usableSelectedRateSources.map((source) => source.rate)
  const selectedRateMinimum = selectedRates.length ? Math.min(...selectedRates) : null
  const selectedRateMaximum = selectedRates.length ? Math.max(...selectedRates) : null
  const selectedRateAverage = selectedRates.length
    ? selectedRates.reduce((sum, rate) => sum + rate, 0) / selectedRates.length
    : null
  const ratePreview = calculateRatePreview(draftMode, selectedRates, Number(draftOffset))
  const previewBelowMaximum = ratePreview != null
    && selectedRateMaximum != null
    && ratePreview + Number.EPSILON < selectedRateMaximum
  const selectedImportChannel = (channels.data ?? []).find(
    (channel) => String(channel.id) === selectedImportChannelID,
  )

  return (
    <section className="space-y-5">
      <OperationsPageHeader
        icon={UsersRound}
        title="目标账号"
        description="管理目标 Sub2API 中未由上游同步器托管的账号状态、余额告警和调度入口；同步器托管账号继续在上游动态同步中维护。"
        targets={target.targets}
        selectedTargetID={target.selectedTargetID}
        targetLoading={target.loading}
        onTargetChange={target.selectTarget}
        refreshing={loading}
        onRefresh={() => setReload((value) => value + 1)}
      />

      {target.error ? <OperationsError title="目标站点加载失败" message={target.error} /> : null}

      <OperationsMetricStrip
        items={[
          { label: "账号总数", value: decimal(summary.total, 0) },
          { label: "参与调度", value: decimal(summary.schedulable, 0), detail: "配置参与目标调度池" },
          { label: "错误账号", value: decimal(summary.errors, 0), detail: "需要检查凭据或上游状态" },
          { label: "临时不可用", value: decimal(summary.temporary_unavailable, 0), detail: "余额或额度耗尽后自动复检" },
        ]}
      />

      {rateWorkspace?.groups.length ? (
        <section className="border-y border-border py-4">
          <div className="mb-3 flex items-end justify-between gap-3">
            <div>
              <h2 className="text-sm font-semibold text-foreground">分组倍率同步</h2>
              <p className="mt-1 text-xs text-muted-foreground">一个目标分组可绑定多个采集分组，由同一 worker 聚合可用倍率。</p>
            </div>
            <span className="text-xs text-muted-foreground">{rateWorkspace.groups.length} 个分组</span>
          </div>
          <div className="divide-y divide-border border-y border-border">
            {rateWorkspace.groups.map((group) => {
              const bindings = group.bindings ?? []
              return (
                <div key={group.id} className="grid gap-2 py-2.5 sm:grid-cols-[minmax(0,1fr)_7rem_minmax(0,1fr)_2.25rem] sm:items-center">
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{group.name}</p>
                    <p className="text-[11px] text-muted-foreground">分组 #{group.id}</p>
                  </div>
                  <p className="font-mono text-sm">{decimal(group.current_rate, 6)}</p>
                  <p className="truncate text-xs text-muted-foreground">
                    {group.rule.enabled && bindings.length > 0
                      ? bindings.length === 1
                        ? `${bindings[0].channel_name} / ${bindings[0].group_name || bindings[0].group_id}`
                        : `${bindings.length} 个采集源 · ${bindings.slice(0, 2).map((binding) => binding.channel_name).join("、")}${bindings.length > 2 ? "…" : ""}`
                      : "未绑定采集源"}
                  </p>
                  <Button variant="ghost" size="icon" onClick={() => openRateEditor("group", group.id)} aria-label={`编辑 ${group.name} 分组倍率同步`}>
                    <Pencil className="size-4" />
                  </Button>
                </div>
              )
            })}
          </div>
        </section>
      ) : null}

      <div className="flex flex-col gap-2 border-b border-border pb-4 sm:flex-row sm:items-center">
        <div className="relative min-w-0 flex-1 sm:max-w-md">
          <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="搜索账号、平台或错误"
            className="pl-9"
          />
        </div>
        <Select value={schedule} onValueChange={(value) => setSchedule(value as TargetAccountScheduleFilter)}>
          <SelectTrigger className="w-full bg-background sm:w-40" aria-label="调度筛选">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部账号</SelectItem>
            <SelectItem value="enabled">参与调度</SelectItem>
            <SelectItem value="disabled">暂停调度</SelectItem>
            <SelectItem value="errors">错误账号</SelectItem>
            <SelectItem value="temporary_unavailable">临时不可用</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {error ? <OperationsError message={error} /> : null}
      {loading && !data ? <OperationsLoading rows={6} /> : null}
      {!loading && !error && data?.items.length === 0 ? (
        <OperationsEmpty title="没有匹配的目标账号" description="调整搜索或调度筛选后重试。" />
      ) : null}

      {data?.items.length ? (
        <div className="overflow-hidden rounded-md border border-border">
          <div className="hidden grid-cols-[minmax(0,1.5fr)_minmax(9rem,1fr)_9rem_9rem_11rem] gap-3 border-b border-border bg-muted/35 px-4 py-2 text-xs font-medium text-muted-foreground md:grid">
            <span>账号</span>
            <span>分组与健康</span>
            <span>成本倍率 / 上游同步</span>
            <span>余额</span>
            <span className="text-right">调度</span>
          </div>
          <div className="divide-y divide-border">
            {data.items.map((account) => {
              const pending = busyAccount === account.id
              const balanceLow =
                account.balance != null &&
                account.balance_threshold != null &&
                account.balance < account.balance_threshold
              return (
                <div
                  key={account.id}
                  className="grid gap-3 px-4 py-3 md:grid-cols-[minmax(0,1.5fr)_minmax(9rem,1fr)_9rem_9rem_11rem] md:items-center"
                >
                  <div className="min-w-0">
                    <div className="flex min-w-0 flex-wrap items-center gap-2">
                      <span className="truncate text-sm font-medium text-foreground">{account.name}</span>
                      <OperationsStatusBadge status={account.temporary_unavailable ? "temporary_unavailable" : account.status} />
                    </div>
                    <p className="mt-1 truncate text-xs text-muted-foreground">
                      #{account.id} · {[account.platform, account.type].filter(Boolean).join(" / ") || "未分类"}
                    </p>
                    {account.last_error ? (
                      <p className="mt-1 line-clamp-2 text-xs text-red-600 dark:text-red-400">
                        {account.last_error}
                      </p>
                    ) : null}
                    {account.temporary_unavailable ? (
                      <p className="mt-1 line-clamp-2 text-xs text-amber-700 dark:text-amber-300">
                        {account.temporary_unavailable_reason || "余额或额度耗尽"}
                        {account.temporary_unavailable_until ? ` · 复检 ${relativeTime(account.temporary_unavailable_until)}` : ""}
                      </p>
                    ) : null}
                  </div>

                  <div className="min-w-0">
                    <div className="flex flex-wrap gap-1">
                      {(account.group_names ?? []).slice(0, 3).map((group) => (
                        <Badge key={group} variant="outline" className="max-w-32 truncate font-normal">
                          {group}
                        </Badge>
                      ))}
                    </div>
                    <p className="mt-1 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                      {account.health_state ? (
                        <HealthStateBadge state={account.health_state} weight={account.health_weight} />
                      ) : null}
                      <span>
                        健康 {account.health_score == null ? "—" : `${decimal(account.health_score, 0)}%`} · 并发 {account.concurrency}
                      </span>
                    </p>
                  </div>

                  <div>
                    <p className="text-xs text-muted-foreground md:hidden">成本倍率</p>
                    <p className="font-mono text-sm text-foreground">{decimal(account.rate_multiplier, 6)}</p>
                    <p className="text-[11px] text-muted-foreground">优先级 {account.priority}</p>
                    {(() => {
                      const policy = rateWorkspace?.accounts.find((item) => item.id === account.id)
                      const binding = policy?.bindings?.[0]
                      return (
                        <div className="mt-1 flex min-w-0 items-center gap-1 text-[11px] text-muted-foreground">
                          <Link2 className="size-3 shrink-0" />
                          <span className="truncate">
                            {policy?.rule.enabled && binding ? `${binding.channel_name} / ${binding.group_name || binding.group_id}` : "未绑定采集源"}
                          </span>
                        </div>
                      )
                    })()}
                  </div>

                  <div>
                    <p className="text-xs text-muted-foreground md:hidden">余额</p>
                    <div className={cn("flex items-center gap-1.5 text-sm", balanceLow && "text-amber-700 dark:text-amber-300")}>
                      {balanceLow ? <AlertTriangle className="size-3.5" /> : <WalletCards className="size-3.5 text-muted-foreground" />}
                      <span>{money(account.balance)}</span>
                    </div>
                    <p className="text-[11px] text-muted-foreground">更新 {relativeTime(account.updated_at)}</p>
                  </div>

                  <div className="flex items-center justify-between gap-2 md:justify-end">
                    <Button
                      variant="ghost"
                      size="icon"
                      disabled={!rateWorkspace || rateLoading || pending}
                      onClick={() => openRateEditor("account", account.id)}
                      title="编辑倍率同步"
                      aria-label="编辑倍率同步"
                    >
                      <Pencil className="size-4" />
                    </Button>
                    {account.last_error ? (
                      <Button
                        variant="ghost"
                        size="icon"
                        disabled={pending}
                        onClick={() => void runAction(account.id, "clear_error")}
                        title="清除错误"
                        aria-label="清除错误"
                      >
                        <Eraser className="size-4" />
                      </Button>
                    ) : account.temporary_unavailable ? (
                      <AlertTriangle className="size-4 text-amber-600" aria-label="临时不可用" />
                    ) : (
                      <CircleCheck className="size-4 text-emerald-600" aria-label="状态正常" />
                    )}
                    <span className="text-xs text-muted-foreground">{account.temporary_unavailable ? "临时不可用" : account.schedulable ? "调度中" : "已暂停"}</span>
                    <Switch
                      checked={account.schedulable}
                      disabled={pending}
                      onCheckedChange={(checked) => void runAction(account.id, checked ? "enable" : "disable")}
                      aria-label={`${account.name} 调度状态`}
                    />
                    <Button
                      variant="ghost"
                      size="icon"
                      disabled={pending}
                      onClick={() => void deleteAccount(account.id, account.name)}
                      title="删除账号"
                      aria-label="删除账号"
                    >
                      <Trash2 className="size-4 text-destructive" />
                    </Button>
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      ) : null}

      {rateError ? <OperationsError message={rateError} /> : null}
      {rateLoading && !rateWorkspace ? (
        <p className="flex items-center gap-2 text-xs text-muted-foreground"><Loader2 className="size-3.5 animate-spin" />正在加载倍率同步规则</p>
      ) : null}

      {(data?.pages ?? 1) > 1 ? (
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>
            第 {data?.page ?? page} / {data?.pages ?? 1} 页 · 共 {data?.total ?? 0} 个账号
          </span>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" disabled={page <= 1 || loading} onClick={() => setPage((value) => value - 1)}>
              上一页
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={page >= (data?.pages ?? 1) || loading}
              onClick={() => setPage((value) => value + 1)}
            >
              下一页
            </Button>
          </div>
        </div>
      ) : null}

      {target.selectedTargetID != null ? (
        <div className="flex flex-col gap-3 border-y border-border py-4 sm:flex-row sm:items-end sm:justify-between">
          <div className="min-w-0">
            <p className="text-sm font-medium text-foreground">添加 Sub2API 账号</p>
            <p className="mt-1 text-xs text-muted-foreground">从新面板采集源的 API Key 导入，并在导入时绑定或新建目标分组。</p>
          </div>
          <div className="flex w-full flex-col gap-2 sm:w-auto sm:flex-row">
            <Select value={selectedImportChannelID} onValueChange={setSelectedImportChannelID}>
              <SelectTrigger className="w-full bg-background sm:w-56" aria-label="账号来源站点">
                <SelectValue placeholder={channels.loading ? "加载源站" : "选择源站"} />
              </SelectTrigger>
              <SelectContent>
                {(channels.data ?? []).map((channel) => (
                  <SelectItem key={channel.id} value={String(channel.id)}>
                    {channel.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              disabled={!selectedImportChannel || channels.loading}
              onClick={() => setAPIKeyChannel(selectedImportChannel ?? null)}
            >
              <Plus className="size-4" />
              添加账号
            </Button>
          </div>
        </div>
      ) : null}

      <Dialog open={rateEditorOpen} onOpenChange={(open) => open ? setRateEditorOpen(true) : closeRateEditor()}>
        <DialogContent className="max-h-[calc(100vh-2rem)] min-w-0 overflow-x-hidden overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>编辑{rateEditor?.target_type === "group" ? "分组" : "账号"}倍率同步</DialogTitle>
            <DialogDescription>
              {rateEditor
                ? `${rateEditor.name} · ${rateEditor.target_type === "group" ? "可绑定多个采集分组" : "仅保存这一个账号的绑定关系"}`
                : "选择采集源"}
            </DialogDescription>
          </DialogHeader>
          <div className="min-w-0 space-y-4 py-2">
            <div className="flex items-center justify-between rounded-md border border-border px-3 py-2">
              <div>
                <Label htmlFor="rate-enabled">启用上游动态同步</Label>
                <p className="text-xs text-muted-foreground">worker 下一轮采集后更新该{rateEditor?.target_type === "group" ? "分组" : "账号"}倍率</p>
              </div>
              <Switch id="rate-enabled" checked={draftEnabled} onCheckedChange={setDraftEnabled} />
            </div>

            {rateEditor?.target_type === "group" ? (
              <div className="min-w-0 space-y-2">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <Label>采集源</Label>
                  <div className="flex items-center gap-1">
                    <span className="mr-1 text-xs text-muted-foreground">已选 {draftSourceKeys.length}</span>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="h-7 px-2 text-xs"
                      disabled={!draftEnabled || draftMode === "locked" || filteredRateSources.length === 0}
                      onClick={() => setDraftSourceKeys((current) => Array.from(new Set([
                        ...current,
                        ...sortedFilteredRateSources.map(sourceKey),
                      ])))}
                    >
                      全选当前
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="h-7 px-2 text-xs"
                      disabled={!draftEnabled || draftMode === "locked" || draftSourceKeys.length === 0}
                      onClick={() => setDraftSourceKeys([])}
                    >
                      清空
                    </Button>
                  </div>
                </div>
                <Select
                  value={draftSourceChannelID}
                  onValueChange={setDraftSourceChannelID}
                  disabled={!draftEnabled || draftMode === "locked"}
                >
                  <SelectTrigger className="w-full min-w-0"><SelectValue className="min-w-0 flex-1 truncate" /></SelectTrigger>
                  <SelectContent className="max-w-[calc(100vw-2rem)]">
                    <SelectItem value="all">全部源站</SelectItem>
                    {rateSourceChannels.map((source) => (
                      <SelectItem key={source.channel_id} value={String(source.channel_id)}>
                        {source.channel_name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <div className="relative">
                  <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    value={rateSourceSearch}
                    onChange={(event) => setRateSourceSearch(event.target.value)}
                    placeholder="搜索采集源、分组或 ID"
                    className="pl-9"
                    disabled={!draftEnabled || draftMode === "locked"}
                  />
                </div>
                <ScrollArea className="h-52 rounded-md border border-border">
                  <div className="divide-y divide-border">
                    {sortedFilteredRateSources.map((source) => {
                      const monitorExcluded = selectedRateExclusionKeys.has(sourceKey(source))
                      const sourceUsable = isUsableRateSource(source) && !monitorExcluded
                      return (
                        <label
                          key={sourceKey(source)}
                          className={cn(
                            "grid min-h-14 cursor-pointer grid-cols-[1rem_minmax(0,1fr)_auto] items-center gap-3 px-3 py-2",
                            (!draftEnabled || draftMode === "locked") && "cursor-not-allowed opacity-60",
                          )}
                        >
                          <Checkbox
                            checked={draftSourceKeys.includes(sourceKey(source))}
                            disabled={!draftEnabled || draftMode === "locked"}
                            onCheckedChange={(checked) => toggleDraftSource(sourceKey(source), checked === true)}
                          />
                          <span className="min-w-0">
                            <span className="block truncate text-sm font-medium">{source.group_name || source.group_id}</span>
                            <span className="block truncate text-xs text-muted-foreground">{source.channel_name}</span>
                          </span>
                          <span className="text-right">
                            <span className="block font-mono text-sm">{decimal(source.rate, 6)}</span>
                            <span className="flex items-center justify-end gap-1 text-[11px] text-muted-foreground">
                              <span className={cn("size-1.5 rounded-full", sourceUsable ? "bg-emerald-500" : "bg-amber-500")} />
                              {sourceUsable ? "可用" : monitorExcluded ? "监控排除" : source.last_status}
                            </span>
                          </span>
                        </label>
                      )
                    })}
                    {sortedFilteredRateSources.length === 0 ? (
                      <p className="px-3 py-8 text-center text-xs text-muted-foreground">当前源站没有可选采集分组</p>
                    ) : null}
                  </div>
                </ScrollArea>
              </div>
            ) : (
              <div className="grid min-w-0 gap-3 sm:grid-cols-2">
                <div className="min-w-0 space-y-2">
                  <Label>筛选源站</Label>
                  <Select
                    value={draftSourceChannelID}
                    onValueChange={(value) => {
                      setDraftSourceChannelID(value)
                      const selected = rateWorkspace?.sources.find((source) => sourceKey(source) === draftSourceKeys[0])
                      if (value !== "all" && selected && String(selected.channel_id) !== value) {
                        setDraftSourceKeys([])
                      }
                    }}
                    disabled={!draftEnabled || draftMode === "locked"}
                  >
                    <SelectTrigger className="w-full min-w-0"><SelectValue className="min-w-0 flex-1 truncate" /></SelectTrigger>
                    <SelectContent className="max-w-[calc(100vw-2rem)]">
                      <SelectItem value="all">全部源站</SelectItem>
                      {rateSourceChannels.map((source) => (
                        <SelectItem key={source.channel_id} value={String(source.channel_id)}>{source.channel_name}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="min-w-0 space-y-2">
                  <Label>一对一采集源</Label>
                  <Select
                    value={draftSourceKeys[0] ?? "none"}
                    onValueChange={(value) => setDraftSourceKeys(value === "none" ? [] : [value])}
                    disabled={!draftEnabled || draftMode === "locked"}
                  >
                    <SelectTrigger className="w-full min-w-0"><SelectValue className="min-w-0 flex-1 truncate" placeholder="选择采集源" /></SelectTrigger>
                    <SelectContent className="max-w-[calc(100vw-2rem)]">
                      <SelectItem value="none">不绑定</SelectItem>
                      {sortedFilteredRateSources.map((source) => (
                        <SelectItem key={sourceKey(source)} value={sourceKey(source)}>
                          {source.channel_name} / {source.group_name || source.group_id} · {decimal(source.rate, 6)} · {source.last_status}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
            )}

            <div className="grid min-w-0 gap-3 sm:grid-cols-2">
              <div className="min-w-0 space-y-2">
                <Label>计算模式</Label>
                <Select value={draftMode} onValueChange={(value) => setDraftMode(value as RatePolicyMode)} disabled={!draftEnabled}>
                  <SelectTrigger className="w-full min-w-0"><SelectValue className="min-w-0 flex-1 truncate" /></SelectTrigger>
                  <SelectContent>
                    {rateEditor?.target_type === "group" ? <SelectItem value="max">最高可用源（推荐）</SelectItem> : null}
                    {rateEditor?.target_type === "group" ? <SelectItem value="locked">手动锁定倍率</SelectItem> : null}
                    <SelectItem value="first">首个可用源（按源站与分组 ID）</SelectItem>
                    <SelectItem value="average">可用源平均</SelectItem>
                    <SelectItem value="min">最低可用源</SelectItem>
                    {rateEditor?.target_type !== "group" ? <SelectItem value="max">最高可用源</SelectItem> : null}
                  </SelectContent>
                </Select>
              </div>
              <div className="min-w-0 space-y-2">
                <Label htmlFor="rate-offset">
                  {draftMode === "locked" ? "固定倍率" : rateEditor?.target_type === "group" ? "加价倍率" : "偏移量"}
                </Label>
                <Input id="rate-offset" type="number" step="0.0001" min={draftMode === "locked" ? "0.0001" : undefined} value={draftOffset} onChange={(event) => setDraftOffset(event.target.value)} disabled={!draftEnabled} />
              </div>
            </div>

            {rateEditor?.target_type === "group" && draftMode !== "locked" ? (
              <div className="grid grid-cols-2 gap-x-4 gap-y-3 border-y border-border py-3 sm:grid-cols-4">
                <div>
                  <p className="text-[11px] text-muted-foreground">可用源</p>
                  <p className="font-mono text-sm">{usableSelectedRateSources.length} / {selectedRateSources.length}</p>
                </div>
                <div>
                  <p className="text-[11px] text-muted-foreground">最低 / 平均</p>
                  <p className="font-mono text-sm">{decimal(selectedRateMinimum, 6)} / {decimal(selectedRateAverage, 6)}</p>
                </div>
                <div>
                  <p className="text-[11px] text-muted-foreground">最高成本</p>
                  <p className="font-mono text-sm">{decimal(selectedRateMaximum, 6)}</p>
                </div>
                <div>
                  <p className="text-[11px] text-muted-foreground">预计倍率</p>
                  <p className={cn("font-mono text-sm font-semibold", previewBelowMaximum && "text-amber-700 dark:text-amber-300")}>{decimal(ratePreview, 6)}</p>
                </div>
              </div>
            ) : null}
            {selectedRateSources.length > usableSelectedRateSources.length && draftMode !== "locked" ? (
              <p className="text-xs text-amber-700 dark:text-amber-300">异常或过期采集源不会参与本轮定价。</p>
            ) : null}
            {previewBelowMaximum ? (
              <p className="text-xs text-amber-700 dark:text-amber-300">预计倍率低于最高上游成本，可能出现成本倒挂。</p>
            ) : null}
            {draftMode === "locked" ? (
              <p className="text-xs text-muted-foreground">固定倍率由 worker 持续保持，不读取采集源，也不随上游倍率变化。</p>
            ) : null}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={closeRateEditor}>取消</Button>
            <Button type="button" onClick={() => void saveRateEditor()} disabled={rateSaving}>
              {rateSaving ? <Loader2 className="size-4 animate-spin" /> : null}
              保存并同步
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <ChannelAPIKeysDialog
        open={apiKeyChannel != null}
        onOpenChange={(open) => {
          if (!open) {
            setAPIKeyChannel(null)
            setReload((value) => value + 1)
          }
        }}
        channel={apiKeyChannel}
        targetID={target.selectedTargetID}
      />
      {confirmDialog}
    </section>
  )
}

function HealthStateBadge({ state, weight }: { state: string; weight?: number }) {
  const map: Record<string, { label: string; variant: "default" | "secondary" | "destructive" | "outline" }> = {
    healthy: { label: "正常", variant: "default" },
    degraded: { label: "降权", variant: "secondary" },
    suspended: { label: "熔断", variant: "destructive" },
    observing: { label: "观察", variant: "secondary" },
    recovering: { label: "恢复", variant: "secondary" },
    disabled: { label: "停用", variant: "outline" },
  }
  const item = map[state] ?? { label: state, variant: "outline" as const }
  return (
    <Badge variant={item.variant} className="font-normal">
      {item.label}
      {typeof weight === "number" ? ` ${weight}%` : ""}
    </Badge>
  )
}
