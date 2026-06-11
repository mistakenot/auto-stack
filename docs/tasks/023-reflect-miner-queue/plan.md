# Plan: Task 023 — reflect-miner-queue

## Summary

Hoist the canonical session/message parquet schema into `auto-shared/model` (re-pointing auto-etl +
auto-search), then build a `miner` command group in `auto-reflect` that reads that parquet, ranks
unmined top-level sessions by deterministic text signals, and tracks coverage (`session_mined`
events with status + score snapshot) on the existing append-only reflect event log.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| + | `auto-shared/model/schema.go` | Canonical `AgentSession`/`AgentMessage`/`MessageRole`+consts, schema/truncate consts, `PartitionKey`/`WeekPartition`/`MonthPartition` (tags unchanged) |
| ~ | `auto-shared/go.mod`,`go.sum` | add `parquet-go v0.29.0`; `go mod tidy` |
| ~ | `auto-etl/internal/model/model.go` | remove moved decls; keep git/github types + `TransformedRows` (imports `auto-shared/model`) |
| ~ | `auto-etl/internal/{transform,writer}/*.go` | re-point session/message struct refs to `auto-shared/model` |
| - | `auto-search/internal/model/parquet.go` | delete `ParquetSessionRow`/`ParquetMessageRow` (now redundant) |
| ~ | `auto-search/internal/{etlscan,indexdb,testutil}/*.go` | re-point to `auto-shared/model` types |
| ~ | `auto-reflect/go.mod` | add `parquet-go` require |
| + | `auto-reflect/internal/etlread/read.go` | `Discover`, `ReadSessions` (shared model), `ReadMessageSignals` (projection), `ResolveSource` (AC-8) |
| ~ | `auto-reflect/internal/events/model.go` | `TypeSessionMined`, `AckStatus`, `SessionMinedPayload`, `Validate` case |
| + | `auto-reflect/internal/miner/{miner,score}.go` | `Version`, coverage fold (terminal vs retryable), `Next`/`Describe`/`SignalsFor`, signals + scoring |
| + | `auto-reflect/internal/cli/miner.go` | `next`/`ack`/`status`/`describe`/`signals`; source-state errors + exit codes |
| ~ | `auto-reflect/internal/cli/root.go` | `AddCommand(newMinerCmd(...))` |
| ~ | `auto-reflect/internal/cli/quickstart.go` | mention `miner` |
| ~ | `auto-reflect/internal/loop/service.go` + `cli/stats.go` | `StatsReport.PendingToMine` + output |
| + | `auto-reflect/cmd/autoreflect/e2e_miner_test.go` | e2e next/ack/status/describe + source-state |
| ~ | `auto-etl/docs/reference/normalized-schema.md` | note the `auto-shared/model` relocation |

## Links
- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test
- [ ] `auto-shared/model` — compiles; `auto-etl` + `auto-search` suites stay green after re-point
- [ ] `auto-reflect/internal/etlread/read_test.go` — reads fixture parquet; `MsgSignalRow` projection excludes `content`; `ResolveSource` distinguishes missing/empty/ok
- [ ] `auto-reflect/internal/events/model.go` validation test — `session_mined` accepted; bad payloads rejected
- [ ] `auto-reflect/internal/miner/{miner,score}_test.go` — fold (terminal vs `failed` retryable; version bump), scope, dedupe, deterministic score + length-floor, `prior_ack`/`remined`
- [ ] `auto-reflect/internal/cli/miner_test.go` — `ack --status` round-trip; `describe`/`signals` write nothing; source-state errors + exit codes
- [ ] `auto-reflect/cmd/autoreflect/e2e_miner_test.go` — full `next → ack → status` against a fixture `~/.auto/etl/output`; `describe`; `--all`/`--since`

## Execution Sequence
```
Phase 1 (auto-shared/model + re-point auto-etl & auto-search)
   --> Phase 2 (etlread + session_mined event type)
        --> Phase 3 (miner service: fold + scoring + Next/Describe/SignalsFor)
             --> Phase 4 (miner CLI + source-state + wiring)
                  --> Phase 5 (reflect stats + docs + e2e dogfood)
```
Linear: each phase imports the prior. Within Phase 1 the auto-etl and auto-search re-points are
disjoint but must land together to keep the workspace green; dispatch serially in the shared
worktree and verify on disk before proceeding (017/018 lesson).

## Plan

