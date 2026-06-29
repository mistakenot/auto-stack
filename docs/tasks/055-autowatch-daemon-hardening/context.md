# Context: Task 055

Code and doc context for autowatch daemon hardening. See [plan.html](plan.html).

## Key Files

- `auto-watch/internal/daemon/daemon.go:222-263` — `Tick()`: calls Reap, iterates projects via tickProject, logs config_warning per bad project every tick, calls `Clean(ctx, false)` at the end
- `auto-watch/internal/daemon/daemon.go:390-438` — `Clean()`: queries `ListTerminalRunsOlderThan(now - 24h)` (hardcoded TTL), iterates terminal runs, calls removeWorktree per run. Never deletes run/event rows.
- `auto-watch/internal/daemon/daemon.go:986-1007` — `removeWorktree()`: skips if WorktreePath empty, calls `gitx.RemoveWorktree`, logs `worktree_removed` event. Does NOT clear WorktreePath on the run record afterward.
- `auto-watch/internal/daemon/daemon.go:500-643` — `evaluateTrigger()`: logs `trigger_evaluated` with outcome metadata every tick for every trigger
- `auto-watch/internal/store/store.go:414-416` — `ListTerminalRunsOlderThan()`: `WHERE state IN (completed, failed) AND completed_at < cutoff`
- `auto-watch/internal/store/store.go:74-83` — WAL pragmas: `journal_mode=WAL`, `busy_timeout=5000`. No checkpoint call.
- `auto-watch/internal/store/store.go:287` — `InsertEvent()`: INSERT-only, no age/size limit
- `auto-watch/internal/gitx/git.go:63-69` — `RemoveWorktree()`: returns nil if `os.Stat` says path doesn't exist (silent no-op)
- `auto-watch/internal/daemon/daemon_integration_test.go:427-479` — `TestCleanRemovesExpiredTerminalWorktrees`: single-tick only, creates worktree + completes run + calls Clean once. No multi-tick assertion.
- `auto-watch/internal/daemon/daemon_integration_test.go:19-34` — `fakeBackend`: Start() writes exit "0" immediately, tasks complete synchronously
- `auto-watch/internal/testutil/env.go` — `NewEnv(t)` sets HOME to tempdir; `NewRepo`, `CommitFile`, `RunCLI` helpers

## Patterns

- No daemon-level config struct exists. The 24h TTL in Clean is hardcoded. A retention setting would go in a new daemon-level config or `~/.auto/watch/settings.json`.
- `model.RunRecord.WorktreePath` (model/types.go:56-76) is never cleared — the field persists after the directory is gone, causing re-processing every tick.
- Event types are plain strings, not constants: `worktree_removed`, `trigger_evaluated`, `config_warning`, `task_failed`, `task_completed`, etc.
- The v1-solution spec (section 10) has a Reaper for active runs but no retention for terminal runs/events.
- The v1-solution spec (section 8.2 step 6) explicitly designed `trigger_evaluated` logging for every evaluation — designed noise, not accidental.
- Failure backoff was never designed. Specs say "no automatic retries" but don't account for cron re-evaluating and launching new runs for broken tasks every tick.
- Existing tests are all single-tick. fakeBackend completes tasks synchronously — no multi-tick lifecycle testing.

## Related Tasks

- Task 018 (auto-watch-easy-daemon): initial daemon implementation
- Task 047 (hook-retarget-autowatch): hook retargeting to autowatch
