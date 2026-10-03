import assert from "node:assert/strict";
import test from "node:test";
import {
  accountPriceProtectionFactor,
  allocateLoadFactors,
  calculateAccountHealth,
  calculatePassiveAccountHealth,
  calculateAccountPriority,
  meetsHealthyPoolTarget,
  meetsMinimumHealthyPool,
  mergeAccountHealth,
  mutateAccountPoolRuntime,
  normalizeAccountPoolPolicy,
  runRapidAccountPoolBurstForConnection,
  runAccountPoolPolicies,
  runAccountPoolPolicyForConnection,
  selectAccountsToDisable,
  shouldDisableAccount,
} from "./account-pool-policy";
import { planBurstConcurrency, summarizeBurstTrafficRows, type BurstTrafficSample } from "./account-pool-burst";

const burstTraffic = (requestsPerMinute: number, degradedAccountIds: number[] = []): BurstTrafficSample => ({
  requestsPerMinute,
  completedRequestsLastMinute: requestsPerMinute,
  accountRequestsPerMinute: {},
  degradedAccountIds,
});

test("manual account pool runs are restricted to the requested connection", async () => {
  let mappedLegacyId: string | undefined;
    let canonicalTargetId: bigint | undefined;
  const db = {
    setting: {
      findMany: async () => [
        { key: "account_pool_policy:1" },
        { key: "account_pool_policy:2" },
      ],
    },
      legacyImportMap: {
      findFirst: async ({ where }: { where: { legacyId: string } }) => {
        mappedLegacyId = where.legacyId;
        return { canonicalId: "22" };
      },
      },
      upstreamSyncTarget: {
      findUnique: async ({ where }: { where: { id: bigint } }) => {
        canonicalTargetId = where.id;
        return {
          id: 22,
          name: "disabled target",
          baseUrl: "https://disabled.example.test",
          adminApiKeyCipher: "unused",
          enabled: false,
        };
        },
        findMany: async () => [],
      },
  };

  const result = await runAccountPoolPolicies(db as never, new Date("2026-07-29T07:00:00Z"), undefined, 2);

  assert.deepEqual(result, []);
  assert.equal(mappedLegacyId, "2");
    assert.equal(canonicalTargetId, 22n);
});

test("normalizes account pool policy ranges and account exclusions", () => {
  const policy = normalizeAccountPoolPolicy({
    minAccountConcurrency: 250,
    maxAccountConcurrency: 20,
    failureWindow: 5,
    failureCount: 99,
    excludedAccountIds: [1431, "1430", 1431, -1, "bad"],
  });

  assert.equal(policy.minAccountConcurrency, 250);
  assert.equal(policy.maxAccountConcurrency, 250);
  assert.equal(policy.failureCount, 5);
  assert.deepEqual(policy.excludedAccountIds, [1430, 1431]);
});

test("runtime mutation retries a concurrent write and preserves burst ownership", async () => {
  let value = JSON.stringify({ burstEligibleAccountIds: [1], burst: null });
  let attempts = 0;
  const db = {
    setting: {
      findUnique: async () => ({ value }),
      updateMany: async ({ where, data }: { where: { value: string }; data: { value: string } }) => {
        attempts += 1;
        if (attempts === 1) {
          value = JSON.stringify({
            burstEligibleAccountIds: [1],
            burst: { managedConcurrencyBaselines: { "1": 500 } },
          });
          return { count: 0 };
        }
        if (where.value !== value) return { count: 0 };
        value = data.value;
        return { count: 1 };
      },
      upsert: async () => ({}),
    },
  };

  await mutateAccountPoolRuntime(db as never, 1, (runtime) => ({ ...runtime, lastStatus: "success" }));

  assert.equal(attempts, 2);
  assert.equal(JSON.parse(value).lastStatus, "success");
  assert.deepEqual(JSON.parse(value).burst.managedConcurrencyBaselines, { "1": 500 });
});

test("normalizes burst ceilings above the regular concurrency envelope", () => {
  const policy = normalizeAccountPoolPolicy({
    totalConcurrency: 5_000,
    maxAccountConcurrency: 500,
    burstTotalConcurrency: 100,
    burstMaxAccountConcurrency: 100,
  });

  assert.equal(policy.burstTotalConcurrency, 5_000);
  assert.equal(policy.burstMaxAccountConcurrency, 500);
});

test("burst RPM takes the largest global window instead of summing per-account peaks", () => {
  const summary = summarizeBurstTrafficRows([
    { account_id: 1, rpm_60: 80, rpm_30_scaled: 20, rpm_10_scaled: 6 },
    { account_id: 2, rpm_60: 10, rpm_30_scaled: 60, rpm_10_scaled: 12 },
  ]);

  assert.equal(summary.requestsPerMinute, 90);
  assert.equal(summary.completedRequestsLastMinute, 90);
  assert.deepEqual(summary.accountRequestsPerMinute, { "1": 80, "2": 60 });
});

test("rapid burst expands only saturated healthy accounts", () => {
  const policy = normalizeAccountPoolPolicy({ burstRpmThreshold: 1_000, burstStepPct: 20 });
  const result = planBurstConcurrency({
    accounts: [
      { id: 1, schedulable: true, concurrency: 100, currentConcurrency: 90 },
      { id: 2, schedulable: true, concurrency: 100, currentConcurrency: 95 },
    ],
    eligibleAccountIds: [1, 2],
    traffic: burstTraffic(1_200, [2]),
    policy,
    now: new Date("2026-07-28T07:00:00Z"),
  });

  assert.deepEqual(result.updates, [{ accountId: 1, concurrency: 120, direction: "expand" }]);
  assert.equal(result.evaluation.state, "expanding");
  assert.equal(result.evaluation.active, false);
  assert.equal(result.evaluation.degradedAccounts, 1);
});

test("rapid burst exceeds the regular account limit under RPM pressure", () => {
  const policy = normalizeAccountPoolPolicy({
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    burstRpmThreshold: 1_000,
    burstStepPct: 20,
  });
  const result = planBurstConcurrency({
    accounts: [{ id: 1, schedulable: true, concurrency: 500, currentConcurrency: 480 }],
    eligibleAccountIds: [1],
    traffic: burstTraffic(1_500),
    policy,
    now: new Date("2026-07-28T07:00:00Z"),
  });

  assert.deepEqual(result.updates, [{ accountId: 1, concurrency: 600, direction: "expand" }]);
  assert.equal(result.evaluation.active, true);
  assert.equal(result.evaluation.state, "expanding");
});

