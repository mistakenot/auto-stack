# Solution: Task 010 — Autosearch Co-Change Query

## Engine decision (revises requirements)

Exploration of auto-search changed the execution-engine choice. auto-search today reads parquet **natively** via `parquet-go` (`internal/etlscan`), embeds a **pure-Go SQLite** (`modernc.org/sqlite`, no cgo) for its FTS index, has **no `doctor` command**, **no `--format` flag** (JSON-only), and **zero external runtime dependencies**. Shelling out to the `duckdb` CLI (original AC-12/13/14) would make auto-search the only stack tool needing an external binary.

Chosen engine (**Option C**): per-query, read the target repo's projected parquet with `parquet-go`, load into an **ephemeral in-memory SQLite** database, do the joins / grouping / top-N in SQL, and compute the final scalar scores (confidence, lift, decay) **in Go**. No new dependency; pure-Go binary preserved. See *Requirements deltas* for the AC changes this implies.

## Approach

1. **Resolve repo.** First derive a **directory** to hand `git -C` (git rejects a file path or a missing path):
   - Resolve the input to an absolute path. If it is an existing file → use its parent dir; if an existing dir → use it as-is; if it does not exist → walk up to the nearest existing ancestor directory (stop at filesystem root). If nothing exists (e.g. the path's drive/root is absent) → fall back to the current working directory.
   - Run `git -C <dir> rev-parse --show-toplevel`. If that fails (not inside a git repo) → fail fast (AC-10).
   - Compute `resolved_path` as the input path relative to the toplevel (lexical; the file need not exist — AC-9 unknown files still resolve).
   - Then `git -C <toplevel> remote get-url origin`, normalise via the shared `NormalizeRemoteURL`, and match `git_repositories.repo_remote_normalized` → `repo_id`. If the repo **has no origin remote**, do not guess: exit non-zero with an actionable error ("repo has no origin remote; pass `--repo-id <id>`"). A `--repo-id` flag overrides the whole resolution (no-origin repos, tests, odd remotes). Path-derived matching (auto-etl's `ComputeRepoIDFromPath`) is intentionally NOT used because `git_repositories.repo_path` is host-volatile and dropped from the fixture (AC-16). This is reflected in AC-1/AC-10.

<!-- RESOLVED(P1): ResolveRepo cannot run git commands from file paths
REVIEW: The design says to resolve the repo "from the input path" with `git rev-parse --show-toplevel`, but the command has to accept `autosearch co-change <path/to/file>` and unknown/untracked files. I checked git behavior in this repo: `git -C auto-etl/internal/git/extract.go rev-parse --show-toplevel` fails with "Not a directory", and `git -C no/such/path.go rev-parse --show-toplevel` fails with "No such file or directory". Specify the path resolution algorithm before implementation: for existing files use the parent directory; for non-existent relative paths use the current working directory or nearest existing parent inside the repo; then compute `resolved_path` relative to the repo root. Add CLI tests for an existing file path and a non-existent file path inside a repo so AC-1/AC-9 are covered.
AUTHOR: Correct — `git -C` needs an existing directory. Rewrote step 1 with an explicit path→dir algorithm: existing file → parent dir; existing dir → itself; non-existent path → nearest existing ancestor dir (else cwd); then `git -C <dir> rev-parse --show-toplevel`, and `resolved_path` computed lexically relative to the toplevel so unknown/untracked files (AC-9) still resolve. Added plan Phase 6 CLI tests for both an existing file path and a non-existent file path inside a repo (covering AC-1 and AC-9).
-->

<!-- RESOLVED(P2): Repo resolution omits path-derived repo IDs
REVIEW: `auto-etl/internal/git/extract.go` handles repos without an origin remote by computing `repo_id` from the repo path (`ComputeRepoIDFromPath` when `NormalizeRemoteURL(repo.Remote)` is empty). The proposed `ResolveRepo` only runs `git remote get-url origin` and matches `git_repositories.repo_remote_normalized`, so a valid `autoetl run --repo-path` dataset for a local-only repo would fail unless the hidden test flag is used. Either add the same path-derived fallback or make no-origin repos explicitly invalid in AC-1/AC-10.
AUTHOR: Verified the path-derived fallback exists in extract.go (line 36). Chose the "explicitly handle no-origin" route rather than replicating path-derived matching, because the only stable key for path-derived repos is `git_repositories.repo_path`, which is host-volatile and is dropped from the AC-16 fixture — so a path fallback would be both non-portable and untestable. Step 1 now specifies that no-origin repos fail fast with an actionable error directing the user to `--repo-id`, and the `--repo-id` flag is promoted from hidden-test-only to a documented escape hatch. Updated AC-1 and AC-10 to state this.
-->

2. **Discover + read parquet (column-pruned).** Refactor discovery to be **dataset-scoped**: add `etlscan.DiscoverDatasets(inputRoot string, datasets []string) ([]ParquetSource, error)` and reimplement the existing `Discover` as `DiscoverDatasets(inputRoot, []string{"messages","sessions"})`. The existing indexer keeps calling `Discover` (or the explicit messages/sessions list), so it never sees git datasets — preserving "git data is not persisted into the FTS index". Co-change calls `DiscoverDatasets(inputRoot, []string{"commits","commit_files","git_repositories","git_refs"})`. Read only the columns the query needs via slim parquet-tagged structs (parquet-go prunes columns), filtered to `repo_id`.

<!-- RESOLVED(P2): Extending global Discover affects the existing indexer
REVIEW: `auto-search/internal/indexdb/indexer.go` calls `etlscan.Discover` in both `FullBuild` and `IncrementalUpdate`, but its switch only handles `sessions` and `messages`; unknown datasets still get `index_state` rows and increment `FilesProcessed`. If `Discover` starts returning `commits`, `commit_files`, `git_repositories`, and `git_refs`, then `autosearch index` against normal `~/.auto/etl/output` will silently "process" git parquet without indexing it, which conflicts with the solution's rejected alternative that git data is not persisted into the main FTS index. Make discovery dataset-scoped (for example `DiscoverDatasets(inputRoot, []string{...})`) so the existing indexer still sees only sessions/messages, or add explicit indexer filtering/tests for git datasets.
AUTHOR: Good catch — `indexer.go` (FullBuild line 58-89 / IncrementalUpdate) writes `index_state` rows and increments `FilesProcessed` for ANY discovered source, so broadening the global `Discover` would make `autosearch index` silently churn git parquet. Resolved by NOT mutating `Discover`: instead add `DiscoverDatasets(inputRoot, datasets)` and reimplement `Discover` as a thin wrapper over `{"messages","sessions"}`. The indexer's behaviour is byte-for-byte unchanged; co-change calls `DiscoverDatasets` with the git datasets. Updated plan step 2.1 and added a regression test that `Discover` still returns only messages/sessions.
-->

3. **Load into in-memory SQLite.** Create `:memory:` DB with temp tables `cf(commit_id, file_path, change_type, old_path)`, `c(commit_id, author_date, files_changed, author_name, author_email, session_id, short_id, message_truncated, weight)`, and `refs(ref_name, ref_type, commit_id, is_default)`. The per-commit `weight` is computed in Go at load time (it depends on the query's `--decay-tau` / `--no-decay`), so SQL only ever `SUM`s a pre-computed column — no SQLite math functions required. **`refs.commit_id` is the raw SHA**, so when joining to commits the query strips the `<repoID>-` prefix from `commits.id` (`refs.commit_id = substring(c.commit_id, length(repoID)+2)`) — a naive `ON commit_id` matches nothing (see requirements AC-5 note).
4. **Build a repo-wide canonical-path map, then A's lineage.** First materialise a `path_canon(observed_path → canonical_path)` mapping for the *whole repo* by following `change_type='R'` rename edges (`old_path` → `file_path`) **forward** to each file's latest path (a recursive CTE over all rename edges). Every `cf.file_path` is resolved through `path_canon` before aggregation, so a candidate that was later renamed is merged under its current path (satisfies AC-4's "current path"), not split across historical names. A's lineage is simply the set of observed paths whose canonical path equals the input file's canonical path; the rename points along A's chain populate `renamed_from`.

<!-- RESOLVED(P2): Candidate paths are not canonicalized after renames
REVIEW: AC-4 requires each related file's `path` to be the current path, but the approach only builds rename lineage for input file A. Historical co-changes with a candidate that was later renamed will be grouped under the old path and can be emitted as the related `path`, instead of being merged into the candidate's current path. Specify either candidate-side rename canonicalization or relax AC-4 to return observed historical paths.
AUTHOR: Good catch — A-only lineage would split renamed candidates across old/new paths. Reworked step 4 to build a repo-wide `path_canon` map (every observed path → its latest canonical path, via forward rename-edge traversal) and to resolve ALL `cf.file_path` values through it before aggregation. Candidates are therefore grouped under their current path, satisfying AC-4. A's lineage becomes the special case of paths sharing the input's canonical path. Added a `query_test.go` case with a renamed candidate asserting it appears once under its current path.
-->

5. **Aggregate in SQL.** Drop commits with `files_changed > 50`. Compute aggregates in three independent passes so `Wb` is never restricted to A-co-occurring commits:
   - **(5a) Per-path totals** — `GROUP BY canonical_path` over *all* filtered `cf` rows (joined to commit weights): yields `Wpath` (Σ weight over every commit touching that path) and the raw `commits_touching(path)` count. `Wa` is this table's row for A's canonical path; `Wb` for candidate B is this table's row for B. This pass is independent of A, so `Wb` correctly counts B's non-A commits.
   - **(5b) Co-occurrence** — self-join `cf` (A's canonical path) × `cf` (other canonical paths) on `commit_id`, `GROUP BY` candidate path: yields `Wab` (Σ weight over commits touching both), raw `co_commits`, and `last_co_change`. This pass produces *only* `Wab`/`co_commits`, never `Wb`.
   - **(5c) Scalars + top-N** — `Wn` = Σ weight over all filtered commits. Window functions over the relevant commits produce per-candidate top-5 authors, top-5 sessions (recency), top-3 sample commits.
   - The final per-candidate row joins 5b (`Wab`, `co_commits`) to 5a (`Wb`); `Wa` and `Wn` are scalars. AC-18 includes a test where candidate B has commits with no A involvement, asserting `Wb > Wab` and `confidence_b_to_a < 1`.

<!-- RESOLVED(P1): `Wb` must be computed outside the co-occurrence self-join
REVIEW: The scoring block correctly defines `Wb` as the weighted sum over all commits touching candidate B. Step 5 says the self-join of A-touched commits produces `Wab` and `Wb`; if implemented literally, `Wb` only sees A-and-B co-occurring commits, so `Wb == Wab`, `confidence_b_to_a` becomes 1 for every candidate, and lift is inflated. Add a separate candidate-total aggregate over all filtered `cf` rows, plus a test where B has independent non-A commits.
AUTHOR: Correct and important. Rewrote step 5 into three explicit passes: (5a) a per-path weighted-total table over ALL filtered cf rows (this is where Wa and Wb come from, independent of A); (5b) the co-occurrence self-join which now produces ONLY Wab + raw co_commits + last_co_change; (5c) the Wn scalar and top-N windows. The final candidate row joins Wab (5b) to Wb (5a). Added an explicit AC-18 test where B has non-A commits, asserting Wb > Wab and confidence_b_to_a < 1.
-->

6. **Score in Go.** For each candidate: `confidence_a_to_b = Wab/Wa`, `confidence_b_to_a = Wab/Wb`, `lift = (Wab*Wn)/(Wa*Wb)`, `score = confidence_a_to_b * log1p(lift)`. Filter `co_commits < 3`; if raw commits-touching-A `< 5`, return metadata-only with `warning:"insufficient history"`. Sort by score desc, apply `--limit` (default 50, 0 = no cap).
7. **Assemble + emit JSON** using the existing `_meta` envelope convention: `{_meta, metadata, related_files}`.

## Scoring (authoritative formula)

```go
// per commit c (only commits with FilesChanged <= 50 are loaded):
filesWeight := 1.0 / math.Log1p(math.Max(1, float64(c.FilesChanged)))   // >0 always
decay := 1.0
if !noDecay {
    decay = math.Exp(-ageSeconds(c.AuthorDate) / (tauDays * 86400.0))
}
weight := filesWeight * decay   // stored in c.weight at load time

// SQL sums (weighted) + raw counts:
//   Wa  = Σ weight over commits touching any lineage path of A
//   Wb  = Σ weight over commits touching candidate B
//   Wab = Σ weight over commits touching both
//   Wn  = Σ weight over all loaded commits
//   coCommits(B) = COUNT(DISTINCT commit) touching both   (raw, for threshold + display)

confidenceAtoB := Wab / Wa
confidenceBtoA := Wab / Wb
lift := (Wab * Wn) / (Wa * Wb)
score := confidenceAtoB * math.Log1p(lift)
```

## Files

```
~ auto-search/internal/etlscan/discover.go        # add DiscoverDatasets(root, datasets); Discover becomes a {messages,sessions} wrapper (indexer unchanged)
+ auto-search/internal/etlscan/parquet_git.go      # slim readers: ReadCommitsSlim, ReadCommitFilesSlim, ReadGitRepos, ReadGitRefs (column-pruned)
+ auto-search/internal/cochange/types.go           # Result, Metadata, RelatedFile, RefTip JSON structs
+ auto-search/internal/cochange/repo.go            # ResolveRepo(path) -> (repoID, repoRoot, remote); uses git + normaliser
+ auto-search/internal/cochange/load.go            # load projected parquet -> in-memory sqlite; compute per-commit weight in Go
+ auto-search/internal/cochange/query.go           # SQL: lineage CTE, co-occurrence aggregation, top-N authors/sessions/samples
+ auto-search/internal/cochange/score.go           # pure scalar scoring + filters + sort + limit (heavily unit-tested)
+ auto-search/internal/cochange/cochange.go        # orchestrator wiring 1-7 together
+ auto-search/internal/cli/cochange.go             # cobra command, flags, JSON emit, ExitError mapping
~ auto-search/internal/cli/root.go                 # register newCochangeCmd()
~ auto-search/internal/cli/quickstart.go           # add co-change example (AC-15)

+ auto-shared/git/normalize.go                     # NormalizeRemoteURL moved here (single source of truth)
~ auto-etl/internal/git/normalize.go               # delegate to auto-shared (import) so repo-matching can't drift

+ auto-search/internal/cochange/fixturegen/main.go # Go fixture builder (parquet-go): run autoetl under an isolated temp HOME -> project columns -> write slim parquet + SHA.txt; also the privacy guard (schema introspection)
~ Makefile                                          # add `fixtures` + `verify-fixtures` targets (root Makefile; auto-search has none today)
+ auto-search/testdata/fixtures/auto-stack-snapshot/{commits,commit_files,git_repositories,git_refs}/*.parquet  # checked-in slim fixture
+ auto-search/testdata/fixtures/auto-stack-snapshot/SHA.txt

+ auto-search/internal/cochange/score_test.go       # unit: scoring/decay/thresholds (synthetic in-memory rows)
+ auto-search/internal/cochange/query_test.go        # unit: rename lineage CTE, aggregation (synthetic sqlite)
+ auto-search/internal/cochange/conformance_test.go  # e2e vs checked-in auto-stack snapshot
```

Slim reader struct example (column pruning via parquet tags):

```go
type CommitFileSlim struct {
    CommitID   string `parquet:"commit_id"`
    RepoID     string `parquet:"repo_id"`
    FilePath   string `parquet:"file_path"`
    ChangeType string `parquet:"change_type"`
    OldPath    string `parquet:"old_path"`
}
```

## Requirements deltas

These ACs change as a direct consequence of the Option-C engine decision and the JSON-only decision:

- **AC-11 (text output)** — **removed.** auto-search is JSON-only; no `--format` flag is added.
- **AC-12 (duckdb shell-out)** — **replaced.** Engine is parquet-go + ephemeral in-memory SQLite + Go scalar scoring. No shell-out. Injection surface is gone (values pass as Go args / bound table data, never concatenated SQL).
- **AC-13 (duckdb doctor check)** — **removed.** No duckdb to check; auto-search has no `doctor` command and none is added here. Missing-data fail-fast remains covered by AC-10.
- **AC-14 (duckdb missing runtime error)** — **removed.** No external runtime dependency.
- **AC-15 (help/quickstart)** — **revised.** `--help` + a `quickstart` example only. The `autosearch docs` bullet was dropped because auto-search has no `docs` subcommand; no duckdb dependency to mention.

<!-- RESOLVED(P2): AC-15 docs command is not planned or tested
REVIEW: Current `auto-search/internal/cli/root.go` registers `init`, `index`, `search`, `stats`, `session`, `message`, `skills`, `quickstart`, `agents`, and `update`, but no `docs` command. Requirements AC-15 says `autosearch docs` must enumerate the co-change command, while the solution only updates `quickstart.go` and the AC-15 test row only checks `--help` and quickstart. Add a docs command/update and test it, or revise AC-15 to match the current CLI surface.
AUTHOR: Confirmed there is no `docs` command in auto-search. Chose to revise AC-15 to match the actual CLI surface (drop the `autosearch docs` bullet) rather than add a whole new command, which would be scope creep. Requirements AC-15 retitled "Help and quickstart" with a note that a future `docs` command should list co-change but is out of scope here. The solution's Files (`quickstart.go`) and the AC-15 test row (`--help` + quickstart) already match this revised scope.
-->

- **AC-17 (fixture regen)** — **revised.** Projection done by a Go `parquet-go` fixture builder (`fixturegen`), not a duckdb `COPY`. Determinism via stable `ORDER BY` in the writer.
- **AC-20 (privacy guard)** — **revised.** Forbidden-column / forbidden-dataset checks use `parquet-go` schema introspection, not duckdb `DESCRIBE`. Same guarantee, no duckdb.

## Test Coverage

| AC   | Test Type   | File |
|------|-------------|------|
| AC-1 | e2e | `internal/cochange/conformance_test.go` |
| AC-2 | unit | `internal/cochange/score_test.go` |
| AC-3 | unit | `internal/cochange/score_test.go` |
| AC-4 | e2e + unit | `conformance_test.go`, `types` round-trip in `score_test.go` |
| AC-5 | e2e | `conformance_test.go` (metadata block fully populated) |
| AC-6 | unit | `internal/cochange/query_test.go` (rename lineage CTE) |
| AC-7 | unit | `score_test.go` (decay on/off, `--decay-tau`) |
| AC-8 | unit | `score_test.go` (sort + `--limit`) |
| AC-9 | e2e | `conformance_test.go` (unknown file → metadata-only, exit 0) |
| AC-10 | integration | `internal/cli/cli_integration_test.go` (outside repo / missing parquet → non-zero) |
| AC-15 | integration | `cli_integration_test.go` (`--help`, quickstart contains example) |
| AC-16 | integration | `conformance_test.go` (fixture loads; size < 1 MB asserted) |
| AC-17 | integration | `fixturegen` determinism test (re-run → byte-identical) |
| AC-18 | unit | `score_test.go`, `query_test.go` (synthetic in-memory fixtures) |
| AC-19 | e2e | `conformance_test.go` |
| AC-20 | integration | `fixturegen` privacy-guard test (asserts forbidden datasets/columns absent) |

## Out of Scope

- All items from requirements (cross-repo, hunk-level, derived index, visualisation, autograph integration, ETL ingestion, author identity normalisation, embedded CGO duckdb, bundling duckdb, slicing fixtures from `~/.auto/etl/output`, fuller-schema fixtures, messages/sessions in fixtures).
- Adding a `doctor` command to auto-search (no longer needed without duckdb).
- Persisting git datasets into the main `autosearch index` SQLite (co-change reads parquet on demand; indexing git data is a possible later optimisation, not v1).
- A `--format text` human renderer.

## Rejected Alternatives

- **DuckDB CLI shell-out (original AC-12):** rejected — introduces auto-search's only external runtime dependency and a doctor/version/CI-install burden, inconsistent with the existing native parquet-go architecture.
- **Persist git data into the main FTS index (index-then-query):** rejected for v1 — couples co-change to an `index` run and needs schema/incremental-state changes; per-query parquet read is simpler and matches AC-1/AC-19's "reads the parquet" framing. Revisit if query latency on large repos becomes a problem.
- **Pure-Go in-memory maps, no SQLite:** viable and dependency-free, but the self-join, top-N windows, and rename CTE are cleaner in SQL; SQLite is already embedded, so we use it for set ops and keep only scalar scoring in Go.
- **Duplicate `NormalizeRemoteURL` into auto-search:** rejected — divergent normalisation would silently break repo→`repo_id` matching. Moving it to `auto-shared` (already imported by auto-search) gives one source of truth.
- **Register `log`/`exp` SQL functions in modernc SQLite:** rejected — relying on registered scalar funcs is more fragile than computing the per-commit weight in Go at load time and the final score in Go; both keep the math unit-testable.
