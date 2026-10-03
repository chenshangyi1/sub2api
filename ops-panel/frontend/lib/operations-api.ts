import { apiFetch } from "@/lib/api"
import type { UpstreamSyncTarget } from "@/lib/api-types"

export type OperationsStatus = "healthy" | "warning" | "error" | "unknown"
export type TargetAccountScheduleFilter = "all" | "enabled" | "disabled" | "errors" | "temporary_unavailable"
export type TargetProbeMode = "light" | "heavy" | "capability"
export type TargetProbeRunMode = "light" | "heavy"

export type RatePolicyTargetType = "group" | "account"
export type RatePolicyMode = "first" | "average" | "min" | "max" | "custom" | "locked" | "manual_source"

export interface RatePolicySource {
  channel_id: number
  channel_name: string
  channel_type: string
  group_id: string
  group_name: string
  rate: number
  fresh: boolean
  enabled: boolean
  last_status: string
  last_error?: string
}

export interface RatePolicyBinding {
  id?: number
  channel_id: number
  channel_name: string
  group_id: string
  group_name: string
  source_platform?: string
}

export interface RatePolicyRule {
  enabled: boolean
  mode: RatePolicyMode
  offset: number
  expression?: string
}

export interface RatePolicyTarget {
  target_type: RatePolicyTargetType
  id: number
  name: string
  current_rate: number
  bindings: RatePolicyBinding[]
  rule: RatePolicyRule
}

export interface RatePolicyWorkspace {
  sources: RatePolicySource[]
  groups: RatePolicyTarget[]
  accounts: RatePolicyTarget[]
  exclusions: RatePolicyExclusion[]
}

export interface RatePolicyExclusion {
  target_type: RatePolicyTargetType
  target_id: number
  channel_id: number
  group_id: string
}

export interface RatePolicyInput {
  enabled: boolean
  mode: RatePolicyMode
  offset: number
  expression?: string
  bindings: Array<{ channel_id: number; group_id: string }>
}

export interface ImportAPIKeyInput {
  name: string
  platform: string
  type?: string
  base_url: string
  api_key: string
  source_channel_id: number
  source_group_id?: string
  source_group_name?: string
  target_group_ids?: number[]
  rate_policy?: RatePolicyInput
}

export interface ImportAPIKeyResult {
  account: TargetAccount
  created: boolean
  rate_policy?: RatePolicyTarget
}

export interface TargetGroup {
  id: number
  name: string
  platform?: string
  ratio?: number
  rate_multiplier?: number
  status?: string
  description?: string
}

export interface TargetAccount {
  id: number
  name: string
  platform?: string
  type?: string
  status: string
  schedulable: boolean
  concurrency: number
  priority: number
  load_factor?: number | null
  rate_multiplier: number
  group_names?: string[]
  health_score?: number | null
  health_state?: string
  health_weight?: number
  balance?: number | null
  balance_currency?: string
  balance_threshold?: number | null
  expires_at?: string | null
  temporary_unavailable: boolean
  temporary_unavailable_until?: string | null
  temporary_unavailable_reason?: string
  last_error?: string
  updated_at?: string | null
}

export interface TargetAccountSummary {
  total: number
  schedulable: number
  errors: number
  balance_low: number
  temporary_unavailable: number
}

export interface TargetAccountPage {
  items: TargetAccount[]
  summary: TargetAccountSummary
  total: number
  page: number
  page_size: number
  pages: number
}

export interface TargetProbe {
  id: number
  name: string
  account_id?: number | null
  group_name?: string
  provider: string
  endpoint: string
  enabled: boolean
  mode: TargetProbeMode
  status: OperationsStatus
  model?: string
  candidate_models?: string[]
  latency_ms?: number | null
  first_token_ms?: number | null
  tokens_per_second?: number | null
  availability_7d?: number | null
  capability_passed?: number
  capability_total?: number
  last_checked_at?: string | null
  next_run_at?: string | null
  last_error?: string
}

export interface TargetProbePage {
  items: TargetProbe[]
  summary: {
    total: number
    healthy: number
    warning: number
    error: number
  }
}

