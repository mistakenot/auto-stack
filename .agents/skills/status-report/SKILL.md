---
name: status-report
description: "Monitors all executor panes in a tmux session and reports the status of running, completed, stuck, or idle tasks. Use when 'status report', 'check executors', 'executor status', 'how are tasks going', or when monitoring background task progress."
---

# Executor Status Check

Monitor all executor panes in a tmux session using `ntm` and report status.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

> **Mixed agent types.** Panes may run Claude Code, Codex, or OpenCode, whose
> identification and `/clear` behaviour differ — and `.type` is unreliable for
> OpenCode (resolve by `.command`). The `ntm grep … --cc` in Step 2 only covers
> Claude panes. See [references/delegating-to-agents.md](references/delegating-to-agents.md)
> for reliable agent identification and per-agent quirks.

## Input

Optional tmux session name (defaults to `$PROJECT--execute`).

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
