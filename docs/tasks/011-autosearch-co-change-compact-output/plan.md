# Plan: Task 011

## Summary

Strip the binary large-commit cutoff from the cochange engine, add a compact text renderer in the engine package, flip the CLI default from JSON to text behind a new `--json` flag (replacing `--limit` with `--budget`/`--all`), and rewrite the quickstart co-change section to explain the two-phase router → read usage pattern.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| ~ | `auto-search/internal/cochange/query.go` | Remove `LargeCommitCutoff`, `cutoffStr`, the `init()` guard, and every `WHERE/AND ... files_changed <= cutoffStr` clause (4 sites: `Wn` SUM, `pathCanonCTE` rename_edge, `commit_decorated` join, `RenamedFrom`) |
| ~ | `auto-search/internal/cochange/types.go` | Delete `ParamsUsed.LargeCommitCutoff` and `ParamsUsed.Limit` fields |
| ~ | `auto-search/internal/cochange/cochange.go` | Drop `LargeCommitCutoff` and `Limit` from `ParamsUsed{}` literal; drop `Options.Limit`; pass `0` to `ScoreAndRank` |
| ~ | `auto-search/internal/cochange/score.go` | Comment-only update on the AC-3b note (no behavioural change) |
| ~ | `auto-search/internal/cochange/score_test.go` | Delete `TestScore_LargeCommitDropped`; add `TestScore_LargeCommitContributesContinuously` asserting a 100-file commit still contributes a small non-zero weight |
| ~ | `auto-search/internal/cochange/conformance_test.go` | Remove the `params_used.large_commit_cutoff` assertion (lines 241-242) |
| + | `auto-search/internal/cochange/render.go` | `RenderOptions`, `Render`, `renderHeader`, `renderRow`, `treeDistance`, `approxTokens`, `truncSubject`, `composeDisclosure` |
| + | `auto-search/internal/cochange/render_test.go` | Unit tests for header / row / tree-distance / normalization / budget truncation / disclosure / unknown-file / insufficient-history |
| ~ | `auto-search/internal/cli/cochange.go` | Remove `--limit`; add `--budget` (default 500), `--all`, `--json`; route JSON via current encoder, text via `cochange.Render` |
| ~ | `auto-search/internal/cli/quickstart.go` | Rewrite section 9 ("Find files that change together"): two-phase usage prose, new example block with `--budget`/`--all`/`--json`, compact-shape closing paragraph |
| ~ | `auto-search/internal/cli/cochange_integration_test.go` | Update existing JSON tests to pass `--json`; update `TestCoChangeHelpListsAllFlags` to drop `--limit` and add `--budget`/`--all`/`--json`; add `TestCoChangeCLIKnownFileText`, `TestCoChangeCLI_AllBypassesBudget`, `TestCoChangeCLI_LimitFlagRejected`, `TestQuickstartCoChangeSection` |

## Links

- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test

- [ ] `cd auto-search && go test ./internal/cochange/...` — engine + renderer unit + conformance tests
- [ ] `cd auto-search && go test ./internal/cli/...` — CLI integration tests (JSON, text, flag rejection, quickstart)
- [ ] `cd auto-search && go test ./...` — full package suite
- [ ] `make verify-fixtures` — fixture privacy/size guard (gates the `make test` target)
- [ ] `cd auto-search && go vet ./...` — lint
- [ ] Manual: `cd auto-search && go run ./cmd/autosearch co-change internal/cli/root.go` — eyeball default text output (header + ranked rows, no JSON, no `--limit` advertised)

<!-- RESOLVED(P2): Manual go run command targets the wrong package
REVIEW: The `auto-search` module root has no non-test Go files; the binary entrypoint is `auto-search/cmd/autosearch/main.go`. The manual command here and Step 4.6 use `go run .`, which will not run the CLI from `auto-search/`. Use `go run ./cmd/autosearch ...` (or the built `autosearch` binary) for the manual verification commands.
AUTHOR: Confirmed `auto-search/cmd/autosearch/main.go` is the entrypoint (the root has no main.go). Fixed both occurrences: the How-to-Test bullet and Phase 4 Step 4.6 now use `go run ./cmd/autosearch ...`.
-->

## Execution Sequence

