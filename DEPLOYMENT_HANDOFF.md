# xinmc source-only handoff — 2026-10-07

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
- Existing source staging tree `/opt/sub2api/build_context/billing-847ce5afa-full` matches the local starting billing source and test hashes. Reuse this tree for source-only synchronization; do not create another parallel source tree. It is not the canonical build root and no build is authorized now.

## Synchronization and rollback

Transfer only the committed changed source/tests and this operation's notes after checking the prior file hashes. Preserve all existing root, Compose, Caddy, environment, data and frontend paths. Save pre-change copies in a root-only `/opt/sub2api/backups/` directory and record the exact source commit in the staging tree's `SOURCE_REVISION`.

Recheck the container ID, image, start timestamp and readiness after transfer. They must remain unchanged. The source update does not activate the new prices; current production pricing continues until an explicitly authorized rollout.

For source rollback, use the recorded backup to restore the two previously existing changed files and remove only files newly introduced by this operation. Do not restore the database or change the running container. Before a future rollout, reconcile the canonical build root and review the supplier-specific pricing policy, migrations, known full-test failures and runtime rollback plan.