test("emergency saturation expands despite lagging completion RPM", () => {
  const policy = normalizeAccountPoolPolicy({
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    burstRpmThreshold: 1_000,
    burstStepPct: 20,
  });
  const result = planBurstConcurrency({
    accounts: [{ id: 1, schedulable: true, concurrency: 500, currentConcurrency: 490 }],
    eligibleAccountIds: [1],
    traffic: burstTraffic(10),
    policy,
    now: new Date("2026-07-28T07:00:00Z"),
  });

  assert.equal(result.updates[0]?.concurrency, 600);
  assert.equal(result.evaluation.state, "expanding");
});

test("burst capacity cools down before shrinking to the regular limit", () => {
  const policy = normalizeAccountPoolPolicy({
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    burstRpmThreshold: 1_000,
    burstStepPct: 20,
    burstScaleDownThresholdPct: 60,
    burstCooldownSeconds: 300,
  });
  const previous = {
    state: "holding" as const,
    active: true,
    requestsPerMinute: 1_200,
    completedRequestsLastMinute: 1_200,
    rpmThreshold: 1_000,
    currentLoad: 480,
    currentCapacity: 600,
    peakUtilizationPct: 80,
    thresholdPct: 80,
    regularAccountLimit: 500,
    burstAccountLimit: 1_000,
    burstTotalLimit: 6_000,
    eligibleAccounts: 1,
    degradedAccounts: 0,
    cooldownRemainingSeconds: 0,
    lastHighLoadAt: "2026-07-28T07:00:00Z",
    lastCheckedAt: "2026-07-28T07:00:00Z",
    updatedAccounts: 0,
    failedWrites: 0,
    managedConcurrencyBaselines: { "1": 500 },
  };
  const account = { id: 1, schedulable: true, concurrency: 600, currentConcurrency: 10 };
  const cooling = planBurstConcurrency({
    accounts: [account],
    eligibleAccountIds: [1],
    traffic: burstTraffic(10),
    policy,
    previous,
    now: new Date("2026-07-28T07:04:00Z"),
  });
  const shrinking = planBurstConcurrency({
    accounts: [account],
    eligibleAccountIds: [1],
    traffic: burstTraffic(10),
    policy,
    previous,
    now: new Date("2026-07-28T07:05:01Z"),
  });

  assert.equal(cooling.evaluation.state, "cooling_down");
  assert.equal(cooling.evaluation.cooldownRemainingSeconds, 60);
  assert.deepEqual(shrinking.updates, [{ accountId: 1, concurrency: 500, direction: "shrink" }]);
  assert.equal(shrinking.evaluation.state, "shrinking");
  assert.equal(shrinking.evaluation.active, false);
});

test("burst planning ignores a manually disabled account above the regular limit", () => {
  const policy = normalizeAccountPoolPolicy({
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    burstCooldownSeconds: 0,
  });
  const result = planBurstConcurrency({
    accounts: [{ id: 2099, schedulable: false, concurrency: 1_000, currentConcurrency: 0 }],
    eligibleAccountIds: [],
    traffic: burstTraffic(10),
    policy,
    now: new Date("2026-07-28T07:10:00Z"),
  });

  assert.deepEqual(result.updates, []);
  assert.equal(result.evaluation.active, false);
  assert.deepEqual(result.evaluation.managedConcurrencyBaselines, {});
});

test("burst planning preserves an unowned manual concurrency above the regular limit", () => {
  const policy = normalizeAccountPoolPolicy({
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    burstCooldownSeconds: 0,
  });
  const result = planBurstConcurrency({
    accounts: [{ id: 1, schedulable: true, concurrency: 800, currentConcurrency: 10 }],
    eligibleAccountIds: [1],
    traffic: burstTraffic(10),
    policy,
    previous: {
      state: "holding",
      active: false,
      requestsPerMinute: 1_200,
      completedRequestsLastMinute: 1_200,
      rpmThreshold: 1_000,
      currentLoad: 700,
      currentCapacity: 800,
      peakUtilizationPct: 87.5,
      thresholdPct: 80,
      regularAccountLimit: 500,
      burstAccountLimit: 1_000,
      burstTotalLimit: 6_000,
      eligibleAccounts: 1,
      degradedAccounts: 0,
      cooldownRemainingSeconds: 0,
      lastHighLoadAt: "2026-07-28T07:00:00Z",
      lastCheckedAt: "2026-07-28T07:00:00Z",
      updatedAccounts: 0,
      failedWrites: 0,
    },
    now: new Date("2026-07-28T07:10:00Z"),
  });

  assert.deepEqual(result.updates, []);
  assert.equal(result.evaluation.active, false);
  assert.deepEqual(result.evaluation.managedConcurrencyBaselines, {});
});

test("burst planning cannot expand a saturated unowned account above the regular limit", () => {
  const policy = normalizeAccountPoolPolicy({
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    burstRpmThreshold: 1_000,
  });
  const result = planBurstConcurrency({
    accounts: [{ id: 1, schedulable: true, concurrency: 800, currentConcurrency: 790 }],
    eligibleAccountIds: [1],
    traffic: burstTraffic(1_500),
    policy,
    now: new Date("2026-07-28T07:10:00Z"),
  });

  assert.deepEqual(result.updates, []);
  assert.equal(result.evaluation.active, false);
  assert.deepEqual(result.evaluation.managedConcurrencyBaselines, {});
});

test("owned disabled or degraded accounts remain eligible for safe shrink", () => {
  const policy = normalizeAccountPoolPolicy({
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    burstRpmThreshold: 1_000,
    burstCooldownSeconds: 0,
  });
  const previous = planBurstConcurrency({
    accounts: [{ id: 1, schedulable: true, concurrency: 500, currentConcurrency: 480 }],
    eligibleAccountIds: [1],
    traffic: burstTraffic(1_500),
    policy,
    now: new Date("2026-07-28T07:00:00Z"),
  }).evaluation;
  const result = planBurstConcurrency({
    accounts: [{ id: 1, schedulable: false, concurrency: 600, currentConcurrency: 10 }],
    eligibleAccountIds: [],
    traffic: burstTraffic(10, [1]),
    policy,
    previous,
    now: new Date("2026-07-28T07:00:01Z"),
  });

  assert.deepEqual(result.updates, [{ accountId: 1, concurrency: 500, direction: "shrink" }]);
  assert.deepEqual(result.evaluation.managedConcurrencyBaselines, {});
});

