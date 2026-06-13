---
hash: "06bfe957"
id: "a4827708"
read_when: "implementing or reviewing the reflect miner queue feature and understanding the event log and parquet schema touchpoints"
summary: "Verified codebase facts for implementing the reflect miner queue: event log type constants, envelope schema, ETL parquet reader locations, and reflect stats command surface."
title: "Context: Task 023 — Reflect Miner Queue"
---

# Context: Task 023 — reflect-miner-queue

Verified codebase facts grounding [solution.md](solution.md): where the reflect event log, ETL
parquet schema, parquet readers, and `reflect stats` live, and the exact migration surface for the
shared-model extraction.

## Key Files

### Reflect event log (where `session_mined` is added)
- `auto-reflect/internal/events/model.go:14` — `const SchemaVersion = 1`.
- `auto-reflect/internal/events/model.go:16-30` — event-type consts (`TypeRuleCreated`,
  `TypeObservation`, `TypeConsolidation`, …); add `TypeSessionMined = "session_mined"` here.
- `auto-reflect/internal/events/model.go:49-60` — `Event` envelope: `ID, Type, SchemaVersion, Seq,
  TS, Host, SessionID, Agent, Git, Payload`. Envelope `SessionID` = the emitting agent's session,
  so the mined target session ID goes in the payload, not the envelope.
- `auto-reflect/internal/events/log.go:38-97` — `AppendEvent(cwd, eventType, payload, opts)`:
  exclusive file lock + monotonic per-shard seq. Use this for `session_mined`.
- `auto-reflect/internal/events/log.go:190-259` — `ReadAll(repoRoot)` / `ReadAllSharded(repoRoot)`
  return events in deterministic `(ts, shard, seq)` order; fold over these for coverage.
- `auto-reflect/internal/events/shard.go:17-27` — shard name `<host>-<YYYY-MM-DD>-<wt8>.jsonl`;
  `wt8` = first 8 hex of SHA-256 of the worktree root (per-repo isolation). Confirms the event log
  is already repo-scoped, consistent with the miner's default workspace scope.
- `auto-reflect/internal/store/paths.go:10-35` — `EventsDir(repoRoot)` →
  `<repoRoot>/.auto/reflect/events`; playbook at `~/.auto/reflect/playbook.json`.

### Command wiring
- `auto-reflect/internal/cli/root.go:55-82` — `NewRootCmd()` adds each command group via
  `cmd.AddCommand(...)`. Add `newMinerCmd(application)` here.
- `auto-reflect/internal/cli/rule.go:18-32` — parent-command + subcommand pattern to mirror for
  `miner` (`next`/`ack`/`status`). `observation.go:20-30` is the simpler two-subcommand template.

### reflect stats (where `pending_to_mine` is added)
- `auto-reflect/internal/loop/service.go:205-208` — `type StatsReport struct {
  UnconsolidatedObservations int; Rules []RuleStats }`. Add `PendingToMine int`.
- `auto-reflect/internal/cli/stats.go:13-50` — `stats --format json|text`; text branch prints
  `unconsolidated_observations=%d`. Add the `pending_to_mine` line alongside.

### ETL parquet schema (the move target)
- `auto-etl/internal/model/model.go:28-102` — `AgentMessage` (40 fields; `content`,
  `content_truncated`, `role,dict`, `tool_name,dict`, `is_error`, `session_id,dict`, `is_subagent`,
  `parent_session_id,dict`, `workspace,dict`, `git_remote,dict`, …).
- `auto-etl/internal/model/model.go:105-154` — `AgentSession` (28 fields; `id`, `is_subagent`,
  `parent_session_id`, `workspace,dict`, `git_remote,dict`, `last_message_at`, `transcript_truncated`,
  …). **No `message_count` field** — compute it from the message scan.
- `auto-etl/internal/model/model.go:5-25,163-178` — also movable: `SchemaVersion`,
  `DefaultTruncateMaxChars`, `IntentTruncateMaxChars`, `DefaultTranscriptMaxChars`, `MessageRole`
  + role consts, `PartitionKey`, `WeekPartition`, `MonthPartition`. File imports only `time` — no
  auto-etl internal deps, so the move is clean.
- `auto-etl/internal/model/model.go:157-160` — `TransformedRows` **stays** (references the session/
  message slices; lives in the transform pipeline). `git.go` + `github.go` in the same package are
  orthogonal and do not move.

### Existing parquet read pattern (to reuse in auto-reflect)
- `auto-search/internal/etlscan/parquet_messages.go:13-49` and `parquet_sessions.go:14-49` —
  canonical reader: `parquet.OpenFile` → `parquet.NewGenericReader[T](pf)` → batched `Read` until
  `io.EOF`. Library: `github.com/parquet-go/parquet-go`.
