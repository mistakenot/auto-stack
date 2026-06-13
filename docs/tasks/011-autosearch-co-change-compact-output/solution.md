---
hash: "0a2540b5"
id: "7cf8239a"
read_when: "implementing compact text rendering for autosearch co-change or understanding the render.go budget truncation design"
summary: "Solution for compact co-change text output: remove binary large-commit cutoff, add a text renderer with budget truncation and boring-first trim, normalize score display, and add --text flag to the CLI."
title: "Solution: Task 011 — Autosearch Co-Change Compact Output"
---

# Solution: Task 011

<!-- REJECTED(P1): Missing context and execution plan docs
REVIEW: I checked `docs/tasks/011-autosearch-co-change-compact-output/` and it currently contains only `requirements.md` and `solution.md`. The `review-task` workflow expects `context.md` and `plan.md` as well, so this task cannot be reviewed as ready for execution: there is no verified codebase context with snippets/line references and no phase order or test-command plan to validate. Add those docs before treating this as implementation-ready.
AUTHOR: This task is in the solution stage of the task-planning pipeline (new-task → new-solution → new-plan → request-codex-review at each stage). `context.md` and `plan.md` are intentionally absent — they get created by `/new-plan` after the solution is reviewed and approved. The review at this stage is scoped to whether the solution shape and AC mapping are sound, which the rest of this review correctly performs. Same pattern as the rejected P1 on task 010's requirements review.
-->

## Approach

1. **Remove the binary large-commit cutoff from the engine** (AC-5). In `internal/cochange/query.go`, drop the `WHERE c.files_changed <= 50` clauses (the `cutoffStr` constant and its init check), and delete `LargeCommitCutoff` plus the `large_commit_cutoff` field in `ParamsUsed` / the JSON envelope. The continuous `1 / log(1 + files_changed)` weight applied at load time (already in place) becomes the *only* large-commit handling. Update the AC-3b conformance test to assert that a 100-file commit still contributes (just with low per-edge weight).

2. **Add a text renderer in the engine package** at `internal/cochange/render.go` so it can consume `*Result` (the existing AC-4/AC-5 struct) directly and stay testable in isolation. Public API:
   ```go
   type RenderOptions struct{ Budget int; All bool }   // Budget in approx tokens; 0 = default 500
   func Render(r *Result, opts RenderOptions) string   // text form (header + rows + optional disclosure)
   ```
   Internals split into three small pure functions to keep tests focused:
   ```go
   func renderHeader(meta *Metadata) string                        // 2-line header, AC-2
   func renderRow(r *RelatedFile, seedPath string, normScore float64) string  // AC-3 — takes the full seed path, not its dir
   func treeDistance(rowPath, seedPath string) int                 // AC-3 d<n>; drops basenames internally
   ```
   Score normalization (AC-4) is computed once over the slice as `raw / rawTop` then formatted via `strconv.FormatFloat(x, 'f', 2, 64)` (always 2 fractional digits, top row reads `1.00`). Subject truncation: hard cap at 60 chars, trailing ellipsis `…` (single rune) on overflow; SHA = first 7 chars of the most-recent sample commit (already populated by `FillCandidateDetails`).

<!-- RESOLVED(P2): renderRow parameter conflicts with treeDistance contract
REVIEW: AC-3's distance is between the row path's directory and the seed path's directory. The proposed `treeDistance(a, b)` drops basenames from both arguments, but `renderRow` takes `seedDir`; passing `seedDir` to a function that drops a basename will misclassify same-directory rows (for example, seed `src/hot.go`, row `src/other.go`, and `seedDir == "src"` treats the seed directory as a basename and yields `d1` instead of `d0`). Make the renderer pass the full seed path to `treeDistance`, or change the helper contract to accept directories explicitly.
AUTHOR: Both signatures now name the full seed path (`seedPath`, not `seedDir`); `treeDistance(rowPath, seedPath string)` is contractually defined to drop the basename from each arg internally. `renderRow` passes the seed path through unmodified. This eliminates the double-drop hazard the reviewer flagged. The render.go outline below and the plan are updated to match.
-->

