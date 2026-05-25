# Plan: Task 002

## Summary

Implement git history ETL as a new source in auto-etl, following the GitHub PR ETL pattern: model structs → extraction package → writer → CLI wiring → tests.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| + | `auto-etl/internal/model/git.go` | Five parquet row structs + GitETLResult container |
| + | `auto-etl/internal/git/normalize.go` | URL normalization + repo_id computation |
| + | `auto-etl/internal/git/normalize_test.go` | Tests for URL normalization |
| + | `auto-etl/internal/git/state.go` | Seen-commit set: load/save per-repo SHA sets |
| + | `auto-etl/internal/git/state_test.go` | Tests for incremental state |
| + | `auto-etl/internal/git/extract.go` | Git command execution + parsing into model structs |
| + | `auto-etl/internal/git/discover.go` | Repo discovery from remotes cache + --repo-path |
| + | `auto-etl/internal/git/extract_test.go` | Tests for git output parsing |
| + | `auto-etl/internal/writer/git.go` | WriteGit: partition and write five datasets |
| ~ | `auto-etl/cmd/run.go` | Add "git" to validOnlyValues, --repo-path, --since, runGitETL() |
| ~ | `auto-etl/cmd/run_only_test.go` | Test --only git validation |

## Links

- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test

- [ ] `auto-etl/internal/git/normalize_test.go` — unit: URL normalization, repo_id generation, no-remote fallback
- [ ] `auto-etl/internal/git/state_test.go` — unit: seen-commit load/save/filter
- [ ] `auto-etl/internal/git/extract_test.go` — unit: parse git log, diff-tree, show, for-each-ref output
- [ ] `auto-etl/cmd/run_only_test.go` — unit: --only git accepted, unknown values rejected
- [ ] Manual e2e: `go run . run --only git --repo-path . --output .tmp/output` against this repo, inspect with duckdb

## Execution Sequence

```
Phase 1 (Model) --> Phase 2 (Normalize+State) --> Phase 3 (Extract) --> Phase 4 (Writer) --> Phase 5 (CLI+E2E)
```

All phases are sequential — each builds on the prior phase's types and functions.

## Plan

### Phase 1: Model Structs

Define the five parquet row structs and result container.

- [x] Step 1.1: Create `auto-etl/internal/model/git.go` with structs:
  - `GitRepository` — fields from spec table: `repo_id`, `repo_remote`, `repo_remote_normalized`, `repo_path`, `worktree_path`, `default_branch_observed`, `host_id`, `first_seen_at`, `last_seen_at`, `etl_run_id`, `collected_at`, `schema_version`
  - `GitRef` — fields: `id`, `repo_id`, `ref_name`, `ref_type`, `commit_id`, `is_default`, `is_remote`, `etl_run_id`, `collected_at`, `schema_version`
  - `Commit` — fields: `id`, `short_id`, `repo_id`, `tree_sha`, `author_name`, `author_email`, `author_date`, `author_date_offset`, `committer_name`, `committer_email`, `committer_date`, `committer_date_offset`, `message`, `message_truncated`, `is_merge`, `parent_count`, `parent_shas`, `files_changed`, `insertions`, `deletions`, `trailers_json`, `patch_id`, `etl_run_id`, `collected_at`, `year`, `month`, `schema_version`
  - `CommitFile` — fields: `id`, `commit_id`, `repo_id`, `file_index`, `file_path`, `change_type`, `old_path`, `insertions`, `deletions`, `old_blob_sha`, `new_blob_sha`, `old_mode`, `new_mode`, `is_binary`, `diff`, `diff_truncated`, `author_date`, `etl_run_id`, `collected_at`, `year`, `month`, `schema_version`
  - `CommitHunk` — fields: `id`, `commit_id`, `repo_id`, `file_index`, `hunk_index`, `file_path`, `old_path`, `old_start`, `old_lines`, `new_start`, `new_lines`, `hunk_header`, `hunk_text`, `hunk_text_truncated`, `hunk_hash`, `author_date`, `etl_run_id`, `collected_at`, `year`, `month`, `schema_version`
  - `GitETLResult` — container: `Repositories []GitRepository`, `Refs []GitRef`, `Commits []Commit`, `Files []CommitFile`, `Hunks []CommitHunk`
  - Use `parquet:"field,dict"` for: `repo_id`, `ref_name`, `ref_type`, `host_id`, `etl_run_id`, `author_name`, `author_email`, `committer_name`, `committer_email`, `change_type`, `file_path`, `old_path`
  - No dict for: `id`, `commit_id`, content strings (`message`, `diff`, `hunk_text`), truncated strings, SHAs, timestamps, counts
