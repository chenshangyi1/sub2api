## 2026-10-07 - Task: SiliconFlow DeepSeek V3.2 base pricing and source synchronization

### What was done

Corrected V3.2 site base prices to 4/6/0.40 per million tokens, protected them from dynamic USD catalog and generic Flash rewrites, preserved explicit pricing overrides and effective multipliers. Audited other configured DeepSeek models without changing their prices. User selected sub2api1/main for GitHub synchronization. Server synchronization is source-only to its existing matching staging tree; no runtime deployment is authorized.

### Testing

New regression tests reproduced the wrong Flash rate before the fix. After the fix all four V3.2 regression tests passed using the package's production GoFiles and focused test file, with GOEXPERIMENT=jsonv2. The server command built successfully. Full unit package compilation is blocked by preexisting duplicate definitions and stale account-stats test APIs. Expanded tests exposed underscore-prefix normalization and V4 date-suffix catalog matching failures; the same failures were reproduced with the unchanged 847ce5afa billing source through a Go overlay. See the audit note. Runtime preservation is checked during source synchronization.

### Notes

Changed billing_service.go, moved/expanded the V3.2 tests out of deepseek_pricing_test.go into deepseek_v32_pricing_test.go, and added the audit/handoff/progress notes. Local rollback is a revert of this operation's commit; server source rollback uses its operation backup. No historical balances/bills, group settings, compose files or running images were changed.
