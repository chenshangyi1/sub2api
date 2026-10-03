-- Per-key Anti-Stall PRO tier. off = disabled for this key.
-- Admin configures parameters for basic/pro/ultra; users only pick a tier.
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS anti_stall_tier VARCHAR(20) NOT NULL DEFAULT 'off';

ALTER TABLE api_keys
    DROP CONSTRAINT IF EXISTS api_keys_anti_stall_tier_check;

ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_anti_stall_tier_check
    CHECK (anti_stall_tier IN ('off', 'basic', 'pro', 'ultra'));
