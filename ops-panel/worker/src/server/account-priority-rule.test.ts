import assert from "node:assert/strict";
import test from "node:test";
import type { Sub2ApiAdminClient } from "./clients/sub2api-admin";
import {
  applyAccountPriorityRule,
  maxAccountPriority,
  minAccountPriority,
  type AccountPriorityRuleConfig,
} from "./account-priority-rule";

test("an empty priority group list applies to every account group", async () => {
  const updates: number[] = [];
  const client = {
    updateAccount: async (accountId: number) => {
      updates.push(accountId);
    },
  } as unknown as Sub2ApiAdminClient;
  const rule: AccountPriorityRuleConfig = {
    enabled: true,
    targetGroupIds: [],
    strategy: "rate",
    sampleSize: 10,
    lookbackMinutes: 60,
    firstTokenCoefficient: 1,
    rateCoefficient: 10_000,
    missingSamplePenaltyMs: 5_000,
  };

  const result = await applyAccountPriorityRule({
    db: {},
    connectionId: 1,
    s2Client: client,
    rule,
    accounts: [
      { id: 11, name: "first", rate_multiplier: 1, priority: 0, group_ids: [101] },
      { id: 22, name: "second", rate_multiplier: 2, priority: 0, group_ids: [202] },
    ],
    groups: [
      { id: 101, name: "first group" },
      { id: 202, name: "second group" },
    ],
  });

  assert.equal(result.ok, true);
  assert.equal(result.matchedAccounts, 2);
  assert.deepEqual(result.targetGroupIds, []);
  assert.deepEqual(result.groups.map((group) => group.groupId), [101, 202]);
  assert.deepEqual(updates.sort((left, right) => left - right), [11, 22]);
});

function ratePriorityRule(): AccountPriorityRuleConfig {
  return {
    enabled: true,
    targetGroupIds: [1],
    strategy: "rate",
    sampleSize: 1,
    lookbackMinutes: 60,
    firstTokenCoefficient: 1,
    rateCoefficient: 0,
    missingSamplePenaltyMs: 0,
  };
}

function accountsWithDistinctRates(count: number) {
  return Array.from({ length: count }, (_, index) => ({
    id: index + 1,
    name: `account-${index + 1}`,
    rate_multiplier: index + 1,
    priority: 0,
    group_ids: [1],
  }));
}

function assertBoundedAndOrdered(result: Awaited<ReturnType<typeof applyAccountPriorityRule>>, count: number) {
  assert.equal(result.updated, count);
  assert.equal(Math.min(...result.updates.map((update) => update.newPriority)), minAccountPriority);
  assert.equal(Math.max(...result.updates.map((update) => update.newPriority)), maxAccountPriority);

  const priorityByAccount = new Map(result.updates.map((update) => [update.accountId, update.newPriority]));
  for (let accountId = 1; accountId <= count; accountId += 1) {
    const priority = priorityByAccount.get(accountId);
    assert.ok(priority !== undefined && priority >= minAccountPriority && priority <= maxAccountPriority);
    if (accountId > 1) {
      assert.ok((priorityByAccount.get(accountId - 1) ?? maxAccountPriority) <= priority);
    }
  }
}

test("rate priority maps more than thirty rate tiers into the 1..30 contract", async () => {
  const client = {
    updateAccount: async () => undefined,
  } as unknown as Sub2ApiAdminClient;

  const result = await applyAccountPriorityRule({
    db: {},
    connectionId: 1,
    s2Client: client,
    rule: ratePriorityRule(),
    accounts: accountsWithDistinctRates(31),
    groups: [{ id: 1, name: "pool" }],
  });

  assertBoundedAndOrdered(result, 31);
});

test("latency priority retains the raw score while mapping more than thirty scores into 1..30", async () => {
  const now = new Date().toISOString();
  const client = {
    listUsageLogs: async ({ accountId }: { accountId: number }) => [{
      account_id: accountId,
      stream: true,
      first_token_ms: accountId,
      created_at: now,
    }],
    updateAccount: async () => undefined,
  } as unknown as Sub2ApiAdminClient;
  const rule = { ...ratePriorityRule(), strategy: "latency_rate" as const };

  const result = await applyAccountPriorityRule({
    db: {},
    connectionId: 1,
    s2Client: client,
    rule,
    accounts: accountsWithDistinctRates(31).map((account) => ({ ...account, rate_multiplier: 0 })),
    groups: [{ id: 1, name: "pool" }],
  });

  assertBoundedAndOrdered(result, 31);
  assert.equal(result.updates.find((update) => update.accountId === 1)?.priorityScore, 1);
  assert.equal(result.updates.find((update) => update.accountId === 31)?.priorityScore, 31);
});
