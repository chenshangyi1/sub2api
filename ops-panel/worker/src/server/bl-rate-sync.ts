import type { Prisma, PrismaClient } from "@prisma/client";
import {
  CanonicalRateClient,
  isCanonicalRateFresh,
  resolveCanonicalEffectiveRate,
} from "@/server/clients/canonical-rate";
import { Sub2ApiAdminClient, type Sub2ApiGroup } from "@/server/clients/sub2api-admin";
import { evaluateGroupRateRule } from "@/server/rate-rule-evaluator";
import { CanonicalTargetResolutionError, resolveCanonicalTarget } from "@/server/canonical-target";
import { gateRateRuleSources, rateRuleNeedsSource } from "@/shared/rate-rule-source-gate";
import { publishRateChangeAnnouncements } from "@/server/announcement-rules";
import { ratesEqual } from "@/shared/rates";
import { writeSyncLog } from "@/server/sync-logs";
import { getAccountId, getAccountName, getAccountRate } from "@/server/account-utils";
import {
  MANUAL_ZERO_RATE_REASON,
  groupAccountRateSkipPolicy,
  isAccountEnabledForScheduling,
  resolveGroupAccountRateMultiplier,
  syncGroupAccountRateMultipliers,
} from "@/server/group-account-rate-sync";
import { applyAccountPriorityRule } from "@/server/account-priority-rule";
import {
  groupMonitorRateExclusionsBySource,
  monitorGroupRateExclusionKey,
  readActiveMonitorRateExclusions,
  type ChangedMonitorRateSource,
} from "@/server/upstream-monitor-rate-exclusions";

type RateRuleDb = Pick<Prisma.TransactionClient, "blSourceBinding" | "blGroupRateRule" | "blAccountRateRule" | "announcementRule" | "upstreamMonitorRateExclusion" | "upstreamMonitorRule" | "legacyImportMap" | "upstreamSyncTarget" | "setting">;
type LockableRateSyncDb = RateRuleDb & Pick<PrismaClient, "$transaction">;

type LoggableDb = unknown;

export type BoundRateSyncSummary = {
  appliedGroupRules: number;
  appliedAccountRules: number;
  appliedPriorityRules: number;
  skippedGroupRules: number;
  skippedAccountRules: number;
  skippedPriorityRules: number;
  failedGroupRules: number;
  failedAccountRules: number;
  failedPriorityRules: number;
};

export type ChangedRateSource = {
  sourceSiteId: number;
  sourceGroupId: string;
};

type SourceBindingCleanupSummary = {
  removedBindings: number;
  updatedBindings: number;
  disabledRules: number;
  checkedSourceSiteIds: number[];
};

function sourceKey(siteId: number, groupId: string) {
  return `${siteId}:${groupId}`;
}

function nameChanged(left: string | null | undefined, right: string | null | undefined) {
  return (left ?? "").trim() !== (right ?? "").trim();
}

function changedSourceSet(changedSources?: Array<ChangedRateSource | ChangedMonitorRateSource>) {
  if (!changedSources?.length) return null;
  return new Set(changedSources.map((source) => sourceKey(source.sourceSiteId, source.sourceGroupId)));
}

function formatSourceLabel(sources: Array<{ sourceSiteName?: string | null; sourceGroupId: string; sourceGroupName?: string | null }>) {
  if (sources.length === 0) return null;
  return sources
    .map((source) => `${source.sourceSiteName || "BL"} / ${source.sourceGroupName || source.sourceGroupId}`)
    .join(", ");
}

async function logSync(db: LoggableDb, connectionId: number, action: string, target: string, detail: Record<string, unknown>, status: "success" | "failed", error?: string) {
  try {
    await writeSyncLog(db, { connectionId, action, target, detail, status, error });
  } catch {
    // Sync logging must not stop rule application.
  }
}

async function withConnectionLock<T>(db: LockableRateSyncDb, connectionId: number, task: (tx: RateRuleDb) => Promise<T>) {
  const lockKey = (BigInt(connectionId) << 32n) | 0x424c526en;
  return db.$transaction(async (tx) => {
    await tx.$executeRaw`SELECT pg_advisory_xact_lock(${lockKey})`;
    return task(tx);
  }, { maxWait: 30_000, timeout: 180_000 });
}

function sameRate(left: number, right: number) {
  return ratesEqual(left, right);
}

function emptySummary(): BoundRateSyncSummary {
  return {
    appliedGroupRules: 0,
    appliedAccountRules: 0,
    appliedPriorityRules: 0,
    skippedGroupRules: 0,
    skippedAccountRules: 0,
    skippedPriorityRules: 0,
    failedGroupRules: 0,
    failedAccountRules: 0,
    failedPriorityRules: 0,
  };
}

function sourceSiteSet(sourceSiteIds?: number[]) {
  const ids = (sourceSiteIds ?? []).filter((id) => Number.isInteger(id) && id > 0);
  return ids.length > 0 ? new Set(ids) : null;
}

