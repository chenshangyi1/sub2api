import assert from "node:assert/strict";
import test from "node:test";
import { acquireMonitorExecutionLease, releaseMonitorExecutionLease } from "@/server/monitor-execution-lease";

function leaseDb() {
  let row: { key: string; value: string } | null = null;
  const db = {
    setting: {
      create: async ({ data }: { data: { key: string; value: string } }) => {
        if (row) throw { code: "P2002" };
        row = { ...data };
        return row;
      },
      findUnique: async ({ where }: { where: { key: string } }) => row?.key === where.key ? row : null,
      updateMany: async ({ where, data }: { where: { key: string; value: string }; data: { value: string } }) => {
        if (!row || row.key !== where.key || row.value !== where.value) return { count: 0 };
        row = { ...row, value: data.value };
        return { count: 1 };
      },
      deleteMany: async ({ where }: { where: { key: string; value: string } }) => {
        if (!row || row.key !== where.key || row.value !== where.value) return { count: 0 };
        row = null;
        return { count: 1 };
      },
    },
  };
  return { db: db as never, current: () => row };
}

test("monitor execution lease blocks a second container and releases by owner", async () => {
  const fixture = leaseDb();
  const first = await acquireMonitorExecutionLease({
    db: fixture.db,
    ruleId: 17,
    now: new Date("2026-07-26T08:00:00.000Z"),
  });
  const blocked = await acquireMonitorExecutionLease({
    db: fixture.db,
    ruleId: 17,
    now: new Date("2026-07-26T08:00:01.000Z"),
  });

  assert.ok(first);
  assert.equal(blocked, null);
  await releaseMonitorExecutionLease(fixture.db, first);
  assert.equal(fixture.current(), null);
});

test("expired monitor execution lease is atomically replaced", async () => {
  const fixture = leaseDb();
  const expired = await acquireMonitorExecutionLease({
    db: fixture.db,
    ruleId: 17,
    now: new Date("2026-07-26T08:00:00.000Z"),
    ttlMs: 60_000,
  });
  const replacement = await acquireMonitorExecutionLease({
    db: fixture.db,
    ruleId: 17,
    now: new Date("2026-07-26T08:01:01.000Z"),
  });

  assert.ok(expired);
  assert.ok(replacement);
  assert.notEqual(replacement.owner, expired.owner);
  await releaseMonitorExecutionLease(fixture.db, expired);
  assert.ok(fixture.current());
  await releaseMonitorExecutionLease(fixture.db, replacement);
  assert.equal(fixture.current(), null);
});
