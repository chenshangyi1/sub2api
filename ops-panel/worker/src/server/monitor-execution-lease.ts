import { randomUUID } from "node:crypto";
import type { PrismaClient } from "@prisma/client";

type LeaseDb = Pick<PrismaClient, "setting">;

export type MonitorExecutionLease = {
  key: string;
  value: string;
  owner: string;
  expiresAt: Date;
};

const leaseKey = (ruleId: number) => `upstream-monitor-lease:${ruleId}`;

function isUniqueConstraintError(error: unknown) {
  return Boolean(error && typeof error === "object" && "code" in error && error.code === "P2002");
}

function leaseExpiry(value: string) {
  try {
    const parsed = JSON.parse(value) as { expiresAt?: unknown };
    const expiresAt = typeof parsed.expiresAt === "string" ? new Date(parsed.expiresAt) : null;
    return expiresAt && Number.isFinite(expiresAt.getTime()) ? expiresAt : null;
  } catch {
    return null;
  }
}

export async function acquireMonitorExecutionLease(input: {
  db: LeaseDb;
  ruleId: number;
  now?: Date;
  ttlMs?: number;
}) {
  const now = input.now ?? new Date();
  const ttlMs = Math.max(60_000, input.ttlMs ?? 8 * 60_000);
  const owner = randomUUID();
  const expiresAt = new Date(now.getTime() + ttlMs);
  const key = leaseKey(input.ruleId);
  const value = JSON.stringify({ owner, expiresAt: expiresAt.toISOString() });
  const lease = { key, value, owner, expiresAt } satisfies MonitorExecutionLease;

  try {
    await input.db.setting.create({ data: { key, value } });
    return lease;
  } catch (error) {
    if (!isUniqueConstraintError(error)) throw error;
  }

  const current = await input.db.setting.findUnique({ where: { key } });
  if (!current) return null;
  const currentExpiry = leaseExpiry(current.value);
  if (currentExpiry && currentExpiry.getTime() > now.getTime()) return null;

  const claimed = await input.db.setting.updateMany({
    where: { key, value: current.value },
    data: { value },
  });
  return claimed.count === 1 ? lease : null;
}

export async function releaseMonitorExecutionLease(db: LeaseDb, lease: MonitorExecutionLease) {
  await db.setting.deleteMany({ where: { key: lease.key, value: lease.value } });
}

export const monitorExecutionLeaseInternals = { leaseExpiry, leaseKey };
