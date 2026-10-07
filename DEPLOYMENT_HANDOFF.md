# xinmc pricing candidate handoff — 2026-10-07

This handoff records the current operation, superseding old runtime assumptions only where explicitly verified below. No credentials are stored here.

- Local checkout: `/Users/chenmingxin/Downloads/sub2api-main 2`.
- User-selected GitHub destination: `https://github.com/chenshangyi1/sub2api1`, branch `main`; starting commit `847ce5afaa0d111e170406c32712fc7417d6f62d`.
- Authorized scope: correct SiliconFlow V3.2 base pricing, push source, and sync source to the server. Other DeepSeek models were audited, not repriced. Group/user multipliers are preserved.
- See `docs/deepseek-pricing-audit-20261007.md` for policy, verified sources, outstanding issues and test limitations.

## Verified server state before synchronization

- SSH origin: `root@216.22.13.148:22`, existing authentication via xinmc helper with host checking enabled.
- Running container ID: `0abe6b4db7624f9c7ec2e7fa4a8b587aefa97c59b530ba3c58f37ddc0cae6811`.
- Running image: `sub2api1:deepseek-standard-pricing-20261007-2`.
- StartedAt: `2026-10-06T19:53:49.955885423Z`; healthy, origin `/readyz` HTTP 200.
- Runtime Compose: `/opt/sub2api/deploy/docker-compose.local.yml`. Root checkout and Compose have intentional local changes; never reset or overwrite them.
- `/opt/sub2api/build_context` also has local modifications and an older Git baseline. Do not treat it as aligned with local main.
- Existing source staging tree `/opt/sub2api/build_context/billing-847ce5afa-full` matches the local starting billing source and test hashes. Reuse this tree for source-only synchronization; do not create another parallel source tree. It is not the canonical build root. The user subsequently authorized a candidate image build after local verification, but did not authorize switching the running service.

## Synchronization and rollback

Transfer only the committed changed source/tests and this operation's notes after checking the prior file hashes. Preserve all existing root, Compose, Caddy, environment, data and frontend paths. Save pre-change copies in a root-only `/opt/sub2api/backups/` directory and record the exact source commit in the staging tree's `SOURCE_REVISION`.

Recheck the container ID, image, start timestamp and readiness after transfer. They must remain unchanged. The source update does not activate the new prices; current production pricing continues until an explicitly authorized rollout.

For source rollback, use the recorded backup to restore the two previously existing changed files and remove only files newly introduced by this operation. Do not restore the database or change the running container. Before a future rollout, reconcile the canonical build root and review the supplier-specific pricing policy, migrations, known full-test failures and runtime rollback plan.

## Candidate build authorized after local verification

The candidate uses a locally cross-compiled linux/amd64, CGO-disabled binary with `embed timetzdata` tags and GOEXPERIMENT=jsonv2. Before compilation, all 2,883 backend Go/module files present in the running image were compared with local source: only this operation’s billing source and test differed, with the new V3.2 test present locally. All 221 embedded frontend files matched the running image byte for byte (excluding macOS metadata). No other runtime-source change is included.

Package the binary from `/opt/sub2api/build_context/outputs/deepseek-v32-candidate/sub2api` using `deploy/Dockerfile.deepseek-v32-candidate` from the canonical build root. Its Dockerfile-specific ignore file admits only that binary. The base must remain image ID `sha256:7ef469688d398f748e7cca40a8b105f3a6a4e2a506e0f4ec2953c83dbe6e79d7`, tag `sub2api1:deepseek-standard-pricing-20261007-2`. This preserves existing runtime dependencies and files while replacing only the executable. Record the candidate tag, source revision, binary hash and isolated version check after construction.

V3.2 regression coverage now includes the actual gateway calculation path with and without its pricing resolver, a real bill’s token counts, zero multiplier, zero-token/cache-only requests, partial/zero custom cards and concurrent group isolation. Expanded existing billing and tier tests must be reported separately from unrelated baseline failures; full-package test compilation still has the previously recorded blockers. Building a candidate does not activate prices. Do not restart the app, change Compose, mount production volumes into a candidate, or connect a candidate to production databases.

## Verified candidate result (built, not deployed)

- Candidate tag: `sub2api1:deepseek-v32-cny-9c3c6fbd8-20261007`.
- Image ID: `sha256:e8f9acf6dfcab55ace34151be7526456acdb0f2706592a3c9c4e4a44e9b18f9a`.
- Compiled source revision: `9c3c6fbd809441d657da26e9039f29ca8c3ab009`; application version 0.1.329; built at 2026-10-07T08:23:01Z. Later documentation-only commits do not change this executable.
- Executable SHA256: `b8cf200a71a28313d7978d8da0b3953756d5e14cbb9e925ce1bffa67e64d9f9b`. Local ELF and Go build information confirm linux/amd64, CGO_ENABLED=0 and embed/timetzdata. Git's dirty marker arises from the unrelated untracked api_key file; tracked source was clean at compilation.
- Candidate artifact/evidence directory: `/opt/sub2api/build_context/outputs/deepseek-v32-candidate`; `candidate_build.json`, `provenance.json`, `isolated-pricing-test.txt` and the executable are retained there.
- Source-sync backup for this verification/build step: `/opt/sub2api/backups/v32-candidate-20261007T082356Z`. Original price-patch backup remains `/opt/sub2api/backups/v32-source-20261007T081207Z`.
- Isolated candidate version execution and executable-hash check passed. Seven cross-compiled V3.2 regression tests passed in a read-only, network-disabled candidate container, with only its test executable mounted and no production configuration/data mounts.
- Runtime command, entrypoint, environment, healthcheck, working directory, user and exposed ports match the base image. Production container ID, image ID and StartedAt remain unchanged; healthy and origin /readyz returns HTTP 200. No Compose, database, group multiplier or static release change occurred.

This is a binary-overlay image: inherited `/app` Go source files are retained from the old image and are not the source of the new executable. For future builds, use the recorded GitHub revision/source staging, not the inherited `/app` source. The canonical server source tree's preexisting modifications were preserved; its candidate recipe consumes only the verified artifact.

No rollout is authorized. New pricing remains inactive online. A future authorized switch must preserve the old image/Compose and first review existing baseline test failures and migration behavior; this operation performed no database-connected app-start or real-money test. Discarding the unused candidate and its task-owned artifacts is sufficient to undo image preparation; do not restart production to roll back an unused image.
