---
hash: "1f679d60"
id: "18f984e6"
read_when: "implementing Go scanner/resolver or extending auto-graph language dispatch"
summary: "Solution design for adding Go language support to auto-graph via GoScanner (using go/parser), GoResolver (reading go.mod), package-directory expansion in buildGraph, and language detection from go.mod vs tsconfig.json."
title: "Solution: Go Import Graph (Task 003)"
---
# Solution: Task 003

<!-- REJECTED(P1): Missing required planning docs
REVIEW: This task folder currently contains only `requirements.md` and `solution.md`; `context.md` and `plan.md` are absent. The review workflow expects all four planning docs, and without `context.md`/`plan.md` the implementer has no codebase-verified line references, phase sequence, commands, or success criteria to execute against. Add the missing docs before implementation.
AUTHOR: This is the normal workflow progression: requirements → solution → context → plan. Solution is reviewed before context.md and plan.md exist. The user runs `/new-plan` next to generate those docs. Same pattern as Task 001.
-->

## Approach

1. **Implement Go scanner** (`internal/scanner/go.go`) using `go/parser.ParseFile` with `parser.ImportsOnly` mode. Walks the project directory for `.go` files, extracts `ast.ImportSpec` entries. Classifies imports as `"static"`, `"blank"` (`_`), `"dot"` (`.`), or `"aliased"` based on the `Name` field. No external tool dependency — `go/parser` is in the Go standard library
2. **Implement Go resolver** (`internal/resolver/go.go`) — reads `go.mod` to extract the module path (simple line parsing, no external dependency). Classifies imports into three buckets: same-module (starts with module path) → resolves to package directory path; stdlib (no dot in first path element) → external; other module (has dot, different prefix) → external
3. **Extend `buildGraph`** to handle package-level resolution. Go imports resolve to package directories, not individual files. Build a `dirToFiles` map from discovered file paths. When the resolver returns a path that's a directory (not a file in the node set), expand to all files in that directory. Backwards-compatible: TypeScript always returns file paths, so the expansion never triggers for TS
4. **Extend `code_graph.go` language dispatch** — add `"go"` to `detectLanguage` (from `go.mod`), `discoverFiles` (`.go` extension), and scanner/resolver selection. Move the ast-grep binary check to be TypeScript-specific (Go doesn't need it). Handle ambiguous detection when both `go.mod` and `tsconfig.json` are present
5. **Build Go test fixtures** — checked-in Go projects under `auto-graph/testdata/fixtures/go-*` covering each AC: basic imports, all import styles, module path resolution, stdlib/external classification
6. **Add Go e2e test** — extend `TestEdgeReferentialIntegrity` to detect `go.mod` fixtures alongside `tsconfig.json`. Add a Go sample project under `e2e/testdata/go-sample-project/`

<!-- RESOLVED(P1): AC-8 public repo e2e is not covered
REVIEW: AC-8 requires the e2e suite to clone a public Go repo at a pinned commit and run snapshot assertions. I checked `auto-graph/e2e/e2e_test.go`: it currently scans local `testdata` fixtures only, and `auto-graph/e2e/repos.json` contains one pinned TypeScript repo that the test code does not read. The proposed change adds only a local Go sample project and fixture detection, so AC-8 would remain unmet.
AUTHOR: Confirmed. Added step 7: add a Go entry to `repos.json` with a pinned public Go repo (e.g. a small well-known Go project), and add `TestPublicGoRepo` to the e2e suite that clones, scans, and performs golden-file snapshot assertions. Updated Files section to include `repos.json` modification.
-->

7. **Add public repo e2e for Go** — add a Go entry to `e2e/repos.json` (pinned commit of a small public Go repo). Add `TestPublicGoRepo` to `e2e/e2e_test.go` that clones the repo, runs `autograph code graph`, and compares against golden snapshot files under `e2e/testdata/golden/`
8. **Update CLAUDE.md** — document Go support in the description

### Go Scanner Detail

```go
type GoScanner struct{}

func (s *GoScanner) Scan(dir string) ([]ImportMatch, error) {
    // filepath.WalkDir for .go files
    // Skip: vendor/, testdata/, .* dirs, _* dirs (Go convention)
    // For each .go file:
    //   parser.ParseFile(fset, path, nil, parser.ImportsOnly)
    //   For each ast.ImportSpec:
    //     ImportPath = strings.Trim(imp.Path.Value, `"`)
    //     Kind = classifyGoImport(imp.Name)
    //     Line = fset.Position(imp.Pos()).Line
}
```

Import kind classification from `ast.ImportSpec.Name`:
- `nil` → `"static"` (standard import)
- `"_"` → `"blank"` (side-effect import)
- `"."` → `"dot"` (dot import)
- anything else → `"aliased"`

Grouped imports (`import (...)`) are handled transparently — `go/parser` returns individual `ImportSpec` entries regardless of grouping syntax.

### Go Resolver Detail

```go
type GoResolver struct {
    modulePath string
}

