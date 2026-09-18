-- 233_add_group_monitor_result_history.sql
-- 分组监控检测历史：每次探测追加一条，支撑 1h/1d/7d 可用率、TTFT、缓存率与历史状态条。

CREATE TABLE IF NOT EXISTS group_monitor_result_history (
    id                    BIGSERIAL PRIMARY KEY,
    monitor_id            BIGINT NOT NULL REFERENCES group_monitors(id) ON DELETE CASCADE,
    account_id            BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    status                VARCHAR(20) NOT NULL DEFAULT 'unknown',
    latency_ms            BIGINT NOT NULL DEFAULT 0,
    ttft_ms               BIGINT NOT NULL DEFAULT 0,
    input_tokens          BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens     BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    checked_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_gmrh_monitor_checked
    ON group_monitor_result_history(monitor_id, checked_at DESC);
CREATE INDEX IF NOT EXISTS idx_gmrh_monitor_status
    ON group_monitor_result_history(monitor_id, status);
