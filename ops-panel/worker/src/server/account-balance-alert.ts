import { fetchAccountBalances, type AccountBalanceResult } from "@/server/account-balance";
import { getAccountId, getAccountName } from "@/server/account-utils";
import type { Sub2ApiAdminClient } from "@/server/clients/sub2api-admin";
import { requestText } from "@/server/http";
import { getSetting, setSetting } from "@/server/settings";
import { writeSyncLog } from "@/server/sync-logs";

export type AccountBalanceWebhookConfig = {
  enabled: boolean;
  url: string;
  cooldownMinutes: number;
  template: string;
  updatedAt?: string | null;
};

export type AccountBalanceAlertAccount = {
  accountId: number;
  accountName: string;
  threshold: number;
  remaining: number;
  unit: string;
  provider: string | null;
  planName: string | null;
  checkedAt: string;
};

export type AccountBalanceAlertResult = {
  ok: boolean;
  enabled: boolean;
  checked: number;
  low: number;
  sent: number;
  skippedCooldown: number;
  balanceIssues: number;
  failed: number;
  accounts: AccountBalanceAlertAccount[];
  message: string;
};

type AlertState = Record<string, { lastSentAt?: string; lastRemaining?: number; lastThreshold?: number }>;
type AlertStateRow = AlertState[string];
type BalanceIssue = {
  accountId: number;
  threshold: number;
  status: string;
  remaining?: number | null;
  unit?: string | null;
  provider?: AccountBalanceResult["provider"] | null;
  planName?: string | null;
  message?: string | null;
  checkedAt?: string;
};

const defaultTemplate = [
  "Sub2API 运营面板 账号余额预警",
  "连接：{{connectionName}}",
  "账号：{{accountName}} (#{{accountId}})",
  "余额：{{remaining}} {{unit}}",
  "阈值：{{threshold}} {{unit}}",
  "来源：{{provider}} {{planName}}",
  "检测时间（北京时间）：{{checkedAt}}",
].join("\n");

export const defaultAccountBalanceWebhookConfig: AccountBalanceWebhookConfig = {
  enabled: false,
  url: "",
  cooldownMinutes: 360,
  template: defaultTemplate,
  updatedAt: null,
};

function thresholdsSettingKey(connectionId: number) {
  return `account_balance_thresholds:${connectionId}`;
}

function defaultThresholdSettingKey(connectionId: number) {
  return `account_balance_default_threshold:${connectionId}`;
}

function webhookSettingKey(connectionId: number) {
  return `account_balance_webhook:${connectionId}`;
}

function stateSettingKey(connectionId: number) {
  return `account_balance_alert_state:${connectionId}`;
}

export async function markAccountBalanceAlertsDue() {
  await setSetting("account_balance_alert_next_run_at", "");
}

function finiteNonNegativeNumber(value: unknown) {
  const numeric = Number(value);
  return Number.isFinite(numeric) && numeric >= 0 ? numeric : null;
}

