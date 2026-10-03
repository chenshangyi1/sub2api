import { requestText } from "@/server/http";
import type { Sub2ApiAdminClient, Sub2ApiDataAccount, Sub2ApiDataPayload } from "@/server/clients/sub2api-admin";
import { getAccountId } from "@/server/account-utils";
import { BoundedPromiseCache } from "@/server/promise-cache";

export type AccountBalanceStatus = "ok" | "unsupported" | "invalid" | "error";

export type AccountBalanceResult = {
  accountId: number;
  status: AccountBalanceStatus;
  provider?: "sub2api-admin" | "usage" | "new-api";
  planName?: string | null;
  remaining?: number | null;
  used?: number | null;
  total?: number | null;
  unit?: string | null;
  message?: string | null;
  checkedAt: string;
};

export function isAccountBalanceExhausted(result: Pick<AccountBalanceResult, "status" | "remaining"> | null | undefined) {
  return result?.status === "ok"
    && typeof result.remaining === "number"
    && Number.isFinite(result.remaining)
    && result.remaining <= 0;
}

type ExportAccountMap = Map<number, Sub2ApiDataAccount>;
type ExportAccountLookup = {
  accounts: ExportAccountMap;
  issues: Map<number, string>;
};
type NewApiSession = {
  baseUrl: string;
  accessToken: string;
  sourceSiteId: number;
  sourceSiteName: string;
};
type NewApiSessionLookup = Map<number, NewApiSession>;

const DEFAULT_NEW_API_QUOTA_PER_UNIT = 500_000;
const NEW_API_QUOTA_CACHE_TTL_MS = 5 * 60_000;
const newApiQuotaPerUnitCache = new BoundedPromiseCache<string, number>(NEW_API_QUOTA_CACHE_TTL_MS);

function finiteNumber(value: unknown) {
  if (value === null || value === undefined || value === "") return null;
  const numeric = Number(value);
  return Number.isFinite(numeric) ? numeric : null;
}

function pickNumber(source: Record<string, unknown> | null | undefined, keys: string[]) {
  if (!source) return null;
  for (const key of keys) {
    const value = finiteNumber(source[key]);
    if (value !== null) return value;
  }
  return null;
}

function stringValue(value: unknown) {
  return typeof value === "string" && value.trim() ? value.trim() : "";
}

function pickString(source: Record<string, unknown> | null | undefined, keys: string[]) {
  if (!source) return "";
  for (const key of keys) {
    const value = stringValue(source[key]);
    if (value) return value;
  }
  return "";
}

function lowerString(value: unknown) {
  return stringValue(value).toLowerCase();
}

function normalizeBaseUrl(value: string) {
  return value.trim().replace(/\/+$/, "");
}

function nestedObject(source: Record<string, unknown>, key: string) {
  const value = source[key];
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function nestedArray(source: Record<string, unknown>, key: string) {
  const value = source[key];
  return Array.isArray(value) ? value as unknown[] : [];
}

function deepPickObject(source: unknown, keys: string[]) {
  if (!source || typeof source !== "object" || Array.isArray(source)) return null;
  const record = source as Record<string, unknown>;
  for (const key of keys) {
    const value = record[key];
    if (value && typeof value === "object" && !Array.isArray(value)) return value as Record<string, unknown>;
  }
  return null;
}

function collectCredentialScopes(credentials: Record<string, unknown>) {
  const scopes: Record<string, unknown>[] = [credentials];
  const nested = deepPickObject(credentials, ["auth", "auth_info", "api", "token", "tokens", "new_api", "newApi", "new_api_auth", "newApiAuth", "provider", "config"]);
  if (nested) scopes.push(nested);
  return scopes;
}

function findCredentialString(credentials: Record<string, unknown>, keys: string[]) {
  for (const scope of collectCredentialScopes(credentials)) {
    for (const key of keys) {
      const value = stringValue(scope[key]);
      if (value) return value;
    }
  }
  return "";
}

function jwtUserId(token: string) {
  const payload = token.split(".")[1];
  if (!payload) return "";
  try {
    const decoded = JSON.parse(Buffer.from(payload.replace(/-/g, "+").replace(/_/g, "/"), "base64").toString("utf8")) as Record<string, unknown>;
    const value = decoded.id ?? decoded.user_id ?? decoded.userId ?? decoded.sub;
    return value === undefined || value === null ? "" : String(value);
  } catch {
    return "";
  }
}

function unwrapJson(raw: string) {
  const parsed = JSON.parse(raw) as unknown;
  if (parsed && typeof parsed === "object" && "code" in parsed && "data" in parsed) {
    const envelope = parsed as { code?: unknown; message?: unknown; data?: unknown };
    if (envelope.code !== true && Number(envelope.code) !== 0) throw new Error(stringValue(envelope.message) || "请求失败");
    return envelope.data;
  }
  return parsed;
}

function unwrapSuccessData(payload: unknown) {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) return null;
  const response = payload as Record<string, unknown>;
  return response.success === true && response.data && typeof response.data === "object" && !Array.isArray(response.data)
    ? response.data as Record<string, unknown>
    : response;
}

