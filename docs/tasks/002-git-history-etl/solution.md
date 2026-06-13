---
hash: "6b9c37f4"
id: "4f5af672"
read_when: "implementing git history ETL or designing a new incremental ETL source in auto-etl"
summary: "Design for ingesting git repository history into auto-etl parquet: five row structs, git shell extraction, incremental sync-state, repo discovery, URL normalization, and CLI wiring."
title: "Solution: Task 002 — Git History ETL"
---

# Solution: Task 002 — Git History ETL

## Approach

1. **Model layer**: Define five parquet row structs (`GitRepository`, `GitRef`, `Commit`, `CommitFile`, `CommitHunk`) and a `GitETLResult` container in a new model file, following the existing `AgentMessage`/`PullRequest` pattern with `parquet:""` struct tags and dictionary encoding for low-cardinality fields.

2. **Git extraction package** (`internal/git/`): New package that shells out to git, parses structured output, and returns model structs. Per repo, runs a small number of git commands:
   - **Repo/ref observation**: `rev-parse`, `remote get-url`, `symbolic-ref`, `for-each-ref` (4 commands)
   - **Commit metadata**: `git log --all --format='%H%x00...'` (1 command, all commits)
   - **Per-commit file stats and diffs**: `git diff-tree` and `git show` per new non-merge commit (2 commands × N new commits, skipping merge commits)
   - **Patch IDs**: `git log --all -p | git patch-id --stable` (1 piped command for all commits)

3. **Incremental state**: Track a set of already-seen commit SHAs per `repo_id` in `~/.auto/etl/git/sync-state.json` (separate file, matching the GitHub ETL pattern at `~/.auto/etl/github/sync-state.json`). On each run, `git log --all --format='%H'` returns all reachable SHAs; client-side filtering against the seen set determines which commits need detail extraction. The `--since` flag limits the initial `git log` depth on first run only. When `--full` is passed, the sync state is cleared to force a full re-extraction.

<!-- RESOLVED(P1): Incremental state location is inconsistent across docs
REVIEW: Requirements and this solution store `git_cursors` in `~/.auto/etl/settings.json`, while `context.md` and Phase 2 of `plan.md` say Git ETL should use `~/.auto/etl/git/sync-state.json`. I checked the current code: `settings.json` is modeled by `etlSettings` with only `Remotes`, while GitHub incremental state uses a separate `~/.auto/etl/github/sync-state.json`. Pick one storage location/schema and align requirements, solution, context, and plan before implementation.
AUTHOR: Aligned all docs to `~/.auto/etl/git/sync-state.json`, matching the GitHub ETL pattern. Requirements table updated. Also added --full clearing sync state.
-->

4. **Repo discovery**: Reuse the existing `remotes` cache (workspace → git remote map) built by session ETL. Iterate workspace paths, validate each is a git repo via `rev-parse`. When `--repo-path` is provided, add those paths directly. No parquet reading needed — the cache is already populated.

5. **URL normalization**: Normalize `origin` remote URLs by stripping `.git` suffix, lowercasing host, converting SSH `git@host:owner/repo` to `https://host/owner/repo` form. `repo_id` = SHA256 of normalized URL (or SHA256 of absolute path for repos with no remote).

6. **Writer**: New `WriteGit()` function. `commits`, `commit_files`, `commit_hunks` use monthly partitions (`year=YYYY/month=MM/`). `git_repositories` and `git_refs` are unpartitioned (single file, read-merge-write per run since refs are append-only snapshots and repo rows are upserted). Uses the existing `writeParquet[T]` and `readExistingParquet[T]` generics.

7. **CLI wiring**: Add `"git"` to `validOnlyValues` in `cmd/run.go`. Add `--repo-path` (string slice) and `--since` (string, e.g. `6m`) flags. New `runGitETL()` function called after session ETL (so remotes cache is fresh). When `--only git` is passed, git ETL runs alone.

## Files

