---
hash: "d1328579"
id: "98a95cc9"
read_when: "implementing git history ETL in auto-etl or understanding the writer/model pattern for new ETL sources"
summary: "Verified codebase reference for implementing git history ETL: key files for model, writer, sync state, transform utilities, and CLI with exact line numbers and struct signatures."
title: "Context: Task 002 — Git History ETL"
---

# Context: Task 002

Codebase reference for the git history ETL implementation. See [solution.md](./solution.md) for the design.

## Key Files

### Model layer
- `auto-etl/internal/model/model.go:5` — `const SchemaVersion = 2`, used by all row structs
- `auto-etl/internal/model/model.go:24-63` — `AgentMessage` struct: pattern for parquet tags, `dict` encoding on low-cardinality fields (`session_id`, `host_id`, `role`, `workspace`, `git_remote`), no dict on content/ID/timestamp fields
- `auto-etl/internal/model/model.go:98-101` — `TransformedRows` struct: container for ETL output (`Messages []AgentMessage`, `Sessions []AgentSession`)
- `auto-etl/internal/model/github.go:117-120` — `GitHubSyncResult` struct: same pattern for GitHub ETL output

### Writer layer
- `auto-etl/internal/writer/writer.go:63-67` — `partKey` struct: `{Year, Week, Month int}`, used for partition grouping
- `auto-etl/internal/writer/writer.go:87-105` — `writeParquet[T any](path string, rows []T) error`: generic writer, creates dirs, uses `parquet.NewGenericWriter[T](f)`
- `auto-etl/internal/writer/writer.go:78-85` — `groupSessionsByMonth()`: partition grouping pattern, returns `map[partKey][]T`
- `auto-etl/internal/writer/github.go:24-67` — `WriteGitHub()`: read-merge-write pattern for mutable partitions
- `auto-etl/internal/writer/github.go:145-164` — `readExistingParquet[T any](path string) ([]T, error)`: generic reader, returns `nil, nil` if file doesn't exist

### Sync state
- `auto-etl/internal/github/syncstate.go:12-15` — `SyncState` struct: `{SchemaVersion int, Repos map[string]*RepoState}`
- `auto-etl/internal/github/syncstate.go:33-36` — `SyncStatePath()`: returns `~/.auto/etl/github/sync-state.json`
- `auto-etl/internal/github/syncstate.go:40-65` — `LoadSyncState()`: returns empty state on missing/corrupt file, logs warning
- `auto-etl/internal/github/syncstate.go:68-82` — `Save()`: atomic write via temp file + rename

### Transform utilities
- `auto-etl/internal/transform/transform.go:463-479` — `MidTruncate(s string, maxChars int) string`: exported, symmetric mid-truncation with marker `\n…[truncated N chars]…\n`

### CLI
- `auto-etl/cmd/run.go:33-36` — `validOnlyValues`: `map[string]bool{"sessions": true, "github": true}`
- `auto-etl/cmd/run.go:38-49` — `init()`: flag registration pattern with `runCmd.Flags().StringVar()`
- `auto-etl/cmd/run.go:107-126` — `parseOnlyFlag()`: validates `--only` values, returns source map, default=all
- `auto-etl/cmd/run.go:190-264` — `runGitHubSync()`: signature `(ctx, hostID, remotes, explicitOnly) error`
- `auto-etl/cmd/run.go:294-327` — `etlSettings` struct with remotes cache, `loadRemotesCache()` / `saveRemotesCache()`

### E2E tests
- `auto-etl/e2e_test.go:23-50` — `TestMain`: builds binary, runs pipeline with `--only sessions`, validates output with `genstats`

## Patterns

### Adding a new ETL source (GitHub PR precedent)
The GitHub PR ETL was added as a single large commit followed by refinements:
1. New model structs in `internal/model/github.go`
2. New extraction package in `internal/github/` with client, fetch, transform, syncstate
3. New writer function in `internal/writer/github.go`
4. CLI wiring: added `"github"` to `validOnlyValues`, new `runGitHubSync()` function
5. No changes to existing session ETL code

### Parquet conventions
- Dict encoding (`parquet:"field,dict"`) for: role, status, type discriminators, paths that repeat across rows, email, remote URLs
- No dict for: content strings, diffs, messages, IDs, truncated strings, timestamps, numeric counts
- Partition fields (`Year`, `Month`, `SchemaVersion`) always `int32`

### Incremental state
- GitHub uses `SyncState` JSON file at `~/.auto/etl/github/sync-state.json`
- Session ETL uses existing-file check (immutable partitions, skip if file exists)
- Git ETL uses `~/.auto/etl/git/sync-state.json` (matching GitHub pattern, NOT inside `settings.json`)

### Remote URL handling
- `cmd/run.go:345-352` — `gitRemoteOrigin(dir)`: shells out to `git -C dir remote get-url origin`, returns trimmed string
- Remotes cache: `workspace → remote URL` map in `~/.auto/etl/settings.json`

## Related Tasks

- Task 001 (`001-ts-import-graph`): different sub-project (autograph), not directly relevant to ETL patterns