function quotaPerUnitFromPayload(payload: unknown) {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) return null;
  const response = payload as Record<string, unknown>;
  const data = nestedObject(response, "data");
  const quotaPerUnit = finiteNumber(response.quota_per_unit)
    ?? finiteNumber(data?.quota_per_unit)
    ?? finiteNumber(nestedObject(response, "status")?.quota_per_unit)
    ?? finiteNumber(nestedObject(response, "meta")?.quota_per_unit);
  return quotaPerUnit !== null && quotaPerUnit > 0 ? quotaPerUnit : null;
}

function newApiQuotaToUsd(value: number | null, quotaPerUnit: number) {
  return value === null ? null : value / quotaPerUnit;
}

function invalidResult(accountId: number, message: string, checkedAt: string): AccountBalanceResult {
  return { accountId, status: "invalid", remaining: null, unit: null, message, checkedAt };
}

function unsupportedResult(accountId: number, message: string, checkedAt: string): AccountBalanceResult {
  return { accountId, status: "unsupported", remaining: null, unit: null, message, checkedAt };
}

function errorResult(accountId: number, message: string, checkedAt: string): AccountBalanceResult {
  return { accountId, status: "error", remaining: null, unit: null, message, checkedAt };
}

function successResult(accountId: number, provider: AccountBalanceResult["provider"], data: Omit<AccountBalanceResult, "accountId" | "status" | "provider" | "checkedAt">, checkedAt: string): AccountBalanceResult {
  return { accountId, status: "ok", provider, checkedAt, ...data };
}

function parseUsageLikeResponse(accountId: number, payload: unknown, checkedAt: string): AccountBalanceResult | null {
  if (!payload || typeof payload !== "object") return null;
  const response = payload as Record<string, unknown>;

  const remaining = finiteNumber(response.remaining)
    ?? finiteNumber((response.quota as Record<string, unknown> | undefined)?.remaining)
    ?? finiteNumber(response.balance);
  const unit = stringValue(response.unit) || stringValue((response.quota as Record<string, unknown> | undefined)?.unit) || "USD";
  const isValid = response.is_active ?? response.isValid ?? true;
  if (isValid === false) {
    return invalidResult(accountId, stringValue(response.message) || "账号不可用", checkedAt);
  }
  if (remaining === null) return null;

  return successResult(accountId, "usage", { remaining, unit }, checkedAt);
}

function parseNewApiSelfResponse(accountId: number, payload: unknown, checkedAt: string, fallbackQuotaPerUnit = DEFAULT_NEW_API_QUOTA_PER_UNIT): AccountBalanceResult | null {
  if (!payload || typeof payload !== "object") return null;
  const response = payload as Record<string, unknown>;
  if (response.success === false) {
    return invalidResult(accountId, stringValue(response.message) || "查询失败", checkedAt);
  }

  const data = unwrapSuccessData(response);
  if (!data) return null;
  const quotaPerUnit = quotaPerUnitFromPayload(response) ?? fallbackQuotaPerUnit;
  const quota = pickNumber(data, ["quota", "remain_quota", "remaining_quota", "remainingQuota", "balance_quota", "balanceQuota", "total_available", "totalAvailable", "balance", "remaining"]);
  const usedQuota = pickNumber(data, ["used_quota", "usedQuota", "used"]);
  const totalQuota = pickNumber(data, ["total", "total_quota", "totalQuota", "total_granted", "totalGranted"]);

  if (quota !== null || usedQuota !== null || totalQuota !== null) {
    const remainingQuota = quota ?? (totalQuota !== null && usedQuota !== null ? Math.max(0, totalQuota - usedQuota) : null);
    const resolvedTotalQuota = totalQuota ?? (remainingQuota !== null && usedQuota !== null ? remainingQuota + usedQuota : null);

    return successResult(accountId, "new-api", {
      planName: pickString(data, ["group", "plan_name", "planName", "plan", "subscription_name", "subscriptionName"]) || "默认套餐",
      remaining: newApiQuotaToUsd(remainingQuota, quotaPerUnit),
      used: newApiQuotaToUsd(usedQuota, quotaPerUnit),
      total: newApiQuotaToUsd(resolvedTotalQuota, quotaPerUnit),
      unit: "USD",
    }, checkedAt);
  }

  return parseNewApiSubscriptionResponse(accountId, data, checkedAt, quotaPerUnit);
}

