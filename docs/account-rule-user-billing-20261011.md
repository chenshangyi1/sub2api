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
