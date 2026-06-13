---
hash: "f9387a17"
id: "33859bc4"
read_when: "implementing the autograph context-pack feature or understanding its codebase dependencies"
summary: "Verified codebase context for the context-pack task, covering runCodeGraph extraction, import metadata merging, and key file references in autograph."
title: "Context: Task 004 — Context Pack"
---

# Context: Task 004

This file records verified implementation context for [solution.md](./solution.md).

## Key Files

- `auto-graph/internal/cli/code_graph.go:51` -- `runCodeGraph(cmd, dir, formatFlag, langFlag) error` currently owns the full graph workflow: absolute path validation, directory check, ast-grep check, language detection, file discovery, scanner/resolver creation, graph construction, and format output. This is the code to extract into `internal/codegraph`.

```go
// auto-graph/internal/cli/code_graph.go:51
func runCodeGraph(cmd *cobra.Command, dir, formatFlag, langFlag string) error {
    projectRoot, err := filepath.Abs(dir)
    // ...
    filePaths, err := discoverFiles(projectRoot, lang)
    // ...
    matches, err := sc.Scan(projectRoot)
    // ...
    g := buildGraph(projectRoot, filePaths, matches, res)
}
```

- `auto-graph/internal/cli/code_graph.go:63` -- ast-grep is checked before language dispatch. Task 004 is TypeScript-only, but the reusable `codegraph.Build` should keep the check scoped to TypeScript behavior and avoid making the context command duplicate this logic.
- `auto-graph/internal/cli/code_graph.go:132` -- language detection currently only detects `tsconfig.json` and returns `typescript`.
- `auto-graph/internal/cli/code_graph.go:146` -- file discovery walks the project, skips hidden dirs and `node_modules`, includes `.ts`/`.tsx`, and sorts paths.
- `auto-graph/internal/cli/code_graph.go:197` -- `buildGraph` creates nodes and import edges. It hardcodes `Language: "typescript"` at line 211.
- `auto-graph/internal/cli/code_graph.go:222` -- graph edge construction skips external/unresolved imports, skips self-imports, requires source/target in the node set, and dedupes by `(source, target)`.

```go
// auto-graph/internal/cli/code_graph.go:252
key := edgeKey{source: sourceRel, target: targetRel}
if edgeSeen[key] {
    continue
}
edgeSeen[key] = true
```

- `auto-graph/internal/cli/code_graph.go:259` -- current edge attrs preserve only one `import_kind` and one `raw` string. Task 004 must preserve merged `import_kinds` and `raws` so context selection can distinguish runtime, type-only, dynamic, side-effect, and re-export relationships.
- `auto-graph/internal/scanner/scanner.go:4` -- scanners return `ImportMatch{SourceFile, ImportPath, Kind, Line}`; `Line` is available but not currently copied into graph edge attrs.
- `auto-graph/internal/scanner/typescript.go:66` -- `TypeScriptScanner.Scan` shells out to ast-grep for TypeScript import discovery.
- `auto-graph/internal/scanner/typescript.go:72` -- scanner patterns cover static imports, dynamic imports, `require`, named/star/type re-exports, and side-effect imports.
- `auto-graph/internal/scanner/typescript.go:82` -- scanner runs both `ts` and `tsx` ast-grep language modes.
- `auto-graph/internal/scanner/typescript.go:86` -- scanner dedupes by `(sourceFile, importPath)`, which can collapse multiple import kinds for the same raw specifier before graph construction.

```go
// auto-graph/internal/scanner/typescript.go:86
type seenKey struct {
    file       string
    importPath string
}
```