function parseNewApiSubscriptionResponse(accountId: number, data: Record<string, unknown>, checkedAt: string, quotaPerUnit: number) {
  const rows = nestedArray(data, "subscriptions").length > 0
    ? nestedArray(data, "subscriptions")
    : nestedArray(data, "all_subscriptions");
  if (rows.length === 0) return null;

  let remainingQuota = 0;
  let usedQuota = 0;
  let totalQuota = 0;
  let hasRemaining = false;
  let hasUsed = false;
  let hasTotal = false;
  let planName = "";

  for (const row of rows) {
    if (!row || typeof row !== "object" || Array.isArray(row)) continue;
    const record = row as Record<string, unknown>;
    const subscription = nestedObject(record, "subscription") ?? record;
    const plan = nestedObject(record, "plan");

    const total = pickNumber(subscription, ["amount_total", "total_amount", "totalAmount", "amountTotal", "total_quota", "totalQuota", "total_granted", "totalGranted"]);
    const used = pickNumber(subscription, ["amount_used", "used_amount", "amountUsed", "usedAmount", "used_quota", "usedQuota", "used"]);
    const remaining = pickNumber(subscription, ["amount_remaining", "remaining_amount", "amountRemaining", "remainingAmount", "remain_quota", "remaining_quota", "remainingQuota", "balance", "remaining"]);
    const computedRemaining = remaining ?? (total !== null && used !== null && total > 0 ? Math.max(0, total - used) : null);

    if (!planName) {
      planName = pickString(plan, ["title", "name"]) || pickString(subscription, ["title", "name", "plan_name", "planName"]);
    }
    if (computedRemaining !== null) {
      remainingQuota += computedRemaining;
      hasRemaining = true;
    }
    if (used !== null) {
      usedQuota += used;
      hasUsed = true;
    }
    if (total !== null && total > 0) {
      totalQuota += total;
      hasTotal = true;
    }
  }

  if (!hasRemaining && !hasUsed && !hasTotal) return null;

  return successResult(accountId, "new-api", {
    planName: planName || "Subscription",
    remaining: hasRemaining ? newApiQuotaToUsd(remainingQuota, quotaPerUnit) : null,
    used: hasUsed ? newApiQuotaToUsd(usedQuota, quotaPerUnit) : null,
    total: hasTotal ? newApiQuotaToUsd(totalQuota, quotaPerUnit) : null,
    unit: "USD",
  }, checkedAt);
}

function parseSub2ApiUsage(accountId: number, payload: unknown, checkedAt: string): AccountBalanceResult | null {
  if (!payload || typeof payload !== "object") return null;
  const response = payload as Record<string, unknown>;
  if (typeof response.error === "string" && response.error.trim()) {
    return errorResult(accountId, response.error.trim(), checkedAt);
  }

  const credits = Array.isArray(response.ai_credits) ? response.ai_credits as Array<Record<string, unknown>> : [];
  if (credits.length > 0) {
    const total = credits.reduce((sum, item) => sum + (finiteNumber(item.amount) ?? 0), 0);
    return successResult(accountId, "sub2api-admin", { remaining: total, unit: "credits" }, checkedAt);
  }

  const windows = [
    ["5h", response.five_hour],
    ["7d", response.seven_day],
    ["7d Sonnet", response.seven_day_sonnet],
    ["1d", response.gemini_shared_daily],
    ["Pro", response.gemini_pro_daily],
    ["Flash", response.gemini_flash_daily],
  ] as const;
  const window = windows.find(([, value]) => value && typeof value === "object");
  if (!window) return null;

  const data = window[1] as Record<string, unknown>;
  const utilization = finiteNumber(data.utilization);
  const limitRequests = finiteNumber(data.limit_requests);
  const usedRequests = finiteNumber(data.used_requests);
  const remainingRequests = limitRequests !== null && usedRequests !== null ? Math.max(0, limitRequests - usedRequests) : null;
  const windowStats = data.window_stats && typeof data.window_stats === "object" ? data.window_stats as Record<string, unknown> : null;
  const cost = finiteNumber(windowStats?.cost);

  return successResult(accountId, "sub2api-admin", {
    planName: window[0],
    remaining: remainingRequests,
    used: cost,
    unit: remainingRequests !== null ? "req" : "%",
    message: utilization !== null ? `使用率 ${formatNumber(utilization)}%` : null,
  }, checkedAt);
}

