import assert from "node:assert/strict";
import { mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { Sub2ApiAdminClient } from "@/server/clients/sub2api-admin";
import { applyBoundRateRules } from "@/server/bl-rate-sync";
import { evaluateGroupRateRule } from "@/server/rate-rule-evaluator";
import {
  groupAccountRateConflictReasons,
  isAccountEnabledForScheduling,
  syncGroupAccountRateMultipliers,
} from "@/server/group-account-rate-sync";

function account(id: number, groupIds: number[], rateMultiplier: number) {
  return {
    id,
    name: `account-${id}`,
    status: "active",
    schedulable: true,
    group_ids: groupIds,
    rate_multiplier: rateMultiplier,
  };
}

function groupRuleHarness(input: {
  enabledAccountRateGroupIds: number[];
  updateAccountRateMultiplier?: (accountId: number, rateMultiplier: number) => Promise<void>;
}) {
  let groupBindingQueries = 0;
  let enabledModes: unknown;
  const groupBinding = {
    id: 1,
    connectionId: 1,
    targetType: "group",
    targetId: 17,
    sourceSiteId: 1,
    sourceSiteName: "source",
    sourceGroupId: "33",
    sourceGroupName: "source-group",
    sourcePlatform: "openai",
  };
  const rule = {
    id: 1,
    connectionId: 1,
    groupId: 17,
    enabled: true,
    mode: "manual_source" as const,
    offset: 0.05,
    expression: null,
  };
  const currentGroupRate = evaluateGroupRateRule({
    rule,
    sourceRates: [],
    currentRate: 1,
  });
  const db = {
    blSourceBinding: {
      findMany: async (args: { where?: { targetType?: string } }) => {
        if (args.where?.targetType === "account") return [];
        groupBindingQueries += 1;
        return [groupBinding];
      },
    },
    blGroupRateRule: {
      findMany: async (args: { where?: { mode?: unknown }; select?: unknown }) => {
        if (args.select) {
          enabledModes = args.where?.mode;
          return input.enabledAccountRateGroupIds.map((groupId) => ({ groupId }));
        }
        return [rule];
      },
    },
    upstreamMonitorRateExclusion: { findMany: async () => [] },
    upstreamMonitorRule: { findMany: async () => [] },
  };
  const rateClient = { fetchRates: async () => [] };
  const s2Client = {
    getGroup: async () => ({ id: 17, name: "target", rate_multiplier: currentGroupRate }),
    getAccount: async (accountId: number) => account(accountId, [17], 0.015),
    updateGroupRateMultiplier: async () => assert.fail("group rate is already current"),
    updateAccountRateMultiplier: input.updateAccountRateMultiplier ?? (async () => undefined),
  };

  return {
    db,
    rateClient,
    s2Client,
    getGroupBindingQueries: () => groupBindingQueries,
    getEnabledModes: () => enabledModes,
    currentGroupRate,
  };
}

async function createLogDir(prefix: string) {
  const logDir = await mkdtemp(path.join(tmpdir(), prefix));
  await writeFile(
    path.join(logDir, "settings.json"),
    `${JSON.stringify({ enabled: true, retentionDays: 30, minLevel: "info" })}\n`,
    "utf8",
  );
  return logDir;
}

test("groupAccountRateConflictReasons identifies overlapping enabled rules", () => {
  const reasons = groupAccountRateConflictReasons({
    accounts: [
      account(631, [17, 72, 85], 0.015),
      account(632, [17, 72], 0.05),
      account(633, [17, 85, 85], 0.015),
    ],
    enabledGroupIds: new Set([17, 85]),
  });

  assert.match(reasons.get(631) ?? "", /17,85$/);
  assert.equal(reasons.has(632), false);
  assert.match(reasons.get(633) ?? "", /17,85$/);
});

test("Sub2ApiAdminClient updates an account rate through the field-level bulk endpoint", async () => {
  const originalFetch = globalThis.fetch;
  let requestUrl = "";
  let requestInit: RequestInit | undefined;
  globalThis.fetch = async (input, init) => {
    requestUrl = String(input);
    requestInit = init;
    return new Response(JSON.stringify({
      code: 0,
      data: { success: 1, failed: 0, success_ids: [631], failed_ids: [] },
    }), { status: 200, headers: { "content-type": "application/json" } });
  };

  try {
    const client = new Sub2ApiAdminClient("http://sub2api:8080", "test-key");
    const result = await client.updateAccountRateMultiplier(631, 0.05);

    assert.equal(result.success, 1);
    assert.equal(requestUrl, "http://sub2api:8080/api/v1/admin/accounts/bulk-update");
    assert.equal(requestInit?.method, "POST");
    assert.deepEqual(JSON.parse(String(requestInit?.body)), {
      account_ids: [631],
      rate_multiplier: 0.05,
    });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("Sub2ApiAdminClient rejects HTTP 200 bulk account update business failures", async () => {
  const originalFetch = globalThis.fetch;
  const client = new Sub2ApiAdminClient("http://sub2api:8080", "test-key");
  const cases = [
    {
      name: "reported failure",
      data: { success: 0, failed: 1, success_ids: [], failed_ids: [631] },
      error: /failed=1.*failed_ids=631/,
    },
    {
      name: "failed ID with a zero failure count",
      data: { success: 1, failed: 0, success_ids: [631], failed_ids: [631] },
      error: /failed_ids=631/,
    },
    {
      name: "missing target success ID",
      data: { success: 1, failed: 0, success_ids: [999], failed_ids: [] },
      error: /missing success_ids=631/,
    },
    {
      name: "mismatched success count",
      data: { success: 0, failed: 0, success_ids: [631], failed_ids: [] },
      error: /success=0.*expected=1.*success_ids=1/,
    },
    {
      name: "malformed success IDs",
      data: { success: 1, failed: 0, success_ids: [631, "bad-id"], failed_ids: [] },
      error: /invalid success_ids/,
    },
    {
      name: "malformed failed IDs",
      data: { success: 1, failed: 0, success_ids: [631], failed_ids: "631" },
      error: /invalid failed_ids/,
    },
  ];

  try {
    for (const scenario of cases) {
      globalThis.fetch = async () => new Response(JSON.stringify({ code: 0, data: scenario.data }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });

      await assert.rejects(
        client.updateAccountRateMultiplier(631, 0.05),
        scenario.error,
        scenario.name,
      );
    }
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("syncGroupAccountRateMultipliers uses the field-level rate writer", async () => {
  const calls: Array<{ accountId: number; rateMultiplier: number }> = [];
	const client = {
		getAccount: async () => account(631, [17], 0.015),
		updateAccountRateMultiplier: async (accountId: number, rateMultiplier: number) => {
      calls.push({ accountId, rateMultiplier });
    },
  } as unknown as Sub2ApiAdminClient;

  const result = await syncGroupAccountRateMultipliers({
    client,
    groupId: 17,
    accounts: [account(631, [17], 0.015)],
    rateMultiplier: 0.05,
  });

  assert.equal(result.updated, 1);
  assert.deepEqual(calls, [{ accountId: 631, rateMultiplier: 0.05 }]);
});

test("isAccountEnabledForScheduling follows the permanent scheduling switch", () => {
  assert.equal(isAccountEnabledForScheduling(account(631, [17], 0.015)), true);
  assert.equal(isAccountEnabledForScheduling({ ...account(631, [17], 0.015), schedulable: false }), false);
  assert.equal(isAccountEnabledForScheduling({ ...account(631, [17], 0.015), status: "inactive" }), false);
  assert.equal(isAccountEnabledForScheduling({
    ...account(631, [17], 0.015),
    temp_unschedulable_until: Date.now() + 60_000,
  }), true);
});

test("syncGroupAccountRateMultipliers ignores accounts that are not enabled for scheduling", async () => {
  const calls: number[] = [];
  const disabled = { ...account(632, [17], 0.015), schedulable: false };
  const inactive = { ...account(633, [17], 0.015), status: "inactive" };

  const result = await syncGroupAccountRateMultipliers({
    client: {
      getAccount: async (accountId) => account(accountId, [17], 0.015),
      updateAccountRateMultiplier: async (accountId) => {
        calls.push(accountId);
      },
    },
    groupId: 17,
    accounts: [disabled, inactive],
    rateMultiplier: 0.05,
  });

  assert.deepEqual(calls, []);
  assert.equal(result.total, 0);
  assert.equal(result.updated, 0);
});

test("syncGroupAccountRateMultipliers keeps temporarily unavailable scheduled accounts as rate sources", async () => {
  const calls: number[] = [];
  const temporarilyUnavailable = {
    ...account(634, [17], 0.015),
    temp_unschedulable_until: Date.now() + 60_000,
  };

  const result = await syncGroupAccountRateMultipliers({
    client: {
      getAccount: async () => account(634, [17], 0.015),
      updateAccountRateMultiplier: async (accountId) => {
        calls.push(accountId);
      },
    },
    groupId: 17,
    accounts: [temporarilyUnavailable],
    rateMultiplier: 0.05,
  });

  assert.deepEqual(calls, [634]);
  assert.equal(result.updated, 1);
});

test("syncGroupAccountRateMultipliers preserves a manually locked zero rate", async () => {
	let reads = 0;
	let updates = 0;
	const result = await syncGroupAccountRateMultipliers({
		client: {
			getAccount: async () => {
				reads += 1;
				return account(631, [17], 0);
			},
			updateAccountRateMultiplier: async () => {
				updates += 1;
			},
		},
		groupId: 17,
		accounts: [account(631, [17], 0)],
		rateMultiplier: 0.05,
	});

	assert.equal(reads, 0);
	assert.equal(updates, 0);
	assert.equal(result.skipped, 1);
	assert.match(result.accounts[0]?.reason ?? "", /manually locked at zero/);
});

test("syncGroupAccountRateMultipliers rechecks zero before writing a stale account", async () => {
	let updates = 0;
	const result = await syncGroupAccountRateMultipliers({
		client: {
			getAccount: async () => account(631, [17], 0),
			updateAccountRateMultiplier: async () => {
				updates += 1;
			},
		},
		groupId: 17,
		accounts: [account(631, [17], 1)],
		rateMultiplier: 0.05,
	});

	assert.equal(updates, 0);
	assert.equal(result.skipped, 1);
  assert.match(result.accounts[0]?.reason ?? "", /manually locked at zero/);
});

test("syncGroupAccountRateMultipliers fails closed on a malformed account detail", async () => {
  const scenarios: Array<{ name: string; detail: unknown }> = [
    { name: "missing", detail: { id: 631, group_ids: [17] } },
    { name: "null", detail: { id: 631, group_ids: [17], rate_multiplier: null } },
    { name: "NaN", detail: { id: 631, group_ids: [17], rate_multiplier: Number.NaN } },
  ];

  for (const scenario of scenarios) {
    let updates = 0;
    const result = await syncGroupAccountRateMultipliers({
      client: {
        getAccount: async () => scenario.detail,
        updateAccountRateMultiplier: async () => {
          updates += 1;
        },
      },
      groupId: 17,
      accounts: [account(631, [17], 1)],
      rateMultiplier: 0.05,
    });

    assert.equal(updates, 0, scenario.name);
    assert.equal(result.updated, 0, scenario.name);
    assert.equal(result.failed, 1, scenario.name);
    assert.match(result.accounts[0]?.error ?? "", /missing rate_multiplier|invalid value/, scenario.name);
  }
});

test("syncGroupAccountRateMultipliers skips conflicting memberships", async () => {
  let updates = 0;
	const client = {
		getAccount: async () => account(631, [17, 85], 0.015),
		updateAccountRateMultiplier: async () => {
      updates += 1;
    },
  } as unknown as Sub2ApiAdminClient;
  const conflictReasons = groupAccountRateConflictReasons({
    accounts: [account(631, [17, 85], 0.015)],
    enabledGroupIds: new Set([17, 85]),
  });

  const result = await syncGroupAccountRateMultipliers({
    client,
    groupId: 17,
    accounts: [account(631, [17, 85], 0.015)],
    rateMultiplier: 0.05,
    skipAccountReasons: conflictReasons,
  });

  assert.equal(updates, 0);
  assert.equal(result.skipped, 1);
  assert.match(result.accounts[0]?.reason ?? "", /multiple enabled group rate rules/);
});

test("dedicated source binding takes precedence over a group conflict", async () => {
  const target = account(631, [17, 85], 0.015);
  const conflictReasons = groupAccountRateConflictReasons({
    accounts: [target],
    enabledGroupIds: new Set([17, 85]),
  });
	const client = {
		getAccount: async () => target,
		updateAccountRateMultiplier: async () => assert.fail("dedicated account must not be updated by a group rule"),
  } as unknown as Sub2ApiAdminClient;

  const result = await syncGroupAccountRateMultipliers({
    client,
    groupId: 17,
    accounts: [target],
    rateMultiplier: 0.05,
    skipAccountIds: new Set([631]),
    skipAccountReasons: conflictReasons,
  });

  assert.equal(result.skipped, 1);
  assert.match(result.accounts[0]?.reason ?? "", /dedicated upstream source binding/);
});

test("applyBoundRateRules treats an explicit empty target set as a no-op", async () => {
  const harness = groupRuleHarness({ enabledAccountRateGroupIds: [17] });
  const result = await applyBoundRateRules({
    db: harness.db as never,
    connectionId: 1,
    rateClient: harness.rateClient as never,
    s2Client: harness.s2Client as never,
    groups: [],
    accounts: [],
    targetGroupIds: [],
    includeAccountRules: false,
    includePriorityRules: false,
  });

  assert.equal(harness.getGroupBindingQueries(), 0);
  assert.equal(result.summary.appliedGroupRules, 0);
  assert.equal(result.summary.skippedGroupRules, 0);
  assert.equal(result.summary.failedGroupRules, 0);
});

test("partial group sync skips cross-group conflicts and records the reason", async () => {
  const logDir = await createLogDir("rate-sync-conflict-");
  process.env.S2A_LOG_DIR = logDir;
  const harness = groupRuleHarness({
    enabledAccountRateGroupIds: [17, 85],
    updateAccountRateMultiplier: async () => assert.fail("conflicting account must be skipped"),
  });

  try {
    const result = await applyBoundRateRules({
      db: harness.db as never,
      connectionId: 1,
      rateClient: harness.rateClient as never,
      s2Client: harness.s2Client as never,
      groups: [{ id: 17, name: "target", rate_multiplier: harness.currentGroupRate }] as never,
      accounts: [account(631, [17, 85], 0.015)],
      targetGroupIds: [17],
      includeAccountRules: false,
      includePriorityRules: false,
    });

    assert.equal(result.summary.skippedGroupRules, 1);
    assert.equal(harness.getEnabledModes(), "manual_source");
    const files = (await readdir(logDir)).filter((file) => file.startsWith("s2a-manager-"));
    assert.equal(files.length, 1);
    const log = await readFile(path.join(logDir, files[0] ?? ""), "utf8");
    assert.match(log, /multiple enabled group rate rules/);
    assert.match(log, /17,85/);
  } finally {
    await rm(logDir, { recursive: true, force: true });
  }
});

test("group rules preserve unbound manual account rates", async () => {
  const logDir = await createLogDir("rate-sync-unbound-manual-");
  process.env.S2A_LOG_DIR = logDir;
  let updates = 0;
  const harness = groupRuleHarness({
    enabledAccountRateGroupIds: [17],
    updateAccountRateMultiplier: async () => {
      updates += 1;
    },
  });

  try {
    const result = await applyBoundRateRules({
      db: harness.db as never,
      connectionId: 1,
      rateClient: harness.rateClient as never,
      s2Client: harness.s2Client as never,
      groups: [{ id: 17, name: "target", rate_multiplier: harness.currentGroupRate }] as never,
      accounts: [account(631, [17], 0.015)],
      includeAccountRules: false,
      includePriorityRules: false,
    });

    assert.equal(updates, 0);
    assert.equal(result.summary.skippedGroupRules, 1);
    assert.equal(result.summary.failedGroupRules, 0);
    const files = (await readdir(logDir)).filter((file) => file.startsWith("s2a-manager-"));
    const log = await readFile(path.join(logDir, files[0] ?? ""), "utf8");
    assert.match(log, /preserving its manual rate/);
  } finally {
    await rm(logDir, { recursive: true, force: true });
  }
});

test("bound account sync fails closed when the latest account read fails", async () => {
  const logDir = await createLogDir("rate-sync-account-read-failure-");
  process.env.S2A_LOG_DIR = logDir;
  let updates = 0;
  const binding = {
    id: 1,
    connectionId: 1,
    targetType: "account",
    targetId: 631,
    sourceSiteId: 1,
    sourceSiteName: "source",
    sourceGroupId: "33",
    sourceGroupName: "source-group",
    sourcePlatform: "openai",
  };
  const accountRule = {
    id: 1,
    connectionId: 1,
    accountId: 631,
    enabled: true,
    mode: "manual_source",
    offset: 0.05,
    expression: null,
  };
  const db = {
    blSourceBinding: {
      findMany: async (args: { where?: { targetType?: string } }) => (
        args.where?.targetType === "account" ? [binding] : []
      ),
    },
    blGroupRateRule: { findMany: async () => [] },
    blAccountRateRule: { findMany: async () => [accountRule] },
    upstreamMonitorRateExclusion: { findMany: async () => [] },
    upstreamMonitorRule: { findMany: async () => [] },
  };
  const s2Client = {
    getAccount: async () => {
      throw new Error("synthetic latest-account read failure");
    },
    updateAccountRateMultiplier: async () => {
      updates += 1;
    },
  };

  try {
    const result = await applyBoundRateRules({
      db: db as never,
      connectionId: 1,
      rateClient: { fetchRates: async () => [] } as never,
      s2Client: s2Client as never,
      groups: [],
      accounts: [account(631, [17], 1)],
      targetGroupIds: [],
      includePriorityRules: false,
    });

    assert.equal(updates, 0);
    assert.equal(result.summary.appliedAccountRules, 0);
    assert.equal(result.summary.failedAccountRules, 1);
    const files = (await readdir(logDir)).filter((file) => file.startsWith("s2a-manager-"));
    const log = await readFile(path.join(logDir, files[0] ?? ""), "utf8");
    assert.match(log, /synthetic latest-account read failure/);
  } finally {
    await rm(logDir, { recursive: true, force: true });
  }
});

test("bound account sync fails closed on a malformed account detail", async () => {
  const logDir = await createLogDir("rate-sync-account-detail-failure-");
  process.env.S2A_LOG_DIR = logDir;
  const binding = {
    id: 1,
    connectionId: 1,
    targetType: "account",
    targetId: 631,
    sourceSiteId: 1,
    sourceSiteName: "source",
    sourceGroupId: "33",
    sourceGroupName: "source-group",
    sourcePlatform: "openai",
  };
  const accountRule = {
    id: 1,
    connectionId: 1,
    accountId: 631,
    enabled: true,
    mode: "manual_source",
    offset: 0.05,
    expression: null,
  };
  const db = {
    blSourceBinding: {
      findMany: async (args: { where?: { targetType?: string } }) => (
        args.where?.targetType === "account" ? [binding] : []
      ),
    },
    blGroupRateRule: { findMany: async () => [] },
    blAccountRateRule: { findMany: async () => [accountRule] },
    upstreamMonitorRateExclusion: { findMany: async () => [] },
    upstreamMonitorRule: { findMany: async () => [] },
  };
  const scenarios: Array<{ name: string; detail: unknown }> = [
    { name: "missing", detail: { id: 631, group_ids: [17] } },
    { name: "null", detail: { id: 631, group_ids: [17], rate_multiplier: null } },
    { name: "NaN", detail: { id: 631, group_ids: [17], rate_multiplier: Number.NaN } },
  ];

  try {
    for (const scenario of scenarios) {
      let updates = 0;
      const result = await applyBoundRateRules({
        db: db as never,
        connectionId: 1,
        rateClient: { fetchRates: async () => [] } as never,
        s2Client: {
          getAccount: async () => scenario.detail,
          updateAccountRateMultiplier: async () => {
            updates += 1;
          },
        } as never,
        groups: [],
        accounts: [account(631, [17], 1)],
        targetGroupIds: [],
        includePriorityRules: false,
      });

      assert.equal(updates, 0, scenario.name);
      assert.equal(result.summary.appliedAccountRules, 0, scenario.name);
      assert.equal(result.summary.failedAccountRules, 1, scenario.name);
    }
    const files = (await readdir(logDir)).filter((file) => file.startsWith("s2a-manager-"));
    const log = await readFile(path.join(logDir, files[0] ?? ""), "utf8");
    assert.match(log, /missing rate_multiplier|invalid value/);
  } finally {
    await rm(logDir, { recursive: true, force: true });
  }
});

test("bound account sync does not record a failed field-level update as applied", async () => {
  const logDir = await createLogDir("rate-sync-account-update-failure-");
  process.env.S2A_LOG_DIR = logDir;
  let updates = 0;
  const binding = {
    id: 1,
    connectionId: 1,
    targetType: "account",
    targetId: 631,
    sourceSiteId: 1,
    sourceSiteName: "source",
    sourceGroupId: "33",
    sourceGroupName: "source-group",
    sourcePlatform: "openai",
  };
  const accountRule = {
    id: 1,
    connectionId: 1,
    accountId: 631,
    enabled: true,
    mode: "manual_source",
    offset: 0.05,
    expression: null,
  };
  const db = {
    blSourceBinding: {
      findMany: async (args: { where?: { targetType?: string } }) => (
        args.where?.targetType === "account" ? [binding] : []
      ),
    },
    blGroupRateRule: { findMany: async () => [] },
    blAccountRateRule: { findMany: async () => [accountRule] },
    upstreamMonitorRateExclusion: { findMany: async () => [] },
    upstreamMonitorRule: { findMany: async () => [] },
  };
  const s2Client = {
    getAccount: async () => account(631, [17], 1),
    updateAccountRateMultiplier: async () => {
      updates += 1;
      throw new Error("synthetic bulk update business failure");
    },
  };

  try {
    const result = await applyBoundRateRules({
      db: db as never,
      connectionId: 1,
      rateClient: { fetchRates: async () => [] } as never,
      s2Client: s2Client as never,
      groups: [],
      accounts: [account(631, [17], 1)],
      targetGroupIds: [],
      includePriorityRules: false,
    });

    assert.equal(updates, 1);
    assert.equal(result.summary.appliedAccountRules, 0);
    assert.equal(result.summary.failedAccountRules, 1);
    const files = (await readdir(logDir)).filter((file) => file.startsWith("s2a-manager-"));
    const log = await readFile(path.join(logDir, files[0] ?? ""), "utf8");
    assert.match(log, /synthetic bulk update business failure/);
  } finally {
    await rm(logDir, { recursive: true, force: true });
  }
});
