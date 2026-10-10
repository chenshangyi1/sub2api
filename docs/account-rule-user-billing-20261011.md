# Account Rules for Customer Billing

## Behavior

An account stats pricing rule can now explicitly opt into customer billing with
`apply_to_user_billing`. Existing and new rules default to false. The rule uses
the account that served the request, so different suppliers can charge different
base prices for the same public model. Normal user/group multipliers still apply.

Pricing order is enabled account rule, channel card, group card, then global
pricing. All rules first try the sent upstream model, then the public billing
model. Within each model pass, rule order is preserved. Rules match any selected
account OR group; both empty means no match. For supplier-specific prices, select
only the supplier account and leave groups empty. The UI now states this rule.

This does not automatically scrape or synchronize suppliers' prices. Operators
must maintain each rule, including currency conversion, token units, input,
output and cache prices. The example 4.5/13.5 per million is test data, not a
verified current supplier tariff. Public model cards cannot predict the final
supplier selected by routing. This change does not alter their pre-request price
display. Operational cost accounting remains separate from customer multipliers.

Token, per-request, image and video pricing use the selected account context.
Account rules take precedence over legacy group media prices. Non-billable
upstream errors remain zero-cost even when Free Fast is enabled.

## Local Validation

- Production backend compiles with `GOEXPERIMENT=jsonv2 go build ./cmd/server`.
- Frontend `pnpm run build` passes i18n checks, vue-tsc and Vite.
- Migration content/default compatibility test passes.
- A temporary copy containing all production source and the resolver, channel,
  OpenAI usage, account-rule and helper test files passes ordinary tests after
  excluding the existing Normalize/ExtractOpenAIServiceTier tests.
- Focused account-rule, resolver and Free Fast tests pass with `-race`.
- Tests cover two suppliers, upstream-name priority, public-alias fallback,
  disabled rules, unmatched accounts, zero multipliers, token costs, group media
  overrides and actual RecordUsage zero charging for rejected Free Fast requests.
- For 35,435 input and 1,120 output tokens, 4.5/13.5 per million produces
  0.1745775 base and 0.043644375 at multiplier 0.25; 9/27 produces 0.349155 base.

Full repository tests are not passing. Existing service test compilation failures
include duplicate normalizeGroupModelsListConfig, outdated account pricing test
fields/signatures, and missing image cooldown/helper names. Expanded race testing
also detects unsynchronized mock counters in the unchanged channel cache test.
The unchanged ultrafast service-tier tests fail/panic because their expectations
do not match current normalization. These were preserved, not treated as passes.
Browser interaction was not exercised; frontend verification is build/type/i18n.

## Candidate Scope

Authorized operations are GitHub push to `chenshangyi1/sub2api1/main`, source sync
into `/opt/sub2api/build_context/billing-847ce5afa-full`, and candidate construction.
No production container replacement/restart, Compose/Caddy/static pointer update,
production database mutation, paid provider call or live migration is authorized.

The build uses the pinned running gateway-cors image's Go toolchain and module
cache with source from staging. It embeds the new frontend and SQL migration.
The candidate also includes the previously committed, undeployed Grok build-fast
base pricing change in local main. It overlays only `/app/sub2api` on the pinned
runtime base; inherited `/app` source is old and must not be used for rebuilding.

Before a future rollout, validate fresh backups and the full migration ledger.
The separate Caddy static release must also be updated to expose the new toggle.
Building this image does not activate the feature or configure supplier tariffs.

## Verified Candidate, Not Deployed

- Implementation revision: `eb6b9fba19a6d77e2fe6216199bd93e5458cd024`, pushed
  and verified on `chenshangyi1/sub2api1/main`.
- Image tag: `sub2api1:account-rule-eb6b9fba1-20261011`.
- Docker image ID: `sha256:b4f5d8a64330a9981b2d2de96297619379275244fd6cbe61dac517860a402f2c`.
- Executable SHA256: `28c7eba46eb219a70e3d39fbf7e80fa1d8cd63310d6b35fe8a2cf48603209a05`.
- Go toolchain: `go1.27.0 linux/amd64`, CGO disabled, `embed timetzdata`, jsonv2.
- Evidence: `/opt/sub2api/build_context/outputs/account-rule-eb6b9fba1-20261011`.
- Source/frontend backup: `/opt/sub2api/backups/account-rule-source-20261010T161626Z`.
  The UTC timestamp is October 10; local operation date is October 11 Singapore.
- Build recipe and isolation checks:
  `/opt/sub2api/build_context/account-rule-upload-20261011/build-account-rule-candidate.sh`.
  Its SHA256 is `bd4af4a6838c4735497af58f2b62162cb52adf59c35248df904b01fc8c564f8b`.

Transferred archives were checksum-verified before extraction. All 3,425
source/module/SQL/embedded frontend files were verified against the local
manifest; staging has no extra Go or SQL files. Previous changed source and the
embedded frontend were backed up before replacement. Canonical server checkout
and its intentional local changes were preserved. The image uses the verified
local base tag, checked against its pinned ID; BuildKit cannot use a bare image ID
as the FROM reference. Compile logs, source manifest, version, binary hash, runtime
configuration comparison, migration and pricing results are retained in evidence.

The read-only, network-disabled candidate passed 22 top-level pricing tests.
Its runtime environment, entrypoint, command, healthcheck, working directory,
user, ports, volumes and stop signal match the pinned base. An isolated PostgreSQL
container with tmpfs storage, no network or production mounts verified the new
migration's existing-row default, new-row default, stored true value and repeated
execution. The test container was removed. This tests the new migration against
a minimal table, not the application's entire migration history.

Before/after production snapshots are identical: application container
`107ee164bb285c77e4392bf0d2c94f5a62e23323e5a07f6c969c54a452857f13`, gateway-cors
image `sha256:5667a6fd1e60793eda3c830b6551ad9ddf37cd2e831c03133db166dc5f923139`,
StartedAt `2026-10-09T05:27:47.029166888Z`, restart 0, healthy. Postgres and Redis
container IDs, images, timestamps and restart counts also match. Compose/Caddy
hashes and frontend pointer are unchanged. Origin/public readyz return 200.
No deployment, live migration, supplier rule/balance write or service restart
occurred. Resource-limited building consumes server resources; health checks do
not establish zero latency impact for every production request.

For source-only rollback, extract the backup source/frontend archives into the
same staging tree, remove only the new paths listed in backup `added.txt`, and
restore backup SOURCE_REVISION. Never restore production data for source rollback.
