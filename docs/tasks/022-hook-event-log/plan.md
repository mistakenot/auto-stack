---
hash: "a1e583e0"
id: "577b1a40"
read_when: "implementing hook event logging or the hooks ETL ingest pipeline"
summary: "Implementation plan for a durable daily-partitioned JSONL hook log written by auto hooks fire, and a new incremental hooks ETL source that ingests it into monthly-partitioned parquet using the proven git-ETL watermark pattern."
title: "Plan: Task 022 — Hook Event Log"
---

# Plan: Task 022

## Summary

Add a durable, daily-partitioned JSONL hook log written by `auto hooks fire`, and
a new incremental `hooks` ETL source that ingests it into a monthly-partitioned
parquet dataset — built on a shared `auto-shared/hooks` contract package and the
proven git-ETL watermark/merge pattern.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| + | `auto-shared/hooks/log.go` | `Envelope` type; `RawDir`/`LogPath(t)`; `Append`; `ExtractEventName`/`ExtractSessionID`/`ExtractTool`/`ExtractPaths` |
| + | `auto-shared/hooks/log_test.go` | append round-trip, path layout, UTC day, extractor cases |
| ~ | `auto-cli/cmd/auto/hookscmd.go` | append envelope (cwd+project) **before** POST; `hostIDQuietly()`; `buildHookEvent`/`extractPaths` delegate to `hooks.Extract*` |
| ~ | `auto-cli/cmd/auto/hookscmd_test.go` | log-line-written; failure-swallowed-still-exits-0-and-POSTs |
| + | `auto-etl/internal/model/hooks.go` | `HookEventRow` parquet struct (`schema_version` = 1) |
| + | `auto-etl/internal/hooks/state.go` | `HooksSyncState` (per-file byte offset); load/save atomic, lenient |
| + | `auto-etl/internal/hooks/state_test.go` | save/load round-trip, corrupt-tolerant |
| + | `auto-etl/internal/hooks/ingest.go` | discover `events-*.jsonl`, seek-from-offset, `ReadBytes('\n')`, parse → rows |
| + | `auto-etl/internal/hooks/ingest_test.go` | incremental, lenient, partial-line, large-line |
| + | `auto-etl/internal/writer/hooks.go` | `WriteHooks`: monthly read-merge-write by `ID` |
| + | `auto-etl/internal/writer/hooks_test.go` | merge dedup + raw_json round-trip |
| ~ | `auto-etl/cmd/run.go` | `validOnlyValues`+default+help; `runHooksETL(hostID)` phase |
| ~ | `auto-etl/cmd/run_only_test.go` | `parseOnlyFlag` accepts/normalizes `hooks`, default includes it (existing `--only` tests live here) |
| ~ | `auto-etl/docs/reference/normalized-schema.md` | document the `hooks` dataset |

<!-- RESOLVED(P3): parseOnlyFlag tests live in run_only_test.go, not run_test.go
REVIEW: The existing `parseOnlyFlag` tests are in `auto-etl/cmd/run_only_test.go` (verified:
it is the only test file referencing `parseOnlyFlag`; `run_test.go` does not). This Changes
table row and the How-to-Test checklist (line 40) point the implementer at `run_test.go`,
which would either fragment the `--only` test coverage across two files or have them recreate
a parseOnlyFlag test harness that already exists. Step 4.4 hedges with "(or run_only_test.go)",
but the authoritative file list here should name `run_only_test.go` so the AC-5 cases land
next to the existing ones.
AUTHOR: Renamed throughout — the Changes table row, the How-to-Test checklist entry, and Step
4.4 now all name `run_only_test.go` (and Step 4.4 drops the "(or …)" hedge, noting `run_test.go`
does not test `parseOnlyFlag`), so the AC-5 cases land beside the existing `--only` tests.
-->


