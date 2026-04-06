---
hash: "3deddeab"
id: "f315bcb9"
summary: "Implementation spec for autowatch v1: cron-triggered task scheduling, tmux-backed execution, SQLite run logging, and daemon lifecycle management."
title: "autowatch v1 Technical Solution"
---

# autowatch v1 - Technical Solution

This is the implementation spec for `autowatch` v1. It is the document a junior engineer should build from. Read it alongside [requirements.md](requirements.md) and the `autowatch` section in [../../docs/user-journey.md](../../docs/user-journey.md).

This document intentionally replaces the older `solution.md` as the source of truth for v1. The older file is useful as background, but it still contains prototype assumptions and internal review comments that should not drive implementation.

## 1. v1 Boundary

### In scope

- Cron triggers only
- Two task types: `bash` and `claude`
- Imperative CRUD commands that mutate `.auto/watch/project.json`
- Daemon loop with 60 second ticks
- tmux-backed execution behind an interface
- Claude tasks in isolated git worktrees
- Bash tasks in the repo root
- Logging to `~/.auto/watch/logs.sqlite`
- Manual foreground execution through `autowatch task run`
- Automatic worktree cleanup after 24 hours

### Explicitly out of scope

- GitHub events
- Global tasks and triggers
- Codex or other agent backends
- Local file watching triggers
- Automatic retries
- Distributed or multi-host dedup

## 2. Design Decisions

These decisions close ambiguities in the current requirements and should be treated as implementation defaults unless they are revised later.

### 2.1 Project IDs

`autowatch init --project-id <id>` is part of the v1 CLI surface.

Reason:

- `docs/user-journey.md` already assumes it exists
- Folder-name-only IDs will collide across repos named `api`, `backend`, `demo`, etc.
- `project_id` is used throughout logging and filtering, so ambiguity here will leak everywhere

Default behavior:

- If `--project-id` is omitted, use the repo folder name
- If the chosen ID is already used by a different registered project path, `init` fails with a remediation hint

### 2.2 Resource Key Format

Use `cron:<trigger-id>` as the resource key, not the raw cron expression.

Reason:

- Two different triggers can legally share the same cron expression
- Dedup must key off the actual configured trigger, not an incidental schedule string
- The raw schedule should still be stored in `TriggerDef.When` and logged in event metadata for debugging

This gives us both:

- human-readable config via the literal cron expression in `project.json`
- stable runtime dedup via `resource_key = cron:<trigger-id>`

### 2.3 Cron Timezone Semantics

Cron schedules are evaluated in the host local timezone exactly as the requirements specify.

Implementation rule:

- Compute one `tickNow := time.Now()` per tick
- Convert to `tickMinute := tickNow.Truncate(time.Minute)`
- Evaluate cron match against `tickMinute` in local time
- Missed minutes while the daemon was not running are ignored

There is no catch-up queue in v1.

### 2.4 Single Daemon per Host

v1 is single-host and single-daemon.

Implementation:

- `autowatch start` acquires an exclusive lock on `~/.auto/watch/daemon.lock`
- If the lock is already held, `start` exits non-zero with a remediation hint
- This prevents accidental duplicate daemons on the same machine

This does not solve cross-host duplication. That is still out of scope for v1.

### 2.5 Worktree Location

Do not create worktrees under `.git/`.

Use:

```text
<repo>/.auto/watch/worktrees/<run-id>/
```

Reason:

- Keeps worktrees scoped to the repo they belong to
- Makes manual inspection and debugging easier
- Git worktrees behave better at normal filesystem paths
- `git status` stays clean once `worktrees/` is ignored
- This avoids poking internal `.git` layout

Because worktrees are repo-local, `autowatch init` must ensure `.auto/watch/.gitignore` contains:

```gitignore
worktrees/
```

### 2.6 Run Artifact Location

Each launched run gets a runtime directory:

```text
~/.auto/watch/runs/<run-id>/
```

This directory stores:

- `launch.sh` - the wrapper script executed by tmux
- `prompt.txt` - for `claude` tasks only
- `command.txt` - for `bash` tasks only
- `output.log` - full stdout and stderr for the run
- `exit-code` - numeric exit code written by the wrapper script

