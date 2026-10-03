import assert from "node:assert/strict";
import crypto from "node:crypto";
import test from "node:test";
import {
  CanonicalTargetResolutionError,
  resolveCanonicalTarget,
} from "./canonical-target";

const secret = "existing-secret";

function encryptCanonicalTargetSecret(plaintext: string, nonceByte: number) {
  const nonce = Buffer.alloc(12, nonceByte);
  const key = crypto.createHash("sha256").update(secret, "utf8").digest();
  const cipher = crypto.createCipheriv("aes-256-gcm", key, nonce);
  const encrypted = Buffer.concat([cipher.update(plaintext, "utf8"), cipher.final()]);
  return Buffer.concat([nonce, encrypted, cipher.getAuthTag()]).toString("base64");
}

function withAppSecret<T>(work: () => Promise<T>) {
  const previous = process.env.APP_SECRET;
  process.env.APP_SECRET = secret;
  return work().finally(() => {
    if (previous === undefined) delete process.env.APP_SECRET;
    else process.env.APP_SECRET = previous;
  });
}

test("canonical target resolution observes updated address and APP_SECRET credential", async () => withAppSecret(async () => {
  let target = {
    id: 17,
    name: "primary",
    baseUrl: "https://old.example.test/",
    adminApiKeyCipher: encryptCanonicalTargetSecret("old-key", 1),
    enabled: true,
  };
  const db = {
    legacyImportMap: {
      findFirst: async () => ({ canonicalId: "17" }),
    },
    upstreamSyncTarget: {
      findUnique: async () => target,
    },
  };

  const first = await resolveCanonicalTarget(db as never, 4);
  assert.equal(first.targetId, 17);
  assert.equal(first.baseUrl, "https://old.example.test");
  assert.equal(first.adminApiKey, "old-key");

  target = {
    ...target,
    baseUrl: "https://new.example.test/api/",
    adminApiKeyCipher: encryptCanonicalTargetSecret("new-key", 2),
  };
  const updated = await resolveCanonicalTarget(db as never, 4);
  assert.equal(updated.baseUrl, "https://new.example.test/api");
  assert.equal(updated.adminApiKey, "new-key");
}));

test("canonical target resolution rejects a disabled canonical target", async () => withAppSecret(async () => {
  const db = {
    legacyImportMap: { findFirst: async () => ({ canonicalId: "9" }) },
    upstreamSyncTarget: {
      findUnique: async () => ({
        id: 9,
        name: "disabled",
        baseUrl: "https://disabled.example.test",
        adminApiKeyCipher: "not-read-for-disabled-targets",
        enabled: false,
      }),
    },
  };

  await assert.rejects(
    resolveCanonicalTarget(db as never, 2),
    (error: unknown) => error instanceof CanonicalTargetResolutionError && error.code === "target_disabled",
  );
}));

test("canonical target resolution rejects a missing active legacy import mapping", async () => withAppSecret(async () => {
  let targetQueried = false;
  let mappingWhere: Record<string, unknown> | undefined;
  const db = {
    legacyImportMap: {
      findFirst: async ({ where }: { where: Record<string, unknown> }) => {
        mappingWhere = where;
        return null;
      },
    },
    upstreamSyncTarget: {
      findUnique: async () => {
        targetQueried = true;
        return null;
      },
    },
  };

  await assert.rejects(
    resolveCanonicalTarget(db as never, 23),
    (error: unknown) => error instanceof CanonicalTargetResolutionError && error.code === "mapping_missing",
  );
  assert.equal(targetQueried, true);
  assert.deepEqual(mappingWhere, {
    migrationVersion: "20260729_upstream_ops_v007",
    legacyTable: "connections",
    legacyId: "23",
    canonicalTable: "upstream_sync_targets",
    rolledBackAt: null,
  });
}));