test("rapid burst applies the planned concurrency and persists runtime state", async () => {
  const policy = normalizeAccountPoolPolicy({
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    burstRpmThreshold: 1_000,
    burstStepPct: 20,
  });
  const runtime = { burstEligibleAccountIds: [1] };
  let persisted = "";
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1"
          ? { value: JSON.stringify(policy) }
          : { value: JSON.stringify(runtime) }
      ),
      upsert: async ({ update }: { update: { value: string } }) => { persisted = update.value; return {}; },
    },
  };
  const writes: Array<{ accountId: number; payload: Record<string, unknown> }> = [];
  const client = {
    listAccounts: async () => [{
      id: 1,
      name: "burst-ready",
      schedulable: true,
      concurrency: 500,
      current_concurrency: 480,
      rate_multiplier: 1,
      groups: [],
    }],
    setSchedulable: async () => undefined,
    updateAccount: async (accountId: number, payload: Record<string, unknown>) => { writes.push({ accountId, payload }); },
  };

  const result = await runRapidAccountPoolBurstForConnection(
    db as never,
    1,
    new Date("2026-07-28T07:00:00Z"),
    client,
    async () => burstTraffic(1_500),
  );

  assert.deepEqual(writes, [{ accountId: 1, payload: { concurrency: 600 } }]);
  assert.equal(result.burst.active, true);
  assert.equal(result.burst.updatedAccounts, 1);
  assert.equal(JSON.parse(persisted).burst.currentCapacity, 600);
});

test("rapid burst gives each platform an independent total concurrency budget", async () => {
  const policy = normalizeAccountPoolPolicy({
    totalConcurrency: 600,
    maxAccountConcurrency: 500,
    burstTotalConcurrency: 600,
    burstMaxAccountConcurrency: 1_000,
    burstRpmThreshold: 1_000,
    burstStepPct: 20,
  });
  const runtime = { burstEligibleAccountIds: [1, 2] };
  let persisted = "";
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1"
          ? { value: JSON.stringify(policy) }
          : { value: JSON.stringify(runtime) }
      ),
      upsert: async ({ update }: { update: { value: string } }) => { persisted = update.value; return {}; },
    },
  };
  const accounts = [
    { id: 1, name: "openai-burst", platform: "openai", schedulable: true, concurrency: 500, current_concurrency: 480, rate_multiplier: 1, groups: [] },
    { id: 2, name: "anthropic-burst", platform: "anthropic", schedulable: true, concurrency: 500, current_concurrency: 480, rate_multiplier: 1, groups: [] },
  ];
  const writes: Array<{ accountId: number; concurrency: number }> = [];
  const client = {
    listAccounts: async () => accounts.map((account) => ({ ...account })),
    setSchedulable: async () => undefined,
    updateAccount: async (accountId: number, payload: Record<string, unknown>) => {
      writes.push({ accountId, concurrency: Number(payload.concurrency) });
    },
  };

  const result = await runRapidAccountPoolBurstForConnection(
    db as never,
    1,
    new Date("2026-07-28T07:00:00Z"),
    client,
    async () => ({
      requestsPerMinute: 3_000,
      completedRequestsLastMinute: 3_000,
      accountRequestsPerMinute: { "1": 1_500, "2": 1_500 },
      degradedAccountIds: [],
    }),
  );

  assert.deepEqual(writes.sort((left, right) => left.accountId - right.accountId), [
    { accountId: 1, concurrency: 600 },
    { accountId: 2, concurrency: 600 },
  ]);
  assert.equal(result.burst.currentCapacity, 1_200);
  assert.equal(result.burst.burstTotalLimit, 1_200);
  assert.equal(result.burst.platforms?.openai.currentCapacity, 600);
  assert.equal(result.burst.platforms?.anthropic.currentCapacity, 600);
  assert.equal(JSON.parse(persisted).burst.platforms.openai.currentCapacity, 600);
});

test("disabled rapid burst skips remote account and traffic reads without managed baselines", async () => {
  const policy = normalizeAccountPoolPolicy({
    smartExpansionEnabled: false,
    burstExpansionEnabled: false,
  });
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1"
          ? { value: JSON.stringify(policy) }
          : { value: JSON.stringify({ burstEligibleAccountIds: [1], burst: null }) }
      ),
    },
  };
  let accountReads = 0;
  let trafficReads = 0;
  let writes = 0;
  const result = await runRapidAccountPoolBurstForConnection(
    db as never,
    1,
    new Date("2026-07-28T07:00:00Z"),
    {
      listAccounts: async () => { accountReads += 1; return []; },
      setSchedulable: async () => undefined,
      updateAccount: async () => { writes += 1; },
    },
    async () => { trafficReads += 1; return burstTraffic(0); },
  );

  assert.equal(result.ok, true);
  assert.equal(result.burst.state, "disabled");
  assert.equal(accountReads, 0);
  assert.equal(trafficReads, 0);
  assert.equal(writes, 0);
});

test("rapid burst records ownership after expansion and returns to its baseline", async () => {
  const policy = normalizeAccountPoolPolicy({
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    burstRpmThreshold: 1_000,
    burstStepPct: 20,
    burstScaleDownThresholdPct: 60,
    burstCooldownSeconds: 300,
  });
  let runtime: Record<string, unknown> = { burstEligibleAccountIds: [1] };
  let concurrency = 500;
  const writes: number[] = [];
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1"
          ? { value: JSON.stringify(policy) }
          : { value: JSON.stringify(runtime) }
      ),
      upsert: async ({ update }: { update: { value: string } }) => { runtime = JSON.parse(update.value); return {}; },
    },
  };
  const client = {
    listAccounts: async () => [{
      id: 1,
      name: "burst-owned",
      schedulable: true,
      concurrency,
      current_concurrency: concurrency === 500 ? 480 : 10,
      rate_multiplier: 1,
      groups: [],
    }],
    setSchedulable: async () => undefined,
    updateAccount: async (_accountId: number, payload: Record<string, unknown>) => {
      concurrency = Number(payload.concurrency);
      writes.push(concurrency);
    },
  };

  await runRapidAccountPoolBurstForConnection(
    db as never,
    1,
    new Date("2026-07-28T07:00:00Z"),
    client,
    async () => burstTraffic(1_500),
  );
  assert.deepEqual((runtime.burst as { managedConcurrencyBaselines: Record<string, number> }).managedConcurrencyBaselines, { "1": 500 });

  const result = await runRapidAccountPoolBurstForConnection(
    db as never,
    1,
    new Date("2026-07-28T07:05:01Z"),
    client,
    async () => burstTraffic(10),
  );

  assert.deepEqual(writes, [600, 500]);
  assert.equal(result.burst.active, false);
  assert.deepEqual(result.burst.managedConcurrencyBaselines, {});
});