async function groupRuleTargets(input: {
  db: RateRuleDb;
  connectionId: number;
  sourceSiteIds?: number[];
  targetGroupIds?: number[];
}) {
  if (input.targetGroupIds && input.targetGroupIds.length === 0) {
    return { allBindings: [], boundSourceKeys: new Set<string>(), targetGroupIds: new Set<number>(), rules: [] };
  }

  const allBindings = await input.db.blSourceBinding.findMany({
    where: {
      connectionId: input.connectionId,
      targetType: "group",
      ...(input.targetGroupIds?.length ? { targetId: { in: input.targetGroupIds } } : {}),
    },
    orderBy: [{ targetId: "asc" }, { sourceSiteId: "asc" }, { sourceGroupId: "asc" }],
  });
  const sourceSites = sourceSiteSet(input.sourceSiteIds);
  const boundGroupIds = new Set<number>();
  const boundSourceKeys = new Set<string>();
  for (const binding of allBindings) {
    boundSourceKeys.add(sourceKey(binding.sourceSiteId, binding.sourceGroupId));
    if (!sourceSites || sourceSites.has(binding.sourceSiteId)) {
      boundGroupIds.add(binding.targetId);
    }
  }

  const candidateRules = await input.db.blGroupRateRule.findMany({
    where: {
      connectionId: input.connectionId,
      enabled: true,
      ...(input.targetGroupIds?.length ? { groupId: { in: input.targetGroupIds } } : {}),
    },
  });
  const rules = candidateRules.filter((rule) => !rateRuleNeedsSource(rule.mode) || boundGroupIds.has(rule.groupId));
  const targetGroupIds = new Set([...boundGroupIds, ...rules.map((rule) => rule.groupId)]);
  return { allBindings, boundSourceKeys, targetGroupIds, rules };
}

async function latestSourceGroups(input: {
  rateClient: CanonicalRateClient;
  sourceSiteIds?: number[];
}) {
  const sourceSites = sourceSiteSet(input.sourceSiteIds);
  const rates = await input.rateClient.fetchRates(undefined, { bypassCache: true });
  const latest = new Map<string, { siteId: number; siteName: string; groupId: string; groupName: string; platform: string | null }>();
  const checkedSourceSiteIds = new Set<number>();
  for (const rate of rates) {
    if (sourceSites && !sourceSites.has(rate.site_id)) continue;
    if (!isCanonicalRateFresh(rate)) continue;
    checkedSourceSiteIds.add(rate.site_id);
    const entry = {
      siteId: rate.site_id,
      siteName: rate.site_name,
      groupId: rate.group_id,
      groupName: rate.name,
      platform: rate.platform,
    };
    // 双键：group_id（含 remote 兜底）与 model_name 都指向同一源组，供绑定匹配。
    latest.set(sourceKey(rate.site_id, rate.group_id), entry);
    if (rate.remote_group_id && rate.remote_group_id !== rate.group_id) {
      latest.set(sourceKey(rate.site_id, rate.remote_group_id), entry);
    }
    if (rate.name && rate.name !== rate.group_id) {
      latest.set(sourceKey(rate.site_id, rate.name), entry);
    }
  }
  return { latest, checkedSourceSiteIds: [...checkedSourceSiteIds].sort((left, right) => left - right) };
}

async function disableRulesWithoutBindings(input: {
  db: RateRuleDb;
  connectionId: number;
}) {
  const [groupTargets, accountTargets] = await Promise.all([
    input.db.blSourceBinding.findMany({
      where: { connectionId: input.connectionId, targetType: "group" },
      select: { targetId: true },
      distinct: ["targetId"],
    }),
    input.db.blSourceBinding.findMany({
      where: { connectionId: input.connectionId, targetType: "account" },
      select: { targetId: true },
      distinct: ["targetId"],
    }),
  ]);
  const groupIds = groupTargets.map((row) => row.targetId);
  const accountIds = accountTargets.map((row) => row.targetId);
  let disabled = 0;

  disabled += (await input.db.blGroupRateRule.updateMany({
    where: {
      connectionId: input.connectionId,
      enabled: true,
      mode: { notIn: ["locked", "manual_source"] },
      ...(groupIds.length > 0 ? { groupId: { notIn: groupIds } } : {}),
    },
    data: { enabled: false },
  })).count;

  disabled += (await input.db.blAccountRateRule.updateMany({
    where: {
      connectionId: input.connectionId,
      enabled: true,
      mode: { notIn: ["locked", "manual_source"] },
      ...(accountIds.length > 0 ? { accountId: { notIn: accountIds } } : {}),
    },
    data: { enabled: false },
  })).count;

  return disabled;
}

