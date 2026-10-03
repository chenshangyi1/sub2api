import { Prisma } from "@prisma/client";
import { sub2apiDb } from "@/server/sub2api-usage-stats";

export type AccountPoolBurstPolicy = {
  smartExpansionEnabled: boolean;
  burstExpansionEnabled: boolean;
  maxAccountConcurrency: number;
  expansionLoadThresholdPct: number;
  burstRpmThreshold: number;
  burstTotalConcurrency: number;
  burstMaxAccountConcurrency: number;
  burstStepPct: number;
  burstScaleDownThresholdPct: number;
  burstCooldownSeconds: number;
};

export type BurstAccount = {
  id: number;
  schedulable: boolean;
  concurrency: number;
  currentConcurrency: number;
};

export type BurstTrafficSample = {
  requestsPerMinute: number;
  completedRequestsLastMinute: number;
  accountRequestsPerMinute: Record<string, number>;
  degradedAccountIds: number[];
};

export type AccountPoolBurstState =
  | "disabled"
  | "waiting_for_healthy"
  | "waiting_for_rpm"
  | "waiting_for_saturation"
  | "expanding"
  | "holding"
  | "cooling_down"
  | "shrinking"
  | "hard_limit_reached"
  | "failed";

export type AccountPoolBurstEvaluation = {
  state: AccountPoolBurstState;
  active: boolean;
  requestsPerMinute: number;
  completedRequestsLastMinute: number;
  rpmThreshold: number;
  currentLoad: number;
  currentCapacity: number;
  peakUtilizationPct: number;
  thresholdPct: number;
  regularAccountLimit: number;
  burstAccountLimit: number;
  burstTotalLimit: number;
  eligibleAccounts: number;
  degradedAccounts: number;
  cooldownRemainingSeconds: number;
  lastHighLoadAt: string | null;
  lastCheckedAt: string;
  updatedAccounts: number;
  failedWrites: number;
  platforms?: Record<string, AccountPoolBurstEvaluation>;
  managedConcurrencyBaselines?: Record<string, number>;
  managedConcurrencyTokens?: Record<string, string>;
};

export type BurstConcurrencyUpdate = {
  accountId: number;
  concurrency: number;
  direction: "expand" | "shrink";
};

type RawBurstTraffic = {
  account_id: bigint | number;
  rpm_60: bigint | number;
  rpm_30_scaled: bigint | number;
  rpm_10_scaled: bigint | number;
};

type RawDegradedAccount = {
  account_id: bigint | number;
};

function numeric(value: unknown) {
  const result = Number(value);
  return Number.isFinite(result) ? result : 0;
}

function utilizationPct(account: BurstAccount) {
  if (account.concurrency <= 0) return account.currentConcurrency > 0 ? 100 : 0;
  return account.currentConcurrency / account.concurrency * 100;
}

function validDate(value: string | null | undefined) {
  if (!value) return null;
  const date = new Date(value);
  return Number.isFinite(date.getTime()) ? date : null;
}

export function summarizeBurstTrafficRows(trafficRows: RawBurstTraffic[]) {
  const accountRequestsPerMinute: Record<string, number> = {};
  let rpm60Total = 0;
  let rpm30ScaledTotal = 0;
  let rpm10ScaledTotal = 0;
  for (const row of trafficRows) {
    const rpm60 = numeric(row.rpm_60);
    const rpm30Scaled = numeric(row.rpm_30_scaled);
    const rpm10Scaled = numeric(row.rpm_10_scaled);
    accountRequestsPerMinute[String(numeric(row.account_id))] = Math.max(rpm60, rpm30Scaled, rpm10Scaled);
    rpm60Total += rpm60;
    rpm30ScaledTotal += rpm30Scaled;
    rpm10ScaledTotal += rpm10Scaled;
  }
  return {
    requestsPerMinute: Math.max(rpm60Total, rpm30ScaledTotal, rpm10ScaledTotal),
    completedRequestsLastMinute: rpm60Total,
    accountRequestsPerMinute,
  };
}

