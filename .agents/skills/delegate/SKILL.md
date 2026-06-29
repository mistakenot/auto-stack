---
name: delegate
description: "Sends a freeform prompt to an idle agent pane in a tmux session, without requiring task planning docs. Use when 'delegate this', 'send this to an executor', 'run this in background', or when the user wants to hand off ad-hoc work. Not for planned tasks with docs (use delegate-task instead)."
---

# Delegate

Send a freeform prompt to an idle coding-agent pane in a tmux session using `ntm`, without requiring task planning documents.

> For delegating **planned tasks** with requirements/solution/plan docs, use
> [/delegate-task](../delegate-task/SKILL.md) instead.
>
> For agent discovery, type identification, and send conventions, see
> [references/delegating-to-agents.md](references/delegating-to-agents.md).
>
> Dispatches into the **workers** pool (`<project>--workers`). For the
> planner/worker pool model and label rules, see
> [references/ntm-agent-pools.md](references/ntm-agent-pools.md).

## Input

A prompt describing the work to delegate, and optionally:
- A tmux session name (defaults to `$PROJECT--execute`)
- A target agent type preference (Claude Code, Codex, OpenCode)

If no prompt is given, ask the user what they'd like to delegate.

## Delegation Workflow

### Step 1: Find an eligible pane

```bash
ntm status $SESSION --json
```

Parse the JSON output to get the list of panes. Then inspect each candidate:

```bash
ntm copy $SESSION:$PANE --last 20 --quiet --output /dev/stdout
```

A pane is eligible when **all** of the following are true:

1. **Not busy** — the pane output shows a prompt waiting for input, not an active task in progress.
2. **On `main`** — the pane's status line or prompt shows `(main)`, not a feature/worktree branch. A pane on a feature branch almost always means a prior task is in flight (open PR, awaiting review/merge). **Never target a pane that is not on `main`**, even if it appears idle — an open/unmerged PR or feature branch disqualifies it.
3. **No open PR** — there is no evidence of an unmerged PR from a prior task.

If a candidate is idle but on a feature branch, skip it and report why (e.g. "pane 2 skipped: on branch `task-505` with open PR").

If **no panes are eligible** — all busy, on feature branches, or only a `user`
shell — **add a fresh worker** instead of stopping. Do not interrupt active work.
First check the **ceiling**: if the session already has **6** Claude panes (the
cap) and none are eligible, every worker is genuinely busy — report and stop (or
wait and re-check); do not add a seventh. Otherwise add one:

```bash
ntm add $SESSION --cc=1 --json
```

Then re-query `ntm status $SESSION --json` to find the new pane's index (the
`add` output's `new_panes[].index` is unreliable — pick the highest-index
`claude` pane), and wait until it is ready for input:

```bash
ntm --robot-wait=$SESSION --wait-until=idle --timeout=60s
```

It returns when the agent reports `state: WAITING`. Set `$PANE` to the new pane.
Add **one** worker per dispatch; if `ntm add` fails, report and stop. See
[references/delegating-to-agents.md](references/delegating-to-agents.md#4-spin-up-a-new-worker-when-none-are-available)
for the full flow.

### Step 2: Clear and send

If you found an existing idle pane, use `ntm send` with `--smart` to auto-route
to the best idle agent; if you just added a worker, target it explicitly with
`--pane=$PANE` (not `--smart`):

```bash
ntm send $SESSION --smart --cc --json '/clear'       # existing idle pane
ntm send $SESSION --pane=$PANE --json '/clear'       # freshly added worker
```

Note which pane index was selected from the JSON response. Wait 5 seconds, then send the prompt:

```bash
ntm send $SESSION --pane=$PANE --json '$PROMPT'
```

**Prompt construction:** Send the user's prompt as-is. Do not wrap it in a slash command or add preamble. If the prompt references files or paths, ensure they are absolute paths (the executor pane may have a different working directory).

### Step 3: Verify kickoff

Capture the pane output to confirm the prompt was received:

```bash
ntm copy $SESSION:$PANE --last 30 --quiet --output /dev/stdout
```

Check that the output shows the agent received the prompt and work has begun.

### Step 4: Report

Output which pane was selected, confirmation that the prompt was sent, and what the pane is currently doing.

Tell the user how to check on progress later:

```
To check on the delegated work:
  ntm copy $SESSION:$PANE --last 50
  ntm copy $SESSION:$PANE --code        # extract just code blocks
  ntm grep 'error\|warning' $SESSION    # search across all panes
  ntm watch $SESSION --pane=$PANE       # stream output live
```
