---
hash: "ae6ec843"
id: "e9a37562"
read_when: "implementing or reviewing the hook event log feature and understanding where hook payloads are produced in auto-cli"
summary: "Verified codebase facts for implementing the durable hook event log: producer side in auto-cli hookscmd.go where verbatim raw bytes are available, and key file locations for the append path."
title: "Context: Task 022 — Hook Event Log"
---

# Context: Task 022

Verified codebase facts grounding the hook-event-log design. See [solution.md](./solution.md).

## Key Files

### Producer side (auto-cli)
- `auto-cli/cmd/auto/hookscmd.go:58-91` — `newHooksFireCmd`: reads stdin via
  `io.LimitReader(.., maxHookPayloadBytes=1MiB)` (line 30), builds the event,
  `postHookEvent(uiPort(), ev)`, **always returns nil** (line 85). This is the
  exact spot to also append the durable envelope; the verbatim `raw []byte` is
  already in hand at line 79-83.
- `auto-cli/cmd/auto/hookscmd.go:96-121` — `buildHookEvent`: lenient
  `json.Unmarshal` into `map[string]any`; extracts `hook_event_name`,
  `session_id`, `tool_name`, `cwd`; **falls back to `os.Getwd()` when cwd is
  empty (lines 109-111)** — the reason cwd must live in the capture envelope.
  Resolves project via `registry.FindProjectByPath(ev.Cwd)` (lines 115-119) —
  this producer-side resolution now also feeds the envelope's `project` field
  (not re-resolved at ETL); the field/path extraction delegates to the shared
  `auto-shared/hooks` `Extract*` helpers.
- `auto-cli/cmd/auto/hookscmd.go:124-145` — `extractPaths`: pulls `file_path` /
  `notebook_path` / `path` out of `tool_input`, sorted, deduped. **This task
  moves this logic (plus `stringField` and the event-name/session/tool
  extraction) into `auto-shared/hooks` as exported `Extract*` helpers** so the
  producer's `buildHookEvent` and the ETL ingest call one implementation; the
  ETL consumer cannot and must not import auto-cli's `package main`.

<!-- RESOLVED(P2): "ETL ingest reuses this logic" is not possible as written — it's in package main
REVIEW: `extractPaths`, `stringField`, and `buildHookEvent` all live in auto-cli's
`package main` (verified: hookscmd.go:1), which auto-etl cannot import (and must not — the
binaries depend only on a file format, not each other; confirmed no auto-cli import anywhere
in auto-etl). So the ETL "reuses" claim here and at the buildHookEvent bullet (context.md:18-21)
is actually *re-implementation* — the path/field-extraction logic gets duplicated across
producer and consumer, which then drift independently (the exact risk the shared-package
rejected-alternative warns about in solution.md:159-162). Since this task already creates the
`auto-shared/hooks` package, the clean fix is to hoist the normalization helpers
(extractPaths / stringField / event-name+session+tool extraction) into `auto-shared/hooks`
so both the producer's `buildHookEvent` and the ETL ingest genuinely share one implementation.
At minimum, change "reuses" → "re-implements (or moves to auto-shared/hooks)" so the design is
honest about the duplication. This is a real decision the solution should make explicitly.
AUTHOR: Took the hoist. The `Extract*` normalizers (event name, session, tool, paths) move
into `auto-shared/hooks`; `buildHookEvent` now delegates to them and the ETL ingest calls the
same functions — genuinely one implementation, no `package main` import. solution.md item 1 +
the log.go outline + the Files table + the rejected-alternatives entry all updated to match.
This bullet and the buildHookEvent bullet above now say "moves to / delegates to
auto-shared/hooks" rather than "reuses".
-->
- `auto-cli/cmd/auto/hookscmd.go:22` — `hookPostTimeout = 150ms`; the durable
  append must stay well inside the hot-path budget.
- `auto-cli/cmd/auto/hookscmd.go:176-191` — `uiPort()`: pattern for reading a
  `~/.auto/<tool>/settings.json` via `sharedconfig.AutoDir()` +
  `DecodeJSONFile`. Mirror for locating `~/.auto/hooks/raw/`.
