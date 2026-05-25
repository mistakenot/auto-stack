# Feedback: Task 001

## Problems faced
1. ast-grep treats `--lang=ts` and `--lang=tsx` as separate language modes -- imports inside `.tsx` files were silently missed until we added scanning with both modes. Caught by unit tests in Phase 5.
2. Re-export pattern `export { $_ } from "$_"` only matches single-name exports -- caught in PR review, replaced with `export { $$$ } from "$_"` plus star and type-export variants.
3. PR showed 140+ unrelated files because the branch was created from local main which was 3 commits ahead of origin/main. Fixed by pushing main before creating the PR, then patching the PR base via API.

## Reflections
- The scanner needed more thought on ast-grep's language mode behavior. Testing against real fixtures early (Phase 5) was critical to finding the tsx bug.
- The coordinator-subagent pattern worked well for this task. Each phase was self-contained enough to delegate. Phase 5 (fixtures + tests) was the most complex because it needed to exercise the full pipeline and found a real bug.
- Almost skipped the `--globs` exclusion for node_modules since "correctness is fine" (filtered in buildGraph), but the perf impact on real codebases would be significant.

## Useful context
- ast-grep `--globs '!dir'` syntax for path exclusion (not documented prominently)
- ast-grep `$$$` matches multiple items vs `$_` matches exactly one -- critical for re-export patterns
- GitHub doesn't auto-update PR base SHA when the base branch moves forward; use `gh api -X PATCH` to force recalculation
