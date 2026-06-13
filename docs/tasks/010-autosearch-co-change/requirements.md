---
hash: "38c5490d"
id: "b25bc182"
read_when: "implementing or reviewing the autosearch co-change query feature"
summary: "Requirements for the auto-search co-change command: lift-weighted confidence scoring, time decay, in-process SQLite engine, and AC-1 through AC-20."
title: "Task 010: Autosearch Co-Change Query Requirements"
---

# Task 010: Autosearch Co-Change Query

<!-- REJECTED(P1): Missing planning documents
REVIEW: I checked `docs/tasks/010-autosearch-co-change/` and it currently contains only `requirements.md`. The review-task workflow expects `solution.md`, `context.md`, and `plan.md` as well, so this review cannot verify implementation file paths, current-code snippets, phase ordering, or test commands. Add those docs before treating this task as ready for execution review.
AUTHOR: This task is currently in the requirements stage of the task-planning pipeline (new-task → new-solution → new-plan → request-codex-review at each stage). `solution.md`, `context.md`, and `plan.md` are intentionally absent — they get created by `/new-solution` and `/new-plan` after requirements are reviewed and approved. The review at this stage is scoped to whether the requirements themselves are sound, which the rest of this review correctly performs. The missing docs are not a defect of this task; they are the next stage.
-->

## Problem

auto-etl indexes git history into parquet (`commits`, `commit_files`, with `session_id` linkage from task 008), but auto-search has no commands that read it. There is no way to ask "given file X, which other files historically change with it?" — a question that is both a fast onboarding heuristic for new contributors and a refactor-safety signal (touching X tends to drag Y, Z along, so review them too).

## Why This Matters

Temporal coupling — files that co-change in version control — is a well-studied proxy for architectural coupling that is invisible to static import-graph analysis (auto-graph). Two files with no import relationship that always ship together usually share an implicit contract: a schema, a protocol, a feature flag. Surfacing that contract:

- Speeds up onboarding ("if I'm editing the API handler, what else?")
- Catches forgotten changes in code review
- Highlights candidates for refactor (high coupling, no shared dependency = hidden contract)
- Bridges git data into auto-search session history via `session_id`, turning "files that change together" into "sessions where they changed together"

This is the first integration of auto-etl git data into auto-search and establishes the pattern for future commands.

## Goals

- Add a new `autosearch co-change <path>` command that reads `commits` and `commit_files` parquet under `~/.auto/etl/output` and returns files temporally coupled to the input file
- Use directional confidence weighted by lift, with per-commit size penalty and exponential time decay, as the primary score
- Output JSON by default: a metadata header summarising the input file's history plus a ranked list of related files with per-file stats
- Follow renames so a file's full lineage contributes to coupling
- Resolve the repo automatically from the input path's git toplevel (single repo per query)
- Surface `top_sessions` per related file so callers can pivot into `autosearch session get`
- Execute in-process with no external dependency — read parquet via `parquet-go`, aggregate in an ephemeral in-memory pure-Go SQLite (`modernc.org/sqlite`), and score in Go; auto-search stays a self-contained pure-Go binary (this supersedes the original `duckdb`-CLI plan — see solution.md "Engine decision")

## Acceptance Criteria

**AC-1**: Command exists and reads git parquet
- Given: auto-etl has produced `commits` and `commit_files` parquet under `~/.auto/etl/output`
- When: user runs `autosearch co-change <path/to/file>` from anywhere inside a git repo (whose origin remote matches an indexed repo), or passes `--repo-id <id>` explicitly
- Then: the command resolves the repo via the input path's git toplevel + normalised origin remote (or the explicit `--repo-id`), loads only that repo's commits, and prints JSON to stdout

**AC-2**: Primary score formula
- Given: a candidate file B has co-occurred with input file A in some commits
- When: scores are computed
- Then: `score = confidence(A→B) * log(1 + lift)`, where each commit's contribution to `co_commits`, `commits(A)`, and `commits(B)` is multiplied by `1 / log(1 + files_in_commit)` (large-commit penalty) and by `exp(-Δt / τ)` with τ = 90 days (time decay)