3. **Budget truncation with boring-first trim order** (AC-6/7/8) inside `Render`:
   - Pre-render every row to a string and tag each with `(d int, score float64)`.
   - If `opts.All`, emit header + every row, no disclosure. Done.
   - Compute `approxTokens(s) = (utf8.RuneCountInString(s) + 3) / 4` and `budget = opts.Budget` (default `500`). Byte-length is wrong here: the renderer deliberately emits `→`, `×`, `—`, `…` (per the AC-3/AC-7 contract and the out-of-scope glyph list), which are 2-3 UTF-8 bytes each but exactly one rune.
   - Build emit set starting with all rows. While `approxTokens(currentOutput + prospectiveDisclosure) > budget` and the set is non-empty: drop the *next victim* using sort key `(d ascending, score ascending)` — i.e. lowest-d, lowest-score first. This keeps every `d>0` row until every `d=0` row is gone, and within a `d`-group drops the weakest rows first.
   - Compose disclosure as `N more hidden (<characterization>) — run with --all` where `<characterization>` is `all same-dir siblings` iff every hidden row has `d == 0`, else `incl. <K> cross-dir` with `K = count(hidden where d > 0)`. Append only when N > 0.
   - The "prospective disclosure" included in the budget check uses the same composition so the final string is provably under budget.

<!-- RESOLVED(P2): Budget helper still uses byte length in solution
REVIEW: AC-8 and plan Step 2.3 require `utf8.RuneCountInString` because renderer output deliberately includes `→`, `×`, `—`, and `…`. This solution step and the `render.go` outline below still specify `(len(s)+3)/4`; `len` counts UTF-8 bytes, not runes, so budget tests would use a different contract and can over-trim rows containing the permitted glyphs. Update the solution and outline to the rune-count formula.
AUTHOR: Step 3 now spells out `(utf8.RuneCountInString(s) + 3) / 4` and explains why byte length is wrong; the `render.go` outline (below) and the plan (Step 2.3) already use the rune-count form. All three references are now consistent.
-->

4. **Insufficient-history text path** (AC-11). When `Result.Metadata.Warning == "insufficient history"`, `Render` emits header + a single `insufficient history` line and stops (no rows, no disclosure).

5. **Replace CLI flags** (AC-1, AC-9, AC-10) in `internal/cli/cochange.go`:
   - Remove `--limit` entirely. The cobra command no longer registers it; passing `--limit` falls through to cobra's unknown-flag error, exit code 1.
   - Add `--budget int` (default `500`) and `--all bool` (default `false`).
   - Add `--json bool` (default `false`). When set, marshal the existing `*Result` as today. Otherwise call `cochange.Render(result, RenderOptions{Budget: budget, All: all})` and write to stdout. Errors keep going to stderr; stdout in text mode is the rendered string (a trailing newline only).
   - Remove the `Limit` field from `cochange.Options` and the `LargeCommitCutoff`-related plumbing in `cochange.Run`. The engine no longer caps rows — text-mode truncation is rendering-only; JSON mode emits everything that survived `MinCoCommits`.

<!-- RESOLVED(P2): `--limit` removal misses user-facing docs cleanup
REVIEW: AC-10 requires `--limit` to be removed from help text and task-010 documentation references. I checked `auto-search/internal/cli/quickstart.go:207`: quickstart still advertises "Limit results and disable time decay" with `autosearch co-change internal/cli/root.go --limit 10 --no-decay`. The solution lists `cochange.go` and `cochange_integration_test.go` for CLI work but does not include quickstart or task-doc cleanup, so an executor could remove the flag while leaving user-facing docs stale. Add the quickstart/task-doc updates and a test/assertion that quickstart no longer shows co-change `--limit`.
AUTHOR: Added `auto-search/internal/cli/quickstart.go` to the Files list with a concrete change: replace the `--limit 10 --no-decay` example with a `--budget` / `--all` / `--json` example block that matches the new CLI surface. Extended the existing `TestQuickstartMentionsCoChange` CLI test (in `cochange_integration_test.go`) to also assert the quickstart output does NOT contain `--limit`, so a stale example will fail CI. Task-010 docs (requirements.md, solution.md, plan.md, feedback.md) are intentionally NOT modified — they are historical records of the task-010 state and per project convention historical task docs are not retroactively edited; AC-10's "task-010 documentation references" is read as user-facing/help docs outside the historical task folder.
-->