- `auto-search/internal/etlscan/discover.go:32-83` — `Discover(inputRoot)` walks `messages/` and
  `sessions/` subdirs, returns `[]ParquetSource{dataset, partitionKey, path, …}`.
- `auto-search/internal/config/settings.go:62-68` — ETL output root = `~/.auto/etl/output`.
  Partitioning: `sessions/year=YYYY/month=MM/sessions.parquet` (monthly),
  `messages/year=YYYY/week=WW/messages.parquet` (weekly → enables `--since` partition pruning).

### Shared-model extraction surface (verified consumers)
- `auto-search/internal/model/parquet.go:1-113` — `ParquetSessionRow` (28 fields) and
  `ParquetMessageRow` (40 fields). **Field-for-field, tag-for-tag identical** to auto-etl's structs
  (diffed in full). Safe to delete and replace with the shared types.
- Writers/constructors that must keep identical tags: `auto-etl/internal/writer/writer.go:21,69,78`
  (`parquet.NewGenericWriter[model.AgentMessage|AgentSession]`),
  `auto-etl/internal/transform/transform.go:43-69` (populates the structs).
- auto-search consumers to re-point: `etlscan/parquet_{sessions,messages}.go`,
  `indexdb/indexer.go:162,181` (`insert*FromParquet`), `testutil/fixtures.go`,
  `testutil/stats_fixtures.go`.
- `auto-etl/internal/model` is **not imported outside auto-etl** (internal/ restriction; only stale
  *comments* in auto-search/cochange reference it) — confirmed no hidden cross-module coupling.
- No golden/asserted parquet schema files; auto-search fixtures are generated at test time
  (`testutil/fixtures.go`), so the move cannot break a frozen-schema assertion.

### Module layout
- `go.work:1-16` — workspace includes `./auto-shared`, `./auto-etl`, `./auto-search`,
  `./auto-reflect`.
- `auto-shared/go.mod` — module `github.com/mistakenot/auto-shared`, go 1.26.1, **no parquet-go dep
  yet** (adding it is fine; both downstream modules already depend on `parquet-go v0.x`).
- `auto-etl`, `auto-search`, `auto-reflect` each `replace github.com/mistakenot/auto-shared =>
  ../auto-shared`. auto-shared subpackages today: `bus/`, `config/`, `git/`, `update/`, `version/`
  — add `model/` alongside.

<!-- RESOLVED(P3): subpackage list omits `bus/`
REVIEW: `ls auto-shared/` shows `bus config git update version` — the existing subpackage list here misses `bus/` (likely landed via task 021). Harmless to the move, but keep the inventory accurate.
AUTHOR: Verified (`ls -d auto-shared/*/` → bus, config, git, update, version). Inventory corrected to include `bus/`.
-->

- `auto-shared/git` (`detect.go`, `normalize.go`) — reuse for normalizing the current repo's remote
  and each session's `git_remote` for the **default scope key** (AC-1c scopes by normalized
  `GitRemote`, worktree-stable, with a `Workspace` path-prefix fallback when the repo has no remote).

### Consumed command (the skill's content fetch — not built here)
- `auto-search/internal/cli/session.go:191-232` — `auto search session get <id>` renders a session
  transcript for agent consumption. The miner returns IDs only; the skill calls this for content.

## Patterns
- **Event-sourced, fold-on-read**: state (playbook, and now mining coverage) is a fold over
  append-only sharded JSONL — never a separate mutable store (AC-7). The `rules` package
  (`rules/model.go:65-69` `FoldedThrough map[string]int`) is the precedent for per-shard fold
  bookkeeping; the miner's coverage fold is simpler (latest ack version per session ID).
- **JSON-default CLI**: stdout = parseable payload only, stderr = diagnostics, `--text` for human
  mode (docs/auto-package-patterns.md). `reflect stats` already does the json/text split.
- **Parquet column projection**: `NewGenericReader[T]` reads only columns named in `T`'s tags —
  the basis for the `MsgSignalRow` projection that avoids the large `content` column.
- **Resource noun + verb (`describe`/`get`)**: `docs/auto-package-patterns.md:239-295` — domain data
  gets `list`/`describe <id>`/`get <id>`; cheap rungs return IDs + metadata only, `describe`
  summarizes, truncation prints the recovery command. This is the convention AC-11's
  `miner describe`/`signals` follow and the `fetch_cmd` hint implements.
