---
hash: "130c1b9a"
id: "fcfb2401"
read_when: "implementing TypeScript alias resolution fixes or understanding re-export scanning improvements in auto-graph"
summary: "Design for fixing TypeScript path alias resolution and re-export edge detection in auto-graph: hardened path mapping, baseUrl probing, unresolved alias diagnostics to stderr."
title: "Solution: Task 005 — Code Graph Alias Re-exports"
---

# Solution: Task 005

## Approach

1. **Add regression fixture first**: create a focused TypeScript fixture that combines the reported cases: `@/*` path aliases with `./src/*` targets, a static alias import, a dynamic alias import, named/type/star re-exports, and one intentionally unresolved alias import for diagnostics.
2. **Harden TypeScript path mapping**: update `internal/resolver/typescript.go` so `paths` mappings keep wildcard prefix and suffix information instead of prefix-only matching. Exact mappings should match exactly; wildcard mappings should capture and substitute the wildcard segment into each target, including targets that start with `./`.
3. **Support modest `baseUrl` probing**: after explicit `paths` mappings fail to match, probe `baseUrl/importPath` before treating a non-relative specifier as external. This covers common project-local absolute imports without implementing full TypeScript package resolution.
4. **Expose alias-match metadata from the resolver**: extend `resolver.ResolveResult` with a boolean such as `MatchedAlias`. When an import matched a configured `paths` alias but no file was found, return `MatchedAlias: true` with an empty `ResolvedPath`.
5. **Emit unresolved alias diagnostics to stderr**: change graph construction to collect unresolved alias diagnostics from `scanner.ImportMatch` metadata (`SourceFile`, `Line`, `ImportPath`) and write warning-style messages to `cmd.ErrOrStderr()`. Keep graph payload output on `cmd.OutOrStdout()` so JSON stdout remains parseable.
6. **Verify re-export scanning end to end**: keep the ast-grep based scanner, but add tests for named re-export, type re-export, and star re-export variants. If ast-grep requires quote-specific patterns, add the single-quote variants alongside the current patterns and rely on dedupe to prevent duplicate matches.
7. **Preserve current graph schema**: do not add warning fields to `graph.Graph`. Diagnostics are process diagnostics, not graph data, and belong on stderr for CLI compatibility.

The implementation should be intentionally small: no TypeScript compiler integration, no new graph package, and no schema migration. Existing code already has the right extension points; this task is about making those points correct and covered by tests.

## Files

```
+ auto-graph/testdata/fixtures/alias-reexports/tsconfig.json
+ auto-graph/testdata/fixtures/alias-reexports/src/routes/dashboard.tsx
+ auto-graph/testdata/fixtures/alias-reexports/src/utils/format.ts
+ auto-graph/testdata/fixtures/alias-reexports/src/components/Header.tsx
+ auto-graph/testdata/fixtures/alias-reexports/src/services/heavy-service.ts
+ auto-graph/testdata/fixtures/alias-reexports/src/client/my-feature/Widget.tsx
+ auto-graph/testdata/fixtures/alias-reexports/src/client/my-feature/widget.utils.ts
+ auto-graph/testdata/fixtures/alias-reexports/src/client/my-feature/index.ts
~ auto-graph/internal/resolver/resolver.go      # add alias-match metadata to ResolveResult
~ auto-graph/internal/resolver/typescript.go    # improve paths/baseUrl resolution and alias unresolved signaling
~ auto-graph/internal/resolver/typescript_test.go # cover wildcard, exact, ./ target, baseUrl, and unresolved alias behavior
~ auto-graph/internal/scanner/typescript.go     # ensure named/type/star re-export patterns are all captured
~ auto-graph/internal/scanner/typescript_test.go # cover all re-export variants from the fixture
~ auto-graph/internal/cli/code_graph.go         # collect diagnostics during graph construction and write them to stderr
~ auto-graph/internal/cli/code_graph_test.go    # graph-level regression test for aliases, re-exports, diagnostics, and JSON stdout
```

Resolver outline:

```go
type ResolveResult struct {
    ResolvedPath string
    IsExternal   bool
    MatchedAlias bool
}
```

Graph diagnostic outline:

```go
type graphDiagnostic struct {
    Source string
    Line   int
    Raw    string
}

func buildGraph(...) (*graph.Graph, []graphDiagnostic)
```

Diagnostic text should include the source file, line, raw import specifier, and a remediation hint such as checking `compilerOptions.paths`, `baseUrl`, or file extensions.

## Test Coverage

| AC | Test Type | File |
|----|-----------|------|
| AC-1 | integration | `auto-graph/internal/cli/code_graph_test.go` using `testdata/fixtures/alias-reexports` |
| AC-2 | integration | `auto-graph/internal/cli/code_graph_test.go` asserting `@/services/heavy-service` resolves with `import_kind=dynamic` |
| AC-3 | unit + integration | `auto-graph/internal/scanner/typescript_test.go` for variants; `auto-graph/internal/cli/code_graph_test.go` for resolved graph edges |
| AC-4 | regression | Existing `auto-graph/internal/*/*_test.go` and `auto-graph/e2e/e2e_test.go` remain green; run `go test ./...` in `auto-graph` |
| AC-5 | integration | `auto-graph/internal/cli/code_graph_test.go` captures stdout/stderr, verifies stdout JSON parses and stderr contains unresolved alias warning |

## Out of Scope

- Monorepo or nested `tsconfig.json` resolution beyond the project-root config
- Full TypeScript compiler module resolution parity for package `exports`, `imports`, or npm dependency graphing
- Symbol-level dependency tracking
- Creating edges to external packages in `node_modules`
- Changing graph output schemas beyond any diagnostic mechanism needed for unresolved alias reporting
- Adding a JavaScript/Node helper process or requiring TypeScript as a runtime dependency

## Rejected Alternatives

- **Use the TypeScript compiler API**: most correct long term, but it adds a Node runtime path and dependency boundary to a Go CLI for a narrowly scoped bug fix.
- **Use `tsconfig-paths` directly**: good for JavaScript tools, but not directly usable from this Go binary without a helper process.
- **Store warnings in the JSON graph**: makes diagnostics machine-readable, but changes the graph schema when the requirement only needs non-silent CLI reporting.
- **Treat every unresolved import as an error**: catches more problems, but would make ordinary external packages and generated files noisy; this task only requires diagnostics for configured alias misses.