export interface TargetProbeInput {
  account_id: number
  model_id?: string
  prompt?: string
  group_name?: string
  enabled?: boolean
  check_interval_minutes?: number
  failure_threshold?: number
  pause_minutes?: number
}

export interface TargetAnalyticsPeriod {
  key: string
  label: string
  start_at: string
  end_at: string
  user_cost: number
  upstream_cost: number
  administrator_cost: number
  profit: number
  profit_margin: number
  requests: number
  active_users: number
  stream_requests: number
  total_tokens: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_creation_tokens: number
  cache_hit_rate: number
  average_first_token_ms: number
  p95_first_token_ms: number
  slow_first_token_rate: number
}

export interface TargetAnalyticsDay extends TargetAnalyticsPeriod {
  date: string
}

export interface TargetAnalyticsHeatmapCell {
  date: string
  hour: number
  requests: number
  failures: number
  average_first_token_ms: number
}

export interface TargetSlowRequest {
  id: number | string
  created_at: string
  user_id?: number
  user_name?: string
  account_id?: number
  model: string
  stream: boolean
  duration_ms: number
  first_token_ms?: number | null
  status_code: number
  error?: string
}

export interface TargetAnalytics {
  range: "day" | "week" | "month"
  summary: TargetAnalyticsPeriod
  daily: TargetAnalyticsDay[]
  heatmap: TargetAnalyticsHeatmapCell[]
  slow_requests: TargetSlowRequest[]
}

export interface OperationsServiceStatus {
  id: string
  name: string
  status: OperationsStatus
  detail?: string
  checked_at?: string | null
  restart_count?: number
}

export interface OperationsTaskStatus {
  id: string
  name: string
  status: "running" | "success" | "failed" | "idle"
  started_at?: string | null
  finished_at?: string | null
  message?: string
}

export interface OperationsDiagnosticLog {
  id: number | string
  created_at: string
  action: string
  target?: string
  status: "success" | "failed"
  message?: string
}

export interface OperationsDiagnostics {
  services: OperationsServiceStatus[]
  connections: OperationsConnectionStatus[]
  worker: {
    status: OperationsStatus
    heartbeat_at?: string | null
    last_run_at?: string | null
    last_run_status?: string
    last_run_message?: string
  }
  tasks: OperationsTaskStatus[]
  recent_logs: OperationsDiagnosticLog[]
  invalid_data: {
    bindings: number
    managed_accounts: number
    probe_rules: number
  }
}

export interface OperationsConnectionStatus {
  key: string
  name: string
  ok: boolean
  detail: string
}

export interface TargetOperationsSettings {
  account_balance_alert_enabled: boolean
  account_balance_default_threshold: number
  account_balance_cooldown_minutes: number
  account_balance_webhook_url: string
  account_balance_webhook_template: string
  suppress_native_monitors: boolean
}

export interface OperationsSettings {
  heavy_probe_interval_minutes: number
}

export interface AccountPoolPolicy {
  health_return_enabled: boolean
  health_return_threshold: number
  smart_expansion_enabled: boolean
  total_concurrency: number
  min_account_concurrency: number
  max_account_concurrency: number
  expansion_load_threshold_pct: number
  load_factor_enabled: boolean
  total_load_factor: number
  min_account_load_factor: number
  max_account_load_factor: number
  price_protection_enabled: boolean
  failure_disable_enabled: boolean
  failure_window: number
  failure_count: number
  slow_window: number
  slow_first_token_ms: number
  slow_count: number
  min_available_accounts: number
  target_healthy_accounts: number
}

export interface AccountPriorityPolicy {
  enabled: boolean
  strategy: "rate" | "latency_rate"
  target_group_ids: number[]
  sample_size: number
  lookback_minutes: number
  first_token_coefficient: number
  rate_coefficient: number
  missing_sample_penalty_ms: number
}

export interface TargetAutomationSettings {
  account_pool: AccountPoolPolicy
  account_priority: AccountPriorityPolicy
  last_applied_at?: string | null
  last_apply_status?: string
  last_apply_message?: string
}