async function cleanupSourceBindings(input: {
  db: RateRuleDb;
  connectionId: number;
  rateClient: CanonicalRateClient;
  sourceSiteIds?: number[];
}): Promise<SourceBindingCleanupSummary> {
  const { latest: sources, checkedSourceSiteIds } = await latestSourceGroups(input);
  if (checkedSourceSiteIds.length === 0) {
    return { removedBindings: 0, updatedBindings: 0, disabledRules: 0, checkedSourceSiteIds };
  }

  const bindings = await input.db.blSourceBinding.findMany({
    where: {
      connectionId: input.connectionId,
      sourceSiteId: { in: checkedSourceSiteIds },
    },
    orderBy: [{ sourceSiteId: "asc" }, { sourceGroupId: "asc" }],
  });

  const missingBindingIds: number[] = [];
  let updatedBindings = 0;
  const renamed: Array<{ targetType: string; targetId: number; sourceSiteId: number; sourceGroupId: string; oldName: string | null; newName: string }> = [];
  const removed: Array<{ targetType: string; targetId: number; sourceSiteId: number; sourceGroupId: string; sourceGroupName: string | null }> = [];

  for (const binding of bindings) {
    const source = sources.get(sourceKey(binding.sourceSiteId, binding.sourceGroupId));
    if (!source) {
      missingBindingIds.push(binding.id);
      removed.push({
        targetType: binding.targetType,
        targetId: binding.targetId,
        sourceSiteId: binding.sourceSiteId,
        sourceGroupId: binding.sourceGroupId,
        sourceGroupName: binding.sourceGroupName,
      });
      continue;
    }

    if (nameChanged(binding.sourceGroupName, source.groupName) || nameChanged(binding.sourceSiteName, source.siteName)) {
      await input.db.blSourceBinding.update({
        where: { id: binding.id },
        data: {
          sourceSiteName: source.siteName,
          sourceGroupName: source.groupName,
          sourcePlatform: source.platform,
        },
      });
      updatedBindings += 1;
      renamed.push({
        targetType: binding.targetType,
        targetId: binding.targetId,
        sourceSiteId: binding.sourceSiteId,
        sourceGroupId: binding.sourceGroupId,
        oldName: binding.sourceGroupName,
        newName: source.groupName,
      });
    }
  }

  if (missingBindingIds.length > 0) {
    await input.db.blSourceBinding.deleteMany({ where: { id: { in: missingBindingIds } } });
  }
  const disabledRules = await disableRulesWithoutBindings(input);
  const summary = { removedBindings: missingBindingIds.length, updatedBindings, disabledRules, checkedSourceSiteIds };

  if (summary.removedBindings > 0 || summary.updatedBindings > 0 || summary.disabledRules > 0) {
    await logSync(input.db, input.connectionId, "cleanup_invalid_data", `connection:${input.connectionId}`, {
      reason: "source group changed",
      sourceSiteIds: input.sourceSiteIds ?? null,
      ...summary,
      renamed,
      removed,
    }, "success");
  }

  return summary;
}

async function unavailableGroupRuleSummary(input: {
  db: RateRuleDb;
  connectionId: number;
  message: string;
  sourceSiteIds?: number[];
  targetGroupIds?: number[];
}) {
  const { boundSourceKeys, targetGroupIds, rules } = await groupRuleTargets(input);
  const summary = emptySummary();
  if (rules.length === 0) return { boundGroupIds: targetGroupIds, boundSourceKeys, summary };

  await logSync(
    input.db,
    input.connectionId,
    "auto_bl_bound_group_rule",
    `connection:${input.connectionId}`,
    { enabledGroupRules: rules.length, sourceSiteIds: input.sourceSiteIds ?? null },
    "failed",
    input.message,
  );
  summary.failedGroupRules = rules.length;
  return { boundGroupIds: targetGroupIds, boundSourceKeys, summary };
}

