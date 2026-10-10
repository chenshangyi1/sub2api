-- Opt-in: a per-account stats pricing rule may also price successful customer usage.
-- Existing rules remain operational-cost-only until explicitly enabled.
ALTER TABLE channel_account_stats_pricing_rules
    ADD COLUMN IF NOT EXISTS apply_to_user_billing BOOLEAN NOT NULL DEFAULT FALSE;
