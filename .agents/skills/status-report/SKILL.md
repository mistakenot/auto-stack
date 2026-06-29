---
name: status-report
description: "Monitors all executor panes in a tmux session, reports the status of running, completed, stuck, or idle tasks, and garbage-collects surplus idle panes. Use when 'status report', 'check executors', 'executor status', 'how are tasks going', 'reap idle panes', or when monitoring background task progress."
---

# Executor Status Check

Monitor all executor panes in a tmux session using `ntm` and report status.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

> **Mixed agent types.** Panes may run Claude Code, Codex, or OpenCode, whose
> identification and `/clear` behaviour differ — and `.type` is unreliable for
> OpenCode (resolve by `.command`). The `ntm grep … --cc` in Step 2 only covers
> Claude panes. See [references/delegating-to-agents.md](references/delegating-to-agents.md)
> for reliable agent identification and per-agent quirks.

> **Pool model.** Sessions are organised as two label-separated pools —
> `planners` (commit to `main`) and `workers` (worktrees + PRs). This skill
> monitors and GCs the **workers** pool (`<project>--workers`). See
> [references/ntm-agent-pools.md](references/ntm-agent-pools.md).

## Input

Optional tmux session name (defaults to `$PROJECT--workers`, formerly
`$PROJECT--execute`).

## Status Values

- **in progress**: agent actively working (text streaming, tool calls visible)
- **completed**: "Task complete" or PR URL visible with idle prompt
- **stuck**: errors with no recovery, permission prompts, context exhausted (95%+)
- **idle**: empty prompt, no streaming output

## Workflow

### Step 1: Get structured pane metadata

```bash
ntm status $SESSION --json
```

Parse the JSON. Each pane entry includes `index`, `title`, `type`, `command`, `context_tokens`, `context_limit`, `context_percent`, and `context_model`. Use `context_percent` to detect context exhaustion (95%+ = stuck). The `title` field often contains the task name.

### Step 2: Scan for status markers across all panes

```bash
ntm grep '(execute-task|phase|Phase|PR |error|stuck|permission|Task complete)' $SESSION --cc -i
```

This searches all Claude panes at once. Match lines reveal task IDs, phase progress, errors, and completion markers without capturing each pane individually.

### Step 3: Capture detail for ambiguous panes

For any pane whose status is unclear from the grep results, capture more output:

```bash
ntm copy $SESSION:$PANE --last 50 --quiet --output /dev/stdout
```

### Step 4: Extract per-pane status

For each pane, determine:

- **Task ID**: from `/execute-task NNN`, branch name `task/NNN-`, or file paths `docs/tasks/NNN-`
- **Description**: read first heading from task's `plan.md`
- **Last message**: summary of last agent output block
- **Suggested next step**:
  - completed + PR open -> `/address-feedback`
  - feedback resolved -> `/complete-task`
  - PR merged -> `/clear`
  - stuck -> describe the blocker

### Step 5: Present results

```
| Pane | Task | Description | Status | Context | Last Message | Next Step |
|------|------|-------------|--------|---------|--------------|-----------|
| 0    | 434  | Add widget  | completed | 8%  | PR #87 created | /address-feedback |
| 1    | 435  | Fix auth    | in progress | 42% | Running e2e tests | -- |
| 2    | --   | --          | idle   | 1%  | -- | -- |
```

### Step 6: Reclaim idle panes (garbage collection)

After reporting, shrink the pool back toward the warm **floor of 4** so idle
Claude panes (each a full `claude` process with ~7.8 GB `MemoryMax`) don't
accumulate after a burst of tasks.

Reap **only** panes that are **idle on `main` with no open PR** — never
in-progress, stuck, completed-with-PR, or feature-branch panes. Because
`ntm scale` reaps the **highest-index pane first and is not busy-aware**, only
scale down by the contiguous run of idle panes at the **top** of the index
range:

- `cc_total` = number of Claude panes.
- `reapable_top` = idle-on-main panes counting **down from the highest index**,
  stopping at the first busy pane.
- `to_reap = max(0, min(reapable_top, cc_total - 4))`.

Re-confirm those top panes are still idle immediately before scaling, then:

```bash
ntm scale $SESSION --cc=$(( cc_total - to_reap )) --force --json
```

Report which panes were reaped and which idle panes were **kept** and why (floor
reached, or a busy pane sitting above them — that case reaps nothing this cycle).
Full policy: [references/delegating-to-agents.md](references/delegating-to-agents.md#9-reclaiming-panes-garbage-collection).