function normalizeUrl(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function normalizeCooldownMinutes(value: unknown) {
  const numeric = Number(value);
  if (!Number.isFinite(numeric)) return defaultAccountBalanceWebhookConfig.cooldownMinutes;
  return Math.min(Math.max(Math.trunc(numeric), 0), 30 * 24 * 60);
}

export function normalizeBalanceThresholds(value: unknown) {
  const thresholds: Record<string, number> = {};
  if (!value || typeof value !== "object" || Array.isArray(value)) return thresholds;

  for (const [rawAccountId, rawThreshold] of Object.entries(value)) {
    const accountId = Number(rawAccountId);
    const threshold = finiteNonNegativeNumber(rawThreshold);
    if (!Number.isInteger(accountId) || accountId <= 0 || threshold === null) continue;
    thresholds[String(accountId)] = threshold;
  }

  return thresholds;
}

export async function readBalanceThresholds(connectionId: number) {
  const raw = await getSetting(thresholdsSettingKey(connectionId), "{}");
  try {
    return normalizeBalanceThresholds(JSON.parse(raw) as unknown);
  } catch {
    return {};
  }
}

export async function readDefaultBalanceThreshold(connectionId: number) {
  return finiteNonNegativeNumber(await getSetting(defaultThresholdSettingKey(connectionId), "0")) ?? 0;
}

export async function writeBalanceThresholds(connectionId: number, thresholds: Record<string, number>) {
  const sorted = Object.fromEntries(
    Object.entries(normalizeBalanceThresholds(thresholds)).sort(([left], [right]) => Number(left) - Number(right)),
  );
  await setSetting(thresholdsSettingKey(connectionId), JSON.stringify(sorted));
  return sorted;
}

export async function removeBalanceThreshold(connectionId: number, accountId: number) {
  const thresholds = await readBalanceThresholds(connectionId);
  if (!(String(accountId) in thresholds)) return thresholds;
  delete thresholds[String(accountId)];
  return writeBalanceThresholds(connectionId, thresholds);
}

export function normalizeAccountBalanceWebhookConfig(value: unknown): AccountBalanceWebhookConfig {
  if (!value || typeof value !== "object" || Array.isArray(value)) return defaultAccountBalanceWebhookConfig;
  const raw = value as Partial<AccountBalanceWebhookConfig>;
  const template = typeof raw.template === "string" && raw.template.trim()
    ? raw.template
    : defaultAccountBalanceWebhookConfig.template;

  return {
    enabled: raw.enabled === true,
    url: normalizeUrl(raw.url),
    cooldownMinutes: normalizeCooldownMinutes(raw.cooldownMinutes),
    template,
    updatedAt: typeof raw.updatedAt === "string" ? raw.updatedAt : null,
  };
}

export async function readAccountBalanceWebhookConfig(connectionId: number) {
  const raw = await getSetting(webhookSettingKey(connectionId), "");
  if (!raw.trim()) return defaultAccountBalanceWebhookConfig;
  try {
    return normalizeAccountBalanceWebhookConfig(JSON.parse(raw) as unknown);
  } catch {
    return defaultAccountBalanceWebhookConfig;
  }
}

export async function saveAccountBalanceWebhookConfig(
  connectionId: number,
  config: Pick<AccountBalanceWebhookConfig, "enabled" | "url" | "cooldownMinutes" | "template">,
) {
  const normalized = normalizeAccountBalanceWebhookConfig({
    ...config,
    updatedAt: new Date().toISOString(),
  });
  if (normalized.enabled && !normalized.url) {
    throw new Error("启用余额 webhook 预警时必须填写 Webhook URL");
  }
  if (normalized.url && !/^https?:\/\//i.test(normalized.url)) {
    throw new Error("Webhook URL 必须以 http:// 或 https:// 开头");
  }
  await setSetting(webhookSettingKey(connectionId), JSON.stringify(normalized));
  return normalized;
}

function normalizeState(value: unknown): AlertState {
  const state: AlertState = {};
  if (!value || typeof value !== "object" || Array.isArray(value)) return state;
  for (const [accountId, raw] of Object.entries(value)) {
    if (!Number.isInteger(Number(accountId)) || Number(accountId) <= 0) continue;
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) continue;
    const row = raw as AlertState[string];
    state[accountId] = {
      lastSentAt: typeof row.lastSentAt === "string" ? row.lastSentAt : undefined,
      lastRemaining: typeof row.lastRemaining === "number" && Number.isFinite(row.lastRemaining) ? row.lastRemaining : undefined,
      lastThreshold: typeof row.lastThreshold === "number" && Number.isFinite(row.lastThreshold) ? row.lastThreshold : undefined,
    };
  }
  return state;
}

async function readAlertState(connectionId: number) {
  const raw = await getSetting(stateSettingKey(connectionId), "{}");
  try {
    return normalizeState(JSON.parse(raw) as unknown);
  } catch {
    return {};
  }
}

async function writeAlertState(connectionId: number, state: AlertState) {
  await setSetting(stateSettingKey(connectionId), JSON.stringify(state));
}

export async function clearAccountBalanceAlertState(connectionId: number, accountIds?: number[]) {
  if (!accountIds || accountIds.length === 0) {
    await writeAlertState(connectionId, {});
    return;
  }

  const idSet = new Set(accountIds.map(String));
  if (idSet.size === 0) return;

  const state = await readAlertState(connectionId);
  let changed = false;
  for (const accountId of idSet) {
    if (accountId in state) {
      delete state[accountId];
      changed = true;
    }
  }
  if (changed) await writeAlertState(connectionId, state);
}

