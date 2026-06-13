---
hash: "2fe3e3ff"
id: "9ac4d724"
read_when: "implementing compact text output for autosearch co-change or modifying the co-change engine types"
summary: "Verified codebase context for the compact text-mode rewrite of autosearch co-change: key files for CLI dispatcher, engine types, large-commit cutoff plumbing, and scoring path with exact line numbers."
title: "Context: Task 011 — Autosearch Co-Change Compact Output"
---

# Context: Task 011

Codebase context for the compact text-mode rewrite of `autosearch co-change`. Pairs with [solution.md](./solution.md) and [requirements.md](./requirements.md).

## Key Files

### CLI dispatcher

- `auto-search/internal/cli/cochange.go:13-79` — full command definition. Flags currently registered: `--repo-id`, `--limit` (default 50), `--decay-tau` (default `90d`), `--no-decay`, `--input`, `--request-id`. `RunE` builds a `cochange.Options`, calls `cochange.Run`, JSON-encodes the `*Result` to `cmd.OutOrStdout()`. Errors are wrapped in `&ExitError{Code: 1, Err: err}` and surface on stderr.
- `auto-search/internal/cli/root.go:14-36, :59` — `ExitError{Code, Err}` type and the `errors.As` mapping that splits stdout/stderr. `newCoChangeCmd()` is registered on the root command at line 59.
- `auto-search/internal/cli/cli_integration_test.go` — defines `runCLI(t, args ...string) (stdout, stderr string, code int)` and `decodeJSON(t, data string) map[string]any`. These are the harness the task's CLI tests use.

### Engine types (the renderer's input)

- `auto-search/internal/cochange/types.go:10-90` — `Result{Meta, Metadata, RelatedFiles}`, `Meta{RequestID, Command, ElapsedMs}`, `Metadata` (16 fields incl. `ResolvedPath`, `TotalCommits`, `FirstTouched`, `LastTouched`, `Warning`), `RelatedFile{Path, Score, CoCommits, ConfidenceAtoB, ConfidenceBtoA, Lift, LastCoChange, TopAuthors, TopSessions, SampleCommits}`, `SampleCommitJSON{SHA, Date, Subject}`, and `ParamsUsed{DecayTauDays, LargeCommitCutoff, MinCoCommits, MinCommitsA, Limit}`.
- The two fields to delete from `ParamsUsed` per AC-1 are `LargeCommitCutoff` (line 86) and `Limit` (line 89).

### Engine — large-commit cutoff plumbing (all of this is removed per AC-5)

- `auto-search/internal/cochange/query.go:9-12` — `const LargeCommitCutoff = 50`.
- `auto-search/internal/cochange/query.go:174-182` — `const cutoffStr = "50"` and the `init()` panic-guard.
- `auto-search/internal/cochange/query.go:83` — `WHERE files_changed <= ` + cutoffStr in the `Wn` SUM query.
- `auto-search/internal/cochange/query.go:145` — `AND c.files_changed <= ` + cutoffStr in `rename_edge` of `pathCanonCTE`.
- `auto-search/internal/cochange/query.go:215` — `WHERE c.files_changed <= ` + cutoffStr in the `commit_decorated` join (covers per-path/per-candidate aggregation).
- `auto-search/internal/cochange/query.go:471` — `AND c.files_changed <= ` + cutoffStr in the RenamedFrom query.
- `auto-search/internal/cochange/cochange.go:87-93` — `ParamsUsed{... LargeCommitCutoff: LargeCommitCutoff, ... Limit: limit}`. Both field assignments deleted.
- `auto-search/internal/cochange/cochange.go:48-49, :156` — `limit := opts.Limit; limit = max(limit, 0)` and `scored := ScoreAndRank(agg, limit)`. With `--limit` gone, the orchestrator calls `ScoreAndRank(agg, 0)` (no cap) and rendering takes care of trimming for text mode.

### Engine — scoring path that stays