async function applyBoundGroupRules(input: {
  db: RateRuleDb;
  connectionId: number;
  connectionName?: string | null;
  rateClient: CanonicalRateClient;
  s2Client: Sub2ApiAdminClient;
  groups: Sub2ApiGroup[];
  accounts?: unknown[];
  accountsError?: string | null;
  sourceSiteIds?: number[];
  targetGroupIds?: number[];
}): Promise<{ boundGroupIds: Set<number>; boundSourceKeys: Set<string>; summary: BoundRateSyncSummary }> {
  const { allBindings, boundSourceKeys, targetGroupIds: boundGroupIds, rules } = await groupRuleTargets(input);
  const enabledRuleGroupIds = new Set(rules.map((rule) => rule.groupId));
  if (enabledRuleGroupIds.size === 0) {
    return { boundGroupIds, boundSourceKeys, summary: emptySummary() };
  }

  const rates = await input.rateClient.fetchRates(undefined, { bypassCache: true });
  const rulesByGroup = new Map(rules.map((rule) => [rule.groupId, rule]));
  const groupsById = new Map(input.groups.map((group) => [group.id, group]));
  // 双键注册：同一倍率同时按 group_id（remote_group_id 兜底值）与 model_name（组名）
  // 建索引——绑定无论按上游组 ID 还是组名都能命中（修快照 remote_group_id 为空时全落空）。
  const ratesBySource = new Map<string, (typeof rates)[number]>();
  for (const rate of rates) {
    const primary = sourceKey(rate.site_id, rate.group_id);
    if (!ratesBySource.has(primary)) ratesBySource.set(primary, rate);
    if (rate.remote_group_id && rate.remote_group_id !== rate.group_id) {
      const secondary = sourceKey(rate.site_id, rate.remote_group_id);
      if (!ratesBySource.has(secondary)) ratesBySource.set(secondary, rate);
    }
    if (rate.name && rate.name !== rate.group_id) {
      const byName = sourceKey(rate.site_id, rate.name);
      if (!ratesBySource.has(byName)) ratesBySource.set(byName, rate);
    }
  }
  const exclusionsBySource = groupMonitorRateExclusionsBySource(await readActiveMonitorRateExclusions({
    db: input.db,
    connectionId: input.connectionId,
    groupIds: Array.from(enabledRuleGroupIds),
  }));
  const bindingsByGroup = new Map<number, typeof allBindings>();
  for (const binding of allBindings) {
    const current = bindingsByGroup.get(binding.targetId) ?? [];
    current.push(binding);
    bindingsByGroup.set(binding.targetId, current);
  }

  const summary: BoundRateSyncSummary = emptySummary();

  for (const groupId of enabledRuleGroupIds) {
    const target = groupsById.get(groupId);
    const bindings = bindingsByGroup.get(groupId) ?? [];
    const rule = rulesByGroup.get(groupId);
    if (!target) {
      const message = "No target group found for bound BL rule";
      await Promise.all([
        input.db.blSourceBinding.deleteMany({ where: { connectionId: input.connectionId, targetType: "group", targetId: groupId } }),
        input.db.blGroupRateRule.deleteMany({ where: { connectionId: input.connectionId, groupId } }),
      ]);
      await logSync(input.db, input.connectionId, "auto_bl_bound_group_rule", `group:${groupId}`, {
        groupId,
        deletedInvalidBinding: true,
        deletedInvalidRule: true,
        reason: "target group missing",
        sources: bindings.map((binding) => ({
          sourceSiteId: binding.sourceSiteId,
          sourceSiteName: binding.sourceSiteName,
          sourceGroupId: binding.sourceGroupId,
          sourceGroupName: binding.sourceGroupName ?? "",
        })),
      }, "failed", message);
      summary.failedGroupRules += 1;
      continue;
    }
    if (!rule) continue;

    const sources = bindings.map((binding) => {
      const rate = ratesBySource.get(sourceKey(binding.sourceSiteId, binding.sourceGroupId));
      const monitorExclusions = exclusionsBySource.get(monitorGroupRateExclusionKey(binding.targetId, binding.sourceSiteId, binding.sourceGroupId)) ?? [];
      return {
        sourceSiteId: binding.sourceSiteId,
        sourceSiteName: binding.sourceSiteName,
        sourceGroupId: binding.sourceGroupId,
        sourceGroupName: binding.sourceGroupName ?? "",
        currentRate: resolveCanonicalEffectiveRate(rate),
        rawRate: typeof rate?.rate_multiplier === "number" ? rate.rate_multiplier : null,
        userRate: typeof rate?.user_rate === "number" ? rate.user_rate : null,
        effectiveRate: typeof rate?.effective_rate === "number" ? rate.effective_rate : null,
        rechargeRatio: typeof rate?.recharge_ratio === "number" ? rate.recharge_ratio : null,
        fresh: isCanonicalRateFresh(rate),
        siteEnabled: rate?.site_enabled ?? null,
        lastStatus: rate?.last_status ?? null,
        lastSuccessAt: rate?.last_success_at ?? null,
        collectedAt: rate?.collected_at ?? null,
        monitorExcluded: monitorExclusions.length > 0,
        monitorExclusions: monitorExclusions.map((exclusion) => ({
          accountId: exclusion.accountId,
          accountName: exclusion.accountName,
          reason: exclusion.reason,
        })),
      };
    });

    try {
      const sourceGate = gateRateRuleSources(rule.mode, sources);
      if (!sourceGate.ok) {
        await logSync(input.db, input.connectionId, "auto_bl_bound_group_rule", `group:${target.id}`, {
          targetGroupId: target.id,
          targetGroupName: target.name,
          rule: { enabled: rule.enabled, mode: rule.mode, offset: rule.offset, expression: rule.expression },
          sources,
          skipped: true,
          reason: sourceGate.reason,
        }, "success");
        summary.skippedGroupRules += 1;
        continue;
      }
      const activeSources = sourceGate.sources;
      const currentRate = typeof target.rate_multiplier === "number" ? target.rate_multiplier : 1;
      const rateMultiplier = evaluateGroupRateRule({
        rule: { enabled: rule.enabled, mode: rule.mode as "first" | "average" | "min" | "max" | "custom" | "locked" | "manual_source", offset: rule.offset, expression: rule.expression },
        sourceRates: activeSources.map((source) => source.currentRate),
        currentRate,
      });
      const accountRateMultiplier = resolveGroupAccountRateMultiplier({
        rule: { mode: rule.mode as "first" | "average" | "min" | "max" | "custom" | "locked" | "manual_source", offset: rule.offset },
        sources: activeSources,
      });
      const latestTarget = await input.s2Client.getGroup(target.id).catch(() => target);
      const latestRate = typeof latestTarget.rate_multiplier === "number" ? latestTarget.rate_multiplier : currentRate;
      const groupRateChanged = !sameRate(latestRate, rateMultiplier);
      const detail = {
        targetGroupId: target.id,
        targetGroupName: target.name,
        rule: { enabled: rule.enabled, mode: rule.mode, offset: rule.offset, expression: rule.expression },
        sources,
        sourceSelection: {
          total: sources.length,
          used: activeSources.length,
          skipped: sourceGate.skippedSources.length,
          notice: sourceGate.notice,
        },
        excludedSources: sources.filter((source) => source.monitorExcluded),
        currentRate: latestRate,
        rateMultiplier,
        accountRateMultiplier,
        accountRateSync: null as Awaited<ReturnType<typeof syncGroupAccountRateMultipliers>> | null,
        accountRateSyncSkippedReason: null as string | null,
      };

      const syncAccountRates = async () => {
        if (accountRateMultiplier === null) return;
        if (input.accounts) {
          const skipPolicy = await groupAccountRateSkipPolicy({
            db: input.db,
            connectionId: input.connectionId,
            groupId: target.id,
            accounts: input.accounts,
          });
          detail.accountRateSync = await syncGroupAccountRateMultipliers({
            client: input.s2Client,
            groupId: target.id,
            accounts: input.accounts,
            rateMultiplier: accountRateMultiplier,
            skipAccountIds: skipPolicy.skipAccountIds,
            skipAccountReasons: skipPolicy.skipAccountReasons,
          });
          return;
        }
        detail.accountRateSyncSkippedReason = input.accountsError || "accounts unavailable";
      };

      if (groupRateChanged) {
        await input.s2Client.updateGroupRateMultiplier(target.id, rateMultiplier);
        await syncAccountRates();
      } else {
        await syncAccountRates();
        if (!detail.accountRateSync || (detail.accountRateSync.updated === 0 && detail.accountRateSync.failed === 0)) {
          if (detail.accountRateSync?.accounts.some((account) => account.reason)) {
            await logSync(
              input.db,
              input.connectionId,
              "auto_bl_bound_group_rule",
              `group:${target.id}`,
              detail,
              "success",
            );
          }
          summary.skippedGroupRules += 1;
          continue;
        }
      }
      const firstSource = sources[0];
      if (groupRateChanged) {
        await publishRateChangeAnnouncements({
          db: input.db,
          client: input.s2Client,
          context: {
            action: "auto_bl_bound_group_rule",
            connectionId: input.connectionId,
            connectionName: input.connectionName,
            groupId: target.id,
            groupName: target.name,
            oldRate: latestRate,
            newRate: rateMultiplier,
            sourceSiteId: firstSource?.sourceSiteId,
            sourceSiteName: firstSource?.sourceSiteName,
            sourceGroupId: firstSource?.sourceGroupId,
            sourceGroupName: firstSource?.sourceGroupName,
            sourceRate: firstSource?.currentRate,
            sourceLabel: formatSourceLabel(sources),
            ruleMode: rule?.mode ?? "first",
            ruleExpression: rule?.expression,
            changedAt: new Date(),
          },
        });
      }
      if ((detail.accountRateSync?.failed ?? 0) > 0) {
        const message = `${detail.accountRateSync?.failed} account rate update(s) failed`;
        await logSync(input.db, input.connectionId, "auto_bl_bound_group_rule", `group:${target.id}`, detail, "failed", message);
        summary.failedGroupRules += 1;
        continue;
      }
      await logSync(input.db, input.connectionId, "auto_bl_bound_group_rule", `group:${target.id}`, detail, "success");
      summary.appliedGroupRules += 1;
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      await logSync(input.db, input.connectionId, "auto_bl_bound_group_rule", `group:${target.id}`, {
        targetGroupId: target.id,
        targetGroupName: target.name,
        sources,
      }, "failed", message);
      summary.failedGroupRules += 1;
    }
  }

  return { boundGroupIds, boundSourceKeys, summary };
}

