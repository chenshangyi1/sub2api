-- Adaptive usage evidence is inserted as pending before funds are captured.
-- The reservation row is the synchronization point for insert, capture,
-- failed-attempt and reconciliation decisions.

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS adaptive_base_cost DECIMAL(20,10),
    ADD COLUMN IF NOT EXISTS adaptive_management_fee_cost DECIMAL(20,10),
    ADD COLUMN IF NOT EXISTS adaptive_total_cost DECIMAL(20,10),
    ADD COLUMN IF NOT EXISTS adaptive_uncapped_base_cost DECIMAL(20,10),
    ADD COLUMN IF NOT EXISTS adaptive_platform_overage_cost DECIMAL(20,10),
    ADD COLUMN IF NOT EXISTS adaptive_parent_group_id BIGINT,
    ADD COLUMN IF NOT EXISTS routed_group_id BIGINT,
    ADD COLUMN IF NOT EXISTS adaptive_attempt_no SMALLINT,
    ADD COLUMN IF NOT EXISTS adaptive_pricing_snapshot_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS adaptive_reservation_id UUID,
    ADD COLUMN IF NOT EXISTS adaptive_evidence_hash CHAR(64),
    ADD COLUMN IF NOT EXISTS adaptive_settlement_status VARCHAR(16)
        CHECK (adaptive_settlement_status IN ('pending', 'captured', 'released', 'failed'));

-- Keep the uniqueness fence outside usage_logs so it remains global if the
-- usage table is later range-partitioned by created_at. PostgreSQL otherwise
-- requires every unique index on a partitioned table to include that key.
CREATE TABLE IF NOT EXISTS adaptive_usage_evidence_keys (
    reservation_id      UUID NOT NULL REFERENCES usage_billing_reservations(id) ON DELETE RESTRICT,
    attempt_no          SMALLINT NOT NULL CHECK (attempt_no BETWEEN 1 AND 2),
    usage_log_id        BIGINT NOT NULL,
    usage_log_created_at TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (reservation_id, attempt_no)
);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM usage_logs
        WHERE adaptive_reservation_id IS NOT NULL
        GROUP BY adaptive_reservation_id, adaptive_attempt_no
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'duplicate adaptive usage evidence exists';
    END IF;
END $$;

INSERT INTO adaptive_usage_evidence_keys (
    reservation_id, attempt_no, usage_log_id, usage_log_created_at
)
SELECT adaptive_reservation_id, adaptive_attempt_no, id, created_at
FROM usage_logs
WHERE adaptive_reservation_id IS NOT NULL
ON CONFLICT (reservation_id, attempt_no) DO NOTHING;

-- The reservation lookup index is created in
-- 189_adaptive_usage_log_metadata_notx.sql with CREATE INDEX CONCURRENTLY so
-- building the partial index (which still needs a full-table scan to evaluate
-- the WHERE clause on the just-added column) does not take an exclusive lock
-- on the hot usage_logs table inside the migration transaction.

CREATE OR REPLACE FUNCTION enforce_adaptive_usage_evidence_insert()
RETURNS TRIGGER AS $$
DECLARE
    reservation_status       VARCHAR(20);
	reservation_reconcile_from VARCHAR(20);
    reservation_attempt      INTEGER;
    reservation_leaf         BIGINT;
    reservation_user         BIGINT;
    reservation_api_key      BIGINT;
    reservation_subscription BIGINT;
    reservation_funding      VARCHAR(20);
    reservation_parent       BIGINT;
    reservation_pricing      VARCHAR(128);
    reservation_held_base    NUMERIC;
    reservation_held_fee     NUMERIC;
    reservation_held_total   NUMERIC;
    expected_billing_type    SMALLINT;