- `auto-search/internal/cochange/score.go:65-89` — `ScoreAndRank` filters `CoCommits < MinCoCommits` (3), sorts by `Score` descending (tiebreak `Path` ascending), applies `limit > 0`. The sort path is preserved; only the limit clause becomes inert.
- `auto-search/internal/cochange/score.go:43-55` — `scoreCandidate` computes `confidence_a_to_b = Wab/Wa`, `confidence_b_to_a = Wab/Wb`, `lift = (Wab*Wn)/(Wa*Wb)`, `score = confidence_a_to_b * log1p(lift)`. Inverse-fan-out weighting is applied earlier at load time in `load.go`, so this is unchanged.
- `auto-search/internal/cochange/query.go:118-126` — `FillCandidateDetails` runs *after* `ScoreAndRank` and populates each surviving candidate's `SampleCommit` slice via `fillCandidateDetail` at lines 381-406 (`SELECT c.short_id, c.author_date, c.message_truncated ... ORDER BY c.author_date DESC LIMIT 3`). `Candidate.SampleCommit[0]` is the most recent — that's the `<sha7> "<subject>"` source for AC-3.
- `auto-search/internal/cochange/query.go:52-56` — `type SampleCommit{ SHA string; Date int64; Subject string }`. The text renderer reads `SampleCommit[0].SHA[:7]` and `SampleCommit[0].Subject`.

### Engine — output helpers

- `auto-search/internal/cochange/cochange.go:212-217` — `isoDate(unixMs int64) string` (UTC, `2006-01-02` layout). The text header reuses `metadata.FirstTouched` / `metadata.LastTouched` which are already ISO date strings.
- `auto-search/internal/cochange/cochange.go:269-321` — `languageForPath` (extension → language string). Not used by the text renderer.
- The cochange package has **no existing render/format file** — `render.go` is new work.

### Quickstart

- `auto-search/internal/cli/quickstart.go:197-220` — the full "Find files that change together (co-change)" section, including the `--limit 10 --no-decay` example at line 208 and the closing prose block at 217-220 that describes the JSON output. AC-13 rewrites this whole section.

### Test surface (must stay green)

- `auto-search/internal/cli/cochange_integration_test.go:67-89` — `TestCoChangeHelpListsAllFlags` asserts every flag is in `--help` (includes `--limit` today; updated to drop it + add `--budget`, `--all`, `--json`); `TestQuickstartMentionsCoChange` asserts quickstart contains `co-change`.
- `auto-search/internal/cli/cochange_integration_test.go:93-311` — eight JSON-asserting tests (`TestCoChangeCLIKnownFileJSON`, `TestCoChangeCLIExistingAndNonExistentPaths`, `TestCoChangeCLIOutsideRepo`, `TestCoChangeCLIMissingParquet`, `TestCoChangeCLINoOriginRemote`, `TestCoChangeCLINoRepoMatch`). Each invokes `runCLI` and parses stdout via `decodeJSON`; they all need `--json` added to the args list.
- `auto-search/internal/cochange/conformance_test.go:241-242` — `params_used.large_commit_cutoff` assertion to remove.
- `auto-search/internal/cochange/conformance_test.go` — five tests total (`TestConformance_KnownFile`, `TestConformance_LiveRemoteResolution`, `TestConformance_UnknownFile`, `TestConformance_InsufficientHistory`, `TestConformance_FixtureSizeUnder1MB`) — the last is the <1 MB fixture guard that gates the test target.
- `auto-search/internal/cochange/score_test.go:142-166` — `TestScore_LargeCommitDropped` asserts that an oversized commit's contributions are dropped. Replaced (per solution Files row) with `TestLargeCommitContributesContinuously` that asserts a 100-file commit still contributes — small but non-zero.

### Test fixtures

- `auto-search/testdata/fixtures/auto-stack-snapshot/{commits,commit_files,git_refs,git_repositories}/*.parquet` — the integration test fixture. Both the JSON and the new text-mode integration tests reuse it via `snapshotFixtureRoot(t)` and `snapshotRepoID(t, root)` at `cochange_integration_test.go:16, :35`.

