import type { PrismaClient } from "@prisma/client";

type RateExclusionDb = Pick<PrismaClient, "blSourceBinding" | "upstreamMonitorRateExclusion" | "upstreamMonitorRule">;

export type ChangedMonitorRateSource = {
  sourceSiteId: number;
  sourceGroupId: string;
};

export type MonitorRateExclusion = {
  accountId: number;
  accountName: string | null;
  groupId: number;
  sourceSiteId: number;
  sourceSiteName: string;
  sourceGroupId: string;
  sourceGroupName: string | null;
  sourcePlatform: string | null;
  reason: string | null;
  pausedAt: Date;
};

export type MonitorRateExclusionSyncResult = {
  affectedGroupIds: number[];
  changedSources: ChangedMonitorRateSource[];
  sourceSiteIds: number[];
  count: number;
  exclusions: MonitorRateExclusion[];
};

export function monitorRateSourceKey(sourceSiteId: number, sourceGroupId: string) {
  return `${sourceSiteId}:${sourceGroupId}`;
}

export function monitorGroupRateExclusionKey(groupId: number, sourceSiteId: number, sourceGroupId: string) {
  return `${groupId}:${monitorRateSourceKey(sourceSiteId, sourceGroupId)}`;
}

function uniqueNumbers(values: number[]) {
  return Array.from(new Set(values.filter((value) => Number.isInteger(value) && value > 0)));
}

function uniqueChangedSources(values: ChangedMonitorRateSource[]) {
  return Array.from(
    new Map(values.map((source) => [monitorRateSourceKey(source.sourceSiteId, source.sourceGroupId), source])).values(),
  );
}

function monitorRateExclusionRowKey(exclusion: MonitorRateExclusion) {
  return `${exclusion.accountId}:${monitorGroupRateExclusionKey(exclusion.groupId, exclusion.sourceSiteId, exclusion.sourceGroupId)}`;
}

function resultFromExclusions(exclusions: MonitorRateExclusion[]): MonitorRateExclusionSyncResult {
  return {
    affectedGroupIds: uniqueNumbers(exclusions.map((row) => row.groupId)),
    changedSources: uniqueChangedSources(exclusions.map((row) => ({
      sourceSiteId: row.sourceSiteId,
      sourceGroupId: row.sourceGroupId,
    }))),
    sourceSiteIds: uniqueNumbers(exclusions.map((row) => row.sourceSiteId)),
    count: exclusions.length,
    exclusions,
  };
}

async function readPausedRuleDerivedExclusions(input: {
  db: RateExclusionDb;
  connectionId: number;
  groupIds: number[];
  now: Date;
}) {
  const rules = await input.db.upstreamMonitorRule.findMany({
    where: {
      connectionId: input.connectionId,
      pausedUntil: { gt: input.now },
    },
    select: {
      accountId: true,
      accountName: true,
      pauseStartedAt: true,
      updatedAt: true,
    },
    orderBy: [{ accountId: "asc" }],
  });
  if (rules.length === 0) return [];

  const rulesByAccount = new Map(rules.map((rule) => [rule.accountId, rule]));
  const accountIds = uniqueNumbers(rules.map((rule) => rule.accountId));
  const accountBindings = await input.db.blSourceBinding.findMany({
    where: {
      connectionId: input.connectionId,
      targetType: "account",
      targetId: { in: accountIds },
    },
    orderBy: [{ targetId: "asc" }, { sourceSiteId: "asc" }, { sourceGroupId: "asc" }],
  });
  if (accountBindings.length === 0) return [];

  const accountSourceKeys = new Set(accountBindings.map((binding) => monitorRateSourceKey(binding.sourceSiteId, binding.sourceGroupId)));
  const sourceSiteIds = uniqueNumbers(accountBindings.map((binding) => binding.sourceSiteId));
  const groupBindings = await input.db.blSourceBinding.findMany({
    where: {
      connectionId: input.connectionId,
      targetType: "group",
      sourceSiteId: { in: sourceSiteIds },
      ...(input.groupIds.length > 0 ? { targetId: { in: input.groupIds } } : {}),
    },
    orderBy: [{ targetId: "asc" }, { sourceSiteId: "asc" }, { sourceGroupId: "asc" }],
  });
  if (groupBindings.length === 0) return [];

  const groupBindingsBySource = new Map<string, typeof groupBindings>();
  for (const binding of groupBindings) {
    const key = monitorRateSourceKey(binding.sourceSiteId, binding.sourceGroupId);
    if (!accountSourceKeys.has(key)) continue;
    const current = groupBindingsBySource.get(key) ?? [];
    current.push(binding);
    groupBindingsBySource.set(key, current);
  }
  if (groupBindingsBySource.size === 0) return [];

  const exclusions: MonitorRateExclusion[] = [];
  const seen = new Set<string>();
  for (const accountBinding of accountBindings) {
    const rule = rulesByAccount.get(accountBinding.targetId);
    if (!rule) continue;

    const matchingGroupBindings = groupBindingsBySource.get(monitorRateSourceKey(accountBinding.sourceSiteId, accountBinding.sourceGroupId)) ?? [];
    for (const groupBinding of matchingGroupBindings) {
      const exclusion: MonitorRateExclusion = {
        accountId: rule.accountId,
        accountName: rule.accountName,
        groupId: groupBinding.targetId,
        sourceSiteId: groupBinding.sourceSiteId,
        sourceSiteName: groupBinding.sourceSiteName,
        sourceGroupId: groupBinding.sourceGroupId,
        sourceGroupName: groupBinding.sourceGroupName,
        sourcePlatform: groupBinding.sourcePlatform,
        reason: "upstream_monitor_pause",
        pausedAt: rule.pauseStartedAt ?? rule.updatedAt ?? input.now,
      };
      const key = monitorRateExclusionRowKey(exclusion);
      if (seen.has(key)) continue;
      seen.add(key);
      exclusions.push(exclusion);
    }
  }

  return exclusions;
}