## Links
- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test
- [x] `auto-shared/hooks/log_test.go` — `Append` writes one verbatim line + envelope; `LogPath` is UTC-dated; `Extract*` parse common + garbage payloads
- [x] `auto-cli/cmd/auto/hookscmd_test.go` — fire appends a durable line; unwritable raw dir → exit 0 and POST still fires
- [x] `auto-etl/internal/hooks/state_test.go` — watermark save/load round-trip, corrupt → empty
- [x] `auto-etl/internal/hooks/ingest_test.go` — only post-watermark lines ingested; malformed line → row with `raw_json`; >64 KiB line parsed; partial trailing line not advanced
- [x] `auto-etl/internal/writer/hooks_test.go` — re-ingest produces no duplicate rows; all columns incl. `raw_json` round-trip
- [x] `auto-etl/cmd/run_only_test.go` — `--only hooks` valid; default run includes hooks; invalid `--only` still rejected
- [x] E2E: pipe a sample payload to `auto hooks fire --agent claude` → line in `~/.auto/hooks/raw/`; `auto etl run --only hooks` → parquet rows read back with correct normalized + `raw_json`

## Execution Sequence
```
Phase 1 (shared contract: auto-shared/hooks)
        │
        ├─────────────► Phase 2 (producer: auto hooks fire)  ──────────┐
        │                                                              │
        └──► Phase 3 (consumer core: model/state/ingest/writer) ──► Phase 4 (consumer wiring: run.go) ──┐
                                                                                                         │
                                                                            Phase 5 (E2E + docs) ◄───────┘
                                                                            (depends on Phase 2 AND Phase 4)
```
Phases 2 and 3 can run in parallel after Phase 1. Phase 4 needs Phase 3. Phase 5 needs Phases 2 and 4.

## Plan

### Phase 1: Shared contract — `auto-shared/hooks`
Foundation imported by both producer and consumer; defines the file format and the single normalization implementation.

- [x] Step 1.1: Create `auto-shared/hooks/log.go` with the `Envelope` struct (`Agent`, `CapturedAt` RFC3339-UTC, `HostID`, `Cwd` omitempty, `Project` omitempty, `Payload json.RawMessage`).
- [x] Step 1.2: Add path helpers: `RawDir() (string, error)` → `<AutoDir>/hooks/raw` (built on `sharedconfig.AutoDir()`); `LogPath(t time.Time) (string, error)` → `<RawDir>/events-2006-01-02.jsonl` formatting `t` (caller passes `time.Now().UTC()`).
  - verify: a unit test asserts `LogPath(t)` for a fixed `t` yields `.../events-YYYY-MM-DD.jsonl` matching `t`'s UTC date.
- [x] Step 1.3: Add `Append(env Envelope) error`: `MkdirAll(RawDir, 0o755)`, marshal `env` to one line, `os.OpenFile(LogPath, O_APPEND|O_CREATE|O_WRONLY, 0o644)`, single `f.Write(line + "\n")`, close. No `bufio.Writer`.
  - verify: test appends two envelopes, reads the file back, asserts exactly two lines, each `json.Unmarshal`s to the original, and `Payload` bytes are byte-for-byte the input.
- [x] Step 1.4: Add the normalization extractors operating on `map[string]any` (move the logic from `hookscmd.go`): `ExtractEventName` (`hook_event_name`), `ExtractSessionID` (`session_id`), `ExtractTool` (`tool_name`), `ExtractPaths` (`tool_input` → `file_path`/`notebook_path`/`path`, sorted+deduped). Lenient (nil/typed-miss → zero value).
  - verify: tests cover a realistic PostToolUse payload (paths extracted, sorted, deduped) and a garbage payload (all return zero values, no panic).
- [x] Step 1.5: `cd auto-shared && go build ./... && go test ./hooks/...`
  - verify: build clean, all `hooks` tests pass.
- [x] Step 1.6: `cd auto-shared && gofmt -l hooks/` shows no files; `go vet ./hooks/...` clean.
- [x] Step 1.7: Commit: `feat(022): phase 1 — auto-shared/hooks envelope + extractors`

### Phase 2: Producer — durable append in `auto hooks fire`
Depends on Phase 1. Capture to disk first, then the existing best-effort POST.

