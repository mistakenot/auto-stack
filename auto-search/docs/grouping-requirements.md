---
hash: "00000000"
id: "7d9b2f41"
summary: "Requirements for autosearch grouping and aggregation functionality, including prioritized use cases and concrete CLI examples."
title: "AutoSearch Grouping Requirements"
---

# AutoSearch Grouping Requirements

This document defines a v2 `autosearch` capability for grouped and aggregated analysis over indexed session history.

It extends [requirements.md](./requirements.md); it does not replace `search`, `session get`, or `message get`.

## 1. Purpose

`autosearch search` answers "what matched?" by returning example hits.

`autosearch stats` should answer "what patterns dominate?" by returning grouped counts.

Primary outcomes:
- reliable frequency ranking
- structured rollups by indexed fields
- reflection workflows that can prioritize by impact before transcript deep-dives

Context: current search workflows often rely on page-limited hit views, so high-frequency and medium-frequency topics can appear similar without dedicated aggregate counts.

## 2. Scope

In scope:
- grouped aggregations over existing indexed SQLite data
- shared time/workspace/repo filters reused from `search`
- JSON-first output with machine-parseable metadata
- deterministic ranking and pagination over buckets

Out of scope (initial release):
- free-form SQL
- custom user-defined expressions
- cross-index joins
- semantic embeddings
- true co-occurrence matrices across multiple dimensions

## 3. CLI Surface

Add a dedicated command:

```bash
autosearch stats \
  --scope messages|sessions \
  --group-by <key> \
  [--query <fts_query>] \
  [--measure count|distinct_sessions|distinct_messages] \
  [--since <duration> | --after <iso> --before <iso>] \
  [--cwd <path> | --remote <git_remote>] \
  [--role <user|assistant|tool>] \
  [--field all|content|tool_input|tool_output] \
  [--min-count <n>] \
  [--limit <n>] [--offset <n>] \
  [--request-id <id>]
```

Design choices:
- Keep grouping out of `search` to preserve retrieval semantics.
- Keep one explicit aggregation command (`stats`) to avoid mode ambiguity.
- Keep JSON as default output.

## 4. Output Contract

Top-level JSON envelope:

```json
{
  "_meta": {
    "request_id": "",
    "scope": "messages",
    "query": "Exit code 1",
    "group_by": "bash_command",
    "measure": "count",
    "elapsed_ms": 12,
    "total_matches": 842,
    "total_buckets_unfiltered": 189,
    "total_buckets": 147,
    "returned_buckets": 20,
    "page_size": 20,
    "offset": 0,
    "has_more": true,
    "next_offset": 20,
    "is_capped": false
  },
  "buckets": [
    {
      "key": "go build",
      "count": 96,
      "distinct_sessions": 33,
      "distinct_messages": 96,
      "sample_message_id": "abc-123-45",
      "sample_session_id": "abc-123",
      "sample_snippet": "Exit code 1 ... build failed"
    }
  ]
}
```

Rules:
- `stdout` contains JSON only.
- Diagnostics/errors go to `stderr`.
- `total_matches` is uncapped and reflects filtered input rows before grouping.
- `total_buckets_unfiltered` is the number of unique groups before `--min-count`.
- `total_buckets` is the number of unique groups after `--min-count` and before pagination.
- `is_capped` is `false` unless explicit capping is introduced.
- `sample_snippet` is optional and should be short (target 100-160 chars) to prevent bloated responses.
- for `day`/`week` group keys, output is sparse by default (zero-count buckets are omitted).

Sample row selection must be deterministic:
- when `--query` is present, choose the highest-scoring row in the bucket; break ties by newest timestamp, then stable ID.
- when `--query` is absent, choose the newest row in the bucket; break ties by stable ID.
- when `--query` is absent, omit `sample_snippet` by default.

## 5. Group Keys

### 5.1 Message scope (Phase 1 required)
- `session_id`
- `role`
- `tool_name`
- `tool_file_path`
- `bash_command`
- `workspace`
- `git_remote`
- `model`
- `day` (derived from `timestamp`, UTC)
- `week` (derived from `timestamp`, ISO week)

