import { useEffect, useState } from "react"
import { Megaphone, Pencil, Plus, Trash2 } from "lucide-react"
import { toast } from "sonner"
import {
  OperationsEmpty,
  OperationsError,
  OperationsLoading,
  OperationsPageHeader,
  OperationsStatusBadge,
} from "@/components/operations/operations-layout"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { useConfirm } from "@/components/ui/confirm-dialog"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { useOperationsTarget } from "@/hooks/use-operations-target"
import {
  createTargetAnnouncementRule,
  deleteTargetAnnouncementRule,
  listTargetAnnouncementRules,
  updateTargetAnnouncementRule,
  type TargetAnnouncementRule,
  type TargetAnnouncementRuleInput,
} from "@/lib/operations-api"

const emptyRule: TargetAnnouncementRuleInput = {
  name: "",
  enabled: true,
  title_template: "",
  content_template: "",
  target_group_ids: [],
  status: "draft",
  notify_mode: "silent",
}

export function TargetAnnouncementSettings() {
  const target = useOperationsTarget()
  const { confirm, dialog: confirmDialog } = useConfirm()
  const [rules, setRules] = useState<TargetAnnouncementRule[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reload, setReload] = useState(0)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<TargetAnnouncementRule | null>(null)
  const [form, setForm] = useState<TargetAnnouncementRuleInput>(emptyRule)
  const [groupIDs, setGroupIDs] = useState("")
  const [saving, setSaving] = useState(false)
  const [deletingID, setDeletingID] = useState<number | null>(null)

  useEffect(() => {
    if (target.selectedTargetID == null) {
      setRules([])
      return
    }
    let cancelled = false
    setLoading(true)
    setError(null)
    listTargetAnnouncementRules(target.selectedTargetID)
      .then((result) => {
        if (!cancelled) setRules(result)
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : "目标公告规则加载失败")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [reload, target.selectedTargetID])

  function openCreate() {
    setEditingRule(null)
    setForm({ ...emptyRule, target_group_ids: [] })
    setGroupIDs("")
    setDialogOpen(true)
  }

  function openEdit(rule: TargetAnnouncementRule) {
    setEditingRule(rule)
    setForm({
      name: rule.name,
      enabled: rule.enabled,
      title_template: rule.title_template,
      content_template: rule.content_template,
      target_group_ids: [...rule.target_group_ids],
      status: rule.status,
      notify_mode: rule.notify_mode,
    })
    setGroupIDs(rule.target_group_ids.join(", "))
    setDialogOpen(true)
  }

  function patch<K extends keyof TargetAnnouncementRuleInput>(
    key: K,
    value: TargetAnnouncementRuleInput[K],
  ) {
    setForm((current) => ({ ...current, [key]: value }))
  }

  async function save() {
    if (target.selectedTargetID == null) return
    const name = form.name.trim()
    const title = form.title_template.trim()
    const content = form.content_template.trim()
    if (!name || !title || !content) {
      toast.error("请填写规则名称、公告标题和公告内容")
      return
    }

    const parsedGroupIDs = groupIDs
      .split(/[，,\s]+/)
      .filter(Boolean)
      .map(Number)
    if (parsedGroupIDs.some((id) => !Number.isSafeInteger(id) || id <= 0)) {
      toast.error("目标分组 ID 只能填写正整数")
      return
    }

    const input: TargetAnnouncementRuleInput = {
      ...form,
      name,
      title_template: title,
      content_template: content,
      target_group_ids: [...new Set(parsedGroupIDs)],
    }
    setSaving(true)
    try {
      if (editingRule) {
        const saved = await updateTargetAnnouncementRule(target.selectedTargetID, editingRule.id, input)
        setRules((current) => current.map((rule) => (rule.id === saved.id ? saved : rule)))
        toast.success("目标公告规则已更新")
      } else {
        const created = await createTargetAnnouncementRule(target.selectedTargetID, input)
        setRules((current) => [created, ...current])
        toast.success("目标公告规则已创建")
      }
      setDialogOpen(false)
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "目标公告规则保存失败")
    } finally {
      setSaving(false)
    }
  }

  async function remove(rule: TargetAnnouncementRule) {
    if (target.selectedTargetID == null) return
    const accepted = await confirm({
      title: `删除目标公告规则 ${rule.name}？`,
      description: "删除后 Worker 将不再根据该规则向目标站点发布公告。",
      confirmLabel: "删除",
      destructive: true,
    })
    if (!accepted) return
    setDeletingID(rule.id)
    try {
      await deleteTargetAnnouncementRule(target.selectedTargetID, rule.id)
      setRules((current) => current.filter((item) => item.id !== rule.id))
      toast.success("目标公告规则已删除")
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "目标公告规则删除失败")
    } finally {
      setDeletingID(null)
    }
  }

  return (
    <div className="space-y-5">
      <OperationsPageHeader
        icon={Megaphone}
        title="目标公告"
        description="管理发布到目标站点的公告模板和分组范围；上游公告采集与通知渠道继续由原有模块负责。"
        targets={target.targets}
        selectedTargetID={target.selectedTargetID}
        targetLoading={target.loading}
        onTargetChange={target.selectTarget}
        refreshing={loading}
        onRefresh={() => setReload((value) => value + 1)}
        actions={
          <Button size="sm" onClick={openCreate} disabled={target.selectedTargetID == null}>
            <Plus className="size-3.5" />
            新建规则
          </Button>
        }
      />

      {target.error ? <OperationsError title="目标站点加载失败" message={target.error} /> : null}
      {error ? <OperationsError message={error} /> : null}
      {loading && rules.length === 0 ? <OperationsLoading rows={4} /> : null}
      {!loading && !error && target.selectedTargetID != null && rules.length === 0 ? (
        <OperationsEmpty title="还没有目标公告规则" description="创建规则后，扩展 Worker 可按模板和目标分组发布公告。" />
      ) : null}

      {rules.length > 0 ? (
        <div className="divide-y divide-border rounded-md border border-border bg-background">
          {rules.map((rule) => (
            <div key={rule.id} className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0 space-y-2">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="font-medium text-foreground">{rule.name}</p>
                  <OperationsStatusBadge status={rule.status} />
                  <OperationsStatusBadge status={rule.enabled ? "active" : "disabled"} />
                  <span className="text-xs text-muted-foreground">
                    {rule.notify_mode === "popup" ? "弹窗" : "静默"}
                  </span>
                </div>
                <div>
                  <p className="truncate text-sm text-foreground">{rule.title_template}</p>
                  <p className="mt-1 line-clamp-2 text-xs leading-5 text-muted-foreground">{rule.content_template}</p>
                </div>
                <p className="text-xs text-muted-foreground">
                  {rule.target_group_ids.length > 0
                    ? `目标分组 ${rule.target_group_ids.join(", ")}`
                    : "全部目标分组"}
                </p>
              </div>
              <div className="flex shrink-0 items-center gap-1">
                <Button variant="ghost" size="icon-sm" onClick={() => openEdit(rule)} aria-label={`编辑 ${rule.name}`} title="编辑">
                  <Pencil className="size-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-destructive hover:bg-destructive/10 hover:text-destructive"
                  onClick={() => void remove(rule)}
                  disabled={deletingID === rule.id}
                  aria-label={`删除 ${rule.name}`}
                  title="删除"
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
            </div>
          ))}
        </div>
      ) : null}

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{editingRule ? "编辑目标公告规则" : "新建目标公告规则"}</DialogTitle>
            <DialogDescription>模板变量由目标公告 Worker 在发布时解析；分组留空表示应用到全部目标分组。</DialogDescription>
          </DialogHeader>

          <div className="grid gap-4 sm:grid-cols-2">
            <AnnouncementField label="规则名称" htmlFor="announcement-name" className="sm:col-span-2">
              <Input id="announcement-name" value={form.name} onChange={(event) => patch("name", event.target.value)} />
            </AnnouncementField>
            <AnnouncementField label="状态" htmlFor="announcement-status">
              <Select value={form.status} onValueChange={(value) => patch("status", value as TargetAnnouncementRuleInput["status"])}>
                <SelectTrigger id="announcement-status"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="draft">草稿</SelectItem>
                  <SelectItem value="active">生效</SelectItem>
                  <SelectItem value="archived">归档</SelectItem>
                </SelectContent>
              </Select>
            </AnnouncementField>
            <AnnouncementField label="通知方式" htmlFor="announcement-mode">
              <Select value={form.notify_mode} onValueChange={(value) => patch("notify_mode", value as TargetAnnouncementRuleInput["notify_mode"])}>
                <SelectTrigger id="announcement-mode"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="silent">静默</SelectItem>
                  <SelectItem value="popup">弹窗</SelectItem>
                </SelectContent>
              </Select>
            </AnnouncementField>
            <AnnouncementField label="公告标题模板" htmlFor="announcement-title" className="sm:col-span-2">
              <Input id="announcement-title" value={form.title_template} onChange={(event) => patch("title_template", event.target.value)} />
            </AnnouncementField>
            <AnnouncementField label="公告内容模板" htmlFor="announcement-content" className="sm:col-span-2">
              <Textarea id="announcement-content" rows={7} value={form.content_template} onChange={(event) => patch("content_template", event.target.value)} />
            </AnnouncementField>
            <AnnouncementField label="目标分组 ID" htmlFor="announcement-groups" className="sm:col-span-2">
              <Input id="announcement-groups" inputMode="numeric" value={groupIDs} onChange={(event) => setGroupIDs(event.target.value)} placeholder="留空表示全部；多个 ID 用逗号分隔" />
            </AnnouncementField>
            <label className="flex items-center gap-2 text-sm text-foreground sm:col-span-2">
              <Checkbox checked={form.enabled} onCheckedChange={(value) => patch("enabled", value === true)} />
              启用此发布规则
            </label>
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>取消</Button>
            <Button onClick={() => void save()} disabled={saving}>{saving ? "保存中..." : "保存"}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {confirmDialog}
    </div>
  )
}

function AnnouncementField({
  label,
  htmlFor,
  className,
  children,
}: {
  label: string
  htmlFor: string
  className?: string
  children: React.ReactNode
}) {
  return (
    <div className={`space-y-2 ${className ?? ""}`}>
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
    </div>
  )
}
