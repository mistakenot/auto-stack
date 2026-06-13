---
hash: "880e3625"
id: "8ef298b4"
read_when: "implementing the auto-reflect miner queue or the shared parquet schema extraction"
summary: "Design for auto-reflect miner queue: shared-model extraction to auto-shared/model, session_mined event type, priority scoring from message signals, and miner next/ack/status/describe CLI."
title: "Solution: Task 023 — Reflect Miner Queue"
---

# Solution: Task 023 — reflect-miner-queue

## Approach

Add a `miner` command group to `auto reflect` that ranks coding-agent sessions to mine
and tracks coverage as append-only events. The work splits into two parts: a **shared-model
extraction** (so auto-reflect can read ETL parquet without duplicating schema), then the
**miner itself** (read → fold → score → emit).

1. **Extract the canonical parquet schema into `auto-shared/model`.** Move `AgentSession`,
   `AgentMessage`, `MessageRole` + role constants, the schema/truncation constants,
   `PartitionKey`, and `WeekPartition`/`MonthPartition` out of `auto-etl/internal/model`
   (currently unimportable across modules) into a new `auto-shared/model` package. Re-point
   auto-etl to import it, and **delete** auto-search's hand-rolled `ParquetSessionRow`/
   `ParquetMessageRow` mirror (they are byte-for-byte identical — verified field-by-field) in
   favour of the shared types. Parquet tags are preserved exactly, so existing partitions stay
   readable and the ETL writer is unchanged in behaviour.

2. **Add a `session_mined` event type** to the existing reflect event log. New
   `SessionMinedPayload{ session_id, miner_version, status, observations, priority_score, signals }`;
   appended via the existing `events.AppendEvent`, validated alongside the other types. No new store
   (AC-7). The payload carries the **mining status** (`mined`/`empty`/`failed`/`skipped`, AC-9) and a
   **snapshot of `priority_score` + `signals`** computed at mining time (AC-10) — both already in hand
   when `ack` runs, so persisting them is free and makes the v(n-1)→v(n) score diff reconstructable.

3. **Bake a miner version constant** into auto-reflect (`miner.Version = 1`). Re-mining is driven
   entirely by bumping this in code (AC-4) — no migration, no flag.

4. **Add a parquet read layer** in `auto-reflect/internal/etlread` that discovers ETL output
   partitions (reusing the same `~/.auto/etl/output/{sessions,messages}/…` layout auto-search
   uses) and reads them with `parquet.NewGenericReader`. Sessions are read via the shared
   `model.AgentSession`; **messages are read via a narrow local projection struct** (`MsgSignalRow`
   = `session_id`, `role`, `content_truncated`, `tool_name`, `is_error`, `workspace`, `git_remote`,
   `is_subagent`, `parent_session_id`) so the hot path never pulls the multi-hundred-KB `content`
   column. (`role` is needed for the per-user-message normalization in AC-5; `git_remote` for scope.)