### 5.2 Session scope (Phase 1 required)
- `session_id`
- `workspace`
- `git_remote`
- `model`
- `host_id`
- `agent`
- `is_subagent`
- `day` (derived from `first_message_at`, UTC)
- `week` (derived from `first_message_at`, ISO week)

### 5.3 Derived keys (Phase 2)
- `subproject` (derived from `tool_file_path` prefix, e.g. `auto-etl`, `auto-search`)
- `tool_file_dir` (parent directory of `tool_file_path`)
- `exit_code` (parsed from bash tool output where available)
- `duration_bucket` for sessions (`short|medium|long`, thresholded by message count or wall-clock duration)

### 5.4 Normalization rules

`bash_command` must use a normalized command-family key to avoid over-fragmentation:
- trim and collapse whitespace
- strip leading `KEY=VALUE` env assignments
- normalize common wrappers (`bash -lc`, `sh -lc`) to inner command
- split command chains on `&&`, `||`, and `;`, then normalize the last non-empty segment
- bucket by first two command tokens from that segment when present (e.g. `go build`, `go test`)
- if fewer than two tokens exist, use the available token(s)

If exact command grouping is needed later, add `bash_command_raw` as a distinct Phase 2 key.
Pipe-aware normalization is deferred.

Validation:
- unsupported keys fail fast with explicit allowed-values errors
- key validation is case-insensitive for input flag, canonicalized in output
- scope/key mismatch fails fast (example: `--scope sessions --group-by bash_command`).
- null/empty key values must be aggregated into a canonical `(none)` bucket so totals remain additive.

## 6. Measures

Required measures:
- `count`: rows in the bucket
- `distinct_sessions`: unique session IDs in the bucket
- `distinct_messages`: unique message IDs in the bucket (message scope), or unique messages linked to matched sessions (session scope)

Selection behavior:
- `--measure` controls primary sort metric (descending)
- all three counts are still returned for every bucket

Scope semantics:
- in message scope, `distinct_messages` counts matched message rows in the bucket.
- in session scope, `distinct_messages` counts all messages belonging to matched sessions in the bucket (session-volume metric).

## 7. Filters and Semantics

Shared with `search`:
- `--since`
- `--after` + `--before`
- `--cwd` and `--remote` (mutually exclusive)
- `--role` (message role)
- `--field`
- `--query` (FTS prefilter)

Semantics:
- if `--query` is omitted, aggregate over all filtered rows
- if `--query` is provided, aggregate over query+filter matched rows
- `--min-count` is applied after aggregation and before pagination.
- `--min-count` threshold is evaluated against the selected `--measure`.

## 8. Ranking and Pagination

- default sort: primary `--measure` descending, then key ascending for deterministic ties
- `--limit` and `--offset` paginate buckets (not raw matches)
- `has_more` and `next_offset` are based on bucket count

## 9. Reliability and Discoverability Requirements

Reliability:
- uncapped `total_matches` and `total_buckets`
- deterministic output ordering for same data and flags
- no mixed human text in JSON output mode
- hard errors include remediation hints

Discoverability:
- when `stats` ships, `autosearch quickstart` must include a stats section
- quickstart must explicitly explain: `search` = examples, `stats` = patterns/counts

Examples:
- invalid key: `invalid --group-by value "foo"; valid values: ...`
- conflicting filters: `--cwd and --remote are mutually exclusive`

## 10. Performance Targets

Initial targets on a developer index (~1M messages):
- P50 under 500ms for common stats queries
- P95 under 2s for common stats queries
- memory-bounded execution without full in-memory materialization for top-N buckets

Implementation note:
- memory-bounded applies to the Go layer.
- exact `total_buckets_unfiltered` and `total_buckets` may rely on SQL-side aggregation/count queries.

## 11. Real-World Use Cases From a Session Probe (2026-04-12)

### UC-1: Which sessions had the most build failures?

