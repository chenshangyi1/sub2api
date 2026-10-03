import type { PrismaClient, UpstreamMonitorRule } from "@prisma/client";
import { isCanonicalTargetDisabled, resolveCanonicalTarget } from "@/server/canonical-target";
import { Sub2ApiAdminClient } from "@/server/clients/sub2api-admin";
import { writeSyncLog } from "@/server/sync-logs";
import { getAccountId } from "@/server/account-utils";
import { fetchAccountBalances, isAccountBalanceExhausted } from "@/server/account-balance";
import { isBalanceExhaustedAccountResult } from "@/server/account-health-classification";
import { applyBoundRateRulesForConnection } from "@/server/bl-rate-sync";
import { resolveAccountProbeTarget, syncRuleToSub2ApiChannelMonitor } from "@/server/sub2api-channel-monitor-sync";
import { runApiCapabilityProbeSuite, runStreamPerformanceProbe } from "@/server/api-capability-probe";
import { selectMonitorModelCandidates } from "@/server/monitor-model-selection";
import { acquireMonitorExecutionLease, releaseMonitorExecutionLease } from "@/server/monitor-execution-lease";
import {
  pauseAccountMonitorRateSources,
  restoreAccountMonitorRateSources,
  type MonitorRateExclusionSyncResult,
} from "@/server/upstream-monitor-rate-exclusions";

type MonitorDb = Pick<
  PrismaClient,
  | "upstreamMonitorRule"
  | "upstreamMonitorResult"
  | "blSourceBinding"
  | "blGroupRateRule"
  | "blAccountRateRule"
  | "announcementRule"
  | "upstreamMonitorRateExclusion"
  | "legacyImportMap"
  | "upstreamSyncTarget"
  | "setting"
  | "$transaction"
>;

type RemoteAccount = {
  id?: number | string | null;
  name?: string | null;
  username?: string | null;
  schedulable?: boolean | null;
};

type ExecuteMonitorOptions = {
  db: MonitorDb;
  rule: UpstreamMonitorRule;
  client?: Sub2ApiAdminClient;
  account?: RemoteAccount | null;
  now?: Date;
  reason?: "manual" | "scheduled";
  timeoutMs?: number;
  heavyIntervalMinutes?: number;
  forceCheckMode?: "light" | "heavy";
};

type RunDueMonitorOptions = {
  timeoutMs?: number;
  concurrency?: number;
  heavyIntervalMinutes?: number;
  onProgress?: () => Promise<void>;
  shouldStop?: () => boolean;
  connectionId?: number;
  forceCheckMode?: "light" | "heavy";
  reason?: "manual" | "scheduled";
};

const monitorResultKeepCount = 100;

export class MonitorExecutionBusyError extends Error {
  constructor(ruleId: number) {
    super(`渠道检测规则 #${ruleId} 正在执行，请稍后再试`);
    this.name = "MonitorExecutionBusyError";
  }
}

export function shouldRunHeavyMonitorCheck(lastHeavyCheckedAt: Date | null | undefined, now: Date, heavyIntervalMinutes: number) {
  const normalizedMinutes = Number.isFinite(heavyIntervalMinutes) && heavyIntervalMinutes > 0
    ? Math.max(1, Math.floor(heavyIntervalMinutes))
    : 1;
  if (!lastHeavyCheckedAt || !Number.isFinite(lastHeavyCheckedAt.getTime())) return true;
  return now.getTime() - lastHeavyCheckedAt.getTime() >= normalizedMinutes * 60_000;
}

export function selectMonitorCheckMode(input: {
  lastHeavyCheckedAt?: Date | null;
  pausedUntil?: Date | null;
  now: Date;
  heavyIntervalMinutes: number;
}): "light" | "heavy" {
  const recoveryDue = Boolean(input.pausedUntil && input.pausedUntil.getTime() <= input.now.getTime());
  return recoveryDue || shouldRunHeavyMonitorCheck(input.lastHeavyCheckedAt, input.now, input.heavyIntervalMinutes)
    ? "heavy"
    : "light";
}

