"use client"

import { useEffect, useState, type FormEvent } from "react"
import { ArrowRightLeft, Copy, Loader2, Pencil, Plus, RefreshCw, Search, Trash2 } from "lucide-react"
import { toast } from "sonner"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { Checkbox } from "@/components/ui/checkbox"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { apiFetch } from "@/lib/api"
import { copyText as copyToClipboard } from "@/lib/clipboard"
import { channelTypeLabel, dateTime, decimal, formatRatio } from "@/lib/format"
import {
  applyTargetRatePolicies,
  createTargetGroup,
  getTargetRatePolicies,
  importAPIKeyToTarget,
  listTargetGroups,
  saveTargetRatePolicy,
  type RatePolicyTarget,
  type RatePolicyTargetType,
  type TargetGroup,
} from "@/lib/operations-api"
import type {
  Channel,
  ChannelAPIKey,
  ChannelAPIKeyGroup,
  ChannelAPIKeyPage,
  ChannelAPIKeyReveal,
  ChannelAPIKeyStatus,
} from "@/lib/api-types"
import { cn } from "@/lib/utils"

export interface ChannelAPIKeyTargetGroup {
  id: number | null
  name: string
}

export type ChannelAPIKeyInitialAction = {
  type: "create" | "migrate"
  targetGroup: ChannelAPIKeyTargetGroup
}

interface ChannelAPIKeysDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  channel: Channel | null
  initialAction?: ChannelAPIKeyInitialAction
  targetID?: number | null
}

type Mode = "list" | "create" | "edit" | "migrate"

interface KeyForm {
  name: string
  custom_key: string
  status: "active" | "disabled"
  group: string
  group_id: string
  remain_quota: string
  quota: string
  unlimited_quota: boolean
  expired_time: string
  expires_in_days: string
  expires_at: string
  model_limits_enabled: boolean
  model_limits: string
  allow_ips: string
  ip_whitelist: string
  ip_blacklist: string
  cross_group_retry: boolean
  rate_limit_5h: string
  rate_limit_1d: string
  rate_limit_7d: string
  reset_quota: boolean
  reset_rate_limit_usage: boolean
}

const PAGE_SIZE = 10

const emptyForm: KeyForm = {
  name: "",
  custom_key: "",
  status: "active",
  group: "",
  group_id: "",
  remain_quota: "",
  quota: "",
  unlimited_quota: false,
  expired_time: "-1",
  expires_in_days: "",
  expires_at: "",
  model_limits_enabled: false,
  model_limits: "",
  allow_ips: "",
  ip_whitelist: "",
  ip_blacklist: "",
  cross_group_retry: false,
  rate_limit_5h: "",
  rate_limit_1d: "",
  rate_limit_7d: "",
  reset_quota: false,
  reset_rate_limit_usage: false,
}

function formFromKey(key: ChannelAPIKey, channel: Channel): KeyForm {
  const base = { ...emptyForm }
  base.name = key.name ?? ""
  base.status = key.status === "disabled" ? "disabled" : "active"
  if (channel.type === "newapi") {
    base.group = key.group ?? ""
    base.remain_quota = key.quota ? String(key.quota) : "0"
    base.unlimited_quota = !!key.unlimited_quota
    base.expired_time = key.expired_time != null ? String(key.expired_time) : "-1"
    base.model_limits_enabled = !!key.model_limits_enabled
    base.model_limits = key.model_limits ?? ""
    base.allow_ips = key.allow_ips ?? ""
    base.cross_group_retry = !!key.cross_group_retry
  } else {
    base.group_id = key.group_id != null ? String(key.group_id) : ""
    base.quota = key.quota ? String(key.quota) : "0"
    base.expires_at = key.expires_at ? toDateTimeLocal(key.expires_at) : ""
    base.ip_whitelist = (key.ip_whitelist ?? []).join("\n")
    base.ip_blacklist = (key.ip_blacklist ?? []).join("\n")
    base.rate_limit_5h = key.rate_limit_5h ? String(key.rate_limit_5h) : "0"
    base.rate_limit_1d = key.rate_limit_1d ? String(key.rate_limit_1d) : "0"
    base.rate_limit_7d = key.rate_limit_7d ? String(key.rate_limit_7d) : "0"
  }
  return base
}

function groupDisplayName(key: ChannelAPIKey) {
  return key.group_name || key.group || (key.group_id != null ? `#${key.group_id}` : "—")
}

function keyMatchesGroup(key: ChannelAPIKey, channel: Channel, group: ChannelAPIKeyTargetGroup) {
  if (channel.type === "newapi") return (key.group ?? "").trim() === group.name.trim()
  return key.group_id != null && group.id != null && key.group_id === group.id
}

function sourceGroupLabel(key: ChannelAPIKey, channel: Channel, groups: ChannelAPIKeyGroup[]) {
  if (channel.type === "newapi") {
    const name = (key.group ?? "").trim()
    if (!name) return "未分组"
    return groups.some((group) => group.name === name) ? name : `已失效：${name}`
  }

  if (key.group_id == null) return "未分组"
  const liveGroup = groups.find((group) => group.id === key.group_id)
  if (liveGroup) return liveGroup.name
  return `已失效：${key.group_name?.trim() || `#${key.group_id}`}`
}

function statusLabel(status: string) {
  switch (status) {
    case "active":
      return "启用"
    case "disabled":
      return "停用"
    case "expired":
      return "已过期"
    case "quota_exhausted":
      return "额度耗尽"
    default:
      return status || "未知"
  }
}

function statusClass(status: string) {
  switch (status) {
    case "active":
      return "bg-success/10 text-success border-success/20"
    case "disabled":
      return "bg-muted text-muted-foreground border-border"
    case "expired":
    case "quota_exhausted":
      return "bg-warning/10 text-warning border-warning/20"
    default:
      return "bg-muted text-muted-foreground border-border"
  }
}