function isLowBalance(balance: AccountBalanceResult | undefined, threshold: number | undefined) {
  return balance?.status === "ok"
    && threshold !== undefined
    && typeof balance.remaining === "number"
    && Number.isFinite(balance.remaining)
    && balance.remaining < threshold;
}

function formatNumber(value: number) {
  if (Math.abs(value) >= 1000) return value.toFixed(0);
  if (Math.abs(value) >= 1) return value.toFixed(2).replace(/\.?0+$/, "");
  return value.toFixed(4).replace(/\.?0+$/, "");
}

function formatBeijingTime(value: string) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(date).replace(/\//g, "-");
}

function renderTemplate(template: string, variables: Record<string, string>) {
  return template.replace(/\{\{\s*([a-zA-Z0-9_]+)\s*\}\}/g, (_match, key: string) => variables[key] ?? "");
}

function buildWebhookPayload(input: {
  connectionId: number;
  connectionName: string;
  alert: AccountBalanceAlertAccount;
  template: string;
}) {
  const variables = {
    connectionId: String(input.connectionId),
    connectionName: input.connectionName,
    accountId: String(input.alert.accountId),
    accountName: input.alert.accountName,
    remaining: formatNumber(input.alert.remaining),
    threshold: formatNumber(input.alert.threshold),
    unit: input.alert.unit,
    provider: input.alert.provider ?? "",
    planName: input.alert.planName ?? "",
    checkedAt: formatBeijingTime(input.alert.checkedAt),
    checkedAtIso: input.alert.checkedAt,
  };
  const text = renderTemplate(input.template, variables);
  return {
    text,
    content: text,
    msgtype: "text",
    markdown: text,
    title: "Sub2API 运营面板 账号余额预警",
    event: "account_balance_low",
    ...variables,
  };
}

function isEnterpriseWechatWebhook(url: string) {
  try {
    const parsed = new URL(url);
    return parsed.hostname.toLowerCase() === "qyapi.weixin.qq.com"
      && parsed.pathname.toLowerCase().includes("/cgi-bin/webhook/send");
  } catch {
    return false;
  }
}

function enterpriseWechatPayload(payload: Record<string, unknown>) {
  const content = typeof payload.text === "string"
    ? payload.text
    : typeof payload.content === "string"
      ? payload.content
      : JSON.stringify(payload);
  return {
    msgtype: "text",
    text: {
      content,
    },
  };
}

function parseJsonObject(raw: string) {
  try {
    const parsed = JSON.parse(raw) as unknown;
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed as Record<string, unknown> : null;
  } catch {
    return null;
  }
}

function assertEnterpriseWechatResponse(raw: string) {
  const parsed = parseJsonObject(raw);
  if (!parsed || !("errcode" in parsed)) return;
  const errcode = Number(parsed.errcode);
  if (errcode === 0) return;
  const errmsg = typeof parsed.errmsg === "string" ? parsed.errmsg : raw.slice(0, 240);
  throw new Error(`企业微信 Webhook 返回 errcode=${Number.isFinite(errcode) ? errcode : parsed.errcode}: ${errmsg}`);
}

async function sendWebhook(url: string, payload: Record<string, unknown>) {
  const isEnterpriseWechat = isEnterpriseWechatWebhook(url);
  const { status, body } = await requestText({
    method: "POST",
    url,
    headers: { "Content-Type": "application/json; charset=utf-8", Accept: "application/json, text/plain, */*" },
    body: JSON.stringify(isEnterpriseWechat ? enterpriseWechatPayload(payload) : payload),
    timeoutMs: 15_000,
  });
  if (status < 200 || status >= 300) {
    throw new Error(`HTTP ${status}: ${body.slice(0, 240)}`);
  }
  if (isEnterpriseWechat) assertEnterpriseWechatResponse(body);
  return { status, body: body.slice(0, 500) };
}

function accountNameById(accounts: unknown[]) {
  const map = new Map<number, string>();
  for (const account of accounts) {
    const accountId = getAccountId(account);
    if (accountId) map.set(accountId, getAccountName(account, accountId));
  }
  return map;
}

