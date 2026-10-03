import { Prisma, PrismaClient } from "@prisma/client";

type GlobalWithSub2ApiDb = typeof globalThis & {
  sub2apiUsageDb?: PrismaClient;
};

type RawUsageStatsRow = {
  period: string;
  start_date: Date | string;
  end_date: Date | string;
  requests: bigint | number | null;
  active_users: bigint | number | null;
  input_tokens: bigint | number | null;
  output_tokens: bigint | number | null;
  cache_creation_tokens: bigint | number | null;
  cache_read_tokens: bigint | number | null;
  base_cost: unknown;
  user_cost: unknown;
  upstream_cost: unknown;
  administrator_cost: unknown;
  avg_first_token_ms: unknown;
  p95_first_token_ms: unknown;
  first_token_samples: bigint | number | null;
  slow_first_token_requests: bigint | number | null;
  avg_duration_ms: unknown;
  stream_requests: bigint | number | null;
  last_usage_at: Date | string | null;
};

type RawDailyStatsRow = Omit<RawUsageStatsRow, "period" | "start_date" | "end_date"> & {
  bucket_date: Date | string;
};

type RawHeatmapRow = {
  bucket_date: Date | string;
  bucket_hour: number;
  requests: bigint | number | null;
  user_cost: unknown;
  upstream_cost: unknown;
  avg_first_token_ms: unknown;
  p95_first_token_ms: unknown;
  slow_requests: bigint | number | null;
};

type RawSlowRequestRow = {
  id: bigint | number;
  request_id: string;
  created_at: Date | string;
  user_id: bigint | number;
  username: string | null;
  email: string;
  model: string;
  account_id: bigint | number;
  stream: boolean;
  input_tokens: bigint | number | null;
  output_tokens: bigint | number | null;
  first_token_ms: bigint | number | null;
  duration_ms: bigint | number | null;
  user_cost: unknown;
  upstream_cost: unknown;
};

export type ProfitStatsPeriod = {
  key: "day" | "week" | "month";
  label: string;
  startDate: string;
  endDate: string;
  requests: number;
  activeUsers: number;
  inputTokens: number;
  outputTokens: number;
  cacheCreationTokens: number;
  cacheReadTokens: number;
  totalTokens: number;
  cacheHitRate: number | null;
  baseCost: number;
  userCost: number;
  upstreamCost: number;
  administratorCost: number;
  profit: number;
  profitMargin: number | null;
  avgFirstTokenMs: number | null;
  p95FirstTokenMs: number | null;
  slowFirstTokenRate: number | null;
  avgDurationMs: number | null;
  streamRequests: number;
  lastUsageAt: string | null;
};

export type DailyProfitStats = Omit<ProfitStatsPeriod, "key" | "label" | "startDate" | "endDate"> & {
  date: string;
};

export type ProfitStatsOverview = {
  timezone: string;
  generatedAt: string;
  periods: ProfitStatsPeriod[];
  daily: DailyProfitStats[];
};

export type UsageHeatmapCell = {
  date: string;
  hour: number;
  requests: number;
  userCost: number;
  upstreamCost: number;
  profit: number;
  avgFirstTokenMs: number | null;
  p95FirstTokenMs: number | null;
  slowRequests: number;
};

export type SlowUsageRequest = {
  id: number;
  requestId: string;
  createdAt: string;
  userId: number;
  userName: string;
  model: string;
  accountId: number;
  stream: boolean;
  inputTokens: number;
  outputTokens: number;
  firstTokenMs: number | null;
  durationMs: number | null;
  userCost: number;
  upstreamCost: number;
};

export type UsageAnalyticsOverview = {
  timezone: string;
  generatedAt: string;
  heatmap: UsageHeatmapCell[];
  slowRequests: SlowUsageRequest[];
};

const timezone = "Asia/Shanghai";

export const accountUpstreamCostSql =
  "COALESCE(l.total_cost, 0) * COALESCE(l.account_rate_multiplier, 1)";
const accountUpstreamCostExpression = Prisma.raw(accountUpstreamCostSql);

export function calculateAccountUpstreamCost(input: {
  totalCost?: number | null;
  accountRateMultiplier?: number | null;
}) {
  const baseCost = input.totalCost ?? 0;
  return baseCost * (input.accountRateMultiplier ?? 1);
}

export function calculateCacheHitRate(input: {
  inputTokens: number;
  cacheCreationTokens: number;
  cacheReadTokens: number;
}) {
  const promptTokens = input.inputTokens + input.cacheCreationTokens + input.cacheReadTokens;
  return promptTokens > 0 ? input.cacheReadTokens / promptTokens : null;
}

