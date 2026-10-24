---
name: status-report
description: "Reports what every background worker is doing (herdr), flags unhealthy ones, and reaps finished ones. Use when 'status report', 'check executors', 'executor status', 'how are tasks going'."
---

# Worker Status Report

Monitor the background workers under herdr and report what each is doing.

> Part of the task planning workflow. See
> [references/workflow-overview.md](references/workflow-overview.md) for the
> full pipeline.
>
> **Mixed agents.** Workers may run Claude Code or Codex, whose output and
> interstitials differ — see
> [references/agent-conventions.md](references/agent-conventions.md).
>
> **Pool model.** Workers run in worktrees and open PRs; planners commit to
> `main`. This skill monitors and reaps **workers**. See
> [references/worker-pools.md](references/worker-pools.md).

## Input

Optional repo (defaults to the current one). A herdr session spans many
projects, so **always filter by repo** — see
`references/herdr/list-workers.md`.

## Status vocabulary

herdr reports status push-based, from the agent's own integration hook. Use it
rather than guessing from screen output.

| herdr `agent_status` | Report as | Notes |
| --- | --- | --- |
| `working` | **in progress** | A turn is running. |
| `done` | **finished a turn** | The normal resting state for a background worker — **not** an error, and far more common than `idle`. |
| `idle` | **finished a turn** | Same ready state as `done`, but the user has seen the tab. |
| `blocked` | **needs attention** | Waiting at an approval or question dialog. Read it and report what it is asking verbatim. Only keystrokes or a human can clear it — `references/herdr/unblock-worker.md`. |
| `unknown` | **unclassified** | herdr cannot classify it. **This does not mean finished** — read the pane. |

A separate axis, invisible to `agent_status`: a worker launched without
permission flags reports a healthy `done` while being unable to run a single
tool. Check it explicitly — Step 3.

## Workflow

### Step 1: Enumerate workers for this repo

See `references/herdr/list-workers.md`. Pull the structured fields in one pass —
`name`, `pane_id`, `agent`, `agent_status`, `cwd`, `terminal_title_stripped` —
and cross-reference `herdr worktree list --cwd <repo>` to map each worker to its
branch.

### Step 2: Fill in detail only where needed

Most of the report comes from the fields above. Read output
(`references/herdr/read-output.md`) only for workers that are `blocked`,
`unknown`, or otherwise ambiguous, and to pick up things that exist only in the
transcript — a PR URL, an error. Use `references/herdr/scan-output.md` when
looking for one pattern across the fleet.

### Step 3: Health-check each worker

For every worker, run `references/herdr/verify-worker.md`:

- **Running a shell, not an agent** → the agent exited. Report it as dead.
- **Launched without permission flags** (bare `claude` / `codex`) → report as
  **unhealthy: manual approval mode**. It will stall on its first tool call and
  cannot be repaired in place. Recommend reap-and-respawn.
- **cwd is the primary checkout, not a worktree** → report as misplaced; a task
  worker must never run there.

This check is the point of the report as much as the progress table is: a
mis-launched worker looks perfectly healthy in every status field.

### Step 4: Per-worker detail

For each worker determine:

- **Task ID** — from the `task/NNN` branch, the worktree path, or
  `/execute-task NNN` in its output.
- **Description** — the first heading of the task's plan doc.
- **Last activity** — `terminal_title_stripped`, or a summary of the last output
  block.
- **Suggested next step**:
  - finished + PR open → `/address-feedback`
  - feedback resolved → `/complete-task`
  - PR merged → reap it (Step 6)
  - `blocked` → what it is waiting on. If the task docs or the user's
    dispatch prompt already settle the answer, clear it per
    `references/herdr/unblock-worker.md` (`send-keys` one press at a time,
    re-reading to confirm each landed, never `esc`). If the decision
    is load-bearing — schema, scope, anything destructive — report the
    question and let the user answer it.
  - unhealthy → reap and respawn

### Step 5: Present results

```
| Worker   | Task | Description | Status      | Health | Last activity   | Next step         |
|----------|------|-------------|-------------|--------|-----------------|-------------------|
| task-434 | 434  | Add widget  | finished    | ok     | PR #87 created  | /address-feedback |
| task-435 | 435  | Fix auth    | in progress | ok     | Running e2e     | --                |
| task-436 | 436  | Cache layer | finished    | MANUAL | (never started) | reap + respawn    |
```

### Step 6: Reap finished workers

Workers are **ephemeral**: one worker per task, reaped when the task is done.
There is no warm pool to maintain and no floor to shrink toward — a worker's
context is dirty once it has run a task and cannot be cleared in place
(`references/herdr/reset-worker.md`), so keeping it idle buys nothing and costs
a full agent process.

Reap a worker only when **all** of:

1. Its PR is **merged**, or the task was explicitly abandoned.
2. Its `agent_status` is not `working`.
3. It has no uncommitted or unpushed work in its worktree.

Also offer to reap workers that failed the Step 3 health check — those have
produced nothing to lose.

Never reap a worker that is `working`, `blocked`, or holding an open PR.

Follow `references/herdr/reap-worker.md`, and note the trap it documents:
**`workspace close` does not remove the git worktree.** Remove the worktree
first, then close the workspace, then delete the branch.

Report which workers were reaped and which were kept, with the reason for each.
Only reap workers this workflow created; leave the user's own workspaces alone.