This removes the ambiguity in the older `solution.md`, where completion state depended on a worktree-local file that was not cleanly tied to persisted run state.

### 2.7 Claude Invocation

Assume unattended Claude runs require:

```bash
claude --dangerously-skip-permissions -p "<prompt>"
```

Without that flag, detached background sessions are likely to hang waiting for approval.

This is the default for v1. Scheduled Claude tasks are assumed to be running in a trusted VPC-style automation environment.

## 3. Recommended Project Layout

Create `auto-watch` as its own Go module.

```text
auto-watch/
├── cmd/
│   └── autowatch/
│       └── main.go
├── internal/
│   ├── app/
│   │   └── app.go
│   ├── cli/
│   │   ├── root.go
│   │   ├── init.go
│   │   ├── task.go
│   │   ├── trigger.go
│   │   ├── start.go
│   │   ├── doctor.go
│   │   ├── logs.go
│   │   ├── status.go
│   │   ├── health.go
│   │   └── clean.go
│   ├── config/
│   │   ├── global.go
│   │   ├── project.go
│   │   ├── paths.go
│   │   └── validate.go
│   ├── daemon/
│   │   ├── daemon.go
│   │   ├── tick.go
│   │   ├── worker.go
│   │   └── logger.go
│   ├── gitx/
│   │   ├── repo.go
│   │   ├── branch.go
│   │   └── worktree.go
│   ├── runner/
│   │   ├── backend.go
│   │   ├── tmux.go
│   │   └── foreground.go
│   ├── store/
│   │   ├── db.go
│   │   ├── migrations.go
│   │   ├── runs.go
│   │   ├── events.go
│   │   └── trigger_state.go
│   ├── doctor/
│   │   └── doctor.go
│   ├── model/
│   │   └── types.go
│   ├── timeparse/
│   │   └── since.go
│   └── textout/
│       └── format.go
├── docs/
│   ├── requirements.md
│   ├── solution.md
│   ├── future-concerns.md
│   └── v1-solution.md
├── CLAUDE.md
├── go.mod
└── go.sum
```

Why this layout:

- `cmd/autowatch/main.go` stays small
- CLI parsing is separate from business logic
- `gitx`, `runner`, and `store` isolate external systems
- `model` gives one place for shared structs and enums

## 4. Core Config Types

Use explicit structs and one shared validation path per config type.

### 4.1 Global Config

Path: `~/.auto/watch/settings.json`

```go
type GlobalConfig struct {
    Projects []ProjectRef `json:"projects"`
}

type ProjectRef struct {
    ID     string `json:"id"`
    Path   string `json:"path"`
    Remote string `json:"remote"`
}
```

Reason for adding `id`:

- The requirements example omitted it, but the daemon, logs, and filters all need a stable `project_id`
- Storing the ID in global settings lets `start`, `status`, and `logs` show registered projects without reopening every repo first

### 4.2 Project Config

Path: `.auto/watch/project.json`

```go
type ProjectConfig struct {
    ID       string                 `json:"id"`
    Tasks    map[string]TaskDef     `json:"tasks"`
    Triggers map[string]TriggerDef  `json:"triggers"`
}

type TaskDef struct {
    Type    string `json:"type"`
    Command string `json:"command,omitempty"`
    Prompt  string `json:"prompt,omitempty"`
}

type TriggerDef struct {
    Type                string   `json:"type"`
    When                string   `json:"when,omitempty"`
    Tasks               []string `json:"tasks"`
    OnlyIfBranchChanged string   `json:"onlyIfBranchHasChanged,omitempty"`
}
```

### 4.3 Shared Validation Contract

Follow the repo-wide rule from `AGENTS.md`: one strict shared `validate()` path returning structured errors.

```go
type ValidationError struct {
    Code    string `json:"code"`
    Path    string `json:"path"`
    Field   string `json:"field"`
    Message string `json:"message"`
    Value   any    `json:"value,omitempty"`
}
```

Implement:

```go
func ValidateGlobalConfig(cfg GlobalConfig) []ValidationError
func ValidateProjectConfig(cfg ProjectConfig) []ValidationError
```

Validation rules:

- `project.id`, task IDs, and trigger IDs must match `^[a-z0-9]+(?:-[a-z0-9]+)*$`
- task IDs and trigger IDs are case-insensitive in CLI input, but are stored normalized to lowercase
- `TaskDef.Type` must be exactly `bash` or `claude`
- `bash` tasks require `command` and forbid `prompt`
- `claude` tasks require `prompt` and forbid `command`
- `TriggerDef.Type` must be exactly `cron` in v1
- cron triggers require a valid 5-field cron expression parsed with `robfig/cron/v3` using minute, hour, day-of-month, month, and day-of-week fields only
- `OnlyIfBranchChanged`, when set, must pass `git check-ref-format --branch`
- `TriggerDef.Tasks` must reference existing task IDs
- duplicate task IDs inside one trigger are validation errors
- duplicate project paths in global settings are validation errors
- duplicate project IDs in global settings are validation errors

Mutation commands such as `task create` and `trigger create` should fail fast on invalid input through Cobra errors. Read/list commands should return valid data first and then report validation errors, exiting non-zero if any invalid entries were found.

## 5. Runtime Model

### 5.1 Run States

```go
type RunState string

const (
    RunPending   RunState = "pending"
    RunRunning   RunState = "running"
    RunCompleted RunState = "completed"
    RunFailed    RunState = "failed"
)
```

State transitions:

```text
pending -> running -> completed
pending -> failed
running -> failed
```

There is no retry state in v1.

### 5.2 Trigger Evaluation State

The daemon needs persistent state per trigger so it can avoid double-firing within a minute and support `onlyIfBranchHasChanged`.

```go
type TriggerState struct {
    ProjectID      string
    TriggerID      string
    LastDueMinute  time.Time
    LastBranchSHA  string
    UpdatedAt      time.Time
}
```

`LastDueMinute` is the last minute bucket that this trigger processed, regardless of whether a task was actually launched. That is the key to no-catch-up semantics.

## 6. SQLite Schema

Use one database file:

```text
~/.auto/watch/logs.sqlite
```

Initialize with:

- `PRAGMA journal_mode = WAL;`
- `PRAGMA busy_timeout = 5000;`
- `PRAGMA foreign_keys = ON;`

### 6.1 Migrations

Even in v1, include a simple migration table.

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY
);
```

Ship one migration, version `1`, that creates the remaining tables.

### 6.2 Runs Table

```sql
CREATE TABLE runs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id     TEXT NOT NULL,
    project_path   TEXT NOT NULL,
    trigger_id     TEXT NOT NULL,
    trigger_type   TEXT NOT NULL,
    task_id        TEXT NOT NULL,
    task_type      TEXT NOT NULL,
    resource_key   TEXT NOT NULL,
    branch         TEXT,
    state          TEXT NOT NULL,
    session_name   TEXT,
    runtime_dir    TEXT,
    output_path    TEXT,
    exit_path      TEXT,
    worktree_path  TEXT,
    started_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at   DATETIME,
    exit_code      INTEGER,
    error_message  TEXT
);

CREATE INDEX idx_runs_state ON runs (state);
CREATE INDEX idx_runs_project_started_at ON runs (project_id, started_at DESC);

