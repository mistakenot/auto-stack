# Feedback: Task 004

## Problems faced
1. Scanner/builder import-kind string mismatch -- the TypeScript scanner emits `"type"` and `"side-effect"` but the builder expects `"type_only"` and `"side_effect"`. Unit tests passed because they used synthetic edges with the builder's expected strings, and golden files baked in the broken behavior. Fixed by adding `canonicalizeKind()` normalization in `codegraph/build.go` at the edge-building seam.
2. Budget enforcement against partial pack -- the builder's format estimator was called on a pack without relationships, reading order, or guidance populated, so it accepted too many candidates. Fixed by recomputing derived sections before each budget check via `refreshDerived()`.
3. Main diverged during execution -- the Go import graph PR merged to main after the worktree was created, causing rebase conflicts in `code_graph.go`. Resolved via merge commit, updating `codegraph/build.go` to support both TypeScript and Go.

## Reflections
- The scanner-to-builder string contract was the most important bug. It was invisible because synthetic tests and generated goldens formed a closed loop that never touched the real scanner. Integration tests using the actual fixture + ast-grep scanner would have caught this immediately.
- The budget enforcement issue was subtle -- accounting for file content but not the rendering overhead from relationships/guidance sections. The solution spec said "estimate against the final rendered payload" but the implementation only partially followed through. The Omitted section still adds post-selection overhead.
- Starting the worktree from a stale main caused the merge conflict. The CLAUDE.md instruction to `git fetch && git pull` before creating worktrees exists for this reason.

## Useful context
- `internal/scanner/typescript.go:79,237,241` is the source of truth for import-kind strings -- any new consumer must normalize against those.
- The `FormatEstimator` callback pattern works well for format-aware budget enforcement but requires that the pack model is fully populated (including derived sections) when the estimator runs.
- Tarjan's SCC algorithm in the builder is deterministic when nodes are processed in sorted order -- important for golden test stability.