func NewGoResolver(projectRoot string) (*GoResolver, error) {
    // Read go.mod, find "module" directive line
    // Extract module path (e.g. "github.com/example/project")
}

func (r *GoResolver) Resolve(importPath, sourceFile, projectRoot string) (ResolveResult, error) {
    // Same-module: importPath == r.modulePath || strings.HasPrefix(importPath, r.modulePath+"/")
    //   → if exact match: resolve to "." (root package)
    //   → else: strip modulePath+"/" prefix, return relative package directory
    // Stdlib: no dot in first path element (e.g. "fmt", "net/http")
    //   → IsExternal: true
    // External module: has dot, different prefix (e.g. "github.com/other/pkg")
    //   → IsExternal: true
}
```

<!-- RESOLVED(P2): Same-module matching needs path-boundary and root-package handling
REVIEW: The proposed `strings.HasPrefix(importPath, r.modulePath)` check can misclassify `github.com/example/projectx/pkg` as internal for module `github.com/example/project`; it should require exact match or `modulePath + "/"`. An exact root-package import (`importPath == modulePath`) also needs to resolve to `"."`, because the proposed `dirToFiles` index stores root files under `filepath.Dir("main.go") == "."`; returning an empty path would be treated as unresolved.
AUTHOR: Confirmed. The same-module check will use `importPath == r.modulePath || strings.HasPrefix(importPath, r.modulePath+"/")` for path-boundary safety. Root-package imports (`importPath == modulePath`) resolve to `"."`. Updated the resolver pseudocode above to reflect this.
-->

Module path parsing: read `go.mod` line by line, find the line starting with `module `, extract the path. No need for `golang.org/x/mod/modfile` — the format is trivial.

Stdlib detection heuristic: `!strings.Contains(strings.SplitN(importPath, "/", 2)[0], ".")`. This is the standard approach used by `goimports` and other Go tooling.

### buildGraph Package Expansion

<!-- RESOLVED(P2): Go nodes would still be labeled as TypeScript
REVIEW: `auto-graph/internal/cli/code_graph.go` currently hardcodes `Language: "typescript"` when creating every graph node in `buildGraph`. The solution only describes adding package-directory expansion, so a Go graph would still emit TypeScript node language metadata unless `buildGraph` receives or derives the selected language.
AUTHOR: Confirmed. `buildGraph` will take an additional `lang string` parameter and use it for `Node.Language`. The call site in `runCodeGraph` already has the detected language available. Updated approach step 3 implicitly covers this as part of the `buildGraph` extension.
-->

```go
// Build directory-to-files index (cheap, built unconditionally)
dirToFiles := make(map[string][]string)
for _, p := range filePaths {
    dir := filepath.Dir(p)
    dirToFiles[dir] = append(dirToFiles[dir], p)
}

