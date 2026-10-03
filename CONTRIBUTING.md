# Parallel Development

This repository is developed with isolated branches and worktrees. Keep `main`
stable and integrate changes through pull requests or an explicitly assigned
integrator session.

## Session Rules

- Use one branch per task, for example `codex/scheduler-race` or `codex/ui-fix`.
- Use one worktree per branch. Never let two sessions edit the same worktree.
- Before editing, verify the worktree is clean and inspect recent commits.
- Keep each change focused. Do not include unrelated session changes.
- Commit tested changes with a concise imperative message.
- Push branches, not unreviewed changes directly to `main`.
- Rebase or merge only after checking the target branch has not advanced.
- Never commit credentials, runtime configuration, database dumps, generated
  dependencies, or production data.

## Suggested Worktree Layout

From a clean clone, create sibling worktrees such as:

```text
sub2api-repo/
sub2api-worktrees/
  scheduler-race/
  oauth-routing/
  frontend-monitor/
```

Each session owns one directory under `sub2api-worktrees`. The integrator
reviews and merges the resulting branches into `main`.

## Verification

- Run the smallest relevant unit or integration tests before committing.
- Run formatting and static checks for touched languages.
- Record deployment-impacting changes separately from local development work.
- Production builds and deployments require explicit approval and must use the
  documented production build tree and rollback procedure.