async function accountRuleTargets(input: {
  db: RateRuleDb;
  connectionId: number;
  sourceSiteIds?: number[];
}) {
  const allBindings = await input.db.blSourceBinding.findMany({
    where: { connectionId: input.connectionId, targetType: "account" },
    orderBy: [{ targetId: "asc" }, { sourceSiteId: "asc" }, { sourceGroupId: "asc" }],
  });
  const sourceSites = sourceSiteSet(input.sourceSiteIds);
  const boundAccountIds = new Set<number>();
  for (const binding of allBindings) {
    if (!sourceSites || sourceSites.has(binding.sourceSiteId)) {
      boundAccountIds.add(binding.targetId);
    }
  }
  if (boundAccountIds.size === 0) return { allBindings, rules: [] };

  const rules = await input.db.blAccountRateRule.findMany({
    where: { connectionId: input.connectionId, accountId: { in: Array.from(boundAccountIds) }, enabled: true },
  });
  return { allBindings, rules };
}

async function unavailableAccountRuleSummary(input: {
  db: RateRuleDb;
  connectionId: number;
  message: string;
  sourceSiteIds?: number[];
}) {
  const { rules } = await accountRuleTargets(input);
  const summary = emptySummary();
  if (rules.length === 0) return summary;

  await logSync(
    input.db,
    input.connectionId,
    "auto_bl_bound_account_rule",
    `connection:${input.connectionId}`,
    { enabledAccountRules: rules.length, sourceSiteIds: input.sourceSiteIds ?? null },
    "failed",
    input.message,
  );
  summary.failedAccountRules = rules.length;
  return summary;
}

