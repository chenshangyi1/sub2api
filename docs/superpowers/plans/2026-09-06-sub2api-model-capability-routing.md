# Sub2API Model Capability Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use subagent-driven-development (recommended) or executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 固化本地唯一源码基线，并让所有分组的账号选择遵循账号实际上游模型能力，同时正确处理入站非流与上游 stream-only 能力差异。

**Architecture:** 在账号服务层增加可持久化的上游模型能力快照和统一判断函数；普通 GatewayService、OpenAI scheduler、Gemini compatibility 和 adaptive planner 都调用同一能力判断。转发层保留入站与上游 stream 状态分离，在 stream-only 账号上采用内部流式收集器生成非流响应。

**Tech Stack:** Go 1.26-compatible backend, Ent/PostgreSQL JSONB account extra, Gin HTTP handlers, existing SSE/Responses adapters, Go unit tests.

**Spec:** `docs/superpowers/specs/2026-09-06-sub2api-routing-baseline.md`

## Global Constraints

- 本地唯一源码基线是 `D:\\CodexWorkspace\\projects\\sub2api-server` 根目录；禁止把 `work\\integration-final` 或其他平行树当源码真相。
- 生产唯一构建树是 `/opt/sub2api/build_context`；本轮不向生产同步、不构建镜像、不写数据库、不重启容器。
- 未同步账号必须保持可调度；成功同步账号必须按同步列表和最终映射模型过滤；同步失败保留旧成功快照。
- 模型能力过滤必须覆盖普通分组、fallback 分组、混合分组和 adaptive 分组。
- 入站 `stream` 与上游 `stream` 必须独立；stream-only 上游必须能够服务入站非流请求并生成标准非流响应。
- 所有生产代码修改先有失败测试；每个任务完成后运行其列出的精确验证命令。
- 不得记录或提交 API key、access token、密码、生产配置或数据库凭据。

---

### Task 1: 固化唯一源码基线

**Files:**
- Modify: `.gitignore`
- Create: `docs/superpowers/specs/2026-09-06-sub2api-routing-baseline.md`
- Create: `docs/superpowers/plans/2026-09-06-sub2api-model-capability-routing.md`
- Create: Git repository metadata in the project root

**Interfaces:**
- Produces: a root Git repository whose baseline commit contains the synchronized source tree, excludes `work/`, `outputs/`, caches, logs, and backup artifacts, and records the online source/tag in `progress.md`.

- [ ] **Step 1: Write the baseline ignore rules and persistent design documents.**

  Keep source directories trackable. Ignore `/work/`, `/outputs/`, generated caches, operational logs, and backup Dockerfiles that are not source.

- [ ] **Step 2: Initialize Git and create the baseline commit.**

  Run from `D:\\CodexWorkspace\\projects\\sub2api-server`:

  ```powershell
  git init -b baseline
  git add -A
  git status --short
  git commit -m "chore: establish production-synced source baseline"
  ```

  Expected: only source/config/documentation files are committed; no `work/`, `outputs/`, credentials, cache, or tar files are staged.

- [ ] **Step 3: Verify baseline provenance.**

  ```powershell
  git status --short --branch
  git show --stat --oneline HEAD
  Test-Path backend/go.mod
  Test-Path frontend/package.json
  Test-Path Dockerfile
  ```

  Expected: clean `baseline` branch and all three source markers exist.

- [ ] **Step 4: Append the baseline operation to `D:\\CodexWorkspace\\progress.md`.**

  Include the source path, online tag `local/sub2api:datepicker-layer-20260905`, excluded artifacts, verification results, and rollback as `git revert`/restore from the baseline commit.

### Task 2: Add account upstream-model capability snapshot helpers

**Files:**
- Modify: `backend/internal/service/account.go`
- Create: `backend/internal/service/account_upstream_models.go`
- Test: `backend/internal/service/account_upstream_models_test.go`

**Interfaces:**
- Consumes: existing `Account.Extra`, `GetModelMapping`, `ResolveMappedModel`, and `IsModelSupported`.
- Produces: `Account.UpstreamModelCapability()`, `Account.SupportsUpstreamModel(requestedModel string)`, and normalization helpers for JSON-safe snapshot data.

- [ ] **Step 1: Write failing unit tests.**

  Cover:

  ```go
  func TestAccountSupportsUpstreamModel_UnknownAllowsModel(t *testing.T)
  func TestAccountSupportsUpstreamModel_FreshFiltersMappedModel(t *testing.T)
  func TestAccountSupportsUpstreamModel_StaleUsesLastSuccessfulSnapshot(t *testing.T)
  func TestAccountSupportsUpstreamModel_DeduplicatesAndNormalizesSnapshot(t *testing.T)
  ```

  A fresh snapshot containing `upstream-model-a` must reject `upstream-model-b`; a request mapped from `public-a` to `upstream-model-a` must pass. Missing snapshot must pass.

