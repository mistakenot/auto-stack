# Result: Combine redundant SQL count queries for performance

## Branch
`improve/autosearch/2-sql-performance`

## PR
https://github.com/mistakenot/auto-stack/pull/4

## Summary of Changes

### `auto-search/internal/search/messages.go`
Replaced 3 separate COUNT queries (lines 220-236) with a single combined query:
```sql
SELECT COUNT(*), COUNT(DISTINCT m.session_id), COUNT(DISTINCT m.message_id) FROM messages_fts JOIN messages m ...
```
This reduces the count phase from 3 SQL round-trips to 1, each of which previously required a full FTS5 MATCH evaluation.

### `auto-search/internal/search/sessions.go`
Replaced N+1 per-row message count pattern (one `SELECT COUNT(*) FROM messages WHERE session_id = ?` per hit) with a single batch query:
```sql
SELECT session_id, COUNT(*) FROM messages WHERE session_id IN (?, ?, ...) GROUP BY session_id
```
Results are stored in a map and looked up when building hits. This reduces the per-page query count from N+2 to 3.

Added `strings` import for `strings.Join` used in placeholder construction.

## Test Results
- `go build ./...` -- passed
- `go vet ./...` -- passed
- `go test ./...` -- all packages passed (search, cli, indexdb, query, stats, testutil)

## JSON Output Compatibility
No structural changes to `Meta`, `MessageHit`, or `SessionHit` types. All existing JSON field names and semantics are preserved. The same values are computed, just from fewer SQL queries.
