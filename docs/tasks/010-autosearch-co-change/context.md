# Context: Task 010 — Autosearch Co-Change Query

Verified codebase facts grounding the implementation of `autosearch co-change`. See [solution.md](./solution.md) for the design and [requirements.md](./requirements.md) for ACs.

## Key Files

### CLI command pattern (auto-search)
- `auto-search/internal/cli/root.go:54-65` — `AddCommand(...)` chain registering subcommands; a new command slots in here as `newCoChangeCmd()`.
- `auto-search/internal/cli/root.go:14-24` — `type ExitError struct { Code int; Err error }`; root executor unwraps via `errors.As(err, &exitErr)` at `:31` and returns `exitErr.Code`. Use this for non-zero exits (AC-10).
- `auto-search/internal/cli/search.go:32-43` — typical `cobra.Command` shape (`Use`, `Short`, `Args`, `RunE`); flags declared `cmd.Flags().StringVar/IntVar` at `:16-30`; success path `enc.Encode(result)` to `cmd.OutOrStdout()`.
- `auto-search/internal/cli/stats.go` — closest analog: SQL `GROUP BY` aggregation command producing a `_meta` + buckets JSON envelope.

### Parquet read + discovery (auto-search)
- `auto-search/internal/etlscan/discover.go:26` — `func Discover(inputRoot string) ([]ParquetSource, error)`.
- `auto-search/internal/etlscan/discover.go:36` — datasets are **hardcoded** `[]string{"messages","sessions"}`. **Must be extended** to include `commits`, `commit_files`, `git_repositories`, `git_refs` (or a new git-specific discover function).
- `auto-search/internal/etlscan/discover.go:11-22` — `ParquetSource{Dataset, PartitionKey, Path, SizeBytes, MtimeUnixMs}`.
- `auto-search/internal/etlscan/parquet_sessions.go:14-49` — reader pattern: `parquet.OpenFile` → `parquet.NewGenericReader[T](pf)` → batched `reader.Read(batch)` loop. Slim column-pruned structs (only the parquet-tagged fields you declare are read).

### In-memory SQLite (auto-search)
- `auto-search/internal/indexdb/schema.go:9` imports `modernc.org/sqlite`; `:171,:194` open via `sql.Open("sqlite", path)` — driver name is **`"sqlite"`**.
- Production code only opens **file-based** DBs; no `:memory:` usage exists yet. For co-change's ephemeral DB use `sql.Open("sqlite", ":memory:")` and **`db.SetMaxOpenConns(1)`** — a `:memory:` DB is per-connection, so a pool with >1 conn would see empty tables.
- `auto-search/internal/indexdb/query_messages.go:43-75` — query/scan pattern: `db.QueryRow(SQL, args...)` → `row.Scan(&...)`, with `sql.ErrNoRows` handling.
- `auto-search/internal/indexdb/indexer.go:245-283` — parquet→rows→insert pattern (`etlscan.ReadSessions` → loop → `Insert...`). Co-change mirrors the read half but inserts into the `:memory:` DB.

### Config / paths
- `auto-search/internal/config/settings.go:62-68` — `DefaultInputPath() (string, error)` returns `filepath.Join(autoDir, "etl", "output")`.
- `auto-search/internal/config/settings.go:10` imports `sharedconfig "github.com/mistakenot/auto-shared/config"`; `AutoDir()` resolves `~/.auto`.

### Remote normalisation to move into auto-shared
- `auto-etl/internal/git/normalize.go:12` — `func NormalizeRemoteURL(raw string) string` (ssh→https, lowercase host, strip `.git`).
- `auto-etl/internal/git/normalize.go:83` — `func ComputeRepoID(normalizedRemote string) string` (SHA256, first 16 hex).
- `auto-etl/internal/git/normalize.go:91` — `func ComputeRepoIDFromPath(absPath string) string`.
- `auto-shared/go.mod:1` — module `github.com/mistakenot/auto-shared`; existing packages: `config/`, `update/`, `version/`. New package lands at `auto-shared/git/normalize.go`.
- `auto-etl/go.mod` and `auto-search/go.mod` both already have `replace github.com/mistakenot/auto-shared => ../auto-shared`, so importing the moved code from both works immediately.

