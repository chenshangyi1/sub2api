import type { PrismaClient, UpstreamMonitorResult } from "@prisma/client";
import { randomUUID } from "node:crypto";
import { isCanonicalTargetDisabled, resolveCanonicalTarget } from "@/server/canonical-target";
import { Sub2ApiAdminClient } from "@/server/clients/sub2api-admin";
import { getAccountId, getAccountPriority } from "@/server/account-utils";
import { maxAccountPriority, minAccountPriority } from "@/server/account-priority-rule";
import { writeSyncLog } from "@/server/sync-logs";
import { isBalanceExhaustedAccountResult, scoreAccountHealthResult } from "@/server/account-health-classification";
import {
  getSub2ApiPassiveAccountHealth,
  type PassiveAccountHealthSample,
} from "@/server/sub2api-account-health";
import {
  getSub2ApiBurstTraffic,
  planBurstConcurrency,
  type AccountPoolBurstEvaluation,
  type BurstTrafficSample,
} from "@/server/account-pool-burst";

export type AccountPoolPolicy = {
  healthReturnEnabled: boolean;
  healthReturnThreshold: number;
  smartExpansionEnabled: boolean;
  totalConcurrency: number;
  minAccountConcurrency: number;
  maxAccountConcurrency: number;
  expansionLoadThresholdPct: number;
  burstExpansionEnabled: boolean;
  burstRpmThreshold: number;
  burstTotalConcurrency: number;
  burstMaxAccountConcurrency: number;
  burstStepPct: number;
  burstScaleDownThresholdPct: number;
  burstCooldownSeconds: number;
  loadFactorEnabled: boolean;
  totalLoadFactor: number;
  minAccountLoadFactor: number;
  maxAccountLoadFactor: number;
  loadFactorChangeThresholdPct: number;
  loadFactorCooldownSeconds: number;
  priorityEnabled: boolean;
  priceProtectionEnabled: boolean;
  failureDisableEnabled: boolean;
  failureWindow: number;
  failureCount: number;
  failureHealthThreshold: number;
  slowWindow: number;
  slowFirstTokenMs: number;
  slowCount: number;
  minAvailableAccounts: number;
  targetHealthyAccounts: number;
  excludedAccountIds: number[];
};

export type AccountHealth = {
  accountId: number;
  score: number | null;
  sampleCount: number;
  recentFailureCount: number;
  consecutiveFailureCount: number;
  recentSlowCount: number;
  averageFirstTokenMs: number | null;
  temporaryUnavailable?: boolean;
};

type PoolRuntimeState = {
  lastRunAt?: string | null;
  lastStatus?: "idle" | "success" | "failed";
  lastMessage?: string | null;
  lastLoadFactorWrites?: Record<string, string>;
  burstEligibleAccountIds?: number[];
  burst?: AccountPoolBurstEvaluation | null;
  evaluation?: AccountPoolPolicyEvaluation;
  actions?: {
    disabled: number;
    returned: number;
    concurrencyUpdated: number;
    loadFactorUpdated: number;
    priorityUpdated: number;
    failed: number;
  };
};

export type AccountPoolStrategyState =
  | "disabled"
  | "waiting_for_healthy"
  | "waiting_for_load"
  | "target_reached"
  | "account_limit_reached"
  | "balanced"
  | "cooling_down"
  | "adjusted"
  | "failed";

type AccountPoolHealthyEvaluation = {
  current: number;
  minimum: number;
  target: number;
  minimumMet: boolean;
  targetMet: boolean;
};

type AccountPoolExpansionEvaluation = {
  state: AccountPoolStrategyState;
  currentLoad: number;
  currentCapacity: number;
  utilizationPct: number;
  thresholdPct: number;
  targetCapacity: number;
  updatedAccounts: number;
  failedWrites: number;
};

type AccountPoolLoadFactorEvaluation = {
  state: AccountPoolStrategyState;
  currentTotal: number;
  targetTotal: number;
  changeCandidates: number;
  cooldownSkips: number;
  updatedAccounts: number;
  failedWrites: number;
};

type AccountPoolPriorityEvaluation = {
  state: AccountPoolStrategyState;
  changeCandidates: number;
  updatedAccounts: number;
  failedWrites: number;
};

export type AccountPoolPlatformEvaluation = {
  managedAccounts: number;
  schedulableAccounts: number;
  healthyPool: AccountPoolHealthyEvaluation;
  expansion: AccountPoolExpansionEvaluation;
  loadFactor: AccountPoolLoadFactorEvaluation;
  priority: AccountPoolPriorityEvaluation;
};

export type AccountPoolPolicyEvaluation = AccountPoolPlatformEvaluation & {
  monitoredHealthAccounts: number;
  passiveHealthAccounts: number;
  unknownHealthAccounts: number;
  platforms: Record<string, AccountPoolPlatformEvaluation>;
};

type RemotePoolAccount = {
  id: number;
  name: string;
  platform: string;
  schedulable: boolean;
  concurrency: number;
  currentConcurrency: number;
  loadFactor: number | null;
  priority: number | null;
  rateMultiplier: number;
  groupRateMultipliers: number[];
};

type AccountPoolPolicyClient = Pick<Sub2ApiAdminClient, "listAccounts" | "setSchedulable" | "updateAccount">;
type AccountPoolPolicyActions = {
  concurrencyUpdated: number;
  loadFactorUpdated: number;
  priorityUpdated: number;
  failed: number;
};
type PassiveHealthReader = (
  accountIds: number[],
  slowFirstTokenMs: number,
) => Promise<Map<number, PassiveAccountHealthSample>>;

type AccountDisableCandidate = {
  accountId: number;
  platform?: string;
  schedulable: boolean;
  health: AccountHealth;
};

const defaultAccountPoolPolicy: AccountPoolPolicy = {
  healthReturnEnabled: true,
  healthReturnThreshold: 75,
  smartExpansionEnabled: true,
  totalConcurrency: 900,
  minAccountConcurrency: 20,
  maxAccountConcurrency: 250,
  expansionLoadThresholdPct: 80,
  burstExpansionEnabled: true,
  burstRpmThreshold: 1_000,
  burstTotalConcurrency: 6_000,
  burstMaxAccountConcurrency: 1_000,
  burstStepPct: 20,
  burstScaleDownThresholdPct: 60,
  burstCooldownSeconds: 300,
  loadFactorEnabled: true,
  totalLoadFactor: 400,
  minAccountLoadFactor: 20,
  maxAccountLoadFactor: 500,
  loadFactorChangeThresholdPct: 10,
  loadFactorCooldownSeconds: 60,
  priorityEnabled: true,
  priceProtectionEnabled: true,
  failureDisableEnabled: true,
  failureWindow: 5,
  failureCount: 3,
  failureHealthThreshold: 60,
  slowWindow: 10,
  slowFirstTokenMs: 15_000,
  slowCount: 5,
  minAvailableAccounts: 1,
  targetHealthyAccounts: 3,
  excludedAccountIds: [],
};

function policyKey(connectionId: number) {
  return `account_pool_policy:${connectionId}`;
}

function runtimeKey(connectionId: number) {
  return `account_pool_policy_runtime:${connectionId}`;
}

export async function mutateAccountPoolRuntime(
  db: PrismaClient,
  connectionId: number,
  update: (current: PoolRuntimeState) => PoolRuntimeState,
) {
  const key = runtimeKey(connectionId);
  const setting = db.setting as unknown as {
    findUnique: (args: { where: { key: string } }) => Promise<{ value: string } | null>;
    updateMany?: (args: { where: { key: string; value: string }; data: { value: string } }) => Promise<{ count: number }>;
    create?: (args: { data: { key: string; value: string } }) => Promise<unknown>;
    upsert: (args: {
      where: { key: string };
      create: { key: string; value: string };
      update: { value: string };
    }) => Promise<unknown>;
  };
  for (let attempt = 0; attempt < 8; attempt += 1) {
    const row = await setting.findUnique({ where: { key } });
    const next = update(parseJson<PoolRuntimeState>(row?.value, {}));
    const value = JSON.stringify(next);
    if (!setting.updateMany) {
      await setting.upsert({ where: { key }, create: { key, value }, update: { value } });
      return next;
    }
    if (row) {
      const result = await setting.updateMany({ where: { key, value: row.value }, data: { value } });
      if (result.count === 1) return next;
      continue;
    }
    try {
      if (!setting.create) throw new Error("Runtime setting create is unavailable.");
      await setting.create({ data: { key, value } });
      return next;
    } catch (error) {
      if ((error as { code?: string }).code !== "P2002") throw error;
    }
  }
  throw new Error("Account pool runtime changed repeatedly; retry the policy run.");
}

function clampInteger(value: unknown, fallback: number, min: number, max: number) {
  const numeric = Number(value);
  if (!Number.isFinite(numeric)) return fallback;
  return Math.min(max, Math.max(min, Math.round(numeric)));
}

function booleanValue(value: unknown, fallback: boolean) {
  return typeof value === "boolean" ? value : fallback;
}

