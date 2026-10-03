import { checkAccountBalanceAlerts } from "@/server/account-balance-alert";
import {
  runAccountPoolPolicies,
  runRapidAccountPoolBursts,
} from "@/server/account-pool-policy";
import {
  isCanonicalTargetDisabled,
  listMappedConnectionIds,
  resolveCanonicalTarget,
} from "@/server/canonical-target";
import { Sub2ApiAdminClient } from "@/server/clients/sub2api-admin";
import { db } from "@/server/db";
import { applyBoundRateRulesForConnection } from "@/server/bl-rate-sync";
import { cleanupOldLogs, writeSyncLog } from "@/server/sync-logs";
import { runDueUpstreamMonitors } from "@/server/upstream-monitor";
import {
  normalizeWorkerIntervalSeconds,
  workerRuntimeSettingsFromRows,
} from "@/server/worker-settings";
import { planWorkerRun } from "@/server/worker-run-plan";

const runOnce = process.env.S2A_WORKER_ONCE === "1";
let stopping = false;
let wake: (() => void) | null = null;

type WorkerRunRequest = {
  raw: string;
  requestedAt: Date;
  targetId: number | null;
  mode: string;
};

function requestStop() {
  stopping = true;
  wake?.();
}

process.on("SIGINT", requestStop);
process.on("SIGTERM", requestStop);

async function setState(fields: Record<string, string | number | Date | null>) {
  await Promise.all(Object.entries(fields).map(([key, value]) => db.setting.upsert({
    where: { key },
    create: { key, value: value instanceof Date ? value.toISOString() : value === null ? "" : String(value) },
    update: { value: value instanceof Date ? value.toISOString() : value === null ? "" : String(value) },
  })));
}

async function logFailure(connectionId: number, action: string, error: unknown) {
  const message = error instanceof Error ? error.message : String(error);
  await writeSyncLog(db, {
    connectionId,
    action,
    target: `connection:${connectionId}`,
    detail: {},
    status: "failed",
    error: message,
  }).catch(() => undefined);
  console.error(JSON.stringify({ level: "error", action, connectionId, message }));
}

async function readWorkerRunRequest(): Promise<WorkerRunRequest | null> {
  const rows = await db.setting.findMany({
    where: { key: { in: ["worker_run_requested_at", "worker_run_requested_target_id", "worker_run_requested_mode"] } },
  });
  const values = new Map(rows.map((row) => [row.key, row.value]));
  const raw = values.get("worker_run_requested_at")?.trim() ?? "";
  const requestedAt = new Date(raw);
  if (!raw || !Number.isFinite(requestedAt.getTime())) return null;
  const target = Number(values.get("worker_run_requested_target_id"));
  return {
    raw,
    requestedAt,
    targetId: Number.isInteger(target) && target > 0 ? target : null,
    mode: values.get("worker_run_requested_mode")?.trim() || "cycle",
  };
}

async function consumeWorkerRunRequest() {
  const request = await readWorkerRunRequest();
  if (!request) return null;
  const claimed = await db.setting.updateMany({
    where: { key: "worker_run_requested_at", value: request.raw },
    data: { value: "" },
  });
  if (claimed.count !== 1) return null;
  await setState({
    worker_run_consumed_at: new Date(),
    worker_run_consumed_target_id: request.targetId ?? "",
    worker_run_consumed_mode: request.mode,
  });
  return request;
}

async function runBalanceAlerts(targetId?: number | null) {
  const connectionIds = targetId ? [targetId] : await listMappedConnectionIds(db);
  for (const connectionId of connectionIds) {
    if (stopping) return;
    try {
      const target = await resolveCanonicalTarget(db, connectionId);
      const client = new Sub2ApiAdminClient(target.baseUrl, target.adminApiKey);
      await checkAccountBalanceAlerts({
        db,
        connectionId,
        connectionName: target.name,
        s2Client: client,
        force: true,
        ignoreCooldown: false,
        action: "auto_account_balance_webhook_alert",
      });
    } catch (error) {
      if (isCanonicalTargetDisabled(error)) continue;
      await logFailure(connectionId, "auto_account_balance_webhook_alert", error);
    }
    await setState({ worker_heartbeat_at: new Date() });
  }
}

