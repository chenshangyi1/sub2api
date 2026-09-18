-- Per-key Adaptive budget: exclude leaves whose rate_multiplier is above this
-- ceiling. NULL means no budget limit (all pool leaves eligible).
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS adaptive_max_rate_multiplier NUMERIC(10, 4) NULL;

ALTER TABLE api_keys
    DROP CONSTRAINT IF EXISTS api_keys_adaptive_max_rate_multiplier_check;

ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_adaptive_max_rate_multiplier_check
    CHECK (
        adaptive_max_rate_multiplier IS NULL
        OR adaptive_max_rate_multiplier >= 0
    );