function uniquePositiveIntegers(value: unknown) {
  if (!Array.isArray(value)) return [];
  return Array.from(new Set(value
    .map((item) => Number(item))
    .filter((item) => Number.isInteger(item) && item > 0)))
    .sort((left, right) => left - right);
}

export function normalizeAccountPoolPolicy(input: unknown): AccountPoolPolicy {
  const value = input && typeof input === "object" ? input as Partial<AccountPoolPolicy> : {};
  const totalConcurrency = clampInteger(value.totalConcurrency, defaultAccountPoolPolicy.totalConcurrency, 1, 1_000_000);
  const minAccountConcurrency = clampInteger(value.minAccountConcurrency, defaultAccountPoolPolicy.minAccountConcurrency, 1, 100_000);
  const maxAccountConcurrency = clampInteger(value.maxAccountConcurrency, defaultAccountPoolPolicy.maxAccountConcurrency, minAccountConcurrency, 100_000);
  const burstMaxAccountConcurrency = clampInteger(value.burstMaxAccountConcurrency, defaultAccountPoolPolicy.burstMaxAccountConcurrency, maxAccountConcurrency, 100_000);
  const minAccountLoadFactor = clampInteger(value.minAccountLoadFactor, defaultAccountPoolPolicy.minAccountLoadFactor, 1, 100_000);
  const maxAccountLoadFactor = clampInteger(value.maxAccountLoadFactor, defaultAccountPoolPolicy.maxAccountLoadFactor, minAccountLoadFactor, 100_000);
  const failureWindow = clampInteger(value.failureWindow, defaultAccountPoolPolicy.failureWindow, 1, 60);
  const slowWindow = clampInteger(value.slowWindow, defaultAccountPoolPolicy.slowWindow, 1, 60);

  return {
    healthReturnEnabled: booleanValue(value.healthReturnEnabled, defaultAccountPoolPolicy.healthReturnEnabled),
    healthReturnThreshold: clampInteger(value.healthReturnThreshold, defaultAccountPoolPolicy.healthReturnThreshold, 1, 100),
    smartExpansionEnabled: booleanValue(value.smartExpansionEnabled, defaultAccountPoolPolicy.smartExpansionEnabled),
    totalConcurrency,
    minAccountConcurrency,
    maxAccountConcurrency,
    expansionLoadThresholdPct: clampInteger(value.expansionLoadThresholdPct, defaultAccountPoolPolicy.expansionLoadThresholdPct, 1, 100),
    burstExpansionEnabled: booleanValue(value.burstExpansionEnabled, defaultAccountPoolPolicy.burstExpansionEnabled),
    burstRpmThreshold: clampInteger(value.burstRpmThreshold, defaultAccountPoolPolicy.burstRpmThreshold, 1, 1_000_000),
    burstTotalConcurrency: clampInteger(value.burstTotalConcurrency, defaultAccountPoolPolicy.burstTotalConcurrency, totalConcurrency, 1_000_000),
    burstMaxAccountConcurrency,
    burstStepPct: clampInteger(value.burstStepPct, defaultAccountPoolPolicy.burstStepPct, 1, 100),
    burstScaleDownThresholdPct: clampInteger(value.burstScaleDownThresholdPct, defaultAccountPoolPolicy.burstScaleDownThresholdPct, 1, 99),
    burstCooldownSeconds: clampInteger(value.burstCooldownSeconds, defaultAccountPoolPolicy.burstCooldownSeconds, 0, 86_400),
    loadFactorEnabled: booleanValue(value.loadFactorEnabled, defaultAccountPoolPolicy.loadFactorEnabled),
    totalLoadFactor: clampInteger(value.totalLoadFactor, defaultAccountPoolPolicy.totalLoadFactor, 1, 1_000_000),
    minAccountLoadFactor,
    maxAccountLoadFactor,
    loadFactorChangeThresholdPct: clampInteger(value.loadFactorChangeThresholdPct, defaultAccountPoolPolicy.loadFactorChangeThresholdPct, 0, 100),
    loadFactorCooldownSeconds: clampInteger(value.loadFactorCooldownSeconds, defaultAccountPoolPolicy.loadFactorCooldownSeconds, 0, 86_400),
    priorityEnabled: booleanValue(value.priorityEnabled, defaultAccountPoolPolicy.priorityEnabled),
    priceProtectionEnabled: booleanValue(value.priceProtectionEnabled, defaultAccountPoolPolicy.priceProtectionEnabled),
    failureDisableEnabled: booleanValue(value.failureDisableEnabled, defaultAccountPoolPolicy.failureDisableEnabled),
    failureWindow,
    failureCount: clampInteger(value.failureCount, defaultAccountPoolPolicy.failureCount, 1, failureWindow),
    failureHealthThreshold: clampInteger(value.failureHealthThreshold, defaultAccountPoolPolicy.failureHealthThreshold, 1, 100),
    slowWindow,
    slowFirstTokenMs: clampInteger(value.slowFirstTokenMs, defaultAccountPoolPolicy.slowFirstTokenMs, 100, 600_000),
    slowCount: clampInteger(value.slowCount, defaultAccountPoolPolicy.slowCount, 1, slowWindow),
    minAvailableAccounts: clampInteger(value.minAvailableAccounts, defaultAccountPoolPolicy.minAvailableAccounts, 1, 10_000),
    targetHealthyAccounts: clampInteger(value.targetHealthyAccounts, defaultAccountPoolPolicy.targetHealthyAccounts, 1, 10_000),
    excludedAccountIds: uniquePositiveIntegers(value.excludedAccountIds),
  };
}

function resultScore(result: Pick<UpstreamMonitorResult, "status" | "message" | "firstTokenMs">, slowFirstTokenMs: number) {
  return scoreAccountHealthResult(result, slowFirstTokenMs);
}

export function calculateAccountHealth(
  accountId: number,
  results: Array<Pick<UpstreamMonitorResult, "status" | "message" | "firstTokenMs">>,
  policy: AccountPoolPolicy,
): AccountHealth {
  const recent = results.slice(0, 60);
  if (recent.length === 0) {
    return {
      accountId,
      score: null,
      sampleCount: 0,
      recentFailureCount: 0,
      consecutiveFailureCount: 0,
      recentSlowCount: 0,
      averageFirstTokenMs: null,
    };
  }
  const scores = recent.map((result) => resultScore(result, policy.slowFirstTokenMs));
  const shortScores = scores.slice(0, Math.min(10, scores.length));
  const failureWindow = recent.slice(0, policy.failureWindow);
  const firstSuccessIndex = failureWindow.findIndex((result) => result.status === "success");
  const consecutiveFailureCount = firstSuccessIndex === -1 ? failureWindow.length : firstSuccessIndex;
  const average = (values: number[]) => values.reduce((sum, value) => sum + value, 0) / Math.max(1, values.length);
  const score = Math.round((average(shortScores) * 0.7 + average(scores) * 0.3) * 10) / 10;
  const latencySamples = recent
    .filter((result) => result.status === "success" && result.firstTokenMs !== null && result.firstTokenMs >= 0)
    .slice(0, 10)
    .map((result) => result.firstTokenMs as number);
  return {
    accountId,
    score,
    sampleCount: recent.length,
    recentFailureCount: failureWindow.filter((result) => result.status !== "success").length,
    consecutiveFailureCount,
    recentSlowCount: recent.slice(0, policy.slowWindow).filter((result) => (
      result.firstTokenMs !== null && result.firstTokenMs > policy.slowFirstTokenMs
    )).length,
    averageFirstTokenMs: latencySamples.length ? Math.round(average(latencySamples)) : null,
    // Results are ordered newest first. A later successful check clears the
    // Sub2API runtime block, so historical balance exhaustion must not veto
    // future hard-failure handling.
    temporaryUnavailable: isBalanceExhaustedAccountResult(recent[0]),
  };
}

export function calculatePassiveAccountHealth(
  sample: PassiveAccountHealthSample,
  policy: AccountPoolPolicy,
): AccountHealth {
  const total = sample.gatewayErrors + sample.successes;
  if (total < 8) return calculateAccountHealth(sample.accountId, [], policy);

  const failureRate = sample.gatewayErrors / total;
  const bad = sample.gatewayErrors >= 10
    || (sample.gatewayErrors >= 6 && failureRate >= 0.25);
  const rawScore = Math.round((1 - failureRate) * 100);
  const score = bad
    ? policy.failureHealthThreshold
    : Math.max(policy.failureHealthThreshold, rawScore);

  return {
    accountId: sample.accountId,
    score,
    sampleCount: total,
    recentFailureCount: sample.gatewayErrors,
    consecutiveFailureCount: 0,
    recentSlowCount: sample.slowSuccesses,
    averageFirstTokenMs: sample.averageFirstTokenMs,
    temporaryUnavailable: false,
  };
}

