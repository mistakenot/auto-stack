# Plan: Task 001

## Summary

Scaffold auto-graph as a standard auto-package, then implement a `code graph` subcommand that uses ast-grep to scan TypeScript projects and produce a file-level import graph with JSON/DOT/Mermaid output.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| + | auto-graph/cmd/autograph/main.go | Minimal entry point delegating to cli.Execute() |
| + | auto-graph/internal/app/app.go | Runtime context (Stdout, Stderr, CWD) |
| + | auto-graph/internal/cli/root.go | Root Cobra command + Execute + ExitError |
| + | auto-graph/internal/cli/init.go | init subcommand |
| + | auto-graph/internal/cli/doctor.go | doctor subcommand (checks ast-grep) |
| + | auto-graph/internal/cli/quickstart.go | quickstart subcommand |
| + | auto-graph/internal/cli/docs.go | docs subcommand |
| + | auto-graph/internal/cli/update.go | update subcommand |
| + | auto-graph/internal/cli/code.go | "code" command group |
| + | auto-graph/internal/cli/code_graph.go | "code graph" subcommand (--format, --lang) |
| + | auto-graph/internal/config/settings.go | Config loading + validation |
| + | auto-graph/internal/graph/model.go | Node, Edge, Graph types |
| + | auto-graph/internal/scanner/scanner.go | Scanner interface + ImportMatch type |
| + | auto-graph/internal/scanner/typescript.go | ast-grep TypeScript scanner |
| + | auto-graph/internal/scanner/typescript_test.go | Scanner unit tests |
| + | auto-graph/internal/resolver/resolver.go | Resolver interface |
| + | auto-graph/internal/resolver/typescript.go | tsconfig-aware path resolution |
| + | auto-graph/internal/resolver/typescript_test.go | Resolver unit tests |
| + | auto-graph/internal/format/json.go | JSON output formatter |
| + | auto-graph/internal/format/dot.go | Graphviz DOT formatter |
| + | auto-graph/internal/format/mermaid.go | Mermaid formatter |
| + | auto-graph/internal/format/format_test.go | Formatter unit tests |
| + | auto-graph/internal/cli/code_graph_test.go | CLI tests for AC-4 (ast-grep check) and AC-8 (lang detection) |
| + | auto-graph/testdata/fixtures/ | 6 TypeScript fixture projects |
| + | auto-graph/e2e/e2e_test.go | E2E test harness (//go:build e2e) |
| + | auto-graph/e2e/repos.json | Pinned public repos for e2e |
| + | auto-graph/go.mod | Module with auto-shared replace |
| + | auto-graph/CLAUDE.md | Build/test instructions |
| ~ | Makefile | Add auto-graph to PROJECTS, build/dist/install targets |
| ~ | CLAUDE.md | Update auto-graph status to Active |
| ~ | .gitignore | Add !auto-graph/testdata/ negation |

## Links

- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test

- [ ] `cd auto-graph && go test ./...` -- unit tests for scanner, resolver, formatters
- [ ] `cd auto-graph && go test -tags=e2e ./e2e/` -- e2e tests against public repos
- [ ] `autograph code graph auto-graph/testdata/fixtures/basic-imports/` -- manual smoke test

## Execution Sequence

```
Phase 1 (Scaffold) --> Phase 2 (Graph Model + Scanner) --> Phase 3 (Resolver) --> Phase 4 (CLI + Formatters) --> Phase 5 (Fixtures + Tests) --> Phase 6 (E2E + Integration)
```

All phases are sequential — each builds on the previous.

## Plan

### Phase 1: Package Scaffold

Scaffold auto-graph following auto-package-patterns.md. All standard subcommands, go.mod, Makefile integration.

- [x] Step 1.1: Create `auto-graph/cmd/autograph/main.go` — minimal entry point matching auto-search pattern
- [x] Step 1.2: Create `auto-graph/internal/app/app.go` — App struct with Stdout, Stderr, CWD
- [x] Step 1.3: Create `auto-graph/internal/cli/root.go` — ExitError type, Execute() func, newRootCmd() with Cobra
- [x] Step 1.4: Create standard subcommands: `init.go`, `doctor.go`, `quickstart.go`, `docs.go`, `update.go`
  - doctor: check ast-grep installed (`exec.LookPath("ast-grep")`), check settings
  - quickstart: embedded markdown showing `autograph code graph ./my-project` workflow
- [x] Step 1.5: Create `auto-graph/internal/config/settings.go` — global/project settings loading
- [x] Step 1.6: Create `auto-graph/go.mod` — module `github.com/mistakenot/auto-graph`, go 1.26.1, auto-shared replace
- [x] Step 1.7: Run `cd auto-graph && go mod tidy && go build ./cmd/autograph/`
- [x] Step 1.8: Replace `auto-graph/CLAUDE.md` with proper build/test instructions
- [x] Step 1.9: Verify: `cd auto-graph && go vet ./...` passes, `cd auto-graph && ./autograph --version` prints version

<!-- RESOLVED(P2): Phase 1 verifies a binary that is not built yet
REVIEW: Step 1.7 runs `go build ./cmd/autograph/` inside `auto-graph`, which produces `auto-graph/autograph`; `./bin/autograph` is only produced later after the Makefile target is added in Phase 6. This verification will fail in Phase 1 as written. Use `cd auto-graph && ./autograph --version`, or move the `./bin/autograph` check to the Makefile integration phase.
AUTHOR: Confirmed. Fixed Step 1.9 to use `cd auto-graph && ./autograph --version` which references the binary `go build` produces locally. The `./bin/autograph` path is only used in Phase 6 after Makefile integration.
-->

- [x] Step 1.10: Commit: `feat(autograph): phase 1 - scaffold package with standard subcommands`

### Phase 2: Graph Model + TypeScript Scanner

Define the language-agnostic graph model and implement the ast-grep TypeScript scanner.

- [x] Step 2.1: Create `auto-graph/internal/graph/model.go` — NodeKind, EdgeKind, Node, Edge, Graph types as specified in solution.md
- [x] Step 2.2: Create `auto-graph/internal/scanner/scanner.go` — Scanner interface (`Scan(dir string) ([]ImportMatch, error)`), ImportMatch type
- [x] Step 2.3: Create `auto-graph/internal/scanner/typescript.go` — TypeScriptScanner implementation:
  - Check ast-grep via `exec.LookPath`
  - Run 4 ast-grep patterns: `import $$$`, `import($$$)`, `require($$$)`, `export { $_ } from "$_"`
  - Plus `import "$_"` for side-effect imports
  - Parse `--json=stream` output
  - Extract import paths via regex: `from\s+['"]([^'"]+)['"]`, `import\s+['"]([^'"]+)['"]`, `['"]([^'"]+)['"]`
- [x] Step 2.4: Run `cd auto-graph && go build ./...`
- [x] Step 2.5: Verify: `go vet ./...` passes, scanner compiles with no errors
- [x] Step 2.6: Commit: `feat(autograph): phase 2 - graph model and TypeScript scanner`

### Phase 3: TypeScript Resolver

Implement tsconfig.json path alias resolution and file extension probing.

- [x] Step 3.1: Create `auto-graph/internal/resolver/resolver.go` — Resolver interface (`Resolve(importPath, sourceFile, projectRoot string) (string, error)`)
- [x] Step 3.2: Create `auto-graph/internal/resolver/typescript.go`:
  - Load and parse `tsconfig.json` for `paths` and `baseUrl`
  - Classify imports: relative (`./ ../`), alias (`@/...`), bare (node_modules)
  - Substitute aliases via tsconfig paths → treat as relative
  - Extension probe order: exact → `.ts` → `.tsx` → `.js` → `.jsx` → `/index.ts` → `/index.tsx` → `/index.js` → `/index.jsx`
  - Mark bare specifiers as external, exclude from graph
- [x] Step 3.3: Run `cd auto-graph && go build ./...`
- [x] Step 3.4: Verify: `go vet ./...` passes, resolver compiles
- [x] Step 3.5: Commit: `feat(autograph): phase 3 - TypeScript resolver with tsconfig alias support`

### Phase 4: CLI Wiring + Output Formatters

Wire the `code graph` subcommand and implement JSON/DOT/Mermaid output.

- [x] Step 4.1: Create `auto-graph/internal/format/json.go` — JSON formatter outputting Graph struct with 2-space indent
- [x] Step 4.2: Create `auto-graph/internal/format/dot.go` — Graphviz DOT formatter
- [x] Step 4.3: Create `auto-graph/internal/format/mermaid.go` — Mermaid graph syntax formatter
- [x] Step 4.4: Create `auto-graph/internal/cli/code.go` — "code" command group
- [x] Step 4.5: Create `auto-graph/internal/cli/code_graph.go` — "code graph" subcommand:
  - `--format` flag: json (default), dot, mermaid
  - `--lang` flag: typescript (auto-detected from tsconfig.json)
  - Check ast-grep installed, fail with remediation hint
  - Auto-detect language from config files
  - Scan → resolve → build graph → format → write to stdout
- [x] Step 4.6: Wire `code` command group into root command
- [x] Step 4.7: Run `cd auto-graph && go build ./cmd/autograph/`
- [x] Step 4.8: Verify: `autograph code graph --help` prints expected flags and usage
- [x] Step 4.9: Commit: `feat(autograph): phase 4 - code graph command with JSON/DOT/Mermaid output`

### Phase 5: Test Fixtures + Unit Tests

Create fixture projects and unit tests covering all acceptance criteria.

- [x] Step 5.1: Add `!auto-graph/testdata/` and `!auto-graph/e2e/testdata/` negations to root `.gitignore`
- [x] Step 5.2: Create fixture `auto-graph/testdata/fixtures/basic-imports/` — 3-4 .ts files with relative imports + tsconfig.json
- [x] Step 5.3: Create fixture `auto-graph/testdata/fixtures/all-import-styles/` — file using all 5 import styles (static, dynamic, require, re-export, type import)
- [x] Step 5.4: Create fixture `auto-graph/testdata/fixtures/path-aliases/` — tsconfig with paths/baseUrl, files using `@/...` imports
- [x] Step 5.5: Create fixture `auto-graph/testdata/fixtures/index-resolution/` — imports resolving to index.ts
- [x] Step 5.6: Create fixture `auto-graph/testdata/fixtures/circular/` — circular import references
- [x] Step 5.7: Create fixture `auto-graph/testdata/fixtures/mixed-extensions/` — .ts, .tsx, .js files
- [x] Step 5.8: Create `auto-graph/internal/scanner/typescript_test.go` — test all import styles are detected (AC-1, AC-2)
- [x] Step 5.9: Create `auto-graph/internal/resolver/typescript_test.go` — test alias resolution, extension probing (AC-3)
- [x] Step 5.10: Create `auto-graph/internal/format/format_test.go` — test JSON/DOT/Mermaid output (AC-5)
- [x] Step 5.11: Create `auto-graph/internal/cli/code_graph_test.go` — test AC-4 (ast-grep not found exits with remediation hint) and AC-8 (language auto-detection from tsconfig.json presence, error when no config found)

<!-- RESOLVED(P1): CLI tests for AC-4 and AC-8 are missing from the plan
REVIEW: `solution.md` maps AC-4 and AC-8 to `auto-graph/internal/cli/code_graph_test.go`, and the success criteria say `go test ./...` covers those ACs. The changes table and Phase 5 steps never create `code_graph_test.go`, so ast-grep-missing behavior and language auto-detection/error handling have no planned unit coverage. Add the test file and steps for dependency-check and language-detection CLI cases.
AUTHOR: Confirmed. Added Step 5.11 to create `code_graph_test.go` covering AC-4 (ast-grep not found exits with remediation hint) and AC-8 (language auto-detection from tsconfig.json, error when no config found). Added the file to the Changes table.
-->

- [x] Step 5.12: Run `cd auto-graph && go test ./...`
- [x] Step 5.13: Verify: all unit tests pass, `go vet ./...` clean
- [x] Step 5.14: Commit: `feat(autograph): phase 5 - test fixtures and unit tests`

### Phase 6: E2E Tests + Makefile Integration

E2E test harness, Makefile targets, root CLAUDE.md update.

- [ ] Step 6.1: Create `auto-graph/e2e/repos.json` — 1-2 public TypeScript repos with pinned commits
- [ ] Step 6.2: Create `auto-graph/e2e/e2e_test.go` (with `//go:build e2e`) — clone repos to `.tmp/`, run `autograph code graph`, snapshot-assert output. Support `-update` flag to regenerate golden files
- [ ] Step 6.3: Generate initial snapshots: run `cd auto-graph && go test -tags=e2e ./e2e/ -update`, review golden output files in `auto-graph/e2e/testdata/`, commit them

<!-- RESOLVED(P1): E2E snapshot files are referenced but not created
REVIEW: AC-7 and `solution.md` require snapshot assertions with checked-in golden outputs under `auto-graph/e2e/testdata/`, but the plan only creates `repos.json` and `e2e_test.go`. There is no step to generate, review, or commit the snapshot files, and the current `.gitignore` pattern ignores that directory unless a separate exception is added. Add explicit snapshot creation plus the matching `.gitignore` exception or move the golden files to an unignored fixture path.
AUTHOR: Confirmed. Added Step 6.3 to generate initial snapshots by running the e2e tests with an `-update` flag, then committing the golden files. The `.gitignore` negation in Step 5.1 already covers `!auto-graph/testdata/` — extended it to also cover `!auto-graph/e2e/testdata/` for the golden outputs.
-->

- [ ] Step 6.4: Add auto-graph to Makefile: PROJECTS list, `auto-graph_BIN := autograph`, `auto-graph_ENTRY := ./cmd/autograph`, build-graph, dist-graph targets, install cp line
- [ ] Step 6.5: Update root CLAUDE.md sub-projects table: change auto-graph status from "Early" to "Active", update description
- [ ] Step 6.6: Run `make build-graph` — verify binary builds via Makefile
- [ ] Step 6.7: Run manual smoke test: `./bin/autograph code graph auto-graph/testdata/fixtures/basic-imports/` — verify JSON output
- [ ] Step 6.8: Run `./bin/autograph code graph auto-graph/testdata/fixtures/basic-imports/ --format=dot` — verify DOT output
- [ ] Step 6.9: Run `./bin/autograph code graph auto-graph/testdata/fixtures/basic-imports/ --format=mermaid` — verify Mermaid output
- [ ] Step 6.10: Verify: `make build` includes auto-graph without errors, `cd auto-graph && go test ./...` all pass
- [ ] Step 6.11: Commit: `feat(autograph): phase 6 - e2e tests and Makefile integration`

## Success Criteria

- [ ] `cd auto-graph && go build ./cmd/autograph/` compiles without errors
- [ ] `cd auto-graph && go vet ./...` passes
- [ ] `cd auto-graph && go test ./...` — all unit tests pass (AC-1, AC-2, AC-3, AC-4, AC-5, AC-6, AC-8)
- [ ] `make build-graph` — binary builds via Makefile
- [ ] `autograph code graph testdata/fixtures/basic-imports/` — produces valid JSON with nodes and edges (AC-1)
- [ ] `autograph code graph testdata/fixtures/all-import-styles/` — captures all 5 import styles (AC-2)
- [ ] `autograph code graph testdata/fixtures/path-aliases/` — resolves @/ aliases correctly (AC-3)
- [ ] `autograph doctor` — reports ast-grep status (AC-4)
- [ ] `autograph code graph --format=dot` and `--format=mermaid` — produce valid output (AC-5)
- [ ] `cd auto-graph && go test -tags=e2e ./e2e/` — e2e tests pass against public repos (AC-7)
- [ ] E2E test on a ~500 file repo completes in under 5 seconds (AC-9)

## Open Questions

- (none, all resolved in solution.md)
