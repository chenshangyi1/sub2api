import type {
  Sub2ApiAdminClient,
  Sub2ApiChannelMonitorCreate,
  Sub2ApiDataAccount,
} from "@/server/clients/sub2api-admin";
import { selectMonitorModelCandidates } from "@/server/monitor-model-selection";
import { getAccountGroupIds } from "@/server/account-utils";

type MonitorRuleLike = {
  sub2apiChannelMonitorId?: number | null;
};

type StaleMonitorRuleLike = {
  id: number;
  accountId: number;
  sub2apiChannelMonitorId?: number | null;
};

const ownedMonitorGroupPattern = /^Sub2API #(\d+)$/;
const orphanMonitorGraceMs = 5 * 60 * 1000;
const maxMonitorListPages = 100;

type AccountRowLike = {
  id?: number | string | null;
  name?: string | null;
  username?: string | null;
  platform?: string | null;
  type?: string | null;
  channel_type?: string | null;
};

export type AccountProbeTarget = {
  accountId: number;
  accountName: string;
  provider: "openai" | "anthropic" | "gemini" | "grok";
  endpoint: string;
  apiKey: string;
  authScheme: "api-key" | "bearer";
};

export type ChannelMonitorSyncResult = {
  monitorId: number | null;
  created: boolean;
  updated: boolean;
  skipped: boolean;
  modelCandidates?: string[];
  message?: string;
};

export function selectAccountMonitorGroup(
  account: unknown,
  groups: Array<{ id: number; name: string }>,
  requestedGroupId: number,
) {
  const accountGroupIds = getAccountGroupIds(account);
  if (!accountGroupIds.includes(requestedGroupId)) {
    throw new Error(`账号不属于 Sub2API 分组 #${requestedGroupId}`);
  }
  const group = groups.find((item) => item.id === requestedGroupId);
  if (!group) throw new Error(`Sub2API 分组 #${requestedGroupId} 不存在`);
  return group;
}

