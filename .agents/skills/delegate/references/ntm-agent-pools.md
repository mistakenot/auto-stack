# NTM agent pools — planners and workers

How to set up and target `ntm` (Named Tmux Manager) sessions as two
**label-separated agent pools** — **planners** and **workers** — so that hooks
can enforce which git operations each pool is allowed to perform.

This is the *pool model*: how sessions are organised and labelled. For the
mechanics of discovering panes, identifying each agent's type, and sending text
or slash commands once you have a session, see
[references/delegating-to-agents.md](references/delegating-to-agents.md). The
delegate family — [/delegate](../delegate/SKILL.md),
[/delegate-task](../delegate-task/SKILL.md), and
[/status-report](../status-report/SKILL.md) — dispatches into the
**workers** pool described here.

## Why labels matter

Labels are not cosmetic. Git hooks (installed by `ntm init`) read a pane's
**label** to decide which operations it may perform. A pane spawned **without**
a label gets none of the pool rules applied. **Always pass `--label` when
spawning or adding panes** — otherwise planners could push feature branches, or
workers could commit straight to `main`, and nothing would stop them.

## Pool model

| Pool         | Label       | Default count        | Works on   | Allowed operations                                              |
| ------------ | ----------- | -------------------- | ---------- | --------------------------------------------------------------- |
| **Planners** | `planners`  | 4 (2 cc + 2 cod)     | `main`     | Read code, analyse, write planning docs, commit to `main`       |
| **Workers**  | `workers`   | 0 (added on demand)  | worktrees  | Check out branches, create worktrees, open PRs                  |

**Planners** do deep analysis and write planning docs. They work directly on
`main` — since they rarely touch the same files, conflicts are uncommon. The
planning, review, and commit skills (`new-task`,
`new-solution`, `new-plan`, `review-task`,
`commit-task`) run in a planner pane.

**Workers** execute tasks in isolated worktrees with their own branch and
tooling, and open PRs when complete. `execute-task` runs in a worker
pane. **Workers must never work in the primary checkout** — always a worktree
(see [references/worktree-conventions.md](references/worktree-conventions.md)).

Session naming follows `<project>--<label>`, e.g. `auto-stack--planners` and
`auto-stack--workers`. This supersedes the older single `<project>--execute`
session: the worker pool *is* the execute pool, now explicitly labelled.

## Prerequisites

- `ntm` installed and on PATH (`ntm deps` to verify).
- A project directory under the configured `projects_base` (default `~/src`).
- `ntm init` run in the project directory — sets up `.ntm/` config and the git
  hooks that enforce the label rules above. Without `ntm init` the labels carry
  no enforcement.

## Spawning the planner pool

```bash
ntm spawn auto-stack --label planners --cc=2 --cod=2
```

Creates a tmux session named `auto-stack--planners` with four agent panes
(2 Claude Code, 2 Codex) plus a user pane.

## Adding workers on demand

Workers start as an empty pool — add capacity only when a task is dispatched,
rather than pre-allocating a large pool. Add **one** worker per dispatch (never
spawn repeatedly in a loop):

```bash
ntm add auto-stack --label workers --cc=1
```

Or spawn a dedicated worker-pool session up front, giving each agent its own git
worktree automatically:

```bash
ntm spawn auto-stack --label workers --cc=2 --worktrees
```

The on-demand add is the path the delegate skills follow when no eligible worker
is free. The warm-floor / ceiling GC policy for the worker pool (don't shrink
below 4, never exceed 6, reclaim surplus idle panes in `status-report`)
is documented in
[references/delegating-to-agents.md](references/delegating-to-agents.md#9-reclaiming-panes-garbage-collection).

## Sending prompts to a pool

Address a session directly by its `<project>--<label>` name. The send/clear
conventions per agent type are in
[references/delegating-to-agents.md](references/delegating-to-agents.md#5-send-text-to-an-agent).

```bash
# Send to all planners
ntm send auto-stack--planners "review the auth module and write findings"

# Send to all workers
ntm send auto-stack--workers "implement the auth refactor from task 042"

# Send to a specific agent type within a pool
ntm send auto-stack--planners --cc "focus on the Go packages"
```

## Listing sessions

```bash
ntm list                          # all sessions
ntm list --project auto-stack     # both pools for one project
```

Prefer the JSON forms (`ntm list --json`, `ntm status <session> --json`) when
parsing programmatically — the human tables are not stable. See
[references/delegating-to-agents.md](references/delegating-to-agents.md#1-find-sessions-you-can-delegate-to).

## Key points

- **Always set `--label` when spawning or adding.** Hooks read labels to enforce
  pool rules — without a label the rules don't apply.
- **Planners commit directly to `main`. Workers use branches and open PRs.**
- **Workers never work in the primary checkout** — always a worktree.
- **Add workers incrementally** as tasks are dispatched, rather than
  pre-allocating a large pool.