export async function getSub2ApiBurstTraffic(accountIds: number[]): Promise<BurstTrafficSample> {
  const ids = Array.from(new Set(accountIds.filter((id) => Number.isInteger(id) && id > 0)));
  const db = sub2apiDb();
  const trafficPromise = db.$queryRaw<RawBurstTraffic[]>(Prisma.sql`
    SELECT
      account_id,
      count(*) FILTER (WHERE created_at >= now() - interval '60 seconds')::bigint AS rpm_60,
      (count(*) FILTER (WHERE created_at >= now() - interval '30 seconds') * 2)::bigint AS rpm_30_scaled,
      (count(*) FILTER (WHERE created_at >= now() - interval '10 seconds') * 6)::bigint AS rpm_10_scaled
    FROM usage_logs
    WHERE created_at >= now() - interval '60 seconds'
    GROUP BY account_id
  `);
  const degradedPromise = ids.length === 0
    ? Promise.resolve([] as RawDegradedAccount[])
    : db.$queryRaw<RawDegradedAccount[]>(Prisma.sql`
      SELECT account_id
      FROM ops_error_logs
      WHERE created_at >= now() - interval '2 minutes'
        AND account_id IN (${Prisma.join(ids)})
        AND COALESCE(upstream_status_code, status_code) IN (502,503,504,520,521,522,523,524)
        AND lower(COALESCE(error_type, '')) NOT IN ('policy','cyber_policy','session_blocked_by_cyber_policy')
        AND lower(COALESCE(error_type, '')) NOT LIKE '%cyber_policy%'
      GROUP BY account_id
      HAVING count(*) >= 3
    `);
  const [trafficRows, degradedRows] = await Promise.all([trafficPromise, degradedPromise]);
  const traffic = summarizeBurstTrafficRows(trafficRows);
  return {
    ...traffic,
    degradedAccountIds: degradedRows.map((row) => numeric(row.account_id)).filter((id) => id > 0),
  };
}