async function runRatePolicies(targetId?: number | null) {
  const connectionIds = targetId ? [targetId] : await listMappedConnectionIds(db);
  const failures: string[] = [];
  for (const connectionId of connectionIds) {
    if (stopping) return;
    try {
      const result = await applyBoundRateRulesForConnection({
        db,
        connectionId,
        includeAccountRules: true,
        includePriorityRules: true,
      });
      if (!result.ok && !result.skipped) {
        failures.push(`${connectionId}: ${result.message}`);
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      failures.push(`${connectionId}: ${message}`);
      await logFailure(connectionId, "auto_rate_policies", error);
    }
    await setState({ worker_heartbeat_at: new Date() });
  }
  if (failures.length > 0) {
    throw new Error(`rate policies failed: ${failures.join("; ")}`);
  }
}

async function runCycle() {
  const startedAt = new Date();
  const request = await consumeWorkerRunRequest();
  const plan = planWorkerRun(request?.mode);
  const settings = await db.setting.findMany();
  const runtime = workerRuntimeSettingsFromRows(settings);
  await setState({
    worker_heartbeat_at: startedAt,
    worker_last_run_started_at: startedAt,
    worker_last_run_status: "running",
    worker_last_run_message: request
      ? `requested extension worker cycle running (${request.mode})`
      : "extension worker cycle running",
    worker_interval_seconds: runtime.workerIntervalSeconds,
    account_balance_alert_interval_seconds: runtime.accountBalanceAlertIntervalSeconds,
    upstream_monitor_heavy_interval_minutes: runtime.upstreamMonitorHeavyIntervalMinutes,
    worker_next_run_at: null,
  });

  let status = "success";
  let message = "extension worker cycle completed";
  try {
    if (!stopping && plan.cleanupLogs) await cleanupOldLogs(db);
    if (!stopping && plan.balanceAlerts) await runBalanceAlerts(request?.targetId);
    if (!stopping && plan.upstreamMonitors) {
      await runDueUpstreamMonitors(db, new Date(), {
        concurrency: 3,
        heavyIntervalMinutes: runtime.upstreamMonitorHeavyIntervalMinutes,
        connectionId: request?.targetId ?? undefined,
        forceCheckMode: plan.forceCheckMode,
        reason: request ? "manual" : "scheduled",
        shouldStop: () => stopping,
        onProgress: () => setState({ worker_heartbeat_at: new Date() }),
      });
    }
    if (!stopping && plan.ratePolicies) {
      await runRatePolicies(request?.targetId);
    }
    if (!stopping && plan.accountPoolPolicies) {
      await runAccountPoolPolicies(db, new Date(), () => stopping, request?.targetId ?? undefined);
    }
  } catch (error) {
    status = "failed";
    message = error instanceof Error ? error.message : String(error);
    console.error(JSON.stringify({ level: "error", action: "extension_worker_cycle", message }));
  }

  const finishedAt = new Date();
  const nextRunAt = stopping || runOnce
    ? null
    : new Date(startedAt.getTime() + runtime.workerIntervalSeconds * 1000);
  await setState({
    worker_heartbeat_at: finishedAt,
    worker_last_run_finished_at: finishedAt,
    worker_last_run_status: stopping ? "stopped" : status,
    worker_last_run_message: stopping ? "extension worker stopped" : message,
    worker_last_run_duration_ms: finishedAt.getTime() - startedAt.getTime(),
    worker_next_run_at: nextRunAt,
  });
  return runtime.workerIntervalSeconds;
}

async function delayWithRapidChecks(seconds: number) {
  const deadline = Date.now() + seconds * 1000;
  while (!stopping && Date.now() < deadline) {
    await new Promise<void>((resolve) => {
      const timer = setTimeout(resolve, Math.min(10_000, Math.max(0, deadline - Date.now())));
      wake = () => {
        clearTimeout(timer);
        resolve();
      };
    });
    wake = null;
    if (!stopping && Date.now() < deadline) {
      if (await readWorkerRunRequest()) return;
      await runRapidAccountPoolBursts(db, new Date(), () => stopping).catch((error) => {
        console.error(JSON.stringify({
          level: "error",
          action: "rapid_account_pool_burst",
          message: error instanceof Error ? error.message : String(error),
        }));
      });
      await setState({ worker_heartbeat_at: new Date() });
    }
  }
}

async function main() {
  let intervalSeconds = normalizeWorkerIntervalSeconds(process.env.S2A_WORKER_INTERVAL_SECONDS);
  while (!stopping) {
    try {
      intervalSeconds = await runCycle();
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      console.error(JSON.stringify({
        level: "error",
        action: "extension_worker_cycle_uncaught",
        message,
      }));
      // Keep discovery alive across transient database/config failures. The
      // next cycle retries the complete policy set instead of requiring a
      // manual apply or a container restart.
      intervalSeconds = normalizeWorkerIntervalSeconds(
        process.env.S2A_WORKER_RETRY_INTERVAL_SECONDS,
        60,
      );
    }
    if (runOnce) break;
    await delayWithRapidChecks(intervalSeconds);
  }
  await db.$disconnect();
}

main().catch(async (error) => {
  console.error(JSON.stringify({
    level: "fatal",
    action: "extension_worker",
    message: error instanceof Error ? error.message : String(error),
  }));
  await db.$disconnect().catch(() => undefined);
  process.exitCode = 1;
});