```bash
autosearch stats \
  --scope messages \
  --query '"Exit code 1" OR "cannot find main module" OR "declared and not used"' \
  --group-by session_id \
  --role tool \
  --cwd /home/vscode/src/auto-stack \
  --since 14d \
  --limit 10
```

### UC-2: What command families dominate failures?

```bash
autosearch stats \
  --scope messages \
  --query '"Exit code 1"' \
  --group-by bash_command \
  --role tool \
  --cwd /home/vscode/src/auto-stack \
  --since 14d \
  --limit 20
```

### UC-3: Is this a recurring problem or a one-day cluster?

```bash
autosearch stats \
  --scope messages \
  --query '"cannot find main module"' \
  --group-by day \
  --role tool \
  --cwd /home/vscode/src/auto-stack \
  --since 14d
```

### UC-4: Which sub-project causes the most churn? (Phase 2 key)

```bash
autosearch stats \
  --scope messages \
  --query '"Exit code 1" OR "FAIL"' \
  --group-by subproject \
  --role tool \
  --since 14d
```

Fallback before `subproject` exists:

```bash
autosearch stats \
  --scope messages \
  --query '"Exit code 1" OR "FAIL"' \
  --group-by tool_file_path \
  --role tool \
  --since 14d
```

### UC-5: Is an error systemic or session-local?

```bash
autosearch stats \
  --scope messages \
  --query '"pre-commit" OR "hook failed"' \
  --group-by bash_command \
  --measure distinct_sessions \
  --role tool \
  --since 14d
```

### UC-6: Compare suspected co-occurring patterns (deferred true co-occurrence)

```bash
# Query A
autosearch stats --scope messages --query '"declared and not used"' --group-by session_id --role tool --since 14d --limit 30

# Query B
autosearch stats --scope messages --query '"pre-commit" OR "hook failed"' --group-by session_id --role tool --since 14d --limit 30
```

## 12. Ranked Use Cases (By Usefulness)

