# Plan: Task 003

## Summary

Add Go language support to autograph's `code graph` command by implementing a Go scanner (using `go/parser`), Go resolver (using `go.mod`), extending the CLI dispatch and graph builder, and adding test fixtures and e2e tests.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| + | auto-graph/internal/scanner/go.go | GoScanner using go/parser with ImportsOnly mode |
| + | auto-graph/internal/scanner/go_test.go | Unit tests for Go import scanning |
| + | auto-graph/internal/resolver/go.go | GoResolver with go.mod module path parsing |
| + | auto-graph/internal/resolver/go_test.go | Unit tests for Go import resolution |
| + | auto-graph/testdata/fixtures/go-basic-imports/ | Basic intra-module imports fixture |
| + | auto-graph/testdata/fixtures/go-all-import-styles/ | Standard, blank, dot, aliased imports |
| + | auto-graph/testdata/fixtures/go-module-resolution/ | Full module path imports |
| + | auto-graph/testdata/fixtures/go-external-imports/ | Stdlib + third-party (excluded from edges) |
| + | auto-graph/testdata/fixtures/go-circular/ | Circular package imports |
| + | auto-graph/e2e/testdata/go-sample-project/ | Multi-package Go project for e2e |
| ~ | auto-graph/internal/cli/code_graph.go | Extend detectLanguage, discoverFiles, buildGraph, language dispatch |
| ~ | auto-graph/internal/cli/doctor.go | Note ast-grep is TypeScript-only |
| ~ | auto-graph/e2e/e2e_test.go | Add Go fixture detection, TestPublicGoRepo, TestGoSampleProjectFormats |
| ~ | auto-graph/e2e/repos.json | Add Go repo entry with pinned commit |
| ~ | auto-graph/internal/cli/quickstart.go | Update quickstart text to mention Go support |
| ~ | auto-graph/internal/cli/docs.go | Update docs text to mention Go support |
| ~ | auto-graph/CLAUDE.md | Document Go support |

<!-- RESOLVED(P2): User-facing CLI docs omitted
REVIEW: I checked `auto-graph/internal/cli/quickstart.go` and `auto-graph/internal/cli/docs.go`; both hard-code TypeScript/tsconfig-only guidance, and `auto-graph/internal/cli/code_graph.go` help text only mentions `tsconfig.json`. Updating only `CLAUDE.md` will leave `autograph quickstart`, `autograph docs`, and `autograph code graph --help` stale after Go support. Add these files to the change list and update them in Phase 5.
AUTHOR: Confirmed. Added `quickstart.go`, `docs.go`, and `code_graph.go` Long help text to the Changes table and Phase 5 steps. All user-facing CLI text will mention both TypeScript and Go.
-->

## Links

- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test

- [x] `auto-graph/internal/scanner/go_test.go` — unit tests for Go scanning (all import styles, skip dirs)
- [x] `auto-graph/internal/resolver/go_test.go` — unit tests for module resolution (same-module, stdlib, external, root package)
- [x] `go test ./...` in `auto-graph/` — all unit + fixture tests pass
- [x] `go test -tags=e2e ./e2e/` — e2e tests pass (Go sample project, public repo, format variants)
- [x] `go vet ./...` in `auto-graph/` — no vet issues

## Execution Sequence

```
Phase 1 (Go Scanner) --\
                        --> Phase 3 (CLI Integration) --> Phase 4 (Fixtures + Tests) --> Phase 5 (E2E + Docs)
Phase 2 (Go Resolver) -/
```

## Plan

### Phase 1: Go Scanner

Implement the Go scanner using `go/parser` from the standard library.

- [x] Step 1.1: Create `auto-graph/internal/scanner/go.go`
  - Implement `GoScanner` struct (no fields needed — no external tool dependency)
  - Implement `NewGoScanner() *GoScanner` constructor
  - Implement `Scan(dir string) ([]ImportMatch, error)`:
    - `filepath.WalkDir` for `.go` files
    - Skip directories: `vendor/`, `testdata/`, `.`-prefixed, `_`-prefixed (Go convention)
    - For each `.go` file: `parser.ParseFile(fset, path, nil, parser.ImportsOnly)`
    - For each `ast.ImportSpec`: extract ImportPath (trim quotes), classify Kind, get Line from fset
    - Kind classification: Name==nil → "static", Name=="_" → "blank", Name=="." → "dot", else → "aliased"
  - Verify: `go build ./...` passes in `auto-graph/`
