import { useEffect, useState } from "react"
import { Save, Send, SlidersHorizontal } from "lucide-react"
import { toast } from "sonner"
import {
  OperationsError,
  OperationsLoading,
  OperationsPageHeader,
} from "@/components/operations/operations-layout"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { useOperationsTarget } from "@/hooks/use-operations-target"
import {
  getOperationsSettings,
  getTargetOperationsSettings,
  saveOperationsSettings,
  saveTargetOperationsSettings,
  testTargetBalanceWebhook,
  type OperationsSettings,
  type TargetOperationsSettings,
} from "@/lib/operations-api"

const emptySettings: TargetOperationsSettings = {
  account_balance_alert_enabled: false,
  account_balance_default_threshold: 0,
  account_balance_cooldown_minutes: 60,
  account_balance_webhook_url: "",
  account_balance_webhook_template: "",
  suppress_native_monitors: true,
}

const emptyGlobalSettings: OperationsSettings = {
  heavy_probe_interval_minutes: 60,
}

export function TargetSettings() {
  const target = useOperationsTarget()
  const [form, setForm] = useState<TargetOperationsSettings | null>(null)
  const [globalForm, setGlobalForm] = useState<OperationsSettings | null>(null)
  const [loading, setLoading] = useState(false)
  const [globalLoading, setGlobalLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [globalError, setGlobalError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [savingGlobal, setSavingGlobal] = useState(false)
  const [testing, setTesting] = useState(false)
  const [reload, setReload] = useState(0)

  useEffect(() => {
    let cancelled = false
    setGlobalLoading(true)
    setGlobalError(null)
    getOperationsSettings()
      .then((result) => {
        if (!cancelled) setGlobalForm({ ...emptyGlobalSettings, ...result })
      })
      .catch((reason: unknown) => {
        if (!cancelled) setGlobalError(reason instanceof Error ? reason.message : "全局运营设置加载失败")
      })
      .finally(() => {
        if (!cancelled) setGlobalLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [reload])

  useEffect(() => {
    if (target.selectedTargetID == null) {
      setForm(null)
      return
    }
    let cancelled = false
    setLoading(true)
    setError(null)
    getTargetOperationsSettings(target.selectedTargetID)
      .then((result) => {
        if (!cancelled) setForm({ ...emptySettings, ...result })
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : "目标设置加载失败")
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [reload, target.selectedTargetID])

  function patch<K extends keyof TargetOperationsSettings>(key: K, value: TargetOperationsSettings[K]) {
    setForm((current) => (current ? { ...current, [key]: value } : current))
  }

  async function save() {
    if (!form || target.selectedTargetID == null) return
    setSaving(true)
    try {
      const saved = await saveTargetOperationsSettings(target.selectedTargetID, form)
      setForm(saved)
      toast.success("目标运营设置已保存")
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "目标设置保存失败")
    } finally {
      setSaving(false)
    }
  }

  async function saveGlobal() {
    if (!globalForm) return
    setSavingGlobal(true)
    try {
      const saved = await saveOperationsSettings(globalForm)
      setGlobalForm(saved)
      toast.success("全局探测设置已保存")
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "全局探测设置保存失败")
    } finally {
      setSavingGlobal(false)
    }
  }

  async function testWebhook() {
    if (target.selectedTargetID == null) return
    setTesting(true)
    try {
      await testTargetBalanceWebhook(target.selectedTargetID)
      toast.success("测试通知已发送")
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : "测试通知发送失败")
    } finally {
      setTesting(false)
    }
  }

  return (
    <div className="space-y-5">
      <OperationsPageHeader
        icon={SlidersHorizontal}
        title="目标服务"
        description="仅管理目标账号告警与增强探测边界；目标连接、源渠道和标准同步继续由上游动态同步负责。"
        targets={target.targets}
        selectedTargetID={target.selectedTargetID}
        targetLoading={target.loading}
        onTargetChange={target.selectTarget}
        refreshing={loading}
        onRefresh={() => setReload((value) => value + 1)}
        actions={
          <Button size="sm" onClick={() => void save()} disabled={!form || saving}>
            <Save className="size-3.5" />
            保存
          </Button>
        }
      />

      {target.error ? <OperationsError title="目标站点加载失败" message={target.error} /> : null}
      {globalError ? <OperationsError title="全局运营设置加载失败" message={globalError} /> : null}
      {error ? <OperationsError message={error} /> : null}
      {loading && !form ? <OperationsLoading rows={4} /> : null}

      <section className="space-y-4 rounded-md border border-border bg-background p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold text-foreground">全局增强探测</h3>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">重量检测周期由所有目标共同使用，不随上方目标站点切换。</p>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => void saveGlobal()}
            disabled={!globalForm || savingGlobal}
          >
            <Save className="size-3.5" />
            保存全局设置
          </Button>
        </div>
        {globalLoading && !globalForm ? (
          <OperationsLoading rows={1} />
        ) : globalForm ? (
          <div className="max-w-sm">
            <SettingField label="重量检测间隔（分钟）" htmlFor="heavy-probe-interval">
              <Input
                id="heavy-probe-interval"
                type="number"
                min={5}
                max={10080}
                value={globalForm.heavy_probe_interval_minutes}
                onChange={(event) => setGlobalForm({
                  ...globalForm,
                  heavy_probe_interval_minutes: Number(event.target.value),
                })}
              />
            </SettingField>
          </div>
        ) : null}
      </section>

      {form ? (
        <div className="grid gap-5 xl:grid-cols-2">
          <section className="space-y-5 rounded-md border border-border bg-background p-4">
            <div>
              <h3 className="text-sm font-semibold text-foreground">账号余额告警</h3>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">账号独立阈值优先；未设置时使用默认阈值，并通过扩展 Worker 发送通知。</p>
            </div>
            <SettingSwitch
              id="target-balance-alert"
              label="启用目标账号余额告警"
              description="按冷却时间合并重复告警。"
              checked={form.account_balance_alert_enabled}
              onCheckedChange={(value) => patch("account_balance_alert_enabled", value)}
            />
            <div className="grid gap-4 sm:grid-cols-2">
              <SettingField label="默认余额阈值" htmlFor="target-balance-threshold">
                <Input
                  id="target-balance-threshold"
                  type="number"
                  min={0}
                  step="0.01"
                  value={form.account_balance_default_threshold}
                  onChange={(event) => patch("account_balance_default_threshold", Number(event.target.value))}
                />
              </SettingField>
              <SettingField label="通知冷却（分钟）" htmlFor="target-balance-cooldown">
                <Input
                  id="target-balance-cooldown"
                  type="number"
                  min={0}
                  max={43200}
                  value={form.account_balance_cooldown_minutes}
                  onChange={(event) => patch("account_balance_cooldown_minutes", Number(event.target.value))}
                />
              </SettingField>
            </div>
            <SettingField label="Webhook URL" htmlFor="target-balance-webhook">
              <Input
                id="target-balance-webhook"
                type="url"
                value={form.account_balance_webhook_url}
                onChange={(event) => patch("account_balance_webhook_url", event.target.value)}
                placeholder="https://..."
              />
            </SettingField>
            <SettingField label="通知模板" htmlFor="target-balance-template">
              <Textarea
                id="target-balance-template"
                rows={6}
                value={form.account_balance_webhook_template}
                onChange={(event) => patch("account_balance_webhook_template", event.target.value)}
                placeholder="支持账号、余额、阈值和目标站点变量"
              />
            </SettingField>
            <Button variant="outline" size="sm" onClick={() => void testWebhook()} disabled={testing || !form.account_balance_webhook_url}>
              <Send className="size-3.5" />
              测试已保存配置
            </Button>
          </section>

          <section className="space-y-5 rounded-md border border-border bg-background p-4">
            <div>
              <h3 className="text-sm font-semibold text-foreground">增强探测边界</h3>
              <p className="mt-1 text-xs leading-5 text-muted-foreground">避免原生监控与扩展探测同时写入账号状态；重量检测遵循上方全局周期。</p>
            </div>
            <SettingSwitch
              id="suppress-native-monitors"
              label="抑制目标原生监控"
              description="仅对已由增强探测托管的账号生效。"
              checked={form.suppress_native_monitors}
              onCheckedChange={(value) => patch("suppress_native_monitors", value)}
            />
            <div className="rounded-md border border-border bg-muted/25 px-4 py-3 text-xs leading-5 text-muted-foreground">
              轻量检测只验证凭据和基础响应；重量检测执行真实流式请求并记录 TTFT、TPS、能力矩阵与七日可用性。
            </div>
          </section>
        </div>
      ) : null}
    </div>
  )
}

function SettingField({ label, htmlFor, children }: { label: string; htmlFor: string; children: React.ReactNode }) {
  return <div className="space-y-2"><Label htmlFor={htmlFor}>{label}</Label>{children}</div>
}

function SettingSwitch({
  id,
  label,
  description,
  checked,
  onCheckedChange,
}: {
  id: string
  label: string
  description: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <div className="flex items-start justify-between gap-4 border-y border-border py-3">
      <div><Label htmlFor={id}>{label}</Label><p className="mt-1 text-xs text-muted-foreground">{description}</p></div>
      <Switch id={id} checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  )
}