- [x] Step 1.2: Verify: `cd auto-etl && go build ./...` passes
- [x] Step 1.3: Commit: `feat(002): phase 1 — git ETL model structs`

### Phase 2: Normalize + State

URL normalization, repo_id computation, and incremental seen-commit tracking.

- [x] Step 2.1: Create `auto-etl/internal/git/normalize.go`:
  - `NormalizeRemoteURL(raw string) string` — strip `.git` suffix, lowercase host, convert `git@host:owner/repo` to `https://host/owner/repo`
  - `ComputeRepoID(normalizedRemote string) string` — SHA256 hex of normalized remote, truncated to 16 chars for readability
  - `ComputeRepoIDFromPath(absPath string) string` — SHA256 hex of absolute path (fallback for no-remote repos)
- [x] Step 2.2: Create `auto-etl/internal/git/normalize_test.go`:
  - Test cases: HTTPS with/without `.git`, SSH `git@` format, uppercase hosts, no-remote path fallback, idempotency
  - Verify: `go test ./internal/git/...` passes
- [x] Step 2.3: Create `auto-etl/internal/git/state.go`:
  - `GitSyncState` struct: `{SchemaVersion int, Repos map[string]*GitRepoState}` where `GitRepoState` has `SeenSHAs map[string]bool`
  - `GitSyncStatePath() string` — returns `~/.auto/etl/git/sync-state.json`
  - `LoadGitSyncState(path string) *GitSyncState` — returns empty state if missing/corrupt, logs warning (match `syncstate.go` pattern)
  - `(*GitSyncState) Save(path string) error` — atomic write via temp+rename
  - `(*GitRepoState) IsNew(sha string) bool` — check against seen set
  - `(*GitRepoState) MarkSeen(shas []string)` — add batch of SHAs to seen set
- [x] Step 2.4: Create `auto-etl/internal/git/state_test.go`:
  - Test cases: load missing file, load corrupt file, save + reload roundtrip, IsNew/MarkSeen behavior, large SHA set
  - Verify: `go test ./internal/git/...` passes
- [x] Step 2.5: Verify: `cd auto-etl && go build ./...` passes
- [x] Step 2.6: Commit: `feat(002): phase 2 — URL normalization and sync state`

### Phase 3: Git Extraction

Parse git command output into model structs. This is the core logic.

- [ ] Step 3.1: Create `auto-etl/internal/git/discover.go`:
  - `DiscoverRepos(remotes map[string]string, explicitPaths []string) []RepoInfo` — deduplicate by resolved repo path, validate each is a git repo via `git rev-parse --show-toplevel`
  - `RepoInfo` struct: `{Path, Remote string}`
