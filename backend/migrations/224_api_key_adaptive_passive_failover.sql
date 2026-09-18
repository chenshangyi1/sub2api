-- Per-key opt-in for delaying Adaptive leaf changes until the third retryable failure.
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS adaptive_passive_failover_enabled BOOLEAN NOT NULL DEFAULT FALSE;