- [x] Step 1.2: Create `auto-graph/internal/scanner/go_test.go`
  - Test basic import extraction from a Go source string (using `parser.ParseFile` with source bytes)
  - Test all import styles: single, grouped, blank, dot, aliased
  - Test that grouped imports produce individual ImportMatch entries
  - Test skip logic: create temp dirs with vendor/, .hidden/, _ignored/ and verify they're skipped
  - Verify: `go test ./internal/scanner/` passes
- [x] Step 1.3: Commit: `feat(autograph): phase 1 - Go scanner with go/parser`

### Phase 2: Go Resolver

Implement the Go resolver with go.mod module path parsing.

- [x] Step 2.1: Create `auto-graph/internal/resolver/go.go`
  - Implement `GoResolver` struct with `modulePath string` field
  - Implement `NewGoResolver(projectRoot string) (*GoResolver, error)`:
    - Read `go.mod` from projectRoot
    - Parse module path: find line starting with `module `, extract path
    - Return error if go.mod missing or module directive not found
  - Implement `Resolve(importPath, sourceFile, projectRoot string) (ResolveResult, error)`:
    - Same-module check: `importPath == r.modulePath || strings.HasPrefix(importPath, r.modulePath+"/")`
    - If exact match (root package): return `ResolveResult{ResolvedPath: "."}`
    - If prefix match: strip `r.modulePath+"/"`, return `ResolveResult{ResolvedPath: relPkgDir}`
    - Stdlib check: `!strings.Contains(strings.SplitN(importPath, "/", 2)[0], ".")`
    - If stdlib or external: return `ResolveResult{IsExternal: true}`
  - Verify: `go build ./...` passes in `auto-graph/`
- [x] Step 2.2: Create `auto-graph/internal/resolver/go_test.go`
  - Test NewGoResolver reads module path from go.mod correctly
  - Test same-module resolution: `module/internal/pkg` → `internal/pkg`
  - Test root-package resolution: `module` → `"."`
  - Test path-boundary safety: `modulex/pkg` is NOT same-module for `module`
  - Test stdlib classification: `fmt`, `net/http`, `encoding/json` → IsExternal
  - Test external classification: `github.com/other/pkg` → IsExternal
  - Test error case: missing go.mod
  - Verify: `go test ./internal/resolver/` passes
- [x] Step 2.3: Commit: `feat(autograph): phase 2 - Go resolver with go.mod parsing`

### Phase 3: CLI Integration

Wire the Go scanner and resolver into the `code graph` command.

- [x] Step 3.1: Modify `auto-graph/internal/cli/code_graph.go` — `detectLanguage()`
  - Add `go.mod` check alongside existing `tsconfig.json` check
  - If both present: return error with `--lang` hint
  - If only `go.mod`: return `"go"`
  - Update error message when neither found to list both options
  - Verify: `go build ./...` passes

<!-- RESOLVED(P2): AC-5 lacks concrete CLI test updates
REVIEW: AC-5 requires both `go.mod` auto-detection and ambiguous `go.mod`+`tsconfig.json` error handling. The repo already has `auto-graph/internal/cli/code_graph_test.go`, but its current tests cover only TypeScript detection/no-config behavior. Phase 3 changes `detectLanguage`/`runCodeGraph` without adding tests for `go.mod`, both config files, `--lang=go`, and the ast-grep check not running for Go. Add those cases before relying on `go test ./...`.
AUTHOR: Confirmed. Added Step 3.6 to update `code_graph_test.go` with tests for: go.mod-only detection, both config files present (ambiguous error), --lang=go override, and verifying ast-grep is not checked for Go.
-->

- [x] Step 3.2: Modify `auto-graph/internal/cli/code_graph.go` — `discoverFiles()`
  - Add `"go"` case to extension switch: `{".go": true}`
  - Add skip logic for Go-specific dirs: `vendor/`, `testdata/`, `_`-prefixed
  - Verify: `go build ./...` passes
- [x] Step 3.3: Modify `auto-graph/internal/cli/code_graph.go` — `runCodeGraph()`
  - Remove the unconditional ast-grep check (line 64)
  - Replace `if lang != "typescript"` guard (lines 81-86) with a switch statement
  - Add language-specific dispatch:
    ```
    switch lang {
    case "typescript":
        check ast-grep is installed
        sc = scanner.NewTypeScriptScanner()
        res = resolver.NewTypeScriptResolver(projectRoot)
    case "go":
        sc = scanner.NewGoScanner()
        res, err = resolver.NewGoResolver(projectRoot)
    default:
        error with supported languages list
    }
    ```
  - Verify: `go build ./...` passes
