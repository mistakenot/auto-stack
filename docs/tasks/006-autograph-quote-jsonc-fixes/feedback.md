---
hash: "bdb59500"
id: "68cc9e94"
read_when: "reviewing lessons from the quote/JSONC fix task or understanding canonical import kind names and JSONC stripping edge cases"
summary: "Post-task feedback from the autograph quote/JSONC fix task: golden file kind-name mismatch after canonicalization, single-pass stripJSONC rewrite to prevent string content corruption, and parallel phase dispatch lessons."
title: "Feedback: Quote and JSONC Fixes (Task 006)"
---

# Feedback: Task 006

## Problems faced
1. Golden file assertions used pre-canonicalization import kind names (`type`, `side-effect`, `blank`) -- the Phase 2 `Build` signature change triggered a golden file regeneration that surfaced this mismatch. Had to update both existing golden files and the e2e test assertions to use canonicalized names (`type_only`, `side_effect`).
2. `stripJSONC` pass 2 (regex) ran over string contents -- the two-pass approach was reviewed and caught before merge. Rewrote to single-pass to avoid corrupting string values containing `, }` or `, ]`.

## Reflections
- The parallel dispatch of Phase 1 and Phase 2 worked cleanly since the scanner and resolver are independent subsystems. Phase 3 (e2e) correctly depended on both and ran serially after.
- The dedup key `(file, importPath, kind)` in the scanner means you can't test multiple re-export patterns pointing at the same file. The plan caught this during review and used distinct target files per re-export variant.
- Threading `io.Writer` through the call chain was the most tedious part -- 5 files needed updating for a single constructor parameter change. The plan's explicit caller enumeration prevented missed callsites.

## Useful context
- `auto-graph/internal/codegraph/build.go` `canonicalizeKind()` maps scanner kinds to canonical output kinds -- existing golden files used raw scanner kinds, not canonical ones.
- The `TestEdgeReferentialIntegrity` e2e test auto-discovers fixtures with `tsconfig.json`, so new fixtures get structural validation for free.
