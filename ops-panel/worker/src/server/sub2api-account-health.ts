import { Prisma } from "@prisma/client";
import { sub2apiDb } from "@/server/sub2api-usage-stats";

export type PassiveAccountHealthSample = {
  accountId: number;
  gatewayErrors: number;
  successes: number;
  slowSuccesses: number;
  averageFirstTokenMs: number | null;
};

type RawPassiveAccountHealth = {
  account_id: bigint | number;
  gateway_errors: bigint | number;
  successes: bigint | number;
  slow_successes: bigint | number;
  average_first_token_ms: unknown;
};

function numberValue(value: unknown) {
  if (value === null || value === undefined) return 0;
  const numeric = Number(value);
  return Number.isFinite(numeric) ? numeric : 0;
}

export async function getSub2ApiPassiveAccountHealth(
  accountIds: number[],
  slowFirstTokenMs: number,
  windowMinutes = 10,
) {
  const ids = Array.from(new Set(accountIds.filter((id) => Number.isInteger(id) && id > 0)));
  if (ids.length === 0) return new Map<number, PassiveAccountHealthSample>();

  const db = sub2apiDb();
  const rows = await db.$queryRaw<RawPassiveAccountHealth[]>(Prisma.sql`
    WITH errors AS (
      SELECT
        account_id,
        count(*)::bigint AS gateway_errors
      FROM ops_error_logs
      WHERE created_at >= now() - (${windowMinutes} * interval '1 minute')
        AND account_id IN (${Prisma.join(ids)})
        AND COALESCE(upstream_status_code, status_code) IN (502,503,504,520,521,522,523,524)
        AND lower(COALESCE(error_type, '')) NOT IN ('policy','cyber_policy','session_blocked_by_cyber_policy')
        AND lower(COALESCE(error_type, '')) NOT LIKE '%cyber_policy%'
      GROUP BY account_id
    ), successes AS (
      SELECT
        account_id,
        count(*)::bigint AS successes,
        count(*) FILTER (WHERE first_token_ms > ${slowFirstTokenMs})::bigint AS slow_successes,
        avg(first_token_ms) FILTER (WHERE first_token_ms IS NOT NULL)::numeric AS average_first_token_ms
      FROM usage_logs
      WHERE created_at >= now() - (${windowMinutes} * interval '1 minute')
        AND account_id IN (${Prisma.join(ids)})
      GROUP BY account_id
    )
    SELECT
      COALESCE(e.account_id, s.account_id) AS account_id,
      COALESCE(e.gateway_errors, 0)::bigint AS gateway_errors,
      COALESCE(s.successes, 0)::bigint AS successes,
      COALESCE(s.slow_successes, 0)::bigint AS slow_successes,
      s.average_first_token_ms
    FROM errors e
    FULL OUTER JOIN successes s USING (account_id)
  `);

  return new Map(rows.map((row) => {
    const accountId = numberValue(row.account_id);
    return [accountId, {
      accountId,
      gatewayErrors: numberValue(row.gateway_errors),
      successes: numberValue(row.successes),
      slowSuccesses: numberValue(row.slow_successes),
      averageFirstTokenMs: row.average_first_token_ms === null
        ? null
        : Math.round(numberValue(row.average_first_token_ms)),
    }];
  }));
}
