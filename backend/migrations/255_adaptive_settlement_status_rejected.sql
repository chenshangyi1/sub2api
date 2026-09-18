-- Allow reconciliation to void orphaned pending adaptive usage as 'rejected'
-- so the hold can be released instead of capturing or deadlocking.

ALTER TABLE usage_logs
    DROP CONSTRAINT IF EXISTS usage_logs_adaptive_settlement_status_check;

ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_adaptive_settlement_status_check
    CHECK (adaptive_settlement_status IN ('pending', 'captured', 'released', 'failed', 'rejected')) NOT VALID;

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

    IF OLD.adaptive_settlement_status IS DISTINCT FROM 'pending' THEN
        RAISE EXCEPTION 'invalid adaptive usage settlement transition';
    END IF;

    IF NEW.adaptive_settlement_status = 'captured' THEN
        IF COALESCE(OLD.actual_cost, 0) <> 0
           OR NEW.actual_cost IS DISTINCT FROM OLD.adaptive_total_cost THEN
            RAISE EXCEPTION 'invalid adaptive usage settlement transition';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.adaptive_settlement_status = 'rejected'
       AND COALESCE(OLD.actual_cost, 0) = 0
       AND COALESCE(NEW.actual_cost, 0) = 0 THEN
        RETURN NEW;
    END IF;

    RAISE EXCEPTION 'invalid adaptive usage settlement transition';
END;
$$ LANGUAGE plpgsql;