```
Phase 1 (engine: remove cutoff)
   |
   v
Phase 2 (renderer: render.go + tests)
   |
   v
Phase 3 (CLI: flags + dispatch + integration tests)
   |
   v
Phase 4 (quickstart: section rewrite + test)
   |
   v
Phase 5 (E2E: JSON-seeded scenario fixtures + budget bound tests)
```

Phases are strictly sequential. Phase 2 depends on Phase 1's `ParamsUsed` shape change. Phase 3 depends on Phase 2's `Render` API. Phase 4 depends on Phase 3 because the AC-13 test asserts against quickstart text that mentions the new flag set. Phase 5 depends on Phases 1-3 (it exercises engine + renderer + CLI through the new fixture path).

## Plan

### Phase 1: Engine — remove binary large-commit cutoff

- [x] Step 1.1: Delete `const LargeCommitCutoff = 50` and `const cutoffStr = "50"` plus the `init()` panic-guard in `auto-search/internal/cochange/query.go` (lines 9-12 and 174-186). Verify: `grep -n "LargeCommitCutoff\|cutoffStr" auto-search/internal/cochange/query.go` returns nothing.
- [x] Step 1.2: Remove every `WHERE/AND ... files_changed <= cutoffStr` clause in `query.go` (four sites — `Wn` SUM, `pathCanonCTE` rename_edge, the per-path/per-candidate `commit_decorated` join, and `RenamedFrom`). Keep all other SQL intact. Verify: `grep -n "files_changed" auto-search/internal/cochange/query.go` shows zero remaining cutoff filters (matches in other contexts, if any, are not the cutoff).
- [x] Step 1.3: Delete the `LargeCommitCutoff int` and `Limit int` fields from `ParamsUsed` in `auto-search/internal/cochange/types.go` (lines 86 and 89). Verify: `grep -n "LargeCommitCutoff\|Limit " auto-search/internal/cochange/types.go` returns nothing in the `ParamsUsed` block.
- [x] Step 1.4: In `auto-search/internal/cochange/cochange.go`, remove the `LargeCommitCutoff:` and `Limit:` field assignments from the `ParamsUsed{}` literal (lines 87-93); delete `Limit int` from the `Options` struct (line 22); delete `limit := opts.Limit; limit = max(limit, 0)` (lines 48-49); change `ScoreAndRank(agg, limit)` to `ScoreAndRank(agg, 0)` (line 156). Verify: `cd auto-search && go build ./internal/cochange/...` passes (compile-clean after removals).
- [x] Step 1.5: Update the comment in `auto-search/internal/cochange/score.go` that references AC-3b to note continuous-weighting handling. Verify: `cd auto-search && go vet ./internal/cochange/...` passes.
- [x] Step 1.6: In `auto-search/internal/cochange/conformance_test.go`, remove the assertion block at lines 241-242 (`if m.ParamsUsed.LargeCommitCutoff != LargeCommitCutoff`). Verify: `cd auto-search && go test ./internal/cochange/ -run TestConformance` passes.
- [x] Step 1.7: In `auto-search/internal/cochange/score_test.go`, delete `TestScore_LargeCommitDropped` and add `TestScore_LargeCommitContributesContinuously` — a fixture with a single 100-file commit plus four 2-file commits; assert the candidate from the 100-file commit appears in `agg.Candidates` and has a `Wab` strictly greater than 0 but strictly less than the per-edge weight of any 2-file commit. Verify: `cd auto-search && go test ./internal/cochange/ -run TestScore` passes (all score tests green, including the new one).
- [x] Step 1.8: Commit: `feat(011): phase 1 - remove binary large-commit cutoff from cochange engine`

### Phase 2: Renderer — `render.go` + unit tests