**AC-3**: Filters and thresholds
- Given: candidate files
- When: the command ranks results
- Then: it excludes (a) candidates with raw `co_commits < 3`, (b) commits with `files_in_commit > 50` are dropped entirely before scoring, (c) if `commits(A) < 5` the command returns a metadata-only payload with a `"warning": "insufficient history"` field and an empty related list

**AC-4**: Per-related-file output schema
- For each related file the JSON object contains: `path` (current path), `score` (float), `co_commits` (int, raw), `confidence_a_to_b` (float), `confidence_b_to_a` (float), `lift` (float), `last_co_change` (ISO date), `top_authors` (array of `{name, count}`, up to 5), `top_sessions` (array of session_id strings, up to 5, in recency order), `sample_commits` (array of `{sha, date, subject}`, up to 3, most recent)

**AC-5**: Metadata header schema
- The JSON output's top-level `metadata` block contains: `file` (input path as given), `resolved_path` (path relative to repo root), `exists_in_workspace` (bool), `language` (inferred from extension), `repo` (origin remote URL or repo id), `total_commits` (commits touching A after filters), `first_touched` and `last_touched` (ISO dates), `top_authors` (up to 5 with counts), `top_sessions` (up to 5 session IDs in recency order), `avg_files_per_commit` (float, mean cohort size when A was touched), `renamed_from` (array of `{path, until_date}` if A had prior names), `ref_tips_at_touched_commits` (array of `{ref_name, ref_type, is_default}` — branches/tags whose ref tip is one of the commits touching A; this is a ref-tip intersection and intentionally NOT a full "contains" query — see note), `related_files_found` (int, count above threshold), `params_used` (object: `{decay_tau_days, large_commit_cutoff, min_co_commits, min_commits_a, limit}`).
- Note on `ref_tips_at_touched_commits`: this is derivable from current parquet, but the join key is NOT symmetric. Per `auto-etl/internal/git/extract.go`, `commits.id` (and `commit_files.commit_id`) are `<repoID>-<sha>`, whereas `git_refs.commit_id` is the **raw** `<sha>`. The query MUST bridge the two by stripping the `<repoID>-` prefix from `commits.id` — i.e. join `git_refs.commit_id = substring(commits.id, length(repoID) + 2)` (or equivalently `commits.id = repoID || '-' || git_refs.commit_id`). A naive `ON commit_id` join matches nothing. Full "which branches CONTAIN this commit" would require either a local `git for-each-ref --contains` shell-out (out of scope for v1; couples this command to a working tree) or extending ETL to materialise commit→ref membership (also out of scope). The narrower ref-tip field is honest about what current data supports.

<!-- RESOLVED(P1): Ref-tip join key does not match production parquet
REVIEW: I checked `auto-etl/internal/git/extract.go`: `parseCommitLog` writes `commits.id` as `<repoID>-<sha>`, and `commit_files.commit_id` uses that prefixed value, while `parseForEachRef` writes `git_refs.commit_id` as the raw `%(objectname)` SHA. A `commits` / `git_refs` join "ON commit_id" will produce no matches unless the query strips/adds the repo prefix or ETL normalizes `git_refs.commit_id`. AC-5 and the solution need to specify the exact join key before `ref_tips_at_touched_commits` is implementable.
AUTHOR: Verified against extract.go (commit_files.CommitID = commit.ID = repoID+"-"+sha at line 239; git_refs.CommitID = raw objectname at line 324/349). Updated the AC-5 note to specify the exact join: strip the `<repoID>-` prefix from `commits.id` and match against the raw `git_refs.commit_id` (`git_refs.commit_id = substring(commits.id, length(repoID)+2)`), explicitly stating that a naive `ON commit_id` join matches nothing. Solution step 3/5c references this when populating the `refs` table.
-->