export function nextMonitorFailureCounts(input: {
  checkMode: "light" | "heavy";
  success: boolean;
  lightFailures: number;
  heavyFailures: number;
}) {
  return {
    lightFailures: input.checkMode === "light"
      ? input.success ? 0 : input.lightFailures + 1
      : input.lightFailures,
    heavyFailures: input.checkMode === "heavy"
      ? input.success ? 0 : input.heavyFailures + 1
      : input.heavyFailures,
  };
}

export function shouldUseCredentialProbe(balance: { status?: string; message?: string | null } | null | undefined) {
  return balance?.status === "unsupported" || balance?.status === "error";
}

export function credentialProbeDisposition(result: { status: string; httpStatus?: number }) {
  if (result.status === "pass") return "pass" as const;
  if (result.httpStatus === 404 || result.httpStatus === 405) return "neutral" as const;
  return "fail" as const;
}

export function shouldPublishExternalMonitorResult(balanceExhausted: boolean) {
  return !balanceExhausted;
}

export { selectMonitorModelCandidates } from "@/server/monitor-model-selection";

function addMinutes(date: Date, minutes: number) {
  return new Date(date.getTime() + Math.max(1, minutes) * 60_000);
}

function truncate(value: string, max = 2_000) {
  return value.length > max ? `${value.slice(0, max)}...` : value;
}

function accountLabel(account: RemoteAccount | null | undefined, fallbackId: number) {
  const name = account?.name ?? account?.username;
  return name?.trim() || `#${fallbackId}`;
}

export async function runWithConcurrency<T>(
  items: T[],
  concurrency: number,
  worker: (item: T) => Promise<void>,
  shouldStop?: () => boolean,
) {
  const limit = Math.min(Math.max(1, Math.floor(concurrency)), Math.max(1, items.length));
  let index = 0;
  await Promise.all(Array.from({ length: limit }, async () => {
    while (index < items.length) {
      if (shouldStop?.()) return;
      const current = items[index];
      index += 1;
      await worker(current);
    }
  }));
}

function findAccount(accounts: unknown[], accountId: number) {
  return accounts.find((account) => {
    if (!account || typeof account !== "object") return false;
    return getAccountId(account) === accountId;
  }) as RemoteAccount | undefined;
}

async function getClient(db: MonitorDb, connectionId: number) {
  const target = await resolveCanonicalTarget(db, connectionId);
  return new Sub2ApiAdminClient(target.baseUrl, target.adminApiKey);
}

async function safeLogSync(db: MonitorDb, connectionId: number, action: string, target: string, detail: Record<string, unknown>, status: "success" | "failed", error?: string) {
  try {
    await writeSyncLog(db, { connectionId, action, target, detail, status, error });
  } catch {
    // Monitor logging must not stop account recovery or checks.
  }
}

async function applyMonitorRateExclusionChanges(input: {
  db: MonitorDb;
  connectionId: number;
  accountId: number;
  action: "pause" | "restore";
  result: MonitorRateExclusionSyncResult;
}) {
  if (input.result.count === 0) return;

  try {
    const sync = await applyBoundRateRulesForConnection({
      db: input.db,
      connectionId: input.connectionId,
      sourceSiteIds: input.result.sourceSiteIds,
      changedSources: input.result.changedSources,
      targetGroupIds: input.result.affectedGroupIds,
      cleanupBindings: false,
    });
    await safeLogSync(
      input.db,
      input.connectionId,
      input.action === "pause" ? "upstream_monitor_exclude_group_rate_sources" : "upstream_monitor_restore_group_rate_sources",
      `account:${input.accountId}`,
      {
        accountId: input.accountId,
        affectedGroupIds: input.result.affectedGroupIds,
        changedSources: input.result.changedSources,
        exclusions: input.result.exclusions.slice(0, 80),
        rateRuleSync: sync.summary,
      },
      sync.ok ? "success" : "failed",
      sync.ok ? undefined : sync.message,
    );
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    await safeLogSync(
      input.db,
      input.connectionId,
      input.action === "pause" ? "upstream_monitor_exclude_group_rate_sources" : "upstream_monitor_restore_group_rate_sources",
      `account:${input.accountId}`,
      {
        accountId: input.accountId,
        affectedGroupIds: input.result.affectedGroupIds,
        changedSources: input.result.changedSources,
        exclusions: input.result.exclusions.slice(0, 80),
      },
      "failed",
      message,
    );
  }
}

