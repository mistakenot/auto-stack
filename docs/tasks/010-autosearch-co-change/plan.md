# Plan: Task 010 — Autosearch Co-Change Query

## Summary

Add `autosearch co-change <file>`: resolve the repo from the input path, read its git parquet via `parquet-go` into an ephemeral in-memory pure-Go SQLite, aggregate co-change coupling in SQL, score in Go, and emit JSON — with a checked-in fixture and a privacy-guarded regen target.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| + | `auto-shared/git/normalize.go` | Move `NormalizeRemoteURL`, `ComputeRepoID`, `ComputeRepoIDFromPath` here (single source of truth) |
| ~ | `auto-etl/internal/git/normalize.go` | Re-export / delegate to `auto-shared/git`; update internal callers |
| ~ | `auto-search/internal/etlscan/discover.go` | Extend dataset list to git datasets (or add `DiscoverGit`) |
| + | `auto-search/internal/etlscan/parquet_git.go` | Slim column-pruned readers: `ReadCommitsSlim`, `ReadCommitFilesSlim`, `ReadGitRepos`, `ReadGitRefs` |
| + | `auto-search/internal/cochange/types.go` | `Result`, `Metadata`, `RelatedFile`, `RefTip`, `Params` JSON structs |
| + | `auto-search/internal/cochange/load.go` | Load projected parquet → `:memory:` SQLite; compute per-commit `weight` in Go |
| + | `auto-search/internal/cochange/query.go` | SQL: `path_canon` rename CTE, per-path totals, co-occurrence self-join, top-N, ref-tip join |
| + | `auto-search/internal/cochange/score.go` | Pure scalar scoring (confidence, lift, score), filters, sort, limit |
| + | `auto-search/internal/cochange/repo.go` | `ResolveRepo(path, repoIDOverride)` via git toplevel + normalised origin remote |
| + | `auto-search/internal/cochange/cochange.go` | Orchestrator wiring resolve → load → query → score → assemble |
| + | `auto-search/internal/cli/cochange.go` | Cobra command, flags, JSON emit, `ExitError` mapping |
| ~ | `auto-search/internal/cli/root.go` | Register `newCoChangeCmd()` |
| ~ | `auto-search/internal/cli/quickstart.go` | Add co-change example (AC-15) |
| + | `auto-search/internal/cochange/fixturegen/main.go` | Go fixture builder + privacy guard (`parquet-go`) |
| ~ | `Makefile` | `fixtures` + `verify-fixtures` targets (root Makefile) |
| + | `auto-search/testdata/fixtures/auto-stack-snapshot/**` | Checked-in slim parquet + `SHA.txt` |
| + | `auto-search/internal/cochange/{score,query}_test.go` | Unit tests (synthetic in-memory data) |
| + | `auto-search/internal/cochange/conformance_test.go` | E2E vs checked-in snapshot |
| ~ | `auto-search/internal/cli/cli_integration_test.go` | CLI integration cases (AC-10, AC-15) |

## Links
- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test
- [ ] `auto-search/internal/cochange/score_test.go` — scoring/decay/threshold units (AC-2,3,7,8,18), incl. `Wb > Wab` case
- [ ] `auto-search/internal/cochange/query_test.go` — rename `path_canon` CTE, per-path vs co-occurrence aggregation, renamed-candidate canonicalisation (AC-6,18)
- [ ] `auto-search/internal/cochange/conformance_test.go` — end-to-end vs auto-stack snapshot (AC-1,4,5,9,16,19)
- [ ] `auto-search/internal/cli/cli_integration_test.go` — outside-repo / no-origin / missing-parquet fail-fast + `--help` + quickstart (AC-10,15)
- [ ] `make verify-fixtures` — privacy guard (AC-20) and determinism (AC-17)

## Execution Sequence

```
Phase 1 (auto-shared normalize) ─────────────────────────────┐
                                                             v
Phase 2 (slim readers + discover) ─→ Phase 3 (core engine) ─→ Phase 4 (resolve + CLI + orchestrator) ─→ Phase 6 (tests)
                                  \                                                                      ^
                                   └─→ Phase 5 (fixtures + guard + Makefile) ─────────────────────────-/
```

- Phase 1 and Phase 2 can start in parallel.
- Phase 3 depends on Phase 2. Phase 4 depends on Phase 1 (normaliser) + Phase 3.
- Phase 5 depends on Phase 2 (knows the projected columns). Phase 6 depends on Phase 4 + Phase 5.

## Plan

### Phase 1: Extract remote normalisation to auto-shared
- [x] Step 1.1: Create `auto-shared/git/normalize.go` with `NormalizeRemoteURL`, `ComputeRepoID`, `ComputeRepoIDFromPath` (moved verbatim from `auto-etl/internal/git/normalize.go`); move `normalize_test.go` cases too.
- [x] Step 1.2: Update `auto-etl/internal/git/` callers to import `github.com/mistakenot/auto-shared/git` (keep thin wrappers only if external callers exist; otherwise delete the auto-etl copy).
- [x] Step 1.3: Verify: `cd auto-shared && go build ./... && go test ./git/...` passes; `cd auto-etl && go build ./... && go test ./...` passes (normalisation behaviour unchanged).
- [x] Step 1.4: Commit: `refactor(010): phase 1 - move git remote normalisation to auto-shared`

