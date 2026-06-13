---
hash: "0e8b3405"
id: "1b40987f"
read_when: "implementing commit-to-session linkage or reviewing the solution design for task 008"
summary: "Three-workstream design for commit-session linking: git trailer extraction, fallback parquet session matcher, and hook installation via auto-config."
title: "Solution: Task 008 — Commit-Session Link"
---

# Solution: Task 008

## Approach

Three independent workstreams that together satisfy all ACs:

### 1. Trailer extraction (AC-1, AC-4, AC-5, AC-6)

Add a `SessionID` field to the `Commit` struct. During `parseCommitLog`, after calling `parseTrailers()`, unmarshal `TrailersJSON` and extract the first `Session-Id` value. This is zero-cost — the trailers are already parsed and stored as JSON; we just read one key.

### 2. Fallback extraction (AC-2, AC-3)

A new `LinkSessionIDs` function runs as a post-processing step after `ExtractRepo` returns. It:

1. Scans the existing `messages` parquet under `~/.auto/etl/output/messages/` (if any exist)
2. Reads only rows where `bash_command` matches commit-creating patterns (`git commit`, `git merge`, `git cherry-pick`)
3. Filters rows to the current repo by matching `git_remote` against the commit's `RepoID` (both derived from normalized remote URL)
4. Applies regex `\[[\w/.-]+ ([0-9a-f]{7,})\]` to extract short SHAs from `content`
5. Builds a repo-scoped index: `short_sha → session_id`

<!-- RESOLVED(P1): Fallback key is not scoped to the repository
REVIEW: The messages parquet dataset is global, and `AgentMessage` already carries `workspace` and `git_remote` (`auto-etl/internal/model/model.go:51-52`, populated in `auto-etl/internal/transform/transform.go:376-377`). Indexing only by `short_sha` with first-match-wins can attach a commit to an unrelated repo/session if two repos share the same abbreviation or if forked repos produce overlapping output. The link step needs repo/workspace scoping or explicit duplicate-prefix ambiguity handling before setting `session_id`.
AUTHOR: Added repo scoping via `git_remote` filter (step 3). The lightweight `messageRow` struct will include `GitRemote` alongside `SessionID`, `BashCommand`, and `Content`. `LinkSessionIDs` receives the current `repoID` and filters messages to matching repos before building the index. The function signature becomes `LinkSessionIDs(commits []model.Commit, messagesDir string, repoRemote string)`.
-->

6. For each commit where `SessionID` is still empty, looks up its `ShortID` in the index

<!-- RESOLVED(P1): Fallback lookup will miss normal 7-character commit output
REVIEW: `parseCommitLog` currently sets `Commit.ShortID` to the first 8 characters of the full SHA (`auto-etl/internal/git/extract.go:427-430`), while normal git commit output is often a 7-character abbreviation and the fallback regex accepts 7+ characters. If a message contains `[branch abc1234]`, the planned `short_sha -> session_id` index will not match a commit whose `ShortID` is `abc12345`. Match captured abbreviations as prefixes of the full commit SHA, and treat ambiguous prefixes as no-match rather than guessing.
AUTHOR: Changed lookup to use full SHA prefix matching instead of exact `ShortID` match. `LinkSessionIDs` receives the full SHA (available from `Commit.ID` after stripping the `repoID-` prefix). The index maps `captured_short_sha → session_id`, and lookup checks whether any index key is a prefix of the commit's full SHA (or vice versa). If multiple index keys match, the commit is skipped (ambiguous = no match per AC-5).
-->