async function pauseMonitorRateSources(input: {
  db: MonitorDb;
  connectionId: number;
  accountId: number;
  accountName?: string | null;
  reason?: string | null;
  pausedAt?: Date;
}) {
  try {
    const result = await pauseAccountMonitorRateSources(input);
    await applyMonitorRateExclusionChanges({ ...input, action: "pause", result });
    return result;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    await safeLogSync(
      input.db,
      input.connectionId,
      "upstream_monitor_exclude_group_rate_sources",
      `account:${input.accountId}`,
      { accountId: input.accountId },
      "failed",
      message,
    );
    return {
      affectedGroupIds: [],
      changedSources: [],
      sourceSiteIds: [],
      count: 0,
      exclusions: [],
    };
  }
}

export async function restoreMonitorRateSources(input: {
  db: MonitorDb;
  connectionId: number;
  accountId: number;
  restoredAt?: Date;
}) {
  try {
    const result = await restoreAccountMonitorRateSources(input);
    await input.db.upstreamMonitorRule.updateMany({
      where: {
        connectionId: input.connectionId,
        accountId: input.accountId,
        pausedUntil: { not: null },
      },
      data: {
        pausedUntil: null,
        pauseStartedAt: null,
      },
    });
    await applyMonitorRateExclusionChanges({ ...input, action: "restore", result });
    return result;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    await safeLogSync(
      input.db,
      input.connectionId,
      "upstream_monitor_restore_group_rate_sources",
      `account:${input.accountId}`,
      { accountId: input.accountId },
      "failed",
      message,
    );
    return {
      affectedGroupIds: [],
      changedSources: [],
      sourceSiteIds: [],
      count: 0,
      exclusions: [],
    };
  }
}

async function pruneOldResults(db: MonitorDb, ruleId: number) {
  const stale = await db.upstreamMonitorResult.findMany({
    where: { ruleId },
    select: { id: true },
    orderBy: { createdAt: "desc" },
    skip: monitorResultKeepCount,
  });
  if (stale.length > 0) {
    await db.upstreamMonitorResult.deleteMany({ where: { id: { in: stale.map((row) => row.id) } } });
  }
}

async function createResult(input: {
  db: MonitorDb;
  rule: UpstreamMonitorRule;
  status: string;
  message?: string | null;
  latencyMs?: number | null;
  model?: string | null;
  checkMode: "light" | "heavy";
  firstTokenMs?: number | null;
  streamTps?: number | null;
  startedAt: Date;
  finishedAt: Date;
}) {
  await input.db.upstreamMonitorResult.create({
    data: {
      ruleId: input.rule.id,
      connectionId: input.rule.connectionId,
      accountId: input.rule.accountId,
      status: input.status,
      message: input.message ? truncate(input.message) : null,
      latencyMs: input.latencyMs === undefined ? null : input.latencyMs,
      model: input.model || null,
      checkMode: input.checkMode,
      firstTokenMs: input.firstTokenMs ?? null,
      streamTps: input.streamTps ?? null,
      startedAt: input.startedAt,
      finishedAt: input.finishedAt,
    },
  });
  await pruneOldResults(input.db, input.rule.id);
}

