# Model Capability And Upstream Integration

## Local Checkpoint

- Canonical source: repository root, imported from production as commit `4f71582`.
- Branch: `fix/model-capability-routing`.
- Model snapshots now distinguish unknown, fresh and stale observations.
- Unknown accounts retain the existing whitelist rules; fresh/stale observations additionally filter the final upstream model.
- Shared gates cover ordinary, mixed, fallback, adaptive, OpenAI legacy/advanced, and previous-response sticky selection.
- The existing-account sync endpoint persists the complete list before reporting success; failed refresh keeps the prior successful list and timestamp.
- Observations are bound to account, credential, endpoint and proxy identity by a digest. Account creation/duplication and ordinary/bulk edits do not import client-provided observations.
- Non-stream protocol adaptation is still pending at this checkpoint.

## Verification

Focused tests passed on Go 1.26.6 (auto-selected by the installed Go launcher):

```text
go test -tags=unit ./internal/service ./internal/repository ./internal/handler/admin \
  -run 'TestUpstreamModelSnapshot|TestModelSnapshot|TestObservedModels|TestAccountSupportsUpstreamModel|SyncUpstreamModels' -count=1
```

RED runs reproduced both unsupported-account selection and missing sync persistence before implementation.

The broad service/handler/repository/apicompat run did NOT pass. Failures include existing API-key adaptive-field fixture mismatches, JSON error offsets and malformed-stream cases, model-plaza expectations, and a missing migration filename assertion. Baseline reproduction and classification are pending; these are not waived or claimed as passing.

## Upstream References

- Remote requested by user: https://github.com/kiss-kedaya/sub2api
- Imported version: `0.1.250`; upstream tag commit `645b6287cb14d985d94e247deb15b7fe597bd485`.
- Latest observed main: `705432cc8623262bd236313ac6bb3981b9a4e90b`, a version-only update.
- Latest observed stable release: `v0.1.258`, peeled commit `03fcb2572f2128b1a1ec144e872898a82803efc6`.
- The release adds three commits and changes 43 files, including API-key smart routing and dashboard query bounds.
- The production import contains additional adaptive routing, monitoring, billing and UI changes beyond v0.1.250. These must remain local deltas during the three-way merge.
- Upstream migration `239_api_key_group_routes.sql` shares its numeric prefix with a local payment migration; verify filename-based migration tracking before integrating.

## Deployment Boundary

No production source upload, database writes, image builds, restarts, deployment or remote push is authorized by this work. A future release needs an explicit deployment declaration and approval.
