# Tearing down a worker (herdr)

```bash
herdr pane      close  <pane_id>                  # one pane
herdr worktree  remove --workspace <id> [--force] # the git worktree
herdr workspace close  <workspace_id>             # the workspace and its panes
herdr session   stop   <name>                     # a whole session's daemon
herdr session   delete <name>                     # a stopped session's state
```

## `workspace close` does NOT remove the git worktree

Verified live: after `herdr workspace close <id>` on a workspace created by
`worktree create`, the pane was gone but the git worktree directory and its
branch were **still present**, and `git branch -D` refused with
`cannot delete branch '<b>' used by worktree at '<path>'`.

So a worktree worker takes an explicit teardown, worktree **first** — while the
workspace still exists to identify it:

```bash
herdr worktree remove --workspace "$WS" --force
herdr workspace close "$WS"          # may return workspace_not_found; see below
git -C <repo> branch -D task/NNN     # only once the branch is merged or abandoned
```

`worktree remove --workspace` closes the workspace along with the worktree, so
the following `workspace close` often returns
`{"error":{"code":"workspace_not_found"}}`. That is success, not failure — keep
the call for the case where the workspace outlives the worktree, and treat
`workspace_not_found` as a no-op.

Doing it in the other order strands the worktree: once the workspace is closed
there is no `--workspace` id left to pass, and you are down to plain git.

If the workspace is already closed, fall back to plain git:

```bash
git -C <repo> worktree remove --force <worktree-path>
git -C <repo> worktree prune
git -C <repo> branch -D task/NNN
```

`--force` discards uncommitted changes in the worktree. Check for unpushed work
before using it — reaping a worker whose PR was never opened loses the work.

## Non-worktree workers

An agent started into an existing shared workspace has no worktree of its own;
`herdr pane close <pane_id>` removes just that pane and leaves its siblings
alone.

## What not to close

Do not close workspaces, tabs, panes, or sessions you did not create. A herdr
session is shared with the user and with other agents — a stray
`workspace close` kills somebody's live work. Never run `herdr server stop`
against an active session: it stops every pane process in it.

Reap only workers that are genuinely finished — merged, abandoned, or verified
unhealthy. See the pool policy in
[references/worker-pools.md](references/worker-pools.md).
