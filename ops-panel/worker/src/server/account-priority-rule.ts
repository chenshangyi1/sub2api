import type { Sub2ApiGroup } from "@/server/clients/sub2api-admin";
import { Sub2ApiAdminClient } from "@/server/clients/sub2api-admin";
import { getAccountGroupIds, getAccountId, getAccountName, getAccountPriority, getAccountRate } from "@/server/account-utils";
import { getSetting, setSetting } from "@/server/settings";
import { ratesEqual } from "@/shared/rates";
import { writeSyncLog } from "@/server/sync-logs";

export const minAccountPriority = 1;
export const maxAccountPriority = 30;

export type AccountPriorityStrategy = "rate" | "latency_rate";

export type AccountPriorityRuleConfig = {
  enabled: boolean;
  targetGroupIds: number[];
  strategy: AccountPriorityStrategy;
  sampleSize: number;
  lookbackMinutes: number;
  firstTokenCoefficient: number;
  rateCoefficient: number;
  missingSamplePenaltyMs: number;
  updatedAt?: string | null;
};

export type AccountPriorityApplyResult = {
  ok: boolean;
  skipped: boolean;
  enabled: boolean;
  targetGroupIds: number[];
  matchedAccounts: number;
  updated: number;
  unchanged: number;
  skippedAccounts: number;
  failed: number;
  groups: Array<{ groupId: number; groupName: string; accountCount: number }>;
  updates: AccountPriorityUpdatePlan[];
  failures: Array<AccountPriorityUpdatePlan & { error: string }>;
  sampleFailures: AccountPrioritySampleFailure[];
  message: string;
};

type AccountPriorityUpdatePlan = {
  accountId: number;
  accountName: string;
  rateMultiplier: number;
  oldPriority: number | null;
  newPriority: number;
  matchedGroupIds: number[];
  priorityScore?: number;
  averageFirstTokenMs?: number;
  validSampleCount?: number;
  missingSampleCount?: number;
  missingFillMs?: number;
  sampleSize?: number;
  lookbackMinutes?: number;
};

type AccountPrioritySampleFailure = {
  accountId: number;
  accountName: string;
  matchedGroupIds: number[];
  error: string;
};

type CandidateAccount = {
  account: unknown;
  accountId: number;
  accountName: string;
  rateMultiplier: number | null;
  oldPriority: number | null;
  matchedGroupIds: number[];
};

type LatencyPriorityPlanRow = {
  plan: AccountPriorityUpdatePlan | null;
  failure: AccountPrioritySampleFailure | null;
};

const defaultSampleSize = 10;
const defaultLookbackMinutes = 60;
const maxLookbackMinutes = 720 * 60;
const defaultFirstTokenCoefficient = 1;
const defaultRateCoefficient = 10_000;
const defaultMissingSamplePenaltyMs = 5_000;
const usageFetchConcurrency = 4;
const usageFetchTimezone = "Asia/Shanghai";

const defaultRule: AccountPriorityRuleConfig = {
  enabled: false,
  targetGroupIds: [],
  strategy: "rate",
  sampleSize: defaultSampleSize,
  lookbackMinutes: defaultLookbackMinutes,
  firstTokenCoefficient: defaultFirstTokenCoefficient,
  rateCoefficient: defaultRateCoefficient,
  missingSamplePenaltyMs: defaultMissingSamplePenaltyMs,
  updatedAt: null,
};

function settingKey(connectionId: number) {
  return `account_priority_rule:${connectionId}`;
}

function uniquePositiveIds(values: unknown[]) {
  return Array.from(new Set(values
    .map((value) => Number(value))
    .filter((value) => Number.isInteger(value) && value > 0)))
    .sort((left, right) => left - right);
}

function finiteNumber(value: unknown) {
  const numeric = Number(value);
  return Number.isFinite(numeric) ? numeric : null;
}

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}

function normalizeInteger(value: unknown, fallback: number, min: number, max: number) {
  const numeric = finiteNumber(value);
  if (numeric === null) return fallback;
  return clamp(Math.round(numeric), min, max);
}

function normalizeNonNegativeNumber(value: unknown, fallback: number, max = Number.MAX_SAFE_INTEGER) {
  const numeric = finiteNumber(value);
  if (numeric === null) return fallback;
  return clamp(numeric, 0, max);
}

function normalizeStrategy(value: unknown): AccountPriorityStrategy {
  return value === "latency_rate" ? "latency_rate" : "rate";
}