- `auto-cli/cmd/auto/hookscmd_test.go:1-107` — test conventions: `t.Setenv("HOME",
  t.TempDir())`, `httptest.Server` for the UI, `io.Discard` for output,
  `TestFireExitsZeroWhenUIDown` proves exit-0 with nothing listening.

### Shared layer (auto-shared)
- `auto-shared/config/paths.go` — `AutoDir()` → `~/.auto`, `HomeDir()` (prefers
  `$HOME`), `EnsureAutoDir()`. New `auto-shared/hooks` package builds
  `~/.auto/hooks/raw` on top of these.
- `auto-shared/config/host.go` — `HostConfig{HostID, Hostname}`, `LoadHost(path)`,
  `HostConfigPath()` → `~/.auto/host.json`. Producer host-id lookup mirrors
  `auto-etl`'s `loadHostID` (below).
- `auto-shared/config/projects.go` — `ProjectsConfig.FindProjectByPath(dir)`
  (longest-prefix match), `ProjectsConfigPath()` → `~/.auto/projects.json`,
  `LoadProjects`. The **producer** resolves `project` from `cwd` with these at
  fire time and stores the result in the envelope; ETL reads it back rather than
  re-resolving against a possibly-different host registry.

### Consumer side (auto-etl)
- `auto-etl/cmd/run.go:37-41` — `validOnlyValues = {sessions, github, git}`; add
  `hooks: true`.
- `auto-etl/cmd/run.go:74-95` — orchestration: `if sources["sessions"] {…}` etc.
  Add an `if sources["hooks"] { runHooksETL(hostID) }` phase here.
- `auto-etl/cmd/run.go:117-138` — `parseOnlyFlag`: lowercases, validates against
  `validOnlyValues`, default returns all-true map (line 122). Add `hooks` in both
  places + the `--only` help text (line 110).
- `auto-etl/cmd/run.go:280-302` — `loadHostID()`: `HostConfigPath` → `LoadHost`,
  falls back to `os.Hostname()`/"unknown". Producer reuses this shape.
- `auto-etl/cmd/root.go:39-45` — `homeDefaults()`: output dir is
  `~/.auto/etl/output`; hooks parquet lands under `<output>/hooks/`.

### Writer + model (auto-etl)
- `auto-etl/internal/writer/writer.go:21-61` — `Write`: messages weekly /
  sessions monthly; **full-regen current, skip past if exists** (line 33). NOT
  the model for hooks (watermark-incremental needs merge, not regen).
- `auto-etl/internal/writer/writer.go:87-105` — `writeParquet[T any]`: MkdirAll +
  `parquet.NewGenericWriter[T]`. Reused directly for hooks.
- `auto-etl/internal/writer/git.go:122-144` — `mergeByID[T any](existing,
  incoming, idFunc)`: incoming wins on dup id. The hooks writer uses this with
  `func(r *HookEventRow) string { return r.ID }`.
- `auto-etl/internal/writer/github.go:144-164` — `readExistingParquet[T](path)`:
  returns `nil,nil` if absent, else all rows via `parquet.NewGenericReader[T]`.
  Reused by the hooks writer to read the current month partition before merge.
- `auto-etl/internal/writer/git.go:57-115` — WriteGit's monthly read-merge-write
  loop: the exact template for `WriteHooks`.
- `auto-etl/internal/model/model.go:5` — `const SchemaVersion = 6`. New
  `HookEventRow` carries its own `schema_version` (start at 1, independent of the
  messages/sessions version).
- `auto-etl/internal/model/model.go:28-44` — parquet tag conventions:
  `id`/`session_id,dict`/`host_id,dict`, Unix-ms `int64` timestamps, `int32`
  `year`/`week`/`month`/`schema_version`. `HookEventRow` follows these.
- `auto-etl/internal/model/github.go` — `reviewers_json`/`files_json` etc. show
  the **JSON-string-for-arrays** convention; `paths_json` follows it.