function splitLines(value: string): string[] {
  return value
    .split(/\r?\n|,/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function optionalInt(value: string): number | undefined {
  const trimmed = value.trim()
  if (!trimmed) return undefined
  const n = Number(trimmed)
  if (!Number.isFinite(n)) throw new Error("请输入有效整数")
  return Math.trunc(n)
}

function optionalFloat(value: string): number | undefined {
  const trimmed = value.trim()
  if (!trimmed) return undefined
  const n = Number(trimmed)
  if (!Number.isFinite(n)) throw new Error("请输入有效数字")
  return n
}

function toDateTimeLocal(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ""
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function localDateTimeToISO(value: string): string | undefined {
  if (!value.trim()) return undefined
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) throw new Error("过期时间格式无效")
  return d.toISOString()
}

function maskKey(key: string) {
  if (!key) return "—"
  if (key.length <= 12) return key
  return `${key.slice(0, 8)}…${key.slice(-6)}`
}

async function copyText(text: string, label = "已复制") {
  await copyToClipboard(text)
  toast.success(label)
}

function inferAccountPlatform(value: string) {
  const text = value.toLowerCase()
  if (text.includes("gemini") || text.includes("antigravity")) return "gemini"
  if (text.includes("claude") || text.includes("anthropic")) return "anthropic"
  if (text.includes("kiro")) return "kiro"
  if (text.includes("grok")) return "grok"
  return "openai"
}

function normalizeAccountPlatform(value: string | null | undefined) {
  const platform = value?.trim().toLowerCase() ?? ""
  switch (platform) {
    case "claude":
      return "anthropic"
    case "anthropic":
    case "openai":
    case "gemini":
    case "antigravity":
    case "kiro":
    case "grok":
    case "composite":
      return platform
    default:
      return ""
  }
}

function platformLabel(value: string | null | undefined) {
  switch (normalizeAccountPlatform(value)) {
    case "anthropic":
      return "Claude"
    case "openai":
      return "OpenAI"
    case "gemini":
      return "Gemini"
    case "antigravity":
      return "Antigravity"
    case "kiro":
      return "Kiro"
    case "grok":
      return "Grok"
    case "composite":
      return "Composite"
    default:
      return "平台未标记"
  }
}

export function ChannelAPIKeysDialog({
  open,
  onOpenChange,
  channel,
  initialAction,
  targetID,
}: ChannelAPIKeysDialogProps) {
  const { confirm, dialog: confirmDialog } = useConfirm()
  const [mode, setMode] = useState<Mode>("list")
  const [editing, setEditing] = useState<ChannelAPIKey | null>(null)
  const [form, setForm] = useState<KeyForm>(emptyForm)
  const [page, setPage] = useState(1)
  const [reloadTick, setReloadTick] = useState(0)
  const [search, setSearch] = useState("")
  const [status, setStatus] = useState<ChannelAPIKeyStatus | "all">("all")
  const [data, setData] = useState<ChannelAPIKeyPage | null>(null)
  const [groups, setGroups] = useState<ChannelAPIKeyGroup[]>([])
  const [groupsLoading, setGroupsLoading] = useState(false)
  const [groupsError, setGroupsError] = useState<string | null>(null)
  const [groupsReloadTick, setGroupsReloadTick] = useState(0)
  const [loading, setLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [listError, setListError] = useState<string | null>(null)
  const [revealingID, setRevealingID] = useState<number | null>(null)
  const [revealedKeys, setRevealedKeys] = useState<Record<number, string>>({})
  const [selectedKey, setSelectedKey] = useState<ChannelAPIKey | null>(null)
  const [importingID, setImportingID] = useState<number | null>(null)
  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [targetGroups, setTargetGroups] = useState<TargetGroup[]>([])
  const [targetGroupsLoading, setTargetGroupsLoading] = useState(false)
  const [targetGroupsError, setTargetGroupsError] = useState<string | null>(null)
  const [selectedTargetGroupIDs, setSelectedTargetGroupIDs] = useState<string[]>([])
  const [newTargetGroupName, setNewTargetGroupName] = useState("")
  const [newTargetGroupPlatform, setNewTargetGroupPlatform] = useState("openai")
  const [newTargetGroupRate, setNewTargetGroupRate] = useState("1")
  const [newTargetGroupDescription, setNewTargetGroupDescription] = useState("")
  const [creatingTargetGroup, setCreatingTargetGroup] = useState(false)

  const items = data?.items ?? []
  const totalPages = Math.max(1, data?.pages ?? 1)
  const isNewAPI = channel?.type === "newapi"
  const groupByName = new Map(groups.map((g) => [g.name, g]))
  const groupByID = new Map(groups.filter((g) => g.id != null).map((g) => [String(g.id), g]))
  const actionType = initialAction?.type
  const targetGroupID = initialAction?.targetGroup.id ?? null
  const targetGroupName = initialAction?.targetGroup.name ?? ""
  const targetGroup = initialAction?.targetGroup
  const liveTargetGroup = channel?.type === "newapi"
    ? groupByName.get(targetGroupName)
    : targetGroupID == null
      ? undefined
      : groupByID.get(String(targetGroupID))
  const targetValidationRequired = initialAction != null && (mode === "create" || mode === "migrate")
  const targetValidationError = targetValidationRequired
    ? groupsError || (!groupsLoading && !liveTargetGroup
      ? "目标分组已失效，请先同步渠道分组。"
      : null)
    : null
  const visibleGroupsError = targetValidationError || (mode === "create" || mode === "edit" ? groupsError : null)

  useEffect(() => {
    if (!open) return
    setMode(actionType ?? "list")
    setEditing(null)
    setForm(() => {
      const next = { ...emptyForm }
      if (actionType === "create") {
        if (channel?.type === "newapi") next.group = targetGroupName
        else if (targetGroupID != null) next.group_id = String(targetGroupID)
      }
      return next
    })
    setPage(1)
    setSearch("")
    setStatus("all")
    setError(null)
    setListError(null)
    setGroups([])
    setGroupsError(null)
    setGroupsLoading(channel != null)
    setRevealingID(null)
    setRevealedKeys({})
    setSelectedKey(null)
  }, [open, channel?.id, channel?.type, actionType, targetGroupID, targetGroupName])

  useEffect(() => {
    if (!open || !channel) return
    let cancelled = false
    setGroupsLoading(true)
    setGroupsError(null)
    apiFetch<ChannelAPIKeyGroup[]>(`/channels/${channel.id}/api-keys/groups`)
      .then((res) => {
        if (cancelled) return
        setGroups(Array.isArray(res) ? res : [])
      })
      .catch((e) => {
        if (cancelled) return
        const err = e as Error
        setGroupsError(err.message || "加载分组失败")
        setGroups([])
      })
      .finally(() => {
        if (!cancelled) setGroupsLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [open, channel, groupsReloadTick])

  useEffect(() => {
    if (!importDialogOpen || targetID == null) return
    let cancelled = false
    setTargetGroupsLoading(true)
    setTargetGroupsError(null)
    listTargetGroups(targetID)
      .then((items) => {
        if (cancelled) return
        const normalized = Array.isArray(items) ? items : []
        setTargetGroups(normalized)
        setSelectedTargetGroupIDs((current) => (current.length > 0 ? current : normalized[0] ? [String(normalized[0].id)] : []))
      })
      .catch((reason: unknown) => {
        if (!cancelled) setTargetGroupsError(reason instanceof Error ? reason.message : "加载目标分组失败")
      })
      .finally(() => {
        if (!cancelled) setTargetGroupsLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [importDialogOpen, targetID])

  useEffect(() => {
    if (!open || !channel || (mode !== "list" && mode !== "migrate")) return
    let cancelled = false
    const params = new URLSearchParams({
      page: String(page),
      page_size: String(PAGE_SIZE),
    })
    if (search.trim()) params.set("search", search.trim())
    if (status !== "all") params.set("status", status)
    setLoading(true)
    setListError(null)
    apiFetch<ChannelAPIKeyPage>(`/channels/${channel.id}/api-keys?${params.toString()}`)
      .then((res) => {
        if (cancelled) return
        setData({
          items: Array.isArray(res?.items) ? res.items : [],
          total: res?.total ?? 0,
          page: res?.page ?? page,
          page_size: res?.page_size ?? PAGE_SIZE,
          pages: res?.pages ?? 1,
        })
      })
      .catch((e) => {
        if (cancelled) return
        const err = e as Error
        setListError(err.message || "加载密钥失败")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [open, channel, mode, page, search, status, reloadTick])

  function reload() {
    setReloadTick((tick) => tick + 1)
  }

  function retryGroups() {
    setGroupsReloadTick((tick) => tick + 1)
  }

  function openCreate() {
    setEditing(null)
    setForm(() => {
      const next = { ...emptyForm }
      if (actionType === "create" && targetGroup) {
        if (channel?.type === "newapi") next.group = targetGroup.name
        else if (targetGroup.id != null) next.group_id = String(targetGroup.id)
      } else {
        const first = groups[0]
        if (first) {
          if (channel?.type === "newapi") next.group = first.name
          else if (first.id != null) next.group_id = String(first.id)
        }
      }
      return next
    })
    setError(null)
    setMode("create")
  }

  function openEdit(key: ChannelAPIKey) {
    if (!channel) return
    setEditing(key)
    setForm(formFromKey(key, channel))
    setError(null)
    setMode("edit")
  }

  function buildPayload() {
    if (!channel) return {}
    if (!form.name.trim()) throw new Error("密钥名称不能为空")
    if (channel.type === "newapi") {
      const payload: Record<string, unknown> = {
        name: form.name.trim(),
        group: form.group.trim(),
        remain_quota: optionalInt(form.remain_quota) ?? 0,
        unlimited_quota: form.unlimited_quota,
        expired_time: optionalInt(form.expired_time) ?? -1,
        model_limits_enabled: form.model_limits_enabled,
        model_limits: form.model_limits.trim(),
        allow_ips: form.allow_ips.trim(),
        cross_group_retry: form.cross_group_retry,
      }
      if (mode === "create" && form.custom_key.trim()) payload.custom_key = form.custom_key.trim()
      if (mode === "edit") payload.status = form.status
      return payload
    }
    const payload: Record<string, unknown> = {
      name: form.name.trim(),
      group_id: optionalInt(form.group_id),
      quota: optionalFloat(form.quota) ?? 0,
      ip_whitelist: splitLines(form.ip_whitelist),
      ip_blacklist: splitLines(form.ip_blacklist),
      rate_limit_5h: optionalFloat(form.rate_limit_5h) ?? 0,
      rate_limit_1d: optionalFloat(form.rate_limit_1d) ?? 0,
      rate_limit_7d: optionalFloat(form.rate_limit_7d) ?? 0,
    }
    if (mode === "create") {
      if (form.custom_key.trim()) payload.custom_key = form.custom_key.trim()
      const days = optionalInt(form.expires_in_days)
      if (days != null) payload.expires_in_days = days
    } else {
      payload.status = form.status
      payload.expires_at = localDateTimeToISO(form.expires_at) ?? ""
      payload.reset_quota = form.reset_quota
      payload.reset_rate_limit_usage = form.reset_rate_limit_usage
    }
    return payload
  }

  async function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!channel) return
    if (mode === "create" && initialAction && !liveTargetGroup) {
      setError(targetValidationError || "目标分组尚未通过校验。")
      return
    }
    setSubmitting(true)
    setError(null)
    try {
      const payload = buildPayload()
      if (mode === "create") {
        const created = await apiFetch<ChannelAPIKey>(`/channels/${channel.id}/api-keys`, {
          method: "POST",
          body: JSON.stringify(payload),
        })
        if (created?.key) {
          if (created.id) {
            setRevealedKeys((prev) => ({ ...prev, [created.id]: created.key }))
          }
          void copyText(created.key, "密钥已创建并复制").catch((err: Error) => {
            toast.success("密钥已创建")
            toast.error(err?.message || "自动复制失败，请手动复制")
          })
        }
      } else if (editing) {
        await apiFetch<ChannelAPIKey>(`/channels/${channel.id}/api-keys/${editing.id}`, {
          method: "PUT",
          body: JSON.stringify(payload),
        })
        toast.success("密钥已更新")
      }
      setMode("list")
      setEditing(null)
      setForm(emptyForm)
      setPage(1)
      reload()
    } catch (e) {
      const err = e as Error
      setError(err.message || "保存密钥失败")
    } finally {
      setSubmitting(false)
    }
  }

  async function migrateSelectedKey() {
    if (!channel || !selectedKey || !targetGroup || !liveTargetGroup) {
      setError(targetValidationError || "请选择要迁移的 API Key。")
      return
    }

    const sourceGroup = sourceGroupLabel(selectedKey, channel, groups)
    const ok = await confirm({
      title: `迁移 API Key「${selectedKey.name || selectedKey.id}」？`,
      description: `${sourceGroup} → ${liveTargetGroup.name}`,
      confirmLabel: "确认迁移",
    })
    if (!ok) return

    setSubmitting(true)
    setError(null)
    try {
      const payload = channel.type === "newapi"
        ? { group: liveTargetGroup.name }
        : { group_id: targetGroupID }
      await apiFetch<ChannelAPIKey>(`/channels/${channel.id}/api-keys/${selectedKey.id}`, {
        method: "PUT",
        body: JSON.stringify(payload),
      })
      toast.success(`API Key「${selectedKey.name || selectedKey.id}」已从「${sourceGroup}」迁移到「${liveTargetGroup.name}」`)
      onOpenChange(false)
    } catch (e) {
      const err = e as Error
      setError(err.message || "迁移 API Key 失败")
    } finally {
      setSubmitting(false)
    }
  }

  async function revealKey(key: ChannelAPIKey) {
    if (!channel) throw new Error("渠道不存在")
    const cached = revealedKeys[key.id]
    if (cached) return cached
    setRevealingID(key.id)
    try {
      const res = await apiFetch<ChannelAPIKeyReveal>(`/channels/${channel.id}/api-keys/${key.id}/reveal`, {
        method: "POST",
      })
      if (!res?.key) throw new Error("上游未返回完整密钥")
      setRevealedKeys((prev) => ({ ...prev, [key.id]: res.key }))
      return res.key
    } finally {
      setRevealingID((current) => (current === key.id ? null : current))
    }
  }

  async function revealAndShow(key: ChannelAPIKey) {
    try {
      await revealKey(key)
    } catch (e) {
      const err = e as Error
      toast.error(err.message || "获取完整密钥失败")
    }
  }

  async function revealAndCopy(key: ChannelAPIKey) {
    try {
      const fullKey = await revealKey(key)
      await copyText(fullKey)
    } catch (e) {
      const err = e as Error
      toast.error(err.message || "获取完整密钥失败")
    }
  }

  function openImportDialog(key: ChannelAPIKey) {
    setSelectedKey(key)
    setSelectedTargetGroupIDs([])
    setTargetGroupsError(null)
    setNewTargetGroupName("")
    setNewTargetGroupPlatform(inferAccountPlatform(`${channel?.name ?? ""} ${key.name} ${key.group_name || key.group || ""}`))
    setNewTargetGroupRate("1")
    setNewTargetGroupDescription("")
    setImportDialogOpen(true)
  }

  async function createImportTargetGroup() {
    if (targetID == null) return
    const name = newTargetGroupName.trim()
    const rate = Number(newTargetGroupRate)
    if (!name || !Number.isFinite(rate) || rate <= 0) {
      toast.error("请填写有效的分组名称和正倍率")
      return
    }
    setCreatingTargetGroup(true)
    try {
      const created = await createTargetGroup(targetID, {
        name,
        description: newTargetGroupDescription.trim(),
        platform: newTargetGroupPlatform,
        rate_multiplier: rate,
      })
      setTargetGroups((current) => [...current, created])
      setSelectedTargetGroupIDs((current) => [...current, String(created.id)])
      setNewTargetGroupName("")
      setNewTargetGroupDescription("")
      toast.success(`目标分组 ${created.name} 已创建`)
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "创建目标分组失败")
    } finally {
      setCreatingTargetGroup(false)
    }
  }

  async function importToTarget() {
    if (!channel || targetID == null || !selectedKey) {
      toast.error("尚未选择 API Key 或可用的 Sub2API 目标")
      return
    }
const targetGroupIDs = selectedTargetGroupIDs
      .map((value) => Number(value))
      .filter((value) => Number.isInteger(value) && value > 0)
    if (targetGroupIDs.length === 0) {
      toast.error("请选择至少一个目标分组，或先新建一个目标分组")
      return
    }
    const key = selectedKey
    const groupID = key.group_id != null
      ? String(key.group_id)
      : (key.group_name || key.group || "").trim()
    if (!groupID) {
      toast.error("该 API Key 没有可绑定的上游分组")
      return
    }
    const firstTargetGroup = targetGroups.find((group) => group.id === targetGroupIDs[0])
    const platform = normalizeAccountPlatform(firstTargetGroup?.platform)
      || inferAccountPlatform(`${channel.name} ${key.name} ${key.group_name || key.group || ""}`)
    setImportingID(key.id)
    try {
      const secret = await revealKey(key)
      const accountName = `${channel.name} - ${key.name || key.id}`
      const result = await importAPIKeyToTarget(targetID, {
        name: accountName,
        platform,
        type: "apikey",
        base_url: channel.site_url,
        api_key: secret,
        source_channel_id: channel.id,
        source_group_id: groupID,
        source_group_name: key.group_name || key.group || groupID,
        target_group_ids: targetGroupIDs,
        rate_policy: {
          enabled: true,
          mode: "first",
          offset: 0,
          bindings: [{ channel_id: channel.id, group_id: groupID }],
        },
      })
      await applyTargetRatePolicies(targetID)
      toast.success(`${result.created ? "已新增" : "已更新"}目标账号 #${result.account.id}，倍率同步已排队`)
      setImportDialogOpen(false)
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "导入目标账号失败")
    } finally {
      setImportingID(null)
    }
  }

  async function deleteKey(key: ChannelAPIKey) {
    if (!channel) return
    const ok = await confirm({
      title: `删除密钥 ${key.name || key.id}？`,
      description: "删除后该上游密钥将不可恢复。",
      confirmLabel: "删除",
      destructive: true,
    })
    if (!ok) return
    try {
      await apiFetch(`/channels/${channel.id}/api-keys/${key.id}`, { method: "DELETE" })
      toast.success("密钥已删除")
      const groupID = key.group_id != null
        ? String(key.group_id)
        : (key.group_name || key.group || "").trim()
      if (targetID != null && groupID) {
        await detachKeyRateBindings(targetID, channel.id, groupID)
      }
      reload()
    } catch (e) {
      const err = e as Error
      toast.error(err.message || "删除密钥失败")
    }
  }

  async function detachKeyRateBindings(targetIDValue: number, channelID: number, groupID: string) {
    try {
      const workspace = await getTargetRatePolicies(targetIDValue)
      const targets: Array<{ type: RatePolicyTargetType; item: RatePolicyTarget }> = [
        ...workspace.groups.map((item) => ({ type: "group" as const, item })),
        ...workspace.accounts.map((item) => ({ type: "account" as const, item })),
      ]
      let changed = false
      for (const { type, item } of targets) {
        const remaining = item.bindings.filter((binding) => !(binding.channel_id === channelID && binding.group_id === groupID))
        if (remaining.length === item.bindings.length) continue
        changed = true
        await saveTargetRatePolicy(targetIDValue, type, item.id, {
          enabled: remaining.length > 0 ? item.rule.enabled : false,
          mode: item.rule.mode,
          offset: item.rule.offset,
          expression: item.rule.expression ?? "",
          bindings: remaining.map((binding) => ({ channel_id: binding.channel_id, group_id: binding.group_id })),
        })
      }
      if (changed) {
        await applyTargetRatePolicies(targetIDValue)
        toast.success("已同步移除倍率源绑定")
      }
    } catch {
      toast.error("倍率源绑定清理失败，请到倍率同步面板手动处理")
    }
  }

  const isMigrate = mode === "migrate"
  const targetLocked = mode === "create" && actionType === "create" && targetGroup != null
  const dialogTitle = isMigrate ? "迁移 API Key" : "API 密钥管理"
  const description = channel
    ? `${channel.name} · ${channelTypeLabel(channel.type)}${initialAction ? ` · 目标分组 ${targetGroupName}` : ""}`
    : "管理上游 API 密钥。"

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="sm:max-w-5xl">
          <DialogHeader>
            <DialogTitle>{dialogTitle}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </DialogHeader>

          {mode !== "list" && (groupsLoading || visibleGroupsError) ? (
            <div className={cn(
              "flex items-center justify-between gap-3 rounded-md border px-3 py-2 text-sm",
              visibleGroupsError
                ? "border-destructive/30 bg-destructive/5 text-destructive"
                : "border-border bg-muted/20 text-muted-foreground",
            )}>
              <span className="min-w-0 break-words">
                {groupsLoading ? "正在校验目标分组…" : visibleGroupsError}
              </span>
              {visibleGroupsError ? (
                <Button type="button" variant="outline" size="sm" className="shrink-0 gap-1.5" onClick={retryGroups}>
                  <RefreshCw className="size-3.5" />
                  重试校验
                </Button>
              ) : (
                <Loader2 className="size-4 shrink-0 animate-spin" />
              )}
            </div>
          ) : null}

          {mode === "list" || mode === "migrate" ? (
            <div className="space-y-4">
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                  <div className="relative w-full sm:max-w-sm sm:flex-1">
                    <Search className="pointer-events-none absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
                    <Input
                      value={search}
                      onChange={(e) => {
                        setSearch(e.target.value)
                        setPage(1)
                      }}
                      placeholder="搜索名称或密钥"
                      className="pl-8"
                    />
                  </div>
                  <Select
                    value={status}
                    onValueChange={(value) => {
                      setStatus(value as ChannelAPIKeyStatus | "all")
                      setPage(1)
                    }}
                  >
                    <SelectTrigger className="w-full sm:w-28">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">全部状态</SelectItem>
                      <SelectItem value="active">启用</SelectItem>
                      <SelectItem value="disabled">停用</SelectItem>
                      <SelectItem value="expired">已过期</SelectItem>
                      <SelectItem value="quota_exhausted">额度耗尽</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                {isMigrate ? (
                  <div className="flex min-h-9 items-center gap-2 text-xs text-muted-foreground">
                    <ArrowRightLeft className="size-4" />
                    <span>迁移到</span>
                    <Badge variant="outline" className="max-w-56 truncate text-foreground">
                      {liveTargetGroup?.name || targetGroupName}
                    </Badge>
                  </div>
                ) : (
                  <Button type="button" size="sm" className="w-full gap-1.5 sm:w-auto" onClick={openCreate}>
                    <Plus className="size-4" />
                    新建密钥
                  </Button>
                )}
              </div>

              <RadioGroup
                role={isMigrate ? "radiogroup" : "presentation"}
                value={selectedKey == null ? "" : String(selectedKey.id)}
                onValueChange={(value) => {
                  const key = items.find((item) => String(item.id) === value)
                  if (key) setSelectedKey(key)
                }}
                className="block"
              >
              <div className="overflow-hidden rounded-lg border border-border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      {isMigrate ? <TableHead className="w-12"><span className="sr-only">选择</span></TableHead> : null}
                      <TableHead className="min-w-28">名称</TableHead>
                      <TableHead className="min-w-40">密钥</TableHead>
                      <TableHead>状态</TableHead>
                      <TableHead className="min-w-52">分组</TableHead>
                      <TableHead className="min-w-24">额度</TableHead>
                      <TableHead className="min-w-32">过期</TableHead>
                      {!isMigrate ? <TableHead className="min-w-28 text-right">操作</TableHead> : null}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {loading ? (
                      <TableRow>
                        <TableCell colSpan={7} className="h-28 text-center text-muted-foreground">
                          <Loader2 className="mx-auto mb-2 size-4 animate-spin" />
                          加载中…
                        </TableCell>
                      </TableRow>
                    ) : items.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={7} className="h-28 text-center text-muted-foreground">
                          暂无密钥
                        </TableCell>
                      </TableRow>
                    ) : (
                      items.map((item) => {
                        const displayKey = revealedKeys[item.id] || maskKey(item.key)
                        const isRevealing = revealingID === item.id
                        const isCurrentTarget = !!(isMigrate && channel && targetGroup && keyMatchesGroup(item, channel, targetGroup))
                        const migrationSourceGroup = channel ? sourceGroupLabel(item, channel, groups) : "未分组"
                        const selectionDisabled = isCurrentTarget || groupsLoading || targetValidationError != null
                        return (
                        <TableRow
                          key={item.id}
                          className={cn(
                            isMigrate && !selectionDisabled && "cursor-pointer",
                            isCurrentTarget && "bg-muted/30 opacity-60",
                          )}
                          onClick={() => {
                            if (isMigrate && !selectionDisabled) setSelectedKey(item)
                          }}
                        >
                          {isMigrate ? (
                            <TableCell onClick={(event) => event.stopPropagation()}>
                              <RadioGroupItem
                                value={String(item.id)}
                                disabled={selectionDisabled}
                                aria-label={`选择 ${item.name || item.id}`}
                              />
                            </TableCell>
                          ) : null}
                          <TableCell className="font-medium">{item.name || "未命名"}</TableCell>
                          <TableCell className="min-w-40">
                            {isMigrate ? (
                              <span className="font-mono text-xs">{maskKey(item.key)}</span>
                            ) : (
                              <Input
                                readOnly
                                value={isRevealing ? "加载中…" : displayKey}
                                className="h-8 w-40 cursor-pointer truncate font-mono text-xs sm:w-48"
                                title="点击显示完整密钥"
                                disabled={isRevealing}
                                onClick={() => void revealAndShow(item)}
                              />
                            )}
                          </TableCell>
                          <TableCell>
                            <Badge variant="outline" className={cn(statusClass(item.status))}>
                              {statusLabel(item.status)}
                            </Badge>
                          </TableCell>
                          <TableCell>
                            <div className="max-w-64 whitespace-normal">
                              <div className="flex flex-wrap items-center gap-1.5">
                                <p className="break-words text-xs font-medium">
                                  {isMigrate ? migrationSourceGroup : groupDisplayName(item)}
                                </p>
                                {isCurrentTarget ? <Badge variant="outline">已在当前分组</Badge> : null}
                              </div>
                              <p className="break-words text-[11px] leading-4 text-muted-foreground">
                                {item.group_description || "无描述"}
                              </p>
                              {item.group_ratio > 0 ? (
                                <p className="text-[11px] text-muted-foreground">倍率 {formatRatio(item.group_ratio)}</p>
                              ) : null}
                            </div>
                          </TableCell>
                          <TableCell>
                            {isNewAPI
                              ? item.unlimited_quota
                                ? "无限"
                                : decimal(item.quota, 0)
                              : `${decimal(item.quota_used)}/${item.quota > 0 ? decimal(item.quota) : "无限"}`}
                          </TableCell>
                          <TableCell>
                            {isNewAPI
                              ? item.expired_time === -1 || !item.expired_time
                                ? "永不过期"
                                : dateTime(new Date(item.expired_time * 1000).toISOString())
                              : item.expires_at
                                ? dateTime(item.expires_at)
                                : "永不过期"}
                          </TableCell>
                          {!isMigrate ? <TableCell>
                            <div className="flex justify-end gap-1">
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon-sm"
                                title="一键添加到 Sub2API 并同步倍率"
                                disabled={targetID == null || importingID === item.id}
                                onClick={() => openImportDialog(item)}
                              >
                                {importingID === item.id ? (
                                  <Loader2 className="size-4 animate-spin" />
                                ) : (
                                  <ArrowRightLeft className="size-4" />
                                )}
                              </Button>
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon-sm"
                                title="复制完整密钥"
                                disabled={isRevealing}
                                onClick={() => void revealAndCopy(item)}
                              >
                                {isRevealing ? (
                                  <Loader2 className="size-4 animate-spin" />
                                ) : (
                                  <Copy className="size-4" />
                                )}
                              </Button>
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon-sm"
                                title="编辑"
                                onClick={() => openEdit(item)}
                              >
                                <Pencil className="size-4" />
                              </Button>
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon-sm"
                                title="删除"
                                className="text-destructive hover:text-destructive"
                                onClick={() => void deleteKey(item)}
                              >
                                <Trash2 className="size-4" />
                              </Button>
                            </div>
                          </TableCell> : null}
                        </TableRow>
                        )
                      })
                    )}
                  </TableBody>
                </Table>
              </div>
              </RadioGroup>

              {listError ? (
                <div className="flex items-center justify-between gap-3 text-sm text-destructive" role="alert">
                  <span className="min-w-0 break-words">{listError}</span>
                  <Button type="button" variant="outline" size="sm" className="shrink-0" onClick={reload}>
                    重新加载
                  </Button>
                </div>
              ) : null}
              {isMigrate && error ? <p className="text-sm text-destructive" role="alert">{error}</p> : null}

              <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                <p className="text-xs text-muted-foreground">
                  共 {data?.total ?? 0} 条，第 {data?.page ?? page}/{totalPages} 页
                </p>
                <div className="grid grid-cols-2 gap-2 sm:flex">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={page <= 1 || loading}
                    onClick={() => setPage((p) => Math.max(1, p - 1))}
                  >
                    上一页
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    disabled={page >= totalPages || loading}
                    onClick={() => setPage((p) => p + 1)}
                  >
                    下一页
                  </Button>
                </div>
              </div>

              {isMigrate ? (
                <DialogFooter className="gap-2 sm:gap-0">
                  <div className="mr-auto min-w-0 text-left text-xs text-muted-foreground">
                    {selectedKey ? `已选择：${selectedKey.name || selectedKey.id}` : "请选择一个 API Key"}
                  </div>
                  <Button type="button" variant="outline" disabled={submitting} onClick={() => onOpenChange(false)}>
                    取消
                  </Button>
                  <Button
                    type="button"
                    disabled={submitting || groupsLoading || targetValidationError != null || selectedKey == null}
                    onClick={() => void migrateSelectedKey()}
                  >
                    {submitting ? "迁移中…" : "迁移"}
                  </Button>
                </DialogFooter>
              ) : null}
            </div>
          ) : (
            <form onSubmit={handleSubmit} className="space-y-4">
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label="名称">
                  <Input
                    value={form.name}
                    onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                    disabled={submitting}
                    required
                  />
                </Field>
                {mode === "create" ? (
                  <Field label="自定义 Key">
                    <Input
                      value={form.custom_key}
                      onChange={(e) => setForm((f) => ({ ...f, custom_key: e.target.value }))}
                      placeholder="留空则由上游生成"
                      disabled={submitting}
                    />
                  </Field>
                ) : (
                  <Field label="状态">
                    <Select
                      value={form.status}
                      onValueChange={(value) => setForm((f) => ({ ...f, status: value as "active" | "disabled" }))}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="active">启用</SelectItem>
                        <SelectItem value="disabled">停用</SelectItem>
                      </SelectContent>
                    </Select>
                  </Field>
                )}
              </div>

              {isNewAPI ? (
                <NewAPIFields form={form} setForm={setForm} disabled={submitting} groups={groups} groupsLoading={groupsLoading} groupLocked={targetLocked} selectedGroup={groupByName.get(form.group)} />
              ) : (
                <Sub2APIFields form={form} setForm={setForm} disabled={submitting} mode={mode} groups={groups} groupsLoading={groupsLoading} groupLocked={targetLocked} selectedGroup={groupByID.get(form.group_id)} />
              )}

              {error ? (
                <p className="text-sm text-destructive" role="alert">
                  {error}
                </p>
              ) : null}

              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    setMode("list")
                    setEditing(null)
                    setError(null)
                  }}
                  disabled={submitting}
                >
                  返回
                </Button>
                <Button type="submit" disabled={submitting || groupsLoading || targetValidationError != null}>
                  {submitting ? "保存中…" : mode === "create" ? "创建" : "保存"}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
      <Dialog
        open={importDialogOpen}
        onOpenChange={(nextOpen) => {
          setImportDialogOpen(nextOpen)
          if (!nextOpen) setSelectedKey(null)
        }}
      >
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>添加 API Key 到 Sub2API</DialogTitle>
            <DialogDescription>
              {selectedKey ? `${selectedKey.name || selectedKey.id} · 显式选择目标分组后导入` : "选择目标分组"}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
<div className="space-y-2">
              <Label>目标分组（可多选）</Label>
              <div className="max-h-56 space-y-1 overflow-y-auto rounded-md border border-border p-2">
                {targetGroupsLoading ? (
                  <p className="px-2 py-1 text-sm text-muted-foreground">加载目标分组中...</p>
                ) : Array.from(
                    new Map(
                      targetGroups
                        .filter((group) => group.id != null)
                        .map((group) => [String(group.id), group]),
                    ).values(),
                  ).map((group) => {
                    const groupID = String(group.id)
                    const checked = selectedTargetGroupIDs.includes(groupID)
                    return (
                      <label
                        key={groupID}
                        className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-accent"
                      >
                        <Checkbox
                          checked={checked}
                          onCheckedChange={(value) => {
                            setSelectedTargetGroupIDs((current) =>
                              value
                                ? [...current, groupID]
                                : current.filter((id) => id !== groupID),
                            )
                          }}
                          disabled={importingID != null}
                        />
                        <span className="flex-1">
                          {group.name || "未命名"} · {platformLabel(group.platform)} ·{" "}
                          {formatRatio(group.ratio ?? group.rate_multiplier ?? 1)}
                        </span>
                      </label>
                    )
                  })}
              </div>
              {selectedTargetGroupIDs.length > 1 ? (
                <p className="text-xs text-muted-foreground">
                  已选 {selectedTargetGroupIDs.length} 个分组，将绑定到同一账号；请确保所选分组平台一致。
                </p>
              ) : null}
              {targetGroupsError ? <p className="text-xs text-destructive">{targetGroupsError}</p> : null}
              {!targetGroupsLoading && targetGroups.length === 0 && !targetGroupsError ? (
                <p className="text-xs text-muted-foreground">当前目标还没有分组，请先在下面创建。</p>
              ) : null}
            </div>
            <div className="space-y-3 rounded-md border border-border p-3">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <Label>新建目标分组</Label>
                  <p className="text-xs text-muted-foreground">创建后自动选中，并用于本次账号导入。</p>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  onClick={() => void createImportTargetGroup()}
                  disabled={creatingTargetGroup || importingID != null}
                  aria-label="新建目标分组"
                  title="新建目标分组"
                >
                  {creatingTargetGroup ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
                </Button>
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label htmlFor="import-target-group-name">名称</Label>
                  <Input
                    id="import-target-group-name"
                    value={newTargetGroupName}
                    onChange={(event) => setNewTargetGroupName(event.target.value)}
                    placeholder="例如：Gemini 主力"
                    disabled={creatingTargetGroup || importingID != null}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="import-target-group-rate">倍率</Label>
                  <Input
                    id="import-target-group-rate"
                    type="number"
                    min="0.0001"
                    step="0.0001"
                    value={newTargetGroupRate}
                    onChange={(event) => setNewTargetGroupRate(event.target.value)}
                    disabled={creatingTargetGroup || importingID != null}
                  />
                </div>
              </div>
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label>平台</Label>
                  <Select value={newTargetGroupPlatform} onValueChange={setNewTargetGroupPlatform} disabled={creatingTargetGroup || importingID != null}>
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="openai">OpenAI</SelectItem>
                      <SelectItem value="anthropic">Anthropic</SelectItem>
                      <SelectItem value="gemini">Gemini</SelectItem>
                      <SelectItem value="antigravity">Antigravity</SelectItem>
                      <SelectItem value="grok">Grok</SelectItem>
                      <SelectItem value="composite">Composite</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="import-target-group-description">备注</Label>
                  <Input
                    id="import-target-group-description"
                    value={newTargetGroupDescription}
                    onChange={(event) => setNewTargetGroupDescription(event.target.value)}
                    disabled={creatingTargetGroup || importingID != null}
                  />
                </div>
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setImportDialogOpen(false)} disabled={importingID != null}>取消</Button>
            <Button type="button" onClick={() => void importToTarget()} disabled={importingID != null || targetGroupsLoading || selectedTargetGroupIDs.length === 0}>
              {importingID != null ? <Loader2 className="size-4 animate-spin" /> : <ArrowRightLeft className="size-4" />}
              导入并同步倍率
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      {confirmDialog}
    </>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      {children}
    </div>
  )
}

function InlineSwitch({
  label,
  checked,
  disabled,
  onCheckedChange,
}: {
  label: string
  checked: boolean
  disabled?: boolean
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <label className="flex items-center justify-between rounded-lg border border-border px-3 py-2 text-sm">
      <span>{label}</span>
      <Switch checked={checked} onCheckedChange={onCheckedChange} disabled={disabled} />
    </label>
  )
}

function NewAPIFields({
  form,
  setForm,
  disabled,
  groups,
  groupsLoading,
  groupLocked,
  selectedGroup,
}: {
  form: KeyForm
  setForm: React.Dispatch<React.SetStateAction<KeyForm>>
  disabled: boolean
  groups: ChannelAPIKeyGroup[]
  groupsLoading: boolean
  groupLocked: boolean
  selectedGroup?: ChannelAPIKeyGroup
}) {
  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-3">
        <Field label="分组">
          <Select
            value={form.group}
            onValueChange={(value) => setForm((f) => ({ ...f, group: value }))}
            disabled={disabled || groupsLoading || groupLocked}
          >
            <SelectTrigger className="w-full">
              {selectedGroup ? (
                <SelectValue>{selectedGroup.name} · {formatRatio(selectedGroup.ratio)}</SelectValue>
              ) : (
                <SelectValue placeholder={groupsLoading ? "加载中…" : "选择分组"} />
              )}
            </SelectTrigger>
            <SelectContent className="max-h-64">
              {Array.from(
                new Map(
                  groups
                    .filter((group) => group.name != null && group.name.trim() !== "")
                    .map((group) => [group.name, group]),
                ).values(),
              ).map((group) => (
                <SelectItem key={group.name} value={group.name}>
                  <span className="flex flex-col items-start">
                    <span>{group.name} · {formatRatio(group.ratio)}</span>
                    <span className="max-w-96 whitespace-normal break-words text-[11px] text-muted-foreground">
                      {group.description || "无描述"}
                    </span>
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <GroupHint group={selectedGroup} />
        </Field>
        <Field label="剩余额度">
          <Input value={form.remain_quota} onChange={(e) => setForm((f) => ({ ...f, remain_quota: e.target.value }))} disabled={disabled} inputMode="numeric" />
        </Field>
        <Field label="过期时间戳">
          <Input value={form.expired_time} onChange={(e) => setForm((f) => ({ ...f, expired_time: e.target.value }))} disabled={disabled} inputMode="numeric" />
        </Field>
      </div>
      <div className="grid gap-3 sm:grid-cols-3">
        <InlineSwitch label="无限额度" checked={form.unlimited_quota} disabled={disabled} onCheckedChange={(v) => setForm((f) => ({ ...f, unlimited_quota: v }))} />
        <InlineSwitch label="启用模型限制" checked={form.model_limits_enabled} disabled={disabled} onCheckedChange={(v) => setForm((f) => ({ ...f, model_limits_enabled: v }))} />
        <InlineSwitch label="跨分组重试" checked={form.cross_group_retry} disabled={disabled} onCheckedChange={(v) => setForm((f) => ({ ...f, cross_group_retry: v }))} />
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="模型限制">
          <Textarea value={form.model_limits} onChange={(e) => setForm((f) => ({ ...f, model_limits: e.target.value }))} placeholder="逗号分隔" disabled={disabled} />
        </Field>
        <Field label="允许 IP">
          <Textarea value={form.allow_ips} onChange={(e) => setForm((f) => ({ ...f, allow_ips: e.target.value }))} placeholder="每行一个 IP" disabled={disabled} />
        </Field>
      </div>
    </div>
  )
}

function Sub2APIFields({
  form,
  setForm,
  disabled,
  mode,
  groups,
  groupsLoading,
  groupLocked,
  selectedGroup,
}: {
  form: KeyForm
  setForm: React.Dispatch<React.SetStateAction<KeyForm>>
  disabled: boolean
  mode: Mode
  groups: ChannelAPIKeyGroup[]
  groupsLoading: boolean
  groupLocked: boolean
  selectedGroup?: ChannelAPIKeyGroup
}) {
  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-4">
        <Field label="分组 ID">
          <Select
            value={form.group_id}
            onValueChange={(value) => setForm((f) => ({ ...f, group_id: value }))}
            disabled={disabled || groupsLoading || groupLocked}
          >
            <SelectTrigger className="w-full">
              {selectedGroup ? (
                <SelectValue>{selectedGroup.name} · {formatRatio(selectedGroup.ratio)}</SelectValue>
              ) : (
                <SelectValue placeholder={groupsLoading ? "加载中…" : "选择分组"} />
              )}
            </SelectTrigger>
            <SelectContent className="max-h-64">
              {groups
                .filter((group) => group.id != null && group.name != null)
                .map((group) => (
                  <SelectItem key={group.id} value={String(group.id)}>
                    <span className="flex flex-col items-start">
                      <span>{group.name || "未命名"} · {formatRatio(group.ratio)}</span>
                      <span className="max-w-96 whitespace-normal break-words text-[11px] text-muted-foreground">
                        {group.description || "无描述"}
                      </span>
                    </span>
                  </SelectItem>
                ))}
            </SelectContent>
          </Select>
          <GroupHint group={selectedGroup} />
        </Field>
        <Field label="额度 USD">
          <Input value={form.quota} onChange={(e) => setForm((f) => ({ ...f, quota: e.target.value }))} disabled={disabled} inputMode="decimal" />
        </Field>
        {mode === "create" ? (
          <Field label="过期天数">
            <Input value={form.expires_in_days} onChange={(e) => setForm((f) => ({ ...f, expires_in_days: e.target.value }))} placeholder="留空永不过期" disabled={disabled} inputMode="numeric" />
          </Field>
        ) : (
          <Field label="过期时间">
            <Input type="datetime-local" value={form.expires_at} onChange={(e) => setForm((f) => ({ ...f, expires_at: e.target.value }))} disabled={disabled} />
          </Field>
        )}
        <Field label="5h 限额">
          <Input value={form.rate_limit_5h} onChange={(e) => setForm((f) => ({ ...f, rate_limit_5h: e.target.value }))} disabled={disabled} inputMode="decimal" />
        </Field>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="IP 白名单">
          <Textarea value={form.ip_whitelist} onChange={(e) => setForm((f) => ({ ...f, ip_whitelist: e.target.value }))} placeholder="每行一个 IP 或 CIDR" disabled={disabled} />
        </Field>
        <Field label="IP 黑名单">
          <Textarea value={form.ip_blacklist} onChange={(e) => setForm((f) => ({ ...f, ip_blacklist: e.target.value }))} placeholder="每行一个 IP 或 CIDR" disabled={disabled} />
        </Field>
      </div>
      <div className="grid gap-3 sm:grid-cols-4">
        <Field label="1d 限额">
          <Input value={form.rate_limit_1d} onChange={(e) => setForm((f) => ({ ...f, rate_limit_1d: e.target.value }))} disabled={disabled} inputMode="decimal" />
        </Field>
        <Field label="7d 限额">
          <Input value={form.rate_limit_7d} onChange={(e) => setForm((f) => ({ ...f, rate_limit_7d: e.target.value }))} disabled={disabled} inputMode="decimal" />
        </Field>
        {mode === "edit" ? (
          <>
            <InlineSwitch label="重置配额" checked={form.reset_quota} disabled={disabled} onCheckedChange={(v) => setForm((f) => ({ ...f, reset_quota: v }))} />
            <InlineSwitch label="重置限额用量" checked={form.reset_rate_limit_usage} disabled={disabled} onCheckedChange={(v) => setForm((f) => ({ ...f, reset_rate_limit_usage: v }))} />
          </>
        ) : null}
      </div>
    </div>
  )
}

function GroupHint({ group }: { group?: ChannelAPIKeyGroup }) {
  if (!group) return null
  return (
    <p className="whitespace-normal break-words text-[11px] leading-4 text-muted-foreground">
      {group.description || "无描述"} · 倍率 {formatRatio(group.ratio)}
    </p>
  )
}
