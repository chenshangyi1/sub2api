import type { PrismaClient } from "@prisma/client";

export const workerSettingKeys = [
  "worker_interval_seconds",
  "account_balance_alert_interval_seconds",
  "upstream_monitor_heavy_interval_minutes",
] as const;

export const defaultWorkerIntervalSeconds = 10 * 60;
export const defaultAccountBalanceAlertIntervalSeconds = 5 * 60;
export const defaultUpstreamMonitorHeavyIntervalMinutes = 60;

type SettingRow = { key: string; value: string };
type SettingsDb = Pick<PrismaClient, "setting">;

function finiteInteger(value: unknown) {
  const numeric = Number(value);
  return Number.isInteger(numeric) && Number.isFinite(numeric) ? numeric : null;
}

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}

export function normalizeWorkerIntervalSeconds(value: unknown, fallback: unknown = defaultWorkerIntervalSeconds) {
  return clamp(finiteInteger(value) ?? finiteInteger(fallback) ?? defaultWorkerIntervalSeconds, 60, 24 * 60 * 60);
}

export function normalizeAccountBalanceAlertIntervalSeconds(value: unknown, fallback: unknown = defaultAccountBalanceAlertIntervalSeconds) {
  return clamp(finiteInteger(value) ?? finiteInteger(fallback) ?? defaultAccountBalanceAlertIntervalSeconds, 60, 24 * 60 * 60);
}

export function normalizeUpstreamMonitorHeavyIntervalMinutes(value: unknown, fallback: unknown = defaultUpstreamMonitorHeavyIntervalMinutes) {
  return clamp(finiteInteger(value) ?? finiteInteger(fallback) ?? defaultUpstreamMonitorHeavyIntervalMinutes, 5, 24 * 60);
}

export function workerRuntimeSettingsFromRows(rows: SettingRow[], env: NodeJS.ProcessEnv = process.env) {
  const map = new Map(rows.map((row) => [row.key, row.value]));
  return {
    workerIntervalSeconds: normalizeWorkerIntervalSeconds(map.get("worker_interval_seconds"), env.S2A_WORKER_INTERVAL_SECONDS),
    accountBalanceAlertIntervalSeconds: normalizeAccountBalanceAlertIntervalSeconds(
      map.get("account_balance_alert_interval_seconds"),
      env.S2A_ACCOUNT_BALANCE_ALERT_INTERVAL_SECONDS,
    ),
    upstreamMonitorHeavyIntervalMinutes: normalizeUpstreamMonitorHeavyIntervalMinutes(
      map.get("upstream_monitor_heavy_interval_minutes"),
      env.S2A_UPSTREAM_MONITOR_HEAVY_INTERVAL_MINUTES,
    ),
  };
}

export async function getWorkerRuntimeSettings(db: SettingsDb, env: NodeJS.ProcessEnv = process.env) {
  const rows = await db.setting.findMany({ where: { key: { in: [...workerSettingKeys] } } });
  return workerRuntimeSettingsFromRows(rows, env);
}
