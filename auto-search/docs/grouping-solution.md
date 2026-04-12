---
hash: "00000000"
id: "a52d8c0b"
summary: "Technical solution sketch for autosearch grouping/statistics, including architecture, query strategy, and expected end-state UX."
title: "AutoSearch Grouping Solution Sketch"
---

# AutoSearch Grouping Solution Sketch

This document describes how to implement `autosearch stats` to satisfy [grouping-requirements.md](./grouping-requirements.md), and what the shipped result should look like for users.

## 1. End-State UX (What Users Get)

### 1.1 Minimal required invocation

```bash
autosearch stats --scope messages --group-by session_id
```

### 1.2 Full invocation with filters

```bash
autosearch stats --scope messages --group-by session_id --query '"Exit code 1"' --role tool --since 14d --limit 10
```

### 1.3 Output shape

- JSON-only response with `_meta` and `buckets`
- uncapped match/bucket metadata
- deterministic bucket ranking
- deterministic drill-down sample IDs

Example:

```json
{
  "_meta": {
    "request_id": "",
    "scope": "messages",
    "query": "\"Exit code 1\"",
    "group_by": "session_id",
    "measure": "count",
    "elapsed_ms": 18,
    "total_matches": 209,
    "total_buckets_unfiltered": 55,
    "total_buckets": 55,
    "returned_buckets": 10,
    "page_size": 10,
    "offset": 0,
    "has_more": true,
    "next_offset": 10,
    "is_capped": false
  },
  "buckets": [
    {
      "key": "0e160d60-06ee-4e5b-8bff-fccdc0138c9a",
      "count": 31,
      "distinct_sessions": 1,
      "distinct_messages": 31,
      "sample_message_id": "0e160d60-06ee-4e5b-8bff-fccdc0138c9a-224",
      "sample_session_id": "0e160d60-06ee-4e5b-8bff-fccdc0138c9a",
      "sample_snippet": "Exit code 1 ... TestSessionSearchRoleFilter"
    }
  ]
}
```

## 2. Implementation Architecture

### 2.1 CLI layer

Add `internal/cli/stats.go` and register in `internal/cli/root.go`.

Responsibilities:
- parse and validate flags
- enforce mutual exclusivity (`--cwd` vs `--remote`)
- open index DB via existing config/indexdb helpers
- call stats service
- emit strict JSON on stdout

### 2.2 Service layer

Add `internal/stats/` package with:
- `types.go`: request/response structs
- `validate.go`: scope/key/measure validation
- `normalize.go`: key normalization helpers (`bash_command`, day/week, empty bucket)
- `query_messages.go`: SQL builder/executor for message scope
- `query_sessions.go`: SQL builder/executor for session scope
- `samples.go`: deterministic sample-row selection
- `stats.go`: orchestration entry point

### 2.3 Shared filter/query boundaries

Reuse stable shared components where possible:
- date filter parsing (`ParseTimeFilter`)
- query parser/compiler (`internal/query`)
- common filter semantics (`cwd`, `remote`, `role`, `field`)

Implementation constraint:
- avoid tight coupling to `internal/search` command internals
- if needed, extract shared filter builders into a neutral internal package (for example `internal/filter`) and consume from both `search` and `stats`

### 2.4 Test architecture

Test layout should be explicit at design level:
- `internal/stats/*_test.go`: unit tests (`normalize`, `validate`, ordering, metadata math)
- `internal/stats/stats_integration_test.go`: SQL/query integration tests by scope
- `internal/cli/cli_integration_test.go`: CLI surface and error contract tests
- fixture setup helpers under `internal/testutil/` dedicated to stats edge cases

## 3. Query Execution Strategy

### 3.1 Core execution model

For both scopes:
- build a `matched` set representing filtered rows
- compute `total_matches` from `matched`
- aggregate into `grouped_unfiltered`
- compute `total_buckets_unfiltered`
- apply `--min-count` into `grouped_filtered`
- compute `total_buckets`
- apply deterministic sort and pagination
- fetch sample rows for returned buckets only

### 3.2 Query-present vs query-absent paths

When `--query` is present:
- use FTS tables and carry relevance score (`bm25`) for sample ranking