function formatNumber(value: number) {
  if (Math.abs(value) >= 100) return value.toFixed(0);
  if (Math.abs(value) >= 1) return value.toFixed(2).replace(/\.?0+$/, "");
  return value.toFixed(4).replace(/\.?0+$/, "");
}

function compactError(error: unknown) {
  const message = error instanceof Error ? error.message : String(error);
  const compact = message.replace(/\s+/g, " ").trim();
  if (/^HTTP\s+403:/i.test(compact) && /cloudflare|Attention Required|cf-error|no-js ie6 oldie/i.test(compact)) {
    return "HTTP 403: 目标站点被 Cloudflare 拦截，当前服务器无法直接访问余额接口";
  }
  if (/^HTTP\s+\d+:/i.test(compact) && /<html|<!doctype/i.test(compact)) {
    const status = compact.match(/^HTTP\s+\d+/i)?.[0] ?? "HTTP 错误";
    return `${status}: 目标站点返回 HTML 页面，未返回可解析的余额 JSON`;
  }
  return compact.length > 240 ? `${compact.slice(0, 240)}...` : compact;
}

async function requestJson(url: string, headers: Record<string, string>, timeoutMs = 12_000) {
  const { status, body } = await requestText({
    method: "GET",
    url,
    headers,
    timeoutMs,
  });
  if (status < 200 || status >= 300) throw new Error(compactError(`HTTP ${status}: ${body.slice(0, 240)}`));
  if (!body.trim()) throw new Error("空响应");
  return unwrapJson(body);
}

function resolveNewApiQuotaPerUnit(baseUrl: string) {
  return newApiQuotaPerUnitCache.getOrCreate(baseUrl, () =>
    requestJson(`${baseUrl}/api/status`, {
      Accept: "application/json",
    }, 8_000)
      .then((payload) => quotaPerUnitFromPayload(payload) ?? DEFAULT_NEW_API_QUOTA_PER_UNIT)
      .catch(() => DEFAULT_NEW_API_QUOTA_PER_UNIT),
  );
}

function newApiUserHeaders(accessToken: string, userId = "") {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    Accept: "application/json",
  };
  const [rawToken, rawUserId] = accessToken.split("::", 2);
  const token = rawToken.trim();
  const resolvedUserId = userId || rawUserId || jwtUserId(token);

  if (token.startsWith("session:")) {
    headers.Cookie = token.slice("session:".length);
  } else if (token) {
    headers.Authorization = token.toLowerCase().startsWith("bearer ") ? token : `Bearer ${token}`;
  }
  if (resolvedUserId) headers["New-Api-User"] = resolvedUserId;
  return headers;
}

function normalizeNewApiCredentialToken(accessToken: string) {
  const rawAccess = accessToken.trim();
  const separatorIndex = rawAccess.indexOf("::");
  const rawToken = separatorIndex >= 0 ? rawAccess.slice(0, separatorIndex) : rawAccess;
  const rawUserId = separatorIndex >= 0 ? rawAccess.slice(separatorIndex + 2).trim() : "";
  let token = rawToken.trim();
  const lowerToken = token.toLowerCase();

  if (lowerToken.startsWith("cookie:")) token = token.slice(token.indexOf(":") + 1).trim();
  if (token.includes("=") && !token.startsWith("session:") && !lowerToken.startsWith("bearer ")) token = `session:${token}`;

  return rawUserId ? `${token}::${rawUserId}` : token;
}

