# Context: Task 028

Codebase context for adding `Host` field to `bus.Event`. See [plan.html](plan.html) for requirements.

## Key Files

### Bus envelope
- `auto-shared/bus/event.go:28-45` — `Event` struct. Currently has `SpecVersion`, `Type`, `Source`, `ID`, `Time` as required fields, plus optional `Project`, `Session`, `Remote`, `Branch`, `Worktree`, `Commit`, `Env`, `Data`.
- `auto-shared/bus/event.go:50-66` — `NewEvent()` constructs an event with `specversion`, random `id`, and current time. Does **not** read host config — callers attach provenance fields after construction.
- `auto-shared/bus/event.go:70-94` — `Validate()` checks required fields (`specversion`, `type`, `source`, `id`, `time`) with structured `ValidationError` returns. The `Host` field will follow the same pattern.
- `auto-shared/bus/event_test.go:74-101` — `TestValidateRequiredFields` uses a table-driven test with one case per required field. New `Host` test case follows this exact pattern.

### Derived events
- `auto-shared/bus/derive.go:76-86` — `newDerived()` copies provenance from source event (`Project`, `Session`, `Remote`, `Branch`, `Worktree`, `Commit`) but does **not** copy `Host` (doesn't exist yet). Must add `ev.Host = src.Host`.

### Hook producer (the main event source)
- `auto-cli/cmd/auto/hookscmd.go:151-233` — `buildBusEvent()` calls `bus.NewEvent()`, then attaches context-dependent provenance fields (`Session`, `Worktree`, `Branch`, `Commit`, `Remote`, `Project`). No code change needed for `Host` — `NewEvent()` auto-populates it.
- `auto-cli/cmd/auto/hookscmd.go:100-103` — `hostIDQuietly()` is called for the durable log `Envelope.HostID`. The same logic moves to `auto-shared/config/HostIDQuietly()` for use by `NewEvent()`.
- `auto-cli/cmd/auto/hookscmd.go:317-331` — `hostIDQuietly()` loads `hostId` from `~/.auto/host.json`, falls back to `os.Hostname()`, then `"unknown"`. This pattern is the basis for the shared `HostIDQuietly()`.

### Auto-ui emit (second event source)
- `auto-ui/internal/cli/emit.go:77-87` — `auto ui emit` constructs events with `bus.NewEvent()` and attaches `Project`/`Worktree`. No code change needed for `Host` — `NewEvent()` auto-populates it.

### Host config
- `auto-shared/config/host.go:10-13` — `HostConfig` struct: `HostID string`, `Hostname string`.
- `auto-shared/config/host.go:28-57` — `EnsureHost()` creates `~/.auto/host.json` with `os.Hostname()` default if missing. No format validation on hostId currently.
- `auto-shared/config/paths.go:36-43` — `HostConfigPath()` returns `~/.auto/host.json`.

### Init command
- `auto-watch/internal/cli/init.go:25` — calls `config.EnsureHostFile()` (autowatch's wrapper). Currently auto-creates host.json silently without prompting. Needs to validate format and optionally prompt.

### Doctor checks
- `auto-watch/internal/doctor/doctor.go:18-37` — `Run()` returns a list of `DoctorCheck` structs. Each check is a function returning `model.DoctorCheck{Name, Status, Message, Remediation, Details}`.
- `auto-watch/internal/model/types.go:100-106` — `DoctorCheck` struct pattern.

### Autowatch event production
- No bus event production in autowatch currently. Zero grep hits for `bus.NewEvent` or `bus.Event` in `auto-watch/`. Future work (RPC listener) will need it, but not this task.

## Patterns

- **Provenance attachment pattern**: `NewEvent()` creates the envelope, then callers set `ev.Field = value` for context-dependent provenance fields (`Project`, `Session`, `Branch`, etc). `Host` is different — it is auto-populated inside `NewEvent()` via `HostIDQuietly()` because it is universal (not caller-specific). See decision `dec-auto-populate` in plan.html.

<!-- RESOLVED(P2): Context contradicts the accepted Host population approach
REVIEW: This pattern says `Host` should be assigned after `NewEvent()`, and the key-file notes above say `hookscmd.go` and `auto-ui/internal/cli/emit.go` must attach `Host`. That conflicts with `plan.html`'s accepted decision `dec-auto-populate` and Phase 2, which say `NewEvent()` will populate `Host` and those callers need no code changes. Please make the context match the selected approach so the executor is not given opposite instructions.
AUTHOR: Fixed. Updated the pattern description to distinguish Host (auto-populated in NewEvent) from context-dependent fields (attached by callers). Also updated the key-file notes below to say "No code change needed" for hookscmd and emit.
-->

- **Quiet fallback pattern**: `hostIDQuietly()` never errors — loads config, falls back to hostname, falls back to `"unknown"`. This is moved to `auto-shared/config/` as `HostIDQuietly()` for use by `NewEvent()`.
- **Doctor check pattern**: standalone function returning `model.DoctorCheck` with `Name`, `Status` (ok/fail), `Message`, `Remediation`, `Details`. Added to the `checks` slice in `doctor.Run()`.
- **Validation pattern**: `Validate()` returns `[]ValidationError` with `Code`, `Field`, `Message`, optional `Value`. Required fields use `Code: "required"`.

## Related Tasks
- Task 021 (auto-bus-standard) — defined the CloudEvents envelope and JSON-RPC framing. Key commits: `8f8416c` (phase 1: envelope, hub, derive), `902779a` (phase 3: hooks fire publishes bus envelope), `60f2946` (PR review: XSS, CSRF, RFC 3339 nano).
- Task 022 (hook-event-log) — durable hook logging with hostId capture (the `Envelope.HostID` field). Commits: `b87272a`/`ef5a1db` (phase 2: durable JSONL log), `1b1dada` (merged PR #76).
- Task 026 (planning-dashboard-live-updates) — WebSocket broadcast with client-side event filtering.
- Task 018 (auto-watch-easy-daemon) — introduced doctor checks pattern. Commits: `2c840b9` (phase 3: daemon ExecStart doctor), `90eb03f` (SplitSeq modernize).
- Epic 003 (multi-host-architecture) — parent epic. This task implements items 1–3 from the sequence of work.

## Git History Notes
- `auto-shared/bus/event.go` last substantively changed in task 021 (`8f8416c`). The `Env` field was added later (`f04a9e7`).
- `auto-shared/config/host.go` introduced alongside project registry plumbing (`66e9c4c`).
- Doctor pattern established in task 018; no `host_test.go` exists in `auto-shared/config/` yet (only `auto-watch/internal/config/host_test.go` tests the autowatch wrapper).
