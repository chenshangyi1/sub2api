"use client"

import { useEffect, useMemo, useRef, useState, type FormEvent } from "react"
import QRCode from "qrcode"
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
import { Progress } from "@/components/ui/progress"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { apiFetch } from "@/lib/api"
import { channelTypeLabel, dateTime, decimal } from "@/lib/format"
import { useIsMobile } from "@/hooks/use-mobile"
import { cn } from "@/lib/utils"
import type {
  Channel,
  ChannelRechargeInfo,
  ChannelRechargeLaunch,
  ChannelSubscriptionInfo,
  ChannelSubscriptionLaunch,
  ChannelSubscriptionMethod,
  ChannelSubscriptionPlan,
  ChannelSubscriptionUsage,
  ChannelSubscriptionUsageInfo,
  ChannelSubscriptionUsageWindow,
  RechargePaymentMethod,
  SubscriptionPaymentMethod,
} from "@/lib/api-types"

interface ChannelRechargeDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  channel: Channel | null
}

type ActiveTab = "recharge" | "subscription"

function openFormInNewTab(action: string, fields: Record<string, string>) {
  const form = document.createElement("form")
  form.method = "POST"
  form.action = action
  form.target = "_blank"
  form.style.display = "none"
  Object.entries(fields).forEach(([key, value]) => {
    const input = document.createElement("input")
    input.type = "hidden"
    input.name = key
    input.value = value
    form.appendChild(input)
  })
  document.body.appendChild(form)
  form.submit()
  document.body.removeChild(form)
}

function paymentMethodLabel(type: string) {
  const map: Record<string, string> = {
    balance: "余额",
    alipay: "支付宝",
    wxpay: "微信",
    stripe: "Stripe",
    creem: "Creem",
    waffo_pancake: "Waffo Pancake",
  }
  return map[type] ?? type
}

function availableSubscriptionMethods(
  plan: ChannelSubscriptionPlan | null,
  methods: ChannelSubscriptionMethod[],
) {
  const allowed = plan?.payment_methods?.filter(Boolean) ?? []
  if (allowed.length === 0) return methods
  const allowedSet = new Set(allowed)
  return methods.filter((item) => allowedSet.has(item.type))
}

function normalizeStringList(value: unknown) {
  if (!Array.isArray(value)) return []
  return value.map((item) => String(item ?? "").trim()).filter(Boolean)
}

function optionalNumber(value: unknown) {
  if (value == null || value === "") return null
  const n = Number(value)
  return Number.isFinite(n) ? n : null
}

function normalizeSubscriptionInfo(value: unknown): ChannelSubscriptionInfo {
  const wrapped = value && typeof value === "object" && "data" in value
    ? (value as { data?: unknown }).data
    : value
  const root = wrapped && typeof wrapped === "object" ? wrapped as Record<string, unknown> : {}
  const plans = Array.isArray(root.plans) ? root.plans : []
  const methods = Array.isArray(root.methods) ? root.methods : []

  return {
    plans: plans.map((item) => {
      const plan = item && typeof item === "object" ? item as Record<string, unknown> : {}
      const quota = Number(plan.quota)
      return {
        id: String(plan.id ?? ""),
        name: String(plan.name ?? ""),
        description: plan.description == null ? undefined : String(plan.description),
        price: Number(plan.price) || 0,
        currency: plan.currency == null ? undefined : String(plan.currency),
        validity: plan.validity == null ? undefined : String(plan.validity),
        group_name: plan.group_name == null ? undefined : String(plan.group_name),
        quota: Number.isFinite(quota) ? quota : undefined,
        daily_limit_usd: optionalNumber(plan.daily_limit_usd),
        weekly_limit_usd: optionalNumber(plan.weekly_limit_usd),
        monthly_limit_usd: optionalNumber(plan.monthly_limit_usd),
        features: normalizeStringList(plan.features),
        payment_methods: normalizeStringList(plan.payment_methods),
      }
    }).filter((plan) => plan.id && plan.name),
    methods: methods.map((item) => {
      const method = item && typeof item === "object" ? item as Record<string, unknown> : {}
      const type = String(method.type ?? "").trim()
      return {
        type,
        name: String(method.name ?? paymentMethodLabel(type)),
      }
    }).filter((method) => method.type),
  }
}