- [x] Step 2.1: Create `auto-search/internal/cochange/render.go` with: `type RenderOptions struct { Budget int; All bool }`, `const defaultBudget = 500`, `const subjectCap = 60`. Public entry `func Render(r *Result, opts RenderOptions) string`. Verify: `cd auto-search && go build ./internal/cochange/...` passes (empty stubs compile).
- [x] Step 2.2: Implement `treeDistance(a, b string) int` over forward-slash paths: split each into directory segments (drop the basename), find longest common prefix length `p`, return `(len(aDir) - p) + (len(bDir) - p)`. Same dir → 0; sibling dirs sharing parent → 2; unrelated tops → sum of depths. Verify: `cd auto-search && go test ./internal/cochange/ -run TestTreeDistance` passes once Step 2.5 lands.
- [x] Step 2.3: Implement `approxTokens(s string) int` as `(utf8.RuneCountInString(s) + 3) / 4` and `truncSubject(s string) string` that caps at `subjectCap` runes with a trailing `…` on overflow. Verify by reading the resulting `render.go`: both use `utf8.RuneCountInString`, neither uses `len(s)`.
- [x] Step 2.4: Implement `renderHeader(m *Metadata) string` (AC-2) and `renderRow(rf *RelatedFile, seedDir string, norm float64) string` (AC-3). Header is two lines: `m.ResolvedPath` then `<TotalCommits> commits, <FirstTouched> → <LastTouched>` (omit the ` → <LastTouched>` segment when `LastTouched` is empty). Row: `<path>  <score>  <N>×  d<n>` (score via `strconv.FormatFloat(norm, 'f', 2, 64)`), plus `  [<sha7> "<subject>"]` segment when `d>0` and `len(rf.SampleCommits) > 0` (use `SampleCommits[0]`, take `SHA[:min(7, len(SHA))]`). Verify: `cd auto-search && go build ./internal/cochange/...` passes.
- [x] Step 2.5: Implement `Render` orchestration. Branch order is critical so AC-11's two distinct cases produce the right shape:
  1. If `m.TotalCommits == 0` (unknown-file, AC-11 case 2): emit exactly `m.ResolvedPath + "\nno history for this file\n"` — **do NOT call `renderHeader`** (it would emit `0 commits...` and add a spurious line). Return.
  2. Else if `m.Warning == "insufficient history"` (AC-11 case 1): emit `renderHeader(m) + "insufficient history\n"`. The full header is appropriate here because `TotalCommits > 0`. Return.
  3. Else (normal case): compute normalization (`norm[i] = rf.Score / rf.RelatedFiles[0].Score`), pre-render every row tagged with `(d int, score float64)`, then run AC-6 truncation: if `opts.All` skip; else iteratively drop the next victim by sort key `(d ascending, score ascending)` while `approxTokens(header + emitted_rows + composeDisclosure(hidden)) > effectiveBudget` (where `effectiveBudget = opts.Budget; if effectiveBudget <= 0 { effectiveBudget = defaultBudget }`). Compose disclosure per AC-7.
  Verify: `cd auto-search && go build ./internal/cochange/...` passes.

<!-- RESOLVED(P1): Unknown-file text path adds an extra header line
REVIEW: AC-11 says when `metadata.total_commits == 0`, text output has line 1 = seed path and line 2 = the literal `no history for this file`; it explicitly omits commit count and date because there are no commits to count. This step says `header + "\nno history for this file\n"`, but Step 2.4 defines `renderHeader` as a two-line header containing `<TotalCommits> commits...`, so a literal implementation would produce three lines (`0 commits...` plus no-history) and violate AC-11. Split the unknown-file case before calling the normal two-line header, or define a metadata-only header variant for `total_commits == 0`.
AUTHOR: Step 2.5 now spells out the branch order: unknown-file case emits `ResolvedPath + "\nno history for this file\n"` directly, bypassing `renderHeader` entirely (which would otherwise produce a `0 commits...` line). The insufficient-history case still uses the full header because `TotalCommits > 0`. Step 2.4 also already specifies that `renderHeader` omits the ` → <LastTouched>` segment when `LastTouched` is empty, so the insufficient case (which may have a `FirstTouched` but no spanning range) renders cleanly.
-->