### Canonical git schema (source of slim structs)
- `auto-etl/internal/model/git.go` — `Commit` (`id`, `short_id`, `repo_id`, `author_name/email`, `author_date`, `files_changed`, `session_id`, `message_truncated`, `year`, `month`, …), `CommitFile` (`commit_id`, `repo_id`, `file_path`, `change_type`, `old_path`, `insertions`, `deletions`, `author_date`, …), `GitRef` (`ref_name`, `ref_type`, `commit_id`, `is_default`, `is_remote`), `GitRepository` (`repo_id`, `repo_remote_normalized`, …). Co-change defines slim structs with a subset of these exact parquet tags.

### ID format — VERIFIED, load-bearing for the ref join (AC-5)
- `auto-etl/internal/git/extract.go:239` — `CommitFile.CommitID = commit.ID`, and `commit.ID = repoID + "-" + sha` (confirmed by `strings.TrimPrefix(c.ID, repoID+"-")` at `:61,:178`). So `commits.id` and `commit_files.commit_id` are **`<repoID>-<sha>`**.
- `auto-etl/internal/git/extract.go:324,349` — `git_refs.commit_id` is the **raw** `%(objectname)` SHA, no prefix.
- ⚠️ `auto-etl/docs/git-history-etl.md` describes `commits.id` loosely as a full SHA — the **code is authoritative**; it is `<repoID>-<sha>`. The ref join must strip the prefix: `git_refs.commit_id = substring(commits.id, length(repoID)+2)`.
- `auto-etl/internal/git/extract.go:36` — no-origin repos get a path-derived `repo_id` via `ComputeRepoIDFromPath`. Co-change deliberately does NOT use this (repo_path is host-volatile + dropped from fixtures); no-origin repos require `--repo-id` (AC-1/AC-10).

### Tests + build
- `auto-search/internal/cli/cli_integration_test.go:90-114` — `runCLI(t, args...) (stdout, stderr string, code int)` builds `cli.NewRootCmd()` and unwraps `ExitError`.
- `auto-search/internal/cli/cli_integration_test.go:22` — `t.Setenv("HOME", t.TempDir())`; `:185` `fixtureInputDir(t)` → `auto-search/testdata/etl-output/` (existing `messages/`,`sessions/` fixtures). New git fixtures go under `auto-search/testdata/fixtures/auto-stack-snapshot/`.
- Root `Makefile:54-55` `build-search` (`ENTRY := ./cmd/autosearch`); `:159-163` `test` loops `go test ./...` per project. No Makefile inside auto-search — fixture targets go in the **root** Makefile.
- Go version `1.26.1` (`auto-search/go.mod:3`, `auto-etl/go.mod:3`).

## Patterns

- **New query command:** `cli/<cmd>.go` (cobra: flags, validation, JSON emit, `ExitError`) → registered in `cli/root.go` → heavy logic in a new `internal/<domain>/` package (types, resolution, load, query, scoring). `stats` and `search` follow this.
- **JSON envelope:** `{_meta:{request_id, elapsed_ms, …}, <payload>}` (per `auto-search/docs/grouping-solution.md`). Co-change uses `{_meta, metadata, related_files}`.
- **Parquet read = slim generic reader** over a struct whose parquet tags select the columns (column pruning). No external query tool.
- **On-demand, not indexed:** auto-search does NOT persist git data into its FTS index; co-change reads parquet per query into an ephemeral `:memory:` DB. (Future optimisation, out of scope: index git data — see solution.md Rejected Alternatives.)
- **CLI conventions (root `CLAUDE.md`):** JSON default; stdout = parseable payload only, diagnostics to stderr; fail-fast on bad args; concrete remediation hints in errors.
- **Single source of truth for normalisation:** moving `NormalizeRemoteURL` to `auto-shared` prevents auto-etl/auto-search drift that would silently break `repo_id` matching.

## Related Tasks

- **Task 002 (git-history-etl)** — built the five git datasets, the `<repoID>-<sha>` commit ID format, repo discovery via remotes cache, and URL normalisation / `repo_id` derivation. Spec: `auto-etl/docs/git-history-etl.md`. This is the data co-change consumes.
- **Task 008 (commit-session-link)** — populates `commits.session_id` (Session-Id trailer, with message-content fallback). Co-change reads `session_id` as-is to produce `top_sessions` (AC-4); join is `commits.session_id` → `sessions.id`.
- **Task 009 (etl-run-hardening)** — upstream reliability of the parquet co-change reads; no direct code overlap.
- No other task overlaps (no existing file-coupling / heatmap / signal task).