export function mergeAccountHealth(monitor: AccountHealth, passive: AccountHealth): AccountHealth {
  if (monitor.score === null) return passive;
  if (passive.score === null) return monitor;
  return {
    accountId: monitor.accountId,
    score: Math.round((monitor.score * 0.35 + passive.score * 0.65) * 10) / 10,
    sampleCount: monitor.sampleCount + passive.sampleCount,
    recentFailureCount: Math.max(monitor.recentFailureCount, passive.recentFailureCount),
    consecutiveFailureCount: monitor.consecutiveFailureCount,
    recentSlowCount: Math.max(monitor.recentSlowCount, passive.recentSlowCount),
    averageFirstTokenMs: passive.averageFirstTokenMs ?? monitor.averageFirstTokenMs,
    temporaryUnavailable: Boolean(monitor.temporaryUnavailable || passive.temporaryUnavailable),
  };
}

export function shouldDisableAccount(health: AccountHealth, policy: AccountPoolPolicy) {
  if (!policy.failureDisableEnabled || health.score === null || health.temporaryUnavailable) return false;
  return (health.consecutiveFailureCount >= policy.failureCount && health.score < policy.failureHealthThreshold)
    || health.recentSlowCount >= policy.slowCount;
}

export function selectAccountsToDisable(candidates: AccountDisableCandidate[], policy: AccountPoolPolicy) {
  const pools = new Map<string, AccountDisableCandidate[]>();
  for (const candidate of candidates) {
    const platform = normalizeAccountPlatform(candidate.platform);
    const pool = pools.get(platform) ?? [];
    pool.push(candidate);
    pools.set(platform, pool);
  }
  return Array.from(pools.values()).flatMap((pool) => selectAccountsToDisableFromPool(pool, policy));
}

function selectAccountsToDisableFromPool(candidates: AccountDisableCandidate[], policy: AccountPoolPolicy) {
  const schedulableCount = candidates.filter((candidate) => candidate.schedulable).length;
  const disableLimit = Math.max(0, schedulableCount - policy.minAvailableAccounts);
  return candidates
    .filter((candidate) => candidate.schedulable && shouldDisableAccount(candidate.health, policy))
    .sort((left, right) => (
      (left.health.score ?? 101) - (right.health.score ?? 101)
      || right.health.consecutiveFailureCount - left.health.consecutiveFailureCount
      || right.health.recentSlowCount - left.health.recentSlowCount
      || left.accountId - right.accountId
    ))
    .slice(0, disableLimit)
    .map((candidate) => candidate.accountId);
}

export function meetsHealthyPoolTarget(healthyAccountCount: number, policy: AccountPoolPolicy) {
  return healthyAccountCount >= policy.targetHealthyAccounts;
}

export function meetsMinimumHealthyPool(healthyAccountCount: number, policy: AccountPoolPolicy) {
  return healthyAccountCount >= policy.minAvailableAccounts;
}

export function accountPriceProtectionFactor(account: Pick<RemotePoolAccount, "rateMultiplier" | "groupRateMultipliers">) {
  if (!(account.rateMultiplier > 0) || account.groupRateMultipliers.length === 0) return 1;
  const localRate = Math.min(...account.groupRateMultipliers.filter((rate) => Number.isFinite(rate) && rate >= 0));
  if (!Number.isFinite(localRate) || localRate >= account.rateMultiplier) return 1;
  return Math.max(0.01, localRate / account.rateMultiplier);
}

export function calculateAccountPriority(
  health: Pick<AccountHealth, "score" | "averageFirstTokenMs">,
  _priceFactor: number,
  slowFirstTokenMs: number,
  healthyThreshold = defaultAccountPoolPolicy.healthReturnThreshold,
) {
  const healthScore = Math.min(100, Math.max(0, health.score ?? 100));
  const latency = Math.max(0, health.averageFirstTokenMs ?? slowFirstTokenMs);
  const healthPenalty = Math.max(0, healthyThreshold - healthScore) * 100;
  const latencyPenalty = Math.max(0, latency - slowFirstTokenMs);

  // The gateway sorts by priority before its real-time load rate. Keep every
  // healthy account in tier 1 so load-aware selection can balance within it;
  // degraded/slow accounts are pushed to higher, lower-priority tiers. The
  // priority field is a compact 1..30 band, so this must never emit 0 or
  // unbounded values that the gateway would reject.
  const tier = Math.ceil((healthPenalty + latencyPenalty) / 25);
  return Math.min(maxAccountPriority, minAccountPriority + Math.max(0, tier));
}

export function allocateLoadFactors(
  accounts: Array<{ accountId: number; healthScore: number; priceFactor: number }>,
  policy: AccountPoolPolicy,
) {
  if (accounts.length === 0) return new Map<number, number>();
  const total = Math.min(
    accounts.length * policy.maxAccountLoadFactor,
    Math.max(accounts.length * policy.minAccountLoadFactor, policy.totalLoadFactor),
  );
  const weighted = accounts.map((account) => ({
    ...account,
    weight: Math.max(0.01, account.healthScore / 100) * Math.max(0.01, account.priceFactor),
  }));
  const weightTotal = weighted.reduce((sum, account) => sum + account.weight, 0);
  const allocation = new Map<number, number>();
  for (const account of weighted) {
    const target = Math.round(total * account.weight / weightTotal);
    allocation.set(account.accountId, Math.min(policy.maxAccountLoadFactor, Math.max(policy.minAccountLoadFactor, target)));
  }

  let difference = total - Array.from(allocation.values()).reduce((sum, value) => sum + value, 0);
  const direction = difference >= 0 ? 1 : -1;
  let guard = Math.abs(difference) * Math.max(1, weighted.length) + weighted.length;
  while (difference !== 0 && guard > 0) {
    for (const account of weighted) {
      if (difference === 0) break;
      const current = allocation.get(account.accountId) ?? policy.minAccountLoadFactor;
      const next = current + direction;
      if (next < policy.minAccountLoadFactor || next > policy.maxAccountLoadFactor) continue;
      allocation.set(account.accountId, next);
      difference -= direction;
    }
    guard -= weighted.length;
  }
  return allocation;
}

function parseJson<T>(value: string | null | undefined, fallback: T): T {
  if (!value) return fallback;
  try {
    return JSON.parse(value) as T;
  } catch {
    return fallback;
  }
}

export async function readAccountPoolPolicyConfiguration(db: PrismaClient, connectionId: number) {
  const row = await db.setting.findUnique({ where: { key: policyKey(connectionId) } });
  return {
    configured: Boolean(row),
    config: normalizeAccountPoolPolicy(parseJson(row?.value, {})),
  };
}

export async function readAccountPoolPolicy(db: PrismaClient, connectionId: number) {
  return (await readAccountPoolPolicyConfiguration(db, connectionId)).config;
}

export async function saveAccountPoolPolicy(db: PrismaClient, connectionId: number, input: unknown) {
  const policy = normalizeAccountPoolPolicy(input);
  await db.setting.upsert({
    where: { key: policyKey(connectionId) },
    create: { key: policyKey(connectionId), value: JSON.stringify(policy) },
    update: { value: JSON.stringify(policy) },
  });
  return policy;
}

function groupResults(results: UpstreamMonitorResult[]) {
  const grouped = new Map<number, UpstreamMonitorResult[]>();
  for (const result of results) {
    const current = grouped.get(result.accountId) ?? [];
    if (current.length < 60) current.push(result);
    grouped.set(result.accountId, current);
  }
  return grouped;
}

async function readHealth(db: PrismaClient, connectionId: number, accountIds: number[], policy: AccountPoolPolicy) {
  if (accountIds.length === 0) return new Map<number, AccountHealth>();
  const results = await db.upstreamMonitorResult.findMany({
    where: { connectionId, accountId: { in: accountIds } },
    orderBy: { createdAt: "desc" },
    take: Math.min(20_000, Math.max(60, accountIds.length * 60)),
  });
  const grouped = groupResults(results);
  return new Map(accountIds.map((accountId) => [
    accountId,
    calculateAccountHealth(accountId, grouped.get(accountId) ?? [], policy),
  ]));
}