export function planBurstConcurrency(input: {
  accounts: BurstAccount[];
  eligibleAccountIds: number[];
  traffic: BurstTrafficSample;
  policy: AccountPoolBurstPolicy;
  previous?: AccountPoolBurstEvaluation | null;
  now: Date;
}) {
  const { accounts, traffic, policy, now } = input;
  const eligibleIds = new Set(input.eligibleAccountIds);
  const degradedIds = new Set(traffic.degradedAccountIds);
  const accountIds = new Set(accounts.map((account) => account.id));
  const managedConcurrencyBaselines: Record<string, number> = Object.fromEntries(Object.entries(
    input.previous?.managedConcurrencyBaselines ?? {},
  ).flatMap(([rawId, rawBaseline]) => {
    const id = Number(rawId);
    const baseline = Number(rawBaseline);
    const account = accounts.find((candidate) => candidate.id === id);
    return Number.isInteger(id)
      && id > 0
      && accountIds.has(id)
      && Number.isFinite(baseline)
      && baseline > 0
      && account
      && account.concurrency > baseline
      ? [[String(id), baseline]]
      : [];
  }));
  const managedIds = new Set(Object.keys(managedConcurrencyBaselines).map(Number));
  const controlledAccounts = accounts.filter((account) => (
    eligibleIds.has(account.id) || managedIds.has(account.id)
  ));
  const eligibleAccounts = accounts.filter((account) => (
    account.schedulable && eligibleIds.has(account.id) && !degradedIds.has(account.id)
  ));
  const expandableAccounts = eligibleAccounts.filter((account) => (
    account.concurrency <= policy.maxAccountConcurrency || managedIds.has(account.id)
  ));
  const burstAccounts = controlledAccounts.filter((account) => managedIds.has(account.id));
  const currentLoad = controlledAccounts.reduce((sum, account) => sum + account.currentConcurrency, 0);
  const currentCapacity = controlledAccounts.reduce((sum, account) => sum + account.concurrency, 0);
  const peakUtilizationPct = expandableAccounts.reduce((peak, account) => Math.max(peak, utilizationPct(account)), 0);
  const thresholdPct = policy.expansionLoadThresholdPct;
  const highRpm = traffic.requestsPerMinute >= policy.burstRpmThreshold;
  const emergencySaturation = peakUtilizationPct >= Math.max(95, thresholdPct);
  const expansionTriggered = highRpm || emergencySaturation;
  const saturatedAccounts = expandableAccounts
    .filter((account) => utilizationPct(account) >= thresholdPct)
    .sort((left, right) => utilizationPct(right) - utilizationPct(left) || left.id - right.id);
  const updates: BurstConcurrencyUpdate[] = [];
  let plannedCapacity = currentCapacity;
  let hardLimitReached = false;
  let lastHighLoadAt = validDate(input.previous?.lastHighLoadAt)?.toISOString() ?? null;
  let state: AccountPoolBurstState = "waiting_for_rpm";

  if (!policy.smartExpansionEnabled || !policy.burstExpansionEnabled) {
    state = "disabled";
  } else if (expansionTriggered && saturatedAccounts.length > 0) {
    lastHighLoadAt = now.toISOString();
    for (const account of saturatedAccounts) {
      const ceiling = account.concurrency >= policy.maxAccountConcurrency
        ? policy.burstMaxAccountConcurrency
        : policy.maxAccountConcurrency;
      if (account.concurrency >= ceiling) {
        hardLimitReached = true;
        continue;
      }
      const capacityRemaining = policy.burstTotalConcurrency - plannedCapacity;
      if (capacityRemaining <= 0) {
        hardLimitReached = true;
        break;
      }
      const stepLimit = account.concurrency + Math.max(1, Math.ceil(account.concurrency * policy.burstStepPct / 100));
      const targetUtilization = Math.max(50, thresholdPct - 10) / 100;
      const demandTarget = Math.ceil(account.currentConcurrency / targetUtilization);
      const target = Math.min(ceiling, account.concurrency + capacityRemaining, Math.max(account.concurrency + 1, Math.min(stepLimit, demandTarget)));
      if (target <= account.concurrency) continue;
      updates.push({ accountId: account.id, concurrency: target, direction: "expand" });
      if (target > policy.maxAccountConcurrency && managedConcurrencyBaselines[String(account.id)] === undefined) {
        managedConcurrencyBaselines[String(account.id)] = account.concurrency;
        managedIds.add(account.id);
      }
      plannedCapacity += target - account.concurrency;
    }
    state = updates.length > 0 ? "expanding" : hardLimitReached ? "hard_limit_reached" : "holding";
  } else if (burstAccounts.length > 0) {
    const scaleDownThreshold = policy.burstScaleDownThresholdPct;
    const lowRpm = traffic.requestsPerMinute < policy.burstRpmThreshold * scaleDownThreshold / 100;
    const lowLoad = burstAccounts.every((account) => utilizationPct(account) < scaleDownThreshold);
    if (!lowRpm || !lowLoad) {
      lastHighLoadAt = now.toISOString();
      state = "holding";
    } else {
      const highLoadAt = validDate(lastHighLoadAt) ?? now;
      if (!lastHighLoadAt) lastHighLoadAt = highLoadAt.toISOString();
      const cooldownRemainingMs = policy.burstCooldownSeconds * 1000 - (now.getTime() - highLoadAt.getTime());
      if (cooldownRemainingMs > 0) {
        state = "cooling_down";
      } else {
        for (const account of burstAccounts) {
          const baseline = managedConcurrencyBaselines[String(account.id)];
          if (baseline === undefined) continue;
          const stepTarget = account.concurrency - Math.max(1, Math.ceil(account.concurrency * policy.burstStepPct / 100));
          const inFlightFloor = account.currentConcurrency + Math.max(1, Math.ceil(account.currentConcurrency * 0.1));
          const target = Math.max(baseline, inFlightFloor, stepTarget);
          if (target >= account.concurrency) continue;
          updates.push({ accountId: account.id, concurrency: target, direction: "shrink" });
          if (target <= baseline) {
            delete managedConcurrencyBaselines[String(account.id)];
            managedIds.delete(account.id);
          }
          plannedCapacity -= account.concurrency - target;
        }
        state = updates.length > 0 ? "shrinking" : "holding";
      }
    }
  } else if (eligibleAccounts.length === 0) {
    state = "waiting_for_healthy";
  } else if (saturatedAccounts.length === 0) {
    state = highRpm ? "waiting_for_saturation" : "waiting_for_rpm";
  }

  const nextConcurrency = new Map(updates.map((update) => [update.accountId, update.concurrency]));
  const active = controlledAccounts.some((account) => {
    const baseline = managedConcurrencyBaselines[String(account.id)];
    return baseline !== undefined && (nextConcurrency.get(account.id) ?? account.concurrency) > baseline;
  });
  const highLoadAt = validDate(lastHighLoadAt);
  const cooldownRemainingSeconds = state === "cooling_down" && highLoadAt
    ? Math.max(0, Math.ceil((policy.burstCooldownSeconds * 1000 - (now.getTime() - highLoadAt.getTime())) / 1000))
    : 0;
  const evaluation: AccountPoolBurstEvaluation = {
    state,
    active,
    requestsPerMinute: traffic.requestsPerMinute,
    completedRequestsLastMinute: traffic.completedRequestsLastMinute,
    rpmThreshold: policy.burstRpmThreshold,
    currentLoad,
    currentCapacity: plannedCapacity,
    peakUtilizationPct: Math.round(peakUtilizationPct * 10) / 10,
    thresholdPct,
    regularAccountLimit: policy.maxAccountConcurrency,
    burstAccountLimit: policy.burstMaxAccountConcurrency,
    burstTotalLimit: policy.burstTotalConcurrency,
    eligibleAccounts: eligibleAccounts.length,
    degradedAccounts: degradedIds.size,
    cooldownRemainingSeconds,
    lastHighLoadAt,
    lastCheckedAt: now.toISOString(),
    updatedAccounts: updates.length,
    failedWrites: 0,
    managedConcurrencyBaselines,
    managedConcurrencyTokens: { ...(input.previous?.managedConcurrencyTokens ?? {}) },
  };
  return { updates, evaluation };
}
