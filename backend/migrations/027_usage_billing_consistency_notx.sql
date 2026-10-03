-- 027_usage_billing_consistency_notx.sql
-- 补齐 027 主迁移引用的幂等唯一索引（CONCURRENTLY 构建，不阻塞写入）。
-- 生产库该索引已存在时 IF NOT EXISTS 直接跳过（no-op）。
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_request_id_api_key_unique
    ON usage_logs (request_id, api_key_id)
    WHERE request_id IS NOT NULL;