- `auto-graph/internal/resolver/typescript.go:26` -- resolver probes exact path, `.ts`, `.tsx`, `.js`, `.jsx`, and index variants. Current discovery does not include `.js`/`.jsx`, so resolved JS files are still filtered out by the graph node set.
- `auto-graph/internal/resolver/typescript.go:124` -- resolver returns `ResolveResult{ResolvedPath, IsExternal}` and marks bare imports as external.
- `auto-graph/internal/graph/model.go:16` -- graph nodes and edges are file-level structs with `Attrs map[string]string`, which is the extension point for merged import metadata.
- `auto-graph/internal/cli/code.go:7` -- the `code` command currently registers only `newCodeGraphCmd()`.
- `auto-graph/internal/cli/root.go:14` -- `ExitError` wraps CLI errors with exit codes. `Execute` prints returned errors to stderr and keeps stdout for successful payloads.
- `auto-graph/internal/format/json.go:11` -- JSON formatter pretty-prints a struct with `json.Encoder` and stable struct field order.
- `auto-graph/internal/config/settings.go:63` -- graph settings default output is `json`; validation only allows `json`, `dot`, and `mermaid` at lines 82-99. Task 004's markdown default should be command-local, not forced into graph settings.
- `auto-graph/e2e/e2e_test.go:1` -- e2e tests are behind the `e2e` build tag and should remain opt-in.
- `auto-graph/e2e/e2e_test.go:276` -- existing e2e golden normalization replaces the absolute root and sorts nodes/edges for deterministic JSON comparison.

## Patterns

- CLI commands live in one file per domain command. `docs/auto-package-patterns.md:41` says implementation lives under `internal/`, one file per command in `internal/cli`, domain logic is separate from CLI wiring, and tests live alongside the code they test.
- `auto-graph/CLAUDE.md:5` documents `cd auto-graph && go build ./cmd/autograph/`; `auto-graph/CLAUDE.md:12` documents `cd auto-graph && go test ./...`; `auto-graph/CLAUDE.md:19` documents `cd auto-graph && go vet ./...`.
- Current graph output formats are JSON, DOT, and Mermaid. Context-pack output is a separate command with markdown default and `--format=json` because requirements explicitly override the general JSON default.
- Existing tests use checked-in fixtures under `auto-graph/testdata/fixtures/`. Scanner/resolver tests compute fixture roots relative to package directories.
- Existing e2e tests use golden files under `auto-graph/e2e/testdata/golden/`; Task 004 solution instead plans context-pack golden outputs under `auto-graph/testdata/golden/context-pack/` for regular `go test ./...`.
- Diagnostics/errors should go to stderr and successful payloads to stdout. This matters for JSON mode so stdout remains parseable.
- Structured validation errors should use fields `code`, `path`, `field`, `message`, and optional `value`, matching repository CLI validation guidance.
- The scanner currently skips duplicate raw specifiers, and graph building skips duplicate source/target edges. Task 004 must change this path before context selection so import metadata is merged, not lost.
- Context-pack selection and rendering tests should not depend on ast-grep when a synthetic `graph.Graph` can cover selection, budget, and formatter behavior.

## Related Tasks

- Task 001: TypeScript Import Graph established autograph as a graph-based context engine foundation and implemented file-level TypeScript import graphs. `docs/tasks/001-ts-import-graph/requirements.md:5` describes the long-term goal of assembling relevant context bundles for agents.
- Task 001 solution defines the language-agnostic graph IR at `docs/tasks/001-ts-import-graph/solution.md:71` and records that nodes/edges JSON is the canonical graph shape at `docs/tasks/001-ts-import-graph/solution.md:99`.
- Task 001 feedback records important scanner risks: ast-grep needs both `ts` and `tsx` language modes, and re-export matching needed broader patterns. See `docs/tasks/001-ts-import-graph/feedback.md:3`.
- Task 003: Go Import Graph is planning-only on main. `docs/tasks/003-go-import-graph/requirements.md:5` states the scanner/resolver interfaces were designed to be language-agnostic, but Task 004 explicitly excludes Go context packs until Go graph support is implemented.
- Task 005: Code Graph Alias and Re-export Resolution is a separate untracked task. `docs/tasks/005-code-graph-alias-reexports/requirements.md:5` records missing TypeScript alias/re-export edges. Task 004 should preserve import metadata needed by context packs but should not expand into Task 005's broader alias diagnostic scope.

## History Notes

- `3d61ecd` merged the TypeScript file-level import graph implementation (`feat(autograph): TypeScript file-level import graph (#36)`).
- `7748ea9` addressed PR feedback on the TypeScript graph, including broader re-export patterns, ast-grep exclusions, formatter performance, and regenerated goldens.
- `3650a75` added Task 001 feedback; it notes TSX scanning and re-export patterns as issues caught after initial implementation.
- `0d6450d` added Task 003 Go import graph planning docs. No tracked commit currently exists for Task 004.
