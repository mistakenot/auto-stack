# Worker pools — planners and workers

How agent panes are organised into two **role-separated pools**, so that git
hooks can enforce which operations each is allowed to perform. This is policy;
the herdr commands that implement it live in `references/herdr/`.

The delegate family —
[/delegate](../delegate/SKILL.md),
[/delegate-task](../delegate-task/SKILL.md), and
[/status-report](../status-report/SKILL.md) — operates on the
**workers** pool described here.

## Pool model

| Pool | Runs in | Works on | Allowed operations |
| ---- | ------- | -------- | ------------------ |
| **Planners** | the primary checkout | `main` | Read code, analyse, write planning docs, commit to `main` |
| **Workers** | a linked git worktree | a `task/NNN` branch | Check out branches, create worktrees, open PRs |

**Planners** do deep analysis and write planning docs, directly on `main` —
they rarely touch the same files, so conflicts are uncommon.
`new-task`, `new-solution`, `new-plan`,
`review-task` and `commit-task` run in a planner.

**Workers** execute tasks in isolated worktrees and open PRs.
`execute-task` runs in a worker. **Workers must never work in the
primary checkout** — see
[references/worktree-conventions.md](references/worktree-conventions.md).

## Role is derived from git, not from a label

herdr has no label-driven enforcement layer, and it does not need one. Role is a
property of the checkout:

- **primary checkout** (`git-dir == git-common-dir`) ⇒ **planner**
- **linked worktree** (`git-dir != git-common-dir`) ⇒ **worker**

This is the check `references/herdr/verify-worker.md` runs against a pane, and
the one a repo-shipped git hook should use to enforce the table above:

```sh
#!/bin/sh
# pre-commit, installed via core.hooksPath
gd=$(git rev-parse --git-dir); cd=$(git rev-parse --git-common-dir)
branch=$(git rev-parse --abbrev-ref HEAD)
[ "$gd" != "$cd" ] && role=worker || role=planner
if [ "$role" = worker ]  && [ "$branch" =  main ]; then
  echo "BLOCKED: worker (worktree) may not commit to main" >&2; exit 1; fi
if [ "$role" = planner ] && [ "$branch" != main ]; then
  echo "BLOCKED: planner (primary checkout) may only commit on main" >&2; exit 1; fi
exit 0
```

Deriving role this way rather than from a multiplexer's metadata means the rules
hold under herdr, under any other multiplexer, and under none. Labels
(`references/herdr/label-worker.md`) stay purely for human-readable
organisation.

## Workers are ephemeral — one per task

There is **no warm pool**. Each dispatch creates a worker; each completed task
reaps one. No floor to maintain, no ceiling to police, no reuse.

This is forced, not chosen. A worker that has run a task carries that task's
context, and there is no way to clear it in place: `/clear` is cooperative and
detonates mid-task, and an agent's permission mode is fixed at launch and cannot
be changed afterwards. See `references/herdr/reset-worker.md`. The only clean
worker is a new one.

The trade-off is paying agent startup latency per task instead of amortising it
across a warm pane — a few seconds, against the class of bug that pane reuse
caused.

## Prerequisites

- herdr **0.8.2+** on PATH, its server running.
- The agent integration installed for each agent you dispatch to
  (`herdr integration status`; `herdr integration install claude|codex`).
  Without it herdr cannot report status and every worker reads as `unknown`.
- A git repo, and a worktree location herdr can write to.

## Key points

- **Role comes from worktree-ness, enforced by a git hook** — not from labels.
- **Planners commit to `main`. Workers use branches and open PRs.**
- **Workers never work in the primary checkout.**
- **One worker per task, spawned on dispatch, reaped on completion.** Never
  reuse a worker for a second task.
- **Always launch with permission flags** — see
  `references/herdr/spawn-worker.md`. This is the most common way a dispatch
  fails silently.
