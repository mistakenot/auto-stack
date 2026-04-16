---
hash: "62212252"
id: "50e72364"
summary: "V1 requirements for autowatch — a cron-driven daemon that monitors repositories and launches bash or Claude Code tasks in response."
title: "autowatch — Requirements"
---

# autowatch — Requirements

A daemon that monitors coding repositories and runs tasks on cron schedules, file creation events, and (later) GitHub events.

Not a replacement for git hooks or CI/CD. Designed for background, longer-running agent tasks like weekly reflections, cleanup jobs, and recurring shell commands like ETL pipelines.

## v1 Scope

**In scope:**
- Trigger types: cron schedules, file creation (glob-based)
- Two task types: `bash` (run a shell command) and `claude` (start a Claude Code session in a worktree)
- Imperative CLI for task/trigger CRUD — commands modify `project.json` directly
- Execution: tmux sessions (abstracted behind an interface for future swapping)
- Task isolation: claude tasks run in their own git worktree; bash tasks run in the project directory
- Worktree cleanup: auto-delete after 24 hours on next tick
- Manual task execution via `autowatch task run` for testing

**Deferred (v2):**
- GitHub event triggers: PR opened/synchronized/reopened (polled via `gh pr list`), push-to-branch, issue events
- Global tasks and triggers (`--global` flag, stored in `~/.auto/watch/project.json`)
- `claude.session.ended` / local session triggers
- Codex / other agent support
- File modification / file deletion triggers (currently only file creation is detected)

### v1 Success Criteria

- Task launch: tasks start successfully in ≥95% of attempts (worktree + tmux + Claude Code all healthy)
- Logging: every trigger evaluation, task start, and task completion/failure is recorded in `logs.sqlite`
- Dedup: duplicate task launches for the same resource key are prevented while a run is in-progress

## Concepts

- **project** — a git repo registered with autowatch
- **trigger** — a named condition that fires (cron schedule in v1; GitHub events in v2)
- **task** — a named unit of work (bash command or claude prompt), linked to triggers

Tasks and triggers are defined separately and linked by ID. A trigger references the task IDs it should launch. This avoids duplicating task definitions when the same task should fire on multiple triggers.

## Config

### Global config: `~/.auto/watch/settings.json`

Created by `autowatch init`. Contains list of registered projects.

```json
{
  "projects": [
    {
      "id": "my-project",
      "path": "/home/user/src/my-project",
      "remote": "git@github.com:user/my-project.git"
    }
  ]
}
```

### Project config: `.auto/watch/project.json`

Lives in each registered repo. Created by `autowatch init` when run inside a git repo. Modified by `task create`, `trigger create`, and `trigger add-task` commands.

Empty initial state after `init`:

```json
{
  "id": "my-project",
  "tasks": {},
  "triggers": {}
}
```

After configuring tasks and triggers:

```json
{
  "id": "my-project",
  "tasks": {
    "run-etl": {
      "type": "bash",
      "command": "autoetl run"
    },
    "regression-review": {
      "type": "claude",
      "prompt": "/regression-review on commits from last 24 hours"
    }
  },
  "triggers": {
    "daily": {
      "type": "cron",
      "when": "0 0 * * *",
      "tasks": ["run-etl", "regression-review"]
    }
  }
}
```

### Task types

**`bash`** — runs a shell command directly in the project directory. No worktree. Useful for ETL, cleanup scripts, and other non-code-modifying operations.

```json
{
  "type": "bash",
  "command": "autoetl run"
}
```

**`claude`** — starts a Claude Code session. Creates a git worktree from the default branch, launches `claude --dangerously-skip-permissions -p "<prompt>"` inside it. Scheduled runs are assumed to be executing in a trusted automation environment such as a VPC. (v2 will support PR head branches.)

```json
{
  "type": "claude",
  "prompt": "/regression-review on commits from last 24 hours"
}
```

### Trigger types

**`cron`** — fires on a cron schedule. Supports optional `onlyIfBranchHasChanged` to skip if no new commits.

```json
{
  "type": "cron",
  "when": "0 9 * * 1",
  "tasks": ["reflect-on-recent-code-sessions"],
  "onlyIfBranchHasChanged": "main"
}
```

**`file_created`** — fires when new files matching a glob pattern appear in the project directory. Poll-based: checked every 60 seconds on the daemon tick loop.

