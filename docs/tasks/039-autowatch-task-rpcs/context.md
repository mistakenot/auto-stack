# Context — 039 autowatch-task-rpcs (Task 5: autowatch serves task RPCs)

File:line grounding for the solution. All paths relative to repo root. Read directly; no nested agents.

## The existing dispatch primitive (what `task.dispatch` must reuse — GR-F2 parity)

The daemon already has one dispatch path, driven only by the 60s tick loop. `task.dispatch` must reuse it verbatim so the RPC-produced run is byte-for-byte equivalent to a tick-produced one.

- **`(*daemon.Service).startWorker(ctx, runID int64, task model.TaskDef) error`** — `auto-watch/internal/daemon/daemon.go:741`
  - `GetRun(runID)` → builds `RunsDir/<runID>/` runtime dir → for `bash` writes `command.txt`; for `claude` resolves `gitx.DefaultBranch`, creates a worktree (`gitx.AddWorktree`, emits `worktree_created`), writes `prompt.txt` via `runner.BuildPrompt`.
  - Writes launch script (`runner.WriteLaunchScript`, daemon.go:802) → `runner.Backend.Start(StartSpec{SessionName,WorkDir,ScriptPath,ExitPath,OutputPath})` (daemon.go:816).
  - `store.UpdateRunStarted(runID, RunStartUpdate{...})` flips the run `pending → running` (store.go:208).
  - On success, `logEvent("task_started")` (daemon.go:837) with `session_name`/`worktree_path`/`resource_key`/`branch` metadata. **This is where `watch.task.started` must also be emitted.**
  - `sessionName := runner.ScheduledRunName(run.ID, run.TaskID)` = `"autowatch-run-<id>--<taskID>"` (`runner/names.go:5`).
  - On any failure, calls `failRun` (daemon.go:855): `MarkRunTerminal(RunFailed, nil, now, err)` + worktree cleanup.
- **`store.ReserveRun(ctx, *ReserveRunInput) (int64, error)`** — `auto-watch/internal/store/store.go:170`
  - Inserts a row in `state='pending'`. Returns **`store.ErrActiveRunExists`** (store.go:18) when the partial unique index `uniq_runs_active ON runs(project_id, task_id, resource_key) WHERE state IN ('pending','running')` (store.go:122-124) is violated. **This is the only logical-dedup mechanism.**
  - `ReserveRunInput{ProjectID,ProjectPath,TriggerID,TriggerType,TaskID,TaskType,ResourceKey,Branch,StartedAt}` (store.go:24).
- **The two current callers (the pattern to mirror)** — both do `ReserveRun` → log `task_reserved` → `startWorker`, and handle `ErrActiveRunExists` by logging `task_skipped_dedup`:
  - `(*Service).evaluateTrigger` cron path — `daemon.go:428` (ReserveRun), `:476` (startWorker); ResourceKey = `"cron:"+triggerID`.
  - `(*Service).evaluateFileCreatedTrigger` — `daemon.go:652` (ReserveRun), `:701` (startWorker).
- These methods are **unexported on `*Service`** and only called from single-threaded `Tick`. There is **no mutex** on `Service` today.

## The reaper (terminal lifecycle — who marks runs completed/failed)

- **`(*Service).Reap(ctx) error`** — `auto-watch/internal/daemon/daemon.go:139`, called at the **top of every `Tick`** (daemon.go:95) and in the `--once` path (ops.go:87).
  - `ListRunsByStates(RunRunning)` (store.go:373) → for each, read `run.ExitPath` file; if absent `continue` (leave running); parse exit code; if the tmux session still exists, `Backend.Kill`; then `MarkRunTerminal(state, &code, now, message)` (store.go:227) with `state = RunCompleted` (exit 0) or `RunFailed` (non-zero, message = `tailOutput(OutputPath,200)`).
  - Emits `task_completed` / `task_failed` SQLite events (daemon.go:184-206). **This is where `watch.task.completed` / `watch.task.failed` must also be emitted.**
  - Then sweeps stale `pending` runs older than 5 min (`ListPendingBefore`, store.go:410) → `MarkRunTerminal(RunFailed, "worker did not start")` + `task_failed` (daemon.go:209-232).
  - **Reap lists ALL running runs regardless of origin** — so an RPC-dispatched run is reaped by this same path. There is exactly one reaper (the tick loop).
- **`runner.Backend`** — `auto-watch/internal/runner/backend.go:27`: `Start(ctx,*StartSpec)(Handle,error)`, `Kill(ctx,Handle)error`, `SessionExists(ctx,sessionName)(bool,error)`. `Handle{SessionName,ExitPath,OutputPath}` (backend.go:21). Tmux impl in `runner/tmux.go`. The launch script writes the exit code to `ExitPath` and tees stdout/stderr to `OutputPath=<runDir>/output.log` (runner/backend.go:55 `WriteLaunchScript`).
- **`model.RunState`** — `auto-watch/internal/model` : `RunPending`/`RunRunning`/`RunCompleted`/`RunFailed` (no `cancelled` state). `model.RunRecord` carries `OutputPath`, `ExitPath`, `SessionName`, `WorktreePath`, `State`, `ExitCode *int`, etc.

