# HANDOFF 2026-08-21 — 站点 502 已恢复 + 两个待合并改动

> 给在 `custom/sub2api` 上继续完善 sub2api 的会话/项目。

## 1. 站点状态：已恢复

- 22:47 部署 `local/sub2api:groupmonitor-v178-20260820-19` 后出现过 502（重启窗口），现容器 healthy、0 重启、健康检查 301 正常，无需再处理。

## 2. 运行中镜像缺一个修复：gemini 池模式 429 冷却

- 运行中的 `groupmonitor-v178-20260820-19` 二进制里 **没有** `geminiPoolMode429FallbackCooldown`（grep 二进制 = 0 处）。
- 服务器源码树里 **有**（`/opt/sub2api/source/sub2api/backend/internal/service/gemini_messages_compat_service.go`，3 处引用，含 `skippedErrorPolicyFailoverError` 里的 429 冷却 + 立即换号逻辑）。
- 结论：**下次从服务器 source 树构建镜像即自动合并**，无需手工操作。若从本地 custom 树打包上传覆盖 source 树，请先同步该文件（本地 custom 树是 `poolModeSkippedFailoverError` 血统，与服务器 `skippedErrorPolicyFailoverError` 血统不一致，直接覆盖会丢 429 冷却且引入血统错位）。

## 3. 本地 custom 树缺少 account_failure_guard.go（重要）

- 服务器 source 树已含连续失败守卫（`internal/service/account_failure_guard.go` + `internal/repository/account_failure_counter_cache.go` + `internal/repository/account_repo.go::ListCooledDownAccounts` + `ratelimit_service.go`/`gemini_messages_compat_service.go` 挂点 + config 9 项 env），**当前运行的 -19 镜像已包含此功能**（二进制 25 处引用）。
- 本地 custom 树 **没有** 这些文件。若下次从本地树全量覆盖服务器 source 树，此功能会丢失。合并时请保留服务器侧这几个文件（或从服务器 source 树拉取）。

## 4. 连续失败守卫行为速览（供合并后验收）

- 429/5xx/520-524 连续失败 5 次/10 分钟窗口 → 账号写 `TempUnschedulableUntil`（临时不可用，默认冷却 600s，到期自动恢复），**不写 StatusError**。
- 每 5 分钟扫冷却中账号（reason 前缀 `auto-disabled:`）做健康探测，成功提前解冷却并清计数。
- 防抖：恢复后 30 分钟内再犯阈值翻倍（上限 8x）。
- env：`gateway.account_failure_guard_*`（enabled/threshold/window_seconds/sample_interval_seconds/cooldown_seconds/recovery_interval_seconds/debounce_seconds/max_multiplier/recovery_batch_size/probe_timeout_seconds）。
- 日志：`account_failure_guard_cooled_down` / `account_failure_guard_recovered`。
- 单测：`internal/service/account_failure_guard_test.go`（11 个用例）。

## 5. 其他

- 服务器备份：`/opt/sub2api/backups/failure-guard-20260821/`（我的改动上传前状态）、compose 备份 `docker-compose.yml.bak-failure-guard-20260821`。
- 存量问题（与本次无关）：`cmd/server/wire.go` 引用了已删除的类型 `service.ChannelMonitorV2Aggregator`，wire 工具无法重新生成 wire_gen.go，改 ProviderSet 后需手工维护注入代码。建议清理。