- **JSON contract + exit codes**: `docs/auto-package-patterns.md:296-312` — stdout = parseable JSON
  only, stderr = diagnostics/errors, exit 1 on error even with partial results; every hard error
  names a remediation (`:468`). This is the basis for AC-8's three-way exit contract and the
  `run auto etl run` hint.
- **Source-state inputs (AC-8)**: `auto-search/internal/etlscan/discover.go:32-83` distinguishes a
  *missing* root (the dir doesn't exist / `os.Stat` fails) from an *empty* one (dir present, walk
  yields zero `ParquetSource`) — the two signals `ResolveSource` maps to `SourceMissing` vs
  `SourceEmpty` vs `SourceOK`, so `coverage_pct` is never divided over an absent source.

## Plan-stage verification (execution-critical facts)

- **Extraction blast radius is small and bounded.** `auto-etl/internal/model/model.go` imports only
  `time` (no internal deps), so the session/message schema + partition helpers move cleanly. Inside
  auto-etl only `internal/transform/transform.go` and `internal/writer/writer.go` use the moved
  symbols; `writer/git.go`, `internal/github/*`, `internal/git/*` use the **git/github** model types
  (`GitETLResult`, `GitRepository`, `GitRef`, `Commit`, `CommitFile`, `CommitHunk`, …) which **stay
  in auto-etl**. `TransformedRows` references the session/message structs, so it moves with them (or
  stays and imports the shared types — either is fine).
- **auto-search mirror is byte-identical.** `ParquetSessionRow`/`ParquetMessageRow` match
  `AgentSession`/`AgentMessage` field-for-field and tag-for-tag (verified), so deleting them in
  favour of the shared types is safe. Consumers to re-point: `etlscan/parquet_{sessions,messages}.go`,
  `indexdb/indexer.go`, `testutil/fixtures.go`, `testutil/stats_fixtures.go`.
- **Whole-workspace-green in one phase.** go.work means `make build`/`make test` compile every
  module; the extraction + both consumer re-points must land together so auto-etl and auto-search
  stay buildable. (Mirrors task 022's "shared contract first, then consumers" phase 1.)
- **auto-shared has no `go.sum` yet** and does not require `parquet-go`. Creating `auto-shared/model`
  adds `github.com/parquet-go/parquet-go v0.29.0` (already used by auto-etl + auto-search) and needs
  `go mod tidy` in auto-shared. No go.work edit (all modules already listed).
- **Event-type registration point**: new `TypeSessionMined` must be added to the `Validate(e *Event)`
  switch in `auto-reflect/internal/events/model.go:~196` (payload-specific validation lives in the
  owning package, e.g. `miner`). `AppendEvent(cwd, eventType string, payload any, opts) (Event, error)`
  is the append path; `ReadAll(repoRoot) ([]Event, error)` the fold source.
- **`quickstart` already exists** at `auto-reflect/internal/cli/quickstart.go` — the friction-win
  "mention miner" edit goes there, not root.go. `--format json|text` flag pattern:
  `cli/stats.go:48` (`StringVar(&format, "format", "json", …)`).
- **Deterministic-scoring precedent**: `auto-search/internal/cochange/score.go` (weighted scalar
  scores with a `safeDiv` guard that returns 0 when the denominator ≤ 0 — the pattern for the
  length-floor / divide-by-zero guards in AC-5/AC-8); `auto-reflect/internal/rules/match.go`
  (keyword-overlap matcher — precedent for cheap text-signal extraction, no LLM).
- **Build/test**: auto-reflect has **no local Makefile** — `cd auto-reflect && go build ./... &&
  go test ./...`; gofmt + `go vet ./...` per module. Global gates: root `make check && make build &&
  make test` (`make check` = fmt-check + vet + lint + stale-refs; it does **not** build/test).
- **Cross-module commit style** (tasks 020–022): one commit per phase, `feat(023): phase N — …`,
  with the `Co-Authored-By: Claude Opus 4.8` trailer. Dispatch subagents **serially** in a shared
  worktree and verify files on disk before the next (017/018 lesson).

## Related Tasks
- Epic `docs/epics/001-reflect-playbook-loop.md` — task 023 is the deterministic work-queue +
  coverage tracker that unblocks sub-task 2.1 (the mining skill). 2.1 mines; 2.2 consolidates.
- Task 022 (hook-event-log) — a **separate** log (`~/.auto/hooks/raw/…`, hook payloads). Task
  023's reflect event log (`.auto/reflect/events/`, mining coverage) is a distinct, pre-existing
  log; no overlap.
- Task 021 (auto-bus-standard) — the live event bus; orthogonal to the reflect coverage log this
  task folds.
