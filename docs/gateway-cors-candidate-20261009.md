# Gateway-only browser CORS candidate

The user authorized implementation, GitHub/source synchronization and candidate construction, but explicitly prohibited switching the running image until later approval.

Browser/WebView model-list calls currently fail at OPTIONS /v1/models with HTTP 403. The change derives its scope from the routes added by RegisterGatewayRoutes, matching the request method (or Access-Control-Request-Method for preflight) and route path, including named parameters and wildcard subpaths. These API-key routes return Access-Control-Allow-Origin: * without Access-Control-Allow-Credentials. Panel, login, user, admin, payment, static and unregistered paths retain the configured origin policy. Existing API-key authentication and billing remain unchanged. Browser OpenAI, Gemini and Anthropic authentication/protocol headers are supported. This does not guarantee a specific third-party client's model/protocol compatibility.

Verification covers arbitrary and null origins, configured panel origins, every registered gateway route, unknown paths/methods and traversal paths, and actual API-key authentication with missing/invalid/valid keys and a cookie-only request. The baseline TestGatewayRoutesAdaptiveInboundDoesNot404LeafProtocols failure reproduces on unchanged main; it is unrelated to this change. Builds require GOEXPERIMENT=jsonv2 as the baseline imports encoding/json/jsontext.

Build over production image sha256:e8f9acf6dfcab55ace34151be7526456acdb0f2706592a3c9c4e4a44e9b18f9a using deploy/Dockerfile.gateway-cors-candidate and the verified linux/amd64 executable. Preserve embedded frontend, migrations, runtime settings, volumes and all pricing changes. Never compile from inherited image source: this image family retains old source under /app. Isolated candidate startup must use separate PostgreSQL/Redis containers, no production data/configuration mounts or credentials, and no paid generation requests.

## Verified candidate (not deployed)

- Compiled source: `8ff6cfb17f1aae0091c18b72672ff90606996c1d`; GitHub destination `chenshangyi1/sub2api1`, branch `main`.
- Candidate: `sub2api1:gateway-cors-8ff6cfb17-20261009`.
- Image ID: `sha256:5667a6fd1e60793eda3c830b6551ad9ddf37cd2e831c03133db166dc5f923139`.
- Executable SHA256: `7fbf4c5058684f254b93d1e0e92f2c4c6506ea9a206648298ef37aa1203b4051`; Linux/amd64, CGO disabled, Go 1.26.6, `GOEXPERIMENT=jsonv2`, `embed,timetzdata`; isolated `--version` reports 0.1.329 and the compiled source revision.
- Server source staging: `/opt/sub2api/build_context/billing-847ce5afa-full`; source backup `/opt/sub2api/backups/gateway-cors-source-20261009T043449Z`.
- Server candidate/evidence: `/opt/sub2api/build_context/outputs/gateway-cors-candidate` (executable, build log, image ID, source revision, version, binary hash and 17 isolated HTTP checks).

The 17 isolated HTTP checks pass for OpenAI model/chat/Responses, Anthropic and Gemini preflights, arbitrary/null origins, protocol headers, missing/invalid/valid API keys, cookie-only rejection, restricted admin/login/user/payment/unknown paths, invalid gateway methods and configured credentialed panel origins. Temporary app/PostgreSQL/Redis containers used an internal network, generated test-only credentials, no production mounts, and no paid generation calls. Only database structure and the migration ledger were exported read-only into an empty isolated database; no business rows, user keys, balances, settings or accounts were copied. Temporary containers/network were removed after verification.

Empty-database startup exposed the existing `027_usage_billing_consistency.sql` migration naming issue (CONCURRENTLY requires `_notx.sql`). This candidate does not change migrations; validation against the deployed schema/migration ledger passes. A new installation requires a separate migration repair. The initial Docker build with a bare image ID was rejected by BuildKit; the successful build uses the verified local base tag, checks its image ID first and disables pulls.

The production container remains `cefd4f8d52c1355bd0a3518fb80c5105fb8042c485a74cbab0d3acef6e99d423`, base image `sha256:e8f9acf6dfcab55ace34151be7526456acdb0f2706592a3c9c4e4a44e9b18f9a`, started `2026-10-07T16:21:19.207851802Z`, healthy with origin readyz HTTP 200. Compose/Caddy hashes and frontend release pointer are unchanged. The candidate preserves prior DeepSeek pricing and embedded frontend. The inherited `/app` sources are historical and must not be used to rebuild the executable.

A future rollout requires explicit user authorization, current backups, a read-only migration check with current production configuration and an app-only rollback plan. Building this candidate does not activate CORS online and does not guarantee third-party client model/protocol compatibility.