<!-- RESOLVED(P2): `branches_touched` is not derivable from current parquet
REVIEW: Current `GitRef` rows store only ref tips (`id`, `repo_id`, `ref_name`, `ref_type`, `commit_id`, `is_default`, `is_remote`, plus provenance fields in `auto-etl/internal/model/git.go`). A DuckDB-only query over `commits`, `commit_files`, and `git_refs` can tell when a touched commit is exactly a ref tip, but not which observed branches contain that commit. Define `branches_touched` as ref tips only, allow an additional local git containment query, or extend ETL data to record commit-to-ref membership.
AUTHOR: Correct. Renamed the field to `ref_tips_at_touched_commits` and redefined it as the ref-tip intersection only (`commits ⨝ git_refs ON commit_id`), with a structured note in AC-5 explicitly distinguishing this from a full containment query and recording why broader semantics are out of scope for v1. The narrower definition is fully derivable from the current parquet schema.
-->

**AC-6**: Rename following
- Given: the input file was renamed at one or more points in its history
- When: the command processes commits
- Then: it follows the rename chain via `commit_files.change_type = 'R'` (or equivalent rename detection in the parquet schema), and historical paths contribute to all scores. The metadata `renamed_from` field lists prior paths.

**AC-7**: Time decay default with flag overrides
- Given: no flag override
- When: scores are computed
- Then: exponential time decay with τ = 90 days is applied. A `--no-decay` flag disables it; `--decay-tau <duration>` (e.g. `30d`, `26w`) overrides τ. Duration is parsed by the shared `search.ParseDurationMs` (units `m|h|d|w`); there is no `y` unit.

**AC-8**: Result ordering and limit
- Given: a non-empty result set
- When: results are emitted
- Then: related files are ordered by `score` descending, limited to top 50 by default. `--limit N` overrides the cap (0 means no cap).

**AC-9**: Unknown file handling
- Given: an input path that does not appear in the repo's git history
- When: the command runs
- Then: it returns a metadata-only payload with `total_commits: 0`, `related_files_found: 0`, exits 0 (not an error). The path is still resolved against the repo even if untracked.

**AC-10**: Invalid input fails fast
- Given: one of — the input path is outside any git repo; the repo has no origin remote and no `--repo-id` was given; the resolved repo has no match in `git_repositories`; or the repo's parquet data is missing
- When: the command runs
- Then: it exits non-zero with a clear stderr message naming which condition failed and the remediation (`run autoetl run --only git` for missing data; `cd` into a repo for the toplevel case; pass `--repo-id <id>` for the no-origin / no-match cases). stdout remains empty / parseable.

<!-- RESOLVED(P2): Remediation uses the wrong binary name
REVIEW: The ETL command is `autoetl`, not `auto-etl`: the root command uses `Use: "autoetl"` and the repo `Makefile` builds `auto-etl` into `bin/autoetl`. AC-17 repeats the hyphenated command. Update the remediation/examples to use the executable command users can actually run.
AUTHOR: Verified — `auto-etl/cmd/root.go` uses `Use: "autoetl"` and `Makefile` builds the binary as `bin/autoetl`. Fixed AC-10's remediation hint and AC-17's example command to use `autoetl`. The hyphenated `auto-etl` is retained only where it refers to the project/directory name, not the executable.
-->

**AC-11**: ~~Text output mode~~ — **REMOVED** (superseded by solution.md). auto-search is JSON-only; no `--format` flag is added. AC number retained to keep cross-references stable.

**AC-12**: Execution engine (in-process, no external dependency)
- Given: the co-change command runs
- When: it queries the parquet datasets
- Then: it reads the target repo's column-projected parquet via `parquet-go`, loads the rows into an ephemeral in-memory SQLite database (`modernc.org/sqlite`, pure-Go, no cgo), performs the joins / grouping / top-N selection in SQL, and computes the final scalar scores (confidence, lift, decay weighting) in Go. There is NO shell-out and NO external runtime binary. User-provided values (file paths, durations) MUST be passed as Go arguments or bound table data — never string-concatenated into SQL — to prevent injection. (Supersedes the original duckdb-CLI design; see solution.md "Engine decision".)

**AC-13**: ~~Doctor checks DuckDB availability~~ — **REMOVED** (superseded by solution.md). No external dependency exists to check, and auto-search has no `doctor` command. Missing-parquet handling remains covered by AC-10. AC number retained.

**AC-14**: ~~Clear runtime failure when DuckDB is missing~~ — **REMOVED** (superseded by solution.md). No external runtime dependency. AC number retained.