async function reconcileSub2ApiChannelMonitor(options: ExecuteMonitorOptions) {
  const client = options.client ?? await getClient(options.db, options.rule.connectionId);
  const suppressNativeMonitor = await shouldSuppressNativeMonitor(options.db, options.rule.connectionId);
  try {
    if (options.rule.sub2apiChannelMonitorId) {
      if (suppressNativeMonitor && options.rule.nativeMonitorSuppressedAt) return options.rule.sub2apiChannelMonitorId;
      await client.updateChannelMonitor(options.rule.sub2apiChannelMonitorId, {
        enabled: suppressNativeMonitor ? false : options.rule.enabled,
        group_name: options.rule.sub2apiGroupName ?? undefined,
        external_ref: `ops:${options.rule.connectionId}:account:${options.rule.accountId}`,
        public_visible: options.rule.enabled,
        management_mode: "external",
      });
      await options.db.upstreamMonitorRule.update({
        where: { id: options.rule.id },
        data: { nativeMonitorSuppressedAt: suppressNativeMonitor ? new Date() : null },
      });
      return options.rule.sub2apiChannelMonitorId;
    }

    const sync = await syncRuleToSub2ApiChannelMonitor({
      client,
      rule: options.rule,
      connectionId: options.rule.connectionId,
      accountId: options.rule.accountId,
      account: options.account ?? null,
      accountName: options.account ? accountLabel(options.account, options.rule.accountId) : options.rule.accountName,
      publicVisible: options.rule.enabled,
      sub2apiGroupId: options.rule.sub2apiGroupId ?? 0,
      sub2apiGroupName: options.rule.sub2apiGroupName ?? "",
      checkIntervalMinutes: options.heavyIntervalMinutes ?? options.rule.checkIntervalMinutes,
      modelId: options.rule.modelId,
    });
    if (!suppressNativeMonitor && sync.monitorId) {
      await client.updateChannelMonitor(sync.monitorId, { enabled: options.rule.enabled });
    }
    if (sync.monitorId && sync.monitorId !== options.rule.sub2apiChannelMonitorId) {
      await options.db.upstreamMonitorRule.update({
        where: { id: options.rule.id },
        data: {
          sub2apiChannelMonitorId: sync.monitorId,
          nativeMonitorSuppressedAt: suppressNativeMonitor ? new Date() : null,
        },
      });
    }
    return sync.monitorId;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    await safeLogSync(
      options.db,
      options.rule.connectionId,
      "upstream_monitor_sync_native_monitor",
      `account:${options.rule.accountId}`,
      { accountId: options.rule.accountId, monitorId: options.rule.sub2apiChannelMonitorId },
      "failed",
      message,
    );
    throw error;
  }
}

async function shouldSuppressNativeMonitor(db: MonitorDb, connectionId: number) {
  try {
    const row = await db.setting.findUnique({
      where: { key: `suppress_native_monitors:${connectionId}` },
      select: { value: true },
    });
    return row?.value.trim().toLowerCase() !== "false";
  } catch {
    return true;
  }
}