test("rapid burst does not record ownership when the expansion write fails", async () => {
  const policy = normalizeAccountPoolPolicy({
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    burstRpmThreshold: 1_000,
  });
  let persisted = "";
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1"
          ? { value: JSON.stringify(policy) }
          : { value: JSON.stringify({ burstEligibleAccountIds: [1] }) }
      ),
      upsert: async ({ update }: { update: { value: string } }) => { persisted = update.value; return {}; },
    },
  };
  const client = {
    listAccounts: async () => [{
      id: 1,
      name: "burst-write-failure",
      schedulable: true,
      concurrency: 500,
      current_concurrency: 480,
      rate_multiplier: 1,
      groups: [],
    }],
    setSchedulable: async () => undefined,
    updateAccount: async () => { throw new Error("remote write failed"); },
  };

  const result = await runRapidAccountPoolBurstForConnection(
    db as never,
    1,
    new Date("2026-07-28T07:00:00Z"),
    client,
    async () => burstTraffic(1_500),
  );

  assert.equal(result.ok, false);
  assert.equal(result.burst.active, false);
  assert.deepEqual(JSON.parse(persisted).burst.managedConcurrencyBaselines, {});
});

test("rapid burst retains prewritten ownership when final runtime persistence fails", async () => {
  const policy = normalizeAccountPoolPolicy({
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    burstRpmThreshold: 1_000,
  });
  let runtimeValue = JSON.stringify({ burstEligibleAccountIds: [1] });
  let writes = 0;
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1"
          ? { value: JSON.stringify(policy) }
          : { value: runtimeValue }
      ),
      updateMany: async ({ where, data }: { where: { value: string }; data: { value: string } }) => {
        writes += 1;
        if (writes > 1) throw new Error("runtime persistence failed");
        assert.equal(where.value, runtimeValue);
        runtimeValue = data.value;
        return { count: 1 };
      },
      upsert: async () => ({}),
    },
  };
  let concurrency = 500;
  const client = {
    listAccounts: async () => [{
      id: 1,
      name: "durable-owner",
      schedulable: true,
      concurrency,
      current_concurrency: 480,
      rate_multiplier: 1,
      groups: [],
    }],
    setSchedulable: async () => undefined,
    updateAccount: async (_accountId: number, payload: Record<string, unknown>) => { concurrency = Number(payload.concurrency); },
  };

  await assert.rejects(
    () => runRapidAccountPoolBurstForConnection(
      db as never,
      1,
      new Date("2026-07-28T07:00:00Z"),
      client,
      async () => burstTraffic(1_500),
    ),
    /runtime persistence failed/,
  );

  assert.equal(concurrency, 600);
  assert.deepEqual(JSON.parse(runtimeValue).burst.managedConcurrencyBaselines, { "1": 500 });
});

test("rapid burst keeps ownership when a disabled account shrink write fails", async () => {
  const policy = normalizeAccountPoolPolicy({
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    burstRpmThreshold: 1_000,
    burstCooldownSeconds: 0,
  });
  const previous = planBurstConcurrency({
    accounts: [{ id: 1, schedulable: true, concurrency: 500, currentConcurrency: 480 }],
    eligibleAccountIds: [1],
    traffic: burstTraffic(1_500),
    policy,
    now: new Date("2026-07-28T07:00:00Z"),
  }).evaluation;
  let persisted = "";
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1"
          ? { value: JSON.stringify(policy) }
          : { value: JSON.stringify({ burstEligibleAccountIds: [], burst: previous }) }
      ),
      upsert: async ({ update }: { update: { value: string } }) => { persisted = update.value; return {}; },
    },
  };
  const client = {
    listAccounts: async () => [{
      id: 1,
      name: "disabled-owned",
      schedulable: false,
      concurrency: 600,
      current_concurrency: 10,
      rate_multiplier: 1,
      groups: [],
    }],
    setSchedulable: async () => undefined,
    updateAccount: async () => { throw new Error("shrink failed"); },
  };

  const result = await runRapidAccountPoolBurstForConnection(
    db as never,
    1,
    new Date("2026-07-28T07:00:01Z"),
    client,
    async () => burstTraffic(10, [1]),
  );

  assert.equal(result.ok, false);
  assert.deepEqual(JSON.parse(persisted).burst.managedConcurrencyBaselines, { "1": 500 });
});

test("concurrent rapid expansions merge ownership for different accounts", async () => {
  const policy = normalizeAccountPoolPolicy({
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    burstRpmThreshold: 1_000,
  });
  let runtimeValue = JSON.stringify({ burstEligibleAccountIds: [1, 2] });
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1"
          ? { value: JSON.stringify(policy) }
          : { value: runtimeValue }
      ),
      updateMany: async ({ where, data }: { where: { value: string }; data: { value: string } }) => {
        if (where.value !== runtimeValue) return { count: 0 };
        runtimeValue = data.value;
        return { count: 1 };
      },
      upsert: async () => ({}),
    },
  };
  const accounts = [
    { id: 1, name: "burst-one", schedulable: true, concurrency: 500, current_concurrency: 480, rate_multiplier: 1, groups: [] },
    { id: 2, name: "burst-two", schedulable: true, concurrency: 500, current_concurrency: 480, rate_multiplier: 1, groups: [] },
  ];
  const writes: number[] = [];
  const client = {
    listAccounts: async () => accounts.map((account) => ({ ...account })),
    setSchedulable: async () => undefined,
    updateAccount: async (accountId: number) => { writes.push(accountId); },
  };

  await Promise.all([
    runRapidAccountPoolBurstForConnection(
      db as never,
      1,
      new Date("2026-07-28T07:00:00Z"),
      client,
      async () => burstTraffic(1_500, [2]),
    ),
    runRapidAccountPoolBurstForConnection(
      db as never,
      1,
      new Date("2026-07-28T07:00:00Z"),
      client,
      async () => burstTraffic(1_500, [1]),
    ),
  ]);

  assert.deepEqual(writes.sort((left, right) => left - right), [1, 2]);
  assert.deepEqual(JSON.parse(runtimeValue).burst.managedConcurrencyBaselines, { "1": 500, "2": 500 });
  assert.deepEqual(JSON.parse(runtimeValue).burst.managedConcurrencyTokens, {});
});

