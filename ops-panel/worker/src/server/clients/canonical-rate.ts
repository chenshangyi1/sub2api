import { Prisma } from "@prisma/client";
import { db } from "@/server/db";
import { BoundedPromiseCache } from "@/server/promise-cache";
import { normalizeRateMultiplier } from "@/shared/rates";

export type CanonicalRate = {
  site_id: number;
  site_name: string;
  site_enabled: boolean;
  site_interval_min: number;
  last_status: string;
  last_success_at: string | null;
  collected_at: string;
  group_id: string;
  name: string;
  platform: string | null;
  recharge_ratio: number;
  rate_multiplier: number;
  user_rate: number;
  effective_rate: number;
  actual_rate_multiplier: number;
  actual_user_rate: number;
  actual_effective_rate: number;
  /** 上游站点的原始组 ID（快照 remote_group_id），可能与 group_id（COALESCE 兜底）不同 */
  remote_group_id: string | null;
};

type CanonicalRateRow = {
  site_id: bigint | number;
  site_name: string;
  site_enabled: boolean;
  site_interval_min: number;
  last_status: string;
  last_success_at: Date | null;
  collected_at: Date;
  group_id: string;
  name: string;
  platform: string | null;
  remote_group_id: string | null;
  rate: number;
};

export function resolveCanonicalEffectiveRate(
  rate: Pick<CanonicalRate, "actual_effective_rate" | "effective_rate"> | null | undefined,
) {
  const value = rate?.actual_effective_rate ?? rate?.effective_rate;
  return typeof value === "number" && Number.isFinite(value) ? normalizeRateMultiplier(value) : null;
}

const MIN_SOURCE_FRESHNESS_MINUTES = 15;

export function isCanonicalRateFresh(rate: CanonicalRate | null | undefined, nowMs = Date.now()) {
  if (!rate?.site_enabled || rate.last_status !== "online") return false;
  const lastSuccessMs = Date.parse(rate.last_success_at || "");
  if (!Number.isFinite(lastSuccessMs)) return false;
  const maxAgeMinutes = Math.max(
    MIN_SOURCE_FRESHNESS_MINUTES,
    (Number.isFinite(rate.site_interval_min) && rate.site_interval_min > 0 ? rate.site_interval_min : 60) * 2,
  );
  const ageMs = nowMs - lastSuccessMs;
  return ageMs >= -60_000 && ageMs <= maxAgeMinutes * 60_000;
}

const RATES_CACHE_TTL_MS = 5_000;
const ratesCache = new BoundedPromiseCache<string, CanonicalRate[]>(RATES_CACHE_TTL_MS);

function toCanonicalRate(row: CanonicalRateRow): CanonicalRate {
  const siteID = Number(row.site_id);
  if (!Number.isSafeInteger(siteID) || siteID <= 0) {
    throw new Error(`canonical rate source id is invalid: ${String(row.site_id)}`);
  }
  const rate = normalizeRateMultiplier(Number(row.rate));
  return {
    site_id: siteID,
    site_name: row.site_name,
    site_enabled: row.site_enabled,
    site_interval_min: row.site_interval_min,
    last_status: row.last_status,
    last_success_at: row.last_success_at?.toISOString() ?? null,
    collected_at: row.collected_at.toISOString(),
    group_id: row.group_id,
    name: row.name,
    platform: row.platform,
    remote_group_id: row.remote_group_id,
    recharge_ratio: 1,
    rate_multiplier: rate,
    user_rate: rate,
    effective_rate: rate,
    actual_rate_multiplier: rate,
    actual_user_rate: rate,
    actual_effective_rate: rate,
  };
}

export class CanonicalRateClient {
  async fetchRates(siteId?: number, options?: { bypassCache?: boolean }) {
    const cacheKey = String(siteId ?? "all");
    return ratesCache.getOrCreate(
      cacheKey,
      () => db.$queryRaw<CanonicalRateRow[]>`
        SELECT rs.channel_id AS site_id,
               c.name AS site_name,
               c.monitor_enabled AS site_enabled,
               c.rate_interval_minutes AS site_interval_min,
               CASE
                 WHEN c.monitor_enabled
                   AND COALESCE(c.last_error, '') = ''
                   AND c.last_rate_scan_at IS NOT NULL
                 THEN 'online'
                 ELSE 'offline'
               END AS last_status,
               GREATEST(c.last_rate_scan_at, rs.last_seen_at) AS last_success_at,
                rs.last_seen_at AS collected_at,
                COALESCE(CAST(rs.remote_group_id AS text), rs.model_name) AS group_id,
                rs.model_name AS name,
                c.type AS platform,
                CAST(rs.remote_group_id AS text) AS remote_group_id,
                rs.ratio AS rate
        FROM rate_snapshots rs
        JOIN channels c ON c.id = rs.channel_id
        WHERE ${siteId ? Prisma.sql`rs.channel_id = ${siteId}` : Prisma.sql`TRUE`}
        ORDER BY c.sort_order ASC, c.id ASC, rs.model_name ASC
      `.then((rows) => rows.map(toCanonicalRate)),
      { bypass: options?.bypassCache },
    );
  }
}