- [x] Step 2.1: Add `hostIDQuietly() string` in `hookscmd.go` mirroring ETL's `loadHostID` (`sharedconfig.HostConfigPath` → `LoadHost` → fall back to `os.Hostname`/"unknown"); never errors.
- [x] Step 2.2: Refactor `buildHookEvent`/`extractPaths` to delegate to `hooks.ExtractEventName/SessionID/Tool/Paths` (remove the now-duplicated local `extractPaths`; keep `stringField` only if still used elsewhere, else move/remove).
  - verify: `cd auto-cli && go build ./...` clean; existing `hookscmd_test.go` tests still pass unchanged.
- [x] Step 2.3: In the `fire` RunE, after building the event and resolving project, construct an `Envelope{Agent, CapturedAt: now UTC RFC3339, HostID: hostIDQuietly(), Cwd: ev.Cwd, Project: ev.Project, Payload: raw}` and call `hooks.Append(env)` **before** `postHookEvent`. Swallow any append error to stderr; still exit 0; still POST.
  - verify: ordering is append-then-POST in source; the `raw` bytes (verbatim stdin) are the `Payload`.
- [x] Step 2.4: Add test `TestFireWritesDurableLog`: `t.Setenv("HOME", t.TempDir())`, pipe a sample payload, run fire, assert one line exists under `~/.auto/hooks/raw/events-*.jsonl` whose `Payload` equals the input and whose `agent`/`cwd`/`project`/`hostId` are populated. (AC-1)
- [x] Step 2.5: Add test `TestFireSwallowsLogFailure`: make the raw dir unwritable (e.g. create `~/.auto/hooks/raw` as a file, or chmod 0500 the parent), run fire with a `httptest.Server` UI, assert exit 0 **and** the POST was received. (AC-2)
  - verify: test passes; fire returns nil even when the append fails.
- [x] Step 2.6: `cd auto-cli && go build ./... && go test ./... && gofmt -l cmd/ && go vet ./...`
  - verify: build + all tests pass, no fmt/vet issues.
- [x] Step 2.7: Commit: `feat(022): phase 2 — auto hooks fire writes durable JSONL log`

### Phase 3: Consumer core — model, state, ingest, writer
Depends on Phase 1. Independent of Phase 2.

- [x] Step 3.1: Create `auto-etl/internal/model/hooks.go` with `HookEventRow` (fields per solution outline: `ID`, `HostID`/`Agent`/`Event`/`SessionID` dict, `Cwd`, `Project` dict, `Tool` dict, `PathsJSON`, `CapturedAt` int64 ms, `RawJSON`, `SourceFile` dict, `Year`/`Month`/`SchemaVersion` int32). Add a `HookSchemaVersion = 1` const.
  - verify: `cd auto-etl && go build ./internal/model/...` clean.
- [x] Step 3.2: Create `auto-etl/internal/hooks/state.go`: `HooksSyncState{SchemaVersion int; Files map[string]*FileState}`, `FileState{Offset int64}`, `HooksSyncStatePath()` → `~/.auto/etl/hooks/sync-state.json`, `LoadHooksSyncState(path)` (lenient: missing/corrupt → empty, nil-map guard), `(*HooksSyncState).Save(path)` (temp+rename) — direct analog of `internal/git/state.go`.
- [x] Step 3.3: Add `state_test.go`: save→load round-trip; corrupt JSON → empty state; offset get/set.
  - verify: `cd auto-etl && go test ./internal/hooks/...` passes for state.
- [x] Step 3.4: Create `auto-etl/internal/hooks/ingest.go`: `Ingest(rawDir string, state *HooksSyncState, fallbackHostID string) ([]model.HookEventRow, error)` — **no registry param** (project is copied from the envelope, not re-resolved at ETL). List `events-*.jsonl` sorted; per file: `size := stat`; if `state.Files[name].Offset >= size` skip; else open, `Seek(offset)`, `r := bufio.NewReader(f)`, loop `r.ReadBytes('\n')`:
  - a chunk ending in `\n` → one complete line: parse `Envelope` (lenient), build a `HookEventRow` — copy `agent`/`cwd`/`project`/`host` from the envelope (use `host := env.HostID; if host == "" { host = fallbackHostID }`), derive `event`/`session`/`tool`/`paths` from `Payload` via `hooks.Extract*`, set `RawJSON` = string(Payload), `CapturedAt` parsed from envelope (fallback 0), `Year`/`Month` from CapturedAt UTC, `SourceFile` = name, `ID` = `sha256(host\x00name\x00offsetStart)` where `host` is the envelope HostID (host-stable id, identical no matter which host ingests the line), `SchemaVersion` = `HookSchemaVersion`; advance the running offset.
  - a final chunk with `io.EOF` and no `\n` → in-flight partial: discard, do **not** advance offset.
  - malformed envelope/payload → still emit a row with `RawJSON` + whatever parsed; never abort. (AC-6)
  - After the file, write the advanced offset back into `state.Files[name]`.