async function applyBoundAccountRules(input: {
  db: RateRuleDb;
  connectionId: number;
  rateClient: CanonicalRateClient;
  s2Client: Sub2ApiAdminClient;
  accounts: unknown[];
  sourceSiteIds?: number[];
  changedSources?: Array<ChangedRateSource | ChangedMonitorRateSource>;
}): Promise<BoundRateSyncSummary> {
  const { allBindings, rules } = await accountRuleTargets(input);
  if (rules.length === 0) {
    return emptySummary();
  }

  const rates = await input.rateClient.fetchRates(undefined, { bypassCache: true });
  const activeMonitorExclusions = await readActiveMonitorRateExclusions({
    db: input.db,
    connectionId: input.connectionId,
  });
  const rulesByAccount = new Map(rules.map((rule) => [rule.accountId, rule]));
  const accountsById = new Map<number, unknown>();
  for (const account of input.accounts) {
    const accountId = getAccountId(account);
    if (accountId) accountsById.set(accountId, account);
  }
  // 双键注册（与分组规则侧一致）：group_id / remote_group_id / model_name 三键兜底匹配。
  const ratesBySource = new Map<string, (typeof rates)[number]>();
  for (const rate of rates) {
    const primary = sourceKey(rate.site_id, rate.group_id);
    if (!ratesBySource.has(primary)) ratesBySource.set(primary, rate);
    if (rate.remote_group_id && rate.remote_group_id !== rate.group_id) {
      const secondary = sourceKey(rate.site_id, rate.remote_group_id);
      if (!ratesBySource.has(secondary)) ratesBySource.set(secondary, rate);
    }
    if (rate.name && rate.name !== rate.group_id) {
      const byName = sourceKey(rate.site_id, rate.name);
      if (!ratesBySource.has(byName)) ratesBySource.set(byName, rate);
    }
  }
  const changedSourceKeys = changedSourceSet(input.changedSources);
  const bindingsByAccount = new Map<number, typeof allBindings>();
  for (const binding of allBindings) {
    const current = bindingsByAccount.get(binding.targetId) ?? [];
    current.push(binding);
    bindingsByAccount.set(binding.targetId, current);
  }

  const summary: BoundRateSyncSummary = emptySummary();

  for (const accountId of rulesByAccount.keys()) {
    const target = accountsById.get(accountId);
    const bindings = bindingsByAccount.get(accountId) ?? [];
    const rule = rulesByAccount.get(accountId);
    if (!target) {
      await Promise.all([
        input.db.blSourceBinding.deleteMany({ where: { connectionId: input.connectionId, targetType: "account", targetId: accountId } }),
        input.db.blAccountRateRule.deleteMany({ where: { connectionId: input.connectionId, accountId } }),
      ]);
      await logSync(input.db, input.connectionId, "auto_bl_bound_account_rule", `account:${accountId}`, {
        accountId,
        deletedInvalidBinding: true,
        deletedInvalidRule: true,
        reason: "target account missing",
        sources: bindings.map((binding) => ({
          sourceSiteId: binding.sourceSiteId,
          sourceSiteName: binding.sourceSiteName,
          sourceGroupId: binding.sourceGroupId,
          sourceGroupName: binding.sourceGroupName ?? "",
        })),
      }, "failed", "No target account found for bound BL rule");
      summary.failedAccountRules += 1;
      continue;
    }
    if (!isAccountEnabledForScheduling(target)) {
      await logSync(input.db, input.connectionId, "auto_bl_bound_account_rule", `account:${accountId}`, {
        accountId,
        preservedDisabledBinding: true,
        preservedDisabledRule: true,
        reason: "target account is not enabled for scheduling",
      }, "success");
      summary.skippedAccountRules += 1;
      continue;
    }
    if (!rule) continue;

    const accountName = getAccountName(target, accountId);
    const sources = bindings.map((binding) => {
      const rate = ratesBySource.get(sourceKey(binding.sourceSiteId, binding.sourceGroupId));
      const monitorExclusions = activeMonitorExclusions.filter((exclusion) => (
        exclusion.accountId === accountId
        && exclusion.sourceSiteId === binding.sourceSiteId
        && exclusion.sourceGroupId === binding.sourceGroupId
      ));
      return {
        sourceSiteId: binding.sourceSiteId,
        sourceSiteName: binding.sourceSiteName,
        sourceGroupId: binding.sourceGroupId,
        sourceGroupName: binding.sourceGroupName ?? "",
        currentRate: resolveCanonicalEffectiveRate(rate),
        rawRate: typeof rate?.rate_multiplier === "number" ? rate.rate_multiplier : null,
        userRate: typeof rate?.user_rate === "number" ? rate.user_rate : null,
        effectiveRate: typeof rate?.effective_rate === "number" ? rate.effective_rate : null,
        rechargeRatio: typeof rate?.recharge_ratio === "number" ? rate.recharge_ratio : null,
        fresh: isCanonicalRateFresh(rate),
        siteEnabled: rate?.site_enabled ?? null,
        lastStatus: rate?.last_status ?? null,
        lastSuccessAt: rate?.last_success_at ?? null,
        collectedAt: rate?.collected_at ?? null,
        monitorExcluded: monitorExclusions.length > 0,
        monitorExclusions: monitorExclusions.map((exclusion) => ({
          accountId: exclusion.accountId,
          accountName: exclusion.accountName,
          reason: exclusion.reason,
        })),
      };
    });

    try {
      const currentAccountRate = getAccountRate(target);
      if (currentAccountRate !== null && sameRate(currentAccountRate, 0)) {
        await logSync(input.db, input.connectionId, "auto_bl_bound_account_rule", `account:${accountId}`, {
          targetAccountId: accountId,
          targetAccountName: accountName,
          skipped: true,
          reason: MANUAL_ZERO_RATE_REASON,
        }, "success");
        summary.skippedAccountRules += 1;
        continue;
      }
      const sourceGate = gateRateRuleSources(rule.mode, sources);
      if (!sourceGate.ok) {
        await logSync(input.db, input.connectionId, "auto_bl_bound_account_rule", `account:${accountId}`, {
          targetAccountId: accountId,
          targetAccountName: accountName,
          rule: { enabled: rule.enabled, mode: rule.mode, offset: rule.offset, expression: rule.expression },
          sources,
          skipped: true,
          reason: sourceGate.reason,
        }, "success");
        summary.skippedAccountRules += 1;
        continue;
      }
      const activeSources = sourceGate.sources;
      const currentRate = currentAccountRate ?? 1;
      const rateMultiplier = evaluateGroupRateRule({
        rule: { enabled: rule.enabled, mode: rule.mode as "first" | "average" | "min" | "max" | "custom" | "locked" | "manual_source", offset: rule.offset, expression: rule.expression },
        sourceRates: activeSources.map((source) => source.currentRate),
        currentRate,
      });
      const latestTarget = await input.s2Client.getAccount(accountId);
      const latestRate = getAccountRate(latestTarget);
      if (latestRate === null) {
        throw new Error(`account ${accountId} detail response is missing rate_multiplier or contains an invalid value`);
      }
      if (sameRate(latestRate, 0)) {
        await logSync(input.db, input.connectionId, "auto_bl_bound_account_rule", `account:${accountId}`, {
          targetAccountId: accountId,
          targetAccountName: accountName,
          skipped: true,
          reason: MANUAL_ZERO_RATE_REASON,
        }, "success");
        summary.skippedAccountRules += 1;
        continue;
      }
      const sourceChanged = Boolean(changedSourceKeys && bindings.some((binding) => changedSourceKeys.has(sourceKey(binding.sourceSiteId, binding.sourceGroupId))));
      const detail = {
        targetAccountId: accountId,
        targetAccountName: accountName,
        rule: { enabled: rule.enabled, mode: rule.mode, offset: rule.offset, expression: rule.expression },
        sources,
        sourceSelection: {
          total: sources.length,
          used: activeSources.length,
          skipped: sourceGate.skippedSources.length,
          notice: sourceGate.notice,
        },
        currentRate: latestRate,
        rateMultiplier,
        sourceChanged,
      };

      if (sameRate(latestRate, rateMultiplier) && !sourceChanged) {
        if (changedSourceKeys && changedSourceKeys.size > 0) {
          await logSync(input.db, input.connectionId, "auto_bl_bound_account_rule", `account:${accountId}`, {
            ...detail,
            skipped: true,
            reason: "账号倍率已是规则计算结果，且本次采集变更源未命中该账号绑定源",
          }, "success");
        }
        summary.skippedAccountRules += 1;
        continue;
      }

      await input.s2Client.updateAccountRateMultiplier(accountId, rateMultiplier);
      await logSync(input.db, input.connectionId, "auto_bl_bound_account_rule", `account:${accountId}`, detail, "success");
      summary.appliedAccountRules += 1;
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      await logSync(input.db, input.connectionId, "auto_bl_bound_account_rule", `account:${accountId}`, {
        targetAccountId: accountId,
        targetAccountName: accountName,
        sources,
      }, "failed", message);
      summary.failedAccountRules += 1;
    }
  }

  return summary;
}

