---
hash: "6a9ced72"
id: "573606cd"
read_when: "implementing git ETL ingestion, designing commit/file/hunk parquet schemas, or adding git history to auto-search"
summary: "Spec for extending auto-etl to index git commit history as first-class parquet datasets alongside coding agent sessions, enabling cross-referencing with session data."
title: "Git History ETL"
---

# Git History ETL

Extend auto-etl to index git commit history as a first-class data source alongside coding agent sessions. Git history provides ground truth about what code actually shipped, enabling cross-referencing with session data to measure agent effectiveness, detect codebase health patterns, and drive reflect/analysis workflows.

## Motivation

Session data tells us how agents think. Git data tells us what actually happened. The intersection — mapping thinking to outcomes — is where the real analytical leverage is.

What git gives us that sessions don't:

- **Ground truth of what shipped** — sessions show intent and iteration, git shows the final artifact. Measure the delta between "work done" and "work kept."
- **Temporal density maps** — which files/directories change most, by whom, at what cadence.
- **Churn detection** — files edited, reverted, re-edited across commits. Strong signal for "this area is poorly understood" or "this API is unstable."
- **Commit-to-session linking** — tie a commit SHA back to the agent session that produced it. Ask "which sessions produced commits that stuck vs. got reverted?"

## Data Model

The first implementation focuses on **canonical ETL facts**: clean, replayable data observed directly from local git history plus ETL collection provenance. Higher-level interpretations such as session links, outcomes, reverts, risk scores, file identity graphs, and agent effectiveness metrics should be derived downstream and recomputable later.

Five new parquet datasets provide the raw substrate:

- `git_repositories` — one row per observed repository/worktree context
- `git_refs` — one row per observed ref snapshot
- `commits` — one row per commit
- `commit_files` — one row per file touched per commit
- `commit_hunks` — one row per unified-diff hunk

<!-- UNRESOLVED(P2): Git parquet schemas omit schema_version
REVIEW: Every current auto-etl parquet row type carries `schema_version` (`AgentMessage`/`AgentSession` in `auto-etl/internal/model/model.go`, and `PullRequest`/`PRComment` in `auto-etl/internal/model/github.go`). None of the five proposed git datasets include it, so an implementer following this spec would produce datasets without the existing schema-evolution marker. Add `schema_version int32` to each git row schema and include it in the output contract.
-->

### `git_repositories` — one row per observed repo/worktree

| Field | Type | Notes |
|---|---|---|
| `repo_id` | string | Stable hash of normalized remote URL; primary join key for git datasets |
| `repo_remote` | string | Origin URL as reported locally |
| `repo_remote_normalized` | string | Normalized origin URL for stable identity |
| `repo_path` | string | Local checkout path |
| `worktree_path` | string | Worktree root path, if different from repo path |
| `default_branch_observed` | string | Best local observation, e.g. `origin/HEAD` target |
| `host_id` | string | Machine that ran the ETL |
| `first_seen_at` | int64 | Unix ms |
| `last_seen_at` | int64 | Unix ms |
| `etl_run_id` | string | ETL run that wrote or refreshed this observation |
| `collected_at` | int64 | Unix ms |

### `git_refs` — one row per observed ref snapshot

Refs are mutable, so this dataset records what the ETL observed at collection time rather than treating branch membership as an immutable commit property.

| Field | Type | Notes |
|---|---|---|
| `id` | string | `{repo_id}-{ref_name}-{collected_at}` or equivalent stable observation ID |
| `repo_id` | string | FK to `git_repositories` |
| `ref_name` | string | Full ref name, e.g. `refs/remotes/origin/main` |
| `ref_type` | string | `branch`, `remote_branch`, or `tag` |
| `commit_id` | string | SHA the ref pointed to at collection time |
| `is_default` | bool | True for observed default branch ref |
| `is_remote` | bool | True for remote refs |
| `etl_run_id` | string | ETL run that captured this snapshot |
| `collected_at` | int64 | Unix ms |

### `commits` — one row per commit