test("a concurrent rapid run cannot steal or erase a pending expansion", async () => {
  const policy = normalizeAccountPoolPolicy({
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    burstRpmThreshold: 1_000,
  });
  let runtimeValue = JSON.stringify({ burstEligibleAccountIds: [1] });
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1"
          ? { value: JSON.stringify(policy) }
          : { value: runtimeValue }
      ),
      updateMany: async ({ where, data }: { where: { value: string }; data: { value: string } }) => {
        if (where.value !== runtimeValue) return { count: 0 };
        runtimeValue = data.value;
        return { count: 1 };
      },
      upsert: async () => ({}),
    },
  };
  const account = {
    id: 1,
    name: "single-owner",
    schedulable: true,
    concurrency: 500,
    current_concurrency: 480,
    rate_multiplier: 1,
    groups: [],
  };
  let releaseFirstWrite = () => {};
  const firstWriteGate = new Promise<void>((resolve) => { releaseFirstWrite = resolve; });
  let firstReachedRemote = () => {};
  const firstReachedRemoteGate = new Promise<void>((resolve) => { firstReachedRemote = resolve; });
  let firstWrites = 0;
  let secondWrites = 0;
  const firstClient = {
    listAccounts: async () => [{ ...account }],
    setSchedulable: async () => undefined,
    updateAccount: async () => {
      firstWrites += 1;
      firstReachedRemote();
      await firstWriteGate;
    },
  };
  const secondClient = {
    listAccounts: async () => [{ ...account }],
    setSchedulable: async () => undefined,
    updateAccount: async () => { secondWrites += 1; throw new Error("second writer must not run"); },
  };
  const now = new Date("2026-07-28T07:00:00Z");

  const first = runRapidAccountPoolBurstForConnection(db as never, 1, now, firstClient, async () => burstTraffic(1_500));
  await firstReachedRemoteGate;
  const second = await runRapidAccountPoolBurstForConnection(db as never, 1, now, secondClient, async () => burstTraffic(1_500));
  releaseFirstWrite();
  await first;

  assert.equal(second.ok, true);
  assert.equal(firstWrites, 1);
  assert.equal(secondWrites, 0);
  assert.deepEqual(JSON.parse(runtimeValue).burst.managedConcurrencyBaselines, { "1": 500 });
  assert.deepEqual(JSON.parse(runtimeValue).burst.managedConcurrencyTokens, {});
});

test("full pool policy preserves unowned manual concurrency above the regular limit", async () => {
  const policy = normalizeAccountPoolPolicy({
    minAvailableAccounts: 1,
    targetHealthyAccounts: 1,
    maxAccountConcurrency: 500,
    burstMaxAccountConcurrency: 1_000,
    loadFactorEnabled: false,
    priorityEnabled: false,
  });
  const remoteUpdates: Array<Record<string, unknown>> = [];
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1"
          ? { value: JSON.stringify(policy) }
          : { value: JSON.stringify({
            burstEligibleAccountIds: [1],
            burst: { active: true, managedConcurrencyBaselines: { "2": 500 } },
          }) }
      ),
      upsert: async () => ({}),
    },
    upstreamMonitorRule: { findMany: async () => [], update: async () => ({}) },
    upstreamMonitorResult: { findMany: async () => [] },
  };
  const client = {
    listAccounts: async () => [
      { id: 1, name: "manual-800", schedulable: true, concurrency: 800, current_concurrency: 0, rate_multiplier: 1, groups: [] },
      { id: 2, name: "burst-owned", schedulable: true, concurrency: 600, current_concurrency: 0, rate_multiplier: 1, groups: [] },
      { id: 2099, name: "manual-disabled-1000", schedulable: false, concurrency: 1_000, current_concurrency: 0, rate_multiplier: 1, groups: [] },
    ],
    setSchedulable: async () => undefined,
    updateAccount: async (_accountId: number, payload: Record<string, unknown>) => { remoteUpdates.push(payload); },
  };

  await runAccountPoolPolicyForConnection(
    db as never,
    1,
    new Date("2026-07-28T07:00:00Z"),
    client,
    async () => new Map(),
  );

  assert.deepEqual(remoteUpdates, []);
});

test("health score weights recent results and triggers default failure rule", () => {
  const policy = normalizeAccountPoolPolicy({});
  const health = calculateAccountHealth(42, [
    { status: "failed", message: "HTTP 502", firstTokenMs: null },
    { status: "failed", message: "HTTP 503", firstTokenMs: null },
    { status: "failed", message: "probe timeout", firstTokenMs: null },
    { status: "success", message: "ok", firstTokenMs: 500 },
    { status: "success", message: "ok", firstTokenMs: 700 },
  ], policy);

  assert.equal(health.recentFailureCount, 3);
  assert.equal(health.consecutiveFailureCount, 3);
  assert.equal(health.sampleCount, 5);
  assert.ok((health.score ?? 100) < 60);
  assert.equal(shouldDisableAccount(health, policy), true);
});

test("balance exhaustion remains a Sub2API temporary state and never selects a permanent disable", () => {
  const policy = normalizeAccountPoolPolicy({ minAvailableAccounts: 1 });
  const exhausted = calculateAccountHealth(42, [
    { status: "failed", message: "HTTP 402: balance exhausted", firstTokenMs: null },
    { status: "failed", message: "HTTP 402: balance exhausted", firstTokenMs: null },
    { status: "failed", message: "HTTP 402: balance exhausted", firstTokenMs: null },
  ], policy);

  assert.equal(exhausted.temporaryUnavailable, true);
  assert.equal(shouldDisableAccount(exhausted, policy), false);
  assert.deepEqual(selectAccountsToDisable([{ accountId: 42, schedulable: true, health: exhausted }], policy), []);

  const recovered = calculateAccountHealth(42, [
    { status: "success", message: "balance check recovered", firstTokenMs: 500 },
    { status: "failed", message: "HTTP 402: balance exhausted", firstTokenMs: null },
  ], policy);
  assert.equal(recovered.temporaryUnavailable, false);
});

test("a successful recovery resets historical failures before policy disable", () => {
  const policy = normalizeAccountPoolPolicy({});
  const health = calculateAccountHealth(17, [
    { status: "success", message: "stream probe recovered", firstTokenMs: 2_795 },
    { status: "failed", message: "HTTP 503", firstTokenMs: null },
    { status: "failed", message: "HTTP 503", firstTokenMs: null },
    { status: "failed", message: "HTTP 503", firstTokenMs: null },
    { status: "failed", message: "HTTP 503", firstTokenMs: null },
  ], policy);

  assert.equal(health.recentFailureCount, 4);
  assert.equal(health.consecutiveFailureCount, 0);
  assert.ok((health.score ?? 100) < 60);
  assert.equal(shouldDisableAccount(health, policy), false);
});

test("slow first-token rule disables an otherwise successful account", () => {
  const policy = normalizeAccountPoolPolicy({});
  const health = calculateAccountHealth(7, Array.from({ length: 10 }, (_, index) => ({
    status: "success",
    message: "ok",
    firstTokenMs: index < 5 ? 20_000 : 500,
  })), policy);

  assert.equal(health.recentSlowCount, 5);
  assert.equal(shouldDisableAccount(health, policy), true);
});