async function applyBoundAccountPriorityRule(input: {
  db: RateRuleDb;
  connectionId: number;
  s2Client: Sub2ApiAdminClient;
  groups: Sub2ApiGroup[];
  accounts?: unknown[];
  refreshAccounts: boolean;
}) {
  const summary = emptySummary();

  try {
    const accounts = input.refreshAccounts ? undefined : input.accounts;
    const result = await applyAccountPriorityRule({
      db: input.db,
      connectionId: input.connectionId,
      s2Client: input.s2Client,
      groups: input.groups,
      accounts,
      action: "auto_account_priority_rule",
    });

    if (!result.enabled) return summary;
    summary.appliedPriorityRules = result.updated;
    summary.skippedPriorityRules = result.unchanged + result.skippedAccounts;
    summary.failedPriorityRules = result.failed;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    await logSync(
      input.db,
      input.connectionId,
      "auto_account_priority_rule",
      `connection:${input.connectionId}`,
      { refreshAccounts: input.refreshAccounts },
      "failed",
      message,
    );
    summary.failedPriorityRules = 1;
  }

  return summary;
}

function mergeSummaries(...summaries: BoundRateSyncSummary[]): BoundRateSyncSummary {
  return summaries.reduce((acc, summary) => ({
    appliedGroupRules: acc.appliedGroupRules + summary.appliedGroupRules,
    appliedAccountRules: acc.appliedAccountRules + summary.appliedAccountRules,
    appliedPriorityRules: acc.appliedPriorityRules + summary.appliedPriorityRules,
    skippedGroupRules: acc.skippedGroupRules + summary.skippedGroupRules,
    skippedAccountRules: acc.skippedAccountRules + summary.skippedAccountRules,
    skippedPriorityRules: acc.skippedPriorityRules + summary.skippedPriorityRules,
    failedGroupRules: acc.failedGroupRules + summary.failedGroupRules,
    failedAccountRules: acc.failedAccountRules + summary.failedAccountRules,
    failedPriorityRules: acc.failedPriorityRules + summary.failedPriorityRules,
  }), emptySummary());
}