When `--query` is absent:
- skip FTS joins entirely
- read directly from base `messages` or `sessions` tables with non-FTS filters

This keeps no-query aggregation fast and avoids unnecessary FTS work.

### 3.3 Message scope CTE sketch

```sql
WITH matched AS (
  SELECT
    m.message_id,
    m.session_id,
    m.role,
    m.timestamp,
    m.tool_name,
    m.tool_file_path,
    m.bash_command,
    m.workspace,
    m.git_remote,
    m.model,
    -- only present in query path:
    bm25(messages_fts) AS score
  FROM ...
  WHERE ... -- filters
), grouped_unfiltered AS (
  SELECT
    <bucket_expr> AS bucket_key,
    COUNT(*) AS count,
    COUNT(DISTINCT session_id) AS distinct_sessions,
    COUNT(DISTINCT message_id) AS distinct_messages
  FROM matched
  GROUP BY bucket_key
), grouped_filtered AS (
  SELECT *
  FROM grouped_unfiltered
  WHERE <selected_measure> >= :min_count
), page AS (
  SELECT *
  FROM grouped_filtered
  ORDER BY <selected_measure> DESC, bucket_key ASC
  LIMIT :limit OFFSET :offset
)
SELECT ... FROM page;
```

Note:
- do not pull `content_truncated` through `matched` unless required for sample snippet extraction
- fetch snippet payload only for the selected sample rows to reduce scan payload

### 3.4 Session scope distinct-message strategy

Avoid a full `matched_sessions x messages` expansion across all buckets.

Plan:
1. compute grouped counts and pagination from `matched_sessions` only
2. for paged bucket keys, run a second query to compute `distinct_messages`
3. for the same paged bucket keys, fetch deterministic sample rows

This keeps execution memory-bounded and avoids join blow-ups on large datasets.

## 4. Key Derivation and Normalization

### 4.1 Empty values

Use canonical empty bucket value:
- `COALESCE(NULLIF(TRIM(expr), ''), '(none)')`

### 4.2 Time buckets

- `day`: UTC date from timestamp
- `week`: UTC ISO week representation
- sparse output only (no synthetic zero-count buckets)

### 4.3 `bash_command` normalization

Algorithm:
- trim and collapse whitespace
- unwrap `bash -lc` or `sh -lc` wrappers
- remove leading `KEY=VALUE` env assignments
- strip common prefixes (`env`, `sudo`) when they precede a command
- split chains on `&&`, `||`, and `;`, then choose the last non-empty segment
- pipe-aware normalization is deferred in Phase 1 (do not split on `|`)
- derive command family from first two tokens of the selected segment
- if normalization yields empty text, bucket as `(none)`

Examples:
- `cd auto-etl && go build -o ../bin/autoetl .` -> `go build`
- `FOO=1 BAR=2 go test ./...` -> `go test`
- `sudo env GOFLAGS=-count=1 go test ./...` -> `go test`
- `bash -lc 'go vet ./...'` -> `go vet`

## 5. Deterministic Sample Selection

When `--query` exists:
- choose sample row by best relevance (most negative `bm25()` value in SQLite FTS5)
- tie-break by newest timestamp, then stable ID

When `--query` is absent:
- choose newest timestamp, then stable ID
- omit `sample_snippet` by default

This keeps bucket drill-down stable across runs.

## 6. Metadata Semantics

- `total_matches`: number of rows in `matched`
- `total_buckets_unfiltered`: distinct bucket count before `--min-count`
- `total_buckets`: distinct bucket count after `--min-count` and before pagination
- `page_size`: requested `--limit` value
- `returned_buckets`: actual rows returned (`min(page_size, remaining_buckets)`)
- `has_more`: whether `offset + returned_buckets < total_buckets`
- `next_offset`: `offset + returned_buckets` when `has_more=true`; otherwise omitted or `null`
- `is_capped`: `false` unless explicit cap behavior is introduced

## 7. Expected File Changes