function normalizeLookbackMinutes(value: unknown, legacyHours: unknown) {
  if (value !== undefined) return normalizeInteger(value, defaultLookbackMinutes, 1, maxLookbackMinutes);
  if (legacyHours !== undefined) {
    return clamp(normalizeInteger(legacyHours, Math.ceil(defaultLookbackMinutes / 60), 1, 720) * 60, 1, maxLookbackMinutes);
  }
  return defaultLookbackMinutes;
}

function normalizeRule(value: unknown): AccountPriorityRuleConfig {
  if (!value || typeof value !== "object" || Array.isArray(value)) return defaultRule;
  const raw = value as Partial<AccountPriorityRuleConfig> & { lookbackHours?: unknown };
  const updatedAt = typeof raw.updatedAt === "string" ? raw.updatedAt : null;
  return {
    enabled: raw.enabled === true,
    targetGroupIds: uniquePositiveIds(Array.isArray(raw.targetGroupIds) ? raw.targetGroupIds : []),
    strategy: normalizeStrategy(raw.strategy),
    sampleSize: normalizeInteger(raw.sampleSize, defaultSampleSize, 1, 200),
    lookbackMinutes: normalizeLookbackMinutes(raw.lookbackMinutes, raw.lookbackHours),
    firstTokenCoefficient: normalizeNonNegativeNumber(raw.firstTokenCoefficient, defaultFirstTokenCoefficient),
    rateCoefficient: normalizeNonNegativeNumber(raw.rateCoefficient, defaultRateCoefficient),
    missingSamplePenaltyMs: normalizeInteger(raw.missingSamplePenaltyMs, defaultMissingSamplePenaltyMs, 0, 3_600_000),
    updatedAt,
  };
}

export async function readAccountPriorityRule(connectionId: number) {
  const raw = await getSetting(settingKey(connectionId), "");
  if (!raw.trim()) return defaultRule;
  try {
    return normalizeRule(JSON.parse(raw) as unknown);
  } catch {
    return defaultRule;
  }
}

export async function saveAccountPriorityRule(
  connectionId: number,
  rule: Pick<AccountPriorityRuleConfig, "enabled" | "targetGroupIds"> & Partial<Omit<AccountPriorityRuleConfig, "enabled" | "targetGroupIds" | "updatedAt">> & { lookbackHours?: number },
) {
  const current = await readAccountPriorityRule(connectionId);
  const lookbackInput = rule.lookbackMinutes !== undefined
    ? { lookbackMinutes: rule.lookbackMinutes }
    : rule.lookbackHours !== undefined
      ? { lookbackMinutes: undefined, lookbackHours: rule.lookbackHours }
      : {};
  const normalized = normalizeRule({
    ...current,
    enabled: rule.enabled,
    targetGroupIds: rule.targetGroupIds,
    ...(rule.strategy !== undefined ? { strategy: rule.strategy } : {}),
    ...(rule.sampleSize !== undefined ? { sampleSize: rule.sampleSize } : {}),
    ...lookbackInput,
    ...(rule.firstTokenCoefficient !== undefined ? { firstTokenCoefficient: rule.firstTokenCoefficient } : {}),
    ...(rule.rateCoefficient !== undefined ? { rateCoefficient: rule.rateCoefficient } : {}),
    ...(rule.missingSamplePenaltyMs !== undefined ? { missingSamplePenaltyMs: rule.missingSamplePenaltyMs } : {}),
    updatedAt: new Date().toISOString(),
  });
  await setSetting(settingKey(connectionId), JSON.stringify(normalized));
  return normalized;
}

function groupLabel(group: Sub2ApiGroup | undefined, groupId: number) {
  return group?.name?.trim() || `#${groupId}`;
}

function buildCandidates(input: {
  accounts: unknown[];
  groups: Sub2ApiGroup[];
  targetGroupIds: number[];
}) {
  const targetGroupIdSet = new Set(input.targetGroupIds);
  const candidateById = new Map<number, CandidateAccount>();
  const skipped: CandidateAccount[] = [];

  for (const account of input.accounts) {
    const accountId = getAccountId(account);
    if (!accountId) continue;

    const matchedGroupIds = getAccountGroupIds(account).filter((groupId) => targetGroupIdSet.has(groupId));
    if (matchedGroupIds.length === 0) continue;

    const accountName = getAccountName(account, accountId);
    const candidate: CandidateAccount = {
      account,
      accountId,
      accountName,
      rateMultiplier: getAccountRate(account),
      oldPriority: getAccountPriority(account),
      matchedGroupIds,
    };

    if (candidate.rateMultiplier === null) {
      skipped.push(candidate);
      continue;
    }

    candidateById.set(accountId, candidate);
  }

  const groupCounts = new Map<number, number>();
  for (const candidate of candidateById.values()) {
    for (const groupId of candidate.matchedGroupIds) {
      groupCounts.set(groupId, (groupCounts.get(groupId) ?? 0) + 1);
    }
  }

  const groupsById = new Map(input.groups.map((group) => [group.id, group]));
  const groups = input.targetGroupIds.map((groupId) => ({
    groupId,
    groupName: groupLabel(groupsById.get(groupId), groupId),
    accountCount: groupCounts.get(groupId) ?? 0,
  }));

  return { candidates: Array.from(candidateById.values()), groups, skipped };
}