5. **Build the miner service** (`auto-reflect/internal/miner`):
   - **Universe**: top-level sessions (`is_subagent = false`), scoped by **normalized `git_remote`**
     matching the current repo's normalized remote (worktree-stable — one queue across all worktrees
     of the repo), with a `Workspace` path-prefix fallback when the repo has no remote; `--all`
     widens to every remote/workspace (AC-1b, AC-1c). Remote resolution + normalization reuse
     `DetectRepoLenient` + `auto-shared/git`.
   - **Coverage fold**: fold `session_mined` events → `map[sessionID]minedState{maxTerminalVersion,
     lastStatus, lastObservations, lastPriorityScore, lastSignals, lastMinedAt, ackCount}`. A session
     is **terminal at version V** only if it has a `mined`/`empty`/`skipped` ack at V; `failed` acks
     are recorded for history but **never make a session terminal** (it stays retryable, AC-9).
     Exclude from `next` any session whose `maxTerminalVersion >= miner.Version`; include never-acked
     (absent), `failed`-only, and stale-version sessions (AC-2, AC-4, AC-9).
   - **Signals + score**: for the surviving candidates, scan their messages (projection read,
     grouped by `session_id`) and compute deterministic signals — correction-language density,
     `tool_use_error`/`is_error` count, build/test-failure markers, `AskUserQuestion` frequency —
     each normalized by message count, combined into a fixed-weight `priority_score` (AC-5).
   - **Subagents**: with `--include-subagents`, attach each top-level item's child session IDs
     (sessions where `parent_session_id == item.id`) so the skill mines them as one unit; `ack`
     is recorded at the parent and covers them (AC-1b).
   - **Re-mine markers** (AC-10): each `WorkItem` carries `prior_ack` (`{version, status,
     observations, ts}` from the fold, or `null`) and a `remined` bool, so a version-bump re-mine is
     distinguishable from a first mine. Results are **deduped by `session_id`** before emission
     (friction-log fix), and each item carries a `fetch_cmd` hint
     (`auto search session get <id>`) so the skill and humans know the content bridge. **Note:**
     `fetch_cmd` resolves through the auto-search FTS index (`auto search index`), a separate build
     step over the same parquet — so the downstream mining skill (2.1) must ensure the index is
     fresh before fetching; the miner itself reads parquet directly and has no such dependency.

<!-- RESOLVED(P3): `fetch_cmd` bridge requires a built auto-search index, not just ETL parquet
REVIEW: The miner reads ETL parquet directly, but the suggested `fetch_cmd` (`auto search session get <id>`) resolves content through the auto-search FTS index (`session.go:191` → `config.IndexPath(...)`), which is a SEPARATE build step (`auto search index`) over the same parquet. So a session can be queue-able by the miner yet un-fetchable if the search index hasn't been built/refreshed. Not a blocker for this CLI surface, but the downstream mining skill (2.1) inherits this prerequisite — worth one sentence noting the index dependency so the skill doesn't emit `fetch_cmd`s that 404.
AUTHOR: Added a sentence to the re-mine-markers bullet noting `fetch_cmd` resolves via the auto-search index (a separate `auto search index` build), so the mining skill (2.1) must refresh the index before fetching; the miner reads parquet directly and carries no such dependency. The WorkItem.FetchCmd comment also flags this. (The skill prerequisite itself is out of scope — epic 2.1.)
-->


6. **Wire the subcommands** under `newMinerCmd`: `next` (ranked, content-free JSON), `ack`
   (append coverage event), `status` (pending vs mined, coverage %, signal distribution),
   `describe <id>` / `signals <id...>` (read-only signals + ack history, AC-11), and `quickstart`
   (mention `miner` in the top-level reflect quickstart too — friction-log fix). Default output JSON
   to stdout, diagnostics to stderr, `--text` for human mode (house convention).
   - **`ack`** takes `--status mined|empty|failed|skipped` (default `mined`) and `--observations N`,
     and persists the score+signals snapshot from a fresh scoring pass on that session (AC-9, AC-10).
   - **Source-state contract** (AC-8): `next`/`status` resolve the ETL output root first. Missing or
     unreadable → a stderr error with hint `run auto etl run` and a **non-zero exit** (not hidden
     behind a flag). When `total_sessions == 0`, `status.coverage_pct` is JSON `null`, never `100`/`0`.
     An empty `next` (exit 0, `[]`) writes a stderr hint distinguishing *drained for this workspace,
     try --all* from *source empty*. Documented exit contract: `0`+items = work, `0`+empty = drained,
     non-zero = source error.
   - **`describe`/`signals`** (AC-11) compute signals for the requested IDs **regardless of ack-state
     or subagent-status** and never mutate state — the read-only escape hatch from the queue filter,
     following the repo's `describe`/`get` resource convention.
   - **`status`** honors `--all` symmetrically with `next` (friction-log fix); `--since` is honored
     for the windowed counts (or rejected with a clear error — never silently ignored).

