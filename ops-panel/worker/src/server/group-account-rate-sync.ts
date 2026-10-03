import type { Prisma } from "@prisma/client";
import type { GroupRateRuleInput } from "@/server/rate-rule-evaluator";
import { getAccountGroupIds, getAccountId, getAccountName, getAccountRate } from "@/server/account-utils";
import { MANUAL_ZERO_RATE_REASON } from "@/server/account-rate-writer";
import { normalizeRateMultiplier, ratesEqual } from "@/shared/rates";

export { MANUAL_ZERO_RATE_REASON } from "@/server/account-rate-writer";

type SourceRateLike = {
  currentRate?: number | null;
};

const MULTI_GROUP_RATE_CONFLICT_REASON =
  "account belongs to multiple enabled group rate rules; add a dedicated account source binding";
const UNBOUND_ACCOUNT_MANUAL_RATE_REASON =
  "account has no dedicated upstream source binding; preserving its manual rate";

export function isAccountEnabledForScheduling(account: unknown) {
  if (!account || typeof account !== "object") return false;
  const row = account as { status?: string | null; schedulable?: boolean | null };
  return row.status?.toLowerCase() === "active" && row.schedulable !== false;
}

export type GroupAccountRateSyncResult = {
  rateMultiplier: number;
  total: number;
  updated: number;
  skipped: number;
  failed: number;
  accounts: Array<{
    accountId: number | null;
    accountName: string;
    oldRate: number | null;
    newRate: number;
    status: "updated" | "skipped" | "failed";
    error?: string;
    reason?: string;
  }>;
};

export async function accountIdsWithDedicatedSourceBindings(input: {
  db: Pick<Prisma.TransactionClient, "blSourceBinding">;
  connectionId: number;
  groupId: number;
  accounts: unknown[];
}) {
  const accountIds = input.accounts.flatMap((account) => {
    const accountId = getAccountId(account);
    return accountId && getAccountGroupIds(account).includes(input.groupId) ? [accountId] : [];
  });
  if (accountIds.length === 0) return new Set<number>();

  const bindings = await input.db.blSourceBinding.findMany({
    where: {
      connectionId: input.connectionId,
      targetType: "account",
      targetId: { in: accountIds },
    },
    select: { targetId: true },
  });
  return new Set(bindings.map((binding) => binding.targetId));
}

export async function groupAccountRateSkipPolicy(input: {
  db: Pick<Prisma.TransactionClient, "blSourceBinding" | "blGroupRateRule">;
  connectionId: number;
  groupId: number;
  accounts: unknown[];
}) {
  const [skipAccountIds, enabledRules] = await Promise.all([
    accountIdsWithDedicatedSourceBindings(input),
    input.db.blGroupRateRule.findMany({
      where: {
        connectionId: input.connectionId,
        enabled: true,
        mode: "manual_source",
      },
      select: { groupId: true },
    }),
  ]);
  const skipAccountReasons = groupAccountRateConflictReasons({
    accounts: input.accounts,
    enabledGroupIds: new Set(enabledRules.map((rule) => rule.groupId)),
  });
  for (const account of input.accounts) {
    const accountId = getAccountId(account);
    if (!accountId || !getAccountGroupIds(account).includes(input.groupId) || skipAccountIds.has(accountId)) continue;
    if (!skipAccountReasons.has(accountId)) {
      skipAccountReasons.set(accountId, UNBOUND_ACCOUNT_MANUAL_RATE_REASON);
    }
  }

  return {
    skipAccountIds,
    skipAccountReasons,
  };
}

function finitePositiveNumber(value: unknown) {
  if (value === null || value === undefined || value === "") return null;
  const numeric = Number(value);
  return Number.isFinite(numeric) && numeric > 0 ? numeric : null;
}

export function resolveGroupAccountRateMultiplier(input: {
  rule: Pick<GroupRateRuleInput, "mode" | "offset">;
  sources: SourceRateLike[];
}) {
  if (input.rule.mode === "manual_source") {
    return finitePositiveNumber(input.rule.offset);
  }

  return null;
}