### Phase 1: Extract canonical parquet schema into `auto-shared/model`
- [ ] Step 1.1: Create `auto-shared/model/schema.go` — move `AgentSession`, `AgentMessage`,
      `MessageRole` + role consts, `SchemaVersion`, `DefaultTruncateMaxChars`,
      `IntentTruncateMaxChars`, `DefaultTranscriptMaxChars`, `PartitionKey`, `WeekPartition`,
      `MonthPartition` verbatim from `auto-etl/internal/model/model.go` (parquet tags **unchanged**).
  - verify: `cd auto-shared && go build ./model/` passes.
- [ ] Step 1.2: Add `github.com/parquet-go/parquet-go v0.29.0` to `auto-shared/go.mod`; `go mod tidy`
      in auto-shared (creates go.sum).
  - verify: `cd auto-shared && go build ./... && go vet ./...` pass; go.sum present.
- [ ] Step 1.3: In `auto-etl/internal/model/model.go` delete the moved decls; keep git/github types
      and `TransformedRows` (have it reference `sharedmodel.AgentSession/AgentMessage`). Re-point
      `internal/transform/transform.go` and `internal/writer/writer.go` (+ their `_test.go` and
      `auto-etl/e2e_test.go`) to import `github.com/mistakenot/auto-shared/model`.
  - verify: `cd auto-etl && go build ./... && go test ./...` pass (writer/transform/e2e green —
      proves the move is behaviour-preserving, partitions still readable).
- [ ] Step 1.4: Delete `auto-search/internal/model/parquet.go`; re-point
      `etlscan/parquet_{sessions,messages}.go`, `indexdb/indexer.go`, `testutil/fixtures.go`,
      `testutil/stats_fixtures.go` to `sharedmodel.AgentSession/AgentMessage`.
  - verify: `cd auto-search && go build ./... && go test ./...` pass.
- [ ] Step 1.5: `gofmt -l` + `go vet ./...` in all three modules; then `make build && make test`.
  - verify: all green — the workspace compiles end-to-end with one schema source of truth.
- [ ] Step 1.6: Commit: `feat(023): phase 1 — hoist parquet schema into auto-shared/model`

### Phase 2: ETL read layer + `session_mined` event type (auto-reflect)
- [ ] Step 2.1: Add `parquet-go` to `auto-reflect/go.mod`; create
      `auto-reflect/internal/etlread/read.go` porting auto-search's read pattern:
      `Discover(root)`, `ReadSessions(path) []sharedmodel.AgentSession`, and
      `ReadMessageSignals(path) []MsgSignalRow` (narrow projection — never reads `content`).
      Resolve the ETL root via `sharedconfig.AutoDir()` → `~/.auto/etl/output`.
  - verify: `cd auto-reflect && go build ./internal/etlread/` passes.
- [ ] Step 2.2: Add `ResolveSource(etlRoot) (SourceState, error)` (AC-8) distinguishing
      `SourceMissing` (dir absent / stat fails) from `SourceEmpty` (present, zero parquet sources)
      from `SourceOK`.
  - verify: `read_test.go` — fixture dirs prove all three states; projection read on a fixture
      parquet returns rows with empty `Content` (column excluded). `go test ./internal/etlread/`.
- [ ] Step 2.3: In `auto-reflect/internal/events/model.go` add `TypeSessionMined = "session_mined"`,
      `type AckStatus string` (+ `mined/empty/failed/skipped` consts), `SessionMinedPayload`
      (`session_id, miner_version, status, observations, priority_score, signals`), and a
      `Validate` switch case for the new type.
  - verify: validation unit test — well-formed `session_mined` passes; bad status / missing
      session_id rejected. `go test ./internal/events/`.
- [ ] Step 2.4: `gofmt -l` + `go vet`; `cd auto-reflect && go build ./... && go test ./...`.
  - verify: all green.
- [ ] Step 2.5: Commit: `feat(023): phase 2 — etlread reader + session_mined event type`

### Phase 3: Miner service — coverage fold + scoring + queue assembly
- [ ] Step 3.1: `auto-reflect/internal/miner/score.go` — `Signals` struct (labelled, unitful) and a
      deterministic `Score(sig Signals) float64` from fixed weights over correction density,
      `tool_error_count`, failure markers, AskUserQuestion count; normalize by message count with a
      **length floor** (`length_floor_applied`) so short noisy sessions don't inflate; guard
      divide-by-zero (cochange `safeDiv` pattern).
  - verify: `score_test.go` — identical inputs → identical score; floor caps a 5-msg session;
      no NaN/Inf. `go test ./internal/miner/`.