| Rank | Use Case | Why Useful | Phase | Example |
|---|---|---|---|---|
| 1 | Sessions with highest error density | Fastest prioritization of transcripts to inspect | 1 | `autosearch stats --scope messages --query '"Exit code 1" OR "FAIL"' --group-by session_id --role tool --limit 20` |
| 2 | Top failing bash command families | Identifies broken workflow classes quickly | 1 | `autosearch stats --scope messages --query '"Exit code 1"' --group-by bash_command --role tool --limit 20` |
| 3 | Most edited files | Surfaces churn hotspots and rework loops | 1 | `autosearch stats --scope messages --query 'Edit' --field tool_input --group-by tool_file_path --min-count 3` |
| 4 | Subproject churn hotspots | Separates monorepo pain areas | 2 | `autosearch stats --scope messages --query '"Exit code 1" OR "FAIL"' --group-by subproject --role tool` |
| 5 | Most read files | Reveals context bottlenecks | 1 | `autosearch stats --scope messages --query 'Read' --field tool_input --group-by tool_file_path --limit 20` |
| 6 | Failing test command trend by day | Detects regressions after changes | 1 | `autosearch stats --scope messages --query '"--- FAIL:" OR "FAIL\t"' --group-by day` |
| 7 | User correction hotspots by session | Quantifies execution drift | 1 | `autosearch stats --scope messages --query '"no " OR "undo" OR "wrong"' --group-by session_id --role user` |
| 8 | Missing-prereq failures by command | Prioritizes env hardening | 1 | `autosearch stats --scope messages --query '"command not found" OR "gcc" OR "not installed"' --group-by bash_command` |
| 9 | Error concentration by git remote | Finds repo-specific instability | 1 | `autosearch stats --scope sessions --query 'error OR fail' --group-by git_remote` |
| 10 | Model-specific failure concentration | Informs model routing | 1 | `autosearch stats --scope messages --query 'error OR fail' --group-by model` |
| 11 | Top commands by distinct sessions | Distinguishes broad vs local failures | 1 | `autosearch stats --scope messages --group-by bash_command --measure distinct_sessions` |
| 12 | Tool usage mix by role | Detects tool-loop heavy workflows | 1 | `autosearch stats --scope messages --group-by role --since 14d` |
| 13 | Frequent tool error families | Targets flaky tool interactions | 1 | `autosearch stats --scope messages --query 'tool_use_error OR "String to replace not found"' --group-by tool_name` |
| 14 | Assistant retry hotspots | Detects loop behavior | 1 | `autosearch stats --scope messages --query 'retry OR try again' --group-by session_id --role assistant` |
| 15 | Exit-code distribution | Splits success/failure paths quickly | 2 | `autosearch stats --scope messages --group-by exit_code --role tool --since 14d` |
| 16 | Session duration bucket vs errors | Separates marathon-noise from real breakage | 2 | `autosearch stats --scope sessions --query 'error OR fail' --group-by duration_bucket` |
| 17 | Host-specific instability | Catches machine-local environment issues | 1 | `autosearch stats --scope sessions --query 'error OR fail' --group-by host_id` |
| 18 | Week-over-week session volume | Supports planning for reflection batch size | 1 | `autosearch stats --scope sessions --group-by week --since 12w` |
| 19 | Workspace-level correction rates | Helps org/repo policy tuning | 1 | `autosearch stats --scope messages --query '"no " OR "wrong"' --group-by workspace --role user` |
| 20 | Long-tail risky files | Finds hidden repeated pain points | 1 | `autosearch stats --scope messages --query 'fail' --group-by tool_file_path --min-count 2` |
| 21 | Invalid-flag error families | Improves CLI UX | 1 | `autosearch stats --scope messages --query '"unknown flag" OR "invalid"' --group-by bash_command` |
| 22 | Tool-output-heavy workflows | Identifies candidates for truncation/UX improvements | 1 | `autosearch stats --scope messages --field tool_output --group-by tool_name` |
| 23 | Skill adoption tracking | Measures impact of newly released skills | 1 | `autosearch stats --scope messages --query 'skill' --group-by tool_name --since 30d` |
| 24 | Read vs edit drift comparison | Detects docs-code mismatch | 1 | run separate read/edit stats, compare top buckets |
| 25 | Cross-query co-occurrence approximation | Spots likely shared root causes | 1 | compare top `session_id` buckets from two related queries |

## 13. Example Workflows

### Workflow A: Prioritize reliability backlog

```bash
# 1) Top failing command families
autosearch stats --scope messages --query '"Exit code 1"' --group-by bash_command --role tool --limit 15

# 2) Hotspot files for one failure family
autosearch stats --scope messages --query '"go test ./..." AND "FAIL"' --group-by tool_file_path --limit 15

# 3) Sessions to inspect first
autosearch stats --scope messages --query '"go test ./..." AND "FAIL"' --group-by session_id --role tool --limit 10
```

### Workflow B: Build skill candidates from churn

```bash
# 1) Edit churn hotspots
autosearch stats --scope messages --group-by tool_file_path --query 'Edit' --field tool_input --min-count 3 --limit 20

# 2) Correction hotspots by session
autosearch stats --scope messages --group-by session_id --query '"no " OR "undo" OR "wrong"' --role user --limit 20

# 3) Drill into top session
autosearch session get <session_id>
```

### Workflow C: Environment hardening

```bash
# 1) Missing toolchain/error command families
autosearch stats --scope messages --group-by bash_command --query '"command not found" OR "gcc" OR "not installed"' --role tool --since 30d

# 2) Repo concentration
autosearch stats --scope sessions --group-by git_remote --query '"command not found" OR "gcc" OR "not installed"' --since 30d
```

## 14. Acceptance Criteria

Functional:
- `autosearch stats` exists and returns JSON envelope with `_meta` and `buckets`
- supports required keys/measures and shared filters
- returns uncapped `total_matches` and `total_buckets`
- deterministic sort order and stable pagination