export function calculateProfitBreakdown(input: {
  userCost: number;
  upstreamCost: number;
  administratorCost?: number;
}) {
  const profit = input.userCost - input.upstreamCost;
  return {
    profit,
    profitMargin: input.userCost > 0 ? profit / input.userCost : null,
  };
}

export function completeUsageHeatmap(rows: UsageHeatmapCell[], dates: string[]) {
  const byCell = new Map(rows.map((row) => [`${row.date}:${row.hour}`, row]));
  return dates.flatMap((date) => Array.from({ length: 24 }, (_, hour) => {
    const existing = byCell.get(`${date}:${hour}`);
    return existing ?? {
      date,
      hour,
      requests: 0,
      userCost: 0,
      upstreamCost: 0,
      profit: 0,
      avgFirstTokenMs: null,
      p95FirstTokenMs: null,
      slowRequests: 0,
    };
  }));
}

const periodLabels: Record<ProfitStatsPeriod["key"], string> = {
  day: "今日",
  week: "本周",
  month: "本月",
};

export function sub2apiDb() {
  const databaseUrl = process.env.SUB2API_DATABASE_URL;
  if (!databaseUrl) {
    throw new Error("SUB2API_DATABASE_URL 未配置，无法读取 Sub2API 用量数据库");
  }

  const globalForDb = globalThis as GlobalWithSub2ApiDb;
  if (!globalForDb.sub2apiUsageDb) {
    globalForDb.sub2apiUsageDb = new PrismaClient({
      datasources: { db: { url: databaseUrl } },
      log: process.env.NODE_ENV === "development" ? ["error", "warn"] : ["error"],
    });
  }
  return globalForDb.sub2apiUsageDb;
}

function toNumber(value: unknown) {
  if (value === null || value === undefined) return 0;
  if (typeof value === "bigint") return Number(value);
  if (typeof value === "number") return Number.isFinite(value) ? value : 0;
  if (typeof value === "object" && value && "toNumber" in value && typeof value.toNumber === "function") {
    const numeric = value.toNumber() as number;
    return Number.isFinite(numeric) ? numeric : 0;
  }
  const numeric = Number(value);
  return Number.isFinite(numeric) ? numeric : 0;
}

function toNullableNumber(value: unknown) {
  if (value === null || value === undefined) return null;
  const numeric = toNumber(value);
  return Number.isFinite(numeric) ? numeric : null;
}

function toDateOnly(value: Date | string) {
  if (value instanceof Date) return value.toISOString().slice(0, 10);
  return String(value).slice(0, 10);
}

function toIsoString(value: Date | string | null) {
  if (!value) return null;
  if (value instanceof Date) return value.toISOString();
  const parsed = new Date(value);
  return Number.isFinite(parsed.getTime()) ? parsed.toISOString() : String(value);
}

function mapStatsBase(row: RawUsageStatsRow | RawDailyStatsRow) {
  const inputTokens = toNumber(row.input_tokens);
  const outputTokens = toNumber(row.output_tokens);
  const cacheCreationTokens = toNumber(row.cache_creation_tokens);
  const cacheReadTokens = toNumber(row.cache_read_tokens);
  const totalTokens = inputTokens + outputTokens + cacheCreationTokens + cacheReadTokens;
  const userCost = toNumber(row.user_cost);
  const upstreamCost = toNumber(row.upstream_cost);
  const administratorCost = toNumber(row.administrator_cost);
  const { profit, profitMargin } = calculateProfitBreakdown({ userCost, upstreamCost, administratorCost });

  return {
    requests: toNumber(row.requests),
    activeUsers: toNumber(row.active_users),
    inputTokens,
    outputTokens,
    cacheCreationTokens,
    cacheReadTokens,
    totalTokens,
    cacheHitRate: calculateCacheHitRate({ inputTokens, cacheCreationTokens, cacheReadTokens }),
    baseCost: toNumber(row.base_cost),
    userCost,
    upstreamCost,
    administratorCost,
    profit,
    profitMargin,
    avgFirstTokenMs: toNullableNumber(row.avg_first_token_ms),
    p95FirstTokenMs: toNullableNumber(row.p95_first_token_ms),
    slowFirstTokenRate: toNumber(row.first_token_samples) > 0
      ? toNumber(row.slow_first_token_requests) / toNumber(row.first_token_samples)
      : null,
    avgDurationMs: toNullableNumber(row.avg_duration_ms),
    streamRequests: toNumber(row.stream_requests),
    lastUsageAt: toIsoString(row.last_usage_at),
  };
}