**AC-15**: Help and quickstart
- `autosearch co-change --help` shows full usage with all flags
- `autosearch quickstart` includes a co-change example
- (Note: auto-search has no `docs` subcommand today — the earlier "`autosearch docs`" bullet was dropped to match the actual CLI surface. If a `docs` command is added later, co-change should be listed there too, but adding one is out of scope for this task.)

**AC-16**: Checked-in auto-stack snapshot fixture (regenerated, not sliced)
- Given: a developer runs the auto-search test suite
- When: integration / conformance tests need a real-shaped parquet dataset
- Then: a slim snapshot under `auto-search/testdata/fixtures/auto-stack-snapshot/` provides parquet for `commits`, `commit_files`, `git_repositories`, and `git_refs`, derived by **regenerating** ETL output from this repo's own git history (NOT by slicing from `~/.auto/etl/output` or any other ambient local dataset). All four datasets are **column-projected** using the actual production column names from `auto-etl/internal/model/git.go`. Retained columns MUST keep their production names and types so production code paths can read the fixture without conditional branches; volatile / host-specific / provenance columns are dropped to keep regen deterministic:
  - `commits/` — retain: `id`, `short_id`, `repo_id`, `tree_sha`, `author_name`, `author_email`, `author_date`, `author_date_offset`, `committer_name`, `committer_email`, `committer_date`, `committer_date_offset`, `message_truncated`, `is_merge`, `parent_count`, `parent_shas`, `files_changed`, `insertions`, `deletions`, `session_id`, `patch_id`, `year`, `month`, `schema_version`. Drop: `message` (full text — privacy / size), `trailers_json`, `etl_run_id`, `collected_at` (volatile).
  - `commit_files/` — retain: `commit_id`, `repo_id`, `file_index`, `file_path`, `change_type`, `old_path`, `insertions`, `deletions`, `is_binary`, `author_date`, `year`, `month`, `schema_version`. Drop: `id`, `diff`, `diff_truncated`, `old_blob_sha`, `new_blob_sha`, `old_mode`, `new_mode`, `etl_run_id`, `collected_at`.
  - `git_repositories/` — retain: `repo_id`, `repo_remote`, `repo_remote_normalized`, `default_branch_observed`, `schema_version`. Drop: `repo_path`, `worktree_path`, `host_id`, `first_seen_at`, `last_seen_at`, `etl_run_id`, `collected_at` (all volatile / host-specific).
  - `git_refs/` — retain: `repo_id`, `ref_name`, `ref_type`, `commit_id`, `is_default`, `is_remote`, `schema_version`. Drop: `id` (production `id` derivation is non-deterministic across regens, per Codex review), `etl_run_id`, `collected_at`.
  - Total checked-in size MUST be < 1 MB. A sidecar `SHA.txt` records the source commit hash the snapshot was built from. Any column the co-change command reads MUST appear in the retain list above; the command MUST NOT depend on any dropped column.

<!-- RESOLVED(P1): Fixture column list does not match production schema
REVIEW: The projected fixture columns conflict with the actual parquet tags in `auto-etl/internal/model/git.go`. `Commit` has `id`, `short_id`, `message`, and `message_truncated`; it does not have `sha`, `short_sha`, or `subject`. `CommitFile` has `file_path`, `insertions`, and `deletions`; it does not have `path` or `additions`. As written, either fixture generation will fail or production query code will need fixture-only branches, which contradicts the "same column names" requirement.
AUTHOR: Verified against `auto-etl/internal/model/git.go`. Rewrote AC-16's column lists for `commits/` and `commit_files/` to use the actual production parquet tag names (`id`, `short_id`, `file_path`, `insertions`, `message_truncated`, etc.), and added an explicit constraint that retained columns keep their production names so production code can read the fixture unmodified. The previous hypothetical names (`sha`, `short_sha`, `subject`, `path`, `additions`) are gone.
-->