### Incremental-state precedent
- `auto-etl/internal/git/state.go:11-25` — `GitSyncState{SchemaVersion, Repos}` +
  `GitSyncStatePath()` → `~/.auto/etl/git/sync-state.json`. `HooksSyncState`
  mirrors this at `~/.auto/etl/hooks/sync-state.json`.
- `auto-etl/internal/git/state.go:28-92` — `LoadGitSyncState` (lenient: missing /
  corrupt → empty), `Save` (temp file + `os.Rename`, atomic), nil-map guards.
  Copy this shape verbatim for hooks (offset map instead of SHA set).
- `auto-etl/cmd/run.go:373-444` — load-state → process → write → `MarkSeen` →
  `syncState.Save(statePath)` lifecycle; `runHooksETL` follows the same order
  (write parquet **before** saving the watermark so a crash re-reads, not loses).

## Patterns

- **Lenient everywhere**: parsing tolerates unknown/missing fields; missing or
  corrupt files become empty state; producer swallows all runtime errors and
  exits 0. Both new readers (envelope parse, ingest) must follow this.
- **File-format contract, not code coupling** (CLAUDE.md): the binaries don't
  depend on each other; `auto-shared` is the legitimate shared layer for the
  envelope type + path helpers that both sides need.
- **Canonical lossless + derived views** (CLAUDE.md "Common data format"): the
  parquet table keeps `raw_json` verbatim and adds a few normalized columns —
  the same preserve-and-normalize split `tool_use_result_json` already uses for
  messages.
- **Atomic writes**: sync-state via temp+rename (`git/state.go:60-71`); log
  append via a single `os.OpenFile(O_APPEND|O_CREATE|O_WRONLY)` + single
  `f.Write` (on Linux the `write()` holds the inode lock for the whole call, so
  concurrent agents' lines don't interleave even at the ~1 MiB bound — not a
  buffered writer that could flush in chunks).
- **Partition + merge for incremental sources**: git/github read the existing
  partition, `mergeByID`, rewrite — the correct model for a watermark source,
  versus the messages/sessions full-regen.

## Related Tasks
- **Task 020 (auto-hooks-install)**: wired `auto hooks fire` onto a
  telemetry-safe event allowlist — Claude (9): PreToolUse, PostToolUse,
  UserPromptSubmit, Notification, Stop, SubagentStop, SessionStart, SessionEnd,
  PreCompact; Codex (10): SessionStart, SubagentStart, PreToolUse,
  PermissionRequest, PostToolUse, PreCompact, PostCompact, UserPromptSubmit,
  SubagentStop, Stop. These are the events whose payloads the durable log will
  capture; PostToolUse dominates the volume.
- **Task 021 (auto-bus-standard)**: owns the live, lossy CloudEvents bus that the
  UI POST migrates to. Task 022 is deliberately **independent** (requirements
  Q4) — the durable log is the offline canonical path; the bus is the live path.
  Both consume the same underlying hook payloads.
- **Task 002 (git-history-etl, commit `8a22803`)**: the structural precedent for
  adding an ETL source. Touched exactly the layout this task mirrors —
  `internal/model/git.go` (row structs), `internal/git/state.go` (watermark),
  `internal/git/extract.go` (ingest), `internal/writer/git.go` (parquet), and
  `cmd/run.go` + `cmd/run_only_test.go` (flag + phase). Its plan phased the work
  Model → State → Ingest → Writer → CLI/E2E; this plan follows the same spine.
- **Task 020 / commit `66e9c4c`**: `auto hooks fire` lives in
  `auto-cli/cmd/auto/hookscmd.go` (+ `hookscmd_test.go`); the same commit added
  the shared `auto-shared/config` (registry) and `auto-shared/git` packages —
  precedent for this task's new `auto-shared/hooks` shared package.

## Execution Facts (verified for plan.md)