function priorityForRank(rank: number, rankCount: number) {
  if (rankCount <= 1) return minAccountPriority;

  const availablePriorities = maxAccountPriority - minAccountPriority + 1;
  if (rankCount <= availablePriorities) return minAccountPriority + rank;

  return minAccountPriority + Math.floor(rank * (maxAccountPriority - minAccountPriority) / (rankCount - 1));
}

function applyPriorityBands(
  groups: AccountPriorityUpdatePlan[][],
) {
  return groups.flatMap((group, rank) => group.map((plan) => ({
    ...plan,
    newPriority: priorityForRank(rank, groups.length),
  })));
}

function planRatePriorityUpdates(candidates: CandidateAccount[]) {
  const sorted = [...candidates].sort((left, right) => {
    const leftRate = left.rateMultiplier ?? 0;
    const rightRate = right.rateMultiplier ?? 0;
    if (!ratesEqual(leftRate, rightRate)) return leftRate - rightRate;
    const nameDiff = left.accountName.localeCompare(right.accountName, "zh-CN");
    return nameDiff === 0 ? left.accountId - right.accountId : nameDiff;
  });

  const groups: AccountPriorityUpdatePlan[][] = [];
  let lastRate: number | null = null;

  for (const candidate of sorted) {
    const rateMultiplier = candidate.rateMultiplier;
    if (rateMultiplier === null) continue;
    if (lastRate === null || !ratesEqual(rateMultiplier, lastRate)) {
      lastRate = rateMultiplier;
      groups.push([]);
    }
    groups[groups.length - 1]?.push({
      accountId: candidate.accountId,
      accountName: candidate.accountName,
      rateMultiplier,
      oldPriority: candidate.oldPriority,
      newPriority: minAccountPriority,
      matchedGroupIds: candidate.matchedGroupIds,
    });
  }

  return applyPriorityBands(groups);
}

function priorityScoreFromValue(value: number) {
  if (!Number.isFinite(value)) return Number.MAX_SAFE_INTEGER;
  return clamp(Math.ceil(value), 0, Number.MAX_SAFE_INTEGER);
}

function parseCreatedAt(value: string | null) {
  if (!value) return null;
  const date = new Date(value);
  return Number.isFinite(date.getTime()) ? date : null;
}

function datePartInTimezone(date: Date, timeZone: string) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(date);
  const year = parts.find((part) => part.type === "year")?.value;
  const month = parts.find((part) => part.type === "month")?.value;
  const day = parts.find((part) => part.type === "day")?.value;
  return year && month && day ? `${year}-${month}-${day}` : date.toISOString().slice(0, 10);
}

async function mapWithConcurrency<T, R>(items: T[], limit: number, task: (item: T) => Promise<R>) {
  const results = new Array<R>(items.length);
  let nextIndex = 0;
  const workers = Array.from({ length: Math.min(limit, items.length) }, async () => {
    while (nextIndex < items.length) {
      const index = nextIndex;
      nextIndex += 1;
      results[index] = await task(items[index]);
    }
  });
  await Promise.all(workers);
  return results;
}