async function queryNewApiBalance(
  accountId: number,
  baseUrl: string,
  accessToken: string,
  checkedAt: string,
  userId = "",
  sourceSiteName = "",
) {
  const normalizedToken = normalizeNewApiCredentialToken(accessToken);
  const quotaPerUnitPromise = resolveNewApiQuotaPerUnit(baseUrl);
  const unsupportedEndpoints: string[] = [];
  const endpointErrors: string[] = [];

  for (const endpoint of ["/api/subscription/self", "/api/user/self"] as const) {
    try {
      const [payload, quotaPerUnit] = await Promise.all([
        requestJson(`${baseUrl}${endpoint}`, newApiUserHeaders(normalizedToken, userId)),
        quotaPerUnitPromise,
      ]);
      const parsed = parseNewApiSelfResponse(accountId, payload, checkedAt, quotaPerUnit);
      if (parsed) return parsed;
      unsupportedEndpoints.push(endpoint);
    } catch (error) {
      endpointErrors.push(`${endpoint}: ${compactError(error)}`);
    }
  }

  if (endpointErrors.length > 0 && unsupportedEndpoints.length === 0) {
    throw new Error(endpointErrors.join("; "));
  }

  const prefix = sourceSiteName ? `New API 采集源「${sourceSiteName}」` : "New API";
  const details = [
    unsupportedEndpoints.length > 0 ? `${unsupportedEndpoints.join(" 和 ")} 响应中没有可解析余额字段` : "",
    endpointErrors.length > 0 ? `请求错误：${endpointErrors.join("; ")}` : "",
  ].filter(Boolean).join("；");
  return unsupportedResult(accountId, `${prefix} ${details || "余额响应不可解析"}`, checkedAt);
}

async function queryUsageEndpoint(accountId: number, credentials: Record<string, unknown>, checkedAt: string) {
  const baseUrl = normalizeBaseUrl(findCredentialString(credentials, ["base_url", "baseUrl", "endpoint", "url"]));
  const apiKey = findCredentialString(credentials, ["api_key", "apiKey", "key", "token", "access_token", "accessToken"]);
  if (!baseUrl || !apiKey) return null;

  const payload = await requestJson(`${baseUrl}/v1/usage`, {
    Authorization: `Bearer ${apiKey}`,
    Accept: "application/json",
  });
  return parseUsageLikeResponse(accountId, payload, checkedAt)
    ?? unsupportedResult(accountId, "usage 响应中没有 remaining/quota/balance 字段", checkedAt);
}

async function queryNewApiEndpoint(accountId: number, credentials: Record<string, unknown>, checkedAt: string) {
  const baseUrl = normalizeBaseUrl(findCredentialString(credentials, ["base_url", "baseUrl", "endpoint", "url"]));
  const accessToken = findCredentialString(credentials, ["user_access_token", "userAccessToken", "session", "session_token", "sessionToken", "cookie", "access_token", "accessToken"]);
  if (!baseUrl || !accessToken) return null;

  const userId = findCredentialString(credentials, ["user_id", "userId", "new_api_user", "newApiUser", "new_api_user_id", "newApiUserId", "new-api-user", "chatgpt_user_id", "chatgptUserId"]);
  return queryNewApiBalance(accountId, baseUrl, accessToken, checkedAt, userId);
}

async function queryNewApiSourceSession(accountId: number, session: NewApiSession | undefined, checkedAt: string) {
  if (!session) return null;

  return queryNewApiBalance(accountId, session.baseUrl, session.accessToken, checkedAt, "", session.sourceSiteName);
}

function accountByIdFromExport(accountIds: number[], accounts: Sub2ApiDataAccount[] | undefined, allowOrderFallback = true): ExportAccountMap {
  const map: ExportAccountMap = new Map();
  const rows = accounts ?? [];
  for (const account of rows) {
    const accountId = getAccountId(account);
    if (accountId && accountIds.includes(accountId)) map.set(accountId, account);
  }

  if (map.size > 0 || !allowOrderFallback || rows.length !== accountIds.length) return map;

  for (let index = 0; index < accountIds.length; index += 1) {
    map.set(accountIds[index], rows[index]);
  }
  return map;
}

function exportedAccounts(payload: Sub2ApiDataPayload | undefined) {
  return Array.isArray(payload?.accounts) ? payload.accounts : [];
}