Planned code additions and updates:
- `internal/cli/stats.go` (new command)
- `internal/cli/root.go` (register command)
- `internal/cli/quickstart.go` (search vs stats guidance)
- `internal/cli/docs.go` (include stats in full docs output)
- `internal/stats/*.go` (new package)
- `internal/cli/cli_integration_test.go` (stats CLI tests)
- `internal/stats/stats_integration_test.go` (scope-level query tests)
- `internal/stats/*_test.go` (unit tests)
- `internal/testutil/stats_fixtures.go` (required dedicated fixture helpers for stats edge cases)
- `auto-search/CLAUDE.md` and related docs index references (ensure new docs are discoverable)

## 8. Performance and Reliability Notes

Performance targets for Phase 1:
- P50 under 500ms on representative queries
- P95 under 2s on approximately 1M messages

Execution and plan constraints:
- keep aggregation in SQL; avoid Go-side full bucket materialization
- use two-pass session strategy for `distinct_messages` to avoid cardinality explosions
- review `EXPLAIN QUERY PLAN` on hot queries to verify predicate pushdown and index usage
- validate existing indexes and add missing indexes only where needed for group/filter paths
- preserve deterministic ordering for all ties
- keep stdout strictly JSON and send diagnostics/errors to stderr

Benchmark and regression expectations:
- benchmark per cardinality tier (low/medium/high distinct bucket counts)
- run existing `search` integration tests unchanged to prove no behavior regressions

## 9. Rollout Shape

Phase 1 implementation order:
1. `session_id`
2. `bash_command`
3. `tool_file_path`
4. `day`
5. `tool_name`
6. `workspace`
7. remaining required Phase 1 keys for both scopes

Phase 1 deliverables:
- `stats` command and required keys/measures
- deterministic metadata and sample IDs
- quickstart/docs update clarifying:
  - `search` returns example matches
  - `stats` returns grouped pattern/count summaries

Phase 2:
- derived keys (`subproject`, `tool_file_dir`, `exit_code`, `duration_bucket`)
- richer drill-down options

Phase 3:
- optional text renderer
- reflection preset templates

## 10. Final User-Visible Result

After shipping, a reflection workflow should become:

1. Run one `stats` query to rank hotspots.
2. Pick top bucket via sample IDs.
3. Drill into context with `message get` or `session get`.

This removes manual cross-query aggregation and makes prioritization reproducible.

## 11. End-State Walkthrough (Concrete)

The end result should feel like a tight loop:

1. Discover pattern hotspots quickly.

```bash
autosearch stats --scope messages --query '"Exit code 1" OR "FAIL"' --group-by session_id --role tool --since 14d --limit 10
```

2. Pivot to root-cause class using the same filter set.

```bash
autosearch stats --scope messages --query '"Exit code 1" OR "FAIL"' --group-by bash_command --role tool --since 14d --limit 10
```

3. Drill into the top session from `sample_session_id`.

```bash
autosearch session get --id <sample_session_id>
```

4. Drill into the exact message from `sample_message_id`.

```bash
autosearch message get --id <sample_message_id>
```

Expected operator experience:
- one stats command to prioritize where to look
- one pivot stats command to classify what is failing
- one or two `get` calls to inspect concrete transcript evidence
- no manual shell pipelines needed for basic ranking workflows

## 12. Definition of Done (Shipped Experience)

The feature is complete when all items below are true:
- `autosearch stats` exists with required flags and help text.
- command validates `scope`, `group-by`, and `measure` with explicit errors.
- scope/key mismatches fail fast and include remediation hints.
- output always follows `{ "_meta": ..., "buckets": [...] }` JSON contract.
- metadata fields are correct and uncapped (`total_matches`, `total_buckets_unfiltered`, `total_buckets`).
- bucket ordering and sample IDs are deterministic across repeated runs on the same index.
- `--min-count`, `--limit`, and `--offset` semantics match requirements.
- message and session scopes both support required Phase 1 keys.
- unit tests cover normalization edge cases, date windows, and flag conflicts.
- integration and e2e tests cover drill-down from stats bucket to `session get` and `message get`.
- existing `search` command tests pass unchanged (no regression in current behavior).
- benchmark checks pass against Phase 1 latency targets.
- quickstart/docs output includes clear search-vs-stats usage guidance.
