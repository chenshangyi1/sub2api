import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import path from "node:path";
import test from "node:test";
import {
  updateAccountRateMultiplierField,
  writeAccountRateMultiplierPreservingManualZero,
} from "@/server/account-rate-writer";
import { groupAccountRateSkipPolicy } from "@/server/group-account-rate-sync";
import { applyBoundRateRules } from "@/server/bl-rate-sync";
import { evaluateGroupRateRule } from "@/server/rate-rule-evaluator";
import {
  isCanonicalRateFresh,
  resolveCanonicalEffectiveRate,
  type CanonicalRate,
} from "@/server/clients/canonical-rate";
import { gateRateRuleSources } from "@/shared/rate-rule-source-gate";
import { runWithConcurrency } from "@/server/upstream-monitor";

const require = createRequire(import.meta.url);
const { load: loadYaml } = require("js-yaml") as { load(source: string): unknown };

function source(overrides: Record<string, unknown> = {}) {
  return {
    currentRate: 0.8,
    fresh: true,
    siteEnabled: true,
    lastStatus: "online",
    monitorExcluded: false,
    ...overrides,
  };
}

function account(id: number, groupIds: number[], rateMultiplier: number) {
  return { id, name: `account-${id}`, group_ids: groupIds, rate_multiplier: rateMultiplier };
}

test("multi-source pricing supports explicit max, average, min, and first strategies", () => {
  const sourceRates = [0.15, 0.05, 0.1];
  const evaluate = (mode: "max" | "average" | "min" | "first", offset = 0) => evaluateGroupRateRule({
    rule: { enabled: true, mode, offset },
    sourceRates,
  });

  assert.equal(evaluate("max", 0.03), 0.18);
  assert.equal(evaluate("average", 0.01), 0.11);
  assert.equal(evaluate("min"), 0.05);
  assert.equal(evaluate("first"), 0.15);
});

test("retired profit mode fails closed instead of silently selecting a source", () => {
  assert.throws(() => evaluateGroupRateRule({
    rule: { enabled: true, mode: "profit" as never, offset: 0 },
    sourceRates: [0.15, 0.05],
  }), /不支持的倍率计算模式/);
});

test("custom expressions retain the legacy profit helper after profit mode retirement", () => {
  assert.equal(evaluateGroupRateRule({
    rule: { enabled: true, mode: "custom", offset: 0, expression: "profit(max())" },
    sourceRates: [0.05, 0.15],
  }), 0.18);
});

test("max uses healthy sources while skipping unavailable bindings", () => {
  const unavailable = [
    source({ siteEnabled: false }),
    source({ siteEnabled: null }),
    source({ siteEnabled: undefined }),
    source({ lastStatus: "offline" }),
    source({ lastStatus: null }),
    source({ lastStatus: undefined }),
    source({ fresh: false }),
    source({ monitorExcluded: true }),
    source({ currentRate: null }),
    source({ currentRate: Number.NaN }),
    source({ currentRate: 0 }),
  ];

  for (const badSource of unavailable) {
    const result = gateRateRuleSources("max", [source(), badSource]);
    assert.equal(result.ok, true, JSON.stringify(badSource));
    assert.deepEqual(result.sources, [source()]);
    assert.equal(result.skippedSources.length, 1);
    assert.match(result.notice ?? "", /自动跳过 1 个异常源/);
  }
});

test("source-based rules stop only when every bound source is unusable", () => {
  const result = gateRateRuleSources("max", [
    source({ lastStatus: "offline" }),
    source({ currentRate: null }),
  ]);

  assert.equal(result.ok, false);
  assert.equal(result.sources.length, 0);
  assert.equal(result.skippedSources.length, 2);
  assert.match(result.reason ?? "", /没有可用倍率/);
});

test("all callers share subset behavior for non-strict modes and bypass source reads for manual modes", () => {
  const good = source();
  const excluded = source({ currentRate: 0.9, monitorExcluded: true });
  const average = gateRateRuleSources("average", [good, excluded]);
  const locked = gateRateRuleSources("locked", [excluded]);
  const manual = gateRateRuleSources("manual_source", []);

  assert.equal(average.ok, true);
  assert.deepEqual(average.sources, [good]);
  assert.equal(locked.ok, true);
  assert.equal(manual.ok, true);
});