function mapPeriod(row: RawUsageStatsRow): ProfitStatsPeriod {
  const key = row.period as ProfitStatsPeriod["key"];
  return {
    key,
    label: periodLabels[key] ?? row.period,
    startDate: toDateOnly(row.start_date),
    endDate: toDateOnly(row.end_date),
    ...mapStatsBase(row),
  };
}

function mapDaily(row: RawDailyStatsRow): DailyProfitStats {
  return {
    date: toDateOnly(row.bucket_date),
    ...mapStatsBase(row),
  };
}

function mapHeatmap(row: RawHeatmapRow): UsageHeatmapCell {
  const userCost = toNumber(row.user_cost);
  const upstreamCost = toNumber(row.upstream_cost);
  return {
    date: toDateOnly(row.bucket_date),
    hour: toNumber(row.bucket_hour),
    requests: toNumber(row.requests),
    userCost,
    upstreamCost,
    profit: userCost - upstreamCost,
    avgFirstTokenMs: toNullableNumber(row.avg_first_token_ms),
    p95FirstTokenMs: toNullableNumber(row.p95_first_token_ms),
    slowRequests: toNumber(row.slow_requests),
  };
}

function mapSlowRequest(row: RawSlowRequestRow): SlowUsageRequest {
  return {
    id: toNumber(row.id),
    requestId: row.request_id,
    createdAt: toIsoString(row.created_at) ?? String(row.created_at),
    userId: toNumber(row.user_id),
    userName: row.username?.trim() || row.email,
    model: row.model,
    accountId: toNumber(row.account_id),
    stream: row.stream,
    inputTokens: toNumber(row.input_tokens),
    outputTokens: toNumber(row.output_tokens),
    firstTokenMs: toNullableNumber(row.first_token_ms),
    durationMs: toNullableNumber(row.duration_ms),
    userCost: toNumber(row.user_cost),
    upstreamCost: toNumber(row.upstream_cost),
  };
}