function normalizeSubscriptionUsageInfo(value: unknown): ChannelSubscriptionUsageInfo {
  const wrapped = value && typeof value === "object" && "data" in value
    ? (value as { data?: unknown }).data
    : value
  const root = wrapped && typeof wrapped === "object" ? wrapped as Record<string, unknown> : {}
  const items = Array.isArray(root.items) ? root.items : []
  return {
    items: items.map((item) => {
      const raw = item && typeof item === "object" ? item as Record<string, unknown> : {}
      return {
        id: Number(raw.id) || 0,
        group_id: Number(raw.group_id) || 0,
        group_name: String(raw.group_name ?? ""),
        status: String(raw.status ?? ""),
        starts_at: raw.starts_at == null ? null : String(raw.starts_at),
        expires_at: raw.expires_at == null ? null : String(raw.expires_at),
        expires_in_days: Number(raw.expires_in_days) || 0,
        daily: normalizeUsageWindow(raw.daily),
        weekly: normalizeUsageWindow(raw.weekly),
        monthly: normalizeUsageWindow(raw.monthly),
      }
    }).filter((item) => item.id > 0),
  }
}

function normalizeUsageWindow(value: unknown): ChannelSubscriptionUsageWindow | null {
  if (!value || typeof value !== "object") return null
  const raw = value as Record<string, unknown>
  const limit = Number(raw.limit_usd)
  if (!Number.isFinite(limit) || limit <= 0) return null
  const used = Number(raw.used_usd) || 0
  const remaining = Number(raw.remaining_usd) || 0
  const usedPct = Math.max(0, Math.min(100, Number(raw.used_percent) || 0))
  const remainingPct = Math.max(0, Math.min(100, Number(raw.remaining_percent) || (100 - usedPct)))
  return {
    limit_usd: limit,
    used_usd: used,
    remaining_usd: remaining,
    remaining_percent: remainingPct,
    used_percent: usedPct,
    window_start: raw.window_start == null ? null : String(raw.window_start),
    resets_at: raw.resets_at == null ? null : String(raw.resets_at),
    resets_in_seconds: Number(raw.resets_in_seconds) || 0,
  }
}

function formatPlanPrice(plan: ChannelSubscriptionPlan) {
  const price = decimal(plan.price)
  return plan.currency ? `${plan.currency} ${price}` : price
}

function usageWindowItems(item: ChannelSubscriptionUsage) {
  return [
    { label: "今日", value: item.daily },
    { label: "本周", value: item.weekly },
    { label: "本月", value: item.monthly },
  ].filter((entry): entry is { label: string; value: ChannelSubscriptionUsageWindow } => !!entry.value && entry.value.limit_usd > 0)
}

function subscriptionStatusLabel(status: string) {
  const map: Record<string, string> = {
    active: "生效中",
    expired: "已过期",
    revoked: "已撤销",
  }
  return map[status] ?? (status || "未知")
}

function planLimitItems(plan: ChannelSubscriptionPlan) {
  return [
    { label: "日限制", value: plan.daily_limit_usd },
    { label: "周限制", value: plan.weekly_limit_usd },
    { label: "月限制", value: plan.monthly_limit_usd },
  ].filter((item) => item.value != null && item.value > 0)
}