BEGIN
    IF NEW.adaptive_reservation_id IS NULL THEN
		IF NEW.adaptive_base_cost IS NOT NULL
		   OR NEW.adaptive_management_fee_cost IS NOT NULL
		   OR NEW.adaptive_total_cost IS NOT NULL
		   OR NEW.adaptive_uncapped_base_cost IS NOT NULL
		   OR NEW.adaptive_platform_overage_cost IS NOT NULL
		   OR NEW.adaptive_parent_group_id IS NOT NULL
		   OR NEW.routed_group_id IS NOT NULL
		   OR NEW.adaptive_attempt_no IS NOT NULL
		   OR NEW.adaptive_pricing_snapshot_id IS NOT NULL
		   OR NEW.adaptive_evidence_hash IS NOT NULL
		   OR NEW.adaptive_settlement_status IS NOT NULL THEN
			RAISE EXCEPTION 'partial adaptive usage evidence is invalid';
		END IF;
        RETURN NEW;
    END IF;

	SELECT status, reconcile_from_status, active_attempt_no, active_leaf_group_id, user_id, api_key_id,
           subscription_id, funding_source, parent_group_id, pricing_snapshot_id,
           held_base_cost, held_management_fee, held_total
	  INTO reservation_status, reservation_reconcile_from, reservation_attempt, reservation_leaf,
           reservation_user, reservation_api_key, reservation_subscription,
           reservation_funding, reservation_parent, reservation_pricing,
           reservation_held_base, reservation_held_fee, reservation_held_total
      FROM usage_billing_reservations
     WHERE id = NEW.adaptive_reservation_id
     FOR SHARE;

	IF NOT FOUND
	   OR NOT (reservation_status = 'in_flight'
		OR (reservation_status = 'reconciling' AND reservation_reconcile_from = 'in_flight')) THEN
        RAISE EXCEPTION 'adaptive reservation is not accepting usage evidence';
    END IF;

    expected_billing_type := CASE reservation_funding
        WHEN 'balance' THEN 0
        WHEN 'subscription' THEN 1
        ELSE -1
    END;

    IF NEW.adaptive_settlement_status IS DISTINCT FROM 'pending'
       OR COALESCE(NEW.actual_cost, 0) <> 0 THEN
        RAISE EXCEPTION 'adaptive usage evidence must start pending with zero actual_cost';
    END IF;
    IF NEW.adaptive_attempt_no IS DISTINCT FROM reservation_attempt
       OR NEW.routed_group_id IS DISTINCT FROM reservation_leaf THEN
        RAISE EXCEPTION 'adaptive usage evidence does not match active attempt';
    END IF;
    IF NEW.user_id IS DISTINCT FROM reservation_user
       OR NEW.api_key_id IS DISTINCT FROM reservation_api_key
       OR NEW.subscription_id IS DISTINCT FROM reservation_subscription
       OR NEW.billing_type IS DISTINCT FROM expected_billing_type
       OR NEW.adaptive_parent_group_id IS DISTINCT FROM reservation_parent
       OR NEW.adaptive_pricing_snapshot_id IS DISTINCT FROM reservation_pricing THEN
        RAISE EXCEPTION 'adaptive usage evidence does not match billing ownership';
    END IF;
    IF NEW.adaptive_base_cost IS NULL
       OR NEW.adaptive_management_fee_cost IS NULL
       OR NEW.adaptive_total_cost IS NULL
       OR NEW.adaptive_uncapped_base_cost IS NULL
       OR NEW.adaptive_platform_overage_cost IS NULL
       OR NEW.adaptive_base_cost < 0
       OR NEW.adaptive_management_fee_cost < 0
       OR NEW.adaptive_total_cost < 0
       OR NEW.adaptive_uncapped_base_cost < 0
       OR NEW.adaptive_platform_overage_cost < 0
       OR NEW.adaptive_base_cost > reservation_held_base
       OR NEW.adaptive_management_fee_cost > reservation_held_fee
       OR NEW.adaptive_total_cost > reservation_held_total
       OR NEW.adaptive_management_fee_cost <> ROUND(NEW.adaptive_base_cost * 0.15, 10)
       OR NEW.adaptive_total_cost <> NEW.adaptive_base_cost + NEW.adaptive_management_fee_cost
       OR NEW.adaptive_platform_overage_cost <> NEW.adaptive_uncapped_base_cost - NEW.adaptive_base_cost THEN
        RAISE EXCEPTION 'adaptive usage evidence amounts violate authorization';
    END IF;
    IF NEW.adaptive_evidence_hash IS NULL
       OR NEW.adaptive_evidence_hash !~ '^[0-9a-f]{64}$' THEN
        RAISE EXCEPTION 'adaptive usage evidence hash is invalid';
    END IF;

	INSERT INTO adaptive_usage_evidence_keys (
		reservation_id, attempt_no, usage_log_id, usage_log_created_at
	) VALUES (
		NEW.adaptive_reservation_id, NEW.adaptive_attempt_no, NEW.id, NEW.created_at
	);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION enforce_adaptive_usage_evidence_update()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.adaptive_reservation_id IS NULL THEN
        IF NEW.adaptive_reservation_id IS NOT NULL THEN
            RAISE EXCEPTION 'adaptive evidence cannot be attached after insert';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.adaptive_reservation_id IS DISTINCT FROM OLD.adaptive_reservation_id
       OR NEW.adaptive_evidence_hash IS DISTINCT FROM OLD.adaptive_evidence_hash
       OR NEW.adaptive_attempt_no IS DISTINCT FROM OLD.adaptive_attempt_no
       OR NEW.routed_group_id IS DISTINCT FROM OLD.routed_group_id
       OR NEW.adaptive_parent_group_id IS DISTINCT FROM OLD.adaptive_parent_group_id
       OR NEW.adaptive_pricing_snapshot_id IS DISTINCT FROM OLD.adaptive_pricing_snapshot_id
       OR NEW.adaptive_base_cost IS DISTINCT FROM OLD.adaptive_base_cost
       OR NEW.adaptive_management_fee_cost IS DISTINCT FROM OLD.adaptive_management_fee_cost
       OR NEW.adaptive_total_cost IS DISTINCT FROM OLD.adaptive_total_cost
       OR NEW.adaptive_uncapped_base_cost IS DISTINCT FROM OLD.adaptive_uncapped_base_cost
       OR NEW.adaptive_platform_overage_cost IS DISTINCT FROM OLD.adaptive_platform_overage_cost THEN
        RAISE EXCEPTION 'adaptive usage evidence is immutable';
    END IF;

    IF OLD.adaptive_settlement_status IS DISTINCT FROM 'pending'
       OR NEW.adaptive_settlement_status IS DISTINCT FROM 'captured'
       OR COALESCE(OLD.actual_cost, 0) <> 0
       OR NEW.actual_cost IS DISTINCT FROM OLD.adaptive_total_cost THEN
        RAISE EXCEPTION 'invalid adaptive usage settlement transition';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_usage_logs_adaptive_evidence_insert ON usage_logs;
CREATE TRIGGER trg_usage_logs_adaptive_evidence_insert
    BEFORE INSERT ON usage_logs
    FOR EACH ROW EXECUTE FUNCTION enforce_adaptive_usage_evidence_insert();

DROP TRIGGER IF EXISTS trg_usage_logs_adaptive_evidence_update ON usage_logs;
CREATE TRIGGER trg_usage_logs_adaptive_evidence_update
    BEFORE UPDATE ON usage_logs
    FOR EACH ROW
    WHEN (OLD.adaptive_reservation_id IS NOT NULL OR NEW.adaptive_reservation_id IS NOT NULL)
    EXECUTE FUNCTION enforce_adaptive_usage_evidence_update();
