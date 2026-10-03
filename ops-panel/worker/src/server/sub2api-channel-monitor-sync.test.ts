import assert from "node:assert/strict";
import test from "node:test";
import type { Sub2ApiAdminClient, Sub2ApiChannelMonitorCreate } from "@/server/clients/sub2api-admin";
import {
  deleteOrphanedSub2ApiChannelMonitors,
  deleteSub2ApiChannelMonitor,
  deleteStaleSub2ApiChannelMonitors,
  findOwnedChannelMonitor,
  selectAccountMonitorGroup,
  syncRuleToSub2ApiChannelMonitor,
} from "@/server/sub2api-channel-monitor-sync";

function syncClient(overrides: Partial<Sub2ApiAdminClient> = {}) {
  return {
    exportAccountsData: async () => ({
      accounts: [{
        id: 17,
        platform: "openai",
        credentials: {
          api_key: "test-key",
          base_url: "https://api.example.test/v1",
        },
      }],
    }),
    getAvailableModels: async () => [{ id: "gpt-test" }],
    createChannelMonitor: async () => ({ id: 101 }),
    updateChannelMonitor: async (id: number) => ({ id }),
    ...overrides,
  } as unknown as Sub2ApiAdminClient;
}

test("native monitor payload identifies its Sub2API account and staggers checks", async () => {
  const submitted: { payload?: Sub2ApiChannelMonitorCreate } = {};
  const client = syncClient({
    createChannelMonitor: async (data) => {
      submitted.payload = data;
      return { id: 101 } as never;
    },
  });

  await syncRuleToSub2ApiChannelMonitor({
    client,
    connectionId: 1,
    accountId: 17,
    account: { id: 17, name: "stable", platform: "openai" },
    publicVisible: true,
    sub2apiGroupId: 108,
    sub2apiGroupName: "Production GPT",
    checkIntervalMinutes: 10,
    modelId: "gpt-test",
  });

  assert.equal(submitted.payload?.group_name, "Production GPT");
  assert.equal(submitted.payload?.enabled, false);
  assert.equal(submitted.payload?.external_ref, "ops:1:account:17");
  assert.equal(submitted.payload?.public_visible, true);
  assert.equal(submitted.payload?.management_mode, "external");
  assert.equal(submitted.payload?.jitter_seconds, 60);
  assert.equal(submitted.payload?.endpoint, "https://api.example.test");
});

test("native monitor sync shares low-cost automatic model selection and fallback order", async () => {
  const submitted: { payload?: Sub2ApiChannelMonitorCreate } = {};
  const client = syncClient({
    getAvailableModels: async () => [
      { id: "gpt-5.4" },
      { id: "gpt-5.4-mini" },
      { id: "gpt-5.4-nano" },
      { id: "gpt-image-1" },
    ],
    createChannelMonitor: async (data) => {
      submitted.payload = data;
      return { id: 101 } as never;
    },
  });

  const result = await syncRuleToSub2ApiChannelMonitor({
    client,
    connectionId: 1,
    accountId: 17,
    account: { id: 17, name: "stable", platform: "openai" },
    publicVisible: false,
    sub2apiGroupId: 108,
    sub2apiGroupName: "Production GPT",
    checkIntervalMinutes: 10,
    modelId: null,
  });

  assert.equal(submitted.payload?.primary_model, "gpt-5.4-nano");
  assert.deepEqual(submitted.payload?.extra_models, ["gpt-5.4-mini", "gpt-5.4"]);
  assert.deepEqual(result.modelCandidates, ["gpt-5.4-nano", "gpt-5.4-mini", "gpt-5.4"]);
});

test("owned monitor discovery scans pages and matches the external account reference", async () => {
  const pages: number[] = [];
  const firstPage = Array.from({ length: 100 }, (_, index) => ({
    id: index + 1,
    group_name: index === 99 ? " Sub2API #17 " : "manual",
  }));
  const client = syncClient({
    listChannelMonitorsPage: async ({ page = 1 } = {}) => {
      pages.push(page);
      const items = page === 1 ? firstPage : [{ id: 101, group_name: "Production GPT", external_ref: "ops:1:account:17" }];
      return { items, total: 101, page, pageSize: 100 } as never;
    },
  });

  const monitor = await findOwnedChannelMonitor({ client, connectionId: 1, accountId: 17 });

  assert.deepEqual(pages, [1, 2]);
  assert.equal(monitor?.id, 101);
});