- [ ] Step 3.2: `auto-reflect/internal/miner/miner.go` — `const Version = 1`;
      `FoldCoverage(evs) map[string]minedState` where a session is **terminal at V** only with a
      `mined/empty/skipped` ack at V (`failed` recorded but never terminal — stays retryable).
  - verify: `miner_test.go` — `failed`-only session stays pending; `mined@v1` excluded at v1,
      reappears when `Version`→2; quality mean counts `mined` only (AC-2, AC-4, AC-9).
- [ ] Step 3.3: `Next(repoRoot, etlRoot, opts) ([]WorkItem, error)` — read sessions, filter to
      top-level (`is_subagent=false`) scoped by **normalized `git_remote`** matching the current repo
      (worktree-stable; `Workspace` path-prefix fallback when no remote; `--all` widens),
      exclude terminal-at-current-version, score the survivors via a `ReadMessageSignals` pass,
      **dedupe by session_id**, attach `prior_ack`/`remined` (AC-10) + `fetch_cmd` hint, and with
      `--include-subagents` attach child IDs (`parent_session_id==id`) — top-level-only queue with
      subagents riding under the parent (AC-1b), normalized-remote scope by default (AC-1c). `cwd` in
      each item is the session `Workspace` column. Sort by descending score.
  - verify: `miner_test.go` — top-level-only; remote-scope groups two worktrees of one repo into one
      queue; `--all` widens; no duplicate IDs; every
      item has `fetch_cmd`; re-mined item has non-null `prior_ack`+`remined=true`
      (AC-1, AC-1b, AC-1c, AC-10).
- [ ] Step 3.4: `Describe(repoRoot, etlRoot, id)` + `SignalsFor(repoRoot, etlRoot, ids)` (AC-11) —
      compute signals + full ack history for any session regardless of ack/subagent state; **no
      writes**.
  - verify: `miner_test.go` — returns signals for an acked session and a subagent session that
      `Next` would exclude; no event appended (AC-11).
- [ ] Step 3.5: `gofmt -l` + `go vet`; `cd auto-reflect && go build ./... && go test ./...`.
  - verify: all green.
- [ ] Step 3.6: Commit: `feat(023): phase 3 — miner fold, scoring, next/describe/signals`

### Phase 4: Miner CLI + source-state contract + wiring
- [ ] Step 4.1: `auto-reflect/internal/cli/miner.go` — `newMinerCmd` (parent) with `next`, `ack`,
      `status`, `describe`, `signals`; `--format json|text` (default json), `--limit`, `--all`,
      `--include-subagents`, `--since`. `ack` takes `<id> --status mined|empty|failed|skipped`
      (default mined) + `--observations N`, scores the session for the snapshot, appends via
      `events.AppendEvent`. JSON to stdout, diagnostics to stderr.
  - verify: `cli/miner_test.go` — `ack --status` writes one `session_mined` event with the
      snapshot; re-ack appends a second (history). `next`/`status` emit parseable JSON on stdout
      (AC-3, AC-6).
- [ ] Step 4.2: Source-state contract (AC-8): `next`/`status` call `ResolveSource` first — missing/
      unreadable → stderr error with `run auto etl run` hint + **non-zero exit**; `coverage_pct` is
      JSON `null` when `total_sessions==0`; empty `next` (exit 0, `[]`) writes a stderr hint
      distinguishing *drained, try --all* from *source empty*. Document the exit contract in help.
  - verify: `cli/miner_test.go` — missing source → exit≠0 + stderr; empty source → `coverage_pct:
      null`; empty workspace → exit 0 + drained hint on stderr.
- [ ] Step 4.3: `status` honors `--all` symmetrically with `next`; `--since` honored for windowed
      counts (or rejected with a clear error). Add `describe`/`signals` output (read-only).
  - verify: `cli/miner_test.go` — `status --all` aggregates across workspaces; `--since` not
      silently ignored.
- [ ] Step 4.4: Wire `newMinerCmd(application)` into `cli/root.go`; mention `miner` in
      `cli/quickstart.go` (and the top-level reflect quickstart text).
  - verify: `auto reflect miner --help` lists all subcommands; `quickstart` output contains
      `miner`. `cd auto-reflect && go build ./...`.
- [ ] Step 4.5: `gofmt -l` + `go vet`; `cd auto-reflect && go test ./...`.
  - verify: all green.
- [ ] Step 4.6: Commit: `feat(023): phase 4 — miner CLI, source-state contract, wiring`