```json
{
  "type": "file_created",
  "glob": "docs/**/*.md",
  "tasks": ["review-docs"]
}
```

Behavior:
- **Baseline seeding:** on the first tick after a trigger is created, existing files are recorded as a baseline snapshot. No tasks fire during the seed tick.
- **Detection:** each subsequent tick globs the project directory, compares against the stored snapshot, and fires linked tasks if new files are found.
- **Snapshot management:** deleted files are removed from the snapshot. Re-creating a previously deleted file counts as a new creation and will fire again.
- **Glob scope:** patterns are relative to the project root. Supports `**` for recursive matching (e.g. `docs/**/*.md`).
- **Dedup key:** `file_created:<trigger-id>` — prevents duplicate active runs for the same trigger.
- **Tracking:** each file in the snapshot stores `mod_time`, `first_seen_at`, and `updated_at` in the SQLite `file_snapshots` table, for future reference and diagnostics.

**`github_pr`** (v2, deferred) — fires when a PR is opened, synchronized, or reopened. Polled via `gh pr list`. Same-repo PRs only.

```json
{
  "type": "github_pr",
  "tasks": ["code-review"]
}
```

## Prerequisites

- `tmux` installed (v3.0+)
- `claude` CLI installed and available on PATH
- Git 2.20+ (worktree support)
- `gh` CLI installed and authenticated (v2, for GitHub triggers)

`autowatch doctor` checks all of the above and reports issues as structured JSON with explanations.

## CLI

### `autowatch init`

Does both global and project setup in one command:

