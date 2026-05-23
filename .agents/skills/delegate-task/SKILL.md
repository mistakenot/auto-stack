---
name: delegate-task
description: "Dispatches task execution to an idle Claude Code pane in a tmux session, freeing the current session for other work. Use when 'delegate task', 'send to executor', or when the user wants to hand off a task to a background pane."
---

# Delegate Task

Dispatch execution to an idle Claude Code pane in a tmux session.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Input

Task ID (numeric) and optionally a tmux session name (defaults to `$PROJECT--execute`).

## Idle Pane Detection

A pane is idle when ALL of these are true:

- Running a `claude` process
- Ends with empty prompt (no text after prompt marker)
- Status shows `(main)` branch
- No active tool calls, spinners, or "Composing..." indicators
- Not mid-conversation

## Dispatch Workflow

### Step 1: List panes

```bash
tmux list-panes -t $SESSION -F '#{pane_index} #{pane_current_command} #{pane_pid}'
```

### Step 2: Capture each pane

```bash
tmux capture-pane -t $SESSION.N -p -S -30 | tail -30
```

### Step 3: Assess idleness

Apply all detection criteria above to each captured pane output.

### Step 4: Pick best idle pane

Prefer: lowest context usage, most recently completed work.

### Step 5: Send command

```bash
tmux send-keys -t $SESSION.N '/clear' Enter
sleep 3
tmux send-keys -t $SESSION.N '/execute-task $ID' Enter
```

### Step 6: Verify kickoff

Wait 10 seconds, then capture pane to confirm the command was received and execution started.

### Step 7: Report

Output which pane was selected, confirmation that the command was sent, and what the pane is currently doing.

## No Idle Panes

If no panes meet the idle criteria, report what each pane is currently doing and stop. Do not interrupt active work.