- [x] Step 2.6: Write `auto-search/internal/cochange/render_test.go` with the unit tests listed in the solution's Test Coverage table (AC-2/3/4/6/7/8/9/11). Use direct `*Result` literals as fixtures — no DB involvement. At minimum: `TestRenderHeader`, `TestRenderRow_D0_NoSampleCommit`, `TestRenderRow_DPositive_IncludesSampleCommit`, `TestTreeDistance` (table-driven: same-dir, sibling, different top-level), `TestScoreNormalization_TopRowIsOne`, `TestApproxTokensCharsDiv4`, `TestBudget_FullFit_NoDisclosure`, `TestBudget_TrimDropsD0BeforeDPositive`, `TestBudget_TrimAllRows`, `TestDisclosure_AllSameDirWording`, `TestDisclosure_InclCrossDirWording`, `TestDisclosure_OmittedWhenNothingTrimmed`, `TestRender_AllBypassesBudget`, `TestRender_InsufficientHistoryTextLine`, `TestRender_NoHistoryTextLine`. Verify: `cd auto-search && go test ./internal/cochange/ -run "TestRender|TestTreeDistance|TestApproxTokens|TestBudget|TestDisclosure"` passes.
- [x] Step 2.7: Run the full engine package: `cd auto-search && go test ./internal/cochange/...`. Verify: green.
- [x] Step 2.8: Run `cd auto-search && go vet ./internal/cochange/...`. Verify: no warnings.
- [x] Step 2.9: Commit: `feat(011): phase 2 - add compact text renderer in cochange package`

### Phase 3: CLI — flags, dispatch, integration tests

- [x] Step 3.1: In `auto-search/internal/cli/cochange.go`, delete the `--limit` flag registration and the `limit int` local; add `var budget int`, `var all bool`, `var emitJSON bool`; register `cmd.Flags().IntVar(&budget, "budget", 500, "approximate token budget for text output (0 = use default)")`, `cmd.Flags().BoolVar(&all, "all", false, "emit every row, bypassing --budget")`, `cmd.Flags().BoolVar(&emitJSON, "json", false, "emit the full JSON envelope instead of compact text")`. Verify: `cd auto-search && go build ./internal/cli/...` passes.
- [x] Step 3.2: In `cochange.go` `RunE`, after the successful `cochange.Run(...)`, branch on `emitJSON`: if set, run the existing `json.NewEncoder(...).SetIndent("", "  ").Encode(result)` path; otherwise call `out := cochange.Render(result, cochange.RenderOptions{Budget: budget, All: all})` and write via `fmt.Fprint(cmd.OutOrStdout(), out)` (no trailing newline injection — `Render` owns its own trailing whitespace). Verify: `cd auto-search && go build ./internal/cli/...` passes.
- [x] Step 3.3: Update `auto-search/internal/cli/cochange_integration_test.go::TestCoChangeHelpListsAllFlags` to drop `--limit` from the expected-flag list and add `--budget`, `--all`, `--json`. Verify: `cd auto-search && go test ./internal/cli/ -run TestCoChangeHelpListsAllFlags` passes.
- [x] Step 3.4: Update every existing JSON-asserting test (`TestCoChangeCLIKnownFileJSON`, `TestCoChangeCLIExistingAndNonExistentPaths`, `TestCoChangeCLIOutsideRepo`, `TestCoChangeCLIMissingParquet`, `TestCoChangeCLINoOriginRemote`, `TestCoChangeCLINoRepoMatch`) by adding `"--json"` to the `runCLI(...)` args list, so they continue to exercise the JSON envelope on the new default-text command. Replace any existing `params_used.large_commit_cutoff` / `params_used.limit` assertions with the inverse — `TestCoChangeCLIKnownFileJSON_NoDeletedParamsFields` (AC-12g) decodes the JSON map and asserts `_, ok := paramsUsed["large_commit_cutoff"]; assert !ok` and the same for `"limit"`. Verify: `cd auto-search && go test ./internal/cli/ -run TestCoChangeCLI` passes (all JSON integration tests green, including the new absence check).
- [x] Step 3.5: Add `TestCoChangeCLIKnownFileText` to `cochange_integration_test.go`: run the same `runCLI(t, "co-change", inputAbs, "--repo-id", repoID, "--input", root)` (no `--json`, default text), assert stdout (a) starts with `<resolvedPath>\n` (header line 1), (b) contains a row line matching the resolved path's sibling test file with both `d0` and the `×` glyph, (c) contains no JSON braces `{`. Run a second invocation with `"--budget", "50"` to force truncation and assert the stdout contains `more hidden` and `run with --all`. Verify: `cd auto-search && go test ./internal/cli/ -run TestCoChangeCLIKnownFileText` passes.
- [x] Step 3.6: Add `TestCoChangeCLI_AllBypassesBudget`: invoke with `--all --budget 1`; assert stdout does NOT contain `more hidden`. Verify: passes.
- [x] Step 3.7: Add `TestCoChangeCLI_LimitFlagRejected`: invoke with `--limit 10`; assert exit code != 0 and stderr contains `unknown flag` or cobra's standard rejection wording. Verify: passes.
- [x] Step 3.7a: Add `TestCoChangeCLI_SurvivingFlagsStillWork` (AC-12h): invoke with `"co-change", inputAbs, "--repo-id", repoID, "--input", root, "--decay-tau", "30d", "--no-decay", "--request-id", "smoke", "--json"`; assert exit code 0, `_meta.request_id == "smoke"`, `metadata.params_used.decay_tau_days == 30`. Verify: passes.
- [x] Step 3.8: Run `cd auto-search && go test ./internal/cli/...`. Verify: green.
- [x] Step 3.9: Run `cd auto-search && go vet ./internal/cli/...`. Verify: no warnings.
- [x] Step 3.10: Commit: `feat(011): phase 3 - default to compact text in co-change CLI; add --budget/--all/--json`

