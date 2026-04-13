# Result: Add input validation for --role, --mode, and --limit flags

## Branch

`improve/autosearch/1-input-validation`

## PR

https://github.com/mistakenot/auto-stack/pull/5

## Summary of Changes

### `auto-search/internal/search/messages.go`
- Added `normalizeRole()` function that accepts only `"user"`, `"assistant"`, `"tool"`, or empty string (case-insensitive, trimmed). Returns a clear error for invalid values.
- Updated `normalizePagination()` to reject negative `pageSize` with an error, keep `pageSize == 0` as "use default", and enforce an upper bound of 1000.
- Added `maxPageSize = 1000` constant.
- `SearchMessages()` now calls `normalizeRole()` and passes the normalized value through.

### `auto-search/internal/search/sessions.go`
- `SearchSessions()` now calls `normalizeRole()` and passes the normalized value through.

### `auto-search/internal/cli/search.go`
- Added `--mode` validation in `RunE`: rejects any value other than `"bm25"` with a clear error message.

### `auto-search/internal/search/search_integration_test.go`
- Updated `TestSessionSearchRoleFilter` to expect an error (not zero results) when an invalid role is provided.

## Test Results

All tests pass:
```
ok  	github.com/mistakenot/auto-search/internal/cli
ok  	github.com/mistakenot/auto-search/internal/indexdb
ok  	github.com/mistakenot/auto-search/internal/query
ok  	github.com/mistakenot/auto-search/internal/search
ok  	github.com/mistakenot/auto-search/internal/stats
ok  	github.com/mistakenot/auto-search/internal/testutil
```

`go vet ./...` clean.