Quality:
- unit tests for key validation and aggregation SQL construction
- integration tests for message and session scopes
- regression tests for `--cwd`/`--remote` conflict behavior
- tests for `--since` and absolute date windows
- tests for `bash_command` normalization behavior

Compatibility:
- no behavior change to existing `search` command outputs
- shared filter semantics stay aligned with `search`

## 15. Detailed Testing Plan

The stats feature should be validated at four levels: unit, query-integration, CLI integration, and end-to-end fixture runs.

### 15.1 Test data strategy

Use committed deterministic fixtures as the primary e2e source:
- `auto-search/testdata/etl-output/messages/year=2026/week=12/messages.parquet`
- `auto-search/testdata/etl-output/sessions/year=2026/month=03/sessions.parquet`

Use generated fixtures for edge cases via existing helpers in `internal/testutil/fixtures.go`:
- `GenerateFixtures`
- `GenerateDuplicateMessageFixtures`
- `GenerateDuplicateSessionFixtures`

Add a dedicated generator for stats edge cases (new helper) to produce:
- chained bash commands (`cd x && go build ...`)
- wrapped commands (`bash -lc 'go test ./...'`)
- empty-key rows for `(none)` bucket validation
- tie-breaking rows for deterministic sample selection

### 15.2 Unit tests (lower-level functions)

Target package: `internal/stats` (or equivalent aggregation package).

Required unit test groups:
- key validation:
  - valid keys per scope accepted
  - scope/key mismatch rejected with explicit error
  - case-insensitive key input canonicalized
- command normalization:
  - whitespace collapse
  - env-prefix stripping (`FOO=1 BAR=2 go test ./...`)
  - wrapper stripping (`bash -lc`, `sh -lc`)
  - chain splitting (`cd repo && go build ...` -> `go build`)
  - short commands and empty commands
- bucket key derivation:
  - `day` and `week` derivation (UTC)
  - `(none)` bucket assignment for empty key values
- sample row selection:
  - with query: highest score, tie -> newest timestamp, tie -> stable ID
  - without query: newest timestamp, tie -> stable ID
  - `sample_snippet` omitted when query is absent
- min-count semantics:
  - threshold applied after aggregation and before pagination
  - threshold metric follows selected `--measure`

### 15.3 Query integration tests (DB + aggregation engine)

Target: `internal/stats/stats_integration_test.go` using temp SQLite built from committed fixtures (same pattern as `internal/search/search_integration_test.go`).

Baseline deterministic assertions using committed fixtures:
- `--scope messages --group-by session_id`:
  - buckets: `test-session-1=8`, `test-session-2=2`, `test-session-3=2`
  - `_meta.total_matches=12`
  - `_meta.total_buckets_unfiltered=3`
  - `_meta.total_buckets=3`
- `--scope messages --group-by role`:
  - `assistant=5`, `tool=3`, `user=3`, `system=1`
- `--scope messages --group-by bash_command`:
  - `(none)=11`, `go test=1` (after normalization)
- `--scope sessions --group-by workspace`:
  - `/workspace/project-a=2`, `/workspace/project-b=1`
  - distinct-messages semantics: `project-a=10`, `project-b=2`
- query filter case:
  - query `\"Exit code 0\"` with `group-by session_id` -> two buckets (`test-session-1=1`, `test-session-3=1`)
- min-count case:
  - `group-by role --measure count --min-count 3` -> buckets `assistant`, `tool`, `user`
  - `group-by role --measure distinct_sessions --min-count 2` -> buckets `assistant`, `user`
- pagination/tie determinism:
  - stable order under ties (`count` ties resolved by key ascending)

### 15.4 CLI integration tests

Target: extend `internal/cli/cli_integration_test.go` with `stats` command coverage.

Required CLI-level checks:
- command success path:
  - `autosearch stats --scope messages --group-by session_id`
  - response shape `_meta` + `buckets`
- metadata invariants:
  - `total_matches` present and uncapped
  - `total_buckets_unfiltered` and `total_buckets` present and consistent
  - `returned_buckets`, `has_more`, `next_offset` pagination correctness
