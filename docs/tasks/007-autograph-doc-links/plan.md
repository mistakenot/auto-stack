---
hash: "ab543228"
id: "4da5d7ed"
read_when: "implementing autodoc doc-link enrichment in autograph or wiring auto-doc into the context pack"
summary: "Implementation plan for adding autodoc doc-link awareness to autograph: public scan/doctree APIs, doclink enrichment layer, --no-docs flag on CLI commands, and doc candidate priorities in context pack builder."
title: "Plan: Task 007 — Autograph Doc Links"
---

# Plan: Task 007

## Summary

Add autodoc doc-link awareness to autograph by exposing autodoc's scan/doctree packages, creating a doclink enrichment layer, wiring it into both CLI commands with `--no-docs`, and extending the context pack builder with doc-specific candidate priorities.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| + | `auto-doc/pkg/scan/scan.go` | Public API wrapper for linkscan.ScanFiles with type aliases |
| + | `auto-doc/pkg/docs/docs.go` | Public API wrapper for doctree.WalkRepo with type aliases |
| ~ | `auto-graph/go.mod` | Add autodoc dependency + replace directive |
| ~ | `auto-graph/internal/graph/model.go` | Add NodeDoc, EdgeDocLink constants |
| + | `auto-graph/internal/doclink/doclink.go` | Scan + Enrich functions with soft-failure |
| + | `auto-graph/internal/doclink/doclink_test.go` | Unit tests for scan, enrich, soft-failure |
| ~ | `auto-graph/internal/cli/code_graph.go` | Add --no-docs flag, call doclink after Build |
| ~ | `auto-graph/internal/cli/code_context.go` | Add --no-docs flag, call doclink after Build |
| ~ | `auto-graph/internal/contextpack/builder.go` | Filter adjacency maps by EdgeKind, add doc candidate tier |
| ~ | `auto-graph/internal/contextpack/builder_test.go` | Test doc candidate priorities 15/35 |
| ~ | `auto-graph/internal/format/dot.go` | shape=note for doc nodes, style=dashed for doc edges |
| ~ | `auto-graph/internal/format/mermaid.go` | Hexagon shape for doc nodes, dotted arrow for doc edges |
| + | `auto-graph/e2e/testdata/doclinks-project/` | E2E fixture: TS project with autodoc tags, docs/, initialized as git repo |
| + | `auto-graph/e2e/testdata/golden/doclinks-project.json` | Golden file for doclinks e2e graph output |
| + | `auto-graph/testdata/fixtures/doclinks/` | Unit test fixture: minimal TS files with autodoc tags and docs/ |
| ~ | `auto-graph/internal/cli/code_graph_test.go` | Test --no-docs flag, doc nodes in output |
| ~ | `auto-graph/internal/cli/code_context_test.go` | Test --no-docs flag, doc inclusion in pack |

## Links

- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test

- [x] `auto-graph/internal/doclink/doclink_test.go` — unit tests for Scan, Enrich, soft-failure on non-git dirs
- [x] `auto-graph/internal/contextpack/builder_test.go` — doc candidate priority tests (P15 seed-linked, P35 non-seed)
- [x] `auto-graph/internal/format/format_test.go` — DOT/Mermaid render doc nodes with correct shapes
- [x] `auto-graph/e2e/e2e_test.go` — e2e: build binary, run against doclinks-project fixture, golden file comparison, --no-docs flag, context pack doc inclusion

## Execution Sequence

```
Phase 1 (autodoc pkg wrappers) --> Phase 2 (graph model + doclink) --> Phase 3 (CLI wiring) --> Phase 5 (integration tests)
                                                                   \-> Phase 4 (builder + formats) -/
```

## Plan

### Phase 1: Autodoc Public API Wrappers

Expose thin wrappers in `auto-doc/pkg/` so autograph can import autodoc's internal packages.

- [x] Step 1.1: Create `auto-doc/pkg/scan/scan.go`
  - Type aliases: `type Tag = linkscan.Tag`, `type ScanResult = linkscan.ScanResult`, `type MalformedTag = linkscan.MalformedTag`
  - Wrapper: `func ScanFiles(rootDir string) (ScanResult, error)` delegating to `linkscan.ScanFiles`
  - Verify: `go build ./pkg/scan/` compiles in the auto-doc module