export async function readAccountPoolPolicyStatus(db: PrismaClient, connectionId: number, policyOverride?: AccountPoolPolicy) {
  const policy = policyOverride ?? await readAccountPoolPolicy(db, connectionId);
  const rules = await db.upstreamMonitorRule.findMany({
    where: { connectionId, enabled: true },
    select: { accountId: true, pausedUntil: true, lastStatus: true },
  });
  const excluded = new Set(policy.excludedAccountIds);
  const managedRules = rules.filter((rule) => !excluded.has(rule.accountId));
  const healthByAccount = await readHealth(db, connectionId, managedRules.map((rule) => rule.accountId), policy);
  const scores = managedRules
    .map((rule) => healthByAccount.get(rule.accountId)?.score)
    .filter((score): score is number => score !== null && score !== undefined);
  const runtimeRow = await db.setting.findUnique({ where: { key: runtimeKey(connectionId) } });
  const runtime = parseJson<PoolRuntimeState>(runtimeRow?.value, {});
  const scoreHealthyAccounts = scores.filter((score) => score >= policy.failureHealthThreshold).length;
  return {
    managedAccounts: runtime.evaluation?.managedAccounts ?? managedRules.length,
    monitoredHealthAccounts: runtime.evaluation?.monitoredHealthAccounts ?? scores.length,
    passiveHealthAccounts: runtime.evaluation?.passiveHealthAccounts ?? 0,
    healthyAccounts: runtime.evaluation?.healthyPool.current ?? scoreHealthyAccounts,
    availableAccounts: runtime.evaluation?.schedulableAccounts
      ?? managedRules.filter((rule) => !rule.pausedUntil && rule.lastStatus === "success").length,
    pausedAccounts: managedRules.filter((rule) => Boolean(rule.pausedUntil)).length,
    unknownHealthAccounts: runtime.evaluation?.unknownHealthAccounts ?? managedRules.length - scores.length,
    averageHealthScore: scores.length ? Math.round(scores.reduce((sum, score) => sum + score, 0) / scores.length * 10) / 10 : null,
    excludedAccounts: policy.excludedAccountIds.length,
    lastRunAt: runtime.lastRunAt ?? null,
    lastStatus: runtime.lastStatus ?? "idle",
    lastMessage: runtime.lastMessage ?? null,
    burst: runtime.burst ?? null,
    lastEvaluation: runtime.evaluation ?? null,
    lastActions: runtime.actions ?? { disabled: 0, returned: 0, concurrencyUpdated: 0, loadFactorUpdated: 0, priorityUpdated: 0, failed: 0 },
  };
}

async function readPassiveHealth(
  accountIds: number[],
  policy: AccountPoolPolicy,
  reader: PassiveHealthReader,
) {
  try {
    const samples = await reader(accountIds, policy.slowFirstTokenMs);
    return new Map(accountIds.map((accountId) => [
      accountId,
      calculatePassiveAccountHealth(samples.get(accountId) ?? {
        accountId,
        gatewayErrors: 0,
        successes: 0,
        slowSuccesses: 0,
        averageFirstTokenMs: null,
      }, policy),
    ]));
  } catch {
    return new Map(accountIds.map((accountId) => [accountId, calculateAccountHealth(accountId, [], policy)]));
  }
}

function numberValue(value: unknown, fallback = 0) {
  const numeric = Number(value);
  return Number.isFinite(numeric) ? numeric : fallback;
}

function normalizeAccountPlatform(value: unknown) {
  const platform = typeof value === "string" ? value.trim().toLowerCase().replace(/[\s-]+/g, "_") : "";
  switch (platform) {
    case "claude":
      return "anthropic";
    case "google":
    case "google_gemini":
      return "gemini";
    default:
      return platform || "unknown";
  }
}

function groupAccountsByPlatform(accounts: RemotePoolAccount[]) {
  const pools = new Map<string, RemotePoolAccount[]>();
  for (const account of accounts) {
    const pool = pools.get(account.platform) ?? [];
    pool.push(account);
    pools.set(account.platform, pool);
  }
  return pools;
}

function normalizeRemoteAccount(value: unknown): RemotePoolAccount | null {
  if (!value || typeof value !== "object") return null;
  const record = value as Record<string, unknown>;
  const id = getAccountId(record);
  if (!id) return null;
  const groups = Array.isArray(record.groups) ? record.groups : [];
  return {
    id,
    name: typeof record.name === "string" && record.name.trim() ? record.name.trim() : `#${id}`,
    platform: normalizeAccountPlatform(record.platform),
    schedulable: record.schedulable !== false,
    concurrency: Math.max(0, Math.round(numberValue(record.concurrency))),
    currentConcurrency: Math.max(0, Math.round(numberValue(record.current_concurrency ?? record.currentConcurrency))),
    loadFactor: record.load_factor === null || record.load_factor === undefined ? null : Math.max(0, Math.round(numberValue(record.load_factor))),
    priority: getAccountPriority(record),
    rateMultiplier: Math.max(0, numberValue(record.rate_multiplier, 1)),
    groupRateMultipliers: groups
      .map((group) => group && typeof group === "object" ? numberValue((group as Record<string, unknown>).rate_multiplier, Number.NaN) : Number.NaN)
      .filter(Number.isFinite),
  };
}

function relativeChangePct(current: number | null, next: number) {
  if (current === null || current <= 0) return 100;
  return Math.abs(next - current) / current * 100;
}

function addMinutes(date: Date, minutes: number) {
  return new Date(date.getTime() + Math.max(1, minutes) * 60_000);
}

async function safeLog(db: PrismaClient, connectionId: number, detail: Record<string, unknown>, status: "success" | "failed", error?: string) {
  try {
    await writeSyncLog(db, {
      connectionId,
      action: "apply_account_pool_policy",
      target: `connection:${connectionId}`,
      detail,
      status,
      error,
    });
  } catch {
    // Runtime policy application should continue when file logging is unavailable.
  }
}

function aggregateStrategyState(states: AccountPoolStrategyState[]) {
  if (states.includes("failed")) return "failed";
  if (states.includes("adjusted")) return "adjusted";
  if (states.includes("cooling_down")) return "cooling_down";
  if (states.includes("waiting_for_healthy")) return "waiting_for_healthy";
  if (states.includes("account_limit_reached")) return "account_limit_reached";
  if (states.includes("waiting_for_load")) return "waiting_for_load";
  if (states.includes("target_reached")) return "target_reached";
  if (states.includes("balanced")) return "balanced";
  return "disabled";
}