export interface TargetAnnouncementRule {
  id: number
  name: string
  enabled: boolean
  title_template: string
  content_template: string
  target_group_ids: number[]
  status: "draft" | "active" | "archived"
  notify_mode: "silent" | "popup"
}

export type TargetAnnouncementRuleInput = Omit<TargetAnnouncementRule, "id">

const operationsRoot = "/operations"

function isRecord(value: unknown): value is Record<string, unknown> {
  return value != null && typeof value === "object" && !Array.isArray(value)
}

function firstArray(value: unknown, keys: string[]): unknown[] {
  if (Array.isArray(value)) return value
  if (!isRecord(value)) return []
  for (const key of keys) {
    if (Array.isArray(value[key])) return value[key]
  }
  return []
}

function requireRecord(value: unknown, message: string): Record<string, unknown> {
  if (isRecord(value)) return value
  throw new Error(message)
}

const ratePolicyModes = new Set<RatePolicyMode>([
  "first", "average", "min", "max", "custom", "locked", "manual_source",
])

function normalizeRatePolicyMode(value: unknown): RatePolicyMode {
  if (value == null || value === "") return "first"
  if (typeof value === "string" && ratePolicyModes.has(value as RatePolicyMode)) {
    return value as RatePolicyMode
  }
  throw new Error(`服务端返回了不支持的倍率计算模式：${String(value)}`)
}

function normalizeRatePolicyTarget(value: unknown): RatePolicyTarget {
  const item = requireRecord(value, "倍率同步保存响应格式异常")
  const rule = isRecord(item.rule) ? item.rule : {}
  const mode = normalizeRatePolicyMode(rule.mode)
  return {
    target_type: item.target_type === "account" ? "account" : "group",
    id: typeof item.id === "number" ? item.id : Number(item.id),
    name: typeof item.name === "string" ? item.name : "",
    current_rate: typeof item.current_rate === "number" ? item.current_rate : Number(item.current_rate ?? 0),
    bindings: firstArray(item.bindings, ["items"]) as RatePolicyBinding[],
    rule: {
      enabled: rule.enabled === true,
      mode,
      offset: typeof rule.offset === "number" ? rule.offset : Number(rule.offset ?? 0),
      expression: typeof rule.expression === "string" ? rule.expression : undefined,
    },
  }
}

function normalizeRatePolicyWorkspace(value: unknown): RatePolicyWorkspace {
  const workspace = requireRecord(value, "倍率同步规则响应格式异常")
  return {
    sources: firstArray(workspace.sources, ["items"]).filter(isRecord).map((source) => ({
      channel_id: Number(source.channel_id ?? source.channelId ?? 0),
      channel_name: typeof source.channel_name === "string" ? source.channel_name : String(source.channel_name ?? ""),
      channel_type: typeof source.channel_type === "string" ? source.channel_type : String(source.channel_type ?? ""),
      group_id: String(source.group_id ?? source.groupId ?? ""),
      group_name: typeof source.group_name === "string" ? source.group_name : String(source.group_name ?? ""),
      rate: Number(source.rate ?? 0),
      fresh: source.fresh !== false,
      enabled: source.enabled !== false,
      last_status: typeof source.last_status === "string" ? source.last_status : String(source.last_status ?? "unknown"),
      last_error: typeof source.last_error === "string" ? source.last_error : undefined,
    })),
    groups: firstArray(workspace.groups, ["items"]).map(normalizeRatePolicyTarget),
    accounts: firstArray(workspace.accounts, ["items"]).map(normalizeRatePolicyTarget),
    exclusions: firstArray(workspace.exclusions, ["items"]) as RatePolicyExclusion[],
  }
}