async function executeUpstreamMonitorRuleUnlocked(options: ExecuteMonitorOptions) {
  const now = options.now ?? new Date();
  const client = options.client ?? await getClient(options.db, options.rule.connectionId);
  const nativeMonitorId = await reconcileSub2ApiChannelMonitor({ ...options, client });
  const wasPaused = Boolean(options.rule.pausedUntil);
  const startedAt = new Date();
  const checkMode = options.forceCheckMode ?? selectMonitorCheckMode({
    lastHeavyCheckedAt: options.rule.lastHeavyCheckedAt,
    pausedUntil: options.rule.pausedUntil,
    now,
    heavyIntervalMinutes: options.heavyIntervalMinutes ?? options.rule.checkIntervalMinutes,
  });
  let success = false;
  let message = "";
  let latencyMs: number | null = null;
  let model: string | null = null;
  let firstTokenMs: number | null = null;
  let streamTps: number | null = null;
  let balanceExhausted = false;

  try {
    if (checkMode === "light") {
      const lightStartedAt = Date.now();
      const [balance] = await fetchAccountBalances({
        client,
        connectionId: options.rule.connectionId,
        accountIds: [options.rule.accountId],
        force: true,
        concurrency: 1,
      });
      latencyMs = Date.now() - lightStartedAt;
      if (isAccountBalanceExhausted(balance)) {
        success = false;
        balanceExhausted = true;
        message = "账号余额或额度已耗尽";
      } else if (balance?.status === "ok") {
        success = true;
        message = `轻量检查通过${balance.planName ? `：${balance.planName}` : ""}`;
      } else if (shouldUseCredentialProbe(balance)) {
        const target = await resolveAccountProbeTarget({
          client,
          accountId: options.rule.accountId,
          account: options.account,
          accountName: accountLabel(options.account, options.rule.accountId),
        });
        const credentialProbe = await runApiCapabilityProbeSuite({
          target,
          model: "credential-check",
          probeIds: ["models"],
          timeoutMs: options.timeoutMs ?? 30_000,
        });
        const credentialResult = credentialProbe.results[0];
        const disposition = credentialProbeDisposition(credentialResult);
        success = disposition !== "fail";
        message = disposition === "pass"
          ? "轻量余额接口不支持；上游凭据验证通过，等待重量探测"
          : disposition === "neutral"
            ? "轻量余额和模型接口均不受支持，保持当前状态并等待重量探测"
            : credentialResult?.summary || balance?.message || "轻量凭据验证失败";
      } else {
        success = false;
        message = balance?.message || "轻量检查失败";
      }
      model = null;
    } else {
      const availableModels = await client.getAvailableModels(options.rule.accountId);
      const modelCandidates = selectMonitorModelCandidates(availableModels);
      if (modelCandidates.length === 0) throw new Error("账号没有可用于文本探测的模型");

      let target: Awaited<ReturnType<typeof resolveAccountProbeTarget>> | null = null;
      let targetError = "";
      try {
        target = await resolveAccountProbeTarget({
          client,
          accountId: options.rule.accountId,
          account: options.account,
          accountName: accountLabel(options.account, options.rule.accountId),
        });
      } catch (error) {
        targetError = error instanceof Error ? error.message : String(error);
      }

      const failures: string[] = [];
      for (const candidate of modelCandidates) {
        model = candidate;
        const attemptFailures: string[] = [];
        if (target) {
          try {
            const stream = await runStreamPerformanceProbe({ target, model: candidate, timeoutMs: options.timeoutMs });
            if (stream.success) {
              success = true;
              message = stream.message;
              latencyMs = stream.latencyMs;
              firstTokenMs = stream.firstTokenMs;
              streamTps = stream.streamTps;
              break;
            }
            attemptFailures.push(stream.message);
          } catch (error) {
            attemptFailures.push(error instanceof Error ? error.message : String(error));
          }
        } else if (targetError) {
          attemptFailures.push(targetError);
        }

        try {
          const fallback = await client.testAccount(options.rule.accountId, {
            model_id: candidate,
            prompt: options.rule.prompt ?? undefined,
            timeoutMs: options.timeoutMs,
          });
          if (fallback.success) {
            success = true;
            message = [attemptFailures[0], fallback.message].filter(Boolean).join("; ");
            latencyMs = Number.isFinite(fallback.latency_ms) ? Math.round(fallback.latency_ms) : latencyMs;
            model = fallback.model ?? candidate;
            break;
          }
          attemptFailures.push(fallback.message);
        } catch (error) {
          attemptFailures.push(error instanceof Error ? error.message : String(error));
        }
        failures.push(`${candidate}: ${attemptFailures.filter(Boolean).join(" / ")}`);
      }

      if (!success) message = `自动模型探测失败（已尝试 ${modelCandidates.join(", ")}）：${failures.join("；")}`;
    }
  } catch (error) {
    success = false;
    message = error instanceof Error ? error.message : String(error);
  }

  const finishedAt = new Date();
  const status = success ? "success" : "failed";
  const nextFailureCounts = nextMonitorFailureCounts({
    checkMode,
    success,
    lightFailures: options.rule.consecutiveLightFailures,
    heavyFailures: options.rule.consecutiveHeavyFailures,
  });
  let consecutiveLightFailures = nextFailureCounts.lightFailures;
  let consecutiveHeavyFailures = nextFailureCounts.heavyFailures;
  let consecutiveFailures = Math.max(consecutiveLightFailures, consecutiveHeavyFailures);
  let pausedUntil: Date | null | undefined = undefined;
  let pauseStartedAt: Date | null | undefined = undefined;
  const nextCheckAt = addMinutes(now, options.rule.checkIntervalMinutes);
  let pauseApplied = false;
  let resumeApplied = false;
  let temporaryUnavailableApplied = false;
  let temporaryUnavailableCleared = false;
  balanceExhausted = balanceExhausted || (!success && isBalanceExhaustedAccountResult({ status, message }));

  if (balanceExhausted) {
    const until = addMinutes(now, options.rule.pauseMinutes);
    try {
      await client.setTempUnschedulable(options.rule.accountId, {
        untilUnix: Math.floor(until.getTime() / 1_000),
        matchedKeyword: "ops_balance_exhausted",
        errorMessage: message,
      });
      temporaryUnavailableApplied = true;
      consecutiveLightFailures = 0;
      consecutiveHeavyFailures = 0;
      consecutiveFailures = 0;
      message = `${message}；已在 Sub2API 中标记为临时不可用至 ${until.toLocaleString()}，不修改渠道状态`;
    } catch (error) {
      message = `${message}；写入 Sub2API 临时不可用状态失败：${error instanceof Error ? error.message : String(error)}`;
    }
  } else if (success) {
    try {
      const tempState = await client.getTempUnschedulable(options.rule.accountId);
      if (tempState.active && tempState.state?.matched_keyword === "ops_balance_exhausted") {
        await client.clearTempUnschedulable(options.rule.accountId, "ops_balance_exhausted");
        temporaryUnavailableCleared = true;
        message = `${message}；余额检查恢复，已清除 Sub2API 临时不可用状态`;
      }
    } catch {
      // A failed status lookup must not turn a successful health check into a pause.
    }
  }

  if (!balanceExhausted && success && wasPaused && checkMode === "heavy") {
    try {
      await client.setSchedulable(options.rule.accountId, true);
      await restoreMonitorRateSources({
        db: options.db,
        connectionId: options.rule.connectionId,
        accountId: options.rule.accountId,
        restoredAt: now,
      });
      pausedUntil = null;
      pauseStartedAt = null;
      resumeApplied = true;
      consecutiveLightFailures = 0;
      consecutiveHeavyFailures = 0;
      consecutiveFailures = 0;
      message = `${message}；检测已恢复正常，已恢复账号调度`;
      await safeLogSync(
        options.db,
        options.rule.connectionId,
        "upstream_monitor_resume_account",
        `account:${options.rule.accountId}`,
        {
          accountId: options.rule.accountId,
          accountName: accountLabel(options.account, options.rule.accountId),
          reason: "monitor_recovered",
        },
        "success",
      );
    } catch (error) {
      const resumeError = error instanceof Error ? error.message : String(error);
      message = `${message}；检测已恢复正常，但恢复账号调度失败：${resumeError}`;
      await safeLogSync(
        options.db,
        options.rule.connectionId,
        "upstream_monitor_resume_account",
        `account:${options.rule.accountId}`,
        {
          accountId: options.rule.accountId,
          accountName: accountLabel(options.account, options.rule.accountId),
          reason: "monitor_recovered",
        },
        "failed",
        resumeError,
      );
    }
  }
  if (!balanceExhausted && success && wasPaused && checkMode === "light") {
    message = `${message}；账号暂停中，等待重量探测通过后恢复调度`;
  }

  const currentModeFailures = checkMode === "light" ? consecutiveLightFailures : consecutiveHeavyFailures;
  if (!balanceExhausted && !success && currentModeFailures >= options.rule.failureThreshold) {
    const account = options.account;
    if (wasPaused) {
      if (options.rule.pausedUntil && options.rule.pausedUntil.getTime() <= now.getTime()) {
        pausedUntil = nextCheckAt;
      }
      message = `${message}；账号暂停调度中，继续按检测间隔监测`;
    } else if (account?.schedulable === false) {
      message = `${message}；已达到连续失败阈值，但账号当前未参与调度，未设置自动恢复`;
    } else {
      const targetPausedUntil = addMinutes(now, options.rule.pauseMinutes);
      try {
        await client.setSchedulable(options.rule.accountId, false);
        pausedUntil = targetPausedUntil;
        pauseStartedAt = now;
        consecutiveLightFailures = 0;
        consecutiveHeavyFailures = 0;
        consecutiveFailures = 0;
        pauseApplied = true;
        const rateExclusions = await pauseMonitorRateSources({
          db: options.db,
          connectionId: options.rule.connectionId,
          accountId: options.rule.accountId,
          accountName: accountLabel(account, options.rule.accountId),
          reason: "upstream_monitor_pause",
          pausedAt: now,
        });
        message = `${message}；已连续失败 ${options.rule.failureThreshold} 次，已暂停调度至 ${targetPausedUntil.toLocaleString()}，期间继续按检测间隔监测`;
        await safeLogSync(
          options.db,
          options.rule.connectionId,
          "upstream_monitor_pause_account",
          `account:${options.rule.accountId}`,
          {
            accountId: options.rule.accountId,
            accountName: accountLabel(account, options.rule.accountId),
            failureThreshold: options.rule.failureThreshold,
            pauseMinutes: options.rule.pauseMinutes,
            pausedUntil: targetPausedUntil,
            excludedGroupRateSources: rateExclusions.count,
            affectedGroupIds: rateExclusions.affectedGroupIds,
            reason: message,
          },
          "success",
        );
      } catch (error) {
        const pauseError = error instanceof Error ? error.message : String(error);
        message = `${message}；已达到连续失败阈值，但暂停调度失败：${pauseError}`;
        await safeLogSync(
          options.db,
          options.rule.connectionId,
          "upstream_monitor_pause_account",
          `account:${options.rule.accountId}`,
          {
            accountId: options.rule.accountId,
            accountName: accountLabel(account, options.rule.accountId),
            failureThreshold: options.rule.failureThreshold,
            pauseMinutes: options.rule.pauseMinutes,
          },
          "failed",
          pauseError,
        );
      }
    }
  }

  await createResult({
    db: options.db,
    rule: options.rule,
    status,
    message,
    latencyMs,
    model,
    checkMode,
    firstTokenMs,
    streamTps,
    startedAt,
    finishedAt,
  });

  if (checkMode === "heavy" && nativeMonitorId && model && shouldPublishExternalMonitorResult(balanceExhausted)) {
    try {
      await client.recordExternalChannelMonitorResults(nativeMonitorId, [{
        model,
        status: success ? "operational" : "failed",
        latency_ms: latencyMs,
        message,
        checked_at: finishedAt.toISOString(),
      }]);
    } catch (error) {
      await safeLogSync(
        options.db,
        options.rule.connectionId,
        "upstream_monitor_publish_external_result",
        `monitor:${nativeMonitorId}`,
        { accountId: options.rule.accountId, monitorId: nativeMonitorId, model, status },
        "failed",
        error instanceof Error ? error.message : String(error),
      );
    }
  }

  const updated = await options.db.upstreamMonitorRule.update({
    where: { id: options.rule.id },
    data: {
      accountName: options.account ? accountLabel(options.account, options.rule.accountId) : options.rule.accountName,
      consecutiveFailures,
      consecutiveLightFailures,
      consecutiveHeavyFailures,
      totalChecks: { increment: 1 },
      successChecks: success ? { increment: 1 } : undefined,
      lastStatus: status,
      lastMessage: truncate(message),
      lastLatencyMs: latencyMs,
      lastHeavyCheckedAt: checkMode === "heavy" ? finishedAt : undefined,
      lastCheckMode: checkMode,
      lastFirstTokenMs: checkMode === "heavy" ? firstTokenMs : undefined,
      lastStreamTps: checkMode === "heavy" ? streamTps : undefined,
      lastCheckedAt: finishedAt,
      nextCheckAt,
      pausedUntil,
      pauseStartedAt,
    },
  });

  await safeLogSync(
    options.db,
    options.rule.connectionId,
    "upstream_monitor_check",
    `account:${options.rule.accountId}`,
    {
      accountId: options.rule.accountId,
      accountName: accountLabel(options.account, options.rule.accountId),
      status,
      latencyMs,
      model,
      checkMode,
      firstTokenMs,
      streamTps,
      consecutiveFailures,
      consecutiveLightFailures,
      consecutiveHeavyFailures,
      pauseApplied,
      resumeApplied,
      temporaryUnavailableApplied,
      temporaryUnavailableCleared,
      reason: options.reason ?? "scheduled",
    },
    success ? "success" : "failed",
    success ? undefined : message,
  );

  return { rule: updated, success, status, message, latencyMs, model, checkMode, firstTokenMs, streamTps, nativeMonitorId, pauseApplied, resumeApplied, temporaryUnavailableApplied, temporaryUnavailableCleared };
}

