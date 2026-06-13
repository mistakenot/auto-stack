---
hash: "530af901"
id: "2112c5eb"
read_when: "implementing Go language support in auto-graph or locating specific extension points in the existing TypeScript implementation"
summary: "Codebase context for adding Go language support to auto-graph, with precise file and line references for the scanner interface, resolver interface, language dispatch, graph building, and E2E fixture patterns."
title: "Context: Go Import Graph (Task 003)"
---

# Context: Task 003

Codebase context for adding Go language support to `auto-graph`. See [solution.md](./solution.md) for the full design.

## Key Files

- `auto-graph/internal/scanner/scanner.go:4-14` — `ImportMatch` struct (SourceFile, ImportPath, Kind, Line) and `Scanner` interface with single method `Scan(dir string) ([]ImportMatch, error)`
- `auto-graph/internal/scanner/typescript.go:53-62` — `TypeScriptScanner` struct with `AstGrepBin` field, `NewTypeScriptScanner()` constructor. Reference for Go scanner constructor pattern
- `auto-graph/internal/scanner/typescript.go:66-125` — `Scan()` implementation: runs ast-grep patterns, dedupes by (file, importPath) key, returns `[]ImportMatch`
- `auto-graph/internal/resolver/resolver.go:3-12` — `ResolveResult` struct (ResolvedPath, IsExternal) and `Resolver` interface with `Resolve(importPath, sourceFile, projectRoot string) (ResolveResult, error)`
- `auto-graph/internal/resolver/typescript.go:59-66` — `NewTypeScriptResolver(projectRoot string)` constructor pattern: reads config, returns resolver. Go equivalent reads `go.mod`
- `auto-graph/internal/resolver/typescript.go:126-150` — `Resolve()` dispatches by import classification (bare → external, alias → substitute, relative → probe)
- `auto-graph/internal/cli/code_graph.go:64` — ast-grep binary check via `exec.LookPath("ast-grep")` — must become TypeScript-specific
- `auto-graph/internal/cli/code_graph.go:81-86` — `if lang != "typescript"` guard — must add `"go"` support
- `auto-graph/internal/cli/code_graph.go:95-96` — `scanner.NewTypeScriptScanner()` instantiation — add Go branch
- `auto-graph/internal/cli/code_graph.go:102` — `resolver.NewTypeScriptResolver(projectRoot)` instantiation — add Go branch
- `auto-graph/internal/cli/code_graph.go:134-144` — `detectLanguage()`: checks only `tsconfig.json` → add `go.mod` check with ambiguity handling
- `auto-graph/internal/cli/code_graph.go:148-195` — `discoverFiles()`: extension map at lines 152-156 has only `.ts`/`.tsx` — add `.go` case
- `auto-graph/internal/cli/code_graph.go:198-271` — `buildGraph()`: creates nodes at line 211 with hardcoded `Language: "typescript"` — must parameterize. Edge creation at lines 222-268 handles file-level targets only — must add directory-to-files expansion for Go
- `auto-graph/internal/graph/model.go:11-14` — constants: `NodeFile = "file"`, `EdgeImport = "import"`. No changes needed
- `auto-graph/internal/cli/doctor.go:47-60` — ast-grep doctor check. Should note it's TypeScript-specific
- `auto-graph/e2e/e2e_test.go:43-89` — helper functions: `buildBinary()`, `runAutograph()`, `testdataDir()`, `goldenDir()`, `sampleProjectDir()`
- `auto-graph/e2e/e2e_test.go:374-390` — `TestEdgeReferentialIntegrity`: loops fixture dirs checking for `tsconfig.json` — must also check `go.mod`
- `auto-graph/go.mod:3` — Go 1.26.1; dependencies: cobra, auto-shared (local replace). No new deps needed for go/parser
- `Makefile:9` — auto-graph already in PROJECTS list; lines 26-27 define binary name `autograph`
- `.gitignore:7-9` — `**/testdata/` excluded, but `!auto-graph/testdata/` and `!auto-graph/e2e/testdata/` negated (fixtures check in)

## Patterns

### Scanner implementation pattern
TypeScript scanner shells out to ast-grep for parsing, iterates patterns × languages, dedupes results. Go scanner will use `go/parser.ParseFile(fset, path, nil, parser.ImportsOnly)` in-process — simpler, no subprocess, no dedup needed (go/parser returns distinct import specs).

### Resolver implementation pattern
TypeScript resolver reads tsconfig.json at construction time, classifies imports (relative/alias/bare), resolves via file probing. Go resolver reads go.mod at construction, classifies imports (same-module/stdlib/external), resolves to package directory paths (no file probing needed).

### buildGraph node creation (code_graph.go:205-213)
```go
for _, p := range filePaths {
    nodeSet[p] = true
    g.Nodes = append(g.Nodes, graph.Node{
        ID:       p,
        Kind:     graph.NodeFile,
        Path:     p,
        Language: "typescript",  // line 211 — must become parameter
    })
}
```

### buildGraph edge creation (code_graph.go:222-268)
Currently matches resolved paths against `nodeSet` (file-level). For Go, must also match against `dirToFiles` map when resolved path is a package directory.

### E2E fixture detection (e2e_test.go:383-389)
```go
tsconfigPath := filepath.Join(fixtureDir, "tsconfig.json")
if _, err := os.Stat(tsconfigPath); err != nil {
    continue  // must also check for go.mod
}
```

### Commit convention
`feat(autograph): phase N - description` (from Task 001 phases 1-6)

## Related Tasks

- **Task 001** (001-ts-import-graph): implemented the TypeScript scanner, resolver, graph model, CLI integration, formatters, fixtures, and e2e tests in 6 phases. Task 003 follows the same phase structure but skips scaffolding (phase 1) and graph model (phase 2) since those already exist. Commit sequence: 78d2616 → d3203f8 → 6d95322 → b486ee7 → 2038f76 → e6b7b44, merged as 3d61ecd