function normalizeProbe(value: unknown): TargetProbe {
  const item = requireRecord(value, "增强探测响应格式异常")
  const candidateModels = firstArray(item.candidate_models, ["candidateModels"]).filter((model): model is string => typeof model === "string")
  const status = String(item.status ?? "unknown").toLowerCase()
  const normalizedStatus: OperationsStatus = status === "healthy" || status === "warning" || status === "error" ? status : "unknown"
  return {
    id: Number(item.id ?? 0),
    name: typeof item.name === "string" ? item.name : `探测规则 #${String(item.id ?? "")}`,
    account_id: item.account_id == null ? null : Number(item.account_id),
    group_name: typeof item.group_name === "string" ? item.group_name : undefined,
    provider: typeof item.provider === "string" ? item.provider : "unknown",
    endpoint: typeof item.endpoint === "string" ? item.endpoint : "",
    enabled: item.enabled === true,
    mode: item.mode === "heavy" || item.mode === "capability" ? item.mode : "light",
    status: normalizedStatus,
    model: typeof item.model === "string" ? item.model : undefined,
    candidate_models: candidateModels,
    latency_ms: item.latency_ms == null ? null : Number(item.latency_ms),
    first_token_ms: item.first_token_ms == null ? null : Number(item.first_token_ms),
    tokens_per_second: item.tokens_per_second == null ? null : Number(item.tokens_per_second),
    availability_7d: item.availability_7d == null ? null : Number(item.availability_7d),
    capability_passed: Number(item.capability_passed ?? 0),
    capability_total: Number(item.capability_total ?? 0),
    last_checked_at: typeof item.last_checked_at === "string" ? item.last_checked_at : null,
    next_run_at: typeof item.next_run_at === "string" ? item.next_run_at : null,
    last_error: typeof item.last_error === "string" ? item.last_error : undefined,
  }
}

function normalizeTargetProbePage(value: unknown): TargetProbePage {
  const page = requireRecord(value, "增强探测响应格式异常")
  const summary = isRecord(page.summary) ? page.summary : {}
  const items = firstArray(page.items, ["probes", "rules"]).map(normalizeProbe)
  return {
    items,
    summary: {
      total: Number(summary.total ?? items.length),
      healthy: Number(summary.healthy ?? items.filter((item) => item.status === "healthy").length),
      warning: Number(summary.warning ?? items.filter((item) => item.status === "warning").length),
      error: Number(summary.error ?? items.filter((item) => item.status === "error").length),
    },
  }
}

function normalizeTargetAccountPage(value: unknown): TargetAccountPage {
  const page = requireRecord(value, "目标账号响应格式异常")
  const summary = isRecord(page.summary) ? page.summary : {}
  return {
    items: firstArray(page.items, ["accounts"]).filter(isRecord) as unknown as TargetAccount[],
    summary: {
      total: Number(summary.total ?? 0),
      schedulable: Number(summary.schedulable ?? 0),
      errors: Number(summary.errors ?? 0),
      balance_low: Number(summary.balance_low ?? 0),
      temporary_unavailable: Number(summary.temporary_unavailable ?? 0),
    },
    total: Number(page.total ?? 0),
    page: Number(page.page ?? 1),
    page_size: Number(page.page_size ?? 50),
    pages: Math.max(1, Number(page.pages ?? 1)),
  }
}

function targetPath(targetID: number, suffix: string) {
  return `${operationsRoot}/targets/${targetID}${suffix}`
}

export async function listOperationsTargets(): Promise<UpstreamSyncTarget[]> {
  const response = await apiFetch<unknown>(`${operationsRoot}/targets`)
  return firstArray(response, ["targets", "items"]).filter(isRecord).map((target) => ({
    id: Number(target.id ?? 0),
    name: typeof target.name === "string" ? target.name : `目标站点 #${String(target.id ?? "")}`,
    base_url: typeof target.base_url === "string" ? target.base_url : String(target.base_url ?? ""),
    enabled: target.enabled !== false,
    last_check_status: typeof target.last_check_status === "string" ? target.last_check_status : undefined,
    last_check_at: typeof target.last_check_at === "string" ? target.last_check_at : null,
    last_check_error: typeof target.last_check_error === "string" ? target.last_check_error : undefined,
  }))
}