7. **Surface backlog in `reflect stats`.** Add a `pending_to_mine *int` field to `StatsReport`. The
   count (in-scope top-level sessions not terminal at the current version) needs the **parquet
   session universe** — a never-mined session emits no events — so it is computed by calling
   `miner.PendingCount(repoRoot, etlRoot, scope) (count int, src SourceState, err error)`, keeping
   the parquet read inside `miner` (`loop`→`miner`→`etlread`, one-way). **Graceful degradation**:
   when the ETL source is missing/empty (`src != SourceOK`), `pending_to_mine` is JSON `null` and
   `reflect stats` still succeeds — preserving today's behaviour where `stats` runs with no ETL
   output at all. Unlike `miner next`/`status`, `reflect stats` never errors on an absent source
   (it is a general-purpose command, not a miner entrypoint).

<!-- RESOLVED(P2): `pending_to_mine` cannot be derived from the event-log fold alone
REVIEW: `pending_to_mine` is "in-scope top-level sessions not acked at the current version". The denominator (the universe of sessions) lives in the ETL parquet, NOT the event log — a never-mined session has zero events, so `Stats()`'s current pure fold over `events.ReadAll` (loop/service.go:216-230) literally cannot see it. So `loop.Service` must gain an `etlread`/parquet read dependency (and a way to resolve the ETL root + current workspace scope). Two consequences to spec: (1) `loop/service` taking a parquet dependency is a notable layering change worth calling out (plan Step 5.1 says "computed from the same fold", which is not achievable); (2) AC-8's source-state contract only covers `miner next`/`status` — decide what `reflect stats` does when the ETL source is missing/empty (omit the field? `null`? error?). Today `reflect stats` works with no ETL output at all; this change must not break that.
AUTHOR: Correct — fixed both consequences. (1) `pending_to_mine` is now computed via `miner.PendingCount(...)` (parquet dependency lives in `miner`, `loop` imports `miner`; one-way layering called out explicitly). plan Step 5.1 wording corrected from "the same fold" to "via miner.PendingCount which reads parquet + folds events". (2) The field is now `*int` and is `null` when the ETL source is missing/empty, and `reflect stats` NEVER errors on an absent source — preserving the existing no-ETL behaviour. AC-8's hard-error contract stays scoped to `miner next`/`status` only.
-->


## Files