function isNewApiAccount(account?: Sub2ApiDataAccount) {
  const platform = lowerString(account?.platform);
  const type = lowerString(account?.type);
  return platform.includes("new") || type.includes("new");
}

export function supportsSub2ApiUsage(account: Sub2ApiDataAccount | undefined, force: boolean) {
  if (!account) return false;
  const platform = lowerString(account.platform).replace(/[\s-]+/g, "_");
  const type = lowerString(account.type).replace(/[\s-]+/g, "_");
  const oauthOrSetupToken = type === "oauth" || type === "setup_token" || type === "setuptoken";

  if (!force) {
    return platform === "anthropic" && oauthOrSetupToken;
  }

  if (platform === "openai") return type === "oauth";
  if (platform === "anthropic") return oauthOrSetupToken;
  return platform === "gemini" || platform === "antigravity" || platform === "grok";
}

export function shouldProbeSub2ApiUsage(account: Sub2ApiDataAccount | undefined, force: boolean, exportIssue?: string) {
  return supportsSub2ApiUsage(account, force) || (!account && Boolean(exportIssue));
}

function exportMissingMessage(accountId: number, rows: Sub2ApiDataAccount[], mappedCount: number) {
  if (rows.length === 0) return "账号数据导出没有返回账号";
  return `账号数据导出无法定位账号 ${accountId}（返回 ${rows.length} 个账号，其中 ${mappedCount} 个带可识别 ID）`;
}

async function fetchNewApiSessionLookup(connectionId: number | undefined, accountIds: number[]): Promise<NewApiSessionLookup> {
  // Source authentication and session renewal belong to upstream channels.
  // The extension worker reads target-exported credentials only and never
  // reactivates the retired legacy collector login path.
  void connectionId;
  void accountIds;
  return new Map();
}

async function fetchExportAccountLookup(client: Sub2ApiAdminClient, accountIds: number[], concurrency: number): Promise<ExportAccountLookup> {
  const uniqueIds = Array.from(new Set(accountIds.filter((id) => Number.isInteger(id) && id > 0)));
  const accounts: ExportAccountMap = new Map();
  const issues = new Map<number, string>();
  if (uniqueIds.length === 0) return { accounts, issues };

  let exportedSuccessfully = false;
  try {
    const exported = await client.exportAccountsData(uniqueIds);
    exportedSuccessfully = true;
    for (const [accountId, account] of accountByIdFromExport(uniqueIds, exported.accounts, false)) {
      accounts.set(accountId, account);
    }

    const rows = exportedAccounts(exported);
    for (const accountId of uniqueIds) {
      if (!accounts.has(accountId)) issues.set(accountId, exportMissingMessage(accountId, rows, accounts.size));
    }
  } catch (error) {
    const message = `账号数据导出失败：${compactError(error)}`;
    for (const accountId of uniqueIds) issues.set(accountId, message);
  }

  const missingIds = uniqueIds.filter((accountId) => !accounts.has(accountId));
  if (missingIds.length === 0 || !exportedSuccessfully) return { accounts, issues };

  let cursor = 0;
  async function worker() {
    while (cursor < missingIds.length) {
      const accountId = missingIds[cursor];
      cursor += 1;

      try {
        const exported = await client.exportAccountsData([accountId]);
        const rows = exportedAccounts(exported);
        const singleMap = accountByIdFromExport([accountId], rows);
        const account = singleMap.get(accountId);
        if (account) {
          accounts.set(accountId, account);
          issues.delete(accountId);
        } else {
          issues.set(accountId, exportMissingMessage(accountId, rows, singleMap.size));
        }
      } catch (error) {
        const previous = issues.get(accountId);
        const retryMessage = `单账号导出重试失败：${compactError(error)}`;
        issues.set(accountId, previous ? `${previous}；${retryMessage}` : retryMessage);
      }
    }
  }

  await Promise.all(Array.from({ length: Math.min(concurrency, missingIds.length) }, () => worker()));
  return { accounts, issues };
}