6. **Test fixtures.** Reuse the existing `internal/cochange` synthetic fixture helpers for unit tests of `Render` / `treeDistance` / normalization / budget truncation. The CLI integration test reuses `testdata/fixtures/auto-stack-snapshot` and the helpers in `cochange_integration_test.go`.

## Files

```
~ auto-search/internal/cochange/query.go               # remove cutoffStr, init check, AC-3b WHERE filters; canon CTE no longer filters by files_changed
~ auto-search/internal/cochange/score.go               # comment update only (AC-3b note); no behavioural change
~ auto-search/internal/cochange/types.go               # remove ParamsUsed.LargeCommitCutoff (JSON breaking change consistent with AC-5)
~ auto-search/internal/cochange/cochange.go            # remove LargeCommitCutoff plumbing in ParamsUsed{...}; drop Options.Limit; no longer calls ScoreAndRank with a cap (pass 0)
+ auto-search/internal/cochange/render.go              # Render, renderHeader, renderRow, treeDistance, score normalization, budget truncation
+ auto-search/internal/cochange/render_test.go         # unit tests for the above (AC-2/3/4/6/7/8/9/11)
~ auto-search/internal/cochange/conformance_test.go    # remove AC-3b assertion on params_used.large_commit_cutoff; add assertion that a 100-file commit still contributes
~ auto-search/internal/cochange/score_test.go          # delete the AC-3b "files_changed > 50 dropped entirely" test; add a test asserting big-commit contribution is small but non-zero (continuous weighting)
~ auto-search/internal/cli/cochange.go                 # remove --limit; add --budget, --all, --json; dispatch JSON vs text
~ auto-search/internal/cli/quickstart.go               # rewrite the entire "Find files that change together (co-change)" section (lines ~197-220): two-phase usage prose, new example block (`--budget` / `--all` / `--json` instead of `--limit`), and a closing paragraph that describes the compact text shape and points at `--json` for the full envelope
~ auto-search/internal/cli/cochange_integration_test.go # update existing tests to pass --json (preserves AC-12a); add new text-mode tests (AC-12d/e); extend TestQuickstartMentionsCoChange to assert quickstart no longer contains co-change `--limit`; add explicit-absence assertions for the two deleted params_used fields (AC-12g); add a surviving-flags smoke test (AC-12h)
+ auto-search/internal/cochange/scenariofixture/scenariofixture.go  # NEW importable package (fixturegen is `package main` and cannot be imported). Re-uses the parquet struct shapes from fixturegen/main.go (`CommitFixture`, `CommitFileFixture`, `GitRepositoryFixture`, `GitRefFixture`) and wires `parquet-go`'s writer the same way. Exports `LoadScenario(t *testing.T, name string) (rootDir string)` which resolves `internal/cochange/testdata/scenarios/<name>.json`, writes parquet to `t.TempDir()/<dataset>/<dataset>.parquet` for the four required datasets (layout matches the checked-in snapshot fixture so `etlscan.DiscoverDatasets` finds them), and returns the dir
+ auto-search/internal/cochange/scenariofixture/scenariofixture_test.go  # round-trip sanity test: write a 2-commit scenario, read back via etlscan.ReadCommitsSlim / ReadCommitFilesSlim, assert row counts match
+ auto-search/internal/cochange/testdata/scenarios/hot_file.json              # seed: 1 file co-changing with 30+ others across mixed d-levels (validates AC-15 budget bound + AC-6 trim order under pressure)
+ auto-search/internal/cochange/testdata/scenarios/cross_dir_coupling.json    # seed: 1 file with one strong d>=2 coupling that would lose to d0 siblings under a row-count cap (validates AC-6 d0-first trim order survives a real query)
+ auto-search/internal/cochange/testdata/scenarios/large_commit.json          # one 100-file commit + a handful of 2-file commits (validates AC-5 continuous weighting end-to-end, not just at the score-unit level)
+ auto-search/internal/cochange/testdata/scenarios/no_history.json            # repo with commits, but the seed path is not in any of them (validates AC-11 `no history for this file` text path through real parquet)
+ auto-search/internal/cochange/testdata/scenarios/insufficient_history.json  # seed file in 2 commits only (< MinCommitsA=5) (validates AC-11 `insufficient history` text path through real parquet)
+ auto-search/internal/cochange/render_e2e_test.go     # E2E tests that LoadScenario → run cochange.Run → Render; covers AC-14 (parquet round-trip works), AC-15 (token bound on hot file, --all bypass, text-vs-json size ratio), and the AC-11 cases through real parquet
```