```
# --- Part 1: shared-model extraction ---
+ auto-shared/model/schema.go              # canonical AgentSession, AgentMessage, MessageRole + role consts,
                                           #   schema/truncate consts, PartitionKey, WeekPartition, MonthPartition
~ auto-shared/go.mod                       # add require github.com/parquet-go/parquet-go (already transitively present)
~ auto-etl/internal/model/model.go         # remove moved decls; keep TransformedRows (now imports auto-shared/model)
~ auto-etl/internal/writer/writer.go       # import auto-shared/model instead of internal/model for session/message
~ auto-etl/internal/transform/transform.go # same import re-point
~ auto-etl/internal/transform/transform_test.go
~ auto-etl/e2e_test.go                      # readAllParquet[model.AgentMessage] → shared model
- auto-search/internal/model/parquet.go     # delete ParquetSessionRow + ParquetMessageRow (now redundant)
~ auto-search/internal/etlscan/parquet_sessions.go   # NewGenericReader[sharedmodel.AgentSession]
~ auto-search/internal/etlscan/parquet_messages.go   # NewGenericReader[sharedmodel.AgentMessage]
~ auto-search/internal/indexdb/indexer.go            # *sharedmodel.AgentSession / *sharedmodel.AgentMessage
~ auto-search/internal/testutil/fixtures.go          # construct sharedmodel.* rows
~ auto-search/internal/testutil/stats_fixtures.go    # construct sharedmodel.* rows

# --- Part 2: the miner ---
~ auto-reflect/go.mod                       # add require github.com/parquet-go/parquet-go + auto-shared already replaced
+ auto-reflect/internal/etlread/read.go     # discover partitions; ReadSessions (shared model) + ReadMessageSignals (projection)
+ auto-reflect/internal/etlread/read_test.go
~ auto-reflect/internal/events/model.go     # + TypeSessionMined; AckStatus; Signals; SessionMinedPayload; + Validate case (payload types live in events — no events→miner cycle)
+ auto-reflect/internal/miner/miner.go      # Version const; coverage fold (terminal vs retryable); queue assembly; GitRemote scope; ResolveSource (AC-8); PendingCount; Describe/SignalsFor (AC-11)
+ auto-reflect/internal/miner/score.go      # deterministic signal extraction → events.Signals; Score(); length floor + safeDiv
+ auto-reflect/internal/miner/miner_test.go
+ auto-reflect/internal/miner/score_test.go
+ auto-reflect/internal/cli/miner.go        # newMinerCmd + next/ack/status/describe/signals/quickstart; source-state errors + exit codes
+ auto-reflect/internal/cli/miner_test.go
~ auto-reflect/internal/cli/root.go         # AddCommand(newMinerCmd(application))
~ auto-reflect/internal/cli/quickstart.go   # mention `miner` in the existing reflect quickstart text (friction-log fix)
~ auto-reflect/internal/loop/service.go     # StatsReport += PendingToMine *int (null when no ETL); calls miner.PendingCount (loop→miner→etlread)
~ auto-reflect/internal/cli/stats.go        # print pending_to_mine in json + text
+ auto-reflect/cmd/autoreflect/e2e_miner_test.go  # miner next/ack/status/describe + source-state against a fixture etl-output
```

### Key type outlines