export async function executeUpstreamMonitorRule(options: ExecuteMonitorOptions) {
  const lease = await acquireMonitorExecutionLease({
    db: options.db,
    ruleId: options.rule.id,
    ttlMs: Math.max(8 * 60_000, (options.timeoutMs ?? 70_000) * 7),
  });
  if (!lease) throw new MonitorExecutionBusyError(options.rule.id);

  try {
    return await executeUpstreamMonitorRuleUnlocked(options);
  } finally {
    await releaseMonitorExecutionLease(options.db, lease).catch(() => undefined);
  }
}

export async function reschedulePausedMonitorChecks(db: MonitorDb, now = new Date(), shouldStop?: () => boolean) {
  const rules = await db.upstreamMonitorRule.findMany({
    where: {
      enabled: true,
      pausedUntil: { gt: now },
    },
    orderBy: [{ connectionId: "asc" }, { nextCheckAt: "asc" }],
  });

  let rescheduled = 0;
  for (const rule of rules) {
    if (shouldStop?.()) break;
    const lastCheckBasis = rule.lastCheckedAt ?? rule.pauseStartedAt ?? rule.updatedAt;
    const shouldHaveCheckedAt = addMinutes(lastCheckBasis, rule.checkIntervalMinutes);
    if (shouldHaveCheckedAt > now) continue;
    if (rule.nextCheckAt && rule.nextCheckAt <= now) continue;
    await db.upstreamMonitorRule.update({
      where: { id: rule.id },
      data: { nextCheckAt: now },
    });
    rescheduled += 1;
  }

  return rescheduled;
}