export function ChannelRechargeDialog({
  open,
  onOpenChange,
  channel,
}: ChannelRechargeDialogProps) {
  const isMobile = useIsMobile()
  const subscriptionEnabled = channel?.type === "sub2api" && !!channel?.subscription_enabled
  const channelID = channel?.id ?? null
  const [activeTab, setActiveTab] = useState<ActiveTab>("recharge")
  const [loading, setLoading] = useState(false)
  const [subLoading, setSubLoading] = useState(false)
  const [usageLoading, setUsageLoading] = useState(false)
  const [subscriptionReloadTick, setSubscriptionReloadTick] = useState(0)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [info, setInfo] = useState<ChannelRechargeInfo | null>(null)
  const [subscriptionInfo, setSubscriptionInfo] = useState<ChannelSubscriptionInfo | null>(null)
  const [subscriptionUsage, setSubscriptionUsage] = useState<ChannelSubscriptionUsageInfo | null>(null)
  const [subscriptionUsageError, setSubscriptionUsageError] = useState<string | null>(null)
  const [amount, setAmount] = useState("")
  const [method, setMethod] = useState<RechargePaymentMethod | "">("")
  const [subscriptionPlanID, setSubscriptionPlanID] = useState("")
  const [subscriptionMethod, setSubscriptionMethod] = useState<SubscriptionPaymentMethod | "">("")
  const [launch, setLaunch] = useState<ChannelRechargeLaunch | ChannelSubscriptionLaunch | null>(null)
  const [launchMethod, setLaunchMethod] = useState("")
  const qrCanvasRef = useRef<HTMLCanvasElement | null>(null)

  useEffect(() => {
    if (!open || channelID == null) return
    let cancelled = false
    setActiveTab("recharge")
    setLoading(true)
    setSubLoading(false)
    setUsageLoading(false)
    setError(null)
    setInfo(null)
    setSubscriptionInfo(null)
    setSubscriptionUsage(null)
    setSubscriptionUsageError(null)
    setLaunch(null)
    setLaunchMethod("")
    setAmount("")
    setMethod("")
    setSubscriptionPlanID("")
    setSubscriptionMethod("")
    apiFetch<ChannelRechargeInfo>(`/channels/${channelID}/recharge-info`)
      .then((data) => {
        if (cancelled) return
        setInfo(data)
        const methods = data.methods ?? []
        const presetAmounts = data.preset_amounts ?? []
        setMethod(methods[0]?.type ?? "")
        if (presetAmounts[0] != null) {
          setAmount(String(presetAmounts[0]))
        } else if (data.min_amount > 0) {
          setAmount(String(data.min_amount))
        }
      })
      .catch((e) => {
        if (cancelled) return
        const err = e as Error
        setError(err.message || "加载充值信息失败")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [open, channelID])

  useEffect(() => {
    if (!open || channelID == null || !subscriptionEnabled || activeTab !== "subscription") {
      setSubLoading(false)
      setUsageLoading(false)
      return
    }
    let cancelled = false
    setSubLoading(true)
    setUsageLoading(true)
    setError(null)
    setSubscriptionUsageError(null)
    Promise.allSettled([
      apiFetch<unknown>(`/channels/${channelID}/subscription-info`),
      apiFetch<unknown>(`/channels/${channelID}/subscription-usage`),
    ])
      .then(([infoResult, usageResult]) => {
        if (cancelled) return
        if (infoResult.status === "fulfilled") {
          const data = normalizeSubscriptionInfo(infoResult.value)
          setSubscriptionInfo(data)
          const plans = data.plans ?? []
          const firstPlan = plans[0] ?? null
          setSubscriptionPlanID(firstPlan?.id ?? "")
          const methods = availableSubscriptionMethods(firstPlan, data.methods ?? [])
          setSubscriptionMethod(methods[0]?.type ?? "")
        } else {
          const err = infoResult.reason as Error
          setError(err.message || "加载订阅套餐失败")
        }
        if (usageResult.status === "fulfilled") {
          setSubscriptionUsage(normalizeSubscriptionUsageInfo(usageResult.value))
        } else {
          const err = usageResult.reason as Error
          setSubscriptionUsageError(err.message || "加载订阅用量失败")
        }
      })
      .finally(() => {
        if (!cancelled) {
          setSubLoading(false)
          setUsageLoading(false)
        }
      })
    return () => {
      cancelled = true
    }
  }, [open, channelID, subscriptionEnabled, activeTab, subscriptionReloadTick])

  useEffect(() => {
    if (!launch || launch.mode !== "qrcode" || !launch.qr_code || !qrCanvasRef.current) {
      return
    }
    void QRCode.toCanvas(qrCanvasRef.current, launch.qr_code, {
      width: 220,
      margin: 1,
    })
  }, [launch])

  const parsedAmount = Number(amount)
  const selectedMethod = useMemo(
    () => info?.methods?.find((item) => item.type === method) ?? null,
    [info, method],
  )
  const canSubmit = !!info && !!selectedMethod && Number.isFinite(parsedAmount) && parsedAmount > 0
  const selectedSubscriptionPlan = useMemo(
    () => subscriptionInfo?.plans?.find((item) => item.id === subscriptionPlanID) ?? null,
    [subscriptionInfo, subscriptionPlanID],
  )
  const subscriptionMethods = useMemo(
    () => availableSubscriptionMethods(selectedSubscriptionPlan, subscriptionInfo?.methods ?? []),
    [selectedSubscriptionPlan, subscriptionInfo],
  )
  const selectedSubscriptionMethod = useMemo(
    () => subscriptionMethods.find((item) => item.type === subscriptionMethod) ?? null,
    [subscriptionMethods, subscriptionMethod],
  )
  const canSubmitSubscription = !!selectedSubscriptionPlan && !!selectedSubscriptionMethod

  useEffect(() => {
    if (!open || activeTab !== "subscription") return
    if (!subscriptionMethods.some((item) => item.type === subscriptionMethod)) {
      setSubscriptionMethod(subscriptionMethods[0]?.type ?? "")
    }
  }, [open, activeTab, subscriptionMethods, subscriptionMethod])

  async function handleSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!channel || !selectedMethod) return
    setSubmitting(true)
    setError(null)
    try {
      const result = await apiFetch<ChannelRechargeLaunch>(`/channels/${channel.id}/recharge`, {
        method: "POST",
        body: JSON.stringify({
          amount: parsedAmount,
          payment_method: selectedMethod.type,
          is_mobile: info?.alipay_force_qrcode && selectedMethod.type === "alipay" ? false : isMobile,
        }),
      })
      if (result.mode === "redirect" && result.pay_url) {
        window.open(result.pay_url, "_blank", "noopener,noreferrer")
        onOpenChange(false)
        return
      }
      if (result.mode === "form" && result.form_action && result.form_fields) {
        openFormInNewTab(result.form_action, result.form_fields)
        onOpenChange(false)
        return
      }
      if (result.mode === "qrcode" && result.qr_code) {
        setLaunch(result)
        setLaunchMethod(selectedMethod.type)
        return
      }
      throw new Error("上游返回了不完整的支付结果")
    } catch (e) {
      const err = e as Error
      setError(err.message || "发起充值失败")
    } finally {
      setSubmitting(false)
    }
  }

  async function handleSubscriptionSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!channel || !selectedSubscriptionPlan || !selectedSubscriptionMethod) return
    setSubmitting(true)
    setError(null)
    try {
      const result = await apiFetch<ChannelSubscriptionLaunch>(`/channels/${channel.id}/subscription`, {
        method: "POST",
        body: JSON.stringify({
          plan_id: selectedSubscriptionPlan.id,
          payment_method: selectedSubscriptionMethod.type,
          is_mobile: isMobile,
        }),
      })
      if (result.mode === "redirect" && result.pay_url) {
        window.open(result.pay_url, "_blank", "noopener,noreferrer")
        onOpenChange(false)
        return
      }
      if (result.mode === "form" && result.form_action && result.form_fields) {
        openFormInNewTab(result.form_action, result.form_fields)
        onOpenChange(false)
        return
      }
      if (result.mode === "qrcode" && result.qr_code) {
        setLaunch(result)
        setLaunchMethod(selectedSubscriptionMethod.type)
        return
      }
      if (result.mode === "success") {
        setLaunch(result)
        setLaunchMethod(selectedSubscriptionMethod.type)
        return
      }
      throw new Error("上游返回了不完整的支付结果")
    } catch (e) {
      const err = e as Error
      setError(err.message || "发起订阅购买失败")
    } finally {
      setSubmitting(false)
    }
  }

  function renderRechargeForm() {
    if (loading) {
      return <p className="text-sm text-muted-foreground">加载中…</p>
    }
    return (
      <form onSubmit={handleSubmit} className="space-y-4">
        <div className="space-y-1.5">
          <Label htmlFor="recharge-amount">{info?.amount_label ?? "充值金额"}</Label>
          <Input
            id="recharge-amount"
            inputMode="decimal"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            placeholder={info ? `最低 ${decimal(info.min_amount)}` : "请输入"}
            disabled={submitting}
          />
          {info ? (
            <p className="text-[11px] text-muted-foreground">
              {`最小 ${decimal(info.min_amount)}${info.max_amount > 0 ? `，最大 ${decimal(info.max_amount)}` : ""}`}
            </p>
          ) : null}
        </div>

        {info?.preset_amounts?.length ? (
          <div className="space-y-1.5">
            <Label>快捷金额</Label>
            <div className="flex flex-wrap gap-2">
              {info.preset_amounts.map((value) => (
                <Button
                  key={value}
                  type="button"
                  size="sm"
                  variant={amount === String(value) ? "default" : "outline"}
                  onClick={() => setAmount(String(value))}
                  disabled={submitting}
                >
                  {decimal(value)}
                </Button>
              ))}
            </div>
          </div>
        ) : null}

        <div className="space-y-1.5">
          <Label>支付方式</Label>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            {info?.methods?.map((item) => (
              <Button
                key={item.type}
                type="button"
                variant={method === item.type ? "default" : "outline"}
                onClick={() => setMethod(item.type)}
                disabled={submitting}
                className="justify-start"
              >
                {item.name}
              </Button>
            ))}
          </div>
        </div>

        {info?.help_text ? (
          <div className="rounded-lg border border-border bg-muted/20 px-3 py-2 text-xs text-muted-foreground whitespace-pre-wrap">
            {info.help_text}
          </div>
        ) : null}

        {info?.help_image_url ? (
          <div className="overflow-hidden rounded-lg border border-border">
            <img src={info.help_image_url} alt="充值说明" className="max-h-48 w-full object-contain bg-white" />
          </div>
        ) : null}

        {error ? (
          <p className="text-sm text-destructive" role="alert">
            {error}
          </p>
        ) : null}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            取消
          </Button>
          <Button type="submit" disabled={!canSubmit || submitting}>
            {submitting ? "发起中…" : "立即充值"}
          </Button>
        </DialogFooter>
      </form>
    )
  }

  function renderSubscriptionUsage() {
    if (usageLoading) {
      return (
        <div className="rounded-lg border border-border bg-muted/20 px-3 py-3 text-sm text-muted-foreground">
          加载订阅用量中…
        </div>
      )
    }
    if (subscriptionUsageError) {
      return (
        <div className="rounded-lg border border-border bg-muted/20 px-3 py-3 text-sm text-muted-foreground">
          {subscriptionUsageError}
        </div>
      )
    }
    const items = subscriptionUsage?.items ?? []
    if (!items.length) {
      return (
        <div className="rounded-lg border border-border bg-muted/20 px-3 py-3 text-sm text-muted-foreground">
          暂无生效中的订阅。
        </div>
      )
    }
    return (
      <div className="space-y-2">
        <Label>当前订阅用量</Label>
        <div className="max-h-56 space-y-2 overflow-y-auto pr-1">
          {items.map((item) => {
            const windows = usageWindowItems(item)
            return (
              <div key={item.id} className="rounded-lg border border-border bg-muted/20 px-3 py-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">
                      {item.group_name || `分组 ${item.group_id || item.id}`}
                    </p>
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      {subscriptionStatusLabel(item.status)}
                      {item.expires_at ? ` · 到期 ${dateTime(item.expires_at)}` : ""}
                    </p>
                  </div>
                  {item.expires_in_days > 0 ? (
                    <span className="shrink-0 rounded-md bg-background px-2 py-1 text-xs text-muted-foreground">
                      剩 {item.expires_in_days} 天
                    </span>
                  ) : null}
                </div>
                {windows.length ? (
                  <div className="mt-3 space-y-3">
                    {windows.map(({ label, value }) => (
                      <div key={label} className="space-y-1">
                        <div className="flex items-center justify-between gap-2 text-xs">
                          <span className="text-muted-foreground">{label}</span>
                          <span className="text-foreground">
                            ${decimal(value.used_usd, 2)} / ${decimal(value.limit_usd, 2)}
                          </span>
                        </div>
                        <Progress value={value.used_percent} className="h-1.5" />
                        <div className="flex items-center justify-between gap-2 text-[11px] text-muted-foreground">
                          <span>剩余 ${decimal(value.remaining_usd, 2)}（{decimal(value.remaining_percent, 1)}%）</span>
                          {value.resets_at ? <span>重置 {dateTime(value.resets_at)}</span> : null}
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className="mt-3 text-xs text-muted-foreground">该订阅暂无额度限制。</p>
                )}
              </div>
            )
          })}
        </div>
      </div>
    )
  }

  function renderSubscriptionForm() {
    if (subLoading) {
      return <p className="text-sm text-muted-foreground">加载套餐中…</p>
    }
    if (!subscriptionInfo) {
      return (
        <div className="space-y-3">
          {error ? (
            <p className="text-sm text-destructive" role="alert">
              {error}
            </p>
          ) : null}
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              setSubscriptionInfo(null)
              setSubscriptionReloadTick((v) => v + 1)
            }}
          >
            重新加载
          </Button>
        </div>
      )
    }
    return (
      <form onSubmit={handleSubscriptionSubmit} className="space-y-4">
        {renderSubscriptionUsage()}

        <div className="space-y-1.5">
          <Label>订阅套餐</Label>
          {subscriptionInfo.plans?.length ? (
            <div className="max-h-64 space-y-2 overflow-y-auto pr-1">
              {subscriptionInfo.plans.map((plan) => {
                const details = [plan.validity, plan.group_name].filter(Boolean).join(" · ")
                const limits = planLimitItems(plan)
                return (
                  <button
                    key={plan.id}
                    type="button"
                    onClick={() => setSubscriptionPlanID(plan.id)}
                    disabled={submitting}
                    className={cn(
                      "w-full rounded-lg border px-3 py-2 text-left transition-colors",
                      subscriptionPlanID === plan.id
                        ? "border-foreground bg-foreground text-background"
                        : "border-border hover:bg-muted/50",
                    )}
                  >
                    <span className="flex items-start justify-between gap-3">
                      <span className="min-w-0">
                        <span className="block truncate text-sm font-medium">{plan.name}</span>
                        {details ? (
                          <span className="mt-0.5 block text-xs opacity-75">{details}</span>
                        ) : null}
                      </span>
                      <span className="shrink-0 text-sm font-medium">{formatPlanPrice(plan)}</span>
                    </span>
                    {plan.description ? (
                      <span className="mt-1 block whitespace-pre-wrap break-words text-xs opacity-75">{plan.description}</span>
                    ) : null}
                    {limits.length ? (
                      <span className="mt-2 grid grid-cols-1 gap-1.5 sm:grid-cols-3">
                        {limits.map((item) => (
                          <span key={item.label} className="rounded-md bg-muted/50 px-2 py-1">
                            <span className="block text-[10px] opacity-60">{item.label}</span>
                            <span className="block text-xs font-medium">${decimal(item.value, 2)}</span>
                          </span>
                        ))}
                      </span>
                    ) : (
                      <span className="mt-2 inline-flex rounded-md bg-muted/50 px-2 py-1 text-xs opacity-75">
                        使用限制：不限
                      </span>
                    )}
                    {plan.features?.length ? (
                      <span className="mt-1 block whitespace-pre-wrap break-words text-xs opacity-75">
                        {plan.features.slice(0, 3).join(" / ")}
                      </span>
                    ) : null}
                  </button>
                )
              })}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">上游暂无可购买套餐</p>
          )}
        </div>

        <div className="space-y-1.5">
          <Label>支付方式</Label>
          {subscriptionMethods.length ? (
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {subscriptionMethods.map((item) => (
                <Button
                  key={item.type}
                  type="button"
                  variant={subscriptionMethod === item.type ? "default" : "outline"}
                  onClick={() => setSubscriptionMethod(item.type)}
                  disabled={submitting}
                  className="justify-start"
                >
                  {item.name || paymentMethodLabel(item.type)}
                </Button>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">当前套餐暂无可用支付方式</p>
          )}
        </div>

        {error ? (
          <p className="text-sm text-destructive" role="alert">
            {error}
          </p>
        ) : null}

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            取消
          </Button>
          <Button type="submit" disabled={!canSubmitSubscription || submitting}>
            {submitting ? "发起中…" : "购买订阅"}
          </Button>
        </DialogFooter>
      </form>
    )
  }

  const description = channel
    ? `${channel.name} · ${channelTypeLabel(channel.type)}`
    : "选择充值方式后发起支付。"

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={cn("sm:max-w-md", subscriptionEnabled && "sm:max-w-lg")}>
        <DialogHeader>
          <DialogTitle>{subscriptionEnabled ? "充值 / 订阅" : "充值"}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>

        {launch?.mode === "qrcode" ? (
          <div className="space-y-3">
            <div className="rounded-lg border border-border bg-muted/20 p-4">
              <div className="flex justify-center">
                <canvas ref={qrCanvasRef} className="rounded bg-white p-2" />
              </div>
              <p className="mt-3 text-center text-sm text-foreground">
                请使用{paymentMethodLabel(launchMethod)}扫码支付
              </p>
              {launch.expires_at ? (
                <p className="mt-1 text-center text-xs text-muted-foreground">
                  过期时间：{dateTime(launch.expires_at)}
                </p>
              ) : null}
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setLaunch(null)}>
                返回
              </Button>
              <Button type="button" onClick={() => onOpenChange(false)}>
                关闭
              </Button>
            </DialogFooter>
          </div>
        ) : launch?.mode === "success" ? (
          <div className="space-y-3">
            <div className="rounded-lg border border-border bg-muted/20 px-3 py-3 text-sm">
              订阅购买已完成。
            </div>
            <DialogFooter>
              <Button type="button" onClick={() => onOpenChange(false)}>
                关闭
              </Button>
            </DialogFooter>
          </div>
        ) : subscriptionEnabled ? (
          <Tabs
            value={activeTab}
            onValueChange={(value) => {
              setActiveTab(value as ActiveTab)
              setError(null)
            }}
            className="gap-4"
          >
            <TabsList className="grid w-full grid-cols-2">
              <TabsTrigger value="recharge">充值</TabsTrigger>
              <TabsTrigger value="subscription">订阅</TabsTrigger>
            </TabsList>
            <TabsContent value="recharge" className="mt-0">
              {renderRechargeForm()}
            </TabsContent>
            <TabsContent value="subscription" className="mt-0">
              {renderSubscriptionForm()}
            </TabsContent>
          </Tabs>
        ) : (
          renderRechargeForm()
        )}
      </DialogContent>
    </Dialog>
  )
}