async function planLatencyPriorityUpdates(input: {
  candidates: CandidateAccount[];
  s2Client: Sub2ApiAdminClient;
  rule: AccountPriorityRuleConfig;
  now: Date;
}) {
  const cutoff = new Date(input.now.getTime() - input.rule.lookbackMinutes * 60 * 1000);
  const startDate = datePartInTimezone(cutoff, usageFetchTimezone);
  const endDate = datePartInTimezone(input.now, usageFetchTimezone);

  const rows = await mapWithConcurrency<CandidateAccount, LatencyPriorityPlanRow>(input.candidates, usageFetchConcurrency, async (candidate) => {
    const rateMultiplier = candidate.rateMultiplier;
    if (rateMultiplier === null) return { plan: null, failure: null };

    try {
      const samples: Array<{ firstTokenMs: number; createdAt: Date }> = [];
      const pageSize = 200;
      for (let page = 1; samples.length < input.rule.sampleSize; page += 1) {
        const usageLogs = await input.s2Client.listUsageLogs({
          accountId: candidate.accountId,
          stream: true,
          startDate,
          endDate,
          timezone: usageFetchTimezone,
          page,
          pageSize,
          sortBy: "created_at",
          sortOrder: "desc",
          exactTotal: false,
        });
        if (usageLogs.length === 0) break;

        let oldestInPage: Date | null = null;
        for (const log of usageLogs) {
          const createdAt = parseCreatedAt(log.created_at);
          if (createdAt && (oldestInPage === null || createdAt.getTime() < oldestInPage.getTime())) {
            oldestInPage = createdAt;
          }
          if (log.stream !== true || log.account_id !== candidate.accountId) continue;
          if (!createdAt || createdAt.getTime() < cutoff.getTime()) continue;
          if (log.first_token_ms === null || !Number.isInteger(log.first_token_ms) || log.first_token_ms < 0) continue;
          samples.push({ firstTokenMs: log.first_token_ms, createdAt });
          if (samples.length >= input.rule.sampleSize) break;
        }

        if (usageLogs.length < pageSize) break;
        if (!oldestInPage || oldestInPage.getTime() < cutoff.getTime()) break;
      }

      const validSampleCount = samples.length;
      const missingSampleCount = input.rule.sampleSize - validSampleCount;
      const sampleTotal = samples.reduce((total, sample) => total + sample.firstTokenMs, 0);
      const missingFillMs = validSampleCount > 0 ? input.rule.missingSamplePenaltyMs : 0;
      const averageFirstTokenMs = (sampleTotal + missingSampleCount * missingFillMs) / input.rule.sampleSize;
      const priorityScore = priorityScoreFromValue(
        averageFirstTokenMs * input.rule.firstTokenCoefficient
        + rateMultiplier * input.rule.rateCoefficient,
      );

      return {
        plan: {
          accountId: candidate.accountId,
          accountName: candidate.accountName,
          rateMultiplier,
          oldPriority: candidate.oldPriority,
          newPriority: minAccountPriority,
          matchedGroupIds: candidate.matchedGroupIds,
          priorityScore,
          averageFirstTokenMs,
          validSampleCount,
          missingSampleCount,
          missingFillMs,
          sampleSize: input.rule.sampleSize,
          lookbackMinutes: input.rule.lookbackMinutes,
        } satisfies AccountPriorityUpdatePlan,
        failure: null,
      };
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return {
        plan: null,
        failure: {
          accountId: candidate.accountId,
          accountName: candidate.accountName,
          matchedGroupIds: candidate.matchedGroupIds,
          error: message,
        } satisfies AccountPrioritySampleFailure,
      };
    }
  });

  const sampleFailures = rows
    .map((row) => row.failure)
    .filter((failure): failure is AccountPrioritySampleFailure => Boolean(failure));
  const sortedPlans = rows
    .map((row) => row.plan)
    .filter((plan): plan is AccountPriorityUpdatePlan => Boolean(plan))
    .sort((left, right) => {
      const leftScore = left.priorityScore ?? Number.MAX_SAFE_INTEGER;
      const rightScore = right.priorityScore ?? Number.MAX_SAFE_INTEGER;
      if (leftScore !== rightScore) return leftScore - rightScore;
      if (!ratesEqual(left.rateMultiplier, right.rateMultiplier)) return left.rateMultiplier - right.rateMultiplier;
      const leftLatency = left.averageFirstTokenMs ?? Number.MAX_SAFE_INTEGER;
      const rightLatency = right.averageFirstTokenMs ?? Number.MAX_SAFE_INTEGER;
      if (leftLatency !== rightLatency) return leftLatency - rightLatency;
      const nameDiff = left.accountName.localeCompare(right.accountName, "zh-CN");
      return nameDiff === 0 ? left.accountId - right.accountId : nameDiff;
    });
  const groups: AccountPriorityUpdatePlan[][] = [];
  for (const plan of sortedPlans) {
    const lastGroup = groups[groups.length - 1];
    const lastPlan = lastGroup?.[lastGroup.length - 1];
    if (!lastPlan || lastPlan.priorityScore !== plan.priorityScore) {
      groups.push([plan]);
      continue;
    }
    lastGroup.push(plan);
  }

  return { plans: applyPriorityBands(groups), sampleFailures };
}

