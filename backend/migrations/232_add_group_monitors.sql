-- 232_add_group_monitors.sql
-- 分组级渠道监控：一个分组 = 一个监控配置，自动检测组内所有账号的健康状态。

CREATE TABLE IF NOT EXISTS group_monitors (
    id                BIGSERIAL PRIMARY KEY,
    group_id          BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    enabled           BOOLEAN NOT NULL DEFAULT true,
    interval_minutes  INT NOT NULL DEFAULT 30,
    model_id          VARCHAR(100) NOT NULL DEFAULT '',
    auto_recover      BOOLEAN NOT NULL DEFAULT false,
    max_output_tokens INT NOT NULL DEFAULT 16,
    last_run_at       TIMESTAMPTZ,
    next_run_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_group_monitors_group_id
    ON group_monitors(group_id);
CREATE INDEX IF NOT EXISTS idx_group_monitors_enabled_next_run
    ON group_monitors(enabled, next_run_at) WHERE enabled = true;

-- 分组监控下每个账号的最新检测状态（upsert 语义，每账号一行）。
CREATE TABLE IF NOT EXISTS group_monitor_results (
    id            BIGSERIAL PRIMARY KEY,
    monitor_id    BIGINT NOT NULL REFERENCES group_monitors(id) ON DELETE CASCADE,
    account_id    BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    status        VARCHAR(20) NOT NULL DEFAULT 'unknown',
    model_id      VARCHAR(100) NOT NULL DEFAULT '',
    latency_ms    BIGINT NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    checked_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_group_monitor_result UNIQUE (monitor_id, account_id)
);

CREATE INDEX IF NOT EXISTS idx_group_monitor_results_monitor
    ON group_monitor_results(monitor_id, status);
