---
name: delegate-task
description: "Dispatches task execution to an idle Claude Code pane in a tmux session, freeing the current session for other work. Use when 'delegate task', 'send to executor', or when the user wants to hand off a task to a background pane."
---

# Delegate Task

Dispatch execution to an idle Claude Code pane in a tmux session using `ntm`.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

> **Ad-hoc work without task docs?** Use [/delegate](../delegate/SKILL.md) instead —
> it sends a freeform prompt to an idle pane without requiring planning documents.

> **Targeting non-Claude agents.** This skill dispatches `/execute-task` to a
> Claude Code pane. To discover agents and send to **Codex** or **OpenCode**
> panes (whose send/clear conventions differ), see
> [references/delegating-to-agents.md](references/delegating-to-agents.md).

## Input

Task ID (numeric) and optionally a tmux session name (defaults to `$PROJECT--execute`).

## Dispatch Workflow

### Step 1: Advance status to executing

Before finding a worker, mark the task as executing so the worker's fresh
worktree (created from `origin/main`) sees the right stage. See
[references/task-status.md](references/task-status.md): set `status: executing`
in `plan.md` frontmatter (markdown task) or `status="executing"` on `<pd-doc>`
in `plan.html` (HTML/beta task), then commit and push:

```bash
git commit -am "docs($ID): status executing" && git push origin main
```

This also satisfies the "task docs pushed" requirement below.

### Step 2: Find an eligible pane

```bash
ntm status $SESSION --json
```

Parse the JSON output to get the list of panes. Then inspect each candidate pane:

```bash
ntm copy $SESSION:$PANE --last 20 --quiet --output /dev/stdout
```

A pane is eligible when **all** of the following are true:

1. **Not busy** — the pane output shows a prompt waiting for input, not an active task in progress.
2. **On `main`** — the pane's status line or prompt shows `(main)`, not a feature/worktree branch. A pane on a feature branch almost always means a prior task is in flight (open PR, awaiting review/merge). **Never target a pane that is not on `main`**, even if it appears idle — an open/unmerged PR or feature branch disqualifies it.
3. **No open PR** — there is no evidence of an unmerged PR from a prior task.

If a candidate is idle but on a feature branch, skip it and report why (e.g. "pane 2 skipped: on branch `task-505` with open PR").

If no panes are eligible, report what each pane is doing and stop. Do not interrupt active work.

> **Warning:** A pane sitting in another task's worktree has a stale checkout. If dispatched there, the executor may read outdated task docs before creating its own worktree. Always ensure the target pane is on `main`.

### Step 3: Ensure task docs are pushed

Before dispatching, verify that the task's planning docs have been pushed to `origin/main`. The executor will create a fresh worktree from `origin/main` and needs access to the docs.

```bash
git log origin/main --oneline -5 -- tasks/$ID/
```

If the task docs are not on `origin/main`, push them first or warn the user.

### Step 4: Send command

Use `ntm send` with `--smart` to auto-route to the best idle agent (least-loaded strategy):

```bash
ntm send $SESSION --smart --cc --json '/clear'
```

Wait 5 seconds, then send the execution command and rename:

```bash
ntm send $SESSION --pane=$PANE --json '/execute-task $ID'
```

Wait 1 second:

```bash
ntm send $SESSION --pane=$PANE --json '/rename'
```

If the first `/clear` used `--smart`, note which pane index was selected from the JSON response and use `--pane=$PANE` for subsequent sends.

### Step 5: Verify kickoff

Capture the pane output to confirm execution started:

```bash
ntm copy $SESSION:$PANE --last 30 --quiet --output /dev/stdout
```

Check that the output shows the execute-task command was received and work has begun.

### Step 6: Report

Output which pane was selected, confirmation that the command was sent, and what the pane is currently doing.

Tell the user how to check on progress later:

```
To read recent output from this session:
  ntm copy $SESSION:$PANE --last 50
  ntm copy $SESSION:$PANE --code        # extract just code blocks
  ntm grep 'error\|warning' $SESSION    # search across all panes
  ntm watch $SESSION --pane=$PANE       # stream output live
```