test("passive health grades real traffic without granting disable authority", () => {
  const policy = normalizeAccountPoolPolicy({});
  const healthy = calculatePassiveAccountHealth({
    accountId: 1,
    gatewayErrors: 1,
    successes: 19,
    slowSuccesses: 0,
    averageFirstTokenMs: 1_200,
  }, policy);
  const degraded = calculatePassiveAccountHealth({
    accountId: 2,
    gatewayErrors: 6,
    successes: 6,
    slowSuccesses: 2,
    averageFirstTokenMs: 18_000,
  }, policy);
  const sparse = calculatePassiveAccountHealth({
    accountId: 3,
    gatewayErrors: 2,
    successes: 3,
    slowSuccesses: 0,
    averageFirstTokenMs: 500,
  }, policy);
  const burst = calculatePassiveAccountHealth({
    accountId: 4,
    gatewayErrors: 10,
    successes: 90,
    slowSuccesses: 0,
    averageFirstTokenMs: 500,
  }, policy);
  const mildlyDegraded = calculatePassiveAccountHealth({
    accountId: 5,
    gatewayErrors: 2,
    successes: 17,
    slowSuccesses: 0,
    averageFirstTokenMs: 1_200,
  }, policy);

  assert.equal(healthy.score, 95);
  assert.equal(degraded.score, policy.failureHealthThreshold);
  assert.equal(degraded.consecutiveFailureCount, 0);
  assert.equal(sparse.score, null);
  assert.equal(burst.score, policy.failureHealthThreshold);
  assert.equal(mildlyDegraded.score, 89);
  assert.equal(calculateAccountPriority(mildlyDegraded, 1, policy.slowFirstTokenMs), 1);
});

test("real traffic contributes most of the effective scheduling score", () => {
  const policy = normalizeAccountPoolPolicy({});
  const monitor = calculateAccountHealth(7, Array.from({ length: 10 }, () => ({
    status: "success" as const,
    message: "ok",
    firstTokenMs: 800,
  })), policy);
  const passive = calculatePassiveAccountHealth({
    accountId: 7,
    gatewayErrors: 6,
    successes: 6,
    slowSuccesses: 1,
    averageFirstTokenMs: 4_000,
  }, policy);
  const merged = mergeAccountHealth(monitor, passive);

  assert.equal(merged.score, 74);
  assert.equal(merged.consecutiveFailureCount, 0);
  assert.equal(merged.averageFirstTokenMs, 4_000);
});

test("load factors honor total, bounds, health, and price protection", () => {
  const policy = normalizeAccountPoolPolicy({
    totalLoadFactor: 400,
    minAccountLoadFactor: 20,
    maxAccountLoadFactor: 500,
  });
  const allocation = allocateLoadFactors([
    { accountId: 1, healthScore: 90, priceFactor: 1 },
    { accountId: 2, healthScore: 90, priceFactor: 0.5 },
  ], policy);

  assert.equal(Array.from(allocation.values()).reduce((sum, value) => sum + value, 0), 400);
  assert.ok((allocation.get(1) ?? 0) > (allocation.get(2) ?? 0));
  assert.ok((allocation.get(2) ?? 0) >= 20);
  assert.equal(accountPriceProtectionFactor({ rateMultiplier: 0.08, groupRateMultipliers: [0.04, 0.06] }), 0.5);
  assert.equal(accountPriceProtectionFactor({ rateMultiplier: 0.04, groupRateMultipliers: [0.08] }), 1);
});

test("disable selection preserves the configured minimum available pool", () => {
  const policy = normalizeAccountPoolPolicy({ minAvailableAccounts: 2 });
  const selected = selectAccountsToDisable([
    { accountId: 1, schedulable: true, health: { accountId: 1, score: 10, sampleCount: 5, recentFailureCount: 5, consecutiveFailureCount: 5, recentSlowCount: 0, averageFirstTokenMs: null } },
    { accountId: 2, schedulable: true, health: { accountId: 2, score: 20, sampleCount: 5, recentFailureCount: 5, consecutiveFailureCount: 5, recentSlowCount: 0, averageFirstTokenMs: null } },
    { accountId: 3, schedulable: true, health: { accountId: 3, score: 30, sampleCount: 5, recentFailureCount: 5, consecutiveFailureCount: 5, recentSlowCount: 0, averageFirstTokenMs: null } },
  ], policy);

  assert.deepEqual(selected, [1]);
});

test("health target and automation safety minimum are independent", () => {
  const policy = normalizeAccountPoolPolicy({ minAvailableAccounts: 3, targetHealthyAccounts: 1 });
  assert.equal(meetsHealthyPoolTarget(1, policy), true);
  assert.equal(meetsMinimumHealthyPool(1, policy), false);
  assert.equal(meetsMinimumHealthyPool(3, policy), true);
});

test("automatic priority keeps healthy accounts in one real-time load balancing tier", () => {
  const preferred = calculateAccountPriority({ score: 95, averageFirstTokenMs: 600 }, 1, 15_000);
  const unknown = calculateAccountPriority({ score: null, averageFirstTokenMs: null }, 1, 15_000);
  const expensive = calculateAccountPriority({ score: 95, averageFirstTokenMs: 600 }, 0.5, 15_000);
  const slow = calculateAccountPriority({ score: 95, averageFirstTokenMs: 20_000 }, 1, 15_000);
  const degraded = calculateAccountPriority({ score: 65, averageFirstTokenMs: 600 }, 1, 15_000);

  assert.equal(preferred, 1);
  assert.equal(unknown, 1);
  assert.equal(expensive, 1);
  assert.ok(slow > preferred);
  assert.ok(degraded > preferred);
  assert.equal(slow, 30);
  assert.equal(degraded, 30);
});

test("minimum healthy pool allows balancing below the operational health target", async () => {
  const policy = normalizeAccountPoolPolicy({
    minAvailableAccounts: 1,
    targetHealthyAccounts: 3,
    totalConcurrency: 3_000,
    expansionLoadThresholdPct: 80,
    totalLoadFactor: 200,
    minAccountLoadFactor: 30,
    maxAccountLoadFactor: 200,
    loadFactorChangeThresholdPct: 0,
    loadFactorCooldownSeconds: 0,
    priorityEnabled: false,
  });
  const remoteUpdates: Array<Record<string, unknown>> = [];
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1" ? { value: JSON.stringify(policy) } : null
      ),
      upsert: async () => ({}),
    },
    upstreamMonitorRule: {
      findMany: async () => [{
        id: 10,
        accountId: 328,
        pauseMinutes: 30,
        pausedUntil: null,
        pauseStartedAt: null,
        nextCheckAt: new Date("2026-07-26T00:05:00Z"),
        lastStatus: "success",
      }],
      update: async () => ({}),
    },
    upstreamMonitorResult: {
      findMany: async () => [{ accountId: 328, status: "success", message: "ok", firstTokenMs: 1_038 }],
    },
  };
  const client = {
    listAccounts: async () => [{
      id: 328,
      name: "healthy",
      schedulable: true,
      concurrency: 40,
      current_concurrency: 0,
      load_factor: 10,
      rate_multiplier: 0.15,
      groups: [],
    }],
    setSchedulable: async () => undefined,
    updateAccount: async (_accountId: number, payload: Record<string, unknown>) => { remoteUpdates.push(payload); },
  };

  const result = await runAccountPoolPolicyForConnection(
    db as never,
    1,
    new Date("2026-07-26T00:00:00Z"),
    client,
  );

  assert.deepEqual(remoteUpdates, [{ load_factor: 200 }]);
  assert.equal(result.actions.concurrencyUpdated, 0);
  assert.equal(result.actions.loadFactorUpdated, 1);
  assert.equal(result.evaluation.expansion.state, "waiting_for_load");
  assert.equal(result.evaluation.loadFactor.state, "adjusted");
  assert.deepEqual(result.evaluation.healthyPool, {
    current: 1,
    minimum: 1,
    target: 3,
    minimumMet: true,
    targetMet: false,
  });
});