async function applyAccountPoolPlatformStrategies(input: {
  accounts: RemotePoolAccount[];
  healthByAccount: Map<number, AccountHealth>;
  policy: AccountPoolPolicy;
  client: AccountPoolPolicyClient;
  now: Date;
  burstManagedIds: Set<number>;
  lastLoadFactorWrites: Record<string, string>;
}): Promise<{ evaluation: AccountPoolPlatformEvaluation; actions: AccountPoolPolicyActions }> {
  const { accounts, healthByAccount, policy, client, now, burstManagedIds, lastLoadFactorWrites } = input;
  const actions: AccountPoolPolicyActions = { concurrencyUpdated: 0, loadFactorUpdated: 0, priorityUpdated: 0, failed: 0 };
  const healthyAccounts = accounts
    .filter((account) => account.schedulable)
    .filter((account) => {
      const score = healthByAccount.get(account.id)?.score;
      return score === null || score === undefined || score >= policy.failureHealthThreshold;
    });
  const minimumHealthyMet = meetsMinimumHealthyPool(healthyAccounts.length, policy);
  const currentLoad = healthyAccounts.reduce((sum, account) => sum + account.currentConcurrency, 0);
  let currentCapacity = healthyAccounts.reduce((sum, account) => sum + account.concurrency, 0);
  let rawUtilizationPct = currentCapacity > 0 ? currentLoad / currentCapacity * 100 : 0;
  let utilizationPct = Math.round(rawUtilizationPct * 10) / 10;
  let expansionState: AccountPoolStrategyState = "disabled";
  let expansionFailedWrites = 0;

  if (policy.smartExpansionEnabled) {
    if (!minimumHealthyMet) {
      expansionState = "waiting_for_healthy";
    } else {
      for (const account of healthyAccounts) {
        const activeCeiling = burstManagedIds.has(account.id)
          ? policy.burstMaxAccountConcurrency
          : Math.max(policy.maxAccountConcurrency, account.concurrency);
        const target = Math.min(activeCeiling, Math.max(policy.minAccountConcurrency, account.concurrency));
        if (target === account.concurrency) continue;
        try {
          await client.updateAccount(account.id, { concurrency: target });
          account.concurrency = target;
          actions.concurrencyUpdated += 1;
        } catch {
          expansionFailedWrites += 1;
          actions.failed += 1;
        }
      }

      currentCapacity = healthyAccounts.reduce((sum, account) => sum + account.concurrency, 0);
      rawUtilizationPct = currentCapacity > 0 ? currentLoad / currentCapacity * 100 : 0;
      utilizationPct = Math.round(rawUtilizationPct * 10) / 10;
      if (currentCapacity >= policy.totalConcurrency) {
        expansionState = actions.concurrencyUpdated > 0 ? "adjusted" : "target_reached";
      } else if (rawUtilizationPct < policy.expansionLoadThresholdPct) {
        expansionState = actions.concurrencyUpdated > 0
          ? "adjusted"
          : expansionFailedWrites > 0 ? "failed" : "waiting_for_load";
      } else {
        let remaining = Math.max(0, policy.totalConcurrency - currentCapacity);
        const sorted = [...healthyAccounts].sort((left, right) => (
          (healthByAccount.get(right.id)?.score ?? policy.failureHealthThreshold)
          - (healthByAccount.get(left.id)?.score ?? policy.failureHealthThreshold)
        ));
        for (const account of sorted) {
          if (remaining <= 0) break;
          const target = Math.min(policy.maxAccountConcurrency, Math.max(account.concurrency, Math.ceil(Math.max(1, account.concurrency) * 1.1)));
          const next = Math.min(target, account.concurrency + remaining);
          if (next <= account.concurrency) continue;
          try {
            await client.updateAccount(account.id, { concurrency: next });
            remaining -= next - account.concurrency;
            account.concurrency = next;
            actions.concurrencyUpdated += 1;
          } catch {
            expansionFailedWrites += 1;
            actions.failed += 1;
          }
        }
        currentCapacity = healthyAccounts.reduce((sum, account) => sum + account.concurrency, 0);
        rawUtilizationPct = currentCapacity > 0 ? currentLoad / currentCapacity * 100 : 0;
        utilizationPct = Math.round(rawUtilizationPct * 10) / 10;
        expansionState = actions.concurrencyUpdated > 0
          ? "adjusted"
          : expansionFailedWrites > 0 ? "failed" : "account_limit_reached";
      }
    }
  }

  const currentLoadFactorTotal = healthyAccounts.reduce((sum, account) => sum + (account.loadFactor ?? 0), 0);
  let targetLoadFactorTotal = currentLoadFactorTotal;
  let loadFactorChangeCandidates = 0;
  let loadFactorCooldownSkips = 0;
  let loadFactorFailedWrites = 0;
  let loadFactorState: AccountPoolStrategyState = "disabled";
  if (policy.loadFactorEnabled) {
    if (!minimumHealthyMet) {
      loadFactorState = "waiting_for_healthy";
    } else {
      const allocations = allocateLoadFactors(healthyAccounts.map((account) => ({
        accountId: account.id,
        healthScore: healthByAccount.get(account.id)?.score ?? 100,
        priceFactor: policy.priceProtectionEnabled ? accountPriceProtectionFactor(account) : 1,
      })), policy);
      targetLoadFactorTotal = Array.from(allocations.values()).reduce((sum, value) => sum + value, 0);
      for (const account of healthyAccounts) {
        const target = allocations.get(account.id);
        if (target === undefined || relativeChangePct(account.loadFactor, target) < policy.loadFactorChangeThresholdPct) continue;
        loadFactorChangeCandidates += 1;
        const lastWriteAt = new Date(lastLoadFactorWrites[String(account.id)] ?? 0).getTime();
        if (Number.isFinite(lastWriteAt) && now.getTime() - lastWriteAt < policy.loadFactorCooldownSeconds * 1000) {
          loadFactorCooldownSkips += 1;
          continue;
        }
        try {
          await client.updateAccount(account.id, { load_factor: target });
          lastLoadFactorWrites[String(account.id)] = now.toISOString();
          account.loadFactor = target;
          actions.loadFactorUpdated += 1;
        } catch {
          loadFactorFailedWrites += 1;
          actions.failed += 1;
        }
      }
      loadFactorState = actions.loadFactorUpdated > 0
        ? "adjusted"
        : loadFactorFailedWrites > 0
          ? "failed"
          : loadFactorChangeCandidates > 0 && loadFactorCooldownSkips === loadFactorChangeCandidates
            ? "cooling_down"
            : "balanced";
    }
  }

  let priorityChangeCandidates = 0;
  let priorityFailedWrites = 0;
  let priorityState: AccountPoolStrategyState = "disabled";
  if (policy.priorityEnabled) {
    if (!minimumHealthyMet) {
      priorityState = "waiting_for_healthy";
    } else {
      for (const account of healthyAccounts) {
        const health = healthByAccount.get(account.id) ?? {
          accountId: account.id,
          score: null,
          sampleCount: 0,
          recentFailureCount: 0,
          consecutiveFailureCount: 0,
          recentSlowCount: 0,
          averageFirstTokenMs: null,
        };
        const priceFactor = policy.priceProtectionEnabled ? accountPriceProtectionFactor(account) : 1;
        const target = calculateAccountPriority({
          score: health.score,
          averageFirstTokenMs: health.averageFirstTokenMs,
        }, priceFactor, policy.slowFirstTokenMs, policy.healthReturnThreshold);
        if (account.priority === target) continue;
        priorityChangeCandidates += 1;
        try {
          await client.updateAccount(account.id, { priority: target });
          account.priority = target;
          actions.priorityUpdated += 1;
        } catch {
          priorityFailedWrites += 1;
          actions.failed += 1;
        }
      }
      priorityState = actions.priorityUpdated > 0
        ? "adjusted"
        : priorityFailedWrites > 0 ? "failed" : "balanced";
    }
  }

  return {
    actions,
    evaluation: {
      managedAccounts: accounts.length,
      schedulableAccounts: accounts.filter((account) => account.schedulable).length,
      healthyPool: {
        current: healthyAccounts.length,
        minimum: policy.minAvailableAccounts,
        target: policy.targetHealthyAccounts,
        minimumMet: minimumHealthyMet,
        targetMet: meetsHealthyPoolTarget(healthyAccounts.length, policy),
      },
      expansion: {
        state: expansionState,
        currentLoad,
        currentCapacity,
        utilizationPct,
        thresholdPct: policy.expansionLoadThresholdPct,
        targetCapacity: policy.totalConcurrency,
        updatedAccounts: actions.concurrencyUpdated,
        failedWrites: expansionFailedWrites,
      },
      loadFactor: {
        state: loadFactorState,
        currentTotal: currentLoadFactorTotal,
        targetTotal: targetLoadFactorTotal,
        changeCandidates: loadFactorChangeCandidates,
        cooldownSkips: loadFactorCooldownSkips,
        updatedAccounts: actions.loadFactorUpdated,
        failedWrites: loadFactorFailedWrites,
      },
      priority: {
        state: priorityState,
        changeCandidates: priorityChangeCandidates,
        updatedAccounts: actions.priorityUpdated,
        failedWrites: priorityFailedWrites,
      },
    },
  };
}