export async function getSub2ApiUsageProfitStats(): Promise<ProfitStatsOverview> {
  const db = sub2apiDb();
  const periodRowsQuery = db.$queryRaw<RawUsageStatsRow[]>`
    WITH clock AS (
      SELECT
        (now() AT TIME ZONE 'Asia/Shanghai')::date AS today,
        ((now() AT TIME ZONE 'Asia/Shanghai')::date + 1) AS tomorrow,
        date_trunc('week', now() AT TIME ZONE 'Asia/Shanghai')::date AS week_start,
        date_trunc('month', now() AT TIME ZONE 'Asia/Shanghai')::date AS month_start
    ),
    periods AS (
      SELECT 'day'::text AS period, today AS start_date, tomorrow AS end_date FROM clock
      UNION ALL
      SELECT 'week'::text AS period, week_start AS start_date, tomorrow AS end_date FROM clock
      UNION ALL
      SELECT 'month'::text AS period, month_start AS start_date, tomorrow AS end_date FROM clock
    ),
    logs AS (
      SELECT
        p.period,
        p.start_date,
        p.end_date,
        l.*,
        ${accountUpstreamCostExpression} AS resolved_upstream_cost,
        COALESCE(u.role = 'admin', false) AS is_administrator
      FROM periods p
      LEFT JOIN usage_logs l
        ON l.created_at >= (p.start_date::timestamp AT TIME ZONE 'Asia/Shanghai')
       AND l.created_at < (p.end_date::timestamp AT TIME ZONE 'Asia/Shanghai')
      LEFT JOIN users u ON u.id = l.user_id
    )
    SELECT
      period,
      start_date,
      (end_date - 1)::date AS end_date,
      count(id)::bigint AS requests,
      count(DISTINCT user_id)::bigint AS active_users,
      COALESCE(sum(COALESCE(input_tokens, 0)), 0)::bigint AS input_tokens,
      COALESCE(sum(COALESCE(output_tokens, 0)), 0)::bigint AS output_tokens,
      COALESCE(sum(COALESCE(cache_creation_tokens, 0)), 0)::bigint AS cache_creation_tokens,
      COALESCE(sum(COALESCE(cache_read_tokens, 0)), 0)::bigint AS cache_read_tokens,
      COALESCE(sum(COALESCE(total_cost, 0)), 0)::numeric AS base_cost,
      COALESCE(sum(COALESCE(actual_cost, 0)) FILTER (WHERE is_administrator = false), 0)::numeric AS user_cost,
      COALESCE(sum(resolved_upstream_cost), 0)::numeric AS upstream_cost,
      COALESCE(sum(resolved_upstream_cost) FILTER (WHERE is_administrator = true), 0)::numeric AS administrator_cost,
      avg(first_token_ms) FILTER (WHERE first_token_ms IS NOT NULL)::numeric AS avg_first_token_ms,
      (percentile_disc(0.95) WITHIN GROUP (ORDER BY first_token_ms) FILTER (WHERE first_token_ms IS NOT NULL))::numeric AS p95_first_token_ms,
      count(id) FILTER (WHERE first_token_ms IS NOT NULL)::bigint AS first_token_samples,
      count(id) FILTER (WHERE first_token_ms > 10000)::bigint AS slow_first_token_requests,
      avg(duration_ms) FILTER (WHERE duration_ms IS NOT NULL)::numeric AS avg_duration_ms,
      count(id) FILTER (WHERE stream = true)::bigint AS stream_requests,
      max(created_at) AS last_usage_at
    FROM logs
    GROUP BY period, start_date, end_date
    ORDER BY CASE period WHEN 'day' THEN 1 WHEN 'week' THEN 2 ELSE 3 END
  `;

  const dailyRowsQuery = db.$queryRaw<RawDailyStatsRow[]>`
    WITH clock AS (
      SELECT
        (now() AT TIME ZONE 'Asia/Shanghai')::date AS today,
        ((now() AT TIME ZONE 'Asia/Shanghai')::date + 1) AS tomorrow
    ),
    days AS (
      SELECT generate_series(today - 13, today, interval '1 day')::date AS bucket_date FROM clock
    ),
    logs AS (
      SELECT
        d.bucket_date,
        l.*,
        ${accountUpstreamCostExpression} AS resolved_upstream_cost,
        COALESCE(u.role = 'admin', false) AS is_administrator
      FROM days d
      LEFT JOIN usage_logs l
        ON l.created_at >= (d.bucket_date::timestamp AT TIME ZONE 'Asia/Shanghai')
       AND l.created_at < ((d.bucket_date + 1)::timestamp AT TIME ZONE 'Asia/Shanghai')
      LEFT JOIN users u ON u.id = l.user_id
    )
    SELECT
      bucket_date,
      count(id)::bigint AS requests,
      count(DISTINCT user_id)::bigint AS active_users,
      COALESCE(sum(COALESCE(input_tokens, 0)), 0)::bigint AS input_tokens,
      COALESCE(sum(COALESCE(output_tokens, 0)), 0)::bigint AS output_tokens,
      COALESCE(sum(COALESCE(cache_creation_tokens, 0)), 0)::bigint AS cache_creation_tokens,
      COALESCE(sum(COALESCE(cache_read_tokens, 0)), 0)::bigint AS cache_read_tokens,
      COALESCE(sum(COALESCE(total_cost, 0)), 0)::numeric AS base_cost,
      COALESCE(sum(COALESCE(actual_cost, 0)) FILTER (WHERE is_administrator = false), 0)::numeric AS user_cost,
      COALESCE(sum(resolved_upstream_cost), 0)::numeric AS upstream_cost,
      COALESCE(sum(resolved_upstream_cost) FILTER (WHERE is_administrator = true), 0)::numeric AS administrator_cost,
      avg(first_token_ms) FILTER (WHERE first_token_ms IS NOT NULL)::numeric AS avg_first_token_ms,
      (percentile_disc(0.95) WITHIN GROUP (ORDER BY first_token_ms) FILTER (WHERE first_token_ms IS NOT NULL))::numeric AS p95_first_token_ms,
      count(id) FILTER (WHERE first_token_ms IS NOT NULL)::bigint AS first_token_samples,
      count(id) FILTER (WHERE first_token_ms > 10000)::bigint AS slow_first_token_requests,
      avg(duration_ms) FILTER (WHERE duration_ms IS NOT NULL)::numeric AS avg_duration_ms,
      count(id) FILTER (WHERE stream = true)::bigint AS stream_requests,
      max(created_at) AS last_usage_at
    FROM logs
    GROUP BY bucket_date
    ORDER BY bucket_date DESC
  `;

  const [periodRows, dailyRows] = await Promise.all([periodRowsQuery, dailyRowsQuery]);

  return {
    timezone,
    generatedAt: new Date().toISOString(),
    periods: periodRows.map(mapPeriod),
    daily: dailyRows.map(mapDaily),
  };
}