### Phase 2: Slim git parquet readers + dataset discovery
- [x] Step 2.1: Add `etlscan.DiscoverDatasets(inputRoot, datasets []string)` and reimplement `Discover` as `DiscoverDatasets(inputRoot, []string{"messages","sessions"})` (do NOT broaden the global `Discover` — the indexer writes `index_state`/`FilesProcessed` for any discovered source). Co-change will call `DiscoverDatasets` with the git datasets. Verify the indexer is untouched.
- [x] Step 2.2: Add `auto-search/internal/etlscan/parquet_git.go` with slim structs (parquet tags matching `auto-etl/internal/model/git.go`) + readers `ReadCommitsSlim`, `ReadCommitFilesSlim`, `ReadGitRepos`, `ReadGitRefs` (follow `parquet_sessions.go` pattern).
- [x] Step 2.3: Verify: `cd auto-search && go build ./...`; add a quick unit test that `Discover` on `testdata/etl-output` still returns only messages/sessions, and that readers parse a hand-written tiny parquet (or defer reader assertion to Phase 6 conformance).
- [x] Step 2.4: Verify: existing `go test ./internal/etlscan/... ./internal/indexdb/...` still pass (no regression to indexing).
- [x] Step 2.5: Commit: `feat(010): phase 2 - slim git parquet readers and dataset discovery`

### Phase 3: Core engine — load → in-memory SQLite → aggregate
- [x] Step 3.1: `cochange/load.go` — open `sql.Open("sqlite", ":memory:")` with `SetMaxOpenConns(1)`; create temp tables `cf`, `c`, `refs`; insert projected rows filtered to `repo_id`; compute per-commit `weight = 1/log1p(max(1,files_changed)) * (noDecay ? 1 : exp(-Δt/τ))` in Go and store on `c`.
- [x] Step 3.2: `cochange/query.go` — (a) `path_canon` recursive CTE folding rename edges forward to current path; (b) per-path weighted totals (`Wa`, `Wb`, raw counts) over ALL filtered `cf`; (c) co-occurrence self-join producing ONLY `Wab`, raw `co_commits`, `last_co_change`; (d) `Wn` scalar + window functions for top-5 authors/sessions, top-3 sample commits; (e) ref-tip query joining `refs.commit_id = substring(c.commit_id, length(repoID)+2)`.
- [x] Step 3.3: Verify: `go build ./...`; `query_test.go` over a synthetic in-memory dataset asserts (i) renamed candidate canonicalised to current path, (ii) `Wb` counts B's non-A commits (so `Wb > Wab` when applicable), (iii) ref-tip join returns the seeded default branch.
- [x] Step 3.4: Commit: `feat(010): phase 3 - in-memory sqlite load and co-change aggregation`

### Phase 4: Scoring, repo resolution, orchestrator, CLI
- [x] Step 4.1: `cochange/types.go` — JSON structs (`Result{Meta, Metadata, RelatedFiles}`, etc.) matching AC-4/AC-5 field names.
- [x] Step 4.2: `cochange/score.go` — pure functions: `confidence_a_to_b=Wab/Wa`, `confidence_b_to_a=Wab/Wb`, `lift=(Wab*Wn)/(Wa*Wb)`, `score=confidence_a_to_b*log1p(lift)`; apply `co_commits<3` filter; `commits(A)<5` → metadata-only `warning:"insufficient history"`; sort desc; `--limit` (default 50, 0=no cap).
- [x] Step 4.3: `cochange/repo.go` — `ResolveRepo`: derive a directory from the input path first (existing file → parent dir; existing dir → itself; non-existent → nearest existing ancestor, else cwd) since `git -C` rejects file/missing paths; then `git -C <dir> rev-parse --show-toplevel`; compute `resolved_path` lexically vs toplevel (works for unknown files, AC-9); `git -C <toplevel> remote get-url origin` → `NormalizeRemoteURL` → match `git_repositories.repo_remote_normalized`; `--repo-id` override; no-origin/no-match → typed error.
- [x] Step 4.4: `cochange/cochange.go` — orchestrator (resolve → load → query → score → assemble metadata incl. language-by-extension, `exists_in_workspace`, `renamed_from`, `ref_tips_at_touched_commits`, `params_used`).
- [x] Step 4.5: `cli/cochange.go` + register in `root.go`; flags `--repo-id`, `--limit`, `--decay-tau` (default `90d`), `--no-decay`, `--input`; map errors to `ExitError`; JSON to stdout. Add quickstart example. **`--decay-tau` is parsed by the existing `search.ParseDurationMs` (`auto-search/internal/search/timefilters.go:85`, units `m|h|d|w`)** — do NOT use `time.ParseDuration` (rejects `d`/`w`). Convert the returned ms to days for τ. Units are `m|h|d|w` only; no `y` (AC-7 example uses `26w`).