<!-- RESOLVED(P2): `registry` param to Ingest is dead after the project-in-envelope decision
REVIEW: The RESOLVED(P2) thread in solution.md (lines 42-63) moved `project` resolution to the
producer: the envelope now carries a producer-resolved `Project`, and solution item 5 says ETL
should "copy `agent`/`cwd`/`project`/`host` straight from the envelope" — explicitly NOT
re-resolving project at ETL time. But this step still declares
`Ingest(rawDir string, state *HooksSyncState, hostID string, registry sharedconfig.ProjectsConfig)`
and Step 4.2 still has `runHooksETL` "load registry ... or `sharedconfig.LoadProjects`" to pass
it in. With project copied from the envelope, the `registry` argument is never used inside
Ingest — it directly contradicts the resolved design and re-introduces the ETL-host registry
lookup that resolution was meant to eliminate. Drop the `registry` parameter from `Ingest`'s
signature and remove the registry load from `runHooksETL` (Step 4.2). (Go won't flag an unused
param, so this will silently ship as misleading dead wiring unless corrected here.)
AUTHOR: Dropped. Step 3.4's signature is now `Ingest(rawDir, state, fallbackHostID)` with an
explicit "no registry param" note, and Step 4.2 no longer loads a registry (project is copied
from the envelope; `hostID` is passed only as the empty-envelope fallback). This is consistent
with the host-stable-id fix in the adjacent solution.md thread — Ingest no longer touches the
ETL-host registry at all.
-->

- [x] Step 3.5: Add `ingest_test.go`: (a) two lines, ingest, assert two rows + offset advanced; (b) re-ingest same state → zero new rows (AC-4); (c) append a third line, ingest → one new row only (AC-4); (d) a garbage line → one row with `RawJSON` intact, run not aborted (AC-6); (e) a >64 KiB line (e.g. 200 KiB `tool_input`) → parsed into a row (proves not using default Scanner); (f) a file ending without `\n` → last partial not ingested and offset left before it.
  - verify: all ingest tests pass.
- [x] Step 3.6: Create `auto-etl/internal/writer/hooks.go`: `WriteHooks(outputDir string, rows []model.HookEventRow) error` grouping rows by `{Year, Month}`, per partition `path = <outputDir>/hooks/year=YYYY/month=MM/hooks.parquet`, `readExistingParquet[model.HookEventRow]` → `mergeByID(existing, incoming, func(r *model.HookEventRow) string { return r.ID })` → `writeParquet`. Mirror `git.go:57-75`.
- [x] Step 3.7: Add `hooks_test.go` (writer): write rows, read back via `NewGenericReader`, assert all columns incl. `RawJSON`; write again with an overlapping `ID` → assert dedup (no duplicate row). (AC-3, AC-4)
- [x] Step 3.8: `cd auto-etl && go build ./... && go test ./internal/hooks/... ./internal/writer/... ./internal/model/... && gofmt -l internal/ && go vet ./internal/...`
  - verify: build + tests pass, no fmt/vet issues.
- [x] Step 3.9: Commit: `feat(022): phase 3 — hooks ETL model, watermark, ingest, writer`

### Phase 4: Consumer wiring — `auto etl run --only hooks`
Depends on Phase 3.