Bare-bones outline of `render.go`:

```go
package cochange

type RenderOptions struct {
    Budget int  // approx tokens; <= 0 means default (500)
    All    bool
}

const defaultBudget = 500
const subjectCap = 60

func Render(r *Result, opts RenderOptions) string { ... }

// renderHeader: full 2-line header used when m.TotalCommits > 0
//   line 1 = m.ResolvedPath
//   line 2 = "<N> commits[, <first> → <last>]"  (date range omitted if empty)
// For m.TotalCommits == 0 the orchestrator emits only line 1 + "no history for this file" (AC-11).
func renderHeader(m *Metadata) string { ... }

// renderRow: "<path>  <score>  <N>×  d<n>[  [<sha7> \"<subject>\"]]"
// score formatted with strconv.FormatFloat(norm, 'f', 2, 64).
// [sha "subject"] segment omitted when d == 0 OR when len(rf.SampleCommits) == 0.
// Takes the full seed path (NOT the seed dir) so treeDistance can drop basenames symmetrically.
func renderRow(rf *RelatedFile, seedPath string, norm float64) string { ... }

// treeDistance: number of "up" segments from dir(a) to LCA + "down" to dir(b).
// Drops the basename from both arguments before computing the LCA. Callers pass full paths.
// Operates purely on forward-slash directory components; same dir == 0; sibling dir == 2.
func treeDistance(rowPath, seedPath string) int { ... }

// approxTokens: (utf8.RuneCountInString(s) + 3) / 4 (ceil of runes/4).
// Rune count, NOT byte length — the renderer emits multi-byte glyphs (→ × — …) that must each count as 1.
func approxTokens(s string) int { ... }

// truncSubject: trim s to <= subjectCap runes, append "…" on overflow.
func truncSubject(s string) string { ... }
```

## Test Coverage