export async function runAccountPoolPolicyForConnection(
  db: PrismaClient,
  connectionId: number,
  now = new Date(),
  clientOverride?: AccountPoolPolicyClient,
  passiveHealthReader: PassiveHealthReader = getSub2ApiPassiveAccountHealth,
) {
  const configuration = await readAccountPoolPolicyConfiguration(db, connectionId);
  if (!configuration.configured) {
    throw new Error("Account pool policy is not configured. Save it before execution.");
  }
  const policy = configuration.config;
  let client = clientOverride;
  if (!client) {
    const target = await resolveCanonicalTarget(db, connectionId);
    client = new Sub2ApiAdminClient(target.baseUrl, target.adminApiKey);
  }
  const [accountPayload, rules, runtimeRow] = await Promise.all([
    client.listAccounts(),
    db.upstreamMonitorRule.findMany({ where: { connectionId, enabled: true } }),
    db.setting.findUnique({ where: { key: runtimeKey(connectionId) } }),
  ]);
  const accounts = (Array.isArray(accountPayload) ? accountPayload : [])
    .map(normalizeRemoteAccount)
    .filter((account): account is RemotePoolAccount => account !== null);
  const accountById = new Map(accounts.map((account) => [account.id, account]));
  const excluded = new Set(policy.excludedAccountIds);
  const managedAccounts = accounts.filter((account) => !excluded.has(account.id));
  const eligibleRules = rules.filter((rule) => accountById.has(rule.accountId) && !excluded.has(rule.accountId));
  const managedAccountIds = managedAccounts.map((account) => account.id);
  const [monitorHealthByAccount, passiveHealthByAccount] = await Promise.all([
    readHealth(db, connectionId, managedAccountIds, policy),
    readPassiveHealth(managedAccountIds, policy, passiveHealthReader),
  ]);
  const healthByAccount = new Map(managedAccountIds.map((accountId) => [
    accountId,
    mergeAccountHealth(
      monitorHealthByAccount.get(accountId) ?? calculateAccountHealth(accountId, [], policy),
      passiveHealthByAccount.get(accountId) ?? calculateAccountHealth(accountId, [], policy),
    ),
  ]));
  const runtime = parseJson<PoolRuntimeState>(runtimeRow?.value, {});
  const burstManagedIds = new Set(Object.keys(runtime.burst?.managedConcurrencyBaselines ?? {}).map(Number));
  const lastLoadFactorWrites = { ...(runtime.lastLoadFactorWrites ?? {}) };
  const actions = { disabled: 0, returned: 0, concurrencyUpdated: 0, loadFactorUpdated: 0, priorityUpdated: 0, failed: 0 };
  const disableAccountIds = new Set(selectAccountsToDisable(eligibleRules.flatMap((rule) => {
    const account = accountById.get(rule.accountId);
    const health = monitorHealthByAccount.get(rule.accountId);
    return account && health ? [{ accountId: account.id, platform: account.platform, schedulable: account.schedulable, health }] : [];
  }), policy));

  for (const rule of eligibleRules) {
    const account = accountById.get(rule.accountId);
    const health = monitorHealthByAccount.get(rule.accountId);
    if (!account || !health) continue;
    try {
      if (disableAccountIds.has(account.id)) {
        const previousPauseState = {
          pausedUntil: rule.pausedUntil,
          pauseStartedAt: rule.pauseStartedAt,
          nextCheckAt: rule.nextCheckAt,
        };
        await db.upstreamMonitorRule.update({
          where: { id: rule.id },
          data: { pausedUntil: addMinutes(now, rule.pauseMinutes), pauseStartedAt: now, nextCheckAt: now },
        });
        try {
          await client.setSchedulable(account.id, false);
        } catch (error) {
          await db.upstreamMonitorRule.update({ where: { id: rule.id }, data: previousPauseState });
          throw error;
        }
        account.schedulable = false;
        actions.disabled += 1;
      } else if (
        policy.healthReturnEnabled
        && !account.schedulable
        && Boolean(rule.pausedUntil)
        && health.score !== null
        && health.score >= policy.healthReturnThreshold
        && rule.lastStatus === "success"
      ) {
        const previousPauseState = {
          pausedUntil: rule.pausedUntil,
          pauseStartedAt: rule.pauseStartedAt,
          nextCheckAt: rule.nextCheckAt,
        };
        await db.upstreamMonitorRule.update({
          where: { id: rule.id },
          data: { pausedUntil: null, pauseStartedAt: null },
        });
        try {
          await client.setSchedulable(account.id, true);
        } catch (error) {
          await db.upstreamMonitorRule.update({ where: { id: rule.id }, data: previousPauseState });
          throw error;
        }
        account.schedulable = true;
        actions.returned += 1;
      }
    } catch {
      actions.failed += 1;
    }
  }

  const platformEvaluations: Record<string, AccountPoolPlatformEvaluation> = {};
  const platformPools = Array.from(groupAccountsByPlatform(managedAccounts).entries())
    .sort(([left], [right]) => left.localeCompare(right));
  for (const [platform, platformAccounts] of platformPools) {
    const result = await applyAccountPoolPlatformStrategies({
      accounts: platformAccounts,
      healthByAccount,
      policy,
      client,
      now,
      burstManagedIds,
      lastLoadFactorWrites,
    });
    platformEvaluations[platform] = result.evaluation;
    actions.concurrencyUpdated += result.actions.concurrencyUpdated;
    actions.loadFactorUpdated += result.actions.loadFactorUpdated;
    actions.priorityUpdated += result.actions.priorityUpdated;
    actions.failed += result.actions.failed;
  }

  const healthyAccounts = managedAccounts
    .filter((account) => account.schedulable)
    .filter((account) => {
      const score = healthByAccount.get(account.id)?.score;
      return score === null || score === undefined || score >= policy.failureHealthThreshold;
    });
  const platformResults = Object.values(platformEvaluations);
  const platformCount = platformResults.length;
  const currentLoad = platformResults.reduce((sum, value) => sum + value.expansion.currentLoad, 0);
  const currentCapacity = platformResults.reduce((sum, value) => sum + value.expansion.currentCapacity, 0);
  const utilizationPct = currentCapacity > 0 ? Math.round(currentLoad / currentCapacity * 1000) / 10 : 0;
  const currentLoadFactorTotal = platformResults.reduce((sum, value) => sum + value.loadFactor.currentTotal, 0);
  const targetLoadFactorTotal = platformResults.reduce((sum, value) => sum + value.loadFactor.targetTotal, 0);
  const evaluation: AccountPoolPolicyEvaluation = {
    managedAccounts: managedAccounts.length,
    monitoredHealthAccounts: Array.from(monitorHealthByAccount.values()).filter((health) => health.score !== null).length,
    passiveHealthAccounts: Array.from(passiveHealthByAccount.values()).filter((health) => health.score !== null).length,
    unknownHealthAccounts: managedAccounts.filter((account) => healthByAccount.get(account.id)?.score === null).length,
    schedulableAccounts: managedAccounts.filter((account) => account.schedulable).length,
    platforms: platformEvaluations,
    healthyPool: {
      current: healthyAccounts.length,
      minimum: policy.minAvailableAccounts * platformCount,
      target: policy.targetHealthyAccounts * platformCount,
      minimumMet: platformCount > 0 && platformResults.every((value) => value.healthyPool.minimumMet),
      targetMet: platformCount > 0 && platformResults.every((value) => value.healthyPool.targetMet),
    },
    expansion: {
      state: aggregateStrategyState(platformResults.map((value) => value.expansion.state)),
      currentLoad,
      currentCapacity,
      utilizationPct,
      thresholdPct: policy.expansionLoadThresholdPct,
      targetCapacity: policy.totalConcurrency * platformCount,
      updatedAccounts: actions.concurrencyUpdated,
      failedWrites: platformResults.reduce((sum, value) => sum + value.expansion.failedWrites, 0),
    },
    loadFactor: {
      state: aggregateStrategyState(platformResults.map((value) => value.loadFactor.state)),
      currentTotal: currentLoadFactorTotal,
      targetTotal: targetLoadFactorTotal,
      changeCandidates: platformResults.reduce((sum, value) => sum + value.loadFactor.changeCandidates, 0),
      cooldownSkips: platformResults.reduce((sum, value) => sum + value.loadFactor.cooldownSkips, 0),
      updatedAccounts: actions.loadFactorUpdated,
      failedWrites: platformResults.reduce((sum, value) => sum + value.loadFactor.failedWrites, 0),
    },
    priority: {
      state: aggregateStrategyState(platformResults.map((value) => value.priority.state)),
      changeCandidates: platformResults.reduce((sum, value) => sum + value.priority.changeCandidates, 0),
      updatedAccounts: actions.priorityUpdated,
      failedWrites: platformResults.reduce((sum, value) => sum + value.priority.failedWrites, 0),
    },
  };

  const status = actions.failed > 0 ? "failed" : "success";
  const message = `managed=${managedAccounts.length}, platforms=${platformCount}, monitored=${evaluation.monitoredHealthAccounts}, passive=${evaluation.passiveHealthAccounts}, unknown=${evaluation.unknownHealthAccounts}, healthy=${healthyAccounts.length}/${evaluation.healthyPool.target}, minimum=${healthyAccounts.length}/${evaluation.healthyPool.minimum}, expansion=${evaluation.expansion.state}(${utilizationPct}/${policy.expansionLoadThresholdPct}%), load_factor=${evaluation.loadFactor.state}, priority=${evaluation.priority.state}, disabled=${actions.disabled}, returned=${actions.returned}, concurrency=${actions.concurrencyUpdated}, load_factor_writes=${actions.loadFactorUpdated}, priority_writes=${actions.priorityUpdated}, failed=${actions.failed}`;
  await mutateAccountPoolRuntime(db, connectionId, (latest) => ({
    ...latest,
    lastRunAt: now.toISOString(),
    lastStatus: status,
    lastMessage: message,
    lastLoadFactorWrites,
    burstEligibleAccountIds: healthyAccounts.map((account) => account.id),
    burst: latest.burst ?? runtime.burst ?? null,
    evaluation,
    actions,
  }));
  await safeLog(db, connectionId, { policy, actions, evaluation, managedAccounts: managedAccounts.length, monitoredAccounts: eligibleRules.length, healthyAccounts: healthyAccounts.length }, status, actions.failed ? message : undefined);
  return { ok: actions.failed === 0, message, actions, evaluation };
}

export async function runAccountPoolPolicies(
  db: PrismaClient,
  now = new Date(),
  shouldStop?: () => boolean,
  targetId?: number,
) {
  // 执行权移交：settings account_pool_execution=delegated 时，
  // 池策略执行由 sub2api 内置健康调度器负责，worker 只保留可视化/调参。
  const execMode = await db.setting.findUnique({ where: { key: "account_pool_execution" }, select: { value: true } });
  if (execMode?.value?.trim().toLowerCase() === "delegated") {
    return [];
  }
  const configuredRows = await db.setting.findMany({
    where: { key: { startsWith: "account_pool_policy:" } },
    select: { key: true },
  });
  const configuredConnectionIds = configuredRows
    .map((row) => Number(row.key.slice("account_pool_policy:".length)))
    .filter((id) => Number.isInteger(id) && id > 0)
    .filter((id) => targetId === undefined || id === targetId);
  if (configuredConnectionIds.length === 0) return [];
  const results: Array<{ connectionId: number; ok: boolean; message: string }> = [];
  for (const connectionId of configuredConnectionIds) {
    if (shouldStop?.()) break;
    try {
      const target = await resolveCanonicalTarget(db, connectionId);
      const client = new Sub2ApiAdminClient(target.baseUrl, target.adminApiKey);
      const result = await runAccountPoolPolicyForConnection(db, connectionId, now, client);
      results.push({ connectionId, ok: result.ok, message: result.message });
    } catch (error) {
      if (isCanonicalTargetDisabled(error)) continue;
      const message = error instanceof Error ? error.message : String(error);
      await mutateAccountPoolRuntime(db, connectionId, (runtime) => ({
        ...runtime,
        lastRunAt: now.toISOString(),
        lastStatus: "failed",
        lastMessage: message,
      }));
      await safeLog(db, connectionId, {}, "failed", message);
      results.push({ connectionId, ok: false, message });
    }
  }
  return results;
}