async function queryOne(input: {
  client: Sub2ApiAdminClient;
  accountId: number;
  exportedAccount?: Sub2ApiDataAccount;
  exportIssue?: string;
  newApiSession?: NewApiSession;
  checkedAt: string;
  force: boolean;
}) {
  const { client, accountId, exportedAccount, exportIssue, newApiSession, checkedAt, force } = input;
  let usageError: AccountBalanceResult | null = null;

  if (shouldProbeSub2ApiUsage(exportedAccount, force, exportIssue)) {
    try {
      const usage = await client.getAccountUsage(accountId, force ? "active" : "passive", force);
      const parsed = parseSub2ApiUsage(accountId, usage, checkedAt);
      if (parsed?.status === "ok") return parsed;
      if (parsed?.status === "error") usageError = parsed;
    } catch {
      // Older Sub2API deployments may not expose account usage; fall back to upstream probing.
    }
  }

  if (newApiSession) {
    try {
      const result = await queryNewApiSourceSession(accountId, newApiSession, checkedAt);
      if (result) return result;
    } catch (error) {
      return errorResult(accountId, `New API 采集源「${newApiSession.sourceSiteName}」账户余额查询失败：${compactError(error)}`, checkedAt);
    }
  }

  const credentials = exportedAccount?.credentials;
  if (!credentials || typeof credentials !== "object" || Array.isArray(credentials) || Object.keys(credentials).length === 0) {
    if (usageError) return usageError;
    return unsupportedResult(accountId, exportIssue || "无法获取账号凭证，目标 Sub2API 可能不支持账号数据导出", checkedAt);
  }

  if (isNewApiAccount(exportedAccount)) {
    try {
      const result = await queryNewApiEndpoint(accountId, credentials, checkedAt);
      if (result) return result;
    } catch (error) {
      return errorResult(accountId, `New API 账号余额查询失败：${compactError(error)}`, checkedAt);
    }
    return unsupportedResult(accountId, "New API 账号未绑定可用采集源登录态，无法查询 /api/subscription/self 或 /api/user/self 账户余额", checkedAt);
  }

  const candidates = [queryUsageEndpoint, queryNewApiEndpoint];

  let lastError = "";
  for (const candidate of candidates) {
    try {
      const result = await candidate(accountId, credentials, checkedAt);
      if (result) return result;
    } catch (error) {
      lastError = error instanceof Error ? error.message : String(error);
    }
  }

  if (lastError) return errorResult(accountId, lastError, checkedAt);
  if (usageError) return usageError;
  return unsupportedResult(accountId, "缺少 base_url/api_key，或 New API 响应中没有可解析的余额字段", checkedAt);
}

export async function fetchAccountBalances(input: {
  client: Sub2ApiAdminClient;
  connectionId?: number;
  accountIds: number[];
  force?: boolean;
  concurrency?: number;
}) {
  const accountIds = Array.from(new Set(input.accountIds.filter((id) => Number.isInteger(id) && id > 0))).slice(0, 200);
  const checkedAt = new Date().toISOString();
  if (accountIds.length === 0) return [];

  const concurrency = Math.max(1, Math.min(input.concurrency ?? 4, 8));
  const results = new Map<number, AccountBalanceResult>();

  async function queryBatch(ids: number[], exportLookup?: ExportAccountLookup, newApiSessions?: NewApiSessionLookup) {
    let cursor = 0;
    async function worker() {
      while (cursor < ids.length) {
        const accountId = ids[cursor];
        cursor += 1;
        try {
          results.set(accountId, await queryOne({
            client: input.client,
            accountId,
            exportedAccount: exportLookup?.accounts.get(accountId),
            exportIssue: exportLookup?.issues.get(accountId),
            newApiSession: newApiSessions?.get(accountId),
            checkedAt,
            force: Boolean(input.force),
          }));
        } catch (error) {
          results.set(accountId, errorResult(accountId, error instanceof Error ? error.message : String(error), checkedAt));
        }
      }
    }

    await Promise.all(Array.from({ length: Math.min(concurrency, ids.length) }, () => worker()));
  }

  await queryBatch(accountIds);

  const fallbackAccountIds = accountIds.filter((accountId) => results.get(accountId)?.status === "unsupported");
  if (fallbackAccountIds.length > 0) {
    const [exportLookup, newApiSessions] = await Promise.all([
      fetchExportAccountLookup(input.client, fallbackAccountIds, concurrency),
      fetchNewApiSessionLookup(input.connectionId, fallbackAccountIds),
    ]);
    await queryBatch(fallbackAccountIds, exportLookup, newApiSessions);
  }

  return accountIds.map((accountId) => results.get(accountId) ?? errorResult(accountId, "查询失败", checkedAt));
}