| Field | Type | Notes |
|---|---|---|
| `id` | string | Full SHA |
| `short_id` | string | 8-char prefix |
| `repo_id` | string | FK to `git_repositories` |
| `tree_sha` | string | Commit tree SHA |
| `author_name` | string | |
| `author_email` | string | |
| `author_date` | int64 | Unix ms |
| `author_date_offset` | string | Original timezone offset from git |
| `committer_name` | string | |
| `committer_email` | string | |
| `committer_date` | int64 | Unix ms |
| `committer_date_offset` | string | Original timezone offset from git |
| `message` | string | Full commit message |
| `message_truncated` | string | Mid-truncated for search |
| `is_merge` | bool | >1 parent |
| `parent_count` | int32 | Number of parents |
| `parent_shas` | string | Comma-separated |
| `files_changed` | int32 | `--stat` count |
| `insertions` | int32 | Lines added |
| `deletions` | int32 | Lines removed |
| `trailers_json` | string | Parsed git trailers such as `Co-Authored-By`, `Signed-off-by`, `Reviewed-by`, `Fixes` |
| `patch_id` | string | Stable patch identity from `git patch-id`, empty for commits without a textual patch |
| `etl_run_id` | string | ETL run that captured this row |
| `collected_at` | int64 | Unix ms |
| `year` | int32 | Partition key |
| `month` | int32 | Partition key |

`commits` intentionally does **not** store mutable or derived interpretations such as current branch, default-branch containment, session links, agent/human classification, revert status, survival duration, or outcome scores. Those belong in downstream derived datasets with `computed_at`, confidence, and evidence fields.

### `commit_files` — one row per file touched per commit

| Field | Type | Notes |
|---|---|---|
| `id` | string | `{sha}-{file_index}` |
| `commit_id` | string | FK to commits |
| `repo_id` | string | FK to `git_repositories` |
| `file_index` | int32 | Stable per-commit file order |
| `file_path` | string | Relative path |
| `change_type` | string | Git status code such as `A`/`M`/`D`/`R`/`C`/`T` |
| `old_path` | string | For renames |
| `insertions` | int32 | Per-file |
| `deletions` | int32 | Per-file |
| `old_blob_sha` | string | Blob before the commit, empty for added files |
| `new_blob_sha` | string | Blob after the commit, empty for deleted files |
| `old_mode` | string | File mode before the commit |
| `new_mode` | string | File mode after the commit |
| `is_binary` | bool | True when git reports binary content |
| `diff` | string | Full unified diff for this file |
| `diff_truncated` | string | Mid-truncated at 4096 chars for search |
| `author_date` | int64 | Denormalized for time queries |
| `etl_run_id` | string | ETL run that captured this row |
| `collected_at` | int64 | Unix ms |
| `year` | int32 | Partition key |
| `month` | int32 | Partition key |

### `commit_hunks` — one row per unified-diff hunk

Hunks preserve the line-range structure that would otherwise need to be repeatedly reparsed from full diffs. This is still raw ETL data: it is directly parsed from git's unified diff output, not a semantic interpretation.

| Field | Type | Notes |
|---|---|---|
| `id` | string | `{sha}-{file_index}-{hunk_index}` |
| `commit_id` | string | FK to commits |
| `repo_id` | string | FK to `git_repositories` |
| `file_index` | int32 | Matches `commit_files.file_index` |
| `hunk_index` | int32 | Stable per-file hunk order |
| `file_path` | string | Relative path after the commit |
| `old_path` | string | Relative path before the commit for renames |
| `old_start` | int32 | Old-file starting line from hunk header |
| `old_lines` | int32 | Old-file line count from hunk header |
| `new_start` | int32 | New-file starting line from hunk header |
| `new_lines` | int32 | New-file line count from hunk header |
| `hunk_header` | string | Full `@@ ... @@` header |
| `hunk_text` | string | Full hunk text |
| `hunk_text_truncated` | string | Mid-truncated at 4096 chars for search |
| `hunk_hash` | string | Hash of normalized hunk text for later duplicate/re-touch analysis |
| `author_date` | int64 | Denormalized for time queries |
| `etl_run_id` | string | ETL run that captured this row |
| `collected_at` | int64 | Unix ms |
| `year` | int32 | Partition key |
| `month` | int32 | Partition key |

## Design Rationale

**Mirrors the existing pattern.** `commits` is to `commit_files`/`commit_hunks` as `sessions` is to `messages`: one summary row, many detail rows. Commit time-series datasets use the same monthly partitioning strategy. Small repo/ref observation datasets can remain unpartitioned unless volume requires partitioning later. Same denormalization philosophy — put `repo_id` and dates on detail tables so filtering works without joins.

**Clean join surface.** The raw ETL datasets expose stable joins without baking in downstream interpretations:

1. **`repo_id` / normalized remote** — links git data to sessions working on the same repo
2. **`commit_id`** — links commits to files, hunks, refs, and future PR/outcome datasets
3. **`file_path` + time** — links `commit_files.file_path` to `messages.tool_file_path` for future cross-source analysis
4. **`hunk_hash` / `patch_id`** — enables later duplicate, repeated-edit, and revert analysis without reparsing history

<!-- UNRESOLVED(P2): Session join key is not actually present yet
REVIEW: Current session/message parquet only stores raw `git_remote` (`auto-etl/internal/model/model.go` and `auto-etl/internal/transform/transform.go`); it does not store `repo_remote_normalized` or `repo_id`. The spec says `repo_id` / normalized remote links git data to sessions, but it does not say where that normalized value is computed for existing session rows or how downstream tools must normalize raw remotes identically. Add a shared normalization contract/helper or explicitly require downstream derived jobs to compute `repo_id` from session `git_remote`.
-->

**`commit_files` and `commit_hunks` are where the analytical power lives.** Per-file granularity supports churn and hotspot queries. Hunk granularity preserves enough structure for later repeated-edit, review-comment, and patch-memory features while keeping ETL grounded in directly observed git output.

**Derived data stays out of canonical ETL.** Session links, file identity across renames, agent/human classification, PR joins, revert chains, outcome scores, and risk metrics should be separate downstream datasets. Those records should carry `computed_at`, `confidence`, and `evidence_json` because they are interpretations, not immutable git facts.

## ETL Source

Git is already structured. The ETL should use a small number of parseable git commands per repo and write observed facts without joining to sessions, GitHub, autodoc, or later analytical state.

**Repo/ref observation.** Populates `git_repositories` and `git_refs`:

```
git rev-parse --show-toplevel
git remote get-url origin
git symbolic-ref --quiet refs/remotes/origin/HEAD
git for-each-ref --format='%(refname)%00%(objectname)%00%(objecttype)%00' refs/heads refs/remotes refs/tags
```

The ETL normalizes the remote URL into `repo_remote_normalized`, derives `repo_id` from that normalized remote, and records all observed refs with the current `collected_at`.

**Commit metadata.** Populates `commits` rows without file-level detail:

```
git log --format='%H%x00%h%x00%T%x00%an%x00%ae%x00%aI%x00%cn%x00%ce%x00%cI%x00%P%x00%B%x00'
```

<!-- UNRESOLVED(P1): Commit walk only covers HEAD
REVIEW: This command has no `--all` or explicit ref list, so Git walks only the current `HEAD`. That conflicts with the preceding `git for-each-ref` step, which observes all local branches, remote branches, and tags. I checked this repository: `git log --format='%H'` returns 106 commits, while `git log --all --format='%H'` returns 158. Implementing the spec literally would leave `git_refs` pointing at commits absent from `commits`/`commit_files` whenever they are not reachable from the checked-out branch. Specify the intended ref set, for example `git log --all ...` or an explicit list derived from `for-each-ref`.
-->

The ISO timestamps preserve timezone offsets for `author_date_offset` and `committer_date_offset`. Commit trailers are parsed from the full body into `trailers_json`. `patch_id` is computed from the textual patch with `git patch-id --stable`; merge commits or commits without a textual patch can leave it empty.

**Per-file raw metadata and stats.** Populates `commit_files` rows except `diff`/`diff_truncated`:

```
git diff-tree --root -r -z --raw -M -C <sha>
git diff-tree --root -r -z --numstat -M -C <sha>
```

<!-- UNRESOLVED(P2): Merge commit file semantics are unspecified
REVIEW: The schema promises one `commit_files` row per file touched per commit, but these commands emit no file rows for merge commits unless a merge diff mode is chosen. I checked merge commit `a5fdb2626d7bb9f9839e8a1afa2b534f182c8125` in this repo: the documented `git diff-tree --root -r --raw --numstat <sha>` produced no output, while adding `-m` produced file rows. Decide whether merge commits intentionally have empty file/diff rows, use first-parent diffs, or use per-parent `-m` rows, and encode that in the schema so `file_index`, stats, and hunk IDs are deterministic.
-->

`--raw` provides old/new modes, old/new blob SHAs, change status, and rename/copy paths. `--numstat` provides insertions/deletions and binary markers. The ETL joins both command outputs by the per-commit file order and normalized path tuple.

Commit-level `files_changed`, `insertions`, and `deletions` are aggregated from the parsed `commit_files` rows for the same commit.

