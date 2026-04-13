# Result: Add `session list` command

## Branch
`improve/autosearch/3-session-list`

## PR
https://github.com/mistakenot/auto-stack/pull/6

## Summary of Changes

### `auto-search/internal/indexdb/query_sessions.go`
- Added `ListSessionsOpts` struct with filters: `Workspace`, `Remote`, `StartMs`, `EndMs`, `Limit`, `Offset`
- Added `SessionListRow` struct with compact session summary fields (session_id, workspace, git_remote, model, agent, timestamps, total_tokens, message_count)
- Added `ListSessions(db, opts)` function that queries the sessions table directly (no FTS) with a LEFT JOIN to messages for message counts, optional WHERE clauses for all filters, ordered by `first_message_at DESC`, with pagination

### `auto-search/internal/cli/session.go`
- Added `newSessionListCmd()` with flags: `--index`, `--since`, `--after`, `--before`, `--cwd`, `--remote`, `--limit`, `--offset`, `--request-id`
- Reuses `search.ParseTimeFilter` for date filter parsing (consistent with existing search commands)
- Outputs JSON with `_meta` (request_id, elapsed_ms, total, offset, limit, returned) and `sessions` array
- Registered in session command group alongside `get` and `describe`

## Test Results
- `go build ./...` -- PASS
- `go vet ./...` -- PASS
- `go test ./...` -- PASS (all 6 test packages)