- [x] Step 3.4: Modify `auto-graph/internal/cli/code_graph.go` — `buildGraph()`
  - Add `lang string` parameter to `buildGraph` signature
  - Use `lang` instead of hardcoded `"typescript"` for `Node.Language` (line 211)
  - Build `dirToFiles` map from filePaths: `map[string][]string` keyed by `filepath.Dir(p)`
  - In edge creation loop: after resolver returns targetRel, check `nodeSet[targetRel]` first (direct file match, existing behavior), then check `dirToFiles[targetRel]` (package directory match, creates edge to each file)
  - Update the `buildGraph` call site to pass `lang`
  - Verify: `go build ./...` passes
- [x] Step 3.5: Modify `auto-graph/internal/cli/doctor.go`
  - Update ast-grep check message to note it's only required for TypeScript scanning
  - Verify: `go build ./...` passes
- [x] Step 3.6: Update `auto-graph/internal/cli/code_graph_test.go` — AC-5 detection tests
  - Test `detectLanguage` with go.mod only → returns `"go"`
  - Test `detectLanguage` with both go.mod and tsconfig.json → returns ambiguity error
  - Test `runCodeGraph` with `--lang=go` on a temp dir with go.mod and a simple .go file
  - Test that ast-grep is NOT checked when `lang == "go"` (should succeed even if ast-grep is missing)
  - Verify: `go test ./internal/cli/` passes
- [x] Step 3.7: Run full test suite
  - Verify: `go test ./...` passes (existing TypeScript tests still work)
  - Verify: `go vet ./...` passes
- [x] Step 3.8: Commit: `feat(autograph): phase 3 - CLI integration for Go language support`

### Phase 4: Test Fixtures and Unit Tests

Create Go fixture projects and integration tests that exercise the full pipeline.

- [x] Step 4.1: Create `auto-graph/testdata/fixtures/go-basic-imports/`
  - `go.mod` with `module example.com/basic`
  - 3-4 `.go` files across 2 packages importing each other
  - Covers AC-1: basic intra-module import scanning
- [x] Step 4.2: Create `auto-graph/testdata/fixtures/go-all-import-styles/`
  - `go.mod` with `module example.com/styles`
  - Files using: standard import, grouped imports, blank import (`_`), dot import (`.`), aliased import
  - Covers AC-2: all import styles recognized
- [x] Step 4.3: Create `auto-graph/testdata/fixtures/go-module-resolution/`
  - `go.mod` with `module github.com/example/project`
  - Files importing via full module path (`github.com/example/project/internal/util`)
  - Covers AC-3: module path resolution
- [x] Step 4.4: Create `auto-graph/testdata/fixtures/go-external-imports/`
  - `go.mod` with `module example.com/external`
  - Files importing stdlib (`fmt`, `net/http`) and external (`github.com/other/pkg`)
  - Covers AC-4: external imports excluded from graph edges
- [x] Step 4.5: Create `auto-graph/testdata/fixtures/go-circular/`
  - `go.mod` with `module example.com/circular`
  - Two packages that import each other (circular dependency)
  - Verifies scanner handles cycles without infinite loops
- [x] Step 4.6: Build and run autograph against each fixture
  - `go build ./cmd/autograph/ && ./autograph code graph ./testdata/fixtures/go-basic-imports/`
  - Repeat for each fixture, verify JSON output has correct nodes and edges
  - Verify: no external imports appear as edges, all intra-module imports are present

<!-- RESOLVED(P1): Fixture validation is manual, so AC-7 is not met
REVIEW: AC-7 explicitly says `go test ./...` must validate checked-in Go fixtures against expected graph output. Step 4.6 is a manual smoke check, and the planned unit tests only cover scanner/resolver behavior. Existing fixture graph coverage is in `auto-graph/e2e/e2e_test.go` behind `//go:build e2e`, so it will not run in `go test ./...`. Add a non-e2e test, for example in `internal/cli/code_graph_test.go` or a reusable graph builder test, that runs the Go fixtures and asserts expected nodes, edges, and `edge.attrs["import_kind"]`.
AUTHOR: Confirmed. Replaced Step 4.6 with automated fixture tests in `internal/cli/code_graph_test.go` (no e2e tag). Each Go fixture gets a test case asserting expected node count, edge count, import_kind attrs, and absence of external edges. These run in `go test ./...`.
-->

- [x] Step 4.6: Add fixture integration tests in `auto-graph/internal/cli/code_graph_test.go`
  - For each Go fixture (`go-basic-imports`, `go-all-import-styles`, `go-module-resolution`, `go-external-imports`, `go-circular`):
    - Run `runCodeGraph` (or equivalent) against the fixture directory
    - Assert expected node count and that all nodes have `language: "go"`
    - Assert expected edge count and verify `edge.attrs["import_kind"]` values
    - Assert no external imports appear as edges
  - These tests have NO build tag — they run in `go test ./...`
  - Verify: `go test ./internal/cli/` passes
