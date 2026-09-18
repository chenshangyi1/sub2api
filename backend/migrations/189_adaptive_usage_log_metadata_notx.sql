-- 189_adaptive_usage_log_metadata_notx.sql
-- 补齐 189 主迁移引用的 adaptive reservation 查找索引（CONCURRENTLY 构建）。
-- 生产库该索引已存在时 IF NOT EXISTS 直接跳过（no-op）。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_adaptive_reservation_attempt
    ON usage_logs (adaptive_reservation_id, adaptive_attempt_no)
    WHERE adaptive_reservation_id IS NOT NULL;