**Per-file diffs and hunks.** Populates `commit_files.diff`, `commit_files.diff_truncated`, and `commit_hunks`:

```
git show <sha> --format= -p --diff-filter=AMDRCT -M -C
```

Split the unified diff output by file header (`diff --git a/... b/...`) to get per-file diffs. Split each file diff by hunk header (`@@ ... @@`) to write `commit_hunks`. Apply MidTruncate at 4096 chars for `diff_truncated` and `hunk_text_truncated`.

**Input:** Local git repositories, discovered automatically from the session-trail remotes cache. The ETL scans `workspace` paths seen across processed sessions to find git repos.

Auto-discovered repos are **registry-gated**: a discovered workspace/remote is only indexed if it belongs to a project registered in `~/.auto/projects.json` (populated by `auto init --project`). An entry is kept when its origin remote matches a registered project's remote (`FindProjectByRemote`, after SSH↔HTTPS and credential normalization — covering worktrees and ordinary subdirectories, which resolve the enclosing repo's origin), when the user registered exactly that directory (`FindProjectByExactPath`), or when the workspace is nested under a registered project (`FindProjectByPath`, longest-prefix) and has no distinct remote of its own. A clone vendored or experimented with *inside* a registered project — a nested path with its own foreign origin — is **not** indexed, so a path-prefix match never silently pulls in a foreign repo's history. This is the same gate the GitHub PR phase applies, and it has the same strict empty-registry behavior: if the registry is empty or absent, auto-discovery indexes **nothing** and a one-line stderr hint suggests running `auto init --project`.

Users can also pass explicit paths via `--repo-path` for repos that have no session history yet. **Explicit `--repo-path` bypasses the gate** — explicitly supplied paths are an intentional user allowlist and are always indexed, registered or not. The gate only filters message-trail-derived auto-discovery.

**Output:**

- `~/.auto/etl/output/git_repositories/*.parquet`
- `~/.auto/etl/output/git_refs/*.parquet`
- `~/.auto/etl/output/commits/year=YYYY/month=MM/*.parquet`
- `~/.auto/etl/output/commit_files/year=YYYY/month=MM/*.parquet`
- `~/.auto/etl/output/commit_hunks/year=YYYY/month=MM/*.parquet`

**CLI integration:** Adds `--only git` to `autoetl run`, alongside the existing `--only sessions` and `--only github`. When run without `--only`, git history ETL runs after session ETL so it can discover repos from freshly-written session data. Supports `--since` for initial depth control (e.g. `--since 6m`); subsequent runs are incremental, tracking the last-processed SHA per repo in `~/.auto/etl/settings.json` under a `git_cursors` key.

<!-- UNRESOLVED(P1): Single repo cursor can miss branch history
REVIEW: A single last-processed SHA per repo only works for a linear one-ref import. This spec also captures all refs, and local/remote branches can advance independently or be force-pushed. If the cursor is on `main`, a later `--only git` run can skip new commits that exist only on another observed ref, leaving `git_refs` and `commits` inconsistent. The incremental state needs to be per ref, or the ETL needs a repo-level seen-commit set/reachability strategy that is explicitly compatible with multi-ref collection.
-->

## Search Indexing (auto-search)

Auto-search ingestion should initially index the raw git datasets as-is. Session linking, outcome scoring, revert chains, and risk metrics are not part of the git ETL contract.

Suggested SQLite tables + FTS5 indexes:

- **`git_repositories` table** keyed by `repo_id`
- **`git_refs` table** indexed on `(repo_id, ref_name, collected_at)` and `(repo_id, commit_id)`
- **`commits` table** with FTS5 on `(message_truncated, author_name, trailers_json)`
- **`commit_files` table** with FTS5 on `(file_path, old_path, diff_truncated)`
- **`commit_hunks` table** with FTS5 on `(file_path, hunk_header, hunk_text_truncated)`
- Composite indexes on `(repo_id, author_date)`, `(repo_id, file_path, author_date)`, and `(repo_id, hunk_hash)`

**Required auto-search changes:**

1. **Discovery** (`etlscan/discover.go`): Extend scanning to `git_repositories/`, `git_refs/`, `commits/`, `commit_files/`, and `commit_hunks/`.
2. **Parquet row structs** (`model/parquet.go`): Add row structs matching all five parquet schemas, with parquet struct tags.
3. **Schema** (`indexdb/schema.go`): Add SQLite tables, FTS5 virtual tables where useful, and sync triggers. Bump schema version.
4. **Indexer** (`indexdb/`): Add indexer cases for all five datasets.
5. **Query scope**: Add query functions for commits, files, hunks, refs, and repo metadata. Keep derived cross-source analysis out of the first ingestion pass.