type BurstTrafficReader = (accountIds: number[]) => Promise<BurstTrafficSample>;

function burstOwnershipTokenIsStale(token: string | undefined, now: Date) {
  if (!token) return false;
  const createdAt = Number(token.split(":", 1)[0]);
  return !Number.isFinite(createdAt) || now.getTime() - createdAt > 120_000;
}

function burstTrafficForAccounts(
  traffic: BurstTrafficSample,
  accountIds: number[],
  preserveAggregate: boolean,
): BurstTrafficSample {
  if (preserveAggregate) return traffic;
  const ids = new Set(accountIds.map(String));
  const accountRequestsPerMinute = Object.fromEntries(
    Object.entries(traffic.accountRequestsPerMinute).filter(([accountId]) => ids.has(accountId)),
  );
  const requestsPerMinute = Object.values(accountRequestsPerMinute).reduce((sum, rpm) => sum + rpm, 0);
  return {
    requestsPerMinute,
    completedRequestsLastMinute: requestsPerMinute,
    accountRequestsPerMinute,
    degradedAccountIds: traffic.degradedAccountIds.filter((accountId) => ids.has(String(accountId))),
  };
}

function aggregateBurstState(states: AccountPoolBurstEvaluation["state"][]) {
  const priority: AccountPoolBurstEvaluation["state"][] = [
    "failed",
    "expanding",
    "shrinking",
    "holding",
    "cooling_down",
    "hard_limit_reached",
    "waiting_for_saturation",
    "waiting_for_rpm",
    "waiting_for_healthy",
    "disabled",
  ];
  return priority.find((state) => states.includes(state)) ?? "disabled";
}

function planBurstConcurrencyByPlatform(input: {
  accounts: RemotePoolAccount[];
  eligibleAccountIds: number[];
  traffic: BurstTrafficSample;
  policy: AccountPoolPolicy;
  previous?: AccountPoolBurstEvaluation | null;
  now: Date;
}) {
  const eligibleIds = new Set(input.eligibleAccountIds);
  const managedIds = new Set(Object.keys(input.previous?.managedConcurrencyBaselines ?? {}).map(Number));
  const platformPools = Array.from(groupAccountsByPlatform(input.accounts).entries())
    .map(([platform, accounts]) => ({
      platform,
      accounts,
      controlledIds: accounts
        .filter((account) => eligibleIds.has(account.id) || managedIds.has(account.id))
        .map((account) => account.id),
    }))
    .filter((pool) => pool.controlledIds.length > 0)
    .sort((left, right) => left.platform.localeCompare(right.platform));

  if (platformPools.length === 0) {
    return planBurstConcurrency(input);
  }

  const updates: ReturnType<typeof planBurstConcurrency>["updates"] = [];
  const platforms: Record<string, AccountPoolBurstEvaluation> = {};
  const plannedPools: Array<{
    platform: string;
    plan: ReturnType<typeof planBurstConcurrency>;
  }> = [];
  for (const pool of platformPools) {
    const platformPrevious = input.previous?.platforms?.[pool.platform];
    const controlledIds = new Set(pool.controlledIds.map(String));
    const previous = platformPrevious ? {
      ...platformPrevious,
      managedConcurrencyBaselines: Object.fromEntries(
        Object.entries(input.previous?.managedConcurrencyBaselines ?? {})
          .filter(([accountId]) => controlledIds.has(accountId)),
      ),
      managedConcurrencyTokens: Object.fromEntries(
        Object.entries(input.previous?.managedConcurrencyTokens ?? {})
          .filter(([accountId]) => controlledIds.has(accountId)),
      ),
    } : input.previous;
    const plan = planBurstConcurrency({
      accounts: pool.accounts,
      eligibleAccountIds: input.eligibleAccountIds.filter((accountId) => pool.controlledIds.includes(accountId)),
      traffic: burstTrafficForAccounts(input.traffic, pool.controlledIds, platformPools.length === 1),
      policy: input.policy,
      previous,
      now: input.now,
    });
    updates.push(...plan.updates);
    plannedPools.push({
      platform: pool.platform,
      plan,
    });
    platforms[pool.platform] = {
      ...plan.evaluation,
      managedConcurrencyBaselines: undefined,
      managedConcurrencyTokens: undefined,
    };
  }

  const evaluations = Object.values(platforms);
  const latestHighLoadAt = evaluations
    .map((evaluation) => evaluation.lastHighLoadAt)
    .filter((value): value is string => Boolean(value))
    .sort()
    .at(-1) ?? null;
  const managedConcurrencyBaselines: Record<string, number> = {};
  for (const planned of plannedPools) {
    Object.assign(managedConcurrencyBaselines, planned.plan.evaluation.managedConcurrencyBaselines);
  }

  return {
    updates,
    evaluation: {
      state: aggregateBurstState(evaluations.map((evaluation) => evaluation.state)),
      active: evaluations.some((evaluation) => evaluation.active),
      requestsPerMinute: evaluations.reduce((sum, evaluation) => sum + evaluation.requestsPerMinute, 0),
      completedRequestsLastMinute: evaluations.reduce((sum, evaluation) => sum + evaluation.completedRequestsLastMinute, 0),
      rpmThreshold: input.policy.burstRpmThreshold,
      currentLoad: evaluations.reduce((sum, evaluation) => sum + evaluation.currentLoad, 0),
      currentCapacity: evaluations.reduce((sum, evaluation) => sum + evaluation.currentCapacity, 0),
      peakUtilizationPct: evaluations.reduce((peak, evaluation) => Math.max(peak, evaluation.peakUtilizationPct), 0),
      thresholdPct: input.policy.expansionLoadThresholdPct,
      regularAccountLimit: input.policy.maxAccountConcurrency,
      burstAccountLimit: input.policy.burstMaxAccountConcurrency,
      burstTotalLimit: input.policy.burstTotalConcurrency * evaluations.length,
      eligibleAccounts: evaluations.reduce((sum, evaluation) => sum + evaluation.eligibleAccounts, 0),
      degradedAccounts: evaluations.reduce((sum, evaluation) => sum + evaluation.degradedAccounts, 0),
      cooldownRemainingSeconds: evaluations.reduce((max, evaluation) => Math.max(max, evaluation.cooldownRemainingSeconds), 0),
      lastHighLoadAt: latestHighLoadAt,
      lastCheckedAt: input.now.toISOString(),
      updatedAccounts: updates.length,
      failedWrites: 0,
      platforms,
      managedConcurrencyBaselines,
      managedConcurrencyTokens: { ...(input.previous?.managedConcurrencyTokens ?? {}) },
    } satisfies AccountPoolBurstEvaluation,
  };
}

