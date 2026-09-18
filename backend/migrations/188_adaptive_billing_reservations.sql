-- Durable pre-billing holds for Adaptive requests. PostgreSQL is authoritative;
-- cache expiry never releases money or quota.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS adaptive_reserved_balance DECIMAL(24,10) NOT NULL DEFAULT 0
        CHECK (adaptive_reserved_balance >= 0);

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS reserved_quota_usd DECIMAL(24,10) NOT NULL DEFAULT 0
        CHECK (reserved_quota_usd >= 0),
    ADD COLUMN IF NOT EXISTS reserved_usage_5h_usd DECIMAL(24,10) NOT NULL DEFAULT 0
        CHECK (reserved_usage_5h_usd >= 0),
    ADD COLUMN IF NOT EXISTS reserved_usage_1d_usd DECIMAL(24,10) NOT NULL DEFAULT 0
        CHECK (reserved_usage_1d_usd >= 0),
    ADD COLUMN IF NOT EXISTS reserved_usage_7d_usd DECIMAL(24,10) NOT NULL DEFAULT 0
        CHECK (reserved_usage_7d_usd >= 0);

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS reserved_usage_usd DECIMAL(24,10) NOT NULL DEFAULT 0
        CHECK (reserved_usage_usd >= 0);

CREATE TABLE IF NOT EXISTS usage_billing_reservations (
    id                              UUID PRIMARY KEY,
    idempotency_key_hash            CHAR(64) NOT NULL,
    request_fingerprint             CHAR(64) NOT NULL,
    logical_request_id              VARCHAR(128) NOT NULL,
    user_id                         BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    api_key_id                      BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE RESTRICT,
    parent_group_id                 BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    canonical_model                 VARCHAR(128) NOT NULL DEFAULT '',
    pricing_snapshot_id             VARCHAR(128) NOT NULL DEFAULT '',
    pricing_generation              BIGINT NOT NULL DEFAULT 0,
    config_generation               BIGINT NOT NULL DEFAULT 0,
    subscription_id                 BIGINT REFERENCES user_subscriptions(id) ON DELETE RESTRICT,
    funding_source                  VARCHAR(20) NOT NULL,
    status                          VARCHAR(20) NOT NULL DEFAULT 'authorized',
    management_fee_bps              INTEGER NOT NULL DEFAULT 1500,
    estimated_base_cost             DECIMAL(20,10) NOT NULL,
    held_base_cost                  DECIMAL(20,10) NOT NULL,
    held_management_fee             DECIMAL(20,10) NOT NULL,
    held_total                      DECIMAL(20,10) NOT NULL,
    uncapped_base_cost              DECIMAL(20,10) NOT NULL DEFAULT 0,
    captured_base_cost              DECIMAL(20,10) NOT NULL DEFAULT 0,
    captured_management_fee         DECIMAL(20,10) NOT NULL DEFAULT 0,
    captured_total                  DECIMAL(20,10) NOT NULL DEFAULT 0,
    platform_overage_base_cost      DECIMAL(20,10) NOT NULL DEFAULT 0,
    winning_leaf_group_id           BIGINT REFERENCES groups(id) ON DELETE RESTRICT,
    winning_attempt_no              INTEGER,
    usage_log_id                    BIGINT,
    usage_log_created_at            TIMESTAMPTZ,
    usage_evidence_hash             CHAR(64),
    active_leaf_group_id            BIGINT REFERENCES groups(id) ON DELETE RESTRICT,
    active_attempt_no               INTEGER,
    attempt_started_at              TIMESTAMPTZ,
    reconcile_from_status           VARCHAR(20),
    owner_id                        VARCHAR(128) NOT NULL,
    lease_epoch                     BIGINT NOT NULL DEFAULT 1,
    row_version                     BIGINT NOT NULL DEFAULT 1,
    lease_expires_at                TIMESTAMPTZ NOT NULL,
    reconciliation_lease_expires_at TIMESTAMPTZ,
    captured_at                     TIMESTAMPTZ,
    released_at                     TIMESTAMPTZ,
    release_reason                  VARCHAR(128),
    created_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT usage_billing_reservations_funding_check
        CHECK (funding_source IN ('balance', 'subscription')),
    CONSTRAINT usage_billing_reservations_subscription_check
        CHECK ((funding_source = 'balance' AND subscription_id IS NULL)
            OR (funding_source = 'subscription' AND subscription_id IS NOT NULL)),
    CONSTRAINT usage_billing_reservations_status_check
        CHECK (status IN ('authorized', 'in_flight', 'reconciling', 'captured', 'released')),
    CONSTRAINT usage_billing_reservations_fee_bps_check
        CHECK (management_fee_bps = 1500),
    CONSTRAINT usage_billing_reservations_identity_check
        CHECK ((parent_group_id > 0
            AND logical_request_id <> ''
            AND canonical_model <> ''
            AND pricing_snapshot_id <> ''
            AND idempotency_key_hash ~ '^[0-9a-f]{64}$'
            AND request_fingerprint ~ '^[0-9a-f]{64}$') IS TRUE),
    CONSTRAINT usage_billing_reservations_amounts_check
        CHECK ((
            estimated_base_cost >= 0
            AND held_base_cost >= estimated_base_cost
            AND held_management_fee >= 0
            AND held_total = held_base_cost + held_management_fee
            AND uncapped_base_cost >= 0
            AND captured_base_cost >= 0
            AND captured_base_cost <= held_base_cost
            AND captured_management_fee >= 0
            AND captured_management_fee <= held_management_fee
            AND captured_total = captured_base_cost + captured_management_fee
            AND captured_total <= held_total
            AND platform_overage_base_cost = uncapped_base_cost - captured_base_cost
            AND platform_overage_base_cost >= 0
        ) IS TRUE),
    CONSTRAINT usage_billing_reservations_fee_math_check
        CHECK ((
            held_base_cost = estimated_base_cost
            AND held_total = CEIL(estimated_base_cost * 1.15 * 10000000000) / 10000000000
            AND captured_management_fee = ROUND(captured_base_cost * 0.15, 10)
            AND (status = 'captured'
                OR (uncapped_base_cost = 0
                    AND captured_base_cost = 0
                    AND captured_management_fee = 0
                    AND captured_total = 0
                    AND platform_overage_base_cost = 0))
        ) IS TRUE),
    CONSTRAINT usage_billing_reservations_versions_check
        CHECK (lease_epoch >= 1 AND row_version >= 1
            AND pricing_generation >= 0 AND config_generation >= 0),
    CONSTRAINT usage_billing_reservations_winner_check
        CHECK ((
            (status = 'captured'
                AND winning_leaf_group_id IS NOT NULL
                AND winning_attempt_no IS NOT NULL
                AND winning_attempt_no BETWEEN 1 AND 2
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
        ) IS TRUE),
    CONSTRAINT usage_billing_reservations_active_attempt_check
        CHECK ((
            (status = 'in_flight'
                AND active_leaf_group_id IS NOT NULL
                AND active_attempt_no IS NOT NULL
                AND active_attempt_no BETWEEN 1 AND 2
                AND attempt_started_at IS NOT NULL)
            OR
            (status = 'reconciling' AND reconcile_from_status = 'in_flight'
                AND active_leaf_group_id IS NOT NULL
                AND active_attempt_no IS NOT NULL
                AND active_attempt_no BETWEEN 1 AND 2
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
        ) IS TRUE),
    CONSTRAINT usage_billing_reservations_reconciliation_check
        CHECK ((
            (status = 'reconciling'
                AND reconcile_from_status IS NOT NULL
                AND reconcile_from_status IN ('authorized', 'in_flight')
                AND reconciliation_lease_expires_at IS NOT NULL)
            OR
            (status <> 'reconciling'
                AND reconcile_from_status IS NULL
                AND reconciliation_lease_expires_at IS NULL)
        ) IS TRUE),
    CONSTRAINT usage_billing_reservations_release_check
        CHECK ((
            (status = 'released' AND released_at IS NOT NULL)
            OR (status <> 'released' AND released_at IS NULL)
        ) IS TRUE)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_billing_reservations_idempotency
    ON usage_billing_reservations (api_key_id, idempotency_key_hash);

CREATE INDEX IF NOT EXISTS idx_usage_billing_reservations_expired_held
    ON usage_billing_reservations (lease_expires_at, id)
    WHERE status IN ('authorized', 'in_flight');

CREATE INDEX IF NOT EXISTS idx_usage_billing_reservations_reconcile_claim
    ON usage_billing_reservations (reconciliation_lease_expires_at, id)
    WHERE status = 'reconciling';

CREATE INDEX IF NOT EXISTS idx_usage_billing_reservations_user_created
    ON usage_billing_reservations (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_usage_billing_reservations_api_key_active
    ON usage_billing_reservations (api_key_id, status)
    WHERE status IN ('authorized', 'in_flight', 'reconciling');

CREATE INDEX IF NOT EXISTS idx_usage_billing_reservations_subscription_active
    ON usage_billing_reservations (subscription_id, status)
    WHERE subscription_id IS NOT NULL AND status IN ('authorized', 'in_flight', 'reconciling');

CREATE TABLE IF NOT EXISTS usage_billing_attempts (
    id                         BIGSERIAL PRIMARY KEY,
    reservation_id             UUID NOT NULL REFERENCES usage_billing_reservations(id) ON DELETE RESTRICT,
    attempt_no                 SMALLINT NOT NULL CHECK (attempt_no BETWEEN 1 AND 2),
    leaf_group_id              BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    status                     VARCHAR(16) NOT NULL CHECK (status IN ('started', 'failed', 'succeeded')),
    start_operation_key_hash   CHAR(64) NOT NULL,
    start_fingerprint          CHAR(64) NOT NULL,
    start_evidence_hash        CHAR(64) NOT NULL,
    failure_operation_key_hash CHAR(64),
    failure_fingerprint        CHAR(64),
    failure_evidence_hash      CHAR(64),
    failure_class              VARCHAR(64),
    usage_log_id               BIGINT,
    usage_log_created_at       TIMESTAMPTZ,
    usage_evidence_hash        CHAR(64),
    started_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at                TIMESTAMPTZ,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (reservation_id, attempt_no),
    UNIQUE (reservation_id, start_operation_key_hash),
    CONSTRAINT usage_billing_attempts_terminal_check CHECK (
        (status = 'started'
            AND finished_at IS NULL
            AND failure_operation_key_hash IS NULL
            AND failure_fingerprint IS NULL
            AND failure_evidence_hash IS NULL
            AND failure_class IS NULL
            AND usage_log_id IS NULL
            AND usage_log_created_at IS NULL
            AND usage_evidence_hash IS NULL)
        OR
        (status = 'failed'
            AND finished_at IS NOT NULL
            AND failure_operation_key_hash IS NOT NULL
            AND failure_fingerprint IS NOT NULL
            AND failure_evidence_hash IS NOT NULL
            AND failure_class IS NOT NULL
            AND usage_log_id IS NULL
            AND usage_log_created_at IS NULL
            AND usage_evidence_hash IS NULL)
        OR
        (status = 'succeeded'
            AND finished_at IS NOT NULL
            AND failure_operation_key_hash IS NULL
            AND failure_fingerprint IS NULL
            AND failure_evidence_hash IS NULL
            AND failure_class IS NULL
            AND usage_log_id IS NOT NULL
            AND usage_log_created_at IS NOT NULL
            AND usage_evidence_hash IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_billing_attempts_failure_operation
    ON usage_billing_attempts (reservation_id, failure_operation_key_hash)
    WHERE failure_operation_key_hash IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_usage_billing_attempts_started
    ON usage_billing_attempts (reservation_id, status)
    WHERE status = 'started';

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_billing_attempts_single_success
    ON usage_billing_attempts (reservation_id)
    WHERE status = 'succeeded';

CREATE TABLE IF NOT EXISTS usage_billing_ledger (
    id                               BIGSERIAL PRIMARY KEY,
    reservation_id                   UUID NOT NULL REFERENCES usage_billing_reservations(id) ON DELETE RESTRICT,
    operation                        VARCHAR(24) NOT NULL,
    component                        VARCHAR(24) NOT NULL,
    operation_key_hash               CHAR(64) NOT NULL,
    operation_fingerprint            CHAR(64) NOT NULL,
    funding_source                   VARCHAR(20) NOT NULL,
    amount                           DECIMAL(20,10) NOT NULL,
    hold_delta                       DECIMAL(20,10) NOT NULL DEFAULT 0,
    capture_delta                    DECIMAL(20,10) NOT NULL DEFAULT 0,
    lease_epoch                      BIGINT NOT NULL,
    row_version                      BIGINT NOT NULL,
    available_balance_after          DECIMAL(24,10),
    adaptive_reserved_balance_after  DECIMAL(24,10),
    subscription_reserved_after      DECIMAL(24,10),
    subscription_daily_usage_after   DECIMAL(24,10),
    subscription_weekly_usage_after  DECIMAL(24,10),
    subscription_monthly_usage_after DECIMAL(24,10),
    api_key_reserved_quota_after     DECIMAL(24,10),
    api_key_quota_used_after         DECIMAL(24,10),
    api_key_reserved_5h_after        DECIMAL(24,10),
    api_key_reserved_1d_after        DECIMAL(24,10),
    api_key_reserved_7d_after        DECIMAL(24,10),
    metadata                         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at                       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT usage_billing_ledger_operation_check
        CHECK (operation IN ('reserve', 'capture', 'release', 'renew', 'reconcile')),
    CONSTRAINT usage_billing_ledger_component_check
        CHECK (component IN ('base', 'management_fee')),
    CONSTRAINT usage_billing_ledger_funding_check
        CHECK (funding_source IN ('balance', 'subscription')),
    CONSTRAINT usage_billing_ledger_amount_check
        CHECK (amount >= 0 AND capture_delta >= 0
            AND lease_epoch >= 1 AND row_version >= 1)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_billing_ledger_operation_component
    ON usage_billing_ledger (reservation_id, operation_key_hash, component);

CREATE INDEX IF NOT EXISTS idx_usage_billing_ledger_reservation_created
    ON usage_billing_ledger (reservation_id, created_at, id);

CREATE INDEX IF NOT EXISTS idx_usage_billing_ledger_created_at
    ON usage_billing_ledger (created_at);

COMMENT ON COLUMN usage_billing_reservations.management_fee_bps IS
    'Immutable 1,500 bps management-fee snapshot used by authorization and capture.';
COMMENT ON COLUMN usage_billing_reservations.uncapped_base_cost IS
    'Actual base amount before the authorization ceiling is applied.';
COMMENT ON COLUMN usage_billing_reservations.platform_overage_base_cost IS
    'Uncapped base amount absorbed by the platform above the customer capture.';
COMMENT ON COLUMN usage_billing_reservations.lease_epoch IS
    'Correctness fence; increments only when reconciliation takes ownership.';
COMMENT ON COLUMN usage_billing_reservations.row_version IS
    'Monotonic audit/CAS version incremented by every successful mutation.';