export async function getSub2ApiUsageAnalytics(input: {
  slowDate?: string;
  slowHour?: number;
  slowThresholdMs?: number;
  slowLimit?: number;
} = {}): Promise<UsageAnalyticsOverview> {
  const db = sub2apiDb();
  const slowThresholdMs = Math.min(120_000, Math.max(1_000, Math.floor(input.slowThresholdMs ?? 10_000)));
  const durationThresholdMs = Math.max(60_000, slowThresholdMs * 3);
  const slowLimit = Math.min(100, Math.max(1, Math.floor(input.slowLimit ?? 30)));
  const selectedWindow = input.slowDate
    ? Prisma.sql`
        AND (l.created_at AT TIME ZONE 'Asia/Shanghai')::date = ${input.slowDate}::date
        ${input.slowHour === undefined ? Prisma.empty : Prisma.sql`AND EXTRACT(hour FROM l.created_at AT TIME ZONE 'Asia/Shanghai')::int = ${input.slowHour}`}
      `
    : Prisma.sql`AND l.created_at >= now() - interval '24 hours'`;

  const [heatmapRows, slowRows] = await Promise.all([
    db.$queryRaw<RawHeatmapRow[]>`
      WITH clock AS (
        SELECT (now() AT TIME ZONE 'Asia/Shanghai')::date AS today
      ),
      days AS (
        SELECT generate_series(today - 6, today, interval '1 day')::date AS bucket_date FROM clock
      ),
      hours AS (
        SELECT generate_series(0, 23)::int AS bucket_hour
      ),
      aggregates AS (
        SELECT
          (l.created_at AT TIME ZONE 'Asia/Shanghai')::date AS bucket_date,
          EXTRACT(hour FROM l.created_at AT TIME ZONE 'Asia/Shanghai')::int AS bucket_hour,
          count(l.id)::bigint AS requests,
          COALESCE(sum(COALESCE(l.actual_cost, 0)), 0)::numeric AS user_cost,
          COALESCE(sum(${accountUpstreamCostExpression}), 0)::numeric AS upstream_cost,
          avg(l.first_token_ms) FILTER (WHERE l.first_token_ms IS NOT NULL)::numeric AS avg_first_token_ms,
          (percentile_disc(0.95) WITHIN GROUP (ORDER BY l.first_token_ms) FILTER (WHERE l.first_token_ms IS NOT NULL))::numeric AS p95_first_token_ms,
          count(l.id) FILTER (
            WHERE l.first_token_ms >= ${slowThresholdMs} OR l.duration_ms >= ${durationThresholdMs}
          )::bigint AS slow_requests
        FROM usage_logs l
        JOIN users u ON u.id = l.user_id
        CROSS JOIN clock
        WHERE u.role <> 'admin'
          AND l.created_at >= ((clock.today - 6)::timestamp AT TIME ZONE 'Asia/Shanghai')
          AND l.created_at < ((clock.today + 1)::timestamp AT TIME ZONE 'Asia/Shanghai')
        GROUP BY 1, 2
      )
      SELECT
        d.bucket_date,
        h.bucket_hour,
        COALESCE(a.requests, 0)::bigint AS requests,
        COALESCE(a.user_cost, 0)::numeric AS user_cost,
        COALESCE(a.upstream_cost, 0)::numeric AS upstream_cost,
        a.avg_first_token_ms,
        a.p95_first_token_ms,
        COALESCE(a.slow_requests, 0)::bigint AS slow_requests
      FROM days d
      CROSS JOIN hours h
      LEFT JOIN aggregates a USING (bucket_date, bucket_hour)
      ORDER BY d.bucket_date, h.bucket_hour
    `,
    db.$queryRaw<RawSlowRequestRow[]>`
      SELECT
        l.id,
        l.request_id,
        l.created_at,
        l.user_id,
        u.username,
        u.email,
        l.model,
        l.account_id,
        l.stream,
        l.input_tokens,
        l.output_tokens,
        l.first_token_ms,
        l.duration_ms,
        COALESCE(l.actual_cost, 0)::numeric AS user_cost,
        ${accountUpstreamCostExpression}::numeric AS upstream_cost
      FROM usage_logs l
      JOIN users u ON u.id = l.user_id
      WHERE u.role <> 'admin'
        AND (COALESCE(l.first_token_ms, 0) >= ${slowThresholdMs} OR COALESCE(l.duration_ms, 0) >= ${durationThresholdMs})
        ${selectedWindow}
      ORDER BY GREATEST(COALESCE(l.first_token_ms, 0), COALESCE(l.duration_ms, 0)) DESC, l.created_at DESC
      LIMIT ${slowLimit}
    `,
  ]);

  const mappedHeatmap = heatmapRows.map(mapHeatmap);
  const dates = Array.from(new Set(mappedHeatmap.map((row) => row.date)));
  return {
    timezone,
    generatedAt: new Date().toISOString(),
    heatmap: completeUsageHeatmap(mappedHeatmap, dates),
    slowRequests: slowRows.map(mapSlowRequest),
  };
}