export function listTargetAccounts(
  targetID: number,
  input: {
    page?: number
    pageSize?: number
    search?: string
    schedule?: TargetAccountScheduleFilter
  } = {},
) {
  const query = new URLSearchParams({
    page: String(input.page ?? 1),
    page_size: String(input.pageSize ?? 50),
    schedule: input.schedule ?? "all",
  })
  if (input.search?.trim()) query.set("search", input.search.trim())
  return apiFetch<unknown>(targetPath(targetID, `/accounts?${query}`)).then(normalizeTargetAccountPage)
}

export function runTargetAccountAction(
  targetID: number,
  accountID: number,
  action: "enable" | "disable" | "clear_error" | "refresh" | "check_balance",
) {
  return apiFetch<{ ok: boolean }>(targetPath(targetID, `/accounts/${accountID}/actions`), {
    method: "POST",
    body: JSON.stringify({ action }),
  })
}

export function deleteTargetAccount(targetID: number, accountID: number) {
  return apiFetch<{ ok: boolean }>(targetPath(targetID, `/accounts/${accountID}`), {
    method: "DELETE",
  })
}

export function importAPIKeyToTarget(targetID: number, input: ImportAPIKeyInput) {
  return apiFetch<ImportAPIKeyResult>(targetPath(targetID, "/accounts/import-api-key"), {
    method: "POST",
    body: JSON.stringify(input),
  })
}

export function listTargetGroups(targetID: number) {
  return apiFetch<TargetGroup[]>(targetPath(targetID, "/groups"))
}

export function createTargetGroup(targetID: number, input: {
  name: string
  description?: string
  platform: string
  rate_multiplier: number
}) {
  return apiFetch<TargetGroup>(targetPath(targetID, "/groups"), {
    method: "POST",
    body: JSON.stringify(input),
  })
}

export function getTargetRatePolicies(targetID: number) {
  return apiFetch<unknown>(targetPath(targetID, "/rate-policies")).then(normalizeRatePolicyWorkspace)
}

export function saveTargetRatePolicy(
  targetID: number,
  targetType: RatePolicyTargetType,
  objectID: number,
  input: RatePolicyInput,
) {
  return apiFetch<unknown>(targetPath(targetID, `/rate-policies/${targetType}/${objectID}`), {
    method: "PUT",
    body: JSON.stringify(input),
  }).then(normalizeRatePolicyTarget)
}

export function applyTargetRatePolicies(targetID: number) {
  return apiFetch<{ queued: boolean; connection_id: number; mode: string }>(targetPath(targetID, "/rate-policies/apply"), {
    method: "POST",
  })
}

export function listTargetProbes(targetID: number) {
  return apiFetch<unknown>(targetPath(targetID, "/probes")).then(normalizeTargetProbePage)
}

export function createTargetProbe(targetID: number, input: TargetProbeInput) {
  return apiFetch<unknown>(targetPath(targetID, "/probes"), {
    method: "POST",
    body: JSON.stringify(input),
  }).then(normalizeProbe)
}

export function deleteTargetProbe(targetID: number, probeID: number) {
  return apiFetch<{ ok: boolean }>(targetPath(targetID, `/probes/${probeID}`), {
    method: "DELETE",
  })
}

export function runTargetProbe(targetID: number, probeID: number) {
  return apiFetch<unknown>(targetPath(targetID, `/probes/${probeID}/run`), {
    method: "POST",
  }).then(normalizeProbe)
}

export function setTargetProbeEnabled(targetID: number, probeID: number, enabled: boolean) {
  return apiFetch<unknown>(targetPath(targetID, `/probes/${probeID}`), {
    method: "PUT",
    body: JSON.stringify({ enabled }),
  }).then(normalizeProbe)
}

export function runTargetProbeBatch(
  targetID: number,
  input: {
    mode: TargetProbeRunMode | "all"
    probeIDs: number[]
    accountIDs: number[]
  },
) {
  return apiFetch<{ queued: number }>(targetPath(targetID, "/probes/run"), {
    method: "POST",
    body: JSON.stringify({
      mode: input.mode,
      probe_ids: input.probeIDs,
      account_ids: input.accountIDs,
    }),
  })
}

export function getOperationsAnalytics(range: "day" | "week" | "month") {
  return apiFetch<TargetAnalytics>(`${operationsRoot}/analytics?range=${range}`)
}