### Phase 4: Quickstart — section rewrite + test

- [ ] Step 4.1: Rewrite section 9 of `auto-search/internal/cli/quickstart.go` (lines ~197-220). The new section MUST contain:
  - A short intro paragraph explaining co-change as a "phase-one router": run it to get a ranked shortlist of files worth opening, then open and read those files. State explicitly that the compact default exists so an agent can fan out across a changeset without burning context.
  - The literal phrase "phase one" or "two-phase" at least once.
  - A new bash example block with at least: (a) a no-flag default invocation, (b) a `--budget 800` (or similar non-default) example, (c) an `--all` example labelled for the hot-file case, (d) a `--json` example labelled "for programmatic / jq consumers", (e) the existing `--decay-tau` and `--repo-id` examples retained.
  - A closing paragraph describing the compact text shape (header + one line per file with `<score>  <N>×  d<n>` columns and `[sha "subject"]` on cross-dir rows) and pointing at `--json` for the full envelope with `_meta`, `metadata`, `related_files`.
  - No `--limit` substring anywhere in the section.
  Verify: `grep -n "limit\|--limit" auto-search/internal/cli/quickstart.go` returns no matches within lines 197-260 (cochange section only).
- [ ] Step 4.2: Add `TestQuickstartCoChangeSection` in `auto-search/internal/cli/cochange_integration_test.go`: `runCLI(t, "quickstart")`, then on stdout assert: contains `phase one` or `two-phase` (case-insensitive); contains `--budget`; contains `--all`; contains `--json`; does NOT contain `--limit`; still contains the section header (`co-change`). Keep the existing `TestQuickstartMentionsCoChange` untouched. Verify: `cd auto-search && go test ./internal/cli/ -run TestQuickstart` passes.
- [ ] Step 4.3: Run `cd auto-search && go test ./...`. Verify: full auto-search package suite green.
- [ ] Step 4.4: Run `make verify-fixtures` from repo root. Verify: fixture privacy guard + <1 MB size check pass.
- [ ] Step 4.5: Run `cd auto-search && go vet ./...`. Verify: no warnings.
- [ ] Step 4.6: Hermetic manual eyeball — does NOT require live ETL on the host. From `auto-search/`, derive the snapshot fixture root and repo id (same paths the integration tests use), then run: `go run ./cmd/autosearch co-change "$(git rev-parse --show-toplevel)/auto-etl/internal/git/extract.go" --repo-id <id-from-git_repositories-parquet> --input ./testdata/fixtures/auto-stack-snapshot 2>&1 | head -40`. Confirm: header on lines 1-2, ranked rows below, no JSON braces, no `--limit` mention. Then `go run ./cmd/autosearch co-change --help 2>&1 | grep -E "limit|budget|all|json"` — confirm `--budget`/`--all`/`--json` present and `--limit` absent. Then `go run ./cmd/autosearch quickstart 2>&1 | grep -A 40 "co-change"` — confirm the new section reads cleanly.
- [ ] Step 4.7: Commit: `feat(011): phase 4 - rewrite co-change quickstart section to explain two-phase usage`

### Phase 5: E2E validation via JSON-seeded scenario fixtures

