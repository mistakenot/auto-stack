---
hash: "b2b4c658"
id: "51db0d59"
read_when: "implementing or reviewing git history ETL requirements and acceptance criteria"
summary: "Requirements and acceptance criteria for extracting git commit history into five parquet datasets (git_repositories, git_refs, commits, commit_files, commit_hunks) via auto-etl."
title: "Requirements: Task 002 — Git History ETL"
---

# Task 002: Git History ETL

## Problem

auto-etl indexes coding agent sessions but has no visibility into what code actually shipped. Without git history, there's no ground truth to cross-reference against session data — no way to measure agent effectiveness, detect churn, or link sessions to outcomes.

## Goals

- Extract git commit history from local repositories into five new parquet datasets: `git_repositories`, `git_refs`, `commits`, `commit_files`, `commit_hunks`
- Follow the existing ETL pattern: parse structured git output, transform into canonical parquet rows, write partitioned output
- Integrate with the existing `autoetl run` CLI via `--only git`
- Support incremental runs so subsequent ETL passes only process new commits
- Discover repos automatically from session parquet workspace paths, with `--repo-path` override for repos without session history

## Spec Reference

Full data model, git commands, and design rationale: [`auto-etl/docs/git-history-etl.md`](../../../auto-etl/docs/git-history-etl.md)

## Resolved Decisions

These were unresolved in the spec and are now decided:

| Decision | Resolution |
|---|---|
| Commit walk scope | Use `git log --all` to walk all reachable commits, keeping `git_refs` and `commits` consistent |
| Incremental cursors | Per-repo seen-commit set in `~/.auto/etl/git/sync-state.json` (separate file, matching GitHub ETL pattern) |
| `schema_version` | Add `schema_version int32` to all five git row schemas, consistent with existing datasets |
| Merge commit files | Skip `commit_files`/`commit_hunks` rows for merge commits; `commits` row gets `is_merge=true` |
| `repo_id` without remote | Hash the absolute repo path as fallback when no `origin` remote exists |
| `git_refs` storage model | Append-only snapshots (one observation per ref per ETL run) |
| Binary/large hunk text | Always preserve full hunk text; `hunk_text_truncated` uses existing mid-truncation at 4096 chars |

## Out of Scope

- Auto-search indexing of git datasets (separate future task)
- Derived/analytical datasets: `commit_session_links`, `commit_outcomes`, `commit_relationships`, `file_identities`
- Agent/human classification, revert detection, risk scoring
- Session-to-commit join key normalization in existing session parquet
- GitHub PR cross-referencing

## Acceptance Criteria

**AC-1**: Five new parquet datasets written
- Given: a local git repository with commit history
- When: `autoetl run --only git --repo-path /path/to/repo`
- Then: five parquet dataset directories are created under `~/.auto/etl/output/` (`git_repositories/`, `git_refs/`, `commits/year=YYYY/month=MM/`, `commit_files/year=YYYY/month=MM/`, `commit_hunks/year=YYYY/month=MM/`) with rows matching the schemas in the spec

**AC-2**: Schema matches spec with resolved decisions
- Given: any written parquet file
- Then: all fields from the spec tables are present, `schema_version` is included on every row type, merge commits have `is_merge=true` and zero `commit_files`/`commit_hunks` rows

**AC-3**: All refs and all reachable commits are captured
- Given: a repo with multiple branches and tags
- When: ETL runs
- Then: `git_refs` contains one row per observed ref, `commits` contains every commit reachable from any ref (via `--all`), and every `git_refs.commit_id` exists in `commits`

**AC-4**: Incremental runs skip already-processed commits
- Given: an ETL run has already processed a repo
- When: `autoetl run --only git` runs again with no new commits
- Then: no new `commits`, `commit_files`, or `commit_hunks` rows are written, and the run completes quickly. `git_refs` always appends new snapshot rows (by design — append-only ref observations). `git_repositories` upserts `last_seen_at`.

<!-- RESOLVED(P1): Incremental AC conflicts with append-only refs
REVIEW: This cannot be satisfied together with the resolved decision above that `git_refs` is append-only with one observation per ref per ETL run. The solution and plan also say `git_refs` is read-existing-plus-append on every run. Either AC-4 should scope "no new rows" to `commits`/`commit_files`/`commit_hunks`, or the append-only ref snapshot decision should change.
AUTHOR: Scoped AC-4 to commits/commit_files/commit_hunks only. git_refs always appends snapshot rows and git_repositories upserts last_seen_at — both are expected behavior on incremental runs.
-->

**AC-5**: Repo discovery from remotes cache
- Given: session ETL has run (in this invocation or a prior one), populating the remotes cache in `~/.auto/etl/settings.json`
- When: `autoetl run` (no `--only`) runs
- Then: git ETL discovers repos from the remotes cache workspace paths and indexes their history. When no `--only` is specified, session ETL runs first, ensuring the cache is fresh.

<!-- RESOLVED(P1): Discovery AC requires parquet but solution only reads cache
REVIEW: This acceptance criterion says existing session parquet is the source of workspace paths, but `solution.md` rejects parquet reading and uses only the `remotes` cache. I checked `auto-etl/cmd/run.go`: `etlSettings` currently persists only `Remotes map[string]string` in `~/.auto/etl/settings.json`, and that cache may be empty even when session parquet already exists. The plan needs either a parquet workspace scan or the AC must be rewritten to require a populated remotes cache/fresh session ETL in the same run.
AUTHOR: Rewrote AC-5 to specify the remotes cache as the discovery source (not session parquet). Default `autoetl run` runs session ETL first, which populates the cache. `--only git` alone requires either a prior session ETL run or explicit `--repo-path`.
-->

**AC-6**: CLI integration
- Given: the `autoetl run` command
- When: `--only git` is passed
- Then: only git ETL runs. When no `--only` flag is passed, git ETL runs after session ETL. `--since` controls initial history depth (e.g. `--since 6m`).

**AC-7**: Repos without origin remote are handled
- Given: a local-only repo with no `origin` remote
- When: ETL runs with `--repo-path` pointing to it
- Then: `repo_id` is derived from a hash of the absolute repo path, and the repo is indexed normally