## Patterns

### Project CLI conventions (CLAUDE.md)

- "Prefer explicit CLI surfaces: one clear command and explicit flags; avoid ambiguous aliases."
- "Remove deprecated flags decisively rather than carrying long-term aliases that make behavior unclear." → directly authorizes AC-10 removing `--limit` with no shim.
- "Default command output to JSON unless a command explicitly documents a different default; provide human-readable text mode via flags where needed." → task 011 is the explicit documented exception: text default, `--json` opt-in.
- "In JSON mode, keep `stdout` strictly parseable payload data only; send diagnostics/errors to `stderr`." → preserved by routing errors through `ExitError` (already done by `cochange.go:49-63`).
- "Use fail-fast for invalid CLI usage (flag conflicts, bad args) through standard command-framework errors." → cobra's unknown-flag error path handles `--limit` rejection for free (AC-10).

### JSON envelope (already canonical)

The `{_meta, metadata, related_files}` envelope at `types.go:10-14` is the shared autosearch shape (also used in `cli/stats.go`, `cli/search.go`). Task 011 only removes two fields from `metadata.params_used`; the envelope shape is untouched.

### Text-output precedent in autosearch

- `auto-search/internal/cli/quickstart.go` emits multi-line Markdown directly to stdout — the closest existing precedent for a non-JSON command output.
- `auto-search/internal/cli/search.go` introduced a `--text` flag (commit `bb7586a`, May 31 2026) that switches the messages-scope search hits to a skim-friendly table and is rejected with a fail-fast error on the sessions scope. That's the convention for "opt-in text"; task 011 inverts it (text default, `--json` opt-in) because the user is an LLM agent rather than a human at a terminal.

### Minimal runtime deps (memory: `feedback_minimal_runtime_deps`)

The project's strong preference is zero new runtime dependencies, prefer reusing already-embedded pure-Go libs. The renderer uses only `strings`, `strconv`, `fmt`, and `unicode/utf8` from stdlib — no tokenizer, no terminal-formatter package.

### Test execution

- Root `Makefile:165-169` — `test` target runs `(cd <project> && go test ./...)` per project, gated on `verify-fixtures` (the <1 MB fixture privacy/size guard at lines 181-188).
- Cochange-specific subset: `cd auto-search && go test ./internal/cochange/... ./internal/cli/...` is the natural per-phase verification command. Full run: `cd auto-search && go test ./...`.

## Related Tasks

- **Task 010** (`docs/tasks/010-autosearch-co-change/`) shipped the engine, JSON envelope, and CLI surface that 011 reshapes. PR #44, commit `9dff600`. Task 011 modifies three of its acceptance criteria:
  - Task 010 AC-3b ("commits with `files_in_commit > 50` are dropped entirely") → Task 011 AC-5 (binary cutoff removed; continuous weighting is sole handling).
  - Task 010 AC-8 ("`--limit N` overrides the cap, default 50") → Task 011 AC-10 (`--limit` removed entirely).
  - Task 010 AC-5 (`params_used` includes `large_commit_cutoff` and `limit`) → Task 011 AC-1 (those two fields removed from JSON envelope; everything else preserved).
- **Task 010 feedback** (`docs/tasks/010-autosearch-co-change/feedback.md`) flagged two relevant pitfalls: (a) shared denominators (`Wn`) must honour the same filters as the per-pass aggregations — when removing the cutoff, every `WHERE files_changed <= 50` site must come out together to keep `Wn` consistent; (b) detail fetch (`FillCandidateDetails`) is the hot path on real datasets and runs only on post-filter survivors — this stays true; the renderer never re-queries.
- **Task 008** (`commit↔session linkage`) made `top_sessions` populated when commits carry trailers; the renderer must not assume `TopSessions` is non-empty (current real-dataset behaviour produces empty slices). Text rows don't display sessions anyway, so this is a no-op for AC-3 but worth noting for AC-13 prose ("pivot into `autosearch session get`" remains true and is still worth mentioning in the rewritten quickstart).