- [x] Step 1.2: Create `auto-doc/pkg/docs/docs.go`
  - Type aliases: `type Entry = doctree.Entry`
  - Wrapper: `func WalkRepo(rootDir, docsDir string, ignores ...string) ([]Entry, error)` delegating to `doctree.WalkRepo`
  - Verify: `go build ./pkg/docs/` compiles in the auto-doc module
- [x] Step 1.3: Verify autodoc still builds and tests pass
  - Run: `cd auto-doc && go build ./... && go test ./...`
  - Verify: zero failures, no regressions
- [x] Step 1.4: Commit: `feat(007): phase 1 - autodoc public API wrappers`

### Phase 2: Graph Model + Doclink Package

Add doc node/edge kinds and the core doclink scanning and enrichment logic.

- [x] Step 2.1: Update `auto-graph/go.mod` to add autodoc dependency
  - Add `require github.com/datadyne-io/autodoc v0.0.0`
  - Add `replace github.com/datadyne-io/autodoc => ../auto-doc`
  - Run: `cd auto-graph && go mod tidy`
  - Verify: `go build ./...` succeeds
- [x] Step 2.2: Add constants to `auto-graph/internal/graph/model.go`
  - Add `NodeDoc NodeKind = "doc"` and `EdgeDocLink EdgeKind = "doc_link"` to the existing const block
  - Verify: `go build ./...` succeeds
- [x] Step 2.3: Create `auto-graph/internal/doclink/doclink.go`
  - Define `Link` struct: `{SourceFile, DocFile, DocID, DocTitle string}`
  - Implement `Scan(projectRoot string, warn io.Writer) ([]Link, error)`:
    - Call `scan.ScanFiles(projectRoot)` — if error, log warning to `warn`, return empty slice (soft-failure)
    - Call `docs.WalkRepo(projectRoot, "")` — if error, log warning to `warn`, return empty slice (soft-failure)
    - Build `docByID map[string]docs.Entry` from WalkRepo results
    - For each tag, resolve `tag.DocId` to a doc entry; skip orphaned tags
    - Convert tag.FilePath to repo-relative path
    - Return deduplicated `[]Link` (unique by SourceFile+DocFile pair)
  - Implement `Enrich(g *graph.Graph, links []Link)`:
    - Build set of existing node IDs from `g.Nodes`
    - For each link, if `link.SourceFile` is in the graph:
      - Add `NodeDoc` node for `link.DocFile` (if not already added), with `Attrs: {"title": link.DocTitle}`
      - Add `EdgeDocLink` edge from `link.SourceFile` to `link.DocFile`
    - No-op when links is empty
  - Verify: `go build ./internal/doclink/` succeeds
- [x] Step 2.4: Create `auto-graph/internal/doclink/doclink_test.go`
  - Test `Scan` with fixture that has autodoc tags + docs with matching IDs → returns correct Links
  - Test `Scan` soft-failure: non-git directory → returns empty slice, no error, warning logged
  - Test `Scan` soft-failure: git dir but no docs → returns empty slice
  - Test `Enrich` adds NodeDoc nodes and EdgeDocLink edges to graph
  - Test `Enrich` deduplicates: two tags in same file pointing to same doc → one edge
  - Test `Enrich` skips links where SourceFile is not in the graph
  - Test `Enrich` no-op when links is empty
  - Verify: `cd auto-graph && go test ./internal/doclink/` all pass
- [x] Step 2.5: Commit: `feat(007): phase 2 - graph model + doclink package`

### Phase 3: CLI Wiring

Add `--no-docs` flag to both commands and call doclink enrichment after Build.

- [x] Step 3.1: Update `auto-graph/internal/cli/code_graph.go`
  - Add `var noDocs bool` flag: `cmd.Flags().BoolVar(&noDocs, "no-docs", false, "exclude documentation links from graph")`
  - After `codegraph.Build` returns (line 69), if `!noDocs`:
    - Call `doclink.Scan(projectRoot, cmd.ErrOrStderr())`
    - Call `doclink.Enrich(g, links)`
  - Verify: `go build ./cmd/autograph/` succeeds