- [ ] Step 3.2: Create `auto-etl/internal/git/extract.go`:
  - `ExtractRepo(repo RepoInfo, config ExtractConfig) (*model.GitETLResult, error)` — orchestrate per-repo extraction
  - `ExtractConfig` struct: `{HostID, ETLRunID string, CollectedAt int64, Since string, SeenSHAs map[string]bool}`
  - Internal functions:
    - `observeRepo(repoPath string) (*model.GitRepository, error)` — runs `rev-parse`, `remote get-url`, `symbolic-ref`
    - `observeRefs(repoPath, repoID string) ([]model.GitRef, error)` — runs `git for-each-ref`, parses output
    - `extractCommits(repoPath, repoID string, since string) ([]model.Commit, error)` — runs `git log --all --format=...`, parses NUL-separated fields, applies `--since` if provided
    - `extractFilesAndHunks(repoPath string, commit *model.Commit) ([]model.CommitFile, []model.CommitHunk, error)` — for non-merge commits: runs `git diff-tree --raw --numstat` and `git show -p`, parses and joins output
    - `computePatchIDs(repoPath string, shas []string) (map[string]string, error)` — runs `git log -p <shas> | git patch-id --stable`, returns `sha → patch_id` map
  - Use `transform.MidTruncate()` for `message_truncated`, `diff_truncated`, `hunk_text_truncated` at 4096 chars
  - Parse trailers from commit message body into `trailers_json` (JSON object of key→value arrays)
  - Compute `hunk_hash` as SHA256 of whitespace-normalized hunk text
  - Set `year`/`month` partition fields from `author_date`
  - Filter commits against `SeenSHAs` — only extract file/hunk detail for new commits
  - Aggregate `files_changed`, `insertions`, `deletions` on commit rows from parsed file rows
- [ ] Step 3.3: Create `auto-etl/internal/git/extract_test.go`:
  - Test `observeRefs` parser with sample `for-each-ref` output
  - Test `extractCommits` parser with sample `git log` output (normal, merge, multi-parent)
  - Test `extractFilesAndHunks` parser with sample `diff-tree --raw`, `--numstat`, and unified diff output
  - Test trailer parsing (Co-Authored-By, Signed-off-by, multiple values)
  - Test hunk header parsing (`@@ -1,5 +1,7 @@`)
  - Test merge commit detection (parent_count > 1, no files/hunks)
  - Verify: `go test ./internal/git/...` passes
- [ ] Step 3.4: Verify: `cd auto-etl && go build ./...` passes
- [ ] Step 3.5: Commit: `feat(002): phase 3 — git extraction and parsing`

### Phase 4: Writer

Write five git datasets to partitioned parquet.

- [ ] Step 4.1: Create `auto-etl/internal/writer/git.go`:
  - `WriteGit(outputDir string, result *model.GitETLResult) error`
  - `git_repositories/git_repositories.parquet` — unpartitioned, read-merge-write by `repo_id`
  - `git_refs/git_refs.parquet` — unpartitioned, append-only (read existing + append new, dedupe by `id`)
  - `commits/year=YYYY/month=MM/commits.parquet` — monthly partitions, read-merge-write by commit `id`
  - `commit_files/year=YYYY/month=MM/commit_files.parquet` — monthly partitions, read-merge-write by `id`
  - `commit_hunks/year=YYYY/month=MM/commit_hunks.parquet` — monthly partitions, read-merge-write by `id`

<!-- RESOLVED(P2): Git row IDs and writer dedupe are not repo-scoped
REVIEW: The planned IDs for commits/files/hunks are based on commit SHA and per-commit indexes, while the writer dedupes by `id` only. Across multiple repos or forks, the same commit SHA can appear under different `repo_id` values; deduping by `id` alone can drop one repo's row and leave `git_refs` pointing at a commit without a matching `(repo_id, commit_id)` row. Use `(repo_id, id)` as the merge key or include `repo_id` in the row IDs for all repo-scoped git datasets.
AUTHOR: Include `repo_id` prefix in all composite IDs: commit id = `{repo_id}-{sha}`, commit_files id = `{repo_id}-{sha}-{file_index}`, commit_hunks id = `{repo_id}-{sha}-{file_index}-{hunk_index}`, git_refs id = `{repo_id}-{ref_name}-{collected_at}`. Writer dedupes by `id` which is now globally unique. Updated Step 3.2 extraction to reflect this.
-->

  - Reuse `writeParquet[T]()`, `readExistingParquet[T]()`, `partKey`, grouping helpers
  - Return early if all result slices are empty
- [ ] Step 4.2: Verify: `cd auto-etl && go build ./...` passes
- [ ] Step 4.3: Commit: `feat(002): phase 4 — git parquet writer`

### Phase 5: CLI Integration + E2E

Wire git ETL into the run command and validate end-to-end.

