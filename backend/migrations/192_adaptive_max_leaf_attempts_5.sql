-- Raise Adaptive cross-leaf attempt budget from 2 to 5 so a parent pool can
-- productively host 4–5 ordered leaf groups. Topology membership was already
-- unbounded; only the per-request attempt ceiling and CHECK constraints were 2.

-- usage_billing_reservations: winner + active attempt bounds
ALTER TABLE usage_billing_reservations
    DROP CONSTRAINT IF EXISTS usage_billing_reservations_winner_check;

ALTER TABLE usage_billing_reservations
    ADD CONSTRAINT usage_billing_reservations_winner_check
    CHECK ((
        (status = 'captured'
            AND winning_leaf_group_id IS NOT NULL
            AND winning_attempt_no IS NOT NULL
            AND winning_attempt_no BETWEEN 1 AND 5
            AND usage_log_id IS NOT NULL
            AND usage_log_created_at IS NOT NULL
            AND usage_evidence_hash IS NOT NULL
            AND captured_at IS NOT NULL
            AND released_at IS NULL)
        OR
        (status <> 'captured'
            AND winning_leaf_group_id IS NULL
            AND winning_attempt_no IS NULL
            AND usage_log_id IS NULL
            AND usage_log_created_at IS NULL
            AND usage_evidence_hash IS NULL
            AND captured_at IS NULL)
    ) IS TRUE);

ALTER TABLE usage_billing_reservations
    DROP CONSTRAINT IF EXISTS usage_billing_reservations_active_attempt_check;

ALTER TABLE usage_billing_reservations
    ADD CONSTRAINT usage_billing_reservations_active_attempt_check
    CHECK ((
        (status = 'in_flight'
            AND active_leaf_group_id IS NOT NULL
            AND active_attempt_no IS NOT NULL
            AND active_attempt_no BETWEEN 1 AND 5
            AND attempt_started_at IS NOT NULL)
        OR
        (status = 'reconciling' AND reconcile_from_status = 'in_flight'
            AND active_leaf_group_id IS NOT NULL
            AND active_attempt_no IS NOT NULL
            AND active_attempt_no BETWEEN 1 AND 5
            AND attempt_started_at IS NOT NULL)
        OR
        (status NOT IN ('in_flight', 'reconciling')
            AND active_leaf_group_id IS NULL
            AND active_attempt_no IS NULL
            AND attempt_started_at IS NULL)
        OR
        (status = 'reconciling' AND reconcile_from_status = 'authorized'
            AND active_leaf_group_id IS NULL
            AND active_attempt_no IS NULL
            AND attempt_started_at IS NULL)
    ) IS TRUE);

-- usage_billing_attempts.attempt_no column check (default PG name)
ALTER TABLE usage_billing_attempts
    DROP CONSTRAINT IF EXISTS usage_billing_attempts_attempt_no_check;

ALTER TABLE usage_billing_attempts
    ADD CONSTRAINT usage_billing_attempts_attempt_no_check
    CHECK (attempt_no BETWEEN 1 AND 5);

-- adaptive_usage_evidence_keys.attempt_no column check
ALTER TABLE adaptive_usage_evidence_keys
    DROP CONSTRAINT IF EXISTS adaptive_usage_evidence_keys_attempt_no_check;

ALTER TABLE adaptive_usage_evidence_keys
    ADD CONSTRAINT adaptive_usage_evidence_keys_attempt_no_check
    CHECK (attempt_no BETWEEN 1 AND 5);
