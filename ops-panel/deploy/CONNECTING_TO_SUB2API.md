# 面板连接 sub2api 的配置清单

面板（ops-panel）要完整工作，需要三条与 sub2api 的接入通道。缺任何一条，
对应功能会降级或报"未配置"。诊断中心 → 连接状态 会逐项自检这三条通道。

| 通道 | 环境变量 | 驱动功能 | 缺配置的表现 |
| --- | --- | --- | --- |
| 面板自身存储 | `DATABASE_URL`（PostgreSQL，`DATABASE_DRIVER=postgres`） | 面板自己的渠道/快照/财务/台账数据 | 面板无法启动 |
| sub2api 主库（只读） | `SUB2API_DATABASE_URL` | 目标分析（收益/成本/利润）、慢请求、账号健康回放、授信建档选人 | 分析页报"SUB2API_DATABASE_URL is not configured"，健康状态为空 |
| sub2api Admin API | `SUB2API_BASE_URL` + `SUB2API_ADMIN_API_KEY` | 账号列表/操作、增强探测、上游同步、API Key 导入 | 目标账号页报"没有可用的 admin key" |

面板后端与扩展 Worker **都需要** `SUB2API_DATABASE_URL`（Worker 用它直连主库做收益统计）。
面板后端在启动时额外校验主库的 `usage_logs` / `users` 表是否存在。

## 1. 只读主库账号（一次性的 SQL）

在 sub2api 的 PostgreSQL 实例上创建只读账号，面板只读接入，双保险：

```sql
CREATE ROLE sub2api_ops_readonly LOGIN PASSWORD '<替换为强密码>';
GRANT CONNECT ON DATABASE sub2api TO sub2api_ops_readonly;
GRANT USAGE ON SCHEMA public TO sub2api_ops_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO sub2api_ops_readonly;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO sub2api_ops_readonly;
```

注意：`usage_logs` / `users` / `accounts` / `groups` 在 sub2api 业务库；
`upstream_monitor_results`、`bl_*` 扩展表在面板自己的库，与本清单无关。

## 2. Compose 环境变量（在 ops-panel 与 ops-worker 两个服务上都加）

```yaml
environment:
  # 面板自身库（与现有 base compose 保持一致）
  DATABASE_DRIVER: postgres
  DATABASE_URL: ${DATABASE_URL:?set the ops-panel database URL}
  # sub2api 只读主库（面板后端 + worker 共用）
  SUB2API_DATABASE_URL: ${SUB2API_DATABASE_URL:?set the read-only sub2api database URL}
  # sub2api Admin API 默认目标（启动时自动注册首个同步目标）
  SUB2API_BASE_URL: ${SUB2API_BASE_URL:-https://baiyuan.cc.cd}
  SUB2API_ADMIN_API_KEY: ${SUB2API_ADMIN_API_KEY:-}
  SUB2API_SITE_NAME: ${SUB2API_SITE_NAME:-Default Sub2API}
  # 面板鉴权与加密
  AUTH_ENABLED: "true"
  APP_SECRET: ${APP_SECRET:?set a 32+ byte secret}
  ADMIN_USERNAME: ${ADMIN_USERNAME:-admin}
  ADMIN_PASSWORD: ${ADMIN_PASSWORD:?set a strong admin password}
```

重启面板与 Worker 后，打开 诊断中心 → 连接状态：

1. `面板数据库` 正常
2. `sub2api 主库（只读）` 正常（连接成功且存在 `usage_logs` / `users`）
3. `sub2api Admin API` 正常（对默认目标做鉴权 Ping）

## 3. 手工验证命令

```bash
# 主库只读连通 + 表存在
psql "$SUB2API_DATABASE_URL" -c "SELECT count(*) FROM usage_logs WHERE created_at > now() - interval '1 day';"

# Admin API 鉴权（与面板 Ping 同一端点：ListGroups + ListAccounts 各一次）
curl -s -o /dev/null -w '%{http_code}\n' -H "x-api-key: $SUB2API_ADMIN_API_KEY" \
  "$SUB2API_BASE_URL/api/v1/admin/groups/all?include_inactive=true"
```

面板内不保存 `SUB2API_DATABASE_URL` 明文；配置只存在于部署环境变量中。