- [ ] Step 5.1: Modify `auto-etl/cmd/run.go`:
  - Add `"git"` to `validOnlyValues`
  - Add flag vars: `repoPathFlag []string`, `sinceFlag string`
  - Register flags: `--repo-path` (string slice), `--since` (string)
  - Update default sources in `parseOnlyFlag` to include `"git": true`
  - Add `runGitETL(hostID string, remotes map[string]string, explicitPaths []string, since string, fullRebuild bool) error`:
    - If `fullRebuild`, delete git sync-state.json before proceeding
    - Generate `etl_run_id` (UUID or timestamp-based)
    - Load git sync state from `GitSyncStatePath()`
    - Call `DiscoverRepos(remotes, explicitPaths)`
    - For each repo: call `ExtractRepo()` with config including seen SHAs
    - Call `WriteGit()` with combined results
    - Update seen-commit state, save atomically

<!-- RESOLVED(P2): Full rebuild and custom output can be inconsistent with seen state
REVIEW: `auto-etl/cmd/run.go` removes only `outputDir` for `--full`; it does not clear external sync state. With the planned always-on seen-SHA state, `autoetl run --full --only git --repo-path ...` or a run against a new `--output` directory can treat every commit as already seen, write no commit/file/hunk rows, and fail AC-1/AC-3. Add a step to reset or ignore git seen state on full rebuild, or make state output-directory-aware.
AUTHOR: Added to Step 5.1: when `--full` is passed, delete the git sync-state.json file before running git ETL (same as how `--full` deletes the output dir). This forces a full re-extraction. Also updated solution.md to document this behavior.
-->

    - Print summary: repos processed, commits indexed, files/hunks written
  - Call `runGitETL()` in the run command after session ETL (when `sources["git"]` is true)
- [ ] Step 5.2: Update `auto-etl/cmd/run_only_test.go`:
  - Add test: `--only git` is accepted
  - Add test: `--only git,sessions` is accepted
  - Add test: default (no `--only`) includes all three sources
  - Verify: `go test ./cmd/...` passes
- [ ] Step 5.3: Manual e2e validation against this repo:
  ```bash
  cd auto-etl
  go build -o .tmp/autoetl . && .tmp/autoetl run --only git --repo-path /home/vscode/src/auto-stack --output .tmp/output --since 3m
  ```
  - Verify: five dataset directories created under `.tmp/output/`
  - Verify: `duckdb -c "SELECT count(*) FROM read_parquet('.tmp/output/commits/**/*.parquet')"` returns rows
  - Verify: `duckdb -c "SELECT count(*) FROM read_parquet('.tmp/output/commit_files/**/*.parquet')"` returns rows
  - Verify: `duckdb -c "SELECT count(*) FROM read_parquet('.tmp/output/commit_hunks/**/*.parquet')"` returns rows
  - Verify: `duckdb -c "SELECT * FROM read_parquet('.tmp/output/git_repositories/*.parquet')"` shows this repo
  - Verify: `duckdb -c "SELECT count(*) FROM read_parquet('.tmp/output/git_refs/*.parquet')"` returns rows
  - Verify: merge commits have `is_merge=true` and no matching `commit_files` rows
  - Run again: verify incremental (no new rows, completes quickly)
- [ ] Step 5.4: Commit: `feat(002): phase 5 — CLI integration and e2e validation`

## Success Criteria

- [ ] `go build ./...` passes in `auto-etl/`
- [ ] `go test ./...` passes in `auto-etl/` — all existing + new tests
- [ ] `go vet ./...` clean
- [ ] `autoetl run --only git --repo-path <this-repo>` writes five parquet dataset directories
- [ ] duckdb confirms non-zero row counts in commits, commit_files, commit_hunks, git_refs, git_repositories
- [ ] Merge commits have `is_merge=true` with zero commit_files/commit_hunks rows
- [ ] Re-running with no new commits produces no new commit/file/hunk rows (incremental; git_refs appends snapshots by design)
- [ ] `--only git` runs only git ETL; default runs all three sources
- [ ] Repo with no origin remote gets indexed with path-based repo_id

## Open Questions

- (none — all resolved in requirements.md)