### Module wiring — no go.mod/go.work change needed
- Root `go.work` (`/home/vscode/src/auto-stack/go.work`) already `use`s
  `./auto-cli`, `./auto-etl`, `./auto-shared`. Both `auto-cli/go.mod:81` and
  `auto-etl/go.mod:12` already have `replace github.com/mistakenot/auto-shared
  => ../auto-shared` and require `auto-shared v0.0.0`. The new package imports as
  `github.com/mistakenot/auto-shared/hooks` — a new sub-package of an
  already-required module, so **no go.mod/go.work edits**, just `go mod tidy` if
  the toolchain complains.

### auto-etl/cmd/run.go — exact insertion points
- `auto-etl/cmd/run.go:36-41` — `validOnlyValues` map; add `"hooks": true`.
- `auto-etl/cmd/run.go:122` — `parseOnlyFlag` default all-true map; add `hooks`.
- `auto-etl/cmd/run.go:110` — `--only` flag help string; add `hooks`.
- `auto-etl/cmd/run.go:71-72` — `remotes := loadRemotesCache()` /
  `hostID := loadHostID()`; the new phase needs only `hostID`.
- `auto-etl/cmd/run.go:74-94` — phase gates (`if sources["sessions"] {…}` …);
  insert `if sources["hooks"] { if err := runHooksETL(hostID); err != nil {…} }`.
- Signatures to mirror: `runSessionETL(hostID string, remotes map[string]string)
  error` (run.go:140), `runGitETL(...)` (run.go:366-455) is the full
  load-state→ingest→write→save-state template. `outputDir` is the package var
  (run.go:29), default `~/.auto/etl/output` (root.go:40-45).

### Writer pattern — reuse, don't reinvent
- `auto-etl/internal/writer/git.go:57-75` — the monthly read-merge-write loop
  (`readExistingParquet` → `mergeByID(…, func(c *T) string { return c.ID })` →
  `writeParquet`); copy this shape for `WriteHooks` grouping by `CapturedAt`
  month. Helpers are all in package `writer`: `mergeByID` (git.go:122-144),
  `readExistingParquet` (github.go:145-164), `writeParquet` (writer.go:87-105).

### Large-line read — concrete precedent + the chosen alternative
- `auto-etl/internal/parser/parser.go:206-207` — `bufio.NewScanner(f)` +
  `scanner.Buffer(make([]byte, 1024*1024), 1024*1024)`. The hooks ingest does
  **not** copy this (a 1 MiB buffer is marginally short once the envelope wraps a
  1 MiB payload) — it uses `bufio.Reader.ReadBytes('\n')` (no cap) per solution
  item 5.

### Test harness
- `auto-etl/e2e_test.go` — TestMain builds the `auto` binary once and runs the
  ETL against fixtures in `.tmp/claude/projects/`; a `readAllParquet[T](t,
  outputDir, subdir)` helper reads rows back via `parquet.NewGenericReader[T]`.
  Mirror for a hooks round-trip (seed JSONL → ingest+write → read back).
- `auto-cli/cmd/auto/hookscmd_test.go` (~107 lines) — `t.Setenv("HOME",
  t.TempDir())`, `httptest.Server` for the UI, `io.Discard` for output;
  `TestFireExitsZeroWhenUIDown` proves exit-0. Add log-appended + unwritable-dir
  tests in this style.

### Build / test / commit conventions
- Per-module: `cd <module> && go build ./...` after each file (CLAUDE.md Go Build
  Discipline); `cd <module> && go test ./...` for that module's tests.
- Whole repo: `make check` (= fmt-check + vet + lint + stale-refs), `make build`
  (builds the `auto` umbrella from `auto-cli`), `make test` (all modules). CI
  runs `make check`, `make build`, `make test`, `make vulncheck`.
- Pre-commit pipeline includes `auto doc fix` (informational, non-blocking),
  gofmt, `go vet`.
- Commit prefix style (verified from `git log`): `feat(022): …`, `test(022): …`,
  `docs(022): …`.
- Worktree discipline (CLAUDE.md): `git fetch origin && git checkout main && git
  pull origin main` before branching.