export async function applyBoundRateRules(input: {
  db: RateRuleDb;
  connectionId: number;
  connectionName?: string | null;
  rateClient: CanonicalRateClient;
  s2Client: Sub2ApiAdminClient;
  groups: Sub2ApiGroup[];
  accounts?: unknown[];
  accountsError?: string | null;
  sourceSiteIds?: number[];
  changedSources?: Array<ChangedRateSource | ChangedMonitorRateSource>;
  groupsError?: string | null;
  targetGroupIds?: number[];
  includeAccountRules?: boolean;
  includePriorityRules?: boolean;
}) {
  const groupResult = input.groupsError
    ? await unavailableGroupRuleSummary({
        db: input.db,
        connectionId: input.connectionId,
        message: input.groupsError,
        sourceSiteIds: input.sourceSiteIds,
        targetGroupIds: input.targetGroupIds,
      })
    : await applyBoundGroupRules({
        db: input.db,
        connectionId: input.connectionId,
        connectionName: input.connectionName,
        rateClient: input.rateClient,
        s2Client: input.s2Client,
        groups: input.groups,
        accounts: input.accounts,
        accountsError: input.accountsError,
        sourceSiteIds: input.sourceSiteIds,
        targetGroupIds: input.targetGroupIds,
      });
  const includeAccountRules = input.includeAccountRules ?? true;
  const includePriorityRules = input.includePriorityRules ?? false;
  const accountSummary = !includeAccountRules
    ? emptySummary()
    : input.accounts
    ? await applyBoundAccountRules({
        db: input.db,
        connectionId: input.connectionId,
        rateClient: input.rateClient,
        s2Client: input.s2Client,
        accounts: input.accounts,
        sourceSiteIds: input.sourceSiteIds,
        changedSources: input.changedSources,
      })
    : input.accountsError
      ? await unavailableAccountRuleSummary({
          db: input.db,
          connectionId: input.connectionId,
          message: input.accountsError,
          sourceSiteIds: input.sourceSiteIds,
        })
      : emptySummary();
  const prioritySummary = includePriorityRules
    ? await applyBoundAccountPriorityRule({
        db: input.db,
        connectionId: input.connectionId,
        s2Client: input.s2Client,
        groups: input.groups,
        accounts: input.accounts,
        refreshAccounts: accountSummary.appliedAccountRules > 0,
      })
    : emptySummary();

  return {
    boundGroupIds: groupResult.boundGroupIds,
    boundSourceKeys: groupResult.boundSourceKeys,
    summary: mergeSummaries(groupResult.summary, accountSummary, prioritySummary),
  };
}

export async function applyBoundRateRulesForConnection(input: {
  db: LockableRateSyncDb;
  connectionId: number;
  sourceSiteIds?: number[];
  changedSources?: Array<ChangedRateSource | ChangedMonitorRateSource>;
  targetGroupIds?: number[];
  includeAccountRules?: boolean;
  includePriorityRules?: boolean;
  cleanupBindings?: boolean;
}) {
  return withConnectionLock(input.db, input.connectionId, async (tx) => {
    let target;
    try {
      target = await resolveCanonicalTarget(tx, input.connectionId);
    } catch (error) {
      if (!(error instanceof CanonicalTargetResolutionError)) throw error;
      return {
        ok: false,
        skipped: true,
        message: error.message,
        boundGroupIds: new Set<number>(),
        boundSourceKeys: new Set<string>(),
        summary: emptySummary(),
      };
    }

    const rateClient = new CanonicalRateClient();
    const s2Client = new Sub2ApiAdminClient(target.baseUrl, target.adminApiKey);
    let groups: Sub2ApiGroup[] | undefined;
    let groupsError: string | null = null;
    try {
      groups = await s2Client.listGroups();
    } catch (error) {
      groupsError = error instanceof Error ? error.message : String(error);
    }
    let accounts: unknown[] | undefined;
    let accountsError: string | null = null;
    try {
      accounts = await s2Client.listAccounts();
    } catch (error) {
      accountsError = error instanceof Error ? error.message : String(error);
    }
    const changedSources = input.changedSources;
    if (input.cleanupBindings !== false) {
      await cleanupSourceBindings({
        db: tx,
        connectionId: input.connectionId,
        rateClient,
        sourceSiteIds: input.sourceSiteIds,
      });
    }
    const result = await applyBoundRateRules({
      db: tx,
      connectionId: input.connectionId,
      connectionName: target.name,
      rateClient,
      s2Client,
      groups: groups ?? [],
      groupsError,
      accounts,
      accountsError,
      sourceSiteIds: input.sourceSiteIds,
      changedSources,
      targetGroupIds: input.targetGroupIds,
      includeAccountRules: input.includeAccountRules,
      includePriorityRules: input.includePriorityRules,
    });

    const { failedGroupRules, failedAccountRules, failedPriorityRules } = result.summary;
    const failed = failedGroupRules + failedAccountRules + failedPriorityRules;
    return {
      ok: failed === 0,
      skipped: false,
      message: failed === 0
        ? "rate rules applied"
        : `rate rules applied with ${failed} failures (group=${failedGroupRules}, account=${failedAccountRules}, priority=${failedPriorityRules})`,
      ...result,
    };
  });
}