export function groupAccountRateConflictReasons(input: {
  accounts: unknown[];
  enabledGroupIds: ReadonlySet<number>;
}) {
  const reasons = new Map<number, string>();
  for (const account of input.accounts) {
    const accountId = getAccountId(account);
    if (!accountId) continue;

    const matchingGroupIds = Array.from(
      new Set(getAccountGroupIds(account).filter((groupId) => input.enabledGroupIds.has(groupId))),
    );
    if (matchingGroupIds.length <= 1) continue;

    reasons.set(
      accountId,
      `${MULTI_GROUP_RATE_CONFLICT_REASON}: ${matchingGroupIds.sort((a, b) => a - b).join(",")}`,
    );
  }
  return reasons;
}

export async function syncGroupAccountRateMultipliers(input: {
  client: {
    getAccount(accountId: number): Promise<unknown>;
    updateAccountRateMultiplier(accountId: number, rateMultiplier: number): Promise<unknown>;
  };
  groupId: number;
  accounts: unknown[];
  rateMultiplier: number;
  skipAccountIds?: ReadonlySet<number>;
  skipAccountReasons?: ReadonlyMap<number, string>;
}): Promise<GroupAccountRateSyncResult> {
  const nextRate = normalizeRateMultiplier(input.rateMultiplier);
  const result: GroupAccountRateSyncResult = {
    rateMultiplier: nextRate,
    total: 0,
    updated: 0,
    skipped: 0,
    failed: 0,
    accounts: [],
  };

  for (const account of input.accounts) {
    if (!getAccountGroupIds(account).includes(input.groupId) || !isAccountEnabledForScheduling(account)) continue;

    const accountId = getAccountId(account);
    const accountName = accountId ? getAccountName(account, accountId) : "#unknown";
    const oldRate = getAccountRate(account);
    result.total += 1;

    if (!accountId) {
      result.failed += 1;
      result.accounts.push({
        accountId: null,
        accountName,
        oldRate,
        newRate: nextRate,
        status: "failed",
        error: "missing account id",
      });
      continue;
    }

    const hasDedicatedBinding = input.skipAccountIds?.has(accountId) ?? false;
    const skipReason = hasDedicatedBinding ? undefined : input.skipAccountReasons?.get(accountId);
    if (hasDedicatedBinding || skipReason) {
      result.skipped += 1;
      result.accounts.push({
        accountId,
        accountName,
        oldRate,
        newRate: nextRate,
        status: "skipped",
        reason: skipReason ?? "account has a dedicated upstream source binding",
      });
      continue;
    }

    if (oldRate !== null && ratesEqual(oldRate, 0)) {
      result.skipped += 1;
      result.accounts.push({
        accountId,
        accountName,
        oldRate,
        newRate: nextRate,
        status: "skipped",
        reason: MANUAL_ZERO_RATE_REASON,
      });
      continue;
    }

    if (oldRate !== null && ratesEqual(oldRate, nextRate)) {
      result.skipped += 1;
      result.accounts.push({
        accountId,
        accountName,
        oldRate,
        newRate: nextRate,
        status: "skipped",
      });
      continue;
    }

    try {
      const latestAccount = await input.client.getAccount(accountId);
      const latestRate = getAccountRate(latestAccount);
      if (latestRate === null) {
        throw new Error(`account ${accountId} detail response is missing rate_multiplier or contains an invalid value`);
      }
      if (ratesEqual(latestRate, 0)) {
        result.skipped += 1;
        result.accounts.push({
          accountId,
          accountName,
          oldRate,
          newRate: nextRate,
          status: "skipped",
          reason: MANUAL_ZERO_RATE_REASON,
        });
        continue;
      }
      if (ratesEqual(latestRate, nextRate)) {
        result.skipped += 1;
        result.accounts.push({
          accountId,
          accountName,
          oldRate,
          newRate: nextRate,
          status: "skipped",
        });
        continue;
      }
      await input.client.updateAccountRateMultiplier(accountId, nextRate);
      result.updated += 1;
      result.accounts.push({
        accountId,
        accountName,
        oldRate,
        newRate: nextRate,
        status: "updated",
      });
    } catch (error) {
      result.failed += 1;
      result.accounts.push({
        accountId,
        accountName,
        oldRate,
        newRate: nextRate,
        status: "failed",
        error: error instanceof Error ? error.message : String(error),
      });
    }
  }

  return result;
}
