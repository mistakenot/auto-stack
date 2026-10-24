# Context: Task 067

Codebase and docs facts behind [plan.html](plan.html): the auto watch Backend port, tick/reap stability, and run observability.

## Key Files

### Execution backend (today)
- `auto-watch/internal/runner/backend.go:13-31` -- `StartSpec{SessionName,WorkDir,ScriptPath,ExitPath,OutputPath}`, `Handle{SessionName,ExitPath,OutputPath}`, `Backend{Start, Kill, SessionExists}`. The interface is tmux-shaped.
- `auto-watch/internal/runner/backend.go:45-87` -- `LaunchSpec` and `WriteLaunchScript`. They write `launch.sh`, which runs the bash or claude command and then writes the `exit-code` file. This is the completion contract.
- `auto-watch/internal/runner/backend.go:89` -- `RunForeground`, used by foreground `task run` (`cli/task.go:155,179`). It bypasses Backend entirely.
- `auto-watch/internal/runner/tmux.go:10-74` -- `TmuxBackend`: `new-session -d` plus `remain-on-exit on`. Kill ignores "can't find session". `sessionAbsent` treats "no server running" as absent (the fix for the 33k-restart incident). Its calls use `exec.CommandContext` but no timeout is applied.
- `auto-watch/internal/app/app.go:17,28` -- `Backend runner.Backend`, hard-wired to `runner.TmuxBackend{}` in `app.New`.
- Daemon Backend calls: `daemon.go:169` (Cancel Kill), `:352` (Reap SessionExists), `:357` (Reap Kill), `:479` (Clean --force Kill), `:924` (WriteLaunchScript), `:938` (Start), `:956` (Kill when UpdateRunStarted fails).
- Test fakes implementing `runner.Backend`: `daemon/daemon_integration_test.go:19-34` (fakeBackend), `daemon/dispatch_test.go:21-42` (recordingBackend), `daemon/backoff_test.go:24-70` (startupFail/failing/exitCode), `rpcmethods/task_test.go:34-50`. `testutil/env.go` has no fake backend; `RunCLI` (:90) always gets real tmux.

### Daemon tick, reap and clean
- `auto-watch/internal/cli/ops.go:211` -- `if tErr := service.Tick(ctx); tErr != nil { return &ExitError{Code: 1, Err: tErr} }`. Any Tick error exits the daemon, and `Restart=always` turns that into a crash loop. The initial Tick at :171 is also fatal.
- `auto-watch/internal/daemon/daemon.go:271-317` -- `Tick`. A Reap error returns (:272), a settings load error returns (:277-284), and a tickProject error is logged as "project skipped due to invalid config" even for git or dispatch errors (:301-307). Errors from logEvent (:308) and Clean (:313) also return.
- `auto-watch/internal/daemon/daemon.go:323-467` -- `Reap`. A running run with no exit file does `continue` (:336-338), so a lost run stays running forever. A non-NotExist read error is fatal (:339). `codePtr := &code` (:362) is logged as `"exit_code": codePtr` at :415. `tailOutput(path, 200)` is at :372/:378. Pending runs older than 5m are failed at :428-465.
- `auto-watch/internal/daemon/daemon.go:469-560` -- `Clean`. The force path is :471-504, the 24h TTL worktree removal :506-515, and retention :521-549 (deletes RuntimeDir or `runs/<id>`, then DeleteRuns). Nothing scans RunsDir for directories with no row. `removeWorktree` (:1026) calls `gitx.RemoveWorktree`, which runs `worktree remove --force` (`gitx/git.go:68`).
- `auto-watch/internal/daemon/daemon.go:622-715` -- `evaluateTrigger` (cron). only_if_branch_changed is at :644-663 and the cursor upsert after the dispatch loop at :708-714.
- `auto-watch/internal/daemon/daemon.go:717-861` -- `evaluateFileCreatedTrigger`. The snapshot upsert (:777-789), delete application (:791-794) and trigger_state upsert (:798-804) all happen **before** dispatch (:816-856).
- `auto-watch/internal/daemon/daemon.go:863-982` -- `startWorker`. Run dir :872, AddWorktree :896, WriteLaunchScript :924, Backend.Start :938, UpdateRunStarted :948.
- `auto-watch/internal/daemon/daemon.go:984-1024` -- `failRun` returns nil on success, so every `return s.failRun(...)` reports success to `Dispatch` (:111-149).
- `auto-watch/internal/daemon/daemon.go:48-50,1127-1135` -- `configWarningsSeen` is never cleared.
- `auto-watch/internal/daemon/daemon.go:669,821` -- the backoff skip is written raw to stderr, not recorded as an Event.
- `auto-watch/internal/daemon/daemon.go:1164-1177` -- `tailOutput` reads the whole file and keeps the last N lines, with no byte cap.

### Store and model
- `auto-watch/internal/store/store.go:66-85` -- `Open`. PRAGMAs run via `db.Exec` (:74-83), which reaches only one pooled connection.
- `auto-watch/internal/store/store.go:103-177` -- `Migrate` is an idempotent statement list (`CREATE … IF NOT EXISTS`, `INSERT OR IGNORE INTO schema_migrations VALUES (1)`/`(2)` at :157/:169). There is no ALTER TABLE precedent, so v3 must gate on `PRAGMA table_info(runs)`.
- `auto-watch/internal/store/store.go:108-131` -- runs table columns, plus the unique partial index `uniq_runs_active (project_id, task_id, resource_key)` while pending or running.
- `auto-watch/internal/store/store.go:209-213, 454-460, 490-496, 672` -- the SELECT column list is duplicated three times, plus `scanRun`. All must change together.
- `auto-watch/internal/store/store.go:217-250` -- `UpdateRunStarted` and `MarkRunTerminal` use an unconditional `WHERE id = ?` and don't report rows affected.
- `auto-watch/internal/model/types.go:24-28` -- `TaskDef{Type, Command, Prompt}`. It is decoded with `DecodeJSONFileStrict` (`config/project.go:24`), so new fields must be added to the struct.
- `auto-watch/internal/model/types.go:50-76` -- `RunState` (pending/running/completed/failed) and `RunRecord`.

