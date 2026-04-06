---
hash: "dda3d92c"
id: "9c27e5d1"
summary: "Implementation plan for autosearch date filters (`--since`, `--after`, `--before`) across message and session search, including validation, SQL wiring, and test coverage."
title: "AutoSearch Date Filter Implementation"
---

# AutoSearch Date Filter Implementation

## 1. Problem (Resolved)

`autosearch search` exposes `--since`, `--after`, and `--before` date filters, fully wired into search execution.

Implemented:

- Flags declared in CLI and passed through to search option structs.
- `MessageSearchOpts` and `SessionSearchOpts` carry `Since`, `After`, `Before` fields.
- SQL queries include timestamp predicates (`messages.timestamp`, `sessions.first_message_at`).
- Unit, integration, and CLI tests cover all date filtering paths.
- Filter canonicalization included in hit IDs for stability.

## 2. Scope

In scope:

- `autosearch search` for both scopes:
- `--scope messages` time filtering on `messages.timestamp`.
- `--scope sessions` time filtering on `sessions.first_message_at`.
- Validation and normalization for `--since`, `--after`, `--before`.
- Deterministic filter canonicalization so hit IDs remain stable and auditable.
- Unit, integration, and CLI coverage for all date-filter paths.

Out of scope:

- `fix` command date filtering.
- semantic mode changes.
- new output formats.

## 2.1 Beads Tracking

Parent feature:

- `auto-ahn` — autosearch: implement search date filters (`--since/--after/--before`)

Child tasks:

- `auto-ahn.1` — add date filter parser and validation utility
- `auto-ahn.2` — wire date filters into message/session search execution
- `auto-ahn.3` — add date-filter coverage in search integration tests
- `auto-ahn.4` — add CLI validation tests and update acceptance docs for date filters

Dependency order:

- `auto-ahn.2` blocks on `auto-ahn.1`
- `auto-ahn.3` blocks on `auto-ahn.2`
- `auto-ahn.4` blocks on `auto-ahn.2` and `auto-ahn.3`

## 3. Behavior Specification

### 3.1 Filter Modes

Allowed modes:

- Relative mode: `--since <duration>`.
- Absolute mode: `--after <time>` and/or `--before <time>`.

Invalid combination:

- `--since` cannot be combined with `--after` or `--before`.

Rationale:

- Keeps one filter mode at a time unless explicitly defined otherwise.

### 3.2 Time Field by Scope

- Messages scope: compare against `messages.timestamp`.
- Sessions scope: compare against `sessions.first_message_at`.

### 3.3 Bound Semantics

- `--since X` means `ts >= now - X`.
- `--after A` means `ts >= A` (inclusive lower bound).
- `--before B` means `ts < B` (exclusive upper bound).

Validation rule:

- If both `after` and `before` are present, `after < before` must hold.

### 3.4 Accepted Input Formats

`--since`:

- `<int><unit>` where unit is one of `m`, `h`, `d`, `w`.
- Examples: `5m`, `12h`, `5d`, `1w`.

`--after` and `--before`:

- `YYYY-MM-DD` (interpreted as `00:00:00Z`).
- RFC3339 timestamp (timezone required or `Z`).

Examples:

- `--after 2026-03-01 --before 2026-03-07`
- `--after 2026-03-01T12:00:00Z --before 2026-03-01T18:00:00Z`

### 3.5 Error Contract

Invalid input returns exit code 1 with actionable message:

- invalid since format
- invalid date/timestamp format
- mixed mode (`--since` with `--after/--before`)
- invalid range (`after >= before`)

## 4. Implementation Plan

### 4.1 New Time Filter Utility

Add `auto-search/internal/search/timefilters.go`:

- Parse and validate flags into normalized unix-ms bounds.
- Expose canonical filter string for hit ID generation.
- Support injected `now` for deterministic tests.

Proposed API:

- `type TimeFilter struct { StartMs *int64; EndMs *int64; Canonical string }`
- `func ParseTimeFilter(now time.Time, since, after, before string) (TimeFilter, error)`

Notes:

- `StartMs` maps to `>=` predicate.
- `EndMs` maps to `<` predicate.

### 4.2 Search Option Structs

Update:

- `MessageSearchOpts` with `Since`, `After`, `Before`.
- `SessionSearchOpts` with `Since`, `After`, `Before`.

CLI wiring in `internal/cli/search.go`:

- Pass date flags into both scope option structs.

### 4.3 SQL Predicate Wiring

Message scope query (`messages` table join):

- Add `AND m.timestamp >= ?` when `StartMs` is set.
- Add `AND m.timestamp < ?` when `EndMs` is set.

Session scope query (`sessions` table join):

- Add `AND s.first_message_at >= ?` when `StartMs` is set.
- Add `AND s.first_message_at < ?` when `EndMs` is set.

### 4.4 Hit ID and Filter Canonicalization

Current hit IDs include normalized repo filters only.

Update normalization to include time filters:

- `since=...` or
- `after=...;before=...`

Use normalized unix-ms values in canonical string to avoid format drift (`2026-03-01` vs `2026-03-01T00:00:00Z`).

### 4.5 Wildcard Fallback Compatibility

Message-scope fallback path must reuse the exact same time bounds.

- Only query text changes during fallback.
- Time predicates remain identical.

## 5. Test Coverage Plan

## 5.1 Unit Tests

Add `auto-search/internal/search/timefilters_test.go` with table-driven cases:

- valid `since` parsing for `m`, `h`, `d`, `w`.
- invalid `since` values (`0d`, `abc`, `7x`, empty numeric).
- valid `after` only.
- valid `before` only.
- valid `after+before`.
- invalid mixed mode (`since` + `after`, `since` + `before`).
- invalid range (`after == before`, `after > before`).
- `YYYY-MM-DD` parsing to UTC midnight.
- RFC3339 parsing with timezone.
- canonical string generation stability.

Coverage expectation:

- 100% branch coverage for parser validation paths.

## 5.2 Search Integration Tests

Extend `auto-search/internal/search/search_integration_test.go`:

Message scope:

- `--since` returns subset of recent fixture messages.
- `--after/--before` returns only in-window messages.
- `before` exclusivity boundary check.

Session scope:

- `--since` filters by `first_message_at`.
- `--after/--before` filters by session start.
- `before` exclusivity boundary check.

Cross-cutting:

- fallback query still works when a date filter is present.
- same query with different date filters produces different stable hit IDs.
- no filters preserves existing result counts (regression guard).

## 5.3 CLI Integration Tests

Extend `auto-search/internal/cli/cli_integration_test.go`:

- valid `search ... --since 7d` returns JSON success.
- valid absolute window returns JSON success.
- invalid mixed mode exits non-zero with clear message.
- invalid date format exits non-zero with clear message.
- invalid range exits non-zero with clear message.

## 5.4 Acceptance Additions

Update v1 acceptance in `auto-search/docs/solution.md` section 11.4 and 11.5:

- include one canonical message-scope date-filter check.
- include one canonical session-scope date-filter check.
- include one invalid-input CLI check.

## 6. Rollout Sequence

1. Add parser utility and unit tests.
2. Wire opts and SQL predicates for both scopes.
3. Update filter canonicalization used for hit IDs.
4. Add/extend integration tests in `search` package.
5. Add/extend CLI integration tests.
6. Update docs (`quickstart`, `solution` acceptance notes).
7. Run full test suite: `go test ./...` in `auto-search`.

## 7. Done Criteria

This work is complete when all are true:

- Date flags materially change search results in both scopes.
- Invalid combinations fail fast and explain remediation.
- Hit IDs include canonical time filters.
- New tests cover parser, search SQL behavior, fallback, and CLI validation.
- `go test ./...` passes in `auto-search`.