test("a source-free locked group rule still applies and owns the target group", async () => {
  let updatedRate: number | null = null;
  const result = await applyBoundRateRules({
    db: {
      blSourceBinding: { findMany: async () => [] },
      blGroupRateRule: {
        findMany: async () => [{
          id: 1,
          connectionId: 1,
          groupId: 17,
          enabled: true,
          mode: "locked",
          offset: 0.085,
          expression: null,
        }],
      },
      upstreamMonitorRateExclusion: { findMany: async () => [] },
      upstreamMonitorRule: { findMany: async () => [] },
      announcementRule: { findMany: async () => [] },
    } as never,
    connectionId: 1,
    rateClient: { fetchRates: async () => [] } as never,
    s2Client: {
      getGroup: async () => ({ id: 17, name: "target", rate_multiplier: 0.1 }),
      updateGroupRateMultiplier: async (_groupId: number, rate: number) => {
        updatedRate = rate;
      },
    } as never,
    groups: [{ id: 17, name: "target", rate_multiplier: 0.1 }] as never,
    includeAccountRules: false,
    includePriorityRules: false,
  });

  assert.equal(updatedRate, 0.085);
  assert.equal(result.summary.appliedGroupRules, 1);
  assert.deepEqual([...result.boundGroupIds], [17]);
  assert.equal(result.boundSourceKeys.size, 0);
});

test("effective user rate and source freshness use the collected site state", () => {
  const now = Date.parse("2026-07-15T00:20:00.000Z");
  const rate = {
    site_id: 1,
    site_name: "source",
    site_enabled: true,
    site_interval_min: 10,
    last_status: "online",
    last_success_at: "2026-07-15T00:10:00.000Z",
    collected_at: "2026-07-15T00:10:00.000Z",
    group_id: "44",
    name: "group",
    platform: "openai",
    remote_group_id: "44",
    recharge_ratio: 2,
    rate_multiplier: 0.8,
    user_rate: 0.6,
    effective_rate: 0.6,
    actual_rate_multiplier: 0.4,
    actual_user_rate: 0.3,
    actual_effective_rate: 0.3,
  } satisfies CanonicalRate;

  assert.equal(resolveCanonicalEffectiveRate(rate), 0.3);
  assert.equal(isCanonicalRateFresh(rate, now), true);
  assert.equal(isCanonicalRateFresh({ ...rate, last_status: "offline" }, now), false);
  assert.equal(isCanonicalRateFresh({ ...rate, last_success_at: "2026-07-14T23:00:00.000Z" }, now), false);
});

test("automatic rule scan exposes explicit source keys even for cross-ID target bindings", async () => {
  const result = await applyBoundRateRules({
    db: {
      blSourceBinding: {
        findMany: async (args: { where?: { targetType?: string } }) => args.where?.targetType === "group"
          ? [{
              id: 1,
              connectionId: 1,
              targetType: "group",
              targetId: 78,
              sourceSiteId: 1,
              sourceSiteName: "source",
              sourceGroupId: "44",
              sourceGroupName: "same-name",
              sourcePlatform: "openai",
            }]
          : [],
      },
      blGroupRateRule: { findMany: async () => [] },
    } as never,
    connectionId: 1,
    rateClient: {} as never,
    s2Client: {} as never,
    groups: [{ id: 44, name: "same-name" }, { id: 78, name: "target" }] as never,
    accounts: [],
    includeAccountRules: false,
    includePriorityRules: false,
  });

  assert.deepEqual(Array.from(result.boundSourceKeys), ["1:44"]);
  assert.equal(result.boundGroupIds.has(78), true);
  assert.equal(result.boundGroupIds.has(44), false);
});


test("manual group account policy includes every enabled manual-source conflict", async () => {
  let ruleWhere: unknown;
  const policy = await groupAccountRateSkipPolicy({
    db: {
      blSourceBinding: {
        findMany: async () => [{ targetId: 632 }],
      },
      blGroupRateRule: {
        findMany: async (args: { where: unknown }) => {
          ruleWhere = args.where;
          return [{ groupId: 17 }, { groupId: 85 }];
        },
      },
    } as never,
    connectionId: 1,
    groupId: 17,
    accounts: [account(631, [17, 85], 0.05), account(632, [17], 0.05), account(633, [17], 0.015)],
  });

  assert.deepEqual(ruleWhere, {
    connectionId: 1,
    enabled: true,
    mode: "manual_source",
  });
  assert.match(policy.skipAccountReasons.get(631) ?? "", /17,85$/);
  assert.equal(policy.skipAccountIds.has(632), true);
  assert.match(policy.skipAccountReasons.get(633) ?? "", /preserving its manual rate/);
});

