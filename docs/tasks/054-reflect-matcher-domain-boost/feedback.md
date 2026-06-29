# Feedback: Task 054

## Problems faced
1. **The boost would silently corrupt the dedupe gate** — `DetectDuplicate` calls `MatchRules` with the candidate rule's *own* domain as the filter, then gates on `MatchScore < 0.5` (calibrated to the [0,1] normalized lexical scale). Naively adding the boost makes that call always fire the boost and inflate scores, false-tripping dedupe. The fix (D-2) was to pre-filter to domain-intersecting rules then score with `nil` domain so no boost fires — keeping dedupe behavior-identical. This was the load-bearing dependency the context doc flagged; missing it would have shipped a real regression with no failing test to catch it by default.
2. **Scale faithfulness vs the validated variant** — the Python `idf-tag` reference ranks by *raw* lexical + *raw* IDF boost, while the Go matcher normalizes by `maxRaw = 4·len(keywords)`. Adding the raw boost *before* dividing by the per-call constant keeps sort order identical to the variant (monotonic), so `MatchScore` can now exceed 1.0 on the retrieve path — intended, and conformance proves the ordering matches exactly.
3. **`domainBoost` double-counts a duplicated domain tag.** Surprising at first, but it's *faithful to the frozen Python reference* (`boost_idf_tag` iterates `rule.domain` positionally) and required for Go↔Python conformance; duplicate domain tags are rejected upstream by `ValidateRule`. Pinned with an explicit by-design test rather than "fixing" it into divergence from the reference.

## Reflections
- **What was tricky?** Not the matcher edit (small, local) but keeping the *two non-retrieve consumers* correct: dedupe's [0,1] threshold and the Python conformance harness. The walking-skeleton ordering (P1 = matcher change + conformance proof first) retired the central integration risk before fanning out — the right call; conformance was green from phase 1.
- **What would you tell yourself at the start?** Trust the context doc — it precisely named the dedupe dependency, the frozen-baseline decision (D-1), and the scale subtlety. Every "surprise" was pre-documented.
- **What did you almost do but didn't?** Almost ran P2/P3/P4 in parallel (the DAG allows it). Didn't, because they share one worktree and overlap on `match.go`/`match_test.go`, and concurrent subagents in a shared worktree leak writes. Serial was the safe call and cost little.

## Useful context
- `context.md`'s "Callers of MatchRules" section — the single most valuable artifact; it located the dedupe gate and the [0,1] calibration before any code was read.
- Decision **D-1** (freeze `baseline.py` as the v1 system-of-record; pin conformance at `variants[SHIPPED]`) kept the A/B-able version registry intact and made the conformance repoint mechanical.
- The retrieval-eval harness builds a **hermetic throwaway store** because `auto reflect retrieve` appends an event — conformance runs never touch the live `.auto/reflect` store. Needed a fresh `.venv` (pytest only; zero runtime deps) and the `auto` binary built from `auto-cli/cmd/auto`.
