---
hash: "58e21064"
id: "41a6d2d5"
read_when: "implementing or reviewing the autosearch co-change compact output format requirements"
summary: "Requirements for making autosearch co-change output compact by default: token-budget cap, directory-tree-distance annotations, continuous inverse-fan-out weighting, and --json flag for verbose detail."
title: "Requirements: Task 011 — Autosearch Co-Change Compact Output"
---

# Task 011: Autosearch Co-Change Compact Output

## Problem

`autosearch co-change` (task 010) emits verbose JSON by default — per-row 15-significant-digit floats, repeated multi-paragraph commit bodies, empty `top_sessions`, and a `--limit` knob that caps by row count rather than context cost. For its primary consumer — an AI coding agent that runs co-change as a phase-one router before reading the actual files — that output is so expensive to ingest that the agent skips the tool or fans it out only sparingly, defeating the point (cheap router → fan out over a changeset → synthesize a coupling map before reading anything).

## Why This Matters

Co-change's value is being a cheap index that points at non-obvious cross-directory couplings (the kind grep wouldn't find). That value evaporates if a single call burns thousands of tokens describing findings the agent is about to re-derive by reading the files. Making the default output compact and budget-bounded is what lets the agent treat co-change as a routine first step rather than an exotic one.

## Goals

- Make compact text the default output of `autosearch co-change`; preserve current verbose detail behind `--json`.
- Each row reports: full repo-relative path, normalized 0–1 score (2 sig figs), co-commit count, and `d<n>` directory tree-distance between the related file and the seed.
- On `d>0` rows only, append one short `[<sha7> "<subject>"]` (most-recent representative co-change commit, subject truncated).
- Replace `--limit` (row cap) with `--budget <tokens>` (default ~500), and add `--all` to bypass the budget entirely.
- When truncation occurs, drop same-directory (`d0`) rows first so weak-but-distant couplings survive; append a one-line disclosure summarizing what was hidden (count + whether any were cross-dir).
- Switch large-commit handling from the current binary cutoff (drop commits with >50 files) to pure continuous inverse-fan-out weighting, so a high-signal coupling that only appears in big commits still surfaces.
- Net effect: a single call costs ~80–500 tokens regardless of how "hot" the seed file is, the lift-based ranking is preserved, and no distant coupling is ever silently dropped before same-dir siblings.

## Acceptance Criteria

**AC-1**: Compact text is the default; `--json` preserves task 010 schema
- Given: a user runs `autosearch co-change <path>` without `--json`
- When: results are emitted
- Then: stdout contains the compact text format defined in AC-2/AC-3 (header + ranked rows + optional disclosure); `--json` continues to emit the task 010 JSON envelope shape (`_meta`, `metadata`, `related_files`) at full precision, with two intentional schema deletions consistent with AC-5 and AC-10: `metadata.params_used.large_commit_cutoff` is removed (the binary cutoff no longer exists) and `metadata.params_used.limit` is removed (the `--limit` flag is gone). All other fields — including every per-related-file score component (`score`, `confidence_a_to_b`, `confidence_b_to_a`, `lift`) and the metadata block — are unchanged in name, type, and precision.

<!-- RESOLVED(P1): JSON preservation conflicts with planned params removal
REVIEW: AC-1 says `--json` keeps the existing task 010 JSON envelope unchanged. I checked `auto-search/internal/cochange/types.go`: `ParamsUsed` currently emits `decay_tau_days`, `large_commit_cutoff`, `min_co_commits`, `min_commits_a`, and `limit`. `solution.md` step 1 / Files remove `large_commit_cutoff` and `limit` from the JSON payload. Those requirements cannot both pass: existing JSON tests either keep those fields or the schema is intentionally revised. Please decide whether `--json` preserves the task 010 `params_used` shape, or AC-1/AC-12a should explicitly allow the breaking `params_used` change.
AUTHOR: AC-1 now explicitly names the two `params_used` field deletions as intentional schema revisions consequent to AC-5 (binary cutoff removed) and AC-10 (`--limit` removed), so the AC-12a JSON tests are expected to update by removing those two field assertions (and only those two). Everything else in the JSON envelope — `_meta`, the metadata block sans those fields, and every per-related-file field including all four score components — stays at full precision exactly as task 010 emits it.
-->

**AC-2**: Header (printed once)
- Given: results to render in text mode
- When: the header is emitted
- Then: it contains the seed file's repo-relative path on line 1, and on line 2 the total commits touching the seed and the date range (e.g. `9 commits, 2026-04-15 → 2026-05-08`). The header is emitted once per invocation; commit metadata is never repeated per row.