- [x] Step 4.7: Verify existing TypeScript fixtures still work
  - `./autograph code graph ./testdata/fixtures/basic-imports/`
  - Verify: output unchanged from before Go changes
- [x] Step 4.8: Run full test suite
  - Verify: `go test ./...` passes
  - Verify: `go vet ./...` passes
- [x] Step 4.9: Commit: `feat(autograph): phase 4 - Go test fixtures`

### Phase 5: E2E Tests and Documentation

Add Go e2e tests, public repo snapshot tests, and update docs.

- [x] Step 5.1: Create `auto-graph/e2e/testdata/go-sample-project/`
  - Multi-package Go project with `go.mod`, 3+ packages, 10+ files
  - Mix of import styles across packages
  - Sufficient complexity for meaningful e2e assertions
- [x] Step 5.2: Modify `auto-graph/e2e/e2e_test.go` — fixture detection
  - In `TestEdgeReferentialIntegrity`: add `go.mod` detection alongside `tsconfig.json`
  - If fixture dir has `go.mod`, include it in the loop (using `--lang=go` if needed)
- [x] Step 5.3: Add `TestGoSampleProjectJSON` to e2e
  - Build binary, run against go-sample-project
  - Parse JSON, verify nodes have `language: "go"`, edges reference valid nodes
  - Golden file comparison (with `-update` flag support)
- [x] Step 5.4: Add `TestGoSampleProjectFormats` to e2e
  - Run go-sample-project through JSON, DOT, and Mermaid formatters
  - Structural assertions for each format (prefix checks, edge markers)
  - Covers AC-6: multiple output formats with Go
- [x] Step 5.5: Modify `auto-graph/e2e/repos.json` — add Go repo
  - Add entry for a public Go repo at a pinned commit (200-500+ files)
  - Include repo URL, commit hash, expected language
- [x] Step 5.6: Add `TestPublicGoRepo` to e2e
  - Clone pinned repo, run `autograph code graph`, snapshot assertions
  - Add timing assertion: elapsed < 3 seconds (AC-9)
  - Golden file comparison
  - Covers AC-8 and AC-9

<!-- RESOLVED(P2): Performance timing scope is ambiguous
REVIEW: AC-9 measures `autograph code graph ./project`, but this e2e step also includes cloning a public repo and the e2e helper builds the binary before running tests. If the timer includes clone, checkout, or build time, the assertion will be network/toolchain-dependent instead of measuring Go graph performance. Specify that the elapsed timer starts after clone/checkout and binary build, immediately around the graph command only.
AUTHOR: Confirmed. The timer wraps only the `autograph code graph <dir>` invocation, started after clone/checkout and binary build are complete. Clone happens in test setup, binary is built once by `buildBinary(t)`. The timing assertion uses `time.Since(start)` around `runAutograph()` only.
-->

- [x] Step 5.7: Update `auto-graph/CLAUDE.md`
  - Change description from "TypeScript file-level import graphs" to "TypeScript and Go file-level import graphs"
  - Note that Go scanning uses `go/parser` (no external dependency needed)
- [x] Step 5.8: Update user-facing CLI text
  - `auto-graph/internal/cli/quickstart.go` — mention Go alongside TypeScript in quickstart output
  - `auto-graph/internal/cli/docs.go` — add Go usage examples to docs output
  - `auto-graph/internal/cli/code_graph.go` — update Long help text to mention `go.mod` detection alongside `tsconfig.json`
  - Verify: `go build ./...` passes
- [x] Step 5.9: Run full test suite including e2e
  - Verify: `go test ./...` passes
  - Verify: `go test -tags=e2e ./e2e/` passes
  - Verify: `go vet ./...` passes
- [x] Step 5.10: Commit: `feat(autograph): phase 5 - Go e2e tests and documentation`

## Success Criteria

- [x] `go build ./...` passes in `auto-graph/`
- [x] `go vet ./...` passes in `auto-graph/`
- [x] `go test ./...` passes in `auto-graph/` (all unit + fixture tests)
- [x] `go test -tags=e2e ./e2e/` passes (all e2e tests including Go)
- [x] `autograph code graph` auto-detects Go from `go.mod` (AC-5)
- [x] All Go import styles captured: static, blank, dot, aliased (AC-2)
- [x] Same-module imports resolve to files, stdlib/external excluded from edges (AC-3, AC-4)
- [x] JSON, DOT, Mermaid output formats work for Go projects (AC-6)
- [x] Existing TypeScript tests continue to pass (no regression)
- [x] Public Go repo scans in under 3 seconds (AC-9)

## Open Questions

- (none, all resolved)