- [ ] Step 5.1: Create a NEW importable sibling package `auto-search/internal/cochange/scenariofixture/scenariofixture.go` (`package scenariofixture`). `fixturegen` cannot be reused directly — it is `package main` and not importable. The new package re-uses the same parquet struct *shapes* as `fixturegen/main.go` (which uses the names `CommitFixture`, `CommitFileFixture`, `GitRepositoryFixture`, `GitRefFixture`), copying them in (one file, the structs and the parquet-go writer wiring are small). Export `LoadScenario(t *testing.T, name string) (rootDir string)`: resolves `internal/cochange/testdata/scenarios/<name>.json` relative to the calling test file via `runtime.Caller`, decodes it, writes parquet to `t.TempDir()/<dataset>/<dataset>.parquet` for the four datasets `commits`, `commit_files`, `git_refs`, `git_repositories` (matching the checked-in snapshot layout: `auto-stack-snapshot/commits/commits.parquet` etc. — NOT under a `git/` subdir, since `etlscan.DiscoverDatasets` walks `<root>/<dataset>/`), and returns the temp dir. Verify: `cd auto-search && go build ./internal/cochange/...` passes.
- [ ] Step 5.2: Define the scenario JSON schema in a doc comment at the top of `scenariofixture.go`: flat object with `repo_id`, `origin_remote`, and arrays of `commits` (`{sha, author_name, author_email, author_date_iso, subject, files: [{path, change_type, old_path?}]}`), `refs` (`{ref_name, ref_type, commit_sha, is_default}`). The helper expands `files` into `commit_files` rows and computes derived fields (`CommitFixture.FilesChanged = len(files)`, `CommitFileFixture.CommitID = <repoID>-<sha>`, ISO dates → unix ms via `time.Parse`, year/month from the parsed date, etc.). Verify by writing a tiny inline test in `scenariofixture_test.go` that round-trips a 2-commit scenario and reads back via `etlscan.ReadCommitsSlim` and `etlscan.ReadCommitFilesSlim` (the actual reader names — not `ReadCommits`/`ReadCommitFiles`) to confirm row counts match.

<!-- RESOLVED(P1): Scenario fixture helper cannot be wired as described
REVIEW: I checked `auto-search/internal/cochange/fixturegen/main.go`: it is `package main`, so `render_e2e_test.go` cannot import an exported `LoadScenario` from it ("program, not an importable package"). The existing structs are named `CommitFixture`, `CommitFileFixture`, `GitRepositoryFixture`, and `GitRefFixture`, not `CommitRow` / `CommitFileRow` / `GitRepoRow`, and `etlscan.DiscoverDatasets` looks for `<inputRoot>/<dataset>/*.parquet` while this step says to write `t.TempDir()/git/<dataset>/<dataset>.parquet`. Step 5.2 also names nonexistent `etlscan.ReadCommits` / `ReadCommitFiles` functions; the current readers are `ReadCommitsSlim` and `ReadCommitFilesSlim`. As written, Phase 5 will either fail to compile or produce a temp root that `cochange.Run(... InputRoot: rootDir)` cannot discover. Move reusable scenario-writing code into an importable package/test helper and write the same dataset layout as the checked-in snapshot fixture.
AUTHOR: All four claims verified against the codebase. Step 5.1 now creates a NEW importable sibling package `internal/cochange/scenariofixture/` (since `fixturegen` is `package main`). The four struct names are corrected to `CommitFixture`/`CommitFileFixture`/`GitRepositoryFixture`/`GitRefFixture` (matching `fixturegen/main.go`). The write layout is corrected to `<tempdir>/<dataset>/<dataset>.parquet` (matching the snapshot, which `DiscoverDatasets` discovers by walking `<root>/<dataset>/`). Step 5.2 reader names are corrected to `etlscan.ReadCommitsSlim`/`ReadCommitFilesSlim`. The Files row in solution.md needs the same change — updated in a follow-up edit.
-->

- [ ] Step 5.3: Author `auto-search/internal/cochange/testdata/scenarios/hot_file.json` — deepen the seed path so the intended d-labels are arithmetically correct. Seed `src/a/hot.go` (dir = `src/a`, depth 2). Mix: 20 same-dir siblings under `src/a/*` (d0); 8 cross-package siblings under `src/b/*` (d2: one up to `src`, one down to `b`); 4 unrelated under `pkg/util/*` (d4: two up to root, two down). Vary `co_commits` per pair from 3 to 15. Include 3-5 "noise" commits with `files_changed >= 20` so continuous weighting matters. Verify: `cd auto-search && go test ./internal/cochange/ -run TestScenario_LoadAndRender_hot_file` passes (test added in Step 5.5).