CREATE UNIQUE INDEX uniq_runs_active
ON runs (project_id, task_id, resource_key)
WHERE state IN ('pending', 'running');
```

That partial unique index is the core dedup guarantee. Do not implement dedup as an in-memory check only.

### 6.3 Trigger State Table

```sql
CREATE TABLE trigger_state (
    project_id       TEXT NOT NULL,
    trigger_id       TEXT NOT NULL,
    last_due_minute  DATETIME,
    last_branch_sha  TEXT,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (project_id, trigger_id)
);
```

### 6.4 Events Table

```sql
CREATE TABLE events (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    level         TEXT NOT NULL,
    event_type    TEXT NOT NULL,
    project_id    TEXT,
    trigger_id    TEXT,
    task_id       TEXT,
    run_id        INTEGER,
    message       TEXT NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX idx_events_timestamp ON events (timestamp DESC);
CREATE INDEX idx_events_project_timestamp ON events (project_id, timestamp DESC);
CREATE INDEX idx_events_task_timestamp ON events (task_id, timestamp DESC);
```

Recommended `event_type` values:

- `trigger_evaluated`
- `trigger_invalid`
- `task_reserved`
- `task_skipped_dedup`
- `task_started`
- `task_completed`
- `task_failed`
- `worktree_created`
- `worktree_removed`
- `config_warning`
- `system_warning`

## 7. CLI Contract

### 7.1 Output Modes

Follow repo-wide CLI rules:

- Text is the default
- Read/list/status/doctor/logs/health must support `--json`
- In `--json` mode, write only JSON payloads to stdout
- Diagnostics and remediation hints go to stderr in `--json` mode

### 7.2 `autowatch init`

Responsibilities:

1. Create `~/.auto/watch/` if missing
2. Create `~/.auto/watch/settings.json` if missing
3. Create or verify `~/.auto/host.json`
4. If current directory is inside a git repo:
   - resolve repo root with `git rev-parse --show-toplevel`
   - create `.auto/watch/project.json` if missing
   - create `.auto/watch/.gitignore` if missing
   - ensure `.auto/watch/.gitignore` contains `worktrees/`
   - register the project in global settings

Behavior details:

- If `project.json` exists, keep its existing `id`
- If `.auto/watch/.gitignore` exists, preserve existing lines and append `worktrees/` only if missing
- If `settings.json` already contains the repo path, update the stored remote if it changed
- If `--project-id` is provided and conflicts with another registered path, fail with:
  - code: `duplicate_project_id`
  - remediation: rerun with a different `--project-id`

### 7.3 `autowatch task create`

Flags:

- `--id` required
- exactly one of `--bash` or `--claude`

Behavior:

- load repo root and project config
- normalize ID to lowercase and trim whitespace
- overwrite the existing task if the ID already exists
- validate the resulting config before writing

### 7.4 `autowatch task list`

Return all valid tasks if possible, even if some other config entries are invalid. If validation errors exist, print them after the valid results and exit non-zero.

Text output should include:

- task ID
- type
- one-line preview of command or prompt

JSON output should return:

```json
{
  "tasks": [...],
  "errors": [...]
}
```

### 7.5 `autowatch task remove`

Behavior:

- remove the task from `tasks`
- keep triggers unchanged
- if any trigger still references the removed task, print a warning and remediation:
  - `run autowatch trigger remove-task --trigger <id> --task <task-id>`

### 7.6 `autowatch task run`

This command is a foreground test path. It should not go through tmux.

For `bash` tasks:

- execute in the repo root
- stream stdout and stderr live
- exit with the underlying command's exit code

For `claude` tasks:

- resolve default branch
- create a temporary worktree
- build the same prompt header used by the daemon
- run Claude in the foreground
- remove the worktree on exit, even on failure

This command may optionally write informational events, but it must not create daemon-managed `runs` rows. It is a developer test command, not scheduled execution.

### 7.7 `autowatch trigger create`

Flags:

- `--id` required
- `--cron` required
- `--only-if-branch-changed` optional

Behavior:

- overwrite existing trigger if IDs match
- reset its task list to empty on overwrite
- validate cron syntax before writing

### 7.8 `autowatch trigger add-task`

Behavior:

- verify trigger exists
- verify task exists
- no-op if the task is already linked
- validate config and rewrite file

### 7.9 `autowatch trigger list`

Return triggers with:

- trigger ID
- type
- cron expression
- branch guard, if any
- linked task IDs

The same partial-results-plus-errors behavior from `task list` applies here.

### 7.10 `autowatch start`

Startup sequence:

1. Acquire daemon lock
2. Open or create SQLite DB
3. Run migrations
4. Run doctor checks
5. Abort on failed doctor checks
6. Write PID metadata file
7. Enter daemon loop

### 7.11 `autowatch doctor`

Checks:

- `tmux` exists and version is `>= 3.0`
- `claude` exists on `PATH`
- `git` exists and version is `>= 2.20`
- `~/.auto/watch/settings.json` exists and validates
- if current directory is a repo and contains `.auto/watch/project.json`, validate it too

Return shape in JSON mode:

```json
{
  "status": "ok",
  "checks": [...]
}
```

Each check should include a remediation hint if it fails.

### 7.12 `autowatch logs`

Read from `events` and support:

- `-n`
- `--project`
- `--task`
- `--level`
- `--since`
- `--json`

`--since` should use the shared date filter convention from the repo root docs: `5m`, `5d`, `1w`, and standard Go durations where possible.

### 7.13 `autowatch status`

Return:

- whether the daemon lock is held
- tracked projects
- trigger counts
- run counts in the last 24 hours by state
- a top-line health summary

### 7.14 `autowatch health`

Start simple. v1 checks:

- running tasks older than 2 hours
- pending tasks older than 5 minutes
- 3 or more failed runs for the same `(project_id, task_id)` in the last 24 hours
- repeated dedup skips for the same `(project_id, task_id, resource_key)` in the last 24 hours

### 7.15 `autowatch clean`

Shared cleanup implementation used by both the daemon tick and the manual command.

Default behavior:

- remove worktrees for terminal runs older than 24 hours
- skip active runs

With `--force`:

- kill active tmux sessions first
- mark those runs as failed with `error_message = 'killed by autowatch clean --force'`
- remove their worktrees

## 8. Daemon Tick Algorithm

The daemon must be deterministic and testable. Sort project IDs, trigger IDs, and task IDs before processing so event order is stable.

### 8.1 High-Level Tick

```text
tick(now):
  load and validate global settings
  load each registered project config
  for each valid project:
    evaluate each trigger
  reap finished runs
  clean expired worktrees
```

### 8.2 Trigger Evaluation Algorithm

For each trigger:

1. Compute `tickMinute := now.In(time.Local).Truncate(time.Minute)`
2. Parse the cron expression once and cache the parser
3. Check whether the schedule matches `tickMinute` with:
   - `prev := tickMinute.Add(-1 * time.Minute)`
   - `due := schedule.Next(prev).Equal(tickMinute)`
4. Load `trigger_state`
5. If `LastDueMinute == tickMinute`, skip because this minute was already processed
6. If the schedule does not match, log `trigger_evaluated` with outcome `not_due` and continue
7. If `onlyIfBranchHasChanged` is set:
   - read current SHA with `git rev-parse <branch>`
   - if `LastBranchSHA` is non-empty and equals current SHA:
     - update `trigger_state` with `LastDueMinute = tickMinute`
     - log `trigger_evaluated` with outcome `branch_unchanged`
     - continue
8. For each linked task:
   - attempt to reserve a run by inserting a `pending` row
   - if the unique active-run index rejects the insert:
     - log `task_skipped_dedup`
     - continue
   - log `task_reserved`
   - spawn a worker goroutine with the reserved run ID
9. Update `trigger_state` with `LastDueMinute = tickMinute` and the current branch SHA if one was checked
10. Log `trigger_evaluated` with the final outcome

Important:

- update `trigger_state` even if dedup blocked the launch
- do not create extra catch-up launches later

## 9. Worker Algorithm

Each worker owns one already-reserved `runs` row.

Algorithm:

1. Load the run row and task definition
2. Create `~/.auto/watch/runs/<run-id>/`
3. If task type is `claude`:
   - resolve default branch
   - create worktree
   - build prompt header
   - write `prompt.txt`
4. If task type is `bash`:
   - use repo root as workdir
   - write `command.txt`
5. Write `launch.sh`
6. Start tmux session through the backend
7. Update the run row to `running` with:
   - `session_name`
   - `runtime_dir`
   - `output_path`
   - `exit_path`
   - `worktree_path`, if any
8. Log `task_started`
9. If any step fails:
   - mark the run `failed`
   - clean up any created worktree
   - log `task_failed`

Do not wait for completion inside the worker.

## 10. Reaper Algorithm

Each tick, after trigger evaluation, inspect all `running` rows.

Algorithm:

1. Query running runs ordered by `started_at`
2. For each run:
   - if `exit_path` does not exist, leave it alone
   - if `exit_path` exists:
     - parse the exit code
     - read the last 200 lines from `output.log`
     - kill the tmux session if it still exists
     - mark the run `completed` when exit code is 0
     - mark the run `failed` when exit code is non-zero
     - set `completed_at`
     - log `task_completed` or `task_failed`

Also handle abandoned pending runs:

- if a `pending` run is older than 5 minutes, mark it failed with `error_message = 'worker did not start'`

That covers crash windows between reservation and session start.

## 11. Prompt Construction

Only `claude` tasks use prompt augmentation.

Header format:

```text
<autowatch-context>
PROJECT_ID: my-project
TRIGGER_TYPE: cron
TRIGGER_ID: daily
RESOURCE_KEY: cron:daily
BRANCH: main
</autowatch-context>

/regression-review on commits from last 24 hours
```

Rules:

- `TRIGGER_TYPE` is always `cron` in v1
- `RESOURCE_KEY` is the stable dedup key, not the raw cron expression
- the raw cron expression remains in `TriggerDef.When` in config and should also be logged in event metadata under `cron`
- leave a blank line between the context block and the user prompt

## 12. Runner Interface

Keep tmux behind a narrow interface.

```go
type StartSpec struct {
    SessionName string
    WorkDir     string
    ScriptPath  string
    ExitPath    string
    OutputPath  string
}

type Handle struct {
    SessionName string
    ExitPath    string
    OutputPath  string
}

type Backend interface {
    Start(ctx context.Context, spec StartSpec) (Handle, error)
    Kill(ctx context.Context, handle Handle) error
    SessionExists(ctx context.Context, sessionName string) (bool, error)
}
```

Do not put `Result()` on the backend. Result reading is file-based from the runtime directory and should stay in daemon code.

### 12.1 tmux Backend

`Start()` should:

1. create a detached session:

```bash
tmux new-session -d -s <session-name> -c <workdir> <script-path>
```

2. enable remain-on-exit:

```bash
tmux set-option -t <session-name> remain-on-exit on
```

3. return the handle

`Kill()` should call:

```bash
tmux kill-session -t <session-name>
```

If the session is already gone, treat that as success.

## 13. Wrapper Script Contract

Generate one script per run instead of building long shell command strings inline. This is easier to debug and avoids quoting bugs.

For `claude` tasks, `launch.sh` should be conceptually equivalent to:

```bash
#!/usr/bin/env bash
set -u

RUN_DIR="$1"
WORK_DIR="$2"
PROMPT_FILE="$RUN_DIR/prompt.txt"
OUTPUT_FILE="$RUN_DIR/output.log"
EXIT_FILE="$RUN_DIR/exit-code"

exec > >(tee -a "$OUTPUT_FILE") 2>&1
cd "$WORK_DIR"

claude --dangerously-skip-permissions -p "$(cat "$PROMPT_FILE")"
code=$?
printf '%s\n' "$code" > "$EXIT_FILE"
exit "$code"
```

For `bash` tasks, the body is the same except the command becomes:

```bash
bash -lc "$(cat "$RUN_DIR/command.txt")"
```

Why this contract matters:

- full output is persisted
- completion is explicit
- the runtime directory fully explains what happened during a run

## 14. Git Rules

All git shell-outs should live in `internal/gitx`.

Implement helpers for:

- repo root discovery
- origin remote lookup
- default branch lookup
- branch head SHA lookup
- worktree add
- worktree remove

### 14.1 Default Branch Resolution

Use this order:

1. `git symbolic-ref refs/remotes/origin/HEAD`
2. if that fails, prefer local `main`
3. if that fails, prefer local `master`
4. if that fails, use the current branch from `git branch --show-current`

Log which branch was chosen when launching a Claude task.

### 14.2 Worktree Creation

Use:

```bash
git -C <repo-root> worktree add <worktree-path> <branch>
```

The worktree path must be:

```text
<repo-root>/.auto/watch/worktrees/<run-id>/
```

It must be unique per run ID.

### 14.3 Worktree Removal

Use:

```bash
git -C <repo-root> worktree remove --force <worktree-path>
```

If the directory is already gone, log a warning and continue.

## 15. Event Logging

Every trigger evaluation, task start, and task completion or failure must write an event row.

Recommended `metadata_json` payload keys:

- `outcome`
- `cron`
- `resource_key`
- `branch`
- `session_name`
- `worktree_path`
- `exit_code`
- `error`

`autowatch start` should print the same events to stdout in text mode as they are written to SQLite. A compact line format is enough:

```text
2026-03-20T10:00:00Z info trigger_evaluated project=my-project trigger=daily outcome=launched
```

## 16. Testing Strategy

### Unit tests

- config load and validation
- ID normalization
- cron matching against fixed `time.Time` values
- trigger evaluation state transitions
- date filter parsing for `--since`
- wrapper script generation
- event formatting

### Integration tests

Use temp directories, a real SQLite file, and temporary git repos.

- `init` creates files and registers project
- `task create` and `trigger create` rewrite config correctly
- trigger due minute reserves exactly one run
- duplicate active run insert is rejected by the DB unique index
- `onlyIfBranchHasChanged` fires on first run, skips on unchanged SHA, fires again after a commit
- worktree cleanup skips active runs and removes old terminal runs
- abandoned `pending` rows are failed by the reaper

### E2E tests

Guard these behind an environment check because they require real `tmux` and `claude`.

- `autowatch start` launches a detached tmux session for a due task
- completion is detected from `exit-code`
- `autowatch logs` returns the expected events

## 17. Implementation Order

Implement in this order.

1. Scaffold `auto-watch/go.mod`, the Cobra root command, and root Makefile updates so `build`, `test`, `vet`, and `install` include `autowatch`.
2. Implement `config`, path helpers, and validation.
3. Implement SQLite migrations and store queries.
4. Implement git helpers and worktree helpers.
5. Implement the tmux backend and wrapper script generation.
6. Implement `task` and `trigger` CRUD commands.
7. Implement `task run` foreground execution.
8. Implement the daemon tick, worker, and reaper.
9. Implement `doctor`, `logs`, `status`, `health`, and `clean`.
10. Add integration and E2E coverage.

Do not start with the daemon loop. The config and storage layers must be solid first.

## 18. Acceptance Criteria

`autowatch` v1 is complete only when all items in this section are true.

### 18.1 Build and Repo Integration

- `auto-watch` is a standalone Go module with a buildable `autowatch` binary.
- The repo root `Makefile` includes `autowatch` in `build`, `test`, `vet`, and `install`.
- `go test ./...` passes inside `auto-watch`.

### 18.2 Init and Config

- `autowatch init` creates `~/.auto/watch/settings.json` when missing.
- `autowatch init` creates or verifies `~/.auto/host.json`.
- Running `autowatch init` inside a git repo creates `.auto/watch/project.json` when missing.
- Running `autowatch init` inside a git repo creates or updates `.auto/watch/.gitignore` so it contains `worktrees/` and does not destroy existing user entries.
- `autowatch init --project-id <id>` is supported.
- If `--project-id` is omitted, the repo folder name is used.
- The project is registered in `settings.json` with `id`, `path`, and `remote`.
- Duplicate project IDs pointing at different paths are rejected with a clear remediation hint.
- Global and project config reads go through shared validation and return structured validation errors.

### 18.3 Task and Trigger CRUD

- `autowatch task create`, `task list`, `task remove`, `trigger create`, `trigger add-task`, `trigger remove-task`, `trigger list`, and `trigger remove` are implemented.
- `task create` enforces exactly one of `--bash` or `--claude`.
- `trigger create` enforces a valid 5-field cron expression.
- IDs are normalized to lowercase, trimmed, and validated against the slug format defined in this spec.
- `task list` and `trigger list` support `--json`.
- In `--json` mode, stdout contains only parseable JSON and diagnostics go to stderr.
- List commands return valid results even when some entries are invalid, then exit non-zero with structured validation errors.

### 18.4 Foreground Manual Execution

- `autowatch task run --id <task>` is implemented for both `bash` and `claude` tasks.
- `bash` tasks run in the repo root, stream output live, and exit with the underlying command exit code.
- `claude` tasks create a temporary worktree, run in the foreground, and remove the worktree on exit.
- `task run` builds the same prompt header used by scheduled Claude runs.
- `task run` does not create daemon-managed `runs` rows in SQLite.

### 18.5 Daemon Startup and Locking

- `autowatch start` acquires an exclusive daemon lock before entering the loop.
- Starting a second daemon on the same host fails cleanly with a remediation hint.
- `start` opens `~/.auto/watch/logs.sqlite`, runs migrations, runs doctor checks, and aborts if required checks fail.
- The daemon rereads global settings and project configs on each tick so config changes are picked up without restart.

### 18.6 Cron Evaluation and Dedup

- Cron triggers are evaluated every 60 seconds in the host local timezone.
- Missed schedules while the daemon is down are not replayed.
- `onlyIfBranchHasChanged` is honored by comparing the configured branch head SHA with persisted trigger state.
- The first run for a branch-guarded trigger is allowed even when there is no previous SHA.
- Trigger state records the last processed minute bucket so the same trigger is not reprocessed multiple times in one minute.
- Dedup is enforced by SQLite, not only by in-memory checks.
- At most one `pending` or `running` run may exist for the same `(project_id, task_id, resource_key)`.
- For cron triggers, `resource_key` is `cron:<trigger-id>`.
- The human-readable cron expression remains stored in `TriggerDef.When` and is logged in event metadata.

### 18.7 Task Launch and Runtime Artifacts

- Scheduled `bash` tasks launch in tmux from the repo root.
- Scheduled `claude` tasks launch in tmux from a repo-local worktree under `.auto/watch/worktrees/<run-id>/`.
- Scheduled Claude runs use `claude --dangerously-skip-permissions -p "<prompt>"`.
- Each reserved run gets a runtime directory under `~/.auto/watch/runs/<run-id>/`.
- The runtime directory contains `launch.sh`, `output.log`, `exit-code`, and task-specific input files such as `prompt.txt` or `command.txt`.
- The prompt header includes `PROJECT_ID`, `TRIGGER_TYPE`, `TRIGGER_ID`, `RESOURCE_KEY`, and `BRANCH`.
- Runs move through the states `pending -> running -> completed|failed` as specified.
- If worker startup fails after a run is reserved, the run is marked `failed` and any created worktree is cleaned up.

### 18.8 Reaping and Cleanup

- The daemon reaps completed tmux-backed runs by reading the persisted `exit-code` file.
- Exit code `0` marks a run `completed`; non-zero exit codes mark a run `failed`.
- The daemon logs completion or failure events with exit code metadata.
- Abandoned `pending` runs older than the configured threshold are marked failed.
- Automatic cleanup removes worktrees for terminal runs older than 24 hours.
- Automatic cleanup skips active runs.
- `autowatch clean` is implemented and reuses the same cleanup logic.
- `autowatch clean --force` kills active tmux sessions, marks those runs failed, and removes their worktrees.

### 18.9 Observability and Operator Commands

- Every trigger evaluation writes an event row.
- Every task start writes an event row.
- Every task completion or failure writes an event row.
- `autowatch start` also emits a readable event stream to stdout in text mode.
- `autowatch doctor`, `logs`, `status`, and `health` are implemented.
- `doctor`, `logs`, `status`, and `health` support `--json`.
- `logs` supports filtering by count, project, task, level, and `--since`.
- `status` reports daemon lock state, tracked projects, trigger counts, and recent run counts by state.
- `health` reports at least the v1 smell checks defined in this spec.

### 18.10 Verification

- Unit tests cover config validation, cron evaluation, trigger state transitions, wrapper generation, and event formatting.
- Integration tests cover init flows, config mutation, dedup via the DB unique index, branch-change gating, reaping, and cleanup.
- E2E tests exist for real tmux-based execution and are gated so they run only in environments with the required binaries.
- The implementation satisfies the high-level v1 success criteria from `requirements.md`: successful launches, complete logging, and prevention of duplicate in-progress launches.

## 19. Status

There are no remaining major design blockers for v1 in this document.

Decisions now locked in:

1. Scheduled Claude runs use `claude --dangerously-skip-permissions`.
2. `autowatch init --project-id` is part of the v1 CLI. If omitted, it defaults to the repo folder name.
3. The literal cron expression stays in `TriggerDef.When` so `project.json` stays easy to read.
4. Runtime dedup still uses `resource_key = cron:<trigger-id>` so task identity stays stable even when multiple triggers share the same schedule.