1. **Global setup** (if `~/.auto/watch/settings.json` doesn't exist):
   - Creates `~/.auto/watch/` directory
   - Creates `~/.auto/watch/settings.json` with `{"projects": []}`
   - Creates or verifies `~/.auto/host.json` with hostname
2. **Project setup** (if cwd is a git repo):
   - Accepts optional `--project-id <id>`; defaults to the repo folder name if omitted
   - Creates `.auto/watch/project.json` with `{"id": "<project-id>", "tasks": {}, "triggers": {}}`
   - Creates or updates `.auto/watch/.gitignore` so it contains `worktrees/`
   - Registers the project in `settings.json` (adds to `projects` array with id, path, and remote)
   - If `project.json` already exists, leaves it alone

If cwd is not a git repo, only the global setup runs and a message explains that project setup was skipped.

### `autowatch task create`

Creates or updates a task definition in the project config.

```bash
# bash task — runs a shell command
autowatch task create --id run-etl --bash "autoetl run"

# claude task — starts a Claude Code agent session
autowatch task create --id regression-review --claude "/regression-review on commits from last 24 hours"
```

Flags:
- `--id` (required) — task identifier, used to reference from triggers
- `--bash <command>` — shell command to execute (mutually exclusive with `--claude`)
- `--claude <prompt>` — Claude Code prompt (mutually exclusive with `--bash`)

Exactly one of `--bash` or `--claude` must be provided. Overwrites if the task ID already exists.

### `autowatch task run`

Runs a task immediately in the foreground. Useful for testing task definitions before wiring up triggers.

```bash
autowatch task run --id run-etl
```

For `bash` tasks: executes the command in the project directory, streams output.
For `claude` tasks: creates a worktree, starts Claude Code, blocks until complete.

### `autowatch task list`

Lists all tasks defined in the project config.

```bash
autowatch task list
```

### `autowatch task remove`

Removes a task definition. Warns if any triggers still reference it.

```bash
autowatch task remove --id run-etl
```

### `autowatch trigger create`

Creates a trigger definition.

```bash
# cron trigger
autowatch trigger create --id daily --cron "0 0 * * *"

# cron trigger with branch-change guard
autowatch trigger create --id weekly-if-changed --cron "0 9 * * 1" --only-if-branch-changed main

# file_created trigger — fires when new files match a glob
autowatch trigger create --type file_created --glob "docs/**/*.md" --id watch-docs
```

Flags:
- `--id` (required) — trigger identifier
- `--type <type>` — trigger type: `cron` (default) or `file_created`
- `--cron <expression>` — cron schedule (required for cron triggers)
- `--glob <pattern>` — glob pattern relative to project root (required for file_created triggers)
- `--only-if-branch-changed <branch>` — only fire if the branch has new commits since last run (cron triggers only)

Overwrites if the trigger ID already exists (resets task list).

### `autowatch trigger add-task`

Links a task to a trigger.

```bash
autowatch trigger add-task --trigger daily --task run-etl
```

Validates that both the trigger and task exist in the project config. Appends to the trigger's task list. No-op if the task is already linked.

### `autowatch trigger remove-task`

Unlinks a task from a trigger.

```bash
autowatch trigger remove-task --trigger daily --task run-etl
```

### `autowatch trigger list`

Lists all triggers with their linked tasks.

### `autowatch trigger remove`

Removes a trigger definition.

```bash
autowatch trigger remove --id daily
```

### `autowatch start`

Starts the watch daemon, blocks.

- Outputs event stream to stdout and to `~/.auto/watch/logs.sqlite`
- Rereads settings each tick — no restart needed when projects are added/removed

### `autowatch doctor`

Check prerequisites and configuration health. Reports structured results.

### `autowatch logs`

Rich, LLM-friendly log search. So that Claude can help debug issues or report current state.

```bash
autowatch logs                           # last 50 events
autowatch logs -n 100                    # last 100 events
autowatch logs --project my-project      # filter by project
autowatch logs --level error             # filter by level
autowatch logs --since 24h               # time filter
autowatch logs --task code-review        # filter by task
```

### `autowatch health`

Quick check for smells:
- Tasks taking too long
- Overlapping task attempts
- Tasks that failed or had many tool errors

### `autowatch status`

Current state with top-line stats:
- Healthy / not healthy
- List of tracked projects + their triggers
- Runs in last 24 hours: succeeded / failed

### `autowatch clean`

Manual worktree cleanup (in addition to the 24h auto-cleanup).

## `start` Control Loop

Ticks every 60s:

1. Load active projects (reread settings each tick).
2. For each project:
   - Read `.auto/watch/project.json`
   - Check event log for active tasks (skip duplicates if one is still running)
   - **Cron triggers:** check if any are due based on schedule + last run time. Cron schedules are evaluated in the host's local timezone (`time.Now()`). Missed runs (daemon was stopped) are skipped — no catch-up on restart.
   - **File-created triggers:** glob the project directory, compare against the stored file snapshot in SQLite. On the first evaluation, seed the snapshot silently. On subsequent evaluations, fire linked tasks for any new files detected.
   - For each task to launch:
     - **Claude tasks:** create a git worktree under `.auto/watch/worktrees/`, build the prompt with context header, start a tmux session with Claude Code
     - **Bash tasks:** start the command in a tmux session in the project directory (no worktree)
     - Context variables injected into claude prompts as a header block:
       - `PROJECT_ID` — the project identifier from `project.json`
       - `TRIGGER_TYPE` — `cron` or `file_created`
       - `RESOURCE_KEY` — the stable runtime key for what fired the trigger (`cron:<trigger-id>` or `file_created:<trigger-id>`)
       - `BRANCH` — the default branch for cron tasks
     - Record the tmux session ID, worktree path (if any), start time, resource key, and cron expression in the event log
   - Check status of in-progress tmux sessions:
     - If a session has exited, record outcome and mark as closed
   - **Failure handling:** no automatic retries in v1. Failed tasks are logged with exit code and marked terminal. Users can re-trigger manually or inspect via `autowatch logs`. Run states: `pending` → `running` → `completed` | `failed`.
   - **Worktree cleanup:** delete any worktrees older than 24 hours

### Execution abstraction

The execution model (currently tmux) is behind an interface so it can be swapped later. The interface needs to support:
- Start a task in a working directory with a prompt or command
- Check if a task is still running
- Get the exit status / output of a completed task
- Kill a running task

## Data Storage

- Single SQLite file at `~/.auto/watch/logs.sqlite`
- Event log tracks: task starts, completions, failures, trigger evaluations, worktree creation/cleanup
- Dedup key is **(project, task ID, resource key)** — not just task ID. For cron triggers the resource key is `cron:<trigger-id>`; for file_created triggers it is `file_created:<trigger-id>`. This means: two tasks on different triggers can run concurrently, but a second run for the same trigger is skipped while the first is still in-progress.
- File snapshot data is stored in a `file_snapshots` table keyed by (project, trigger, file_path), tracking `mod_time`, `first_seen_at`, and `updated_at`.
- Used to evaluate cron schedules (last run time per trigger)