```go
// auto-shared/model/schema.go  (moved verbatim — tags unchanged)
type AgentSession struct { /* 28 fields, parquet tags identical to today */ }
type AgentMessage struct { /* 40 fields, parquet tags identical to today */ }

<!-- RESOLVED(P3): AgentMessage is 40 fields, not 34
REVIEW: `auto-etl/internal/model/model.go:28-102` defines AgentMessage with 40 parquet-tagged fields (verified: 28 session + 40 message = 68, which matches the auto-search mirror's total parquet-tag count). Both this outline and context.md:40 say "34 fields". Cosmetic, but worth correcting so the "moved verbatim" claim is auditable.
AUTHOR: Verified (`sed -n '28,102p' model.go | grep -c parquet:` → 40; session → 28). Corrected to "40 fields" here and in context.md:40.
-->


// auto-reflect/internal/etlread/read.go
type MsgSignalRow struct {                       // narrow projection — never reads `content`
    SessionID        string `parquet:"session_id,dict"`
    Role             string `parquet:"role,dict"`         // user|assistant|tool|… — needed for "per user msg" normalization (AC-5)
    ContentTruncated string `parquet:"content_truncated"`
    ToolName         string `parquet:"tool_name,dict"`
    IsError          bool   `parquet:"is_error"`
    Workspace        string `parquet:"workspace,dict"`
    GitRemote        string `parquet:"git_remote,dict"`   // for normalized-remote scope (AC-1c)
    IsSubagent       bool   `parquet:"is_subagent"`
    ParentSessionID  string `parquet:"parent_session_id,dict"`
}

<!-- RESOLVED(P2): MsgSignalRow projection is missing the `role` column
REVIEW: AC-5 defines `correction-language density` "per 100 user msgs" and the `Signals` struct's `CorrectionDensity` is "corrections per 100 user msgs". Distinguishing user messages from assistant/tool rows requires the `role` column (`AgentMessage.Role`, `parquet:"role,dict"` at model.go:34) — but this projection omits it. Without `role`, you cannot count "user msgs" as the normalization denominator, nor reliably scope correction-language to user turns. Add `Role string \`parquet:"role,dict"\`` to MsgSignalRow. (The column is dict-encoded and tiny, so it doesn't undermine the content-avoidance goal.)
AUTHOR: Added `Role string \`parquet:"role,dict"\`` (verified at model.go:34). Also added `GitRemote` to the projection since AC-1c scope now keys on normalized remote. Both are dict-encoded and tiny, so the content-avoidance goal holds.
-->

func ReadSessions(root string, f Filter) ([]model.AgentSession, error)
func ReadMessageSignals(root string, f Filter) ([]MsgSignalRow, error)  // partition-pruned by --since

// auto-reflect/internal/events/model.go — ALL payload types live in `events` (one-way: consumers→events;
//   events imports only auto-shared/config + internal/{gitutil,store}, NEVER miner). So the AckStatus +
//   Signals payload sub-types live here too, and miner references events.Signals / events.AckStatus.
const TypeSessionMined = "session_mined"
type AckStatus string // "mined" | "empty" | "failed" | "skipped"  (AC-9)
type Signals struct { // labelled, unitful — persisted on the event (AC-10) AND returned by the scorer
    MessageCount       int     `json:"message_count"`        // raw, pre-normalization
    CorrectionDensity  float64 `json:"correction_density"`   // corrections per 100 user msgs
    ToolErrorCount     int     `json:"tool_error_count"`     // is_error=true rows
    FailureMarkerCount int     `json:"failure_marker_count"` // build/test-failure markers
    AskUserCount       int     `json:"ask_user_question_count"`
    LengthFloorApplied bool    `json:"length_floor_applied"` // true when MessageCount < floor (caps short-session inflation)
}
type SessionMinedPayload struct {
    SessionID     string    `json:"session_id"`    // the MINED session (envelope.SessionID is the emitter's own)
    MinerVersion  int       `json:"miner_version"`
    Status        AckStatus `json:"status"`        // mined|empty|failed|skipped; failed is retryable
    Observations  int       `json:"observations"`  // 0 == mined-but-empty
    PriorityScore float64   `json:"priority_score"`// snapshot at mining time (AC-10)
    Signals       Signals   `json:"signals"`       // snapshot at mining time (AC-10)
}

<!-- RESOLVED(P1): import cycle between `events` and `miner`
REVIEW: This places `SessionMinedPayload` (with field `Signals Signals`) and `AckStatus` in `auto-reflect/internal/events/model.go`, but the `Signals` type is defined in `auto-reflect/internal/miner/score.go` (below) and `minedState` in miner.go references `events.AckStatus`. So `events` would import `miner` (for `Signals`) while `miner` imports `events` (for `AckStatus`, `AppendEvent`, `Event`, `FoldCoverage`) — a compile-blocking cycle. Verified the constraint: ALL existing payload types live in `events/model.go` and `events` imports ONLY `auto-shared/config` (no internal packages); consumers like `rules` import `events`, never the reverse (`rules/projection.go:8`). The dependency direction is strictly consumers→events. Fix: define `Signals` (and `AckStatus`) IN `events` (alongside the other payloads), and have `miner` reference `events.Signals` — OR move `SessionMinedPayload` entirely into `miner` (events.Validate only checks the envelope, never decodes payloads, so it does not need the concrete payload type). The current split (Signals in miner, SessionMinedPayload in events) cannot compile. This also affects the phase order: Phase 2 (Step 2.3) adds `SessionMinedPayload` to events before Phase 3 (Step 3.1) creates `Signals` in miner.
AUTHOR: Confirmed the cycle is real (verified `events` imports only auto-shared/config + internal/{gitutil,store}, never miner). Took the first option: `AckStatus` AND `Signals` now live in `events/model.go` alongside `SessionMinedPayload` and the other payloads — the existing convention (all payload types in events). `miner` references `events.Signals`/`events.AckStatus` (one-way miner→events, no cycle). `score.go` computes and returns `events.Signals` (no `Signals` type in miner). Phase order fixed: Phase 2 Step 2.3 now defines AckStatus + Signals + SessionMinedPayload together in events; Phase 3 score.go produces `events.Signals`. Type outlines below updated to `events.Signals` throughout.
-->


// auto-reflect/internal/miner/score.go — computes events.Signals (no miner-local Signals type → no cycle)
func Score(sig events.Signals) float64          // deterministic fixed-weight combine; length-floor + safeDiv guards

// auto-reflect/internal/miner/miner.go
const Version = 1                                // bump to re-open the corpus (AC-4)
type minedState struct {
    MaxTerminalVersion int                       // highest version with a mined|empty|skipped ack (failed excluded)
    LastStatus         events.AckStatus
    LastObservations   int
    LastPriorityScore  float64
    LastSignals        events.Signals
    LastMinedAt        time.Time
    AckCount           int
}
func FoldCoverage(evs []events.Event) map[string]minedState
type PriorAck struct {
    Version      int              `json:"version"`
    Status       events.AckStatus `json:"status"`
    Observations int              `json:"observations"`
    TS           string           `json:"ts"`
}
type WorkItem struct {
    SessionID     string         `json:"session_id"`
    CWD           string         `json:"cwd"`            // = session Workspace column (working-dir path)
    LastMessageAt int64          `json:"last_message_at"`
    MessageCount  int            `json:"message_count"`
    PriorityScore float64        `json:"priority_score"`
    Signals       events.Signals `json:"signals"`
    PriorAck      *PriorAck      `json:"prior_ack"`           // null on first mine (AC-10)
    Remined       bool           `json:"remined"`             // true when PriorAck != nil (AC-10)
    FetchCmd      string         `json:"fetch_cmd"`           // "auto search session get <id>" (AC-11) — needs a built auto-search index
    Subagents     []string       `json:"subagents,omitempty"` // only with --include-subagents
}
func Next(repoRoot, etlRoot string, opts NextOpts) ([]WorkItem, error) // dedupes by session_id

// read-only, decoupled from the queue filter (AC-11)
type SignalRow struct {
    SessionID  string         `json:"session_id"`
    Signals    events.Signals `json:"signals"`
    AckHistory []PriorAck     `json:"ack_history"` // full history, not just latest
}
func Describe(repoRoot, etlRoot, sessionID string) (SignalRow, error)   // one session, regardless of state
func SignalsFor(repoRoot, etlRoot string, ids []string) ([]SignalRow, error)

// source-state resolution (AC-8) — distinguishes drained from missing/empty source
type SourceState int // SourceOK | SourceEmpty | SourceMissing
func ResolveSource(etlRoot string) (SourceState, error)
```

## Test Coverage

| AC    | Test Type   | File                                                  |
|-------|-------------|-------------------------------------------------------|
<!-- RESOLVED(P3): e2e test path inconsistent with plan.md
REVIEW: This table points e2e rows at `auto-reflect/e2e_test.go`, but no such file exists — auto-reflect's e2e tests live in `auto-reflect/cmd/autoreflect/` (e.g. `e2e_lifecycle_test.go`, `e2e_observation_test.go`), and plan.md Step 5.2 correctly uses `auto-reflect/cmd/autoreflect/e2e_miner_test.go`. Align the table with the real package path.
AUTHOR: Corrected both e2e rows (AC-1, AC-8) and the Files entry to `auto-reflect/cmd/autoreflect/e2e_miner_test.go`, matching plan.md Step 5.2 and the existing e2e package location.
-->

| AC-1  | e2e         | auto-reflect/cmd/autoreflect/e2e_miner_test.go (miner next JSON shape, no content) |
| AC-1b | unit        | auto-reflect/internal/miner/miner_test.go (top-level only; --include-subagents attaches children) |
| AC-1c | unit        | auto-reflect/internal/miner/miner_test.go (workspace scope vs --all) |
| AC-2  | unit        | auto-reflect/internal/miner/miner_test.go (exclude acked-at-current; include never/stale) |
| AC-3  | integration | auto-reflect/internal/cli/miner_test.go (ack appends one session_mined event; repeat appends a second) |
| AC-4  | unit        | auto-reflect/internal/miner/miner_test.go (version bump re-opens v1-acked sessions) |
| AC-5  | unit        | auto-reflect/internal/miner/score_test.go (deterministic score from fixed signal inputs; normalization) |
| AC-6  | integration | auto-reflect/internal/cli/miner_test.go (status counts) + stats_test (pending_to_mine line) |
| AC-7  | unit        | auto-reflect/internal/events/model.go validation test (session_mined accepted; folds from JSONL, no new store) |
| AC-8  | e2e         | auto-reflect/cmd/autoreflect/e2e_miner_test.go (missing source → stderr+exit≠0; empty source → coverage_pct null; empty next → drained-vs-empty hint; exit-code contract) |
| AC-9  | unit + integration | miner_test.go (failed stays retryable; mined/empty/skipped terminal; quality mean counts mined only) + cli/miner_test.go (ack --status round-trips on the event) |
| AC-10 | unit        | miner_test.go (score+signals persisted on ack event; prior_ack/remined set on a re-mined item, null on first mine) |
| AC-11 | integration | cli/miner_test.go (describe/signals return signals + ack history for an acked + a subagent session; no event written) |
| friction (dedupe / fetch_cmd / --since / labels / status --all) | unit + e2e | miner_test.go (dedupe by session_id; fetch_cmd present; labelled signal keys) + e2e (status honors --all; --since windowing or rejection) |
| schema| integration | auto-reflect/internal/etlread/read_test.go (read fixture parquet; projection excludes content); auto-search + auto-etl existing tests stay green |

## Out of Scope

- The mining skill itself (epic 2.1) — this task only builds the CLI surface it consumes.
- The consolidation step (2.2) and any change to `observation`/`consolidate`/`rule` commands.
- LLM-based or semantic prioritization — v1 scoring is deterministic keyword/heuristic only.
- Acting on signals beyond ranking (auto-skipping low-signal sessions) — `next` ranks; the skill decides.
- A concurrent multi-miner claim/lease protocol — single-consumer assumed for v1.
- **Outcome/ROI scoring** (mining→rules-shipped) — v1 scores friction, not payoff (sim wildcard).
- **Cross-repo rollup beyond `--all`** — `--group-by workspace` / per-workspace arrays / org totals
  (sim #5); `status --all` widens the universe but reports a single aggregate.
- **CSV/tabular output and atomic large-dump streaming** (sim wildcard) — JSON only in v1.
- **Technical boundary:** no change to parquet tags, the ETL writer, or the on-disk partition
  layout — the shared-model extraction is a pure relocation, asserted by the unchanged auto-etl
  and auto-search test suites.
- **Technical boundary:** `auto-etl/internal/model`'s git/github structs and `TransformedRows`
  stay put — only the agent session/message schema moves.

## Rejected Alternatives

- **Minimal projection structs in auto-reflect only** (no shared module): smallest change, but
  leaves three independent copies of the schema (auto-etl, auto-search, auto-reflect) to drift.
  Rejected in favour of one canonical home (chosen at solution stage).
- **Generic schema-less parquet reader** (`map[string]any`): no compile-time column safety and
  unlike anything else in the repo. Rejected.
- **Type-alias shim** (keep `auto-search/internal/model` re-exporting the shared types): lowest
  churn, but preserves a redundant indirection the task is meant to remove. Kept as a fallback if
  the full re-point proves to ripple further than expected.
- **Markdown coverage file** the skill reads/trusts: the status quo the task replaces — can't
  distinguish mined-but-empty from never-mined, not append-only, not queryable. Rejected per AC-7.
- **Scanning session `transcript_truncated`** instead of message `content_truncated` for signals:
  one row per session is convenient, but AC-5 specifies message-level `content_truncated`, and the
  projection read keeps the message scan cheap. Deferred as a possible future optimization, not v1.
