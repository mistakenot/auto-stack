# Feedback: Task 003

## Problems faced
1. Scanner/walker skip-rule asymmetry -- the Go scanner and `discoverFiles` had different skip lists (scanner didn't skip `node_modules`), which could produce edges with sources not in the node set. Caught by code review, fixed with a `nodeSet[sourceRel]` guard.
2. Naive go.mod parsing -- inline comments (`// ...`) and quoted module paths are valid go.mod syntax but the initial line parser didn't handle them. Could silently produce zero-edge graphs.

## Reflections
- The package-level resolution (Go imports target directories, not files) was the key architectural difference from TypeScript. The `dirToFiles` map + expansion loop handled it cleanly without changing the graph model.
- Should have shared skip-list logic between scanner and `discoverFiles` from the start — the divergence was inevitable and the guard is a band-aid.
- The existing scanner/resolver/buildGraph separation made adding Go trivial structurally — just implement two interfaces and extend the switch statement.

## Useful context
- `go/parser.ParseFile` with `parser.ImportsOnly` is extremely fast — no need for ast-grep or external tools for Go
- The stdlib detection heuristic (`!strings.Contains(firstSegment, ".")`) matches what `goimports` uses — battle-tested
- Task 001's phase structure was a good template; Go was simpler (no alias resolution, no file probing)