test("unmonitored accounts are enrolled in automatic concurrency, load, and priority", async () => {
  const policy = normalizeAccountPoolPolicy({
    minAvailableAccounts: 1,
    minAccountConcurrency: 20,
    maxAccountConcurrency: 250,
    totalLoadFactor: 100,
    minAccountLoadFactor: 20,
    maxAccountLoadFactor: 500,
    loadFactorChangeThresholdPct: 0,
    loadFactorCooldownSeconds: 0,
  });
  const remoteUpdates: Array<Record<string, unknown>> = [];
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1" ? { value: JSON.stringify(policy) } : null
      ),
      upsert: async () => ({}),
    },
    upstreamMonitorRule: {
      findMany: async () => [],
      update: async () => ({}),
    },
    upstreamMonitorResult: { findMany: async () => [] },
  };
  const client = {
    listAccounts: async () => [{
      id: 91,
      name: "new account",
      schedulable: true,
      concurrency: 3,
      current_concurrency: 0,
      load_factor: null,
      priority: 50,
      rate_multiplier: 0.04,
      groups: [{ rate_multiplier: 0.04 }],
    }],
    setSchedulable: async () => undefined,
    updateAccount: async (_accountId: number, payload: Record<string, unknown>) => { remoteUpdates.push(payload); },
  };

  const result = await runAccountPoolPolicyForConnection(db as never, 1, new Date("2026-07-28T00:00:00Z"), client);

  assert.deepEqual(remoteUpdates, [
    { concurrency: 20 },
    { load_factor: 100 },
    { priority: 1 },
  ]);
  assert.equal(result.evaluation.managedAccounts, 1);
  assert.equal(result.evaluation.unknownHealthAccounts, 1);
  assert.deepEqual(result.actions, {
    disabled: 0,
    returned: 0,
    concurrencyUpdated: 1,
    loadFactorUpdated: 1,
    priorityUpdated: 1,
    failed: 0,
  });
});

test("manual execution rejects a policy that has never been saved", async () => {
  const db = {
    setting: { findUnique: async () => null },
  };
  const client = {
    listAccounts: async () => { throw new Error("remote client should not be called"); },
    setSchedulable: async () => undefined,
    updateAccount: async () => undefined,
  };

  await assert.rejects(
    () => runAccountPoolPolicyForConnection(db as never, 1, new Date("2026-07-26T00:00:00Z"), client),
    /policy is not configured/i,
  );
});

test("a local pause-write failure never disables the remote account", async () => {
  const policy = normalizeAccountPoolPolicy({ minAvailableAccounts: 1 });
  const remoteWrites: boolean[] = [];
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1" ? { value: JSON.stringify(policy) } : null
      ),
      upsert: async () => ({}),
    },
    upstreamMonitorRule: {
      findMany: async () => [
        {
          id: 10,
          accountId: 1,
          pauseMinutes: 5,
          pausedUntil: null,
          pauseStartedAt: null,
          nextCheckAt: new Date("2026-07-26T00:05:00Z"),
          lastStatus: "failed",
        },
        {
          id: 11,
          accountId: 2,
          pauseMinutes: 5,
          pausedUntil: null,
          pauseStartedAt: null,
          nextCheckAt: new Date("2026-07-26T00:05:00Z"),
          lastStatus: "success",
        },
      ],
      update: async ({ where }: { where: { id: number } }) => {
        if (where.id === 10) throw new Error("database write failed");
        return {};
      },
    },
    upstreamMonitorResult: {
      findMany: async () => [
        { accountId: 1, status: "failed", message: "HTTP 502", firstTokenMs: null },
        { accountId: 1, status: "failed", message: "HTTP 503", firstTokenMs: null },
        { accountId: 1, status: "failed", message: "HTTP 504", firstTokenMs: null },
        { accountId: 2, status: "success", message: "ok", firstTokenMs: 500 },
      ],
    },
  };
  const client = {
    listAccounts: async () => [
      { id: 1, name: "bad", schedulable: true, concurrency: 20, current_concurrency: 0, rate_multiplier: 1, groups: [] },
      { id: 2, name: "good", schedulable: true, concurrency: 20, current_concurrency: 0, rate_multiplier: 1, groups: [] },
    ],
    setSchedulable: async (_accountId: number, value: boolean) => { remoteWrites.push(value); },
    updateAccount: async () => undefined,
  };

  const result = await runAccountPoolPolicyForConnection(
    db as never,
    1,
    new Date("2026-07-26T00:00:00Z"),
    client,
  );

  assert.deepEqual(remoteWrites, []);
  assert.equal(result.actions.disabled, 0);
  assert.equal(result.actions.failed, 1);
});

