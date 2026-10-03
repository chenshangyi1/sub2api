import crypto from "node:crypto";
import type { PrismaClient } from "@prisma/client";
import { appSecret } from "@/server/env";

const LEGACY_IMPORT_VERSION = "20260729_upstream_ops_v007";
const LEGACY_CONNECTION_TABLE = "connections";
const CANONICAL_TARGET_TABLE = "upstream_sync_targets";
const GCM_NONCE_BYTES = 12;
const GCM_TAG_BYTES = 16;

type CanonicalTargetDb = Pick<PrismaClient, "legacyImportMap" | "upstreamSyncTarget">;

export type CanonicalTarget = {
  connectionId: number;
  targetId: number;
  name: string;
  baseUrl: string;
  adminApiKey: string;
};

export type CanonicalTargetResolutionCode =
  | "mapping_missing"
  | "mapping_invalid"
  | "target_missing"
  | "target_disabled"
  | "credential_invalid";

export class CanonicalTargetResolutionError extends Error {
  constructor(
    public readonly code: CanonicalTargetResolutionCode,
    public readonly connectionId: number,
    message: string,
  ) {
    super(message);
    this.name = "CanonicalTargetResolutionError";
  }
}

export function isCanonicalTargetDisabled(error: unknown) {
  return error instanceof CanonicalTargetResolutionError && error.code === "target_disabled";
}

export function decryptCanonicalTargetSecret(ciphertext: string) {
  const raw = Buffer.from(ciphertext, "base64");
  if (raw.length < GCM_NONCE_BYTES + GCM_TAG_BYTES) {
    throw new Error("canonical target ciphertext is too short");
  }

  const nonce = raw.subarray(0, GCM_NONCE_BYTES);
  const encrypted = raw.subarray(GCM_NONCE_BYTES, raw.length - GCM_TAG_BYTES);
  const tag = raw.subarray(raw.length - GCM_TAG_BYTES);
  const key = crypto.createHash("sha256").update(appSecret(), "utf8").digest();
  const decipher = crypto.createDecipheriv("aes-256-gcm", key, nonce);
  decipher.setAuthTag(tag);
  return Buffer.concat([decipher.update(encrypted), decipher.final()]).toString("utf8");
}

function positiveInteger(value: string) {
  if (!/^\d+$/.test(value)) return null;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
}

export async function listMappedConnectionIds(db: CanonicalTargetDb) {
  const mappings = await db.legacyImportMap.findMany({
    where: {
      migrationVersion: LEGACY_IMPORT_VERSION,
      legacyTable: LEGACY_CONNECTION_TABLE,
      canonicalTable: CANONICAL_TARGET_TABLE,
      rolledBackAt: null,
    },
    select: { legacyId: true },
  });
  const connectionIds = mappings
    .map((mapping) => positiveInteger(mapping.legacyId))
    .filter((value): value is number => value !== null);
  const targets = await db.upstreamSyncTarget.findMany({
    select: { id: true },
  });
  for (const target of targets) {
    const targetId = Number(target.id);
    if (Number.isSafeInteger(targetId) && targetId > 0) connectionIds.push(targetId);
  }
  return [...new Set(connectionIds)].sort((left, right) => left - right);
}

export async function resolveCanonicalTarget(
  db: CanonicalTargetDb,
  connectionId: number,
): Promise<CanonicalTarget> {
  if (!Number.isSafeInteger(connectionId) || connectionId <= 0) {
    throw new CanonicalTargetResolutionError("mapping_invalid", connectionId, "legacy connection id is invalid");
  }

  const mapping = await db.legacyImportMap.findFirst({
    where: {
      migrationVersion: LEGACY_IMPORT_VERSION,
      legacyTable: LEGACY_CONNECTION_TABLE,
      legacyId: String(connectionId),
      canonicalTable: CANONICAL_TARGET_TABLE,
      rolledBackAt: null,
    },
    select: { canonicalId: true },
  });
  const targetId = positiveInteger(mapping?.canonicalId ?? String(connectionId));
  if (!targetId) {
    throw new CanonicalTargetResolutionError(
      "mapping_invalid",
      connectionId,
      `canonical target mapping is invalid for legacy connection ${connectionId}`,
    );
  }

  const target = await db.upstreamSyncTarget.findUnique({
    where: { id: BigInt(targetId) },
    select: {
      id: true,
      name: true,
      baseUrl: true,
      adminApiKeyCipher: true,
      enabled: true,
    },
  });
  if (!target) {
    throw new CanonicalTargetResolutionError(
      mapping ? "target_missing" : "mapping_missing",
      connectionId,
      mapping
        ? `canonical target ${targetId} is missing for legacy connection ${connectionId}`
        : `canonical target mapping is missing and target ${targetId} does not exist`,
    );
  }
  if (!target.enabled) {
    throw new CanonicalTargetResolutionError(
      "target_disabled",
      connectionId,
      `canonical target ${targetId} is disabled`,
    );
  }

  let adminApiKey: string;
  try {
    adminApiKey = decryptCanonicalTargetSecret(target.adminApiKeyCipher);
  } catch (error) {
    throw new CanonicalTargetResolutionError(
      "credential_invalid",
      connectionId,
      `canonical target ${targetId} administrator key is invalid: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
  if (!adminApiKey.trim()) {
    throw new CanonicalTargetResolutionError(
      "credential_invalid",
      connectionId,
      `canonical target ${targetId} administrator key is empty`,
    );
  }

  return {
    connectionId,
    targetId,
    name: target.name,
    baseUrl: target.baseUrl.trim().replace(/\/+$/, ""),
    adminApiKey,
  };
}
