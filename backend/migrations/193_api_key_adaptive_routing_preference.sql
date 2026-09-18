-- Per-key Adaptive routing preference: intelligence (default, high price first)
-- or price (low rate first). Manual intelligence order uses membership sort_order
-- as a secondary key / future calibration hook.

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS adaptive_routing_preference VARCHAR(20) NOT NULL DEFAULT 'intelligence';

ALTER TABLE api_keys
    DROP CONSTRAINT IF EXISTS api_keys_adaptive_routing_preference_check;

ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_adaptive_routing_preference_check
    CHECK (adaptive_routing_preference IN ('intelligence', 'price'));

-- Pool-level switch: when true, intelligence mode ranks by membership sort_order
-- (admin manual intelligence calibration) instead of rate_multiplier descending.
ALTER TABLE adaptive_group_configs
    ADD COLUMN IF NOT EXISTS use_manual_intelligence_order BOOLEAN NOT NULL DEFAULT FALSE;