<!-- RESOLVED(P2): `--decay-tau` parser is underspecified for documented units
REVIEW: AC-7 documents values like `30d` and `1y`, and the plan's default is `90d`. Go's `time.ParseDuration` does not accept `d` or `y`; the existing auto-search helper `search.ParseDurationMs` accepts `m|h|d|w` but not `y`; auto-etl has a separate git `--since` converter that accepts `mo`/`y`. Specify which parser co-change should use or add a small co-change duration parser/test that accepts every unit promised by AC-7. Otherwise an implementation can easily reject the documented default/examples.
AUTHOR: Resolved by standardising on the existing `search.ParseDurationMs` (timefilters.go:85, units m|h|d|w) — reusing it keeps `--decay-tau` consistent with `--since`/`--min-duration` and avoids the `time.ParseDuration` d/w gap. Step 4.5 now names the parser explicitly and notes the ms→days conversion. Updated AC-7's example from `1y` (unsupported by this parser) to `26w`, since adding a `y` unit just for this flag would diverge from the shared helper. `30d`, `90d`, `26w` all parse.
-->

- [x] Step 4.6: Verify: `go build ./...`; `go vet ./...`; `score_test.go` passes incl. decay on/off and `confidence_b_to_a < 1` case; `autosearch co-change --help` lists all flags.
- [x] Step 4.7: Commit: `feat(010): phase 4 - scoring, repo resolution, and co-change CLI command`

### Phase 5: Fixture builder, privacy guard, Makefile
- [x] Step 5.1: `cochange/fixturegen/main.go` — run autoetl under an **isolated temp HOME** (`HOME=<tmp_home> autoetl run --repo-path <repo> --output <tmp_out> --only git`) so the git sync-state is empty → full deterministic extraction and the dev's real `~/.auto` is untouched (do NOT use `--full` against the real HOME — it deletes the user's sync-state). Then `parquet-go`-read each dataset, project to the AC-16 retained columns, sort by stable key, write `auto-search/testdata/fixtures/auto-stack-snapshot/<dataset>/<dataset>.parquet`; write `SHA.txt`; delete temp dirs.
- [x] Step 5.2: Add privacy-guard mode (or `verify-fixtures`): via `parquet-go` schema introspection, assert NO `messages/`,`sessions/`,`commit_hunks/` dirs and NO `diff`/`diff_truncated`/`message`/`trailers_json` columns; fail loudly otherwise (AC-20).
- [x] Step 5.3: Root `Makefile`: add `fixtures` (regen) and `verify-fixtures` (guard + size<1MB) targets.
- [x] Step 5.4: Run `make fixtures` against this repo; verify checked-in snapshot is < 1 MB and `make verify-fixtures` passes; re-run `make fixtures` and confirm byte-identical output (AC-17 determinism).
- [x] Step 5.5: Commit: `feat(010): phase 5 - fixture builder, privacy guard, make targets`

### Phase 6: Tests
- [x] Step 6.1: `conformance_test.go` — run co-change against the snapshot for a known file (e.g. `auto-etl/internal/git/extract.go`); assert valid JSON to schema, an expected related file appears (e.g. `extract_test.go`), all metadata fields populated, unknown-file → metadata-only exit 0 (AC-1,4,5,9,19).
- [x] Step 6.2: `cli_integration_test.go` cases — outside-repo, no-origin-without-`--repo-id`, missing-parquet all exit non-zero with stderr remediation; an **existing file path** and a **non-existent file path inside a repo** both resolve the repo correctly (path→dir algorithm, AC-1/AC-9); `--help` and quickstart contain co-change (AC-10,15).
- [x] Step 6.3: Backfill any uncovered AC unit cases (AC-3 thresholds, AC-8 limit/sort, AC-16 size assertion).
- [x] Step 6.4: Verify: `cd auto-search && go build ./... && go vet ./... && go test ./...` all pass; `make verify-fixtures` passes.
- [x] Step 6.5: Commit: `test(010): phase 6 - conformance, integration, and unit coverage`

## Success Criteria
- [ ] `cd auto-shared && go test ./...`, `cd auto-etl && go test ./...`, `cd auto-search && go test ./...` all pass
- [ ] `go vet ./...` clean in all three modules
- [ ] `autosearch co-change <file>` emits AC-4/AC-5-conforming JSON against real `~/.auto/etl/output`
- [ ] Every AC-1..AC-10 and AC-15..AC-20 has a passing mapped test (AC-11..14 removed per solution.md)
- [ ] `make verify-fixtures` passes; checked-in fixture < 1 MB; `make fixtures` is byte-deterministic
- [ ] Manual: run against this repo for `auto-etl/internal/git/extract.go`, confirm `extract_test.go` ranks highly and `top_sessions` are populated

## Open Questions
- None — all resolved in requirements.md and solution.md (engine, fixtures, privacy, repo resolution, ref-tip join, `Wb` aggregation, rename canonicalisation).