// Edge creation — after resolver returns targetRel:
if nodeSet[targetRel] {
    // Direct file match (TypeScript path)
    createEdge(sourceRel, targetRel, ...)
} else if files, ok := dirToFiles[targetRel]; ok {
    // Package directory match (Go path)
    for _, targetFile := range files {
        createEdge(sourceRel, targetFile, ...)
    }
}
```

### Language Detection Changes

```go
func detectLanguage(projectRoot string) (string, error) {
    hasGoMod := fileExists(filepath.Join(projectRoot, "go.mod"))
    hasTSConfig := fileExists(filepath.Join(projectRoot, "tsconfig.json"))

    if hasGoMod && hasTSConfig {
        return "", fmt.Errorf("ambiguous: both go.mod and tsconfig.json found; use --lang=go or --lang=typescript")
    }
    if hasGoMod {
        return "go", nil
    }
    if hasTSConfig {
        return "typescript", nil
    }
    return "", fmt.Errorf("could not detect language: no go.mod or tsconfig.json found; use --lang to specify")
}
```

### ast-grep Check Scoping

The current `runCodeGraph` checks for ast-grep before language detection. Move this to be TypeScript-specific:

```go
// Before: unconditional ast-grep check at top of runCodeGraph
// After: check only when lang == "typescript", after detection
switch lang {
case "typescript":
    if _, err := exec.LookPath("ast-grep"); err != nil { ... }
    sc = scanner.NewTypeScriptScanner()
    res = resolver.NewTypeScriptResolver(projectRoot)
case "go":
    sc = scanner.NewGoScanner()
    res, err = resolver.NewGoResolver(projectRoot)
}
```

## Files

```
+ auto-graph/internal/scanner/go.go           # GoScanner using go/parser
+ auto-graph/internal/scanner/go_test.go       # unit tests for Go scanning
+ auto-graph/internal/resolver/go.go           # GoResolver with go.mod parsing
+ auto-graph/internal/resolver/go_test.go      # unit tests for Go resolution
+ auto-graph/testdata/fixtures/go-basic-imports/       # 3-4 .go files with intra-module imports
+ auto-graph/testdata/fixtures/go-basic-imports/go.mod
+ auto-graph/testdata/fixtures/go-all-import-styles/   # standard, blank, dot, aliased imports
+ auto-graph/testdata/fixtures/go-all-import-styles/go.mod
+ auto-graph/testdata/fixtures/go-module-resolution/   # imports using full module path
+ auto-graph/testdata/fixtures/go-module-resolution/go.mod
+ auto-graph/testdata/fixtures/go-external-imports/    # stdlib + third-party imports (excluded from edges)
+ auto-graph/testdata/fixtures/go-external-imports/go.mod
+ auto-graph/testdata/fixtures/go-circular/            # circular package imports
+ auto-graph/testdata/fixtures/go-circular/go.mod
+ auto-graph/e2e/testdata/go-sample-project/           # multi-package Go project for e2e
+ auto-graph/e2e/testdata/go-sample-project/go.mod
~ auto-graph/internal/cli/code_graph.go        # extend detectLanguage, discoverFiles, scanner/resolver dispatch, buildGraph
~ auto-graph/internal/cli/doctor.go            # note ast-grep is TypeScript-only dependency
~ auto-graph/e2e/e2e_test.go                   # add Go fixture/sample detection, TestPublicGoRepo, TestGoSampleProjectFormats
~ auto-graph/e2e/repos.json                    # add Go repo entry with pinned commit
~ auto-graph/CLAUDE.md                         # document Go support
```

## Test Coverage

| AC  | Test Type   | File                                          |
|-----|-------------|-----------------------------------------------|
| AC-1 | unit       | auto-graph/internal/scanner/go_test.go        |
| AC-1 | fixture    | auto-graph/testdata/fixtures/go-basic-imports/ |
| AC-2 | unit       | auto-graph/internal/scanner/go_test.go        |
| AC-2 | fixture    | auto-graph/testdata/fixtures/go-all-import-styles/ |
| AC-3 | unit       | auto-graph/internal/resolver/go_test.go       |
| AC-3 | fixture    | auto-graph/testdata/fixtures/go-module-resolution/ |
| AC-4 | unit       | auto-graph/internal/resolver/go_test.go       |
| AC-4 | fixture    | auto-graph/testdata/fixtures/go-external-imports/ |
| AC-5 | unit       | auto-graph/internal/cli/code_graph_test.go (if exists, or inline in go_test.go) |
| AC-6 | e2e        | auto-graph/e2e/e2e_test.go (TestGoSampleProjectFormats)  |
| AC-7 | fixture    | auto-graph/testdata/fixtures/go-* (all)       |
| AC-8 | e2e        | auto-graph/e2e/e2e_test.go                    |
| AC-9 | e2e        | auto-graph/e2e/e2e_test.go (timed assertions) |

<!-- RESOLVED(P2): AC-6 needs Go CLI format coverage
REVIEW: The table says no new format tests are needed, but AC-6 is specifically about `autograph code graph` on a Go project for JSON, DOT, and Mermaid. Existing `TestMultipleFormats` in `auto-graph/e2e/e2e_test.go` uses the TypeScript sample project only, so the Go scanner/resolver/graph path would not be exercised for DOT and Mermaid output.
AUTHOR: Confirmed. Will add `TestGoSampleProjectFormats` to the e2e suite that runs the Go sample project through all three formatters (JSON, DOT, Mermaid) with structural assertions. Updated the AC-6 test coverage entry.
-->

<!-- RESOLVED(P2): Performance criterion lacks a medium-project test input
REVIEW: AC-9 requires a roughly 500-file Go project completing under 3 seconds, but the proposed files add small fixtures and a local sample project only. Unless the public pinned repo or a generated fixture is specified as the medium project, `auto-graph/e2e/e2e_test.go` cannot assert this criterion meaningfully.
AUTHOR: The public Go repo pinned in `repos.json` (step 7) serves as the medium-project input. Will select a repo with 200-500+ Go files. The `TestPublicGoRepo` e2e test will include a timing assertion (elapsed < 3s) in addition to snapshot correctness.
-->

## Out of Scope

- Multi-module repos (scanning across multiple `go.mod` boundaries)
- Resolving external module imports to cached sources (only intra-module imports produce edges)
- Symbol-level import tracking (only file-to-package-to-file level)
- Build tag / `//go:build` conditional compilation filtering
- CGo imports
- `vendor/` directory traversal (skipped like `node_modules`)
- Test file exclusion flag (test files are included by default)
- Changes to graph model, formatters, or scanner/resolver interfaces

## Rejected Alternatives

- **ast-grep for Go scanning**: Consistent with TS approach but adds an unnecessary external dependency. Go's `go/parser` with `parser.ImportsOnly` is faster (in-process, no subprocess), more accurate (standard library parser), and has zero dependencies. ast-grep was chosen for TS because embedding tree-sitter C bindings was the only alternative — Go doesn't have that constraint
- **`golang.org/x/mod/modfile` for go.mod parsing**: Full-featured parser but adds an external dependency. The module path is always the first directive in go.mod (`module X`), trivially extractable with line-by-line reading. No benefit from the full parser for our use case
- **`go list -json` for import resolution**: Would give accurate resolution including build tags, but requires a valid Go build environment (`go` binary, module cache populated). go/parser works on any directory of `.go` files without needing the Go toolchain's module resolution. Our single-module scope makes manual resolution straightforward
- **Package-level nodes instead of file-level**: Would be more idiomatic for Go (imports are package-level), but diverges from the existing file-level graph model. File-level is more granular and consistent across languages. Package-level edges fan out to all files in the target package, which is accurate