If no messages parquet exists (first-time ETL, or sessions haven't been processed yet), the fallback silently skips — commits get empty `session_id` per AC-5.

**Backfill**: Normal incremental ETL only links new commits. To backfill `session_id` for already-processed commits, run `autoetl --full-rebuild` which clears `SeenSHAs` and re-extracts all commits through the new linking step.

This runs as a **separate function call** after `ExtractRepo` returns, not inside it. The git extraction stays pure (no cross-dataset dependency). The caller (the `git` CLI command handler) calls `LinkSessionIDs(commits, messagesDir)` before passing results to the writer.

<!-- RESOLVED(P1): Retroactive fallback does not reach already-seen commits
REVIEW: The current git extractor returns only commits not present in `config.SeenSHAs` (`auto-etl/internal/git/extract.go:152-184`). Calling `LinkSessionIDs` only on `result.Commits` means a normal ETL run after this change will not populate `session_id` for commits already written to parquet, and `WriteGit` only rewrites commit partitions that receive incoming commits (`auto-etl/internal/writer/git.go:57-74`). The design calls the fallback retroactive, so it needs either an explicit full-rebuild/backfill requirement or a step that reads existing commit partitions, links them, and rewrites them.
AUTHOR: The existing `--full-rebuild` flag (`auto-etl/cmd/run.go:373-378`) clears `SeenSHAs` so all commits are re-extracted and re-linked. This is the intended backfill mechanism. Normal incremental runs link only new commits; a one-time `autoetl --full-rebuild` after deploying this change backfills history. No new code needed — this is a deployment instruction. Added a note to the solution clarifying this.
-->

### 3. Hook installation (AC-7, AC-8)

Create a minimal `auto-config` package with just enough for `autoconfig init --project`. The init command calls a `SetupGitHooks()` function that:

1. Resolves the git root via `git rev-parse --git-dir`
2. Checks if `.git/hooks/prepare-commit-msg` exists — if so, returns a clear error and exits non-zero
3. Writes the hook script (embedded in Go via `go:embed` from `auto-config/internal/hooks/prepare-commit-msg`) and sets it executable

The hook content is identical to the existing `hooks/prepare-commit-msg` in the repo root. We embed rather than hardcode so the source stays in one place.

## Files

```
~ auto-etl/internal/model/git.go              # add SessionID field to Commit struct
~ auto-etl/internal/git/extract.go            # extract Session-Id from TrailersJSON during parseCommitLog
+ auto-etl/internal/git/session_link.go       # LinkSessionIDs: fallback matching via messages parquet
+ auto-etl/internal/git/session_link_test.go  # unit tests for fallback matching logic
~ auto-etl/internal/git/extract_test.go       # add trailer → SessionID extraction tests
~ auto-etl/cmd/run.go                         # call LinkSessionIDs after ExtractRepo, before WriteGit
+ auto-config/go.mod                          # new module: github.com/mistakenot/auto-config
+ auto-config/cmd/autoconfig/main.go          # CLI entry point: init command with --project flag
+ auto-config/internal/hooks/install.go       # SetupGitHooks: check-exists + write hook + chmod
+ auto-config/internal/hooks/install_test.go  # unit tests: fresh install, existing hook error
+ auto-config/internal/hooks/prepare-commit-msg  # hook script (embedded via go:embed)
```

## Test Coverage

| AC   | Test Type   | File                                       |
|------|-------------|--------------------------------------------|
| AC-1 | unit        | auto-etl/internal/git/extract_test.go      |
| AC-2 | unit        | auto-etl/internal/git/session_link_test.go |
| AC-3 | unit        | auto-etl/internal/git/session_link_test.go |
| AC-4 | unit        | auto-etl/internal/git/extract_test.go      |
| AC-5 | unit        | auto-etl/internal/git/extract_test.go      |
| AC-6 | integration | auto-etl/internal/git/extract_test.go      |
| AC-7 | unit        | auto-config/internal/hooks/install_test.go |
| AC-8 | unit        | auto-config/internal/hooks/install_test.go |

### Test details

**AC-1/AC-4/AC-5** (extract_test.go): Test `parseCommitLog` with commit messages containing `Session-Id` trailer, both with and without the trailer, and with trailer + fallback-matchable content (verifying trailer wins).

**AC-2/AC-3** (session_link_test.go): Build in-memory message rows with various `bash_command` values. Verify that `LinkSessionIDs` matches only commit-creating commands and ignores `git log`, `cat`, etc. Verify correct short SHA extraction and session ID assignment.

**AC-6** (extract_test.go): Round-trip test — write commits to parquet, read back with parquet-go, assert `session_id` column is present and queryable.

**AC-7/AC-8** (install_test.go): Use `t.TempDir()` as a fake git dir. Test fresh install writes the hook with correct permissions. Test existing hook returns error with descriptive message.

## Out of Scope

- Adding `session_id` to `commit_files` or `commit_hunks` (can be joined through `commits`)
- Building a dedicated `commit_sessions` bridge table
- Hashing or obfuscating session IDs for privacy
- Other `autoconfig` commands beyond `init --project` with git-hooks setup
- Handling repos with `core.hooksPath` set to a custom directory (future enhancement)

## Rejected Alternatives

- **Fallback inside `ExtractRepo`**: Would couple the git extraction to the session ETL pipeline. Keeping it as a separate `LinkSessionIDs` call preserves the clean separation between git-only extraction and cross-dataset enrichment.
- **Separate `autoetl link` command**: A dedicated CLI command for session linking. Rejected because the fallback is cheap (scan a few parquet files) and should run automatically during normal git ETL — an extra manual step would be forgotten.
- **Hardcoded hook script as Go string constant**: Rejected in favor of `go:embed` from a real shell script file, so the hook source stays editable and testable as a standalone script.
