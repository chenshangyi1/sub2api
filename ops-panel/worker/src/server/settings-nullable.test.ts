import assert from "node:assert/strict";
import test from "node:test";

import { PrismaClient } from "@prisma/client";

const testDsn = process.env.TEST_POSTGRES_DSN?.trim();

test("settings with a legacy null updated_at remain readable", { skip: !testDsn }, async () => {
  const schema = `worker_settings_${process.pid}_${Date.now()}`;
  const admin = new PrismaClient({ datasources: { db: { url: testDsn } } });
  const url = new URL(testDsn!);
  url.searchParams.set("schema", schema);
  const client = new PrismaClient({ datasources: { db: { url: url.toString() } } });

  try {
    await admin.$executeRawUnsafe(`CREATE SCHEMA "${schema}"`);
    await client.$executeRawUnsafe(
      `CREATE TABLE settings (key text PRIMARY KEY, value text, updated_at timestamptz NULL)`,
    );
    await client.$executeRawUnsafe(
      `INSERT INTO settings (key, value, updated_at) VALUES ('worker_interval_seconds', '300', NULL)`,
    );

    const rows = await client.setting.findMany();
    assert.equal(rows.length, 1);
    assert.equal(rows[0]?.key, "worker_interval_seconds");
    assert.equal(rows[0]?.value, "300");
    assert.equal(rows[0]?.updatedAt, null);
  } finally {
    await client.$disconnect();
    await admin.$executeRawUnsafe(`DROP SCHEMA IF EXISTS "${schema}" CASCADE`);
    await admin.$disconnect();
  }
});