- [x] Step 4.1: In `run.go`, add `"hooks": true` to `validOnlyValues` (lines 36-41) and to the `parseOnlyFlag` default all-true map (line 122); add `hooks` to the `--only` help string (line 110).
- [x] Step 4.2: Add `runHooksETL(hostID string) error`: `statePath := hookstate.HooksSyncStatePath()`, `state := LoadHooksSyncState(statePath)`, `rawDir := hooks.RawDir()`, `rows, err := hooksingest.Ingest(rawDir, state, hostID)` (no registry load — project comes from the envelope; `hostID` is only the empty-envelope fallback); if rows, `writer.WriteHooks(outputDir, rows)`; **then** `state.Save(statePath)` (write-before-save ordering). Handle a missing rawDir as a clean no-op.
- [x] Step 4.3: Wire the phase gate in RunE (after the `git` gate, lines 74-94): `if sources["hooks"] { if err := runHooksETL(hostID); err != nil { return err } }`.
  - verify: `cd auto-etl && go build ./...` clean.
- [x] Step 4.4: Extend `run_only_test.go` (the existing home of the `parseOnlyFlag` tests — `run_test.go` does not test it): `parseOnlyFlag(["hooks"])` → map with `hooks:true`; `parseOnlyFlag(["HOOKS"])` normalizes; default (`nil`) includes `hooks:true`; an invalid value still errors. (AC-5)
- [x] Step 4.5: `cd auto-etl && go build ./... && go test ./... && gofmt -l . && go vet ./...`
  - verify: full module build + tests pass.
- [x] Step 4.6: Commit: `feat(022): phase 4 — wire hooks source into auto etl run`

### Phase 5: End-to-end, dogfood, and docs
Depends on Phases 2 and 4.

- [x] Step 5.1: Add an e2e in `auto-etl/e2e_test.go` (or a focused integration test): seed `~/.auto/hooks/raw/events-<today>.jsonl` with ≥2 hand-written envelope lines (one normal PostToolUse, one garbage), run the hooks ingest+write against a temp output dir, read back the parquet via the `readAllParquet` helper, and assert: row count, normalized columns populated for the good line, `RawJSON` verbatim for both, the garbage line still produced a row. (AC-3, AC-6)
  - verify: e2e passes.
- [x] Step 5.2: Real-binary dogfood: `make build`, then `printf '<sample-claude-payload>' | ./build/auto hooks fire --agent claude`; assert a line landed in `~/.auto/hooks/raw/`; run `./build/auto etl run --only hooks`; assert `~/.auto/etl/output/hooks/year=*/month=*/hooks.parquet` exists; run it **again** and assert no duplicate rows (re-read count unchanged). (AC-1, AC-3, AC-4, AC-5)
  - verify: capture the row counts before/after the second run; they match.
- [x] Step 5.3: Document the `hooks` dataset in `auto-etl/docs/reference/normalized-schema.md` — column list, monthly partition path `hooks/year=YYYY/month=MM/hooks.parquet`, the raw-log source `~/.auto/hooks/raw/events-YYYY-MM-DD.jsonl`, and the watermark sync-state path. Run `auto doc fix` (informational) and address any `[autodoc()]` link it flags.
  - verify: `auto doc stale` reports no new stale entries for the touched docs (or the pre-commit `auto doc fix` passes informationally).
- [x] Step 5.4: Repo-wide gates: `make check && make build && make test`.
  - verify: all pass.
- [x] Step 5.5: Commit: `feat(022): phase 5 — hooks e2e, dogfood, schema docs`

## Success Criteria
- [x] `cd auto-shared && go test ./hooks/...` passes (envelope, paths, extractors)
- [x] `cd auto-cli && go test ./...` passes (durable log written; failure swallowed, still exits 0 + POSTs) — **AC-1, AC-2**
- [x] `cd auto-etl && go test ./...` passes (state, ingest incremental/lenient/large-line, writer dedup, `--only hooks`) — **AC-3, AC-4, AC-5, AC-6**
- [x] `make check && make build && make test` all green
- [x] Manual dogfood: `auto hooks fire` appends a verbatim line under `~/.auto/hooks/raw/`; `auto etl run --only hooks` writes `~/.auto/etl/output/hooks/.../hooks.parquet`; a second run adds no duplicate rows
- [x] `auto etl run` with no `--only` includes the hooks phase; `--only hooks` runs it alone; help text lists `hooks`
- [x] No new go.work/go.mod edits required; `auto-shared/hooks` imported cleanly by both modules

## Open Questions
- (none — all requirements Open Questions resolved; all review threads RESOLVED)