export async function readActiveMonitorRateExclusions(input: {
  db: RateExclusionDb;
  connectionId: number;
  groupIds?: number[];
  now?: Date;
}) {
  const groupIds = uniqueNumbers(input.groupIds ?? []);
  const now = input.now ?? new Date();
  const [rows, derivedExclusions] = await Promise.all([
    input.db.upstreamMonitorRateExclusion.findMany({
      where: {
        connectionId: input.connectionId,
        active: true,
        ...(groupIds.length > 0 ? { groupId: { in: groupIds } } : {}),
      },
      orderBy: [{ groupId: "asc" }, { sourceSiteId: "asc" }, { sourceGroupId: "asc" }, { accountId: "asc" }],
    }),
    readPausedRuleDerivedExclusions({ db: input.db, connectionId: input.connectionId, groupIds, now }),
  ]);

  const persistedExclusions = rows.map<MonitorRateExclusion>((row) => ({
    accountId: row.accountId,
    accountName: row.accountName,
    groupId: row.groupId,
    sourceSiteId: row.sourceSiteId,
    sourceSiteName: row.sourceSiteName,
    sourceGroupId: row.sourceGroupId,
    sourceGroupName: row.sourceGroupName,
    sourcePlatform: row.sourcePlatform,
    reason: row.reason,
    pausedAt: row.pausedAt,
  }));
  const exclusionsByKey = new Map<string, MonitorRateExclusion>();
  for (const exclusion of [...persistedExclusions, ...derivedExclusions]) {
    const key = monitorRateExclusionRowKey(exclusion);
    if (!exclusionsByKey.has(key)) exclusionsByKey.set(key, exclusion);
  }

  return Array.from(exclusionsByKey.values()).sort((left, right) => (
    left.groupId - right.groupId
    || left.sourceSiteId - right.sourceSiteId
    || left.sourceGroupId.localeCompare(right.sourceGroupId)
    || left.accountId - right.accountId
  ));
}

export function groupMonitorRateExclusionsBySource(exclusions: MonitorRateExclusion[]) {
  const map = new Map<string, MonitorRateExclusion[]>();
  for (const exclusion of exclusions) {
    const key = monitorGroupRateExclusionKey(exclusion.groupId, exclusion.sourceSiteId, exclusion.sourceGroupId);
    const current = map.get(key) ?? [];
    current.push(exclusion);
    map.set(key, current);
  }
  return map;
}