async function logPriorityResult(input: {
  db: unknown;
  connectionId: number;
  action: string;
  rule: AccountPriorityRuleConfig;
  result: AccountPriorityApplyResult;
}) {
  if (input.result.skipped) return;
  try {
    await writeSyncLog(input.db, {
      connectionId: input.connectionId,
      action: input.action,
      target: `connection:${input.connectionId}`,
      detail: {
        strategy: input.rule.strategy,
        sampleSize: input.rule.sampleSize,
        lookbackMinutes: input.rule.lookbackMinutes,
        firstTokenCoefficient: input.rule.firstTokenCoefficient,
        rateCoefficient: input.rule.rateCoefficient,
        missingSamplePenaltyMs: input.rule.missingSamplePenaltyMs,
        targetGroupIds: input.result.targetGroupIds,
        matchedAccounts: input.result.matchedAccounts,
        updated: input.result.updated,
        unchanged: input.result.unchanged,
        skippedAccounts: input.result.skippedAccounts,
        failed: input.result.failed,
        groups: input.result.groups,
        updates: input.result.updates.slice(0, 120),
        failures: input.result.failures.slice(0, 40),
        sampleFailures: input.result.sampleFailures.slice(0, 40),
      },
      status: input.result.ok ? "success" : "failed",
      error: input.result.ok ? undefined : `${input.result.failed} 个账号优先级处理失败`,
    });
  } catch {
    // Logging must not hide the remote operation result.
  }
}

export async function applyAccountPriorityRule(input: {
  db: unknown;
  connectionId: number;
  s2Client: Sub2ApiAdminClient;
  rule?: AccountPriorityRuleConfig;
  accounts?: unknown[];
  groups?: Sub2ApiGroup[];
  action?: string;
}): Promise<AccountPriorityApplyResult> {
  const rule = input.rule ?? await readAccountPriorityRule(input.connectionId);
  const targetGroupIds = uniquePositiveIds(rule.targetGroupIds);
  const action = input.action ?? "apply_account_priority_rule";

  if (!rule.enabled) {
    return {
      ok: true,
      skipped: true,
      enabled: false,
      targetGroupIds,
      matchedAccounts: 0,
      updated: 0,
      unchanged: 0,
      skippedAccounts: 0,
      failed: 0,
      groups: [],
      updates: [],
      failures: [],
      sampleFailures: [],
      message: "priority rule disabled",
    };
  }

  const [accounts, groups] = await Promise.all([
    input.accounts ? Promise.resolve(input.accounts) : input.s2Client.listAccounts(),
    input.groups ? Promise.resolve(input.groups) : input.s2Client.listGroups().catch(() => []),
  ]);
  const effectiveTargetGroupIds = targetGroupIds.length > 0
    ? targetGroupIds
    : uniquePositiveIds([
        ...groups.map((group) => group.id),
        ...accounts.flatMap((account) => getAccountGroupIds(account)),
      ]);
  const { candidates, groups: groupSummaries, skipped } = buildCandidates({
    accounts,
    groups,
    targetGroupIds: effectiveTargetGroupIds,
  });
  const { plans, sampleFailures } = rule.strategy === "latency_rate"
    ? await planLatencyPriorityUpdates({ candidates, s2Client: input.s2Client, rule, now: new Date() })
    : { plans: planRatePriorityUpdates(candidates), sampleFailures: [] };
  const updates = plans.filter((plan) => plan.oldPriority !== plan.newPriority);
  const unchanged = plans.length - updates.length;
  const applied: AccountPriorityUpdatePlan[] = [];
  const failures: Array<AccountPriorityUpdatePlan & { error: string }> = [];

  for (const update of updates) {
    try {
      await input.s2Client.updateAccount(update.accountId, { priority: update.newPriority });
      applied.push(update);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      failures.push({ ...update, error: message });
    }
  }

  const result: AccountPriorityApplyResult = {
    ok: failures.length === 0 && sampleFailures.length === 0,
    skipped: false,
    enabled: true,
    targetGroupIds,
    matchedAccounts: candidates.length,
    updated: applied.length,
    unchanged,
    skippedAccounts: skipped.length,
    failed: failures.length + sampleFailures.length,
    groups: groupSummaries,
    updates: applied,
    failures,
    sampleFailures,
    message: failures.length === 0 && sampleFailures.length === 0 ? "priority rule applied" : "priority rule partially failed",
  };

  await logPriorityResult({
    db: input.db,
    connectionId: input.connectionId,
    action,
    rule,
    result,
  });

  return result;
}