function externalMonitorRef(connectionId: number, accountId: number) {
  return `ops:${connectionId}:account:${accountId}`;
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function numberValue(value: unknown) {
  const numeric = typeof value === "number" ? value : Number(value);
  return Number.isInteger(numeric) && numeric > 0 ? numeric : null;
}

function accountLabel(account: AccountRowLike | null | undefined, fallbackId: number) {
  const name = stringValue(account?.name) || stringValue(account?.username);
  return name || `#${fallbackId}`;
}

function normalizeProvider(platform?: string | null) {
  const value = stringValue(platform).toLowerCase();
  if (value === "openai" || value === "anthropic" || value === "gemini" || value === "grok") return value;
  if (value === "antigravity") return "gemini";
  return null;
}

function credentialString(credentials: Record<string, unknown> | null | undefined, keys: string[]) {
  for (const key of keys) {
    const value = stringValue(credentials?.[key]);
    if (value) return value;
  }
  return "";
}

function credentialEntry(credentials: Record<string, unknown> | null | undefined, keys: string[]) {
  for (const key of keys) {
    const value = stringValue(credentials?.[key]);
    if (value) return { key, value };
  }
  return null;
}

function defaultEndpoint(provider: AccountProbeTarget["provider"]) {
  if (provider === "anthropic") return "https://api.anthropic.com";
  if (provider === "gemini") return "https://generativelanguage.googleapis.com";
  if (provider === "grok") return "https://api.x.ai";
  return "https://api.openai.com";
}

function trimApiSuffix(endpoint: string) {
  return endpoint
    .replace(/\/+$/, "")
    .replace(/\/v1\/chat\/completions$/i, "")
    .replace(/\/v1\/responses$/i, "")
    .replace(/\/v1beta\/models$/i, "")
    .replace(/\/v1$/i, "");
}

export function isHttpNotFoundError(error: unknown) {
  return error instanceof Error && /HTTP 404\b/.test(error.message);
}

function pickExportedAccount(payloadAccounts: Sub2ApiDataAccount[] | undefined, accountId: number) {
  const accounts = payloadAccounts ?? [];
  return accounts.find((account) => {
    const id = numberValue(account.id) ?? numberValue(account.account_id) ?? numberValue(account.accountId);
    return id === accountId;
  }) ?? (accounts.length === 1 ? accounts[0] : null);
}

export async function resolveAccountProbeTarget(input: {
  client: Sub2ApiAdminClient;
  accountId: number;
  account?: AccountRowLike | null;
  accountName?: string | null;
}): Promise<AccountProbeTarget> {
  const exported = pickExportedAccount((await input.client.exportAccountsData([input.accountId])).accounts, input.accountId);
  const provider = normalizeProvider(input.account?.platform ?? exported?.platform);
  if (!provider) {
    throw new Error(`账号平台 ${input.account?.platform ?? exported?.platform ?? "unknown"} 不支持 API 能力探测`);
  }

  const credentials = exported?.credentials ?? null;
  const credential = credentialEntry(credentials, ["api_key", "apiKey", "key", "token", "access_token"]);
  if (!credential) {
    throw new Error("账号导出数据中没有可用于探测的 API 凭证");
  }

  const endpoint = trimApiSuffix(
    credentialString(credentials, ["base_url", "baseUrl", "endpoint", "api_base", "apiBase", "server_url"])
      || defaultEndpoint(provider),
  );

  return {
    accountId: input.accountId,
    accountName: stringValue(input.accountName) || accountLabel(input.account, input.accountId),
    provider,
    endpoint,
    apiKey: credential.value,
    authScheme: credential.key === "access_token" || credential.key === "token" ? "bearer" : "api-key",
  };
}

async function resolveMonitorModels(input: {
  client: Sub2ApiAdminClient;
  accountId: number;
  modelId?: string | null;
}) {
  const requested = stringValue(input.modelId);
  const models = await input.client.getAvailableModels(input.accountId).catch(() => []);
  const candidates = selectMonitorModelCandidates(models);
  if (candidates.length > 0) return candidates;
  if (requested) return [requested];
  throw new Error("无法同步到 Sub2API 原生监控：账号没有可用于自动探测的文本模型");
}

export async function getAutomaticMonitorModels(input: {
  client: Sub2ApiAdminClient;
  accountId: number;
  modelId?: string | null;
}) {
  return resolveMonitorModels(input);
}

export async function findOwnedChannelMonitor(input: {
  client: Pick<Sub2ApiAdminClient, "listChannelMonitorsPage">;
  connectionId: number;
  accountId: number;
}) {
  const marker = `Sub2API #${input.accountId}`;
  const externalRef = externalMonitorRef(input.connectionId, input.accountId);
  const seenMonitorIds = new Set<number>();
  for (let page = 1; page <= maxMonitorListPages; page += 1) {
    const result = await input.client.listChannelMonitorsPage({ page, pageSize: 100 });
    const match = result.items.find((monitor) => monitor.external_ref === externalRef)
      ?? result.items.find((monitor) => monitor.group_name === marker);
    if (match) return match;
    const unseen = result.items.filter((monitor) => !seenMonitorIds.has(monitor.id));
    for (const monitor of unseen) seenMonitorIds.add(monitor.id);
    if (result.items.length < 100 || unseen.length === 0) break;
  }
  return null;
}

async function buildChannelMonitorPayload(input: {
  client: Sub2ApiAdminClient;
  connectionId: number;
  accountId: number;
  account: AccountRowLike | null;
  accountName?: string | null;
  publicVisible: boolean;
  sub2apiGroupId: number;
  sub2apiGroupName: string;
  checkIntervalMinutes: number;
  modelId?: string | null;
}) {
  const target = await resolveAccountProbeTarget(input);
  const modelCandidates = await resolveMonitorModels({
    client: input.client,
    accountId: input.accountId,
    modelId: input.modelId,
  });
  const intervalSeconds = Math.max(15, Math.min(3600, input.checkIntervalMinutes * 60));
  const jitterSeconds = Math.max(0, Math.min(60, Math.floor(intervalSeconds / 10), intervalSeconds - 15));

  return {
    name: stringValue(input.accountName) || accountLabel(input.account, input.accountId),
    provider: target.provider,
    api_mode: "chat_completions",
    endpoint: target.endpoint,
    api_key: target.apiKey,
    primary_model: modelCandidates[0],
    extra_models: modelCandidates.slice(1),
    group_name: input.sub2apiGroupName,
    enabled: false,
    external_ref: externalMonitorRef(input.connectionId, input.accountId),
    public_visible: input.publicVisible,
    management_mode: "external",
    interval_seconds: intervalSeconds,
    jitter_seconds: jitterSeconds,
    body_override_mode: "off",
    body_override: {},
  } satisfies Sub2ApiChannelMonitorCreate;
}

export async function syncRuleToSub2ApiChannelMonitor(input: {
  client: Sub2ApiAdminClient;
  rule?: MonitorRuleLike | null;
  connectionId: number;
  accountId: number;
  account: AccountRowLike | null;
  accountName?: string | null;
  publicVisible: boolean;
  sub2apiGroupId: number;
  sub2apiGroupName: string;
  checkIntervalMinutes: number;
  modelId?: string | null;
}): Promise<ChannelMonitorSyncResult> {
  const payload = await buildChannelMonitorPayload(input);
  const monitorId = input.rule?.sub2apiChannelMonitorId ?? null;

  if (monitorId) {
    try {
      const updated = await input.client.updateChannelMonitor(monitorId, payload);
      return { monitorId: updated.id ?? monitorId, created: false, updated: true, skipped: false, modelCandidates: [payload.primary_model, ...(payload.extra_models ?? [])] };
    } catch (error) {
      if (!isHttpNotFoundError(error)) throw error;
      const created = await input.client.createChannelMonitor(payload);
      return {
        monitorId: created.id ?? null,
        created: true,
        updated: false,
        skipped: false,
        modelCandidates: [payload.primary_model, ...(payload.extra_models ?? [])],
        message: "原监控不存在或不可更新，已重新创建",
      };
    }
  }

  const created = await input.client.createChannelMonitor(payload);
  return { monitorId: created.id ?? null, created: true, updated: false, skipped: false, modelCandidates: [payload.primary_model, ...(payload.extra_models ?? [])] };
}

export async function deleteStaleSub2ApiChannelMonitors(input: {
  client: Pick<Sub2ApiAdminClient, "deleteChannelMonitor">;
  rules: StaleMonitorRuleLike[];
}) {
  const deletableRuleIds: number[] = [];
  const failedRuleIds: number[] = [];
  const errors: string[] = [];
  let deletedNativeMonitors = 0;

  for (const rule of input.rules) {
    const monitorId = rule.sub2apiChannelMonitorId ?? null;
    if (!monitorId) {
      deletableRuleIds.push(rule.id);
      continue;
    }

    try {
      await input.client.deleteChannelMonitor(monitorId);
      deletedNativeMonitors += 1;
      deletableRuleIds.push(rule.id);
    } catch (error) {
      if (isHttpNotFoundError(error)) {
        deletableRuleIds.push(rule.id);
        continue;
      }
      failedRuleIds.push(rule.id);
      const message = error instanceof Error ? error.message : String(error);
      errors.push(`account ${rule.accountId}, monitor ${monitorId}: ${message}`);
    }
  }

  return {
    deletableRuleIds,
    failedRuleIds,
    deletedNativeMonitors,
    errors,
  };
}

export async function deleteOrphanedSub2ApiChannelMonitors(input: {
  client: Pick<Sub2ApiAdminClient, "listChannelMonitors" | "deleteChannelMonitor">;
  rules: StaleMonitorRuleLike[];
  isReferenced?: (monitorId: number) => Promise<boolean>;
}) {
  const referencedMonitorIds = new Set(
    input.rules
      .map((rule) => rule.sub2apiChannelMonitorId ?? null)
      .filter((monitorId): monitorId is number => monitorId !== null),
  );
  const deletedMonitorIds: number[] = [];
  const errors: string[] = [];
  const seenMonitorIds = new Set<number>();
  const ownedOrphans: Array<{ id: number }> = [];

  for (let page = 1; page <= maxMonitorListPages; page += 1) {
    let monitors: Awaited<ReturnType<typeof input.client.listChannelMonitors>>;
    try {
      monitors = await input.client.listChannelMonitors({ page, pageSize: 100 });
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      errors.push(`list native monitors: ${message}`);
      break;
    }

    const unseenMonitors = monitors.filter((monitor) => !seenMonitorIds.has(monitor.id));
    for (const monitor of unseenMonitors) seenMonitorIds.add(monitor.id);

    for (const monitor of unseenMonitors) {
      const externallyOwned = monitor.management_mode === "external" && Boolean(monitor.external_ref?.startsWith("ops:"));
      if (!externallyOwned && !ownedMonitorGroupPattern.test(monitor.group_name ?? "")) continue;
      if (referencedMonitorIds.has(monitor.id)) continue;
      const createdAt = Date.parse(monitor.created_at ?? "");
      if (Number.isFinite(createdAt) && Date.now() - createdAt < orphanMonitorGraceMs) continue;
      ownedOrphans.push(monitor);
    }

    if (monitors.length < 100) break;
    if (unseenMonitors.length === 0) {
      errors.push(`list native monitors repeated page ${page}; cleanup incomplete`);
      break;
    }
    if (page === maxMonitorListPages) {
      errors.push(`list native monitors reached ${maxMonitorListPages} pages; cleanup incomplete`);
    }
  }

  for (const monitor of ownedOrphans) {
    try {
      if (await input.isReferenced?.(monitor.id)) continue;
      const result = await deleteSub2ApiChannelMonitor({
        client: input.client,
        monitorId: monitor.id,
      });
      if (!result.missing) deletedMonitorIds.push(monitor.id);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      errors.push(`monitor ${monitor.id}: ${message}`);
    }
  }

  return {
    deletedNativeMonitors: deletedMonitorIds.length,
    deletedMonitorIds,
    errors,
  };
}

export async function setSub2ApiChannelMonitorEnabled(input: {
  client: Sub2ApiAdminClient;
  monitorId?: number | null;
  enabled: boolean;
}) {
  if (!input.monitorId) return { monitorId: null, skipped: true };
  await input.client.updateChannelMonitor(input.monitorId, { enabled: input.enabled });
  return { monitorId: input.monitorId, skipped: false };
}

export async function deleteSub2ApiChannelMonitor(input: {
  client: Pick<Sub2ApiAdminClient, "deleteChannelMonitor">;
  monitorId?: number | null;
}) {
  if (!input.monitorId) return { monitorId: null, skipped: true, missing: false };
  try {
    await input.client.deleteChannelMonitor(input.monitorId);
    return { monitorId: input.monitorId, skipped: false, missing: false };
  } catch (error) {
    if (!isHttpNotFoundError(error)) throw error;
    return { monitorId: input.monitorId, skipped: false, missing: true };
  }
}
