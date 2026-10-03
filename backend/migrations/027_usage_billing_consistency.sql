-- 027_usage_billing_consistency.sql
-- Ensure usage_logs idempotency (request_id, api_key_id) and add reconciliation infrastructure.

-- -----------------------------------------------------------------------------
-- 1) Normalize legacy request_id values
-- -----------------------------------------------------------------------------
-- Historically request_id may be inserted as empty string. Convert it to NULL so
-- the upcoming unique index does not break on repeated "" values.
--
-- Cleanup runs in bounded batches (5000 rows each) instead of one full-table
-- UPDATE: usage_logs is the hottest write table in the gateway, and a single
-- full-table UPDATE would hold row locks and block billing writes for minutes.
DO $$
DECLARE
    v_rows       INTEGER := 0;
    v_total_rows INTEGER := 0;
    v_batch_size INTEGER := 5000;
BEGIN
    LOOP
        WITH batch AS (
            SELECT id
            FROM usage_logs
            WHERE request_id = ''
            ORDER BY id
            LIMIT v_batch_size
        )
        UPDATE usage_logs ul
        SET request_id = NULL
        FROM batch
        WHERE ul.id = batch.id;

        GET DIAGNOSTICS v_rows = ROW_COUNT;
        EXIT WHEN v_rows = 0;
        v_total_rows := v_total_rows + v_rows;
    END LOOP;
    RAISE NOTICE 'usage_logs request_id empty-string cleanup rows=%', v_total_rows;
END
$$;

-- If duplicates already exist for the same (request_id, api_key_id), keep the
-- first row and NULL-out request_id for the rest so the unique index can be
-- created without deleting historical logs. The window scan is read-only; the
-- UPDATE itself only touches the bounded batch of rows.
DO $$
DECLARE
    v_rows       INTEGER := 0;
    v_total_rows INTEGER := 0;
    v_batch_size INTEGER := 5000;
BEGIN
    LOOP
        WITH dup AS (
            SELECT id
            FROM (
                SELECT
                    id,
                    ROW_NUMBER() OVER (PARTITION BY api_key_id, request_id ORDER BY id) AS rn
                FROM usage_logs
                WHERE request_id IS NOT NULL
            ) ranked
            WHERE ranked.rn > 1
            ORDER BY id
            LIMIT v_batch_size
        )
        UPDATE usage_logs ul
        SET request_id = NULL
        FROM dup
        WHERE ul.id = dup.id;

        GET DIAGNOSTICS v_rows = ROW_COUNT;
        EXIT WHEN v_rows = 0;
        v_total_rows := v_total_rows + v_rows;
    END LOOP;
    RAISE NOTICE 'usage_logs request_id duplicate cleanup rows=%', v_total_rows;
END
$$;

-- -----------------------------------------------------------------------------
-- 2) Idempotency constraint for usage_logs
-- -----------------------------------------------------------------------------
-- The unique index is created in 027_usage_billing_consistency_notx.sql with
-- CREATE UNIQUE INDEX CONCURRENTLY so building it does not take an exclusive
-- lock on the hot usage_logs table inside the migration transaction.

-- -----------------------------------------------------------------------------
-- 3) Reconciliation infrastructure: billing ledger for usage charges
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS billing_usage_entries (
    id BIGSERIAL PRIMARY KEY,
    usage_log_id BIGINT NOT NULL REFERENCES usage_logs(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    subscription_id BIGINT REFERENCES user_subscriptions(id) ON DELETE SET NULL,
    billing_type SMALLINT NOT NULL,
    applied BOOLEAN NOT NULL DEFAULT TRUE,
    delta_usd DECIMAL(20, 10) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS billing_usage_entries_usage_log_id_unique
    ON billing_usage_entries (usage_log_id);

CREATE INDEX IF NOT EXISTS idx_billing_usage_entries_user_time
    ON billing_usage_entries (user_id, created_at);

CREATE INDEX IF NOT EXISTS idx_billing_usage_entries_created_at
    ON billing_usage_entries (created_at);