test("native monitor recreation is limited to a missing remote monitor", async () => {
  let createCalls = 0;
  const client = syncClient({
    updateChannelMonitor: async () => {
      throw new Error("HTTP 404: monitor not found");
    },
    createChannelMonitor: async () => {
      createCalls += 1;
      return { id: 202 } as never;
    },
  });

  const result = await syncRuleToSub2ApiChannelMonitor({
    client,
    rule: { sub2apiChannelMonitorId: 101 },
    connectionId: 1,
    accountId: 17,
    account: { id: 17, name: "stable", platform: "openai" },
    publicVisible: true,
    sub2apiGroupId: 108,
    sub2apiGroupName: "Production GPT",
    checkIntervalMinutes: 10,
    modelId: "gpt-test",
  });

  assert.equal(createCalls, 1);
  assert.equal(result.monitorId, 202);
  assert.equal(result.created, true);
});

test("transient native monitor update failures do not create duplicates", async () => {
  let createCalls = 0;
  const client = syncClient({
    updateChannelMonitor: async () => {
      throw new Error("HTTP 500: upstream unavailable");
    },
    createChannelMonitor: async () => {
      createCalls += 1;
      return { id: 202 } as never;
    },
  });

  await assert.rejects(
    syncRuleToSub2ApiChannelMonitor({
      client,
      rule: { sub2apiChannelMonitorId: 101 },
      connectionId: 1,
      accountId: 17,
      account: { id: 17, name: "stable", platform: "openai" },
      publicVisible: true,
      sub2apiGroupId: 108,
      sub2apiGroupName: "Production GPT",
      checkIntervalMinutes: 10,
      modelId: "gpt-test",
    }),
    /HTTP 500/,
  );
  assert.equal(createCalls, 0);
});

test("group selection rejects a group that is not assigned to the account", () => {
  assert.throws(
    () => selectAccountMonitorGroup({ group_ids: [108] }, [{ id: 109, name: "Other" }], 109),
    /账号不属于 Sub2API 分组 #109/,
  );
  assert.deepEqual(
    selectAccountMonitorGroup({ group_ids: [108] }, [{ id: 108, name: "Production GPT" }], 108),
    { id: 108, name: "Production GPT" },
  );
});

test("stale rule cleanup preserves retry links when native deletion fails", async () => {
  const deleted: number[] = [];
  const client = syncClient({
    deleteChannelMonitor: async (monitorId) => {
      if (monitorId === 103) throw new Error("HTTP 503: retry later");
      deleted.push(monitorId);
      return {};
    },
  });

  const result = await deleteStaleSub2ApiChannelMonitors({
    client,
    rules: [
      { id: 1, accountId: 17, sub2apiChannelMonitorId: 101 },
      { id: 2, accountId: 18, sub2apiChannelMonitorId: null },
      { id: 3, accountId: 19, sub2apiChannelMonitorId: 103 },
    ],
  });

  assert.deepEqual(deleted, [101]);
  assert.deepEqual(result.deletableRuleIds, [1, 2]);
  assert.deepEqual(result.failedRuleIds, [3]);
  assert.equal(result.deletedNativeMonitors, 1);
  assert.match(result.errors[0] ?? "", /account 19.*monitor 103.*HTTP 503/);
});

test("single native monitor deletion is idempotent for an already missing monitor", async () => {
  const client = syncClient({
    deleteChannelMonitor: async () => {
      throw new Error("HTTP 404: monitor not found");
    },
  });

  const result = await deleteSub2ApiChannelMonitor({ client, monitorId: 101 });

  assert.deepEqual(result, { monitorId: 101, skipped: false, missing: true });
});

test("single native monitor deletion still surfaces transient failures", async () => {
  const client = syncClient({
    deleteChannelMonitor: async () => {
      throw new Error("HTTP 503: retry later");
    },
  });

  await assert.rejects(
    deleteSub2ApiChannelMonitor({ client, monitorId: 101 }),
    /HTTP 503/,
  );
});

test("owned orphan reconciliation deletes only unreferenced Sub2API monitors", async () => {
  const deleted: number[] = [];
  const client = syncClient({
    listChannelMonitors: async () => [
      { id: 101, group_name: "Sub2API #17" },
      { id: 102, group_name: "Sub2API #17" },
      { id: 103, group_name: "manual" },
      { id: 104, group_name: "Sub2API #19" },
    ] as never,
    deleteChannelMonitor: async (monitorId) => {
      deleted.push(monitorId);
      return {};
    },
  });

  const result = await deleteOrphanedSub2ApiChannelMonitors({
    client,
    rules: [
      { id: 1, accountId: 17, sub2apiChannelMonitorId: 101 },
      { id: 2, accountId: 18, sub2apiChannelMonitorId: null },
    ],
  });

  assert.deepEqual(deleted, [102, 104]);
  assert.equal(result.deletedNativeMonitors, 2);
  assert.deepEqual(result.deletedMonitorIds, [102, 104]);
  assert.deepEqual(result.errors, []);
});