<!-- RESOLVED(P1): Full repo/ref fixtures conflict with deterministic regeneration
REVIEW: AC-16 keeps `git_repositories` and `git_refs` full, but current rows include volatile/local fields such as `repo_path`, `worktree_path`, `host_id`, `first_seen_at`, `last_seen_at`, `etl_run_id`, and `collected_at`; `git_refs.id` also includes `collected_at`. AC-17 requires byte-identical output when rerun with no git-history changes. Those requirements are incompatible unless the fixture projection drops or normalizes volatile fields, or the ETL exposes a fixture mode with fixed provenance values.
AUTHOR: Resolved by switching `git_repositories/` and `git_refs/` from "full" to explicit column projections that drop every volatile/host/provenance field (`repo_path`, `worktree_path`, `host_id`, `first_seen_at`, `last_seen_at`, `etl_run_id`, `collected_at`, plus `git_refs.id` because its production derivation is non-deterministic). The retained columns are stable across regens, satisfying AC-17's byte-identical requirement without needing a fixture mode in the ETL itself.
-->

**AC-17**: Fixture regen target
- Given: the parquet schema changed, or a developer wants to refresh the snapshot
- When: they run `make fixtures` from the auto-search module root (or repo root, per project convention)
- Then: the target (a) invokes the `autoetl` binary against this repo only, **under an isolated `HOME`** so the git sync-state is empty and the extraction is always complete and deterministic — concretely `HOME=<tmp_home> autoetl run --repo-path . --output <tmp_out> --only git` where `<tmp_home>` is a fresh temp dir (so `GitSyncStatePath()` → `<tmp_home>/.auto/etl/git/sync-state.json`, which does not exist → no SHAs filtered). The developer's real `~/.auto` state MUST NOT be read or mutated (so plain `--full` against the real HOME is forbidden — it deletes the user's global sync-state, per `auto-etl/cmd/run.go:375`). (b) A Go `parquet-go` fixture builder reads the temp output, selects the projected columns, sorts by a stable key, and writes `auto-search/testdata/fixtures/auto-stack-snapshot/<dataset>/<dataset>.parquet`. (c) Updates `SHA.txt` with `git rev-parse HEAD`. (d) Deletes both temp dirs. Re-running with no underlying git-history changes produces byte-identical output (deterministic — empty sync-state guarantees full extraction, sort key pinned, no embedded timestamps in non-data fields).

<!-- RESOLVED(P1): Fixture regen uses ambient git sync state
REVIEW: The example `autoetl run --repo-path . --output <tmp> --only git` is not deterministic on its own. I checked `auto-etl/cmd/run.go`: `runGitETL` loads `gitextract.GitSyncStatePath()` from `~/.auto/etl/git/sync-state.json`, passes `repoState.SeenSHAs` into `ExtractRepo`, and `auto-etl/internal/git/extract.go` filters those SHAs out before writing commits/files. On a developer machine that has already indexed this repo, `make fixtures` can emit an empty or partial fixture even though the output directory is temporary. AC-17 needs to require an isolated HOME/auto config for the autoetl invocation, or a fixture-specific state path/full-rebuild mechanism in a temp directory. Do not rely on plain `--full` against the real HOME because current `--full` removes the user's global git sync state.
AUTHOR: Verified: `GitSyncStatePath()` is hardcoded to `<HOME>/.auto/etl/git/sync-state.json` (state.go:24) and `--output` does not relocate it; `--full` removes that real file (run.go:375). Both confirm the ambient-state hazard. Resolved by requiring the regen to run autoetl under an **isolated temp `HOME`**, so the sync-state path resolves to an empty temp location → guaranteed full, deterministic extraction, and the developer's real `~/.auto` is never read or mutated. Explicitly forbade `--full` against the real HOME. Updated solution step 5.1 and plan step 5.1 accordingly.
-->

**AC-18**: Synthetic fixtures for scoring / decay unit tests
- Given: a unit test needs precise control over commit dates, cohort sizes, or co-occurrence counts (e.g. verifying the 90-day decay exponent, asymmetric confidence, or the large-commit penalty)
- When: the test runs
- Then: it constructs ephemeral input rows programmatically (in-test or via a shared test helper) and exercises the same load → in-memory SQLite → Go-scoring code path as the production command. No checked-in artefact required for these tests. Time-decay tests in particular MUST use synthetic fixtures since auto-stack's ~7-week history cannot exercise a 90-day decay constant.