export interface GroupMonitorSummaryRow {
  monitor_id: number
  group_id: number
  group_name: string
  enabled: boolean
  interval_minutes: number
  last_run_at?: string
  account_count: number
  healthy_count: number
  failed_count: number
  unknown_count: number
  probes_7d: number
  availability_7d: number
  avg_ttft_ms_7d: number
  cache_rate_7d: number
}

export interface GroupMonitorOverview {
  total_monitors: number
  healthy_monitors: number
  failed_monitors: number
  total_accounts: number
  healthy_accounts: number
  failed_accounts: number
  avg_availability: number
  monitors: GroupMonitorSummaryRow[]
}

export function getGroupMonitorOverview() {
  return apiFetch<GroupMonitorOverview>(`${operationsRoot}/group-monitor`)
}

export function getOperationsDiagnostics() {
  return apiFetch<OperationsDiagnostics>(`${operationsRoot}/diagnostics`)
}

export function cleanupOperationsInvalidData() {
  return apiFetch<OperationsDiagnostics["invalid_data"]>(`${operationsRoot}/diagnostics/cleanup`, {
    method: "POST",
  })
}

export function getOperationsSettings() {
  return apiFetch<OperationsSettings>(`${operationsRoot}/settings`)
}

export function saveOperationsSettings(settings: OperationsSettings) {
  return apiFetch<OperationsSettings>(`${operationsRoot}/settings`, {
    method: "PUT",
    body: JSON.stringify(settings),
  })
}

export function getTargetOperationsSettings(targetID: number) {
  return apiFetch<TargetOperationsSettings>(targetPath(targetID, "/settings"))
}

export function saveTargetOperationsSettings(targetID: number, settings: TargetOperationsSettings) {
  return apiFetch<TargetOperationsSettings>(targetPath(targetID, "/settings"), {
    method: "PUT",
    body: JSON.stringify(settings),
  })
}

export function testTargetBalanceWebhook(targetID: number) {
  return apiFetch<{ ok: boolean }>(targetPath(targetID, "/settings/test-balance-webhook"), {
    method: "POST",
  })
}

export function getTargetAutomationSettings(targetID: number) {
  return apiFetch<TargetAutomationSettings>(targetPath(targetID, "/automation"))
}

export function saveTargetAutomationSettings(targetID: number, settings: TargetAutomationSettings) {
  return apiFetch<TargetAutomationSettings>(targetPath(targetID, "/automation"), {
    method: "PUT",
    body: JSON.stringify(settings),
  })
}

export function applyTargetAutomation(targetID: number) {
  return apiFetch<TargetAutomationSettings>(targetPath(targetID, "/automation/apply"), {
    method: "POST",
  })
}

export function getPoolExecutionMode() {
  return apiFetch<{ mode: string }>(`${operationsRoot}/pool-execution`)
}

export function setPoolExecutionMode(mode: "local" | "delegated") {
  return apiFetch<{ mode: string }>(`${operationsRoot}/pool-execution`, {
    method: "PUT",
    body: JSON.stringify({ mode }),
  })
}

export function listTargetAnnouncementRules(targetID: number) {
  return apiFetch<TargetAnnouncementRule[]>(targetPath(targetID, "/announcement-rules"))
}

export function createTargetAnnouncementRule(
  targetID: number,
  input: TargetAnnouncementRuleInput,
) {
  return apiFetch<TargetAnnouncementRule>(targetPath(targetID, "/announcement-rules"), {
    method: "POST",
    body: JSON.stringify(input),
  })
}

export function updateTargetAnnouncementRule(
  targetID: number,
  ruleID: number,
  input: TargetAnnouncementRuleInput,
) {
  return apiFetch<TargetAnnouncementRule>(
    targetPath(targetID, `/announcement-rules/${ruleID}`),
    { method: "PUT", body: JSON.stringify(input) },
  )
}

export function deleteTargetAnnouncementRule(targetID: number, ruleID: number) {
  return apiFetch<{ ok: boolean }>(targetPath(targetID, `/announcement-rules/${ruleID}`), {
    method: "DELETE",
  })
}
