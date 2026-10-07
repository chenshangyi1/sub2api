## 2026-10-07 - Task: SiliconFlow DeepSeek V3.2 base pricing and source synchronization

### What was done

Corrected V3.2 site base prices to 4/6/0.40 per million tokens, protected them from dynamic USD catalog and generic Flash rewrites, preserved explicit pricing overrides and effective multipliers. Audited other configured DeepSeek models without changing their prices. User selected sub2api1/main for GitHub synchronization. Server synchronization is source-only to its existing matching staging tree; no runtime deployment is authorized.

### Testing

New regression tests reproduced the wrong Flash rate before the fix. After the fix all four V3.2 regression tests passed using the package's production GoFiles and focused test file, with GOEXPERIMENT=jsonv2. The server command built successfully. Full unit package compilation is blocked by preexisting duplicate definitions and stale account-stats test APIs. Expanded tests exposed underscore-prefix normalization and V4 date-suffix catalog matching failures; the same failures were reproduced with the unchanged 847ce5afa billing source through a Go overlay. See the audit note. Runtime preservation is checked during source synchronization.

### Notes

Changed billing_service.go, moved/expanded the V3.2 tests out of deepseek_pricing_test.go into deepseek_v32_pricing_test.go, and added the audit/handoff/progress notes. Local rollback is a revert of this operation's commit; server source rollback uses its operation backup. No historical balances/bills, group settings, compose files or running images were changed.

## 2026-10-07 - Task: Local gateway verification and candidate-image preparation

### What was done

The user authorized building a new image after local tests, while retaining the running production container. Added three V3.2 regressions for the actual gateway token-cost path, zero/cache-only requests and concurrent partial/free group overrides. Added a minimal candidate-image recipe and a Dockerfile-specific context filter. Compared all backend Go/module source files and all 221 frontend files with the running image; runtime-source differences are confined to this pricing change.

### Testing

All seven focused V3.2 tests passed, including three repetitions with the Go race detector. The real-bill token tuple 23/137/4352 produces base cost 0.0026548 and actual cost 0.00238932 at multiplier 0.9. Expanded billing/tier/gateway regressions reported 210 passing test/subtest events and two Grok 4.6 failures. Both Grok failures were reproduced against unchanged 847ce5afa source with an overlay and identical mismatches, confirming they predate this change. Full-package compilation limitations and previously verified V4 baseline failures remain unresolved; no claim of a globally green suite.

### Notes

Changed the focused test, DEPLOYMENT_HANDOFF.md and added deploy/Dockerfile.deepseek-v32-candidate plus its .dockerignore. A candidate will replace only /app/sub2api over the verified current runtime image; construction and isolated version checks do not deploy it. Test evidence is in ignored work files. Source rollback is a revert of this task’s commits; existing server source backup remains /opt/sub2api/backups/v32-source-20261007T081207Z. The unrelated api_key file remains untouched.

## 2026-10-07 - Task: Candidate image built and isolated Linux checks passed

### What was done

Built sub2api1:deepseek-v32-cny-9c3c6fbd8-20261007 from the canonical server build root, over the exact current runtime image. Replaced only the executable with the locally validated linux/amd64 artifact from source 9c3c6fbd809441d657da26e9039f29ca8c3ab009. Recorded image/executable hashes, version and backups in DEPLOYMENT_HANDOFF.md. No rollout occurred.

### Testing

Candidate image ID e8f9acf6dfcab55ace34151be7526456acdb0f2706592a3c9c4e4a44e9b18f9a; executable SHA256 b8cf200a71a28313d7978d8da0b3953756d5e14cbb9e925ce1bffa67e64d9f9b. Isolated version and checksum checks passed. All seven pricing regressions also passed as a cross-compiled Linux test executable inside the read-only, network-disabled candidate, without production volumes or credentials. Runtime image configuration matched the base, and the existing production container's identity/start time/image remained unchanged with readiness HTTP 200. This supplements the local/race tests; unrelated baseline failures remain as previously documented.

### Notes

This step changes only handoff/progress documentation locally; uploaded binaries and test evidence reside under the existing server build root's outputs/deepseek-v32-candidate. The unused image can be discarded without runtime rollback. Source sync backup: /opt/sub2api/backups/v32-candidate-20261007T082356Z. Old /app Go source is inherited in the binary-overlay image, so future compilation must use the recorded repository revision. No service restart or production data write was performed.