test("manual account writer preserves initial and TOCTOU zero locks", async () => {
  let reads = 0;
  let updates = 0;
  const client = {
    getAccount: async () => {
      reads += 1;
      return account(631, [17], 0);
    },
    updateAccountRateMultiplier: async () => {
      updates += 1;
      return { success: 1, failed: 0 };
    },
  };

  const initialZero = await writeAccountRateMultiplierPreservingManualZero({
    client: client as never,
    accountId: 631,
    rateMultiplier: 0.7,
    initialAccount: account(631, [17], 0),
  });
  assert.equal(initialZero.status, "skipped");
  assert.equal(reads, 0);

  const changedDuringWrite = await writeAccountRateMultiplierPreservingManualZero({
    client: client as never,
    accountId: 631,
    rateMultiplier: 0.7,
    initialAccount: account(631, [17], 1),
  });
  assert.equal(changedDuringWrite.status, "skipped");
  assert.equal(reads, 1);
  assert.equal(updates, 0);
});

test("manual account field writer never calls the full account update endpoint", async () => {
  const calls: Array<{ accountId: number; rateMultiplier: number }> = [];
  const result = await updateAccountRateMultiplierField({
    client: {
      updateAccountRateMultiplier: async (accountId, rateMultiplier) => {
        calls.push({ accountId, rateMultiplier });
        return { success: 1, failed: 0 } as never;
      },
    },
    accountId: 631,
    rateMultiplier: 0.7,
  });

  assert.deepEqual(calls, [{ accountId: 631, rateMultiplier: 0.7 }]);
  assert.deepEqual(result, { success: 1, failed: 0 });
});

test("cooperative concurrency stops taking queue items after a stop signal", async () => {
  const started: number[] = [];
  let stopping = false;
  await runWithConcurrency([1, 2, 3], 2, async (item) => {
    started.push(item);
    stopping = true;
  }, () => stopping);

  assert.deepEqual(started, [1]);
});

test("ops compose overlay inherits production services and persists candidate runtime data", async () => {
  const source = await readFile(path.join(process.cwd(), "../operations/docker-compose.ops.yml"), "utf8");
  const document = loadYaml(source) as {
    services?: Record<string, Record<string, unknown>>;
    volumes?: unknown;
    networks?: unknown;
    name?: unknown;
  };
  const services = document.services ?? {};
  const panel = services["ops-panel"] ?? {};
  const worker = services["ops-worker"] ?? {};
  const volumes = document.volumes as Record<string, unknown> | undefined;
  const serialized = JSON.stringify(document);

  assert.deepEqual(Object.keys(services).sort(), ["ops-panel", "ops-worker"]);
  for (const service of [panel, worker]) {
    assert.equal("build" in service, false);
    assert.equal("networks" in service, false);
    assert.equal("env_file" in service, false);
  }
  assert.deepEqual(panel.volumes, ["ops_panel_data:/app/data"]);
  assert.deepEqual(worker.volumes, ["ops_panel_data:/app/data"]);
  assert.deepEqual(volumes, { ops_panel_data: null });
  assert.equal(document.networks, undefined);
  assert.equal(document.name, undefined);
  assert.doesNotMatch(serialized, /DATABASE_URL|POSTGRES|OPS_DB_PASSWORD|s2aOps|s2Api2024/);
  assert.match(source, /Build and tag the immutable candidate image separately first/);
  assert.equal(source.includes("# docker compose -f /opt/sub2api/docker-compose.yml \\"), true);
  assert.equal(source.includes("#   -f /opt/sub2api/docker-compose.ops.yml \\"), true);
  assert.equal(source.includes("#   -f /opt/sub2api/ops-panel-s2a/operations/docker-compose.ops.yml \\"), true);
  assert.equal(source.includes("#   up -d --no-build ops-panel ops-worker"), true);
  assert.equal(panel.image, worker.image);
  assert.match(
    String(panel.image),
    /^local\/sub2api-ops-panel:upstream-v0\.0\.7-integration-\d{8}T\d{6}Z-r\d+$/,
  );
  assert.deepEqual(panel.command, ["panel", "-config", "/app/data/config.yaml"]);
  assert.deepEqual(worker.command, ["worker"]);
  assert.equal(panel.restart, "unless-stopped");
  assert.equal(worker.restart, "unless-stopped");
  assert.equal((worker.depends_on as { "ops-panel"?: { condition?: string } })?.["ops-panel"]?.condition, "service_healthy");
  assert.deepEqual(worker.environment, {
    S2A_LOG_DIR: "/app/data/worker-logs",
    S2A_WORKER_INTERVAL_SECONDS: "60",
    S2A_WORKER_RETRY_INTERVAL_SECONDS: "60",
  });
});