- [ ] **Step 2: Run the focused test and verify it fails for the missing helper.**

  ```powershell
  cd backend
  go test ./internal/service -run 'TestAccountSupportsUpstreamModel' -count=1
  ```

  Expected: compile failure because the new capability API does not exist.

- [ ] **Step 3: Implement the smallest snapshot helper.**

  Store the snapshot in `Account.Extra` using constants for `upstream_models`, `upstream_models_synced_at`, `upstream_models_sync_status`, and `upstream_models_source`. Treat only `fresh` and `stale` with a non-empty model list as restrictive; `unknown` remains allow-all. Compare normalized exact model IDs after applying account mapping.

- [ ] **Step 4: Run the focused tests and existing account model tests.**

  ```powershell
  go test ./internal/service -run 'TestAccountSupportsUpstreamModel|TestGatewayService_isModelSupportedByAccount' -count=1
  ```

  Expected: PASS.

### Task 3: Persist sync results and invalidate model caches

**Files:**
- Modify: `backend/internal/handler/admin/account_handler.go`
- Modify: `backend/internal/service/account_test_service.go` only if the sync result needs a reusable service helper
- Modify: `backend/internal/service/admin_account.go` only if extra validation/constants require it
- Test: `backend/internal/handler/admin/account_handler_upstream_models_test.go` or the existing account handler test file

**Interfaces:**
- Consumes: `FetchUpstreamSupportedModels`, `AdminService.UpdateAccountExtra`, and Task 2 constants/helpers.
- Produces: successful sync persists a fresh snapshot and failed sync keeps prior data while recording stale status; the HTTP response remains `{models: [...]}`.

- [ ] **Step 1: Write failing handler/service tests.**

  Cover successful persistence, failed sync without destructive clearing, source/status/timestamp fields, and JSON-safe model ordering.

- [ ] **Step 2: Run the focused tests to verify the persistence behavior is absent.**

  ```powershell
  cd backend
  go test ./internal/handler/admin ./internal/service -run 'UpstreamModels|SyncUpstream' -count=1
  ```

  Expected: FAIL on missing update calls or missing snapshot fields.

- [ ] **Step 3: Persist only after a non-empty successful upstream response.**

  Use `UpdateAccountExtra` with a normalized sorted model list and RFC3339 timestamp. On failure, update status/error metadata without deleting the last successful list. Do not persist credentials or upstream response bodies.

- [ ] **Step 4: Run the focused handler/service tests.**

  ```powershell
  go test ./internal/handler/admin ./internal/service -run 'UpstreamModels|SyncUpstream' -count=1
  ```

  Expected: PASS.

### Task 4: Apply one model-capability predicate to every scheduler

**Files:**
- Modify: `backend/internal/service/gateway_scheduling.go`
- Modify: `backend/internal/service/openai_gateway_scheduling.go`
- Modify: `backend/internal/service/openai_account_scheduler.go` only where a direct model check bypasses the shared predicate
- Modify: `backend/internal/service/gemini_messages_compat_service.go`
- Modify: `backend/internal/service/adaptive_route_planner.go`
- Modify: `backend/internal/service/gateway_model_availability.go`
- Test: `backend/internal/service/gateway_multiplatform_test.go`
- Test: `backend/internal/service/openai_account_scheduler_test.go`
- Test: `backend/internal/service/gemini_multiplatform_test.go`
- Test: `backend/internal/service/adaptive_route_planner_test.go` if present, otherwise the nearest adaptive planner test file

**Interfaces:**
- Consumes: Task 2 `Account.SupportsUpstreamModel` and existing platform/model mapping predicates.
- Produces: every account selection path filters known unsupported mapped models, while unknown accounts remain eligible.

- [ ] **Step 1: Add failing selection tests for each path.**

  Build cases with two accounts: account A has a fresh snapshot without the requested model, account B has a fresh snapshot with it; assert B is selected in ordinary, OpenAI scheduler, Gemini compatibility, fallback, and adaptive selection. Add an unknown account case and assert it remains selectable.

- [ ] **Step 2: Run the focused tests and confirm at least one candidate is incorrectly selected or the new tests do not compile.**

  ```powershell
  cd backend
  go test ./internal/service -run 'UpstreamModel|SelectAccount|Adaptive' -count=1
  ```

- [ ] **Step 3: Route all known-model checks through the shared predicate.**

  Preserve existing platform-specific checks, model rate-limit checks, endpoint capability checks, sticky-session behavior, and error classification. The shared predicate must run after mapping resolution and before account reservation. Do not add a database query inside the per-candidate hot path.