export async function pauseAccountMonitorRateSources(input: {
  db: RateExclusionDb;
  connectionId: number;
  accountId: number;
  accountName?: string | null;
  reason?: string | null;
  pausedAt?: Date;
}) {
  const accountBindings = await input.db.blSourceBinding.findMany({
    where: {
      connectionId: input.connectionId,
      targetType: "account",
      targetId: input.accountId,
    },
    orderBy: [{ sourceSiteId: "asc" }, { sourceGroupId: "asc" }],
  });
  if (accountBindings.length === 0) return resultFromExclusions([]);

  const accountSourceKeys = new Set(accountBindings.map((binding) => monitorRateSourceKey(binding.sourceSiteId, binding.sourceGroupId)));
  const sourceSiteIds = uniqueNumbers(accountBindings.map((binding) => binding.sourceSiteId));
  const groupBindings = await input.db.blSourceBinding.findMany({
    where: {
      connectionId: input.connectionId,
      targetType: "group",
      sourceSiteId: { in: sourceSiteIds },
    },
    orderBy: [{ targetId: "asc" }, { sourceSiteId: "asc" }, { sourceGroupId: "asc" }],
  });
  const matchedBindings = groupBindings.filter((binding) => accountSourceKeys.has(monitorRateSourceKey(binding.sourceSiteId, binding.sourceGroupId)));
  if (matchedBindings.length === 0) return resultFromExclusions([]);

  const pausedAt = input.pausedAt ?? new Date();
  const reason = input.reason ?? null;
  const accountName = input.accountName ?? null;

  await Promise.all(matchedBindings.map((binding) => (
    input.db.upstreamMonitorRateExclusion.upsert({
      where: {
        connectionId_accountId_groupId_sourceSiteId_sourceGroupId: {
          connectionId: input.connectionId,
          accountId: input.accountId,
          groupId: binding.targetId,
          sourceSiteId: binding.sourceSiteId,
          sourceGroupId: binding.sourceGroupId,
        },
      },
      create: {
        connectionId: input.connectionId,
        accountId: input.accountId,
        accountName,
        groupId: binding.targetId,
        sourceSiteId: binding.sourceSiteId,
        sourceSiteName: binding.sourceSiteName,
        sourceGroupId: binding.sourceGroupId,
        sourceGroupName: binding.sourceGroupName,
        sourcePlatform: binding.sourcePlatform,
        reason,
        active: true,
        pausedAt,
      },
      update: {
        accountName,
        sourceSiteName: binding.sourceSiteName,
        sourceGroupName: binding.sourceGroupName,
        sourcePlatform: binding.sourcePlatform,
        reason,
        active: true,
        pausedAt,
        restoredAt: null,
      },
    })
  )));

  const exclusions = matchedBindings.map<MonitorRateExclusion>((binding) => ({
    accountId: input.accountId,
    accountName,
    groupId: binding.targetId,
    sourceSiteId: binding.sourceSiteId,
    sourceSiteName: binding.sourceSiteName,
    sourceGroupId: binding.sourceGroupId,
    sourceGroupName: binding.sourceGroupName,
    sourcePlatform: binding.sourcePlatform,
    reason,
    pausedAt,
  }));

  return resultFromExclusions(exclusions);
}

export async function restoreAccountMonitorRateSources(input: {
  db: RateExclusionDb;
  connectionId: number;
  accountId: number;
  restoredAt?: Date;
}) {
  const activeExclusions = (await readActiveMonitorRateExclusions({
    db: input.db,
    connectionId: input.connectionId,
  })).filter((exclusion) => exclusion.accountId === input.accountId);
  const rows = await input.db.upstreamMonitorRateExclusion.findMany({
    where: {
      connectionId: input.connectionId,
      accountId: input.accountId,
      active: true,
    },
    orderBy: [{ groupId: "asc" }, { sourceSiteId: "asc" }, { sourceGroupId: "asc" }],
  });

  const restoredAt = input.restoredAt ?? new Date();
  if (rows.length > 0) {
    await input.db.upstreamMonitorRateExclusion.updateMany({
      where: { id: { in: rows.map((row) => row.id) } },
      data: {
        active: false,
        restoredAt,
      },
    });
  }

  if (activeExclusions.length > 0) return resultFromExclusions(activeExclusions);

  return resultFromExclusions(rows.map<MonitorRateExclusion>((row) => ({
    accountId: row.accountId,
    accountName: row.accountName,
    groupId: row.groupId,
    sourceSiteId: row.sourceSiteId,
    sourceSiteName: row.sourceSiteName,
    sourceGroupId: row.sourceGroupId,
    sourceGroupName: row.sourceGroupName,
    sourcePlatform: row.sourcePlatform,
    reason: row.reason,
    pausedAt: row.pausedAt,
  })));
}