- [x] Step 3.2: Update `auto-graph/internal/cli/code_context.go`
  - Add `var noDocs bool` flag with same definition
  - After `codegraph.Build` returns (line 90), if `!noDocs`:
    - Call `doclink.Scan(projectRoot, cmd.ErrOrStderr())`
    - Call `doclink.Enrich(g, links)`
  - Verify: `go build ./cmd/autograph/` succeeds
- [x] Step 3.3: Verify: `cd auto-graph && go vet ./... && go test ./...` all pass
- [x] Step 3.4: Commit: `feat(007): phase 3 - CLI --no-docs flag and doclink wiring`

### Phase 4: Context Pack Builder + Format Renderers

Filter adjacency maps by edge kind, add doc candidate tier, and style doc nodes in renderers.

- [x] Step 4.1: Update `buildAdjacencyMaps` in `auto-graph/internal/contextpack/builder.go`
  - Filter to `EdgeImport` edges only: add `if e.Kind != graph.EdgeImport { continue }` in the loop at line 176
  - Verify: existing tests still pass (`go test ./internal/contextpack/`) since all current edges are EdgeImport
- [x] Step 4.2: Add `buildDocAdjacencyMaps` function in `builder.go`
  - Same structure as `buildAdjacencyMaps` but filters to `EdgeDocLink` edges only
  - Returns `docFwd, docRev map[string][]graph.Edge`
- [x] Step 4.3: Extend `collectCandidates` signature and implementation
  - Add `docFwd` parameter to `collectCandidates`
  - After Priority 10 (direct runtime deps) block, add Priority 15 block:
    - For each seed, iterate `docFwd[seed]` → add candidate with `role: "doc"`, `priority: 15`, `distance: 1`, reason: `"doc linked from seed <seed>"`
  - After Priority 30 (type-only) block, add Priority 35 block for non-seed doc links:
    - For each non-seed candidate already added, iterate `docFwd[candidate.path]` → add candidate with `role: "doc"`, `priority: 35`, `distance: 2`, reason: `"doc linked from dependency <path>"`
  - Update the `Build` function call site: pass `docFwd` from `buildDocAdjacencyMaps(opts.Graph)`
- [x] Step 4.4: Add builder tests in `builder_test.go`
  - Test: graph with seed + doc_link edge → doc file included at priority 15 with role "doc"
  - Test: graph with seed → dep → doc_link edge → doc file included at priority 35
  - Test: graph with no doc_link edges → identical behavior to before (regression check)
  - Test: doc file competes for budget → respects token limit, appears in OmittedCandidates if too large
  - Verify: `go test ./internal/contextpack/` all pass
- [x] Step 4.5: Update `auto-graph/internal/format/dot.go`
  - Build a `kindMap` from `g.Nodes` mapping node ID → NodeKind
  - In WriteDOT, emit node declarations before edges:
    - For `NodeDoc` nodes: `"path" [shape=note];`
    - For `NodeFile` nodes: no explicit declaration needed (default shape)
  - For `EdgeDocLink` edges: `"source" -> "target" [style=dashed];`
  - Verify: `go build ./internal/format/` succeeds
- [x] Step 4.6: Update `auto-graph/internal/format/mermaid.go`
  - Build a `kindMap` from `g.Nodes` mapping node ID → NodeKind
  - For `NodeDoc` nodes: render as `id{{path}}` (hexagon shape) instead of `id[path]` (rectangle)
  - For `EdgeDocLink` edges: render as `-.->` (dotted arrow) instead of `-->` (solid)
  - Verify: `go build ./internal/format/` succeeds
- [x] Step 4.7: Add format tests in `format_test.go`
  - Test DOT output with mixed file+doc nodes: doc nodes have `[shape=note]`, doc edges have `[style=dashed]`
  - Test Mermaid output with mixed nodes: doc nodes use `{{}}`, doc edges use `-.->` 
  - Test JSON output: doc nodes and edges serialize correctly (no special handling needed)
  - Verify: `go test ./internal/format/` all pass
- [x] Step 4.8: Verify full test suite: `cd auto-graph && go test ./...`
- [x] Step 4.9: Commit: `feat(007): phase 4 - builder doc priorities + format renderers`

