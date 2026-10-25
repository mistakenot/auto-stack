# autowatch

Monitor repo changes, trigger Claude Code agent tasks in response.

See [docs/requirements.md](docs/requirements.md) for full requirements, config schema, CLI reference, and control loop design.

## Build & Test

```bash
cd auto-watch
go build ./...
go test -race ./...
```

The `rpcserver` conformance tests include a binary-tier fixture that builds `./cmd/autowatch` via `TestMain`. This requires the `go` toolchain and takes a few seconds on the first run.

## Event relay

The daemon runs an in-process `bus.Hub`. Connected RPC peers can call `bus.subscribe` (parameterless, broadcast-all) to receive hub events as JSON-RPC notifications. The hook-ingest endpoint (`HookIngest`) validates, stamps `hostId` (overwrite-always), broadcasts the raw event, derives `doc.changed` for registered projects, and broadcasts each derived event. `ctl.*` events relay only when `--ctl-events` is on; data-plane events always relay. Slow subscribers are dropped (connection closed) — the hub never blocks.

## Execution backends

A run's execution is **pluggable**, not welded to tmux. The daemon drives every run through the stdlib-only execution port in `auto-shared/backend` rather than talking to a session manager directly:

- `backend.Backend` is the port: `Name()`, `Start(ctx, *StartSpec) (Binding, error)`, `Status(ctx, binding, runDir) (Status, error)`, and `Stop(ctx, binding)`. A `Binding` is the opaque `(Manager, Target)` pair that says where a run executes (the same vocabulary auto-mail uses); nothing physical beyond that pair is derivable from it.
- `backend.Status` reports liveness as `StatusState` — `StatusRunning`, `StatusExited` (with `ExitCode *int`, nil when the exit was recorded but the code is unknown), or `StatusLost` (vanished without completing).
- `backend.Registry` (`NewRegistry(...backends)`, `Get(name)`) resolves a backend by name. The empty name resolves to the default, `backend.DefaultBackendName` = `"tmux"`, so configs with no `backend` field keep working. The one shipped adapter is `backend.Tmux` (`Name()` → `"tmux"`).
- `backend.ErrStartUncertain` is returned by `Start` alongside a valid `Binding` when the adapter created a run but could not confirm the launched command is alive. The caller records the binding and lets `Status` and the deadline own it, so a possibly-live command is never re-dispatched.

### The shared completion contract

Exit semantics are identical across adapters because the wrapper, not the adapter, owns completion. `backend.WriteLaunchScript(*LaunchSpec)` writes a `launch.sh` into the run's `RunDir` that `cd`s into the work dir, tees combined stdout/stderr into the output file, runs the inner command, and writes the exit code **atomically** (temp file + rename) into `<RunDir>/exit-code` (`backend.ExitCodeFile`). `backend.ReadExit(runDir)` reads it back: a parseable integer is the code; a present-but-malformed file is "exited, code unknown" (never pins a run to running); a missing file means no exit recorded. `Status` checks this contract first and only asks the manager about session liveness when no exit was recorded.

### Adding a backend adapter