test("owned orphan reconciliation preserves failed monitors for a later retry", async () => {
  const client = syncClient({
    listChannelMonitors: async () => [
      { id: 102, group_name: "Sub2API #17" },
      { id: 103, group_name: "manual" },
    ] as never,
    deleteChannelMonitor: async () => {
      throw new Error("HTTP 503: retry later");
    },
  });

  const result = await deleteOrphanedSub2ApiChannelMonitors({ client, rules: [] });

  assert.equal(result.deletedNativeMonitors, 0);
  assert.deepEqual(result.deletedMonitorIds, []);
  assert.match(result.errors[0] ?? "", /monitor 102.*HTTP 503/);
});

test("owned orphan reconciliation lists every page before deleting shifted rows", async () => {
  const events: string[] = [];
  const firstPage = Array.from({ length: 100 }, (_, index) => ({
    id: index + 1,
    group_name: index === 99 ? "Sub2API #17" : "manual",
  }));
  const client = syncClient({
    listChannelMonitors: async ({ page = 1 } = {}) => {
      events.push(`list:${page}`);
      return (page === 1 ? firstPage : [{ id: 101, group_name: "Sub2API #18" }]) as never;
    },
    deleteChannelMonitor: async (monitorId) => {
      events.push(`delete:${monitorId}`);
      return {};
    },
  });

  const result = await deleteOrphanedSub2ApiChannelMonitors({ client, rules: [] });

  assert.deepEqual(events, ["list:1", "list:2", "delete:100", "delete:101"]);
  assert.deepEqual(result.deletedMonitorIds, [100, 101]);
});

test("owned orphan reconciliation reports an incomplete scan at the page cap", async () => {
  let listCalls = 0;
  const client = syncClient({
    listChannelMonitors: async ({ page = 1 } = {}) => {
      listCalls += 1;
      return Array.from({ length: 100 }, (_, index) => ({
        id: ((page - 1) * 100) + index + 1,
        group_name: "manual",
      })) as never;
    },
  });

  const result = await deleteOrphanedSub2ApiChannelMonitors({ client, rules: [] });

  assert.equal(listCalls, 100);
  assert.match(result.errors[0] ?? "", /100 pages.*incomplete/i);
});

test("owned orphan reconciliation gives newly created monitors time to persist their rule", async () => {
  const deleted: number[] = [];
  const client = syncClient({
    listChannelMonitors: async () => [{
      id: 102,
      group_name: "Sub2API #17",
      created_at: new Date().toISOString(),
    }] as never,
    deleteChannelMonitor: async (monitorId) => {
      deleted.push(monitorId);
      return {};
    },
  });

  const result = await deleteOrphanedSub2ApiChannelMonitors({ client, rules: [] });

  assert.deepEqual(deleted, []);
  assert.deepEqual(result.deletedMonitorIds, []);
});

test("owned orphan reconciliation rechecks references immediately before deletion", async () => {
  const deleted: number[] = [];
  const client = syncClient({
    listChannelMonitors: async () => [{
      id: 102,
      group_name: "Sub2API #17",
      created_at: "2025-01-01T00:00:00.000Z",
    }] as never,
    deleteChannelMonitor: async (monitorId) => {
      deleted.push(monitorId);
      return {};
    },
  });

  const result = await deleteOrphanedSub2ApiChannelMonitors({
    client,
    rules: [],
    isReferenced: async (monitorId) => monitorId === 102,
  });

  assert.deepEqual(deleted, []);
  assert.deepEqual(result.deletedMonitorIds, []);
});

test("owned orphan reconciliation requires an exact ownership marker", async () => {
  const deleted: number[] = [];
  const client = syncClient({
    listChannelMonitors: async () => [{
      id: 102,
      group_name: " Sub2API #17 ",
      created_at: "2025-01-01T00:00:00.000Z",
    }] as never,
    deleteChannelMonitor: async (monitorId) => {
      deleted.push(monitorId);
      return {};
    },
  });

  await deleteOrphanedSub2ApiChannelMonitors({ client, rules: [] });

  assert.deepEqual(deleted, []);
});