function shouldSkipCooldown(state: AlertStateRow | undefined, threshold: number, cooldownMinutes: number, now: Date) {
  if (!state?.lastSentAt || cooldownMinutes <= 0) return false;
  if (state.lastThreshold !== undefined && state.lastThreshold !== threshold) return false;
  const sentAt = new Date(state.lastSentAt).getTime();
  if (!Number.isFinite(sentAt)) return false;
  return now.getTime() - sentAt < cooldownMinutes * 60_000;
}

export async function checkAccountBalanceAlerts(input: {
  db: unknown;
  connectionId: number;
  connectionName?: string | null;
  s2Client: Sub2ApiAdminClient;
  accounts?: unknown[];
  force?: boolean;
  ignoreCooldown?: boolean;
  action?: string;
}): Promise<AccountBalanceAlertResult> {
  const [config, thresholds, state, defaultThreshold] = await Promise.all([
    readAccountBalanceWebhookConfig(input.connectionId),
    readBalanceThresholds(input.connectionId),
    readAlertState(input.connectionId),
    readDefaultBalanceThreshold(input.connectionId),
  ]);
  let accounts = input.accounts;
  if (defaultThreshold > 0) {
    try {
      accounts ??= await input.s2Client.listAccounts();
      for (const account of accounts) {
        const accountId = getAccountId(account);
        if (accountId && thresholds[String(accountId)] === undefined) {
          thresholds[String(accountId)] = defaultThreshold;
        }
      }
    } catch {
      // Explicit per-account thresholds remain usable when account discovery is unavailable.
    }
  }
  const now = new Date();
  const thresholdEntries = Object.entries(thresholds)
    .map(([accountId, threshold]) => ({ accountId: Number(accountId), threshold }))
    .filter((row) => Number.isInteger(row.accountId) && row.accountId > 0 && Number.isFinite(row.threshold));

  if (!config.enabled || !config.url || thresholdEntries.length === 0) {
    return {
      ok: true,
      enabled: config.enabled,
      checked: 0,
      low: 0,
      sent: 0,
      skippedCooldown: 0,
      balanceIssues: 0,
      failed: 0,
      accounts: [],
      message: !config.enabled ? "balance webhook disabled" : thresholdEntries.length === 0 ? "no balance thresholds" : "webhook url missing",
    };
  }

  let skippedCooldown = 0;
  const dueEntries = thresholdEntries.filter((row) => {
    if (input.ignoreCooldown || !shouldSkipCooldown(state[String(row.accountId)], row.threshold, config.cooldownMinutes, now)) {
      return true;
    }
    skippedCooldown += 1;
    return false;
  });

  if (dueEntries.length === 0) {
    const action = input.action ?? "account_balance_webhook_alert";
    await writeSyncLog(input.db, {
      connectionId: input.connectionId,
      action,
      target: `connection:${input.connectionId}`,
      detail: {
        checked: thresholdEntries.length,
        low: 0,
        sent: 0,
        skippedCooldown,
        balanceIssues: [],
        failed: 0,
        accounts: [],
        failures: [],
      },
      status: "success",
      level: "info",
    });
    return {
      ok: true,
      enabled: true,
      checked: thresholdEntries.length,
      low: 0,
      sent: 0,
      skippedCooldown,
      balanceIssues: 0,
      failed: 0,
      accounts: [],
      message: "balance alerts skipped by cooldown",
    };
  }

  const balances = await fetchAccountBalances({
    client: input.s2Client,
    connectionId: input.connectionId,
    accountIds: dueEntries.map((row) => row.accountId),
    force: input.force,
  });
  const balancesById = new Map(balances.map((balance) => [balance.accountId, balance]));
  const balanceIssues = dueEntries.flatMap((row): BalanceIssue[] => {
    const balance = balancesById.get(row.accountId);
    if (!balance) {
      return [{ accountId: row.accountId, threshold: row.threshold, status: "missing", message: "balance result missing" }];
    }
    if (balance.status === "ok" && typeof balance.remaining === "number" && Number.isFinite(balance.remaining)) {
      return [];
    }
    return [{
      accountId: row.accountId,
      threshold: row.threshold,
      status: balance.status,
      remaining: balance.remaining ?? null,
      unit: balance.unit ?? null,
      provider: balance.provider ?? null,
      planName: balance.planName ?? null,
      message: balance.message ?? null,
      checkedAt: balance.checkedAt,
    }];
  });
  const lowRows = dueEntries.flatMap((row) => {
    const balance = balancesById.get(row.accountId);
    return balance && isLowBalance(balance, row.threshold) ? [{ row, balance }] : [];
  });
  let names = new Map<number, string>();
  if (lowRows.length > 0) {
    try {
      names = accountNameById(accounts ?? await input.s2Client.listAccounts());
    } catch {
      names = new Map<number, string>();
    }
  }
  const lowAccounts = lowRows.map(({ row, balance }): AccountBalanceAlertAccount => ({
      accountId: row.accountId,
      accountName: names.get(row.accountId) ?? `#${row.accountId}`,
      threshold: row.threshold,
      remaining: balance.remaining ?? 0,
      unit: balance.unit || "USD",
      provider: balance.provider ?? null,
      planName: balance.planName ?? null,
      checkedAt: balance.checkedAt,
  }));

  let sent = 0;
  let failed = 0;
  const failures: Array<{ accountId: number; error: string }> = [];

  for (const alert of lowAccounts) {
    const key = String(alert.accountId);
    try {
      await sendWebhook(config.url, buildWebhookPayload({
        connectionId: input.connectionId,
        connectionName: input.connectionName || `connection:${input.connectionId}`,
        alert,
        template: config.template,
      }));
      state[key] = {
        lastSentAt: now.toISOString(),
        lastRemaining: alert.remaining,
        lastThreshold: alert.threshold,
      };
      sent += 1;
    } catch (error) {
      failed += 1;
      failures.push({ accountId: alert.accountId, error: error instanceof Error ? error.message : String(error) });
    }
  }

  if (sent > 0) await writeAlertState(input.connectionId, state);

  const action = input.action ?? "account_balance_webhook_alert";
  const shouldLog = action.startsWith("manual_")
    || action.startsWith("auto_")
    || sent > 0
    || skippedCooldown > 0
    || balanceIssues.length > 0
    || failed > 0;
  if (shouldLog) {
    await writeSyncLog(input.db, {
      connectionId: input.connectionId,
      action,
      target: `connection:${input.connectionId}`,
      detail: {
        checked: thresholdEntries.length,
        low: lowAccounts.length,
        sent,
        queried: dueEntries.length,
        skippedCooldown,
        balanceIssues: balanceIssues.slice(0, 80),
        failed,
        accounts: lowAccounts.slice(0, 80),
        failures: failures.slice(0, 40),
      },
      status: failed > 0 ? "failed" : "success",
      level: lowAccounts.length > 0 ? "warning" : "info",
      error: failed > 0 ? `${failed} 个余额预警 webhook 发送失败` : undefined,
    });
  }

  return {
    ok: failed === 0,
    enabled: true,
    checked: thresholdEntries.length,
    low: lowAccounts.length,
    sent,
    skippedCooldown,
    balanceIssues: balanceIssues.length,
    failed,
    accounts: lowAccounts,
    message: failed > 0 ? "balance alerts partially failed" : "balance alerts checked",
  };
}

export async function sendAccountBalanceWebhookTest(input: {
  connectionId: number;
  connectionName?: string | null;
  config?: AccountBalanceWebhookConfig;
}) {
  const config = input.config ?? await readAccountBalanceWebhookConfig(input.connectionId);
  if (!config.url) throw new Error("请先填写 Webhook URL");
  const checkedAt = new Date().toISOString();
  const alert: AccountBalanceAlertAccount = {
    accountId: 0,
    accountName: "Webhook Test",
    threshold: 10,
    remaining: 1.23,
    unit: "USD",
    provider: "test",
    planName: "test",
    checkedAt,
  };
  await sendWebhook(config.url, buildWebhookPayload({
    connectionId: input.connectionId,
    connectionName: input.connectionName || `connection:${input.connectionId}`,
    alert,
    template: config.template || defaultTemplate,
  }));
  return { ok: true, checkedAt };
}