- error paths:
  - invalid `--group-by` key
  - scope/key mismatch
  - `--cwd` with `--remote`
  - invalid date flags / mixed relative+absolute date modes
- output hygiene:
  - JSON only on stdout
  - diagnostics on stderr

### 15.5 End-to-end tests (fixture-indexed command workflow)

E2E tests should exercise the full user flow:
1. `autosearch init`
2. `autosearch index --input auto-search/testdata/etl-output`
3. `autosearch stats ...`

Core e2e scenarios:
- ranking workflow:
  - group by `session_id` and verify top bucket points to expected hotspot session
- command-family workflow:
  - group by `bash_command` and verify normalized bucket keys
- trend workflow:
  - group by `day` and verify sparse output semantics
- drill-down workflow:
  - ensure each bucket has deterministic sample IDs that can be passed to `message get` / `session get`

E2E reliability checks:
- incremental reindex then rerun stats yields same results
- deterministic output ordering across repeated runs

Optional non-CI smoke suite (large local snapshot):
- run against `.tmp/etl-output` if present to catch high-cardinality performance regressions
- keep this suite opt-in via env flag (for example `AUTOSEARCH_E2E_LARGE=1`)

### 15.6 Performance test plan

Add benchmark tests for representative key/query combinations:
- high-cardinality key (`tool_file_path`)
- medium-cardinality key (`bash_command`)
- low-cardinality key (`role`)

For each benchmark, track:
- wall-clock latency
- rows scanned (if available)
- bucket count

Performance acceptance:
- meet Section 10 P50/P95 targets on the reference 1M-message dataset
- no full Go-side materialization for bucket computation

### 15.7 Regression gates

Before merge:
- `go test ./internal/stats ./internal/cli ./internal/search`
- `go test ./...` in `auto-search`
- one fixture-based CLI e2e run logged in CI artifacts

## 16. Rollout Plan

Phase 1 (MVP):
- `stats` command
- measures: `count`, `distinct_sessions`, `distinct_messages`
- key set priority: `session_id`, `bash_command`, `tool_file_path`, `day`, `tool_name`, `workspace`
- quickstart update with a dedicated stats section and search-vs-stats guidance

Phase 2:
- derived keys: `subproject`, `tool_file_dir`, `exit_code`, `duration_bucket`, optional `bash_command_raw`
- richer bucket drill-down metadata (`sample_snippet` standardization)

Phase 3:
- optional text-mode renderer
- reflection template integration and report-oriented presets

## 17. Feedback Resolution Checklist

This section maps prior review comments to concrete requirement updates in this document.

- Cap ambiguity framing added:
Current limitation is explicitly stated in Section 1 context (page-limited views can blur true frequency).
- Separate `stats` command retained:
Section 3 keeps `stats` separate from `search` and codifies the mental model split.
- Quickstart discoverability added:
Section 9 and Phase 1 rollout now require a quickstart section explaining `search` vs `stats`.
- Bucket drill-down usability improved:
Section 4 includes `sample_message_id`, `sample_session_id`, and optional `sample_snippet`.
- `bash_command` fragmentation addressed:
Section 5.4 defines normalization rules and command-family bucketing.
- Monorepo hotspot needs addressed:
Section 5.3 adds `subproject` and `tool_file_dir` as derived keys.
- Exit-code grouping added:
Section 5.3 adds `exit_code` as a derived key.
- Session ranking parity fixed:
Section 5.2 includes `session_id` for session scope, and use cases rank by session.
- Session-size signal added:
Section 5.3 adds `duration_bucket` derived key.
- Weak workspace example corrected:
Section 11 UC-4 now uses `subproject` (with `tool_file_path` fallback), not `workspace`.
- Co-occurrence concern scoped correctly:
Section 2 marks true co-occurrence out-of-scope for initial release; Section 11 UC-6 documents approximation workflow.
- Phase priorities adjusted:
Section 16 Phase 1 key priority now starts with `session_id` and includes the highest-impact keys from probe feedback.
