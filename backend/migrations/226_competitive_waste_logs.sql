CREATE TABLE IF NOT EXISTS competitive_waste_logs (
    id BIGSERIAL PRIMARY KEY,
    logical_request_id VARCHAR(255) NOT NULL,
    upstream_request_id VARCHAR(255),
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    group_id BIGINT,
    winner_account_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    attempt_no INTEGER NOT NULL,
    model VARCHAR(100) NOT NULL,
    upstream_model VARCHAR(100),
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    cache_creation_5m_tokens INTEGER NOT NULL DEFAULT 0,
    cache_creation_1h_tokens INTEGER NOT NULL DEFAULT 0,
    image_output_tokens INTEGER NOT NULL DEFAULT 0,
    usage_reported BOOLEAN NOT NULL DEFAULT FALSE,
    competitive_waste_cost NUMERIC(20,10),
    reason VARCHAR(32) NOT NULL,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (logical_request_id, api_key_id, attempt_no)
);

CREATE INDEX IF NOT EXISTS idx_competitive_waste_logs_created_at
    ON competitive_waste_logs (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_competitive_waste_logs_account_created
    ON competitive_waste_logs (account_id, created_at DESC);