| AC    | Test Type     | File                                                       |
|-------|---------------|------------------------------------------------------------|
| AC-1  | CLI integ     | `auto-search/internal/cli/cochange_integration_test.go` — text mode is default, `--json` selects JSON |
| AC-2  | unit          | `auto-search/internal/cochange/render_test.go::TestRenderHeader` |
| AC-3  | unit          | `render_test.go::TestRenderRow_D0_NoSampleCommit`, `TestRenderRow_DPositive_IncludesSampleCommit`, `TestTreeDistance` (same-dir/sibling/different-top-level fixtures) |
| AC-4  | unit          | `render_test.go::TestScoreNormalization_TopRowIsOne` |
| AC-5  | unit + conformance | `auto-search/internal/cochange/score_test.go::TestLargeCommitContributesContinuously`, `conformance_test.go` updated (large_commit_cutoff field gone) |
| AC-6  | unit          | `render_test.go::TestBudget_FullFit_NoDisclosure`, `TestBudget_TrimDropsD0BeforeDPositive`, `TestBudget_TrimAllRows` |
| AC-7  | unit          | `render_test.go::TestDisclosure_AllSameDirWording`, `TestDisclosure_InclCrossDirWording`, `TestDisclosure_OmittedWhenNothingTrimmed` |
| AC-8  | unit          | `render_test.go::TestApproxTokensCharsDiv4` |
| AC-9  | unit + CLI    | `render_test.go::TestRender_AllBypassesBudget`; `cochange_integration_test.go::TestCoChangeCLI_AllBypassesBudget` |
| AC-10 | CLI integ     | `cochange_integration_test.go::TestCoChangeCLI_LimitFlagRejected`; `TestQuickstartMentionsCoChange` extended to assert quickstart does NOT contain `--limit` |
| AC-11 | unit          | `render_test.go::TestRender_InsufficientHistoryTextLine` |
| AC-12a | CLI integ    | All existing `TestCoChangeCLI*JSON*` tests pass `--json` and continue asserting the AC-4/AC-5 JSON schema |
| AC-12d | CLI integ    | `cochange_integration_test.go::TestCoChangeCLIKnownFileText` — same fixture as the JSON test, asserts on the rendered string and exercises a small `--budget` to force a disclosure line |
| AC-13 | CLI integ     | `cochange_integration_test.go::TestQuickstartCoChangeSection` — asserts the quickstart contains "phase one"/"two-phase", at least one `--budget` example, one `--all` example, one `--json` example, and contains no `--limit` substring within the co-change section. Extends `TestQuickstartMentionsCoChange` rather than replacing it. |
| AC-14 | E2E           | `cochange/render_e2e_test.go::TestScenario_LoadAndRender_*` — one subtest per scenario file; round-trips JSON → parquet → `cochange.Run` → `Render` and asserts on the rendered string |
| AC-15 | E2E           | `cochange/render_e2e_test.go::TestHotFile_TokenBudgetBound` (≤500 token output at default budget), `TestHotFile_AllBypassesBudget` (>500 tokens with `--all`), `TestHotFile_TextVsJSONSize` (text rune count ≤ 25% of JSON byte length) |
| AC-12g | CLI integ    | `TestCoChangeCLIKnownFileJSON_NoDeletedParamsFields` — decodes JSON, asserts `params_used` has no `large_commit_cutoff` and no `limit` keys |
| AC-12h | CLI integ    | `TestCoChangeCLI_SurvivingFlagsStillWork` — single test that invokes the command with `--decay-tau 30d --no-decay --repo-id <id> --input <root> --request-id smoke` and asserts exit code 0, `_meta.request_id == "smoke"`, and `metadata.params_used.decay_tau_days == 30` |

## Out of Scope

- (from requirements) Multi-seed invocation, cross-query score calibration, exact tokenizer, color/TTY adaptation, programmatic surface for compact form.
- **Technical**: No changes to repo resolution, parquet loading, rename CTE structure, decay model, or `FillCandidateDetails`. No new flags beyond `--json`, `--budget`, `--all`. No change to the `_meta` envelope shape. No change to JSON-mode field precision or ordering.

## Rejected Alternatives

- **Render in the CLI package, not the engine**: keeps the engine pure-data, but makes the renderer harder to unit-test against synthetic `*Result` fixtures and forces test files to live under `internal/cli/`. Placing `render.go` next to its data type (`types.go`) is the minimum-friction option.
- **Two-pass renderer that re-renders the row list after dropping each victim**: simpler to read but O(n²) in row count for hot files. Pre-render once and subtract token counts as rows are dropped — same complexity, less waste.
- **Keep `LargeCommitCutoff` as a tunable threshold defaulting to a much higher value (e.g. 500)**: tempting because it preserves the existing param shape, but AC-5 is explicit ("removed entirely"), and continuous weighting already handles the noise. Keeping the cutoff invites someone to re-tune it and silently re-erase high-signal couplings.
- **Use a real tokenizer (tiktoken-go) for the budget check**: more precise but adds a runtime dep against the project's minimal-deps rule (see memory `feedback_minimal_runtime_deps`), and the budget is a soft target — `chars/4` is well within the precision the budget needs.
- **Soft-deprecate `--limit` (warn but accept it)**: violates the project rule "remove deprecated flags decisively rather than carrying long-term aliases that make behavior unclear" in CLAUDE.md.
