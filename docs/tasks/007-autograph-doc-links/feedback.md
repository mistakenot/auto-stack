# Feedback: Task 007

## Problems faced
1. Cross-module `internal/` visibility -- Go prevents importing `internal/` packages across modules, requiring thin `pkg/` wrappers in autodoc. The monorepo `replace` directive pattern from `auto-shared` made this straightforward.
2. `buildAdjacencyMaps` unfiltered edge iteration -- Existing code iterated all edges without checking `EdgeKind`, which would have caused `doc_link` edges to be misclassified as import dependencies. Required splitting into `buildAdjacencyMaps` (import-only) and `buildDocAdjacencyMaps` (doc-only).
3. `linkscan.ScanFiles` git dependency -- The scanner shells out to `git ls-files`, meaning E2E fixtures must be initialized as git repos. Soft-failure in `doclink.Scan` handles non-git dirs gracefully.
4. Triple-backtick collision in context pack markdown -- Caught in review: doc `.md` files containing fenced code blocks would break the outer fence. Fixed with adaptive fence length (`max(3, longest_run+1)` backticks per CommonMark).

## Reflections
- The parallel execution of Phases 3 and 4 worked cleanly because the boundary was well-defined (CLI vs builder/format). The plan's DAG was accurate.
- The `isRuntimeImport` fallback (returns true when no import_kinds set) was the subtlest correctness issue. Context.md flagging it explicitly saved time.
- Having the solution doc pre-resolve the adjacency map filtering approach (separate doc maps vs inline filtering) prevented the most likely architectural mistake during implementation.

## Useful context
- `auto-graph/internal/contextpack/builder.go:425-433` -- `isRuntimeImport` defaulting to true when no kinds set was the critical fact for getting edge filtering right
- The established `replace` directive pattern in `auto-shared` made cross-module dependency trivial
- CommonMark spec allows fences of arbitrary backtick length, which was the clean fix for the collision issue