export async function scheduleExpiredMonitorRecoveryChecks(db: MonitorDb, now = new Date()) {
  const result = await db.upstreamMonitorRule.updateMany({
    where: {
      enabled: true,
      pausedUntil: { lte: now },
    },
    data: { nextCheckAt: now },
  });
  return result.count;
}

export async function runDueUpstreamMonitors(db: MonitorDb, now = new Date(), options: RunDueMonitorOptions = {}) {
  if (options.shouldStop?.()) return 0;
  await scheduleExpiredMonitorRecoveryChecks(db, now);
  if (options.shouldStop?.()) return 0;
  await reschedulePausedMonitorChecks(db, now, options.shouldStop);
  if (options.shouldStop?.()) return 0;

  const rules = await db.upstreamMonitorRule.findMany({
    where: {
      enabled: true,
      ...(options.connectionId ? { connectionId: options.connectionId } : {}),
      OR: [{ nextCheckAt: null }, { nextCheckAt: { lte: now } }],
    },
    orderBy: [{ connectionId: "asc" }, { nextCheckAt: "asc" }],
  });
  if (rules.length === 0) return 0;

  const rulesByConnection = new Map<number, UpstreamMonitorRule[]>();
  for (const rule of rules) {
    const current = rulesByConnection.get(rule.connectionId) ?? [];
    current.push(rule);
    rulesByConnection.set(rule.connectionId, current);
  }

  let checked = 0;
  for (const [connectionId, connectionRules] of rulesByConnection) {
    if (options.shouldStop?.()) break;
    let client: Sub2ApiAdminClient;
    try {
      client = await getClient(db, connectionId);
    } catch (error) {
      if (isCanonicalTargetDisabled(error)) continue;
      const message = error instanceof Error ? error.message : String(error);
      await safeLogSync(
        db,
        connectionId,
        "upstream_monitor_connection",
        `connection:${connectionId}`,
        { ruleCount: connectionRules.length },
        "failed",
        message,
      );
      continue;
    }

    let accounts: unknown[] = [];
    try {
      const payload = await client.listAccounts();
      accounts = Array.isArray(payload) ? payload : [];
    } catch {
      accounts = [];
    }

    await runWithConcurrency(connectionRules, options.concurrency ?? 3, async (rule) => {
      const account = findAccount(accounts, rule.accountId);
      try {
        await executeUpstreamMonitorRule({
          db,
          rule,
          client,
          account,
          now,
          reason: options.reason ?? "scheduled",
          timeoutMs: options.timeoutMs,
          heavyIntervalMinutes: options.heavyIntervalMinutes,
          forceCheckMode: options.forceCheckMode,
        });
        checked += 1;
      } catch (error) {
        if (error instanceof MonitorExecutionBusyError) return;
        const message = error instanceof Error ? error.message : String(error);
        await safeLogSync(
          db,
          rule.connectionId,
          "upstream_monitor_check",
          `account:${rule.accountId}`,
          { accountId: rule.accountId, reason: options.reason ?? "scheduled" },
          "failed",
          message,
        );
      } finally {
        await options.onProgress?.();
      }
    }, options.shouldStop);
  }

  return checked;
}