```
+ internal/model/git.go              # GitRepository, GitRef, Commit, CommitFile, CommitHunk, GitETLResult structs
+ internal/git/extract.go            # GitExtractor: runs git commands per repo, returns GitETLResult
+ internal/git/parse.go              # Parsers for git log, diff-tree, show, for-each-ref output
+ internal/git/normalize.go          # NormalizeRemoteURL(), ComputeRepoID()
+ internal/git/discover.go           # DiscoverRepos() from remotes cache + --repo-path
+ internal/git/state.go              # SeenCommits: load/save per-repo SHA sets from settings.json
+ internal/git/extract_test.go       # Unit tests for parsers and normalization
+ internal/writer/git.go             # WriteGit(): partition and write all five datasets
~ cmd/run.go                         # Add "git" to validOnlyValues, --repo-path, --since flags, runGitETL()
```

## Key Design Details

**Commit metadata parsing**: Use NUL (`%x00`) as field separator in `git log --format` for unambiguous parsing. The commit message body (which can contain newlines) is the last field, terminated by a record separator (`%x00%x00` double-NUL between commits). Trailers are parsed from the message body using `git interpret-trailers --parse` or manual key-value extraction.

**File metadata joining**: `git diff-tree --raw` provides change type, modes, and blob SHAs. `git diff-tree --numstat` provides insertions/deletions. Both are ordered by file path within a commit, so they're joined by position after sorting.

**Hunk extraction**: Parse unified diff output by splitting on `diff --git` headers (per-file) then on `@@ ... @@` headers (per-hunk). `hunk_hash` = SHA256 of whitespace-normalized hunk text for later duplicate analysis.

**Merge commits**: Detected by `parent_count > 1`. Get a `commits` row with `is_merge=true` but no `commit_files` or `commit_hunks` rows. Commit-level `files_changed`, `insertions`, `deletions` are set to 0.

**MidTruncate reuse**: Import the existing `MidTruncate` function from `internal/transform` for `message_truncated`, `diff_truncated`, `hunk_text_truncated` fields. If it's unexported, export it or extract to a shared internal package.

**Seen-commit set storage**: JSON file at `~/.auto/etl/git/sync-state.json`: `{"schema_version": 1, "repos": {"<repo_id>": {"seen_shas": ["sha1", "sha2", ...]}}}`. For repos with 10k+ commits, this list stays manageable (~400KB for 10k 40-char SHAs). Loaded into a `map[string]bool` at runtime for O(1) lookups.

## Test Coverage

| AC  | Test Type   | File                           |
|-----|-------------|--------------------------------|
| AC-1 | e2e        | `e2e_test.go` (extend existing) |
| AC-2 | unit       | `internal/git/extract_test.go` |
| AC-3 | unit + e2e | `internal/git/extract_test.go`, `e2e_test.go` |
| AC-4 | unit       | `internal/git/state_test.go`   |
| AC-5 | unit       | `internal/git/discover_test.go` |
| AC-6 | unit       | `cmd/run_only_test.go` (extend) |
| AC-7 | unit       | `internal/git/normalize_test.go` |

## Out of Scope

- Auto-search indexing of git datasets (separate future task)
- Derived datasets: `commit_session_links`, `commit_outcomes`, `commit_relationships`, `file_identities`
- Agent/human classification, revert detection, risk scoring
- Session-to-commit join key normalization in existing session parquet
- GitHub PR cross-referencing
- Performance optimization for repos with 100k+ commits (batched `git log` with combined output)
- Concurrent multi-repo extraction (sequential per repo is fine for v1)

## Rejected Alternatives

- **Read session parquet for repo discovery**: More correct but adds parquet-reading dependency to the git extraction path; the remotes cache is already built by session ETL and contains the same workspace→remote mapping.
- **Per-ref cursors instead of seen-commit set**: More complex state management with no benefit when using `--all` to walk all reachable commits.
- **Batched `git log --raw --numstat -p` in one pass**: Fewer git commands but significantly harder to parse (interleaved raw/numstat/diff sections). Per-commit commands are simpler and fast enough for typical repo sizes.
- **Store seen SHAs in a separate file or bloom filter**: Overhead not justified — JSON array in settings.json is simple and sufficient for typical commit counts.
