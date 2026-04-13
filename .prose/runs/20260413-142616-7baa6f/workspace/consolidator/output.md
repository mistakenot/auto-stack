# Consolidated Priorities: Autosearch Improvements

kind: output
service: consolidator
count: 3

---

## item_1: Add input validation for `--role`, `--mode`, and `--limit` flags

**Original problems:** P1 (--role silently accepts invalid values), P2 (--limit accepts 0 and negatives), P3 (--mode silently accepts invalid values), P5 (no upper bound on --limit)

**Rationale:** Both analyst and reviewer rated this HIGH confidence. The reviewer verified every root cause claim and found no fatal flaws. Regression risk is LOW -- these are additive validations that reject previously-silent bad input. Effort is Small. This has the best impact-to-effort ratio in the entire set.

**Scope:**

1. **`auto-search/internal/search/messages.go`** -- Add a `normalizeRole()` function (analogous to existing `normalizeField()`) that accepts only `"user"`, `"assistant"`, `"tool"`, or empty string. Call it in both `SearchMessages()` and `SearchSessions()`.

2. **`auto-search/internal/search/messages.go`** -- In `normalizePagination()` (lines 332-339), reject negative `pageSize` with an error. Keep `pageSize == 0` as "use default" (matches Cobra zero-value convention). Add an upper bound cap of 1000.

3. **`auto-search/internal/cli/search.go`** -- Validate `--mode` in `RunE`: if mode is not `"bm25"`, return an error. (Reviewer suggested removing the flag entirely as cleaner, but the analyst's validation approach is safer to avoid breaking scripts that pass `--mode bm25`.)

**Files changed:**
- `auto-search/internal/search/messages.go` (add `normalizeRole`, update `normalizePagination`)
- `auto-search/internal/search/sessions.go` (call `normalizeRole`)
- `auto-search/internal/cli/search.go` (add `--mode` validation in `RunE`)

**Acceptance criteria:**
- `autosearch search --role bogus "test"` returns a non-zero exit code with an error naming the valid values
- `autosearch search --limit -1 "test"` returns a non-zero exit code with an error
- `autosearch search --limit 5000 "test"` returns a non-zero exit code with an error stating the max
- `autosearch search --mode vector "test"` returns a non-zero exit code with an error
- All existing valid usages (including `--limit 0` meaning default, `--role user`, `--mode bm25`) continue to work
- Existing tests in `cli_integration_test.go` and `search_integration_test.go` pass

---

## item_2: Combine redundant SQL count queries for performance

**Original problems:** P4 (performance degradation with workspace filters), P11 (redundant meta fields)

**Rationale:** The reviewer confirmed the 4-query pattern in `execMessageSearch` and the N+1 pattern in `execSessionSearch`. The reviewer also independently discovered the `neighborMessageIDs` N+1 problem (40 extra queries per page) and the 7-query pattern in `CountSessionMessages`. While the analyst's "30-80x degradation" magnitude was flagged as unverified, the architectural analysis is sound. Merging suggestions 2 and 7: combine the three count queries into one (eliminating the redundant `distinct_messages` query entirely), and populate the redundant meta fields from the single result (keeping them as aliases to avoid breaking JSON consumers).

**Scope:**

1. **`auto-search/internal/search/messages.go`** -- Replace the 3 separate count queries (lines 220-235) with a single `SELECT COUNT(*), COUNT(DISTINCT m.session_id), COUNT(DISTINCT m.message_id)` query. Set `TotalHits`, `TotalMatches`, and `DistinctMessages` from this single result (keep all JSON fields as aliases, no breaking change).

2. **`auto-search/internal/search/sessions.go`** -- Fix the N+1 per-row message count at line 216. Replace the per-hit `SELECT COUNT(*) FROM messages WHERE session_id = ?` with a batch query: run a single `SELECT session_id, COUNT(*) FROM messages WHERE session_id IN (?) GROUP BY session_id` after collecting all session IDs from the page, then populate `MessageCount` from the map.

3. Do NOT add the composite index (`idx_messages_workspace_timestamp`) -- reviewer flagged this as speculative and needing benchmarks first. Do NOT attempt the CTE approach in this pass -- save for a follow-up.

**Files changed:**
- `auto-search/internal/search/messages.go` (combine count queries, remove separate `distinctMessagesQuery`)
- `auto-search/internal/search/sessions.go` (batch the per-row message count)

**Acceptance criteria:**
- `autosearch search --scope messages "test"` returns identical JSON output (same meta fields, same values) as before the change
- `autosearch search --scope sessions "test"` returns identical JSON output as before
- `autosearch search --cwd /some/path "test"` completes without errors and produces correct results
- Total SQL queries per search invocation is reduced: messages scope goes from 4 to 2, sessions scope eliminates the N+1 per-row query
- Existing integration tests pass

---

## item_3: Add `session list` command

**Original problems:** P13 (no session list command; workaround is searching for "user" in session scope)

**Rationale:** Both analyst and reviewer rated this HIGH confidence, LOW regression risk, Small effort. The reviewer confirmed it as a legitimate usability gap. It is purely additive (new command, new function) with no impact on existing commands. The current workaround documented in quickstart is unintuitive. This provides high user-facing value for low implementation cost.

**Scope:**

1. **`auto-search/internal/indexdb/query_sessions.go`** -- Add a `ListSessions(ctx, opts)` function that queries the `sessions` table directly (no FTS) with optional filters: `workspace` (for --cwd), `remote`, `since`/`after`/`before` date filters. Order by `first_message_at DESC`. Support `--limit` and `--offset` pagination.

2. **`auto-search/internal/cli/session.go`** -- Add a `newSessionListCmd()` function that wires the CLI flags to `ListSessions()`. Output JSON by default following project conventions. Register it in the session command group (alongside `get` and `describe`).

3. Follow the existing patterns: reuse `SessionRow` scanning from `query_sessions.go`, reuse date filter parsing from the search commands.

**Files changed:**
- `auto-search/internal/indexdb/query_sessions.go` (add `ListSessions` function)
- `auto-search/internal/cli/session.go` (add `newSessionListCmd`, register in command group)

**Acceptance criteria:**
- `autosearch session list` returns a JSON array of sessions ordered by most recent first
- `autosearch session list --limit 5` returns at most 5 sessions
- `autosearch session list --cwd /some/path` filters to sessions matching that workspace
- `autosearch session list --since 1w` returns only sessions from the last week
- Output includes session ID, workspace, first/last message timestamps, and message count
- The command appears in `autosearch session --help`

---

## Rejected / Deferred Items

**Suggestion 4 (error messages):** Good idea, low effort, but lower impact than the top 3. Defer to next round.

**Suggestion 5 (ISO timestamps):** Valid and easy, but cosmetic. Can be bundled with item_3 if convenient.

**Suggestion 6 (session snippets):** Reviewer raised valid concern that transcript snippets may produce low-quality output. Medium effort with uncertain UX benefit. Defer.

**Suggestion 8 (docs/doctor):** Useful per project convention but not addressing user-facing search pain points. Defer.

**Suggestion 9 (skills ETL fix):** Out of scope for autosearch focus area. Root cause unverified in ETL code. High regression risk. Dropped.

**Suggestion 10 (meta-search pollution):** Design-level issue with no clear bounded solution. Reviewer noted the analyst missed simpler approaches. Dropped for now.