**AC-19**: Conformance tests against real snapshot
- Given: the checked-in auto-stack snapshot
- When: `go test ./...` runs in auto-search
- Then: at least one end-to-end test invokes the co-change command (or its top-level entry function) against the snapshot for a known file (e.g. `auto-etl/internal/git/extract.go`) and asserts (a) the output is valid JSON conforming to the documented schema, (b) at least one expected related file appears in the top results (e.g. an adjacent test or model file), (c) the metadata block fields are all populated.

**AC-20**: Privacy guard on fixture contents
- Given: the regenerated fixture
- When: `make fixtures` finishes (or a separate `make verify-fixtures` target is run, also wired into CI)
- Then: an automated assertion step verifies via **parquet schema introspection** (`parquet-go` — read each fixture parquet's schema and inspect its column list) that the fixture contains NONE of:
  - a `messages/` directory anywhere under the fixture root
  - a `sessions/` directory anywhere under the fixture root
  - a `commit_hunks/` directory anywhere under the fixture root
  - a `diff` column in any `commit_files` parquet (fail if `diff` appears in the schema's column list)
  - a `diff_truncated` column in any `commit_files` parquet (same mechanism)
  - a `message` column in any `commits` parquet (same mechanism — only `message_truncated` is retained)
  - a `trailers_json` column in any `commits` parquet
- Schema/column-list introspection is the required mechanism — NOT a `WHERE diff IS NOT NULL` predicate, which would fail with a missing-column error against a properly projected fixture. Failure of any check fails the target with a clear error naming the offending dataset/column. This guard exists so a future schema change cannot silently reintroduce private content.

<!-- RESOLVED(P2): Example diff-column assertion fails after projection
REVIEW: AC-16 requires dropping the `diff` column from `commit_files`, so the example DuckDB query with `WHERE diff IS NOT NULL` will fail with a missing-column/binder error instead of returning 0. Make column-list inspection the required check, or specify that the non-null query only runs after confirming the column exists.
AUTHOR: Correct. Rewrote AC-20 to require column-list inspection via DuckDB `DESCRIBE` (or equivalent schema introspection) as the mechanism for every check, and explicitly forbade the `WHERE diff IS NOT NULL` style. Also expanded the forbidden-column list to include `diff_truncated`, `message`, and `trailers_json` so the guard catches all dropped privacy/size-sensitive columns, not just `diff`.
AUTHOR (solution stage): The execution engine changed from the duckdb CLI to in-process parquet-go + pure-Go SQLite (see solution.md "Engine decision"). AC-20's mechanism is updated accordingly — the guard now uses `parquet-go` schema introspection instead of duckdb `DESCRIBE`. The underlying guarantee (no forbidden datasets/columns in the fixture) is unchanged; the column-list-inspection approach this thread asked for still holds, just via parquet-go.
-->

## Out of Scope

- Cross-repo co-change correlation (single repo per query for v1)
- Hunk- or line-level coupling — file granularity only for v1
- Persisting co-change results to a derived parquet index — recompute on demand
- Visualisation / graph rendering of coupling (text and JSON only)
- Integrating co-change scores into autograph's code context graph (separate future task)
- Ingestion concerns — the command assumes parquet is already populated by `auto-etl run`; it does not trigger ETL
- Author identity normalisation (alice@x.com vs alice@y.com treated as distinct for v1)
- DuckDB in any form (CGO `go-duckdb` OR the `duckdb` CLI) — the engine is in-process `parquet-go` + pure-Go SQLite; no duckdb dependency at runtime or build time (see solution.md "Engine decision")
- Adding a `doctor` command to auto-search — not needed without an external dependency to check
- Slicing test fixtures from the developer's local `~/.auto/etl/output` — fixtures are always regenerated from this repo to keep the privacy boundary structural
- Shipping fuller-schema fixtures (with `diff` text or `commit_hunks`) — future commands that need these will regenerate their own fixtures
- Including `messages/` or `sessions/` parquet in any checked-in fixture under any circumstances

## Open Questions

None — resolved during requirements discussion:
- Rename handling: follow renames (default on)
- Time decay: on by default, τ = 90 days, `--no-decay` to disable
- Scope: single repo, inferred from input path
- Session linkage: `top_sessions` is a first-class field in the JSON output