- [ ] **Step 4: Run all scheduler tests and race-safe focused tests.**

  ```powershell
  go test ./internal/service -run 'SelectAccount|Adaptive|ModelAvailability|UpstreamModel' -count=1
  go test -race ./internal/service -run 'SelectAccount|UpstreamModel' -count=1
  ```

  Expected: PASS.

### Task 5: Separate inbound and upstream stream modes with non-stream aggregation

**Files:**
- Modify: `backend/internal/service/openai_gateway_chat_completions.go`
- Modify: `backend/internal/service/openai_gateway_responses.go` or the actual Responses forwarding file identified by the existing symbols
- Modify: `backend/internal/service/openai_gateway_passthrough.go` only if shared request-body stream rewriting belongs there
- Modify: `backend/internal/handler/openai_chat_completions.go` and `backend/internal/handler/openai_gateway_handler.go` only for explicit mode propagation/logging
- Test: `backend/internal/service/openai_gateway_chat_completions_test.go`
- Test: `backend/internal/service/openai_gateway_responses_test.go` or the nearest existing Responses forwarding test

**Interfaces:**
- Consumes: Task 2 model capability, existing `SupportsOpenAIEndpointCapability`, SSE parsers, response adapters, and failover loop.
- Produces: stream-only accounts can serve `inbound_stream=false` through an internal streamed upstream request and a standard aggregated response; native non-stream accounts retain their direct path.

- [ ] **Step 1: Write failing tests for stream mode separation.**

  Cover:

  ```go
  func TestOpenAIForward_NonStreamInboundUsesStreamOnlyUpstreamAndAggregates(t *testing.T)
  func TestOpenAIForward_NonStreamAggregationPreservesToolCallsAndUsage(t *testing.T)
  func TestOpenAIForward_NativeNonStreamUpstreamRemainsNonStream(t *testing.T)
  ```

  Assert both the outgoing `stream` field and the final response shape, not just that a mock was called.

- [ ] **Step 2: Run focused tests and verify failure.**

  ```powershell
  cd backend
  go test ./internal/service -run 'NonStream|StreamOnly|ToolCalls|Usage' -count=1
  ```

  Expected: FAIL because stream-only capability is not yet translated into an aggregate path.

- [ ] **Step 3: Implement mode selection and aggregation using existing adapters.**

  Define `inboundStream` from the client body and derive `upstreamStream` from account capability. For stream-only/non-stream inbound, force upstream `stream:true`, consume all SSE events, preserve text/tool calls/usage/id/model/finish reason, then encode the endpoint-specific non-stream response. Keep cancellation and failover behavior intact.

- [ ] **Step 4: Run focused protocol tests and existing gateway tests.**

  ```powershell
  go test ./internal/service -run 'NonStream|StreamOnly|ToolCalls|Usage|OpenAI.*Forward' -count=1
  go test ./internal/handler -run 'OpenAI|Responses|Chat' -count=1
  ```

  Expected: PASS.

### Task 6: Add diagnostics, run broad verification, and record release readiness

**Files:**
- Modify: `backend/internal/service` request logging files touched by Tasks 4-5
- Modify: `docs/rule-changelog.md`
- Modify: `D:\\CodexWorkspace\\progress.md`
- Test: existing backend unit/integration test targets

**Interfaces:**
- Consumes: Tasks 2-5 behavior and test evidence.
- Produces: diagnosable request logs without credentials, a release-readiness report, and a clean local baseline diff suitable for later user-approved publication.

- [ ] **Step 1: Add/verify structured fields.**

  Log `requested_model`, `mapped_model`, `account_id`, `inbound_stream`, `upstream_stream`, `upstream_endpoint`, capability state, and failover reason at the existing request log sites.

- [ ] **Step 2: Run backend verification.**

  ```powershell
  cd backend
  go test ./internal/service ./internal/handler ./internal/repository -count=1
  go test ./internal/server/routes -tags=unit -count=1
  go test ./... -run 'Test.*(Gateway|Account|Scheduler|Model|Stream|Responses)' -count=1
  ```

  Expected: focused suites pass. Any pre-existing platform-specific failure is recorded with its exact command and output; it is not hidden.

- [ ] **Step 3: Check repository hygiene and source/build parity metadata.**

  ```powershell
  cd ..
  git status --short
  git diff --check
  git ls-files | Select-String '(^|/)(work|outputs|node_modules|gomodcache|gobuildcache)/|\\.env|secret|token|password'
  ```

  Expected: only intended source/docs changes; no credentials or generated caches.

- [ ] **Step 4: Append `progress.md` and `docs/rule-changelog.md`.**

  Record tests, known gaps, no-deploy status, changed files, rollback commit, and the rule that future releases originate from the local root baseline.