## RPC handler surface (where the new methods live — 031, MERGED)

- **`rpcmethods.Handlers`** — `auto-watch/internal/rpcmethods/methods.go:29`. Constructor `New(hostID, version string, startedAt time.Time, hub *bus.Hub, ctlEvents bool) *Handlers` (methods.go:40). `Register(p *rpc.Peer)` (methods.go:52) binds methods; each wrapped by `counted(method, inner)` (methods.go:69) which increments an `atomic.Int64` and, when `ctlEvents`, broadcasts a `ctl.log.info`/`rpc.served`. `DispatchCount` (methods.go:59) satisfies `conformance.Observations`.
  - **Package imports `rpc`/`bus` only — NEVER `auto-shared/transport`** (enforced by a layering test, per 031 AC-3). `daemon`/`store`/`runner` do **not** import transport, so `rpcmethods` may import them without breaking GR-N3.
- **`rpc.Handler`** — `auto-shared/rpc/message.go:59`: `func(ctx, params json.RawMessage) (any, error)`.
- **`rpc.Peer` dispatches every request in its own goroutine: `go p.handleRequest(...)`** — `auto-shared/rpc/peer.go:181`. So `task.*` handlers run **concurrently** with each other and with the tick loop. This is the central concurrency constraint.
- **bus.Hub** — `auto-shared/bus/hub.go`: `NewHub()` (hub.go:22), `Broadcast(Event)` (hub.go:48) fans out to registered `Sink`s (hub.go:6). In 031 there are no live peer subscribers (the hub→peer relay `bus.subscribe` is Task 6); tests assert emission via a subscribed test `Sink`.
- **bus.Event / NewEvent** — `auto-shared/bus/event.go:51`: `NewEvent(typ, source, data)` stamps specversion/id/time/host. Provenance fields (`Project`, `Branch`, `Worktree`, `Commit`, `Session`) are top-level struct fields (event.go:28) set *after* construction. `ctl.go` (the 031 sibling) is the template for adding a typed namespace: dotted type constants + a payload struct + a `NewCtlLog` builder.

## How it is wired today (`auto watch start`) — `auto-watch/internal/cli/ops.go`

- `service := daemon.New(db, application.Backend, out, now)` (ops.go:77) — `daemon.New` is `daemon.go:41`; `Service{Store,Backend,Output,Now, workerWG}` (daemon.go:28).
- `hub := bus.NewHub()` (ops.go:104); `handlers := rpcmethods.New(hostID, version, startedAt, hub, ctlEvents)` (ops.go:106); `rpcSrv := rpcserver.New(rpcLn, handlers, hub, ctlEvents)` (ops.go:161). Tick loop ticks every 60s (ops.go:179).
- The `Service` and the `Handlers` are constructed in the same function — so the `Handlers` can be given the `*daemon.Service`, and `daemon.New` can be given the `hub`, with no new cross-package coupling beyond `rpcmethods → daemon` (which does not pull `transport`).

## bus spec — `watch.task.*` (docs/auto-bus-spec.md §6.4–6.5)

- §6.4 registry rows (currently "future adopter / paper mapping"): `watch.task.started` ← `task_started`, `watch.task.completed` ← `task_completed`, `watch.task.failed` ← `task_failed` (also `reserved`/`skipped`). Task 5 turns the paper mapping into a live producer.
- §6.5 envelope: `source:"auto/watch/daemon"`, top-level `project`/`remote`/`branch`/`worktree`/`commit` provenance from the run + project registration; domain identifiers in `data`: `task_id`, `run_id`, `trigger_id`, `session_name`, `resource_key`, `exit_code` (failed/completed), `message`. Severity is implicit in the dotted type (no `level` field). These are **data-plane** events: always on, NOT gated by `--ctl-events` (which only governs `ctl.*` control-plane logs).

## Epic constraints (docs/epics/003-multi-host-architecture/epic.html, Task 5 §, resolved threads)

- Task 5 (epic line 377-389): implement `task.dispatch`/`task.cancel`/`task.status`/`task.output`/`task.list`. **Resolved execution model**: `task.dispatch` inserts into SQLite and **starts the worker directly (same dispatch primitive the tick loop uses)** — it does **NOT** signal the tick loop / no wake channel (the wake-channel idea was removed). The tick loop keeps owning cron runs and **reaping**; RPC-dispatched runs "handle their own lifecycle". Emit `watch.task.*` data-plane events.
- GR-F2 (epic:71): dispatch parity — same SQLite record, tmux session, reap lifecycle as the CLI/tick flow.
- GR-N3 (epic:85): JSON-RPC handlers must not reference the transport.
- Event-planes resolved thread (epic:202-203): task lifecycle events are **data plane** (`watch.task.*`), `ctl.log.*` is infrastructure logs only.