export async function runRapidAccountPoolBurstForConnection(
  db: PrismaClient,
  connectionId: number,
  now = new Date(),
  clientOverride?: AccountPoolPolicyClient,
  trafficReader: BurstTrafficReader = getSub2ApiBurstTraffic,
) {
  const configuration = await readAccountPoolPolicyConfiguration(db, connectionId);
  if (!configuration.configured) throw new Error("Account pool policy is not configured.");
  const policy = configuration.config;
  const runtimeRow = await db.setting.findUnique({ where: { key: runtimeKey(connectionId) } });
  const runtime = parseJson<PoolRuntimeState>(runtimeRow?.value, {});
  const hasManagedBurstBaseline = Object.values(runtime.burst?.managedConcurrencyBaselines ?? {})
    .some((baseline) => Number.isFinite(Number(baseline)) && Number(baseline) > 0);
  if ((!policy.smartExpansionEnabled || !policy.burstExpansionEnabled) && !hasManagedBurstBaseline) {
    const burst = planBurstConcurrency({
      accounts: [],
      eligibleAccountIds: runtime.burstEligibleAccountIds ?? [],
      traffic: {
        requestsPerMinute: 0,
        completedRequestsLastMinute: 0,
        accountRequestsPerMinute: {},
        degradedAccountIds: [],
      },
      policy,
      previous: runtime.burst,
      now,
    }).evaluation;
    return { ok: true, burst };
  }
  let client = clientOverride;
  if (!client) {
    const target = await resolveCanonicalTarget(db, connectionId);
    client = new Sub2ApiAdminClient(target.baseUrl, target.adminApiKey);
  }
  const eligibleAccountIds = runtime.burstEligibleAccountIds ?? [];
  const [accountPayload, traffic] = await Promise.all([
    client.listAccounts(),
    trafficReader(eligibleAccountIds),
  ]);
  const accounts = (Array.isArray(accountPayload) ? accountPayload : [])
    .map(normalizeRemoteAccount)
    .filter((account): account is RemotePoolAccount => account !== null)
    .filter((account) => !policy.excludedAccountIds.includes(account.id));
  const plan = planBurstConcurrencyByPlatform({
    accounts,
    eligibleAccountIds,
    traffic,
    policy,
    previous: runtime.burst,
    now,
  });
  const accountById = new Map(accounts.map((account) => [account.id, account]));
  const initialManagedConcurrencyBaselines: Record<string, number> = {};
  const initialManagedConcurrencyTokens = { ...(runtime.burst?.managedConcurrencyTokens ?? {}) };
  const recoveredCommittedOwnerships = new Set<string>();
  for (const [rawId, rawBaseline] of Object.entries(runtime.burst?.managedConcurrencyBaselines ?? {})) {
    const id = Number(rawId);
    const baseline = Number(rawBaseline);
    if (Number.isInteger(id) && id > 0 && Number.isFinite(baseline) && baseline > 0) {
      initialManagedConcurrencyBaselines[String(id)] = baseline;
    }
  }
  const managedConcurrencyBaselines: Record<string, number> = {};
  for (const [rawId, rawBaseline] of Object.entries(initialManagedConcurrencyBaselines)) {
    const id = Number(rawId);
    const baseline = Number(rawBaseline);
    const account = accountById.get(id);
    if (account && Number.isFinite(baseline) && baseline > 0 && account.concurrency > baseline) {
      managedConcurrencyBaselines[String(id)] = baseline;
      if (initialManagedConcurrencyTokens[String(id)] !== undefined) {
        recoveredCommittedOwnerships.add(String(id));
      }
    }
  }
  const burstRunToken = `${now.getTime()}:${randomUUID()}`;
  const successfulOwnershipAdditions: Record<string, number> = {};
  const successfulOwnershipRemovals: Record<string, number> = {};
  const failedOwnershipAdditions = new Set<string>();
  let updatedAccounts = 0;
  let failedWrites = 0;
  for (const update of plan.updates) {
    const account = accountById.get(update.accountId);
    if (!account) continue;
    const previousConcurrency = account.concurrency;
    const addsOwnership = update.direction === "expand"
      && update.concurrency > policy.maxAccountConcurrency
      && managedConcurrencyBaselines[String(update.accountId)] === undefined;
    let ownershipAcquired = !addsOwnership;
    try {
      if (addsOwnership) {
        await mutateAccountPoolRuntime(db, connectionId, (latest) => ({
          ...latest,
          burst: (() => {
            const baselines = { ...(latest.burst?.managedConcurrencyBaselines ?? {}) };
            const tokens = { ...(latest.burst?.managedConcurrencyTokens ?? {}) };
            const key = String(update.accountId);
            const existingBaseline = Number(baselines[key]);
            const existingToken = tokens[key];
            if (Number.isFinite(existingBaseline)
              && previousConcurrency <= existingBaseline
              && (existingToken === undefined || burstOwnershipTokenIsStale(existingToken, now))) {
              delete baselines[key];
              delete tokens[key];
            }
            if (baselines[key] === undefined) {
              baselines[key] = previousConcurrency;
              tokens[key] = burstRunToken;
              ownershipAcquired = true;
            }
            return {
              ...plan.evaluation,
              active: Object.keys(baselines).length > 0,
              managedConcurrencyBaselines: baselines,
              managedConcurrencyTokens: tokens,
            };
          })(),
        }));
        if (!ownershipAcquired) continue;
        managedConcurrencyBaselines[String(update.accountId)] = previousConcurrency;
      }
      await client.updateAccount(update.accountId, { concurrency: update.concurrency });
      account.concurrency = update.concurrency;
      if (addsOwnership) {
        successfulOwnershipAdditions[String(update.accountId)] = previousConcurrency;
      }
      const baseline = managedConcurrencyBaselines[String(update.accountId)];
      if (update.direction === "shrink" && baseline !== undefined && update.concurrency <= baseline) {
        delete managedConcurrencyBaselines[String(update.accountId)];
        successfulOwnershipRemovals[String(update.accountId)] = baseline;
      }
      updatedAccounts += 1;
    } catch {
      if (addsOwnership && ownershipAcquired) {
        const key = String(update.accountId);
        delete managedConcurrencyBaselines[key];
        failedOwnershipAdditions.add(key);
        try {
          await mutateAccountPoolRuntime(db, connectionId, (latest) => {
            const baselines = { ...(latest.burst?.managedConcurrencyBaselines ?? {}) };
            const tokens = { ...(latest.burst?.managedConcurrencyTokens ?? {}) };
            if (tokens[key] === burstRunToken) {
              delete baselines[key];
              delete tokens[key];
            }
            return latest.burst ? {
              ...latest,
              burst: {
                ...latest.burst,
                active: Object.keys(baselines).length > 0,
                managedConcurrencyBaselines: baselines,
                managedConcurrencyTokens: tokens,
              },
            } : latest;
          });
        } catch {
          // The final CAS below retries cleanup while preserving other runs.
        }
      }
      failedWrites += 1;
    }
  }
  const controlledIds = new Set([
    ...eligibleAccountIds,
    ...Object.keys(managedConcurrencyBaselines).map(Number),
  ]);
  const controlledAccounts = accounts.filter((account) => (
    controlledIds.has(account.id)
  ));
  const burst: AccountPoolBurstEvaluation = {
    ...plan.evaluation,
    state: failedWrites > 0 ? "failed" : plan.evaluation.state,
    active: controlledAccounts.some((account) => {
      const baseline = managedConcurrencyBaselines[String(account.id)];
      return baseline !== undefined && account.concurrency > baseline;
    }),
    currentCapacity: controlledAccounts.reduce((sum, account) => sum + account.concurrency, 0),
    updatedAccounts,
    failedWrites,
    managedConcurrencyBaselines,
  };
  const persistedRuntime = await mutateAccountPoolRuntime(db, connectionId, (latest) => {
    const baselines = { ...(latest.burst?.managedConcurrencyBaselines ?? {}) };
    const tokens = { ...(latest.burst?.managedConcurrencyTokens ?? {}) };
    for (const key of recoveredCommittedOwnerships) {
      if (tokens[key] === initialManagedConcurrencyTokens[key]) delete tokens[key];
    }
    for (const [key, baseline] of Object.entries(successfulOwnershipAdditions)) {
      if (baselines[key] === undefined) baselines[key] = baseline;
      if (tokens[key] === burstRunToken) delete tokens[key];
    }
    for (const [key, baseline] of Object.entries(successfulOwnershipRemovals)) {
      if (baselines[key] === baseline && tokens[key] === undefined) delete baselines[key];
    }
    for (const key of failedOwnershipAdditions) {
      if (tokens[key] === burstRunToken) {
        delete baselines[key];
        delete tokens[key];
      }
    }
    for (const [key, baseline] of Object.entries(initialManagedConcurrencyBaselines)) {
      if (managedConcurrencyBaselines[key] === undefined
        && successfulOwnershipRemovals[key] === undefined
        && baselines[key] === baseline
        && (initialManagedConcurrencyTokens[key] === undefined
          ? tokens[key] === undefined
          : tokens[key] === initialManagedConcurrencyTokens[key]
            && burstOwnershipTokenIsStale(tokens[key], now))) {
        delete baselines[key];
        delete tokens[key];
      }
    }
    return {
      ...latest,
      burst: {
        ...burst,
        active: Object.keys(baselines).length > 0,
        managedConcurrencyBaselines: baselines,
        managedConcurrencyTokens: tokens,
      },
    };
  });
  if (updatedAccounts > 0 || failedWrites > 0) {
    await safeLog(db, connectionId, { burst: persistedRuntime.burst ?? burst, updates: plan.updates }, failedWrites > 0 ? "failed" : "success", failedWrites > 0 ? "Burst concurrency update partially failed" : undefined);
  }
  return { ok: failedWrites === 0, burst: persistedRuntime.burst ?? burst };
}

export async function runRapidAccountPoolBursts(db: PrismaClient, now = new Date(), shouldStop?: () => boolean) {
  const configuredRows = await db.setting.findMany({
    where: { key: { startsWith: "account_pool_policy:" } },
    select: { key: true },
  });
  const connectionIds = configuredRows
    .map((row) => Number(row.key.slice("account_pool_policy:".length)))
    .filter((id) => Number.isInteger(id) && id > 0);
  if (connectionIds.length === 0) return [];
  const results: Array<{ connectionId: number; ok: boolean; state: string; updatedAccounts: number }> = [];
  for (const connectionId of connectionIds) {
    if (shouldStop?.()) break;
    try {
      const target = await resolveCanonicalTarget(db, connectionId);
      const client = new Sub2ApiAdminClient(target.baseUrl, target.adminApiKey);
      const result = await runRapidAccountPoolBurstForConnection(db, connectionId, now, client);
      results.push({
        connectionId,
        ok: result.ok,
        state: result.burst.state,
        updatedAccounts: result.burst.updatedAccounts,
      });
    } catch (error) {
      if (isCanonicalTargetDisabled(error)) continue;
      const message = error instanceof Error ? error.message : String(error);
      console.error(`[worker] Rapid account burst failed for connection ${connectionId}: ${message}`);
      results.push({ connectionId, ok: false, state: "failed", updatedAccounts: 0 });
    }
  }
  return results;
}