### CLI and output
- `auto-watch/internal/cli/ops.go:457-503` -- `collectHealth`: old_running_run (2h), old_pending_run (5m), FailedRunGroups, DedupSkipGroups. There is no heartbeat or disk check.
- `auto-watch/internal/textout/format.go:51-75` -- `FormatEventLine` never prints RunID or Message. Metadata is printed with `%v`, so a `*int` shows as an address.
- `auto-watch/internal/cli/trigger.go:16-29`, `cli/task.go:23-35` -- the cobra parent plus `AddCommand` resource pattern. Registered in `cli/root.go:60-73`. `openStore` is at `root.go:78-95`.
- `auto-watch/internal/rpcmethods/task.go:144-242` -- an existing `RunView`/`runToView` with a byte+line output tail (:224-237). It can be reused by `run get`.

### Config validation and gitx
- `auto-watch/internal/config/validate.go:56-115` -- `ValidateTaskEntry`. Backend and timeout checks go after the type switch. `ValidationError{Code, Path, Field, Message, Value}` comes from `auto-shared/config/validation.go`.
- `auto-watch/internal/gitx/git.go:255-260` -- `runGit` returns `"", nil` on any failure. Its callers are `FindRepoRoot` (callers' error branches are dead: `doctor/doctor.go:29`, `cli/root.go:98`, `cli/init.go:39`), `CurrentBranch` (DefaultBranch also swallows, :37-40) and `BranchHeadSHA` (`daemon.go:646`). `OriginRemote` deliberately swallows errors via `runGitAllowFailure`.

### auto-shared and auto-mail
- `auto-shared/rpc/layering_test.go:18` -- `TestLayeringGate`. `go.mod` must have no `require`; there are also rpc/transport import rules. Existing packages: bus, config, git, hooks, lock, model, rpc, transport, update, version. There is no `backend`.
- `auto-mail/mail/client.go:33-37` -- `Binding{Manager, Target, Session}`. `mail/binding.go:12-19` has `ManagerTmux`, `ManagerNTM`, `ManagerCwd`, with the comment "herdr is another value plus an effector implementation". There is no effector code yet.
- `auto-mail/mail/client.go:88` -- `mail.Sender` is a caller-identity struct. **A shared interface named `Sender` would collide in meaning.**

## Patterns
- Resource commands (`docs/auto-package-patterns.md:239-312`): noun + `list | describe | get`. `list` returns ids + metadata. `get` is full fidelity and exposes raw paths. Truncation prints the exact recovery command. JSON is the default with a `--text` alternative, and stdout carries only parseable JSON. Note that auto-watch's existing commands use `--json` instead, which is out of scope here.
- Failure terminal logic belongs in the single chokepoint `failRun`, not in `Dispatch` (memory: autowatch failRun swallows startWorker error).
- Every reap or timeout test needs a negative twin, where a healthy but idle run survives past the threshold (memory: a passing liveness reap test can encode the bug).
- Verify the daemon by actually starting it, not with mocks alone (memory: verify daemon by starting).
- Glossary additions follow `.claude/skills/domain-modelling` add-term (`**Term**:` / `_Avoid_:` / `_Has_:`), then `python3 .claude/skills/domain-modelling/scripts/glossary.py check docs/concepts/UBIQUITOUS_LANGUAGE.md`.
- Bus: `watch.task.failed` already covers "timeout, worker not started" (`docs/auto-bus-spec.md:407`), so a failure reason goes in `data`, not in a new type.
- Lock recovers by liveness, "never by a TTL" (glossary, Lock). That is the precedent for Lost-by-liveness over age-based reaping.

## Incidents that constrain the design
- 2026-09-10: a 33,357-restart crash loop. Reap hit one tmux error on a leftover running run, and Tick exited every time. Fixed narrowly in `b69764d`/`da57108`; this task fixes the class.
- Registry test pollution caused a 7,345-restart flap loop through the strict preflight (memory note).
- A wedged tmux server stalled run 11551 for 7 days (memory note). tmux calls need a context timeout.
- 2026-10-23: a disk-full crash loop (39+ restarts). Afterwards `health` reported ok.

## Related Tasks
- Task 055 (autowatch-daemon-hardening): added retention, WAL checkpoint, per-(Project, TaskDef) backoff, the fake backends and the injectable `now`. Deferred: schema migrations, PBT and soak testing. Feedback: enumerate every terminal path; `os.RemoveAll("")` is a silent no-op.
- Task 039 (task RPCs): `task.dispatch`/`task.cancel` run concurrently with Tick/Reap under `dispatchMu`, proven under `-race`.
- Task 041 (dataplane assurance): a one-way keepalive falsely reaped healthy peers. This is the origin of the negative-twin rule.
- Tasks 062/063 (auto mail): the opaque `(manager, target)` Binding. Effectors and herdr are deferred. `docs/auto-mail-beyond-mvp.md:319-328` defers consolidating mail's effectors with autowatch's Backend until `#new` exists, so this task does not touch mail.
- `auto-watch/docs/solution.md:59-65,486-522`: the original design already planned an `Executor` port (`TmuxExecutor | MockExecutor`). It has an unresolved P0 note: liveness without the run dir can't find the exit file after a restart. The new Status takes the Binding, which carries the run dir, so that is answered.
