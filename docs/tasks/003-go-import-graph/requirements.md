# Task 003: Go Import Graph

## Background

autograph already supports TypeScript file-level import graphs (Task 001). The scanner interface (`internal/scanner/scanner.go`) and resolver interface (`internal/resolver/resolver.go`) were designed to be language-agnostic. This task adds Go as the second supported language, proving the extensibility of the architecture.

Go is a simpler case than TypeScript: no path aliases, no ambiguous file extensions, a single canonical import syntax. The key difference is that Go uses module paths (not relative paths) and all imports are absolute from the module root. We can use Go's standard library `go/parser` and `go/ast` — no external tooling dependency like ast-grep.

## Problem

autograph only supports TypeScript. We need Go support to make the tool useful for Go-dominant projects like auto-stack itself. Go's import resolution is fundamentally different from TypeScript's (module-path-based, not relative-path-based), so a new scanner and resolver are needed.

## Goals

- Add a Go scanner using `go/parser` from the standard library (no external tool dependency)
- Add a Go resolver that reads `go.mod` to determine the module path and resolves same-module imports to actual files
- Extend `autograph code graph` to auto-detect Go from `go.mod` presence and scan `.go` files
- Handle all Go import styles: standard, grouped, blank (`_`), dot (`.`), and aliased imports
- Classify standard library and external module imports as external (excluded from graph edges)
- Reuse existing output formatters (JSON, DOT, Mermaid) and graph model without changes
- Validate with checked-in Go fixture projects and e2e tests against public repos

## Acceptance Criteria

**AC-1**: Basic import scanning
- Given: a Go project with multiple packages importing each other via same-module paths
- When: `autograph code graph ./project`
- Then: JSON output contains a graph with nodes for each `.go` file and edges for each intra-module import relationship

**AC-2**: All import styles recognized
- Given: a file using single import, grouped imports, blank import (`import _ "pkg"`), dot import (`import . "pkg"`), and aliased import (`import alias "pkg"`)
- When: the file is scanned
- Then: all import paths are captured in the graph (with appropriate kind classification in edge attrs)

**AC-3**: go.mod module path resolution
- Given: a project with `go.mod` declaring `module github.com/example/project`
- When: a file imports `github.com/example/project/internal/util`
- Then: the graph resolves this to the actual file(s) in `internal/util/`

**AC-4**: Standard library and external module classification
- Given: a file importing `fmt`, `net/http` (stdlib), and `github.com/other/pkg` (external)
- When: the file is scanned
- Then: stdlib and external imports are classified as external and excluded from graph edges

**AC-5**: Language auto-detection
- Given: a directory containing `go.mod`
- When: `autograph code graph ./directory` (no `--lang` flag)
- Then: Go scanning is automatically selected
- Given: a directory with both `go.mod` and `tsconfig.json`
- When: `autograph code graph ./directory`
- Then: error with hint to use `--lang` to disambiguate

**AC-6**: Multiple output formats
- Given: a scanned Go project
- When: `autograph code graph ./project --format=json|dot|mermaid`
- Then: output matches the existing format specs (reuses same formatters as TypeScript)

**AC-7**: Fixture-based unit tests
- Given: checked-in Go fixture projects under `auto-graph/testdata/fixtures/go-*`
- When: `go test ./...` runs
- Then: tests validate import detection, module resolution, and edge cases against expected graph output

**AC-8**: E2E tests against public repos
- Given: an e2e test case for Go
- When: the e2e suite runs (with `-tags=e2e`), it clones a public Go repo at a pinned commit
- Then: runs `autograph code graph` and performs snapshot assertions on the output

**AC-9**: Performance
- Given: a medium-sized Go project (~500 files)
- When: `autograph code graph ./project`
- Then: completes in under 3 seconds (go/parser is faster than shelling out to ast-grep)

## Out of Scope

- Multi-module repos (scanning across multiple `go.mod` boundaries)
- Resolving external module imports to cached sources (only intra-module imports produce edges)
- Symbol-level import tracking (only file-to-package-to-file level)
- Build tag / `//go:build` conditional compilation filtering
- CGo imports
- `vendor/` directory traversal (skipped like `node_modules`)
- Test file exclusion flag (test files are included by default; filtering can be added later)

## Open Questions

- (none, all resolved)