1. Add **one new file** in `auto-shared/backend` (e.g. `ntm.go`) with a type implementing `backend.Backend`. It is stdlib-only — `auto-shared/go.mod` carries no `require`, so no third-party imports.
2. Implement only `Start` / `Status` / `Stop` liveness — the completion contract (`WriteLaunchScript` / `ReadExit`, the atomic exit-code file) is shared, so the adapter never re-implements exit handling.
3. For an interactive pane manager that can accept input (ntm, herdr), also implement the optional `backend.TextSender` interface (`SendText(ctx, binding, text)`); it is discovered by type assertion. (It is deliberately not named `Sender`, which would collide with mail's caller-identity `Sender`.)
4. Register it in `app.New` by passing it to `backend.NewRegistry(&backend.Tmux{}, &backend.Ntm{}, ...)`. No daemon changes are needed; a `TaskDef` selects it by name.

### TaskDef `backend` and `timeout`

`TaskDef` (in `internal/model/types.go`) gained two optional fields:

- `backend` — the execution backend name. Empty means the default (`tmux`). A non-empty value must match a registered backend name, else config validation reports an `unknown_backend` error.
- `timeout` — a Go duration string (e.g. `30m`, `2h`) bounding a run. Empty means the 2h default. Validated with `time.ParseDuration`; must be `> 0`. A past-deadline run is stopped and failed with reason `timed_out`.

## Inspecting runs

The `run` resource exposes stored runs. **JSON by default**, `--text` for a human-readable table/record.

- `auto watch run list` — runs newest first; metadata rung (ids + the fields you scan first). Filters: `--state` (pending|running|completed|failed), `--task`, `--trigger`, `--project`, `--reason` (a failure reason, see below), and `--since` (duration like `30m`/`2h` or an ISO timestamp). `--state` and `--reason` validate against their closed vocabularies and fail fast listing the valid values.
- `auto watch run get <id>` — one run, full fidelity: every stored field plus `binding{manager,target}`, `runtime_dir`, `output_path`, `exit_path`, `deadline_at`, and a byte-capped `output_tail`. A missing id prints `run <id> not found — list runs with: auto watch run list` and exits non-zero.

**Failure reasons** (a failed run's `failure_reason`, the `--reason` filter values, and the `watch.task.failed` bus `reason`): `exit_nonzero`, `session_lost`, `timed_out`, `start_failed`, `exit_unrecorded`.

## Health and the heartbeat

The daemon writes `~/.auto/watch/heartbeat.json` (`config.HeartbeatPath()`) at the end of **every** tick: `{pid, last_tick_at, last_ok_tick_at, last_error}`. `last_ok_tick_at` advances only on a successful tick, so a daemon that systemctl still reports active but is wedged (failing every tick) shows a stale value — health, not systemctl, is the source of truth. The write is atomic (temp + rename).

`auto watch health` reports runtime warnings (JSON by default, `--json` explicit; exits non-zero when any issue is present). Alongside the existing `old_running_run` / `old_pending_run` / repeated-failure / dedup checks it now reports:

- `stale_heartbeat` — the heartbeat file is missing/unreadable, or no successful tick has been recorded in over 3 minutes (carries the age and `last_error`).
- `low_disk` — under 1 GiB free on the filesystem holding `~/.auto/watch` (a past crash-loop cause).
- `stop_unconfirmed` — a past-deadline run whose backend `Stop` could not be confirmed is kept running (so no overlapping run starts) and flagged until Stop succeeds or the backend reports it gone.

## Debugging a failed run

```bash
auto watch run list --state failed        # find the failed run id
auto watch run get <id>                    # inspect reason, exit_code, binding, output_tail
# then read the full captured output:
cat "$(auto watch run get <id> | jq -r .output_path)"
```

`run get` carries `output_tail` for a quick look and `output_path` for the full captured stdout/stderr; `failure_reason` and `exit_code` classify what went wrong.

<!-- autodoc: start -->
## Documentation Index

*Auto-generated by `autodoc`. Do not edit manually.*

- Run `auto doc quickstart` before first use to learn the workflow.
- Search docs with `auto doc search keyword <query>`.
- Check doc freshness with `auto doc stale`, fix issues with `auto doc fix`.
- Link code to docs with `[autodoc()]` tags, or one `.autodoc` file per folder — run `auto doc quickstart` for details.

**auto-watch/docs**

- [Future Concerns](auto-watch/docs/future-concerns.md): Deferred architectural concerns for auto-watch covering SQLite migration strategy, task output capture, and multi-instance safety, each with a trigger stage and suggested fix. Read when: planning autowatch demo or production deployment
- [autowatch — Requirements](auto-watch/docs/requirements.md): V1 requirements for autowatch — a cron-driven daemon that monitors repositories and launches bash or Claude Code tasks in response. Read when: planning autowatch CLI surface and task scheduling behavior
- [autowatch — Technical Solution](auto-watch/docs/solution.md): Implementation plan for autowatch v1, covering project structure, trigger types, executor design, daemon loop, SQLite state store, and CLI surface. Read when: implementing autowatch daemon architecture and trigger evaluation
- [autowatch v1 Technical Solution](auto-watch/docs/v1-solution.md): Implementation spec for autowatch v1: cron-triggered task scheduling, tmux-backed execution, SQLite run logging, and daemon lifecycle management. Read when: implementing autowatch v1 init, config, and daemon tick loop
<!-- autodoc: end -->