test("a failed remote disable restores the prior local pause state", async () => {
  const policy = normalizeAccountPoolPolicy({ minAvailableAccounts: 1 });
  const updates: Array<Record<string, unknown>> = [];
  const previousNextCheckAt = new Date("2026-07-26T00:05:00Z");
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1" ? { value: JSON.stringify(policy) } : null
      ),
      upsert: async () => ({}),
    },
    upstreamMonitorRule: {
      findMany: async () => [
        {
          id: 10,
          accountId: 1,
          pauseMinutes: 5,
          pausedUntil: null,
          pauseStartedAt: null,
          nextCheckAt: previousNextCheckAt,
          lastStatus: "failed",
        },
        {
          id: 11,
          accountId: 2,
          pauseMinutes: 5,
          pausedUntil: null,
          pauseStartedAt: null,
          nextCheckAt: previousNextCheckAt,
          lastStatus: "success",
        },
      ],
      update: async ({ data }: { data: Record<string, unknown> }) => { updates.push(data); return {}; },
    },
    upstreamMonitorResult: {
      findMany: async () => [
        { accountId: 1, status: "failed", message: "HTTP 502", firstTokenMs: null },
        { accountId: 1, status: "failed", message: "HTTP 503", firstTokenMs: null },
        { accountId: 1, status: "failed", message: "HTTP 504", firstTokenMs: null },
        { accountId: 2, status: "success", message: "ok", firstTokenMs: 500 },
      ],
    },
  };
  const client = {
    listAccounts: async () => [
      { id: 1, name: "bad", schedulable: true, concurrency: 20, current_concurrency: 0, rate_multiplier: 1, groups: [] },
      { id: 2, name: "good", schedulable: true, concurrency: 20, current_concurrency: 0, rate_multiplier: 1, groups: [] },
    ],
    setSchedulable: async () => { throw new Error("remote write failed"); },
    updateAccount: async () => undefined,
  };

  const result = await runAccountPoolPolicyForConnection(
    db as never,
    1,
    new Date("2026-07-26T00:00:00Z"),
    client,
  );

  assert.equal(updates.length, 2);
  assert.deepEqual(updates[1], {
    pausedUntil: null,
    pauseStartedAt: null,
    nextCheckAt: previousNextCheckAt,
  });
  assert.equal(result.actions.disabled, 0);
  assert.equal(result.actions.failed, 1);
});

test("failure isolation preserves the minimum schedulable accounts for every platform", async () => {
  const policy = normalizeAccountPoolPolicy({
    minAvailableAccounts: 1,
    smartExpansionEnabled: false,
    loadFactorEnabled: false,
    priorityEnabled: false,
  });
  const disabled: number[] = [];
  const now = new Date("2026-08-07T00:00:00Z");
  const rules = [
    { id: 1, accountId: 1, pauseMinutes: 5, pausedUntil: null, pauseStartedAt: null, nextCheckAt: now, lastStatus: "failed" },
    { id: 2, accountId: 2, pauseMinutes: 5, pausedUntil: null, pauseStartedAt: null, nextCheckAt: now, lastStatus: "failed" },
    { id: 3, accountId: 3, pauseMinutes: 5, pausedUntil: null, pauseStartedAt: null, nextCheckAt: now, lastStatus: "success" },
    { id: 4, accountId: 4, pauseMinutes: 5, pausedUntil: null, pauseStartedAt: null, nextCheckAt: now, lastStatus: "success" },
  ];
  const failures = (accountId: number) => Array.from({ length: 3 }, () => ({
    accountId,
    status: "failed",
    message: "HTTP 503",
    firstTokenMs: null,
  }));
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1" ? { value: JSON.stringify(policy) } : null
      ),
      upsert: async () => ({}),
    },
    upstreamMonitorRule: {
      findMany: async () => rules,
      update: async () => ({}),
    },
    upstreamMonitorResult: {
      findMany: async () => [
        ...failures(1),
        ...failures(2),
        { accountId: 3, status: "success", message: "ok", firstTokenMs: 500 },
        { accountId: 4, status: "success", message: "ok", firstTokenMs: 600 },
      ],
    },
  };
  const client = {
    listAccounts: async () => [
      { id: 1, name: "anthropic-a", platform: "anthropic", schedulable: true, concurrency: 20, current_concurrency: 0, rate_multiplier: 1, groups: [] },
      { id: 2, name: "anthropic-b", platform: "anthropic", schedulable: true, concurrency: 20, current_concurrency: 0, rate_multiplier: 1, groups: [] },
      { id: 3, name: "openai-a", platform: "openai", schedulable: true, concurrency: 20, current_concurrency: 0, rate_multiplier: 1, groups: [] },
      { id: 4, name: "openai-b", platform: "openai", schedulable: true, concurrency: 20, current_concurrency: 0, rate_multiplier: 1, groups: [] },
    ],
    setSchedulable: async (accountId: number, schedulable: boolean) => {
      if (!schedulable) disabled.push(accountId);
    },
    updateAccount: async () => undefined,
  };

  const result = await runAccountPoolPolicyForConnection(db as never, 1, now, client);

  assert.deepEqual(disabled, [1]);
  assert.equal(result.evaluation.platforms.anthropic.schedulableAccounts, 1);
  assert.equal(result.evaluation.platforms.openai.schedulableAccounts, 2);
});

test("load-factor budgets are allocated independently for each platform", async () => {
  const policy = normalizeAccountPoolPolicy({
    minAvailableAccounts: 1,
    smartExpansionEnabled: false,
    loadFactorEnabled: true,
    totalLoadFactor: 100,
    minAccountLoadFactor: 20,
    maxAccountLoadFactor: 500,
    loadFactorChangeThresholdPct: 0,
    loadFactorCooldownSeconds: 0,
    priorityEnabled: false,
    failureDisableEnabled: false,
  });
  const writes = new Map<number, Record<string, unknown>>();
  const db = {
    setting: {
      findUnique: async ({ where }: { where: { key: string } }) => (
        where.key === "account_pool_policy:1" ? { value: JSON.stringify(policy) } : null
      ),
      upsert: async () => ({}),
    },
    upstreamMonitorRule: { findMany: async () => [], update: async () => ({}) },
    upstreamMonitorResult: { findMany: async () => [] },
  };
  const client = {
    listAccounts: async () => [
      { id: 11, name: "openai-a", platform: "openai", schedulable: true, concurrency: 20, current_concurrency: 0, load_factor: 1, rate_multiplier: 1, groups: [] },
      { id: 12, name: "openai-b", platform: "openai", schedulable: true, concurrency: 20, current_concurrency: 0, load_factor: 1, rate_multiplier: 1, groups: [] },
      { id: 21, name: "anthropic-a", platform: "anthropic", schedulable: true, concurrency: 20, current_concurrency: 0, load_factor: 1, rate_multiplier: 1, groups: [] },
    ],
    setSchedulable: async () => undefined,
    updateAccount: async (accountId: number, payload: Record<string, unknown>) => { writes.set(accountId, payload); },
  };

  const result = await runAccountPoolPolicyForConnection(
    db as never,
    1,
    new Date("2026-08-07T00:00:00Z"),
    client,
  );

  assert.equal(writes.get(11)?.load_factor, 50);
  assert.equal(writes.get(12)?.load_factor, 50);
  assert.equal(writes.get(21)?.load_factor, 100);
  assert.equal(result.evaluation.platforms.openai.loadFactor.targetTotal, 100);
  assert.equal(result.evaluation.platforms.anthropic.loadFactor.targetTotal, 100);
});
