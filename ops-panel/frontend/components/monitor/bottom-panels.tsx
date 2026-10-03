"use client"

import { Fragment, useEffect, useState } from "react"
import { toast } from "sonner"
import {
  AlertTriangle,
  ArrowUpRight,
  Bell,
  Clock3,
  RefreshCw,
  KeyRound,
  Pencil,
  Plus,
  Send,
  ShieldX,
  TestTube2,
  Trash2,
} from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { Badge } from "@/components/ui/badge"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  useAnnouncements,
  useCaptchaConfigs,
  useDashboardSummary,
  useNotificationChannels,
  useNotificationLogs,
} from "@/lib/queries"
import { apiFetch } from "@/lib/api"
import { useTriggerRefresh } from "@/lib/refresh-context"
import { channelTypeLabel, dateTime, decimal, money, relativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import { CaptchaFormDialog } from "@/components/monitor/captcha-form-dialog"
import { NotificationFormDialog } from "@/components/monitor/notification-form-dialog"
import type { LucideIcon } from "lucide-react"
import type {
  CaptchaConfig,
  NotificationChannel,
  NotificationEvent,
  NotificationChannelType,
  NotificationLog,
  NotificationLogPage,
  UpstreamAnnouncement,
} from "@/lib/api-types"

const eventMeta: Record<NotificationEvent, { icon: LucideIcon; cls: string }> = {
  balance_low: { icon: AlertTriangle, cls: "text-warning" },
  login_failed: { icon: ShieldX, cls: "text-danger" },
  captcha_failed: { icon: KeyRound, cls: "text-danger" },
  rate_changed: { icon: ArrowUpRight, cls: "text-brand" },
  rate_structure_changed: { icon: ArrowUpRight, cls: "text-brand" },
  rate_added: { icon: Plus, cls: "text-brand" },
  rate_removed: { icon: Trash2, cls: "text-warning" },
  announcement: { icon: Bell, cls: "text-brand" },
  monitor_failed: { icon: ShieldX, cls: "text-danger" },
  subscription_daily_remaining_low: { icon: AlertTriangle, cls: "text-warning" },
  subscription_weekly_remaining_low: { icon: AlertTriangle, cls: "text-warning" },
  subscription_monthly_remaining_low: { icon: AlertTriangle, cls: "text-warning" },
  subscription_expiring: { icon: Clock3, cls: "text-warning" },
  upstream_sync_group_changed: { icon: RefreshCw, cls: "text-brand" },
}

const FEED_PREVIEW_SIZE = 10
const FEED_DIALOG_SIZE = 20

export function AlertFeed() {
  const [detailOpen, setDetailOpen] = useState(false)
  const [page, setPage] = useState(1)
  const [feed, setFeed] = useState<NotificationLog[]>([])
  const [feedMeta, setFeedMeta] = useState<{ total: number; pages: number }>({
    total: 0,
    pages: 1,
  })
  const [feedLoading, setFeedLoading] = useState(false)
  const [feedError, setFeedError] = useState<string | null>(null)
  const preview = useNotificationLogs(1, FEED_PREVIEW_SIZE)
  const items = preview.data?.items ?? []

  function loadNextPage() {
    if (feedLoading || page >= feedMeta.pages) return
    setPage((prev) => prev + 1)
  }

  useEffect(() => {
    if (!detailOpen) return
    let cancelled = false
    setFeedLoading(true)
    setFeedError(null)
    apiFetch<NotificationLogPage>(
      `/notifications/logs?page=${page}&page_size=${FEED_DIALOG_SIZE}`,
    )
      .then((res) => {
        if (cancelled) return
        const next = Array.isArray(res?.items) ? res.items : []
        setFeed((prev) => (page === 1 ? next : [...prev, ...next]))
        setFeedMeta({
          total: res?.total ?? 0,
          pages: Math.max(1, res?.pages ?? 1),
        })
      })
      .catch((e) => {
        if (cancelled) return
        const err = e as Error
        setFeedError(err.message || "加载告警记录失败")
      })
      .finally(() => {
        if (!cancelled) setFeedLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [detailOpen, page])

  function openDetail() {
    setFeed([])
    setFeedMeta({ total: 0, pages: 1 })
    setFeedError(null)
    setPage(1)
    setDetailOpen(true)
  }

  return (
    <>
      <Card className="border border-border py-4 shadow-none lg:h-100 sm:py-6">
        <CardHeader className="flex shrink-0 flex-col gap-2 px-4 pb-2 sm:flex-row sm:items-center sm:justify-between sm:px-6">
          <CardTitle className="text-base font-semibold">{"告警动态"}</CardTitle>
          <div className="flex items-center gap-3 self-start sm:self-auto">
            <span className="text-xs text-muted-foreground">
              {preview.data?.total ? `共 ${preview.data.total} 条` : ""}
            </span>
            <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={openDetail}>
              {"查看更多"}
            </Button>
          </div>
        </CardHeader>
        <CardContent className="min-h-0 flex-1 px-0">
          {preview.loading ? (
            <p className="px-6 py-4 text-xs text-muted-foreground">{"加载中…"}</p>
          ) : items.length === 0 ? (
            <p className="px-6 py-4 text-xs text-muted-foreground">{"暂无告警记录"}</p>
          ) : (
            <div className="max-h-80 overflow-y-auto overscroll-contain lg:h-full lg:max-h-none">
              <ul className="divide-y divide-border">
                {items.map((a) => {
                  const meta = eventMeta[a.event] ?? { icon: AlertTriangle, cls: "text-muted-foreground" }
                  return (
                    <li key={a.id} className="px-4 py-3 sm:px-6">
                      <div className="flex min-w-0 items-start gap-2.5">
                        <meta.icon className={cn("size-4 shrink-0", meta.cls)} />
                        <div className="min-w-0 flex-1">
                          <div className="flex min-w-0 items-start justify-between gap-3">
                            <p className="min-w-0 flex-1 truncate text-sm text-foreground">{a.subject}</p>
                            <span className="shrink-0 text-xs text-muted-foreground">
                              {relativeTime(a.sent_at)}
                            </span>
                          </div>
                          <p className="truncate text-xs text-muted-foreground">
                            {a.channel_name
                              ? `${a.channel_name}${a.channel_type ? ` · ${channelTypeLabel(a.channel_type)}` : ""}`
                              : `渠道 #${a.channel_id}${a.channel_type ? ` · ${channelTypeLabel(a.channel_type)}` : ""}`}
                          </p>
                        </div>
                      </div>
                    </li>
                  )
                })}
              </ul>
            </div>
          )}
        </CardContent>
      </Card>

      <Dialog open={detailOpen} onOpenChange={setDetailOpen}>
        <DialogContent className="max-h-[85vh] overflow-hidden sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{"告警动态"}</DialogTitle>
          </DialogHeader>
          <div
            className="max-h-[60vh] overflow-y-auto overscroll-contain rounded-md border border-border"
            onScroll={(e) => {
              const target = e.target as HTMLElement
              if (target.scrollTop + target.clientHeight >= target.scrollHeight - 32) {
                loadNextPage()
              }
            }}
          >
            <ul className="divide-y divide-border">
              {feed.map((a) => {
                const meta = eventMeta[a.event] ?? { icon: AlertTriangle, cls: "text-muted-foreground" }
                return (
                  <li key={a.id} className="px-4 py-3">
                    <div className="flex min-w-0 items-start gap-2.5">
                        <meta.icon className={cn("mt-0.5 size-4 shrink-0", meta.cls)} />
                        <div className="min-w-0 flex-1">
                          <div className="flex min-w-0 items-start justify-between gap-3">
                            <p className="min-w-0 flex-1 text-sm font-medium text-foreground">{a.subject}</p>
                            <span className="shrink-0 text-xs text-muted-foreground">
                              {relativeTime(a.sent_at)}
                            </span>
                          </div>
                          <p className="mt-1 text-xs text-muted-foreground">
                            {a.channel_name
                              ? `${a.channel_name}${a.channel_type ? ` · ${channelTypeLabel(a.channel_type)}` : ""}`
                              : `渠道 #${a.channel_id}${a.channel_type ? ` · ${channelTypeLabel(a.channel_type)}` : ""}`}
                          </p>
                          {a.body ? (
                            <p className="mt-1 text-xs leading-5 text-muted-foreground">
                              {a.body}
                            </p>
                          ) : null}
                          {!a.success && a.error_message ? (
                            <p className="mt-1 text-xs leading-5 text-danger">{a.error_message}</p>
                          ) : null}
                        </div>
                    </div>
                  </li>
                )
              })}
              {feedLoading && feed.length === 0 ? (
                <li className="px-4 py-6 text-sm text-muted-foreground">{"加载中…"}</li>
              ) : null}
              {!feedLoading && feed.length === 0 ? (
                <li className="px-4 py-6 text-sm text-muted-foreground">{"暂无告警记录"}</li>
              ) : null}
              {feedLoading && feed.length > 0 ? (
                <li className="px-4 py-3 text-xs text-muted-foreground">{"加载更多中…"}</li>
              ) : null}
            </ul>
          </div>
          {feedError ? <p className="text-sm text-danger">{feedError}</p> : null}
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <span className="text-xs text-muted-foreground">
              {feedMeta.total > 0 ? `已加载 ${feed.length} / ${feedMeta.total} 条` : ""}
            </span>
            <Button
              variant="outline"
              size="sm"
              disabled={feedLoading || page >= feedMeta.pages}
              onClick={loadNextPage}
            >
              {feedLoading && page > 1 ? "加载中…" : "查看更多"}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

export function UpstreamAnnouncements() {
  const summary = useDashboardSummary()
  const preview = useAnnouncements(1, FEED_PREVIEW_SIZE)
  const [active, setActive] = useState<UpstreamAnnouncement | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const [page, setPage] = useState(1)
  const [feed, setFeed] = useState<UpstreamAnnouncement[]>([])
  const [feedMeta, setFeedMeta] = useState<{ total: number; pages: number }>({
    total: 0,
    pages: 1,
  })
  const [feedLoading, setFeedLoading] = useState(false)
  const [feedError, setFeedError] = useState<string | null>(null)
  const items = preview.data?.items ?? []
  const channels = summary.data?.channels ?? []

  const channelByID = new Map(channels.map((c) => [c.id, c] as const))
  const activeChannel = active ? channelByID.get(active.channel_id) : null

  function loadNextPage() {
    if (feedLoading || page >= feedMeta.pages) return
    setPage((prev) => prev + 1)
  }

  useEffect(() => {
    if (!detailOpen) return
    let cancelled = false
    setFeedLoading(true)
    setFeedError(null)
    apiFetch<{ items: UpstreamAnnouncement[]; total: number; pages: number }>(
      `/announcements?page=${page}&page_size=${FEED_DIALOG_SIZE}`,
    )
      .then((res) => {
        if (cancelled) return
        const next = Array.isArray(res?.items) ? res.items : []
        setFeed((prev) => (page === 1 ? next : [...prev, ...next]))
        setFeedMeta({
          total: res?.total ?? 0,
          pages: Math.max(1, res?.pages ?? 1),
        })
      })
      .catch((e) => {
        if (cancelled) return
        const err = e as Error
        setFeedError(err.message || "加载上游公告失败")
      })
      .finally(() => {
        if (!cancelled) setFeedLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [detailOpen, page])

  function openDetail() {
    setFeed([])
    setFeedMeta({ total: 0, pages: 1 })
    setFeedError(null)
    setPage(1)
    setDetailOpen(true)
  }

  return (
    <>
      <Card className="border border-border py-4 shadow-none lg:h-100 sm:py-6">
        <CardHeader className="flex shrink-0 flex-col gap-2 px-4 pb-2 sm:flex-row sm:items-center sm:justify-between sm:px-6">
          <CardTitle className="text-base font-semibold">{"上游公告"}</CardTitle>
          <div className="flex items-center gap-3 self-start sm:self-auto">
            <span className="text-xs text-muted-foreground">
              {preview.data?.total ? `共 ${preview.data.total} 条` : ""}
            </span>
            {preview.data?.total && preview.data.total > items.length ? (
              <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={openDetail}>
                {"查看更多"}
              </Button>
            ) : null}
          </div>
        </CardHeader>
        <CardContent className="min-h-0 flex-1 px-0">
          {preview.loading ? (
            <p className="px-6 py-4 text-xs text-muted-foreground">{"加载中…"}</p>
          ) : items.length === 0 ? (
            <p className="px-6 py-4 text-xs text-muted-foreground">{"暂无上游公告"}</p>
          ) : (
            <div className="max-h-80 overflow-y-auto overscroll-contain lg:h-full lg:max-h-none">
              <ul className="divide-y divide-border">
                {items.map((item) => {
                  const ch = channelByID.get(item.channel_id)
                  const title = announcementTitle(item)
                  const when = item.published_at || item.first_seen_at
                  return (
                    <li key={item.id}>
                      <button
                        type="button"
                        className="block w-full px-4 py-3 text-left transition-colors hover:bg-muted/40 sm:px-6"
                        onClick={() => setActive(item)}
                      >
                        <div className="mb-1.5 flex min-w-0 items-start justify-between gap-3">
                          <div className="flex min-w-0 flex-1 items-start gap-2">
                            <span className="min-w-0 flex-1 truncate text-sm font-medium text-foreground">{title}</span>
                            {item.type ? (
                              <Badge variant="outline" className="h-5 shrink-0 px-1.5 text-[10px]">
                                {item.type}
                              </Badge>
                            ) : null}
                          </div>
                          <span className="shrink-0 text-xs text-muted-foreground">{relativeTime(when)}</span>
                        </div>
                        <div className="flex min-w-0">
                          <span className="truncate text-xs text-muted-foreground">
                            {ch ? `${ch.name} · ${channelTypeLabel(ch.type)}` : `渠道 #${item.channel_id}`}
                          </span>
                        </div>
                      </button>
                    </li>
                  )
                })}
              </ul>
            </div>
          )}
        </CardContent>
      </Card>

      <Dialog open={active !== null} onOpenChange={(open) => !open && setActive(null)}>
        <DialogContent className="max-h-[85vh] overflow-hidden sm:max-w-2xl">
          {active ? (
            <>
              <DialogHeader>
                <DialogTitle className="pr-8 leading-6">{announcementTitle(active)}</DialogTitle>
              </DialogHeader>
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <Badge variant="outline">
                  {activeChannel
                    ? `${activeChannel.name} · ${channelTypeLabel(activeChannel.type)}`
                    : `渠道 #${active.channel_id}`}
                </Badge>
                {active.type ? <Badge variant="outline">{active.type}</Badge> : null}
                <span>{dateTime(active.published_at || active.first_seen_at)}</span>
              </div>
              <div className="max-h-[56vh] overflow-y-auto overscroll-contain rounded-md border border-border">
                <MarkdownContent content={active.content} />
              </div>
              {active.link ? (
                <a
                  href={active.link}
                  target="_blank"
                  rel="noreferrer"
                  className="text-xs text-brand hover:underline"
                >
                  {active.link}
                </a>
              ) : null}
            </>
          ) : null}
        </DialogContent>
      </Dialog>

      <Dialog open={detailOpen} onOpenChange={setDetailOpen}>
        <DialogContent className="max-h-[85vh] overflow-hidden sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{"上游公告"}</DialogTitle>
          </DialogHeader>
          <div
            className="max-h-[60vh] overflow-y-auto overscroll-contain rounded-md border border-border"
            onScroll={(e) => {
              const target = e.target as HTMLElement
              if (target.scrollTop + target.clientHeight >= target.scrollHeight - 32) {
                loadNextPage()
              }
            }}
          >
            <ul className="divide-y divide-border">
              {feed.map((item) => {
                const ch = channelByID.get(item.channel_id)
                const when = item.published_at || item.first_seen_at
                return (
                  <li key={item.id}>
                    <button
                      type="button"
                      className="block w-full px-4 py-3 text-left transition-colors hover:bg-muted/40 sm:px-6"
                      onClick={() => setActive(item)}
                    >
                      <div className="mb-1.5 flex min-w-0 items-start justify-between gap-3">
                        <div className="flex min-w-0 flex-1 items-start gap-2">
                          <span className="min-w-0 flex-1 truncate text-sm font-medium text-foreground">
                            {announcementTitle(item)}
                          </span>
                          {item.type ? (
                            <Badge variant="outline" className="h-5 shrink-0 px-1.5 text-[10px]">
                              {item.type}
                            </Badge>
                          ) : null}
                        </div>
                        <span className="shrink-0 text-xs text-muted-foreground">
                          {relativeTime(when)}
                        </span>
                      </div>
                      <div className="flex min-w-0">
                        <span className="truncate text-xs text-muted-foreground">
                          {ch ? `${ch.name} · ${channelTypeLabel(ch.type)}` : `渠道 #${item.channel_id}`}
                        </span>
                      </div>
                    </button>
                  </li>
                )
              })}
              {feedLoading && feed.length === 0 ? (
                <li className="px-4 py-6 text-sm text-muted-foreground">{"加载中…"}</li>
              ) : null}
              {!feedLoading && feed.length === 0 ? (
                <li className="px-4 py-6 text-sm text-muted-foreground">{"暂无上游公告"}</li>
              ) : null}
              {feedLoading && feed.length > 0 ? (
                <li className="px-4 py-3 text-xs text-muted-foreground">{"加载更多中…"}</li>
              ) : null}
            </ul>
          </div>
          {feedError ? <p className="text-sm text-danger">{feedError}</p> : null}
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <span className="text-xs text-muted-foreground">
              {feedMeta.total > 0 ? `已加载 ${feed.length} / ${feedMeta.total} 条` : ""}
            </span>
            <Button
              variant="outline"
              size="sm"
              disabled={feedLoading || page >= feedMeta.pages}
              onClick={loadNextPage}
            >
              {feedLoading && page > 1 ? "加载中…" : "查看更多"}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

const captchaTypeLabel: Record<string, string> = {
  capsolver: "CapSolver",
  "2captcha": "2Captcha",
  anticaptcha: "AntiCaptcha",
  yescaptcha: "YesCaptcha",
}

export function CaptchaStatus() {
  const { data, loading } = useCaptchaConfigs()
  const refresh = useTriggerRefresh()
  const { confirm, dialog: confirmDialog } = useConfirm()
  const [editing, setEditing] = useState<CaptchaConfig | null>(null)
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState<number | null>(null)

  async function handleRefreshBalance(c: CaptchaConfig) {
    setBusy(c.id)
    try {
      await apiFetch(`/captcha-configs/${c.id}/refresh-balance`, { method: "POST" })
      toast.success(`已更新 ${c.name} 剩余额度`)
      refresh()
    } catch (e) {
      const err = e as Error
      toast.error(err.message || "更新失败")
      refresh()
    } finally {
      setBusy(null)
    }
  }

  async function handleDelete(c: CaptchaConfig) {
    const ok = await confirm({
      title: `删除打码配置 ${c.name}？`,
      description: "删除后引用此配置的渠道将无法自动过码，需要重新指定打码 provider。",
      confirmLabel: "删除",
      destructive: true,
    })
    if (!ok) return
    setBusy(c.id)
    try {
      await apiFetch(`/captcha-configs/${c.id}`, { method: "DELETE" })
      toast.success(`已删除 ${c.name}`)
      refresh()
    } catch (e) {
      const err = e as Error
      toast.error(err.message || "删除失败")
    } finally {
      setBusy(null)
    }
  }

  return (
    <Card className="border border-border py-4 shadow-none sm:py-6">
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="text-base font-semibold">{"验证码服务"}</CardTitle>
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1 text-xs"
          onClick={() => {
            setEditing(null)
            setOpen(true)
          }}
        >
          <Plus className="size-3" />
          {"新增"}
        </Button>
      </CardHeader>
      <CardContent className="px-0">
        {loading ? (
          <p className="px-6 py-4 text-xs text-muted-foreground">{"加载中…"}</p>
        ) : !data || data.length === 0 ? (
          <p className="px-6 py-4 text-xs text-muted-foreground">{"暂未配置打码 provider"}</p>
        ) : (
          <ul className="divide-y divide-border">
            {data.map((p) => (
              <li key={p.id} className="flex items-center justify-between gap-2 px-6 py-2.5">
                <div className="flex min-w-0 items-center gap-2.5">
                  <span
                    className={cn(
                      "size-2 shrink-0 rounded-full",
                      p.enabled ? "bg-success" : "bg-muted-foreground/30",
                    )}
                  />
                  <span className="truncate text-sm font-medium text-foreground">{p.name}</span>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {captchaTypeLabel[p.type] ?? p.type}
                  </span>
                  <span className="shrink-0 text-xs font-medium text-foreground">
                    {formatCaptchaBalance(p)}
                  </span>
                  <span
                    className={cn(
                      "truncate text-xs",
                      p.balance_error ? "text-destructive" : "text-muted-foreground",
                    )}
                  >
                    {p.balance_error
                      ? p.balance_error
                      : p.balance_at
                        ? `更新于 ${relativeTime(p.balance_at)}`
                        : "未更新"}
                  </span>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <span
                    className={cn(
                      "mr-1 text-xs",
                      p.enabled ? "text-success" : "text-muted-foreground",
                    )}
                  >
                    {p.enabled ? "已启用" : "已禁用"}
                  </span>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7"
                    disabled={busy === p.id}
                    onClick={() => handleRefreshBalance(p)}
                  >
                    <RefreshCw className={cn("size-3.5", busy === p.id ? "animate-spin" : "")} />
                  </Button>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7"
                    onClick={() => {
                      setEditing(p)
                      setOpen(true)
                    }}
                  >
                    <Pencil className="size-3.5" />
                  </Button>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7 text-destructive hover:bg-destructive/10 hover:text-destructive"
                    disabled={busy === p.id}
                    onClick={() => handleDelete(p)}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>

      <CaptchaFormDialog
        open={open}
        onOpenChange={(v) => {
          setOpen(v)
          if (!v) setEditing(null)
        }}
        config={editing}
      />

      {confirmDialog}
    </Card>
  )
}

function formatCaptchaBalance(c: CaptchaConfig) {
  if (c.last_balance == null) return "余额 —"
  if (c.balance_unit === "points") return `${decimal(c.last_balance, 0)} 点`
  return money(c.last_balance, { precise: true })
}

const notifyTypeIcon: Partial<Record<NotificationChannelType, LucideIcon>> = {
  telegram: Send,
  webhook: Send,
  email: Send,
  wecom: Send,
  dingtalk: Send,
  feishu: Send,
  serverchan3: Send,
}

export function NotificationStatus() {
  const { data, loading } = useNotificationChannels()
  const logs = useNotificationLogs(1, 10)
  const refresh = useTriggerRefresh()
  const { confirm, dialog: confirmDialog } = useConfirm()
  const [editing, setEditing] = useState<NotificationChannel | null>(null)
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState<number | null>(null)

  const totalLogs = logs.data?.items ?? []
  const lastSent = totalLogs.length > 0 ? totalLogs[0] : null
  const recentFailed = totalLogs.filter((l) => !l.success).length

  async function handleDelete(c: NotificationChannel) {
    const ok = await confirm({
      title: `删除通知渠道 ${c.name}？`,
      description: "删除后系统将不再向该渠道推送告警，历史发送记录会保留以便审计。",
      confirmLabel: "删除",
      destructive: true,
    })
    if (!ok) return
    setBusy(c.id)
    try {
      await apiFetch(`/notifications/channels/${c.id}`, { method: "DELETE" })
      toast.success(`已删除 ${c.name}`)
      refresh()
    } catch (e) {
      const err = e as Error
      toast.error(err.message || "删除失败")
    } finally {
      setBusy(null)
    }
  }

  async function handleTest(c: NotificationChannel) {
    setBusy(c.id)
    try {
      const res = await apiFetch<{ ok: boolean; error?: string }>(
        `/notifications/channels/${c.id}/test`,
        { method: "POST" },
      )
      if (res.ok) {
        toast.success(`已发送测试消息到 ${c.name}`)
      } else {
        toast.error(`测试失败：${res.error ?? "未知错误"}`)
      }
      refresh()
    } catch (e) {
      const err = e as Error
      toast.error(err.message || "测试失败")
    } finally {
      setBusy(null)
    }
  }

  return (
    <Card className="border border-border py-4 shadow-none sm:py-6">
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="text-base font-semibold">{"通知渠道"}</CardTitle>
        <Button
          size="sm"
          variant="outline"
          className="h-7 gap-1 text-xs"
          onClick={() => {
            setEditing(null)
            setOpen(true)
          }}
        >
          <Plus className="size-3" />
          {"新增"}
        </Button>
      </CardHeader>
      <CardContent className="space-y-3">
        {loading ? (
          <p className="text-xs text-muted-foreground">{"加载中…"}</p>
        ) : !data || data.length === 0 ? (
          <p className="text-xs text-muted-foreground">{"暂未配置通知渠道"}</p>
        ) : (
          <ul className="divide-y divide-border rounded-lg border border-border">
            {data.map((c) => {
              const Icon = notifyTypeIcon[c.type] ?? Send
              const subCount = parseSubCount(c.subscriptions)
              return (
                <li key={c.id} className="flex items-center justify-between gap-2 px-3 py-2">
                  <div className="flex min-w-0 items-center gap-2.5">
                    <Icon className={cn("size-4 shrink-0", c.enabled ? "text-brand" : "text-muted-foreground")} />
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium text-foreground">{c.name}</p>
                      <p className="text-[11px] text-muted-foreground">
                        {c.type}
                        {" · "}
                        {subCount === 0 ? "订阅全部" : `${subCount} 条订阅`}
                        {!c.enabled ? " · 已禁用" : ""}
                      </p>
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-0.5">
                    <Button
                      size="icon"
                      variant="ghost"
                      className="h-7 w-7"
                      title="测试发送"
                      disabled={busy === c.id}
                      onClick={() => handleTest(c)}
                    >
                      <TestTube2 className="size-3.5" />
                    </Button>
                    <Button
                      size="icon"
                      variant="ghost"
                      className="h-7 w-7"
                      title="编辑"
                      onClick={() => {
                        setEditing(c)
                        setOpen(true)
                      }}
                    >
                      <Pencil className="size-3.5" />
                    </Button>
                    <Button
                      size="icon"
                      variant="ghost"
                      className="h-7 w-7 text-destructive hover:bg-destructive/10 hover:text-destructive"
                      title="删除"
                      disabled={busy === c.id}
                      onClick={() => handleDelete(c)}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                </li>
              )
            })}
          </ul>
        )}

        <div className="divide-y divide-border rounded-lg border border-border">
          <div className="flex items-center justify-between px-4 py-2.5">
            <span className="text-xs text-muted-foreground">{"上次发送"}</span>
            <span className="text-xs font-medium text-foreground">
              {lastSent ? relativeTime(lastSent.sent_at) : "—"}
            </span>
          </div>
          <div className="flex items-center justify-between px-4 py-2.5">
            <span className="text-xs text-muted-foreground">{"近 10 条失败"}</span>
            <span
              className={cn(
                "text-xs font-semibold",
                recentFailed === 0 ? "text-success" : "text-danger",
              )}
            >
              {recentFailed}
            </span>
          </div>
        </div>
      </CardContent>

      <NotificationFormDialog
        open={open}
        onOpenChange={(v) => {
          setOpen(v)
          if (!v) setEditing(null)
        }}
        channel={editing}
      />

      {confirmDialog}
    </Card>
  )
}

function parseSubCount(raw?: string): number {
  if (!raw) return 0
  try {
    const arr = JSON.parse(raw)
    return Array.isArray(arr) ? arr.length : 0
  } catch {
    return 0
  }
}

function announcementTitle(item: UpstreamAnnouncement): string {
  const title = item.title?.trim()
  if (title) return title
  const content = item.content.trim()
  if (!content) return "上游公告"
  const chars = Array.from(content)
  return chars.length > 32 ? `${chars.slice(0, 32).join("")}...` : content
}

function MarkdownContent({ content }: { content: string }) {
  const blocks = parseMarkdownBlocks(content)
  if (blocks.length === 0) {
    return <div className="p-4 text-sm leading-6 text-muted-foreground">{"—"}</div>
  }
  return (
    <div className="space-y-3 break-words p-4 text-sm leading-6 text-foreground">
      {blocks.map((block, idx) => {
        switch (block.type) {
          case "heading":
            return (
              <div key={idx} className={cn("font-semibold text-foreground", block.level === 1 ? "text-lg" : "text-base")}>
                {renderInlineMarkdown(block.text)}
              </div>
            )
          case "quote":
            return (
              <blockquote key={idx} className="border-l-2 border-border pl-3 text-muted-foreground">
                {renderInlineMarkdown(block.text)}
              </blockquote>
            )
          case "code":
            return (
              <pre key={idx} className="overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs leading-5">
                <code>{block.text}</code>
              </pre>
            )
          case "list":
            return (
              <ul key={idx} className="space-y-1 pl-5">
                {block.items.map((item, itemIdx) => (
                  <li key={itemIdx} className="list-disc">
                    {renderInlineMarkdown(item)}
                  </li>
                ))}
              </ul>
            )
          default:
            return <p key={idx}>{renderInlineMarkdown(block.text)}</p>
        }
      })}
    </div>
  )
}

type MarkdownBlock =
  | { type: "heading"; level: number; text: string }
  | { type: "quote"; text: string }
  | { type: "code"; text: string }
  | { type: "list"; items: string[] }
  | { type: "paragraph"; text: string }

function parseMarkdownBlocks(content: string): MarkdownBlock[] {
  const lines = content.replace(/\r\n/g, "\n").split("\n")
  const blocks: MarkdownBlock[] = []
  let paragraph: string[] = []
  let list: string[] = []
  let code: string[] | null = null

  const flushParagraph = () => {
    const text = paragraph.join("\n").trim()
    if (text) blocks.push({ type: "paragraph", text })
    paragraph = []
  }
  const flushList = () => {
    if (list.length > 0) blocks.push({ type: "list", items: list })
    list = []
  }

  for (const line of lines) {
    if (line.trim().startsWith("```")) {
      flushParagraph()
      flushList()
      if (code) {
        blocks.push({ type: "code", text: code.join("\n") })
        code = null
      } else {
        code = []
      }
      continue
    }
    if (code) {
      code.push(line)
      continue
    }

    const trimmed = line.trim()
    if (!trimmed) {
      flushParagraph()
      flushList()
      continue
    }

    const heading = /^(#{1,6})\s+(.+)$/.exec(trimmed)
    if (heading) {
      flushParagraph()
      flushList()
      blocks.push({ type: "heading", level: heading[1].length, text: heading[2] })
      continue
    }

    const quote = /^>\s?(.+)$/.exec(trimmed)
    if (quote) {
      flushParagraph()
      flushList()
      blocks.push({ type: "quote", text: quote[1] })
      continue
    }

    const bullet = /^[-*+]\s+(.+)$/.exec(trimmed)
    if (bullet) {
      flushParagraph()
      list.push(bullet[1])
      continue
    }

    flushList()
    paragraph.push(line)
  }
  flushParagraph()
  flushList()
  if (code && code.length > 0) blocks.push({ type: "code", text: code.join("\n") })
  return blocks
}

function renderInlineMarkdown(text: string) {
  const parts = text.split(/(\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\([^)]+\)|https?:\/\/\S+)/g)
  return parts.map((part, idx) => {
    if (!part) return null
    if (part.startsWith("**") && part.endsWith("**")) {
      return <strong key={idx}>{part.slice(2, -2)}</strong>
    }
    if (part.startsWith("`") && part.endsWith("`")) {
      return (
        <code key={idx} className="rounded bg-muted px-1 py-0.5 font-mono text-xs">
          {part.slice(1, -1)}
        </code>
      )
    }
    const link = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(part)
    if (link) {
      return (
        <a key={idx} href={link[2]} target="_blank" rel="noreferrer" className="text-brand hover:underline">
          {link[1]}
        </a>
      )
    }
    if (/^https?:\/\//.test(part)) {
      return (
        <a key={idx} href={part} target="_blank" rel="noreferrer" className="text-brand hover:underline">
          {part}
        </a>
      )
    }
    return (
      <Fragment key={idx}>
        {part.split("\n").map((line, lineIdx, arr) => (
          <Fragment key={lineIdx}>
            {line}
            {lineIdx < arr.length - 1 ? <br /> : null}
          </Fragment>
        ))}
      </Fragment>
    )
  })
}

export function BottomPanels() {
  return (
    <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
      <AlertFeed />
      <UpstreamAnnouncements />
    </div>
  )
}