### Phase 5: `reflect stats` backlog + docs + e2e dogfood
- [ ] Step 5.1: Add `PendingToMine *int` to `StatsReport` (`loop/service.go`) computed via
      `miner.PendingCount(repoRoot, etlRoot, scope)` which reads the parquet session universe **and**
      folds events (never-mined sessions emit no events, so the fold alone can't see them;
      `loop`→`miner`→`etlread`, one-way). When the ETL source is missing/empty the field is `null`
      and `reflect stats` still succeeds (preserves today's no-ETL behaviour — it never errors).
      Print it in `cli/stats.go` json + text alongside `unconsolidated_observations` (AC-6).

<!-- RESOLVED(P2): "computed from the same fold" is not achievable — see solution.md §7 thread
REVIEW: `pending_to_mine` needs the parquet session universe (never-mined sessions emit no events), so `Stats()` must read ETL parquet via `etlread`, not just fold `events.ReadAll`. This step also needs to say how `reflect stats` behaves when the ETL source is missing/empty (it currently runs fine with no ETL output). Full detail in the solution.md §7 comment.
AUTHOR: Step rewritten — `pending_to_mine` is now `*int` computed via `miner.PendingCount` (parquet read + fold, layering called out), and is `null` (stats still succeeds, never errors) when the ETL source is absent. Mirrors the resolved solution.md §7 thread.
-->

  - verify: `service_test.go` / stats integration — `pending_to_mine` correct against a fixture
      event log + parquet; **`null` (and stats exits 0) when no ETL output exists**.
- [ ] Step 5.2: `auto-reflect/cmd/autoreflect/e2e_miner_test.go` — build a fixture `~/.auto/etl/output`
      (reuse auto-search `testutil` fixture writer via the shared model), run `next` (assert ranked,
      content-free, deduped, `fetch_cmd`), `ack --status mined --observations 2`, re-run `next`
      (session excluded), `status` (counts + `coverage_pct`), `describe <id>` (no write), and the
      source-missing/empty cases.
  - verify: `cd auto-reflect && go test ./cmd/autoreflect/ -run Miner` passes (AC-1..AC-11 end-to-end).
- [ ] Step 5.3: Real-binary dogfood: `make build`; against this repo's own
      `~/.auto/etl/output`, run `./bin/auto reflect miner next --limit 5`, `ack` one, `status`,
      `describe <id>`; confirm a `session_mined` line landed in `.auto/reflect/events/` and the
      acked session drops from `next`.
  - verify: capture before/after `next` — the acked ID disappears; the event file has one new line.
- [ ] Step 5.4: Update `auto-etl/docs/reference/normalized-schema.md` with the `auto-shared/model`
      relocation note; run `auto doc fix` (informational) and address any flagged `[autodoc()]` link.
  - verify: `auto doc stale` reports no new stale entries for touched docs.
- [ ] Step 5.5: Repo-wide gates: `make check && make build && make test`.
  - verify: all pass.
- [ ] Step 5.6: Commit: `feat(023): phase 5 — reflect stats backlog, docs, e2e dogfood`

## Success Criteria
- [ ] `cd auto-shared && go test ./...` passes; `auto-etl` + `auto-search` suites stay green after the schema move (Phase 1)
- [ ] `cd auto-reflect && go test ./...` passes: etlread projection + ResolveSource (AC-8), event validation (AC-7), fold terminal-vs-retryable + version bump (AC-2/AC-4/AC-9), score determinism + floor (AC-5), Next scope/dedupe/markers (AC-1/1b/1c/10), describe/signals read-only (AC-11), CLI source-state + ack-status (AC-3/AC-8)
- [ ] `auto reflect miner next` returns ranked, content-free JSON with `fetch_cmd`, `prior_ack`, `signals`; `status` reports counts + `coverage_pct` (null over empty source); `reflect stats` shows `pending_to_mine` (AC-6)
- [ ] Re-mine: bumping `miner.Version` re-opens previously-acked sessions with no migration (AC-4)
- [ ] `make check && make build && make test` all green
- [ ] Manual dogfood on this repo's `~/.auto/etl/output`: `next → ack → next` excludes the acked session; one `session_mined` line appended to `.auto/reflect/events/`
- [ ] One schema source of truth: no `ParquetSessionRow`/`ParquetMessageRow` remain in auto-search; auto-etl + auto-search import `auto-shared/model`

## Open Questions
- (none — all requirements Open Questions resolved, incl. Q4 → option B; sim findings folded into AC-8–AC-11)
