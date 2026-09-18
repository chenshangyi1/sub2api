-- Align every monetary column touched by Adaptive authorization and capture at
-- ten fractional digits. The wider integer range prevents a valid wallet value
-- from overflowing the reservation or audit ledger.

ALTER TABLE users
    ALTER COLUMN balance TYPE DECIMAL(24,10);

ALTER TABLE api_keys
    ALTER COLUMN quota TYPE DECIMAL(24,10),
    ALTER COLUMN quota_used TYPE DECIMAL(24,10),
    ALTER COLUMN rate_limit_5h TYPE DECIMAL(24,10),
    ALTER COLUMN rate_limit_1d TYPE DECIMAL(24,10),
    ALTER COLUMN rate_limit_7d TYPE DECIMAL(24,10),
    ALTER COLUMN usage_5h TYPE DECIMAL(24,10),
    ALTER COLUMN usage_1d TYPE DECIMAL(24,10),
    ALTER COLUMN usage_7d TYPE DECIMAL(24,10);

ALTER TABLE groups
    ALTER COLUMN daily_limit_usd TYPE DECIMAL(24,10),
    ALTER COLUMN weekly_limit_usd TYPE DECIMAL(24,10),
    ALTER COLUMN monthly_limit_usd TYPE DECIMAL(24,10);

ALTER TABLE user_subscriptions
    ALTER COLUMN daily_usage_usd TYPE DECIMAL(24,10),
    ALTER COLUMN weekly_usage_usd TYPE DECIMAL(24,10),
    ALTER COLUMN monthly_usage_usd TYPE DECIMAL(24,10);