**AC-3**: Row format and columns
- Given: a related file passing the AC-3 thresholds from task 010 (`co_commits ≥ 3`)
- When: the row is rendered
- Then: it contains, in this order: repo-relative path; normalized score formatted to exactly 2 fractional digits in `[0.00, 1.00]` (e.g. `1.00`, `0.65`, `0.04`, `0.00`); co-commit count formatted as `N×`; and `d<n>` where `n` is the directory tree-distance between the row's path and the seed's path (number of "up" segments from the row's dir to the lowest common ancestor plus "down" segments back to the seed's dir; same dir = `d0`, sibling dir = `d2`). On `d>0` rows only, when `len(SampleCommits) > 0`, the row also includes a final `[<sha7> "<subject>"]` segment, where `<sha7>` is the 7-char short SHA of the most recent co-change commit between the two files and `<subject>` is its commit subject truncated to fit a soft cap (e.g. 60 chars, ellipsis on overflow). When a `d>0` row has empty `SampleCommits` (the `FillCandidateDetails` path can return empty for edge cases like fully-pruned history), the row is emitted without the bracket segment — the row is never dropped solely on that basis. `d0` rows MUST NOT include the `[sha "subject"]` segment.

**AC-4**: Score normalization
- Given: the scored, ranked candidate set for a query
- When: text-mode scores are emitted
- Then: each score is divided by the top-row raw score in the same result set so the top row reads `1.00` and all subsequent rows are in `[0.00, 1.00]`, formatted to exactly 2 fractional digits (per AC-3). JSON mode (`--json`) continues to emit the raw, unnormalized score at full precision. Ranking order is unchanged by normalization (it's a positive monotonic rescaling).

<!-- RESOLVED(P2): Score precision wording is inconsistent
REVIEW: AC-3/AC-4 say scores use "2 significant figures", but the examples (`1.00`) and `solution.md` use fixed two fractional digits via `strconv.FormatFloat(x, 'f', 2, 64)`. Those differ for small normalized scores; for example `0.0049` becomes `0.00` with two decimal places, not two significant figures. Choose either fixed two decimal places or true significant-figure formatting so renderer tests are deterministic.
AUTHOR: AC-3 and AC-4 now both say "exactly 2 fractional digits" with explicit examples spanning the range (1.00, 0.65, 0.04, 0.00), matching the `strconv.FormatFloat(x, 'f', 2, 64)` shape in solution.md. Very-weak couplings rendering as `0.00` is acceptable: those rows are also the first ones the AC-6 truncator drops, so they rarely survive to be displayed in practice.
-->

**AC-5**: Continuous large-commit weighting (no binary cutoff)
- Given: a commit touching N files
- When: that commit contributes to scoring
- Then: its contribution is weighted by inverse fan-out as already specified in task 010's AC-2 (`1 / log(1 + files_in_commit)`), AND the previous AC-3b binary cutoff that dropped commits with `files_in_commit > 50` is removed entirely. Large commits are downweighted, never excluded. The change applies uniformly to text mode, JSON mode, and the underlying scoring engine (single source of truth).

**AC-6**: Budget-based truncation with boring-first trim order
- Given: the rendered candidate rows, a `--budget <tokens>` value (default 500), and `--all` not set
- When: emission would exceed the budget
- Then: rows are trimmed to fit, scanning the rendered list in `d0`-first order — i.e. every `d0` row is dropped before any `d>0` row is touched. Within the same `d` group, the lowest-scoring row is dropped first. Truncation cuts only on whole-row boundaries (never mid-row). If the full list already fits within budget (the common case for a typical seed), it is emitted in full with no disclosure line.

**AC-7**: Truncation disclosure
- Given: one or more rows were trimmed under AC-6
- When: emission completes
- Then: a single trailing line is appended in the form `N more hidden (<characterization>) — run with --all`, where `<characterization>` is `all same-dir siblings` if every hidden row was `d0`, or `incl. <K> cross-dir` (with `K` = number of `d>0` rows hidden) otherwise. The line is omitted entirely when nothing was trimmed.

**AC-8**: Token-budget approximation
- Given: a `--budget` value
- When: the renderer measures output size against the budget
- Then: it uses an approximate token count of `ceil(runes / 4)` over the candidate rendered string (header + rows emitted so far + the prospective next row + the prospective disclosure line), where `runes` is the count returned by `utf8.RuneCountInString` (so the fixed UTF-8 glyphs `→`, `×`, `—`, `…` each count as one rune, not as their UTF-8 byte length). No external tokenizer dependency is added.

**AC-9**: `--all` bypasses the budget
- Given: `--all` is set
- When: results are emitted
- Then: every scored row is rendered regardless of `--budget`, and no disclosure line is appended. `--all` is mutually compatible with `--json` (in JSON mode the budget never applies; `--all` is a no-op).

**AC-10**: `--limit` is removed
- Given: a user passes `--limit <N>` on the command line
- When: the command parses flags
- Then: it errors out via the standard cobra unknown-flag path. The flag is removed from the binary, help text, and task-010 documentation references (no alias, no deprecation shim — per project rule "remove deprecated flags decisively").

**AC-11**: Metadata-only cases (insufficient history, unknown file) render cleanly in text mode
- Given: the seed file has too little history (task 010 AC-3c, `commits(A) < 5`)
- When: text mode is rendered
- Then: line 1 is the seed path (per AC-2); line 2 is `<N> commits` with no date range (the engine's `first_touched`/`last_touched` may be empty when there are too few commits to range-bound, in which case the ` → <date>` segment is omitted); line 3 is the literal text `insufficient history`. No rows, no disclosure.
- AND given: the seed path never appears in git history (task 010 AC-9, `total_commits == 0`, no warning)
- When: text mode is rendered
- Then: line 1 is the seed path (per AC-2); line 2 is the literal text `no history for this file` (the date range and commit count are omitted entirely because there are no commits to count). No rows, no disclosure.
- `--json` continues to emit the existing task 010 envelopes for both cases unchanged (`warning: "insufficient history"` for the first, no `warning` field and `total_commits: 0`/`related_files: []` for the second).

<!-- RESOLVED(P2): Text mode omits the existing unknown-file metadata case
REVIEW: Task 010 AC-9 still allows an input path that never appears in git history; current `cochange.Run` returns `total_commits: 0`, `related_files: []`, and no warning for that case. AC-11 covers only the insufficient-history warning path. With compact text now the default, the renderer needs an explicit output contract and test for unknown files; otherwise an implementation may print only a header with an empty date range or silently omit useful status.
AUTHOR: AC-11 now covers both metadata-only cases distinctly: the insufficient-history path emits `<N> commits` (with date range when present) followed by `insufficient history`, and the unknown-file path (task 010 AC-9, `total_commits == 0`) emits a dedicated `no history for this file` line under the seed path with no commit count or date segment to fabricate. AC-12b is updated alongside to require renderer tests for both cases.
-->

**AC-12**: Test coverage for both output modes
- Given: the existing task 010 cochange test suite (`internal/cochange/*_test.go`, `internal/cli/cochange_integration_test.go`, conformance fixtures)
- When: this task lands
- Then: (a) every task 010 test that asserts on JSON output is preserved and still passes against `--json` (no regression in JSON-mode coverage); (b) new unit tests cover the text renderer in isolation — header formatting, row formatting for both `d0` (no `[sha "subject"]`) and `d>0` (with `[sha "subject"]`) rows, score normalization (top row = `1.00`, others scaled), the `d<n>` tree-distance computation across same-dir / sibling-dir / different-top-level fixtures, subject truncation at the soft cap, the `insufficient history` case, and the `no history for this file` unknown-file case; (c) new unit tests cover the budget truncator — full-fit (no disclosure), trim-some (drops `d0` before `d>0`, disclosure line matches AC-7 wording for the all-`d0` and the mixed cases), trim-all (only header + disclosure remain), and `--all` bypass; (d) a new CLI integration test in `internal/cli/cochange_integration_test.go` exercises the default text path end-to-end against the same fixture used by the JSON integration test, asserting on the rendered string (including the disclosure line for a deliberately small budget); (e) a CLI test confirms `--limit` is rejected as an unknown flag; (f) a CLI test asserts the quickstart co-change section satisfies AC-13 (contains the two-phase wording, shows `--budget`/`--all`/`--json` examples, and does NOT contain `--limit`); (g) a JSON-mode test explicitly asserts that `metadata.params_used.large_commit_cutoff` and `metadata.params_used.limit` are absent from the decoded JSON map (not just unchecked — a future regression that re-adds either field must fail this test); (h) a CLI test exercises each surviving flag (`--decay-tau`, `--no-decay`, `--repo-id`, `--input`, `--request-id`) at least once in either JSON or text mode to confirm they remain wired after the CLI rewrite.

**AC-14**: Synthetic scenario fixture exercises the full parquet → render pipeline
- Given: a need to test scenarios that the live snapshot fixture cannot reliably reproduce (a 100-file commit, a deeply cross-directory coupling, a hot file with 30+ couplings, an unknown path, an insufficient-history file)
- When: the test suite runs
- Then: the cochange package ships a JSON-seeded scenario fixture mechanism. Each scenario is a hand-authored, human-reviewable JSON file under `auto-search/internal/cochange/testdata/scenarios/` (one file per scenario: `hot_file.json`, `cross_dir_coupling.json`, `large_commit.json`, `no_history.json`, `insufficient_history.json`). A helper in `auto-search/internal/cochange/fixturegen/` reads a scenario JSON, writes valid parquet for all four required datasets (`commits`, `commit_files`, `git_refs`, `git_repositories`) into a `t.TempDir()`, and returns the root path. Tests then invoke either `cochange.Run` or the CLI binary against that root via `--input <tempdir>` and assert end-to-end on the rendered output. The existing live-data snapshot fixture (`testdata/fixtures/auto-stack-snapshot/`) is unchanged and continues to back the conformance tests.

**AC-15**: End-to-end token-budget bound is verified on a hot-file scenario
- Given: the `hot_file.json` scenario (one seed file co-changing with ≥30 other files of varying tree-distance)
- When: the CLI runs against the scenario with default flags (no `--budget` override, no `--all`)
- Then: the rendered stdout's approximate token count (`ceil(utf8.RuneCountInString / 4)`) is ≤ 500. The same test, run with `--all`, produces output >500 tokens (proving `--all` actually bypasses the bound). A separate before/after test runs both `--json` and the default text mode against the same scenario and asserts the text-mode rune count is at most 25% of the JSON-mode byte length (the headline win the task promises).

**AC-13**: Quickstart explains the two-phase usage pattern
- Given: a user runs `autosearch quickstart` after this task lands
- When: the co-change section is read
- Then: the prose around the co-change examples explicitly describes the intended workflow — co-change is a *phase-one router* whose output is a cheap ranked shortlist of files worth opening, not a report the agent should read instead of the files. The section names the two phases ("run co-change to get the shortlist; then open the listed files") and explains *why* the compact default exists (so fan-out across a changeset is affordable). It MUST also show: (a) the default compact text invocation (no flags), (b) at least one `--budget` example with a non-default value, (c) an `--all` example for the rare hot-file case, and (d) a `--json` example labeled as "for programmatic / jq consumers". The phrase "two-phase" or "phase one" appears at least once in the prose, and `--limit` MUST NOT appear anywhere in the co-change section. The closing paragraph (currently "Output is JSON: a `metadata` header…") is rewritten to describe the compact text shape (header + one line per file with `<score> <N>× d<n>` columns; `[sha "subject"]` on cross-dir rows) and to point at `--json` for the full envelope.

## Out of Scope

- Multi-seed invocation (one call, N seed paths). Agents fan out by calling the command N times; the concise output is what makes that affordable.
- Changes to the scoring formula beyond removing the AC-3b binary cutoff (the underlying `confidence * log1p(lift)` ranking from task 010 AC-2 is preserved).
- A new programmatic surface for the compact form. JSON consumers keep using `--json`; the compact form is text-only and not a stable parseable format.
- Cross-query score calibration. Normalization is per-query relative to the top row.
- Exact-tokenizer integration. `chars/4` is the contract.
- Color, terminal-width adaptation, or TTY-aware padding. Output is UTF-8 (not ASCII), with a closed, fixed set of non-ASCII glyphs the renderer is permitted to emit: `→` (date-range separator in the header), `×` (co-commit count suffix on each row), `—` (em dash in the AC-7 disclosure line), and `…` (ellipsis when AC-3 subject truncation overflows the cap). No other non-ASCII characters in renderer-produced output. Per AC-8 the budget approximation counts runes, not bytes.

<!-- RESOLVED(P2): Output glyph contract conflicts with plain ASCII scope
REVIEW: AC-2/AC-3/AC-7 require or exemplify non-ASCII glyphs (`→`, `×`, and an em dash), and `solution.md` uses a Unicode ellipsis for subject truncation, but this scope line says the output is "Plain ASCII". Pick one output contract and mirror it in renderer tests; it also affects whether the budget approximation can safely use Go `len(s)` bytes or must count characters/runes.
AUTHOR: Replaced the "Plain ASCII" line with an explicit closed set of permitted non-ASCII glyphs (`→`, `×`, `—`, `…`) — matching the user's original proposal, which used these intentionally — and updated AC-8 to specify rune-based counting via `utf8.RuneCountInString` so the budget math stays correct for multi-byte glyphs. Renderer tests can assert on these exact glyphs without ambiguity.
-->

## Open Questions

- [x] All resolved via clarifying questions and Codex review (two rounds, 13 threads): score normalization = per-query divide-by-top; `--limit` removed entirely; multi-seed out of scope; budget measurement = `ceil(runes/4)` via `utf8.RuneCountInString`; JSON-seeded scenario fixtures cover the E2E gaps (AC-14/AC-15).