<!-- RESOLVED(P2): Scenario distance labels do not match AC-3
REVIEW: AC-3 defines distance between directories. For seed `src/hot.go`, a row under `src/other/*` has row dir `src/other` and seed dir `src`, so the distance is one up and zero down = `d1`, not `d2`; a row under `pkg/util/*` is `d3`, not `d4`. If tests assert the d-values stated here, they will fail against the planned `treeDistance` implementation. Adjust the fixture paths to produce the intended distances or update the expected labels.
AUTHOR: Confirmed the math: for seed `src/hot.go` (dir `src`), the LCA with `src/other/x.go` (dir `src/other`) is `src`, giving distance (1-1)+(2-1)=1, not 2. Fixed by deepening the seed path to `src/a/hot.go` (dir `src/a`, depth 2) so the intended d-labels work out: same-dir = d0; under `src/b/*` = d2 (one up, one down); under `pkg/util/*` = d4 (two up, two down). Step 5.4's `cross_dir_coupling.json` already uses `src/a/main.go` for the seed and `infra/pipeline/runner.go` for the cross-dir coupling — the math there is (2-0)+(2-0) = d4, consistent with its existing `d>=4` annotation.
-->

- [ ] Step 5.4: Author the remaining four scenarios:
  - `cross_dir_coupling.json` — seed `src/a/main.go`, 4 d0 siblings with weak `co_commits=3`, 1 d>=4 coupling at `infra/pipeline/runner.go` with `co_commits=4`. Under a tight `--budget`, the d>=4 row must survive while d0 siblings get dropped.
  - `large_commit.json` — one 100-file commit + 5 separate 2-file commits, all sharing a common file. Validates that the 100-file commit still contributes (weighted down) and doesn't dominate (continuous weighting working).
  - `no_history.json` — repo has 10 commits across unrelated files, none touching `src/missing.go`. Querying for `src/missing.go` must produce `no history for this file` text.
  - `insufficient_history.json` — seed file in exactly 2 commits (< `MinCommitsA = 5`). Must produce `insufficient history` text.
  Verify: each JSON parses cleanly via the Step 5.2 helper (`go test -run TestScenario_LoadAndRender_<name>` passes per scenario in Step 5.5).
- [ ] Step 5.5: Create `auto-search/internal/cochange/render_e2e_test.go` with five `TestScenario_LoadAndRender_<name>` subtests — one per scenario. Each: calls `LoadScenario`, then `cochange.Run(&Options{InputPath: <abs path>, RepoIDOverride: "fixture-repo", InputRoot: rootDir})`, then `Render(result, RenderOptions{})`, and asserts on the rendered string (header line 1 matches the seed path; rows shape matches AC-3; warning paths render per AC-11). Verify: `cd auto-search && go test ./internal/cochange/ -run TestScenario` passes.
- [ ] Step 5.6: Add the three AC-15 budget tests to the same file:
  - `TestHotFile_TokenBudgetBound` — runs `hot_file.json` at default budget; asserts `approxTokens(output) <= 500`.
  - `TestHotFile_AllBypassesBudget` — same scenario with `RenderOptions{All: true}`; asserts `approxTokens(output) > 500` (proves `--all` actually bypasses).
  - `TestHotFile_TextVsJSONSize` — runs the same scenario in both modes, asserts `utf8.RuneCountInString(textOut) <= len(jsonOut) / 4`.
  Verify: `cd auto-search && go test ./internal/cochange/ -run TestHotFile` passes.
- [ ] Step 5.7: **Required (not optional) CLI-level AC-15 tests.** Add three tests to `auto-search/internal/cli/cochange_integration_test.go` that use `runCLI` against a scenario tempdir built via `scenariofixture.LoadScenario(t, "hot_file")`:
  - `TestCoChangeCLI_HotFile_TokenBudgetBound` — `runCLI(t, "co-change", "src/a/hot.go", "--repo-id", "fixture-repo", "--input", scenarioRoot)` (no `--budget` flag, exercises the CLI default value of 500). Assert `approxTokens(stdout) <= 500`.
  - `TestCoChangeCLI_HotFile_AllBypassesBudget` — same args plus `"--all"`. Assert `approxTokens(stdout) > 500`.
  - `TestCoChangeCLI_HotFile_TextVsJSONSize` — run twice (with and without `"--json"`), assert `utf8.RuneCountInString(textOut) <= len(jsonOut) / 4`.
  These exercise the actual CLI flag-default wiring AC-15 is bounding, not just the engine-level `Render` defaults. Verify: `cd auto-search && go test ./internal/cli/ -run TestCoChangeCLI_HotFile` passes.