### Phase 5: E2E Tests + Fixtures

E2E tests follow the established pattern: build binary, run against fixture dir, assert structural properties and golden file match. Unit test fixtures use `testdata/fixtures/` for builder/doclink tests.

- [x] Step 5.1: Create unit test fixture `auto-graph/testdata/fixtures/doclinks/`
  - `tsconfig.json` (minimal)
  - `src/app.ts` — imports `./helper`, contains `// [autodoc(DOCID1@HASH1, SCOPE1)]` tag
  - `src/helper.ts` — imported by app.ts, contains `// [autodoc(DOCID2@HASH2, SCOPE2)]` tag
  - `src/utils.ts` — imported by helper.ts, no autodoc tags
  - `docs/guide.md` — frontmatter with `id: DOCID1`, matching title/hash
  - `docs/api.md` — frontmatter with `id: DOCID2`, matching title/hash
  - Verify: files exist and contain valid autodoc tags
- [x] Step 5.2: Create e2e fixture `auto-graph/e2e/testdata/doclinks-project/`
  - Same structure as unit fixture, but initialized as a git repo (`git init && git add -A && git commit -m "init"`)
  - This is required because `linkscan.ScanFiles` shells out to `git ls-files`
  - Verify: `git -C <fixture> log` shows the init commit
- [x] Step 5.3: Add e2e tests in `auto-graph/e2e/e2e_test.go`
  - `TestDocLinksProjectJSON`: build binary → `code graph <doclinks-project>` → parse JSON → assert NodeDoc nodes exist, EdgeDocLink edges exist, referential integrity holds. Golden file comparison with `doclinks-project.json`
  - `TestDocLinksProjectNoDocs`: build binary → `code graph <doclinks-project> --no-docs` → assert only NodeFile and EdgeImport in output
  - `TestDocLinksProjectDOT`: assert DOT output contains `[shape=note]` for doc nodes
  - `TestDocLinksProjectMermaid`: assert Mermaid output contains `{{` for doc nodes
  - Generate golden files with `-update` flag on first run
  - Verify: `cd auto-graph && go test -tags e2e ./e2e/ -run TestDocLinks` all pass
- [x] Step 5.4: Add e2e context pack tests in `auto-graph/e2e/e2e_test.go`
  - `TestDocLinksContextPack`: build binary → `code context <doclinks-project> --file src/app.ts --token-limit 50000` → assert output contains doc file content with role "doc"
  - `TestDocLinksContextPackNoDocs`: same with `--no-docs` → assert no doc files in output
  - Verify: `cd auto-graph && go test -tags e2e ./e2e/ -run TestDocLinks` all pass
- [x] Step 5.5: Run full test suite
  - Run: `cd auto-graph && go test ./... && go vet ./...`
  - Run: `cd auto-graph && go test -tags e2e ./e2e/`
  - Run: `cd auto-doc && go test ./... && go vet ./...`
  - Verify: zero failures in both modules
- [x] Step 5.6: Commit: `feat(007): phase 5 - e2e tests and doclinks fixtures`

## Success Criteria

- [x] `cd auto-doc && go test ./...` passes (no regressions from pkg/ wrappers)
- [x] `cd auto-graph && go test ./...` passes (all new + existing tests)
- [x] `cd auto-graph && go vet ./...` clean
- [x] AC-1: `autograph code graph` on doclinks fixture produces JSON with `"kind": "doc"` nodes and `"kind": "doc_link"` edges; DOT has `[shape=note]`; Mermaid has `{{}}`
- [x] AC-2: `autograph code graph --no-docs` on doclinks fixture produces output with only `"file"` nodes and `"import"` edges
- [x] AC-3: `autograph code context` on doclinks fixture includes doc files with content in pack output
- [x] AC-4: Doc linked to seed file appears at priority 15; doc linked to non-seed dep appears at priority 35
- [x] AC-5: `autograph code context --no-docs` produces pack with only code files
- [x] AC-6: Running either command on a non-git directory or project without autodoc tags produces identical output to current behavior with no errors

## Open Questions

(none — all resolved in requirements.md and solution.md review)