<!-- UNRESOLVED(P2): Auto-search incremental cleanup steps are missing
REVIEW: Adding indexer cases is not enough for the current auto-search incremental path. `auto-search/internal/indexdb/state.go` has `DeleteRowsBySource` hard-coded to delete only `sessions` and `messages`, and `RowCounts`/`IndexResult` also only account for those datasets. Dirty git partitions would either leave stale rows behind or hit unique-key conflicts after reindexing. Add explicit plan steps for source-path deletes, result counters/CLI JSON, row-count tests, and unknown-dataset handling alongside the five new indexer cases.
-->

## Future Analysis Enabled

With raw git datasets indexed alongside `sessions` and `messages`, later derived jobs can build higher-level datasets without changing the canonical ETL format:

| Future query | Raw facts used | Derived dataset likely needed |
|---|---|---|
| Sessions that produced reverted commits | `commits`, `commit_files`, `commit_hunks`, `sessions`, `messages` | `commit_session_links`, `commit_relationships`, `commit_outcomes` |
| Agent commit survival rate | `commits.trailers_json`, `git_refs`, later commits | `commit_outcomes`, `agent_commit_classifications` |
| Files the agent reads most but never commits to | `messages.tool_file_path`, `commit_files.file_path` | Optional materialized analysis view |
| Hot files this week | `commit_files` | None |
| Docs stale relative to code churn | `commit_files`, autodoc metadata | Optional doc freshness/churn view |
| Average session-to-commit latency | `sessions`, `messages`, `commits` | `commit_session_links` |
| Wasted work detection | `git_refs`, `commits`, `sessions`, `messages` | `commit_session_links`, `commit_outcomes` |
| Cherry-pick / duplicate change detection | `commits.patch_id`, `commit_hunks.hunk_hash` | `commit_relationships` |
| Same fix attempted repeatedly | `commit_hunks.hunk_hash`, `commit_files.file_path` | `commit_relationships` or repeated-edit view |
| PR-to-session bridge | `commits`, GitHub PR parquet, sessions | `commit_session_links`, PR join table |
| Shipped vs experimental | `git_refs`, `commits` | `commit_outcomes` |

The important design boundary: these are **enabled by** git ETL, not implemented inside it.

## Future Derived Dataset Sketches

These datasets are intentionally postponed. They should be computed from the raw git/session/GitHub/autodoc data and should carry `computed_at`, `confidence`, and `evidence_json` where inference is involved.

### `commit_session_links`

| Field | Notes |
|---|---|
| `id` | Stable link ID |
| `commit_id` | FK to `commits` |
| `session_id` | FK to `sessions` |
| `repo_id` | Shared repo identity |
| `confidence` | 0.0-1.0 |
| `rank` | Candidate rank for this commit |
| `link_type` | `strong`, `probable`, or `weak` |
| `evidence_json` | Timestamp delta, workspace match, repo match, branch/ref evidence, git command evidence, message match, touched-file overlap |
| `computed_at` | Unix ms |

Candidate scoring can use normalized remote/repo ID match, workspace path match, timestamp proximity, session-visible `git commit` commands, commit message text, branch/ref evidence from messages, and touched-file overlap.

### Other postponed derived datasets

- **`commit_relationships`**: parent, revert, cherry-pick, duplicate patch, fixes, and follows relationships.
- **`commit_outcomes`**: default-branch containment, first-seen-on-default timestamp, reverted-by commit, survival windows, and later-touch counts.
- **`file_identities`**: rename-aware file identity graph so churn and file memory survive path moves.
- **Risk/reviewability views**: file categories, generated-file detection, test/docs/config/schema/migration flags, hunk size buckets, and hotspot-aware risk scores.

## Open Questions

- How should `repo_id` behave for repos with no `origin` remote: hash the absolute repo path, another remote, or an explicit local-repo identity?
- Should `git_refs` be append-only snapshots, or should the latest observation replace prior snapshots for the same ref? Append-only is more powerful; latest-only is smaller.
- Should hunk text be omitted for binary files and very large generated diffs by default, or should ETL always preserve every textual hunk and rely on downstream truncation/indexing choices?