<!-- RESOLVED(P2): AC-15 requires a CLI hot-file scenario test
REVIEW: AC-15 says the CLI runs against `hot_file.json` with default flags, with `--all`, and with `--json` for the text-vs-JSON size comparison. The plan puts the hot-file checks in `internal/cochange/render_e2e_test.go` after `cochange.Run -> Render`, then marks the CLI-level scenario test optional. That misses the default `--budget` flag value and CLI dispatch path AC-15 is explicitly trying to bound. Make at least the AC-15 hot-file budget / `--all` / `--json` checks exercise `runCLI` or the compiled command against the scenario tempdir.
AUTHOR: Step 5.7 is now required and spells out three CLI-level tests against `scenariofixture.LoadScenario(t, "hot_file")` that use `runCLI` with NO `--budget` override (so the cobra flag default of 500 is what's bounded, not just the engine constant). The engine-level `TestHotFile_*` tests in Step 5.6 stay (they're cheaper and isolate the renderer), but the CLI tests in Step 5.7 are what actually fulfill AC-15. Updated the AC-15 → Phase 5 mapping in Success Criteria accordingly.
-->

- [ ] Step 5.8: Run full test suite: `cd auto-search && go test ./...`. Verify: green.
- [ ] Step 5.9: Run `make verify-fixtures` and confirm the new scenario JSONs are NOT picked up by the <1 MB live-snapshot guard (the guard targets `testdata/fixtures/auto-stack-snapshot/`, not `internal/cochange/testdata/scenarios/`). Document the path distinction in the helper's doc comment if not already obvious.
- [ ] Step 5.10: Commit: `test(011): phase 5 - JSON-seeded scenario fixtures and E2E budget-bound validation`

## Success Criteria

- [ ] `go build ./...` succeeds in `auto-search/`.
- [ ] `go vet ./...` clean in `auto-search/`.
- [ ] `go test ./...` green in `auto-search/`, including: every existing JSON conformance/integration test (now passing via `--json`); every new text renderer unit test; every new CLI text/flag/quickstart integration test.
- [ ] `make verify-fixtures` passes (fixture privacy + <1 MB size).
- [ ] Manual: `autosearch co-change <file>` (no flags) prints the compact text format; `--json` prints the existing envelope; `--limit` is rejected as unknown.
- [ ] Manual: `autosearch quickstart` section 9 explains the two-phase workflow and shows `--budget`/`--all`/`--json` (no `--limit`).
- [ ] AC mapping:
  - AC-1 → Phase 3 CLI dispatch + Phase 3 Step 3.4 (JSON tests still pass with `--json`).
  - AC-2/3/4 → Phase 2 Steps 2.4, 2.6.
  - AC-5 → Phase 1 Steps 1.1, 1.2, 1.7.
  - AC-6/7/8 → Phase 2 Step 2.5, 2.6.
  - AC-9 → Phase 2 Step 2.6 + Phase 3 Step 3.6.
  - AC-10 → Phase 1 Step 1.4 (Options.Limit) + Phase 3 Step 3.1, 3.7.
  - AC-11 → Phase 2 Step 2.5, 2.6.
  - AC-12 → Phase 1 Step 1.6, 1.7 + Phase 2 Step 2.6 + Phase 3 Steps 3.3-3.7 + Phase 4 Step 4.2.
  - AC-13 → Phase 4 Steps 4.1, 4.2.
  - AC-14 → Phase 5 Steps 5.1-5.5.
  - AC-15 → Phase 5 Step 5.6 (engine-level) + Step 5.7 (required CLI-level — the binding for the cobra flag default).
  - AC-12g → Phase 3 Step 3.4 (absence assertions).
  - AC-12h → Phase 3 Step 3.7a (surviving-flags smoke test).

## Open Questions

- (empty — all four pre-design questions answered; 13 Codex review threads across two rounds resolved or rejected)
