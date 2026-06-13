---
hash: "0b4bb9a9"
id: "e1b173ee"
read_when: "implementing autodoc tag integration in autograph or locating extension points for cross-module doc-link enrichment"
summary: "Codebase context for integrating autodoc [autodoc()] tags into autograph's graph and context pack, with precise file/line references for the graph model, build pipeline, context pack builder, CLI commands, format renderers, and autodoc packages."
title: "Context: Autograph Doc Links (Task 007)"
---

# Context: Task 007

Codebase facts for integrating autodoc's `[autodoc()]` tags into autograph's graph and context pack. See [solution.md](./solution.md) for the design.

## Key Files

### Graph Model

- `auto-graph/internal/graph/model.go:3-14` — NodeKind/EdgeKind types with comments anticipating `doc` and `references` kinds:
  ```go
  // NodeKind classifies graph nodes. Currently only "file" is supported;
  // future kinds (commit, doc, script) can be added without schema changes.
  type NodeKind string
  // EdgeKind classifies graph edges. Currently only "import" is supported;
  // future kinds (modifies, references) can be added without schema changes.
  type EdgeKind string
  const (
      NodeFile   NodeKind = "file"
      EdgeImport EdgeKind = "import"
  )
  ```

### Build Pipeline

- `auto-graph/internal/codegraph/build.go:27` — `func Build(projectRoot, lang string, warn io.Writer) (*graph.Graph, []Diagnostic, error)`
- `auto-graph/internal/codegraph/build.go:59-60` — injection point after graph construction:
  ```go
  g, diags := buildGraph(projectRoot, filePaths, matches, res, lang)
  return g, diags, nil
  ```

### Context Pack Builder

- `auto-graph/internal/contextpack/builder.go:19-36` — `BuildOptions` struct (takes `Graph`, `Seeds`, `TokenLimit`, `Estimator`)
- `auto-graph/internal/contextpack/builder.go:53` — `func Build(opts BuildOptions) (*Pack, error)`
- `auto-graph/internal/contextpack/builder.go:173-181` — `buildAdjacencyMaps` iterates ALL edges without filtering EdgeKind:
  ```go
  func buildAdjacencyMaps(g *graph.Graph) (fwd map[string][]graph.Edge, rev map[string][]graph.Edge) {
      for _, e := range g.Edges {
          fwd[e.Source] = append(fwd[e.Source], e)
          rev[e.Target] = append(rev[e.Target], e)
      }
  }
  ```
- `auto-graph/internal/contextpack/builder.go:185` — `collectCandidates(seeds, fwd, rev, riskFlags, g)` — priority tiers:
  - Line 225: Priority 10 (direct runtime deps)
  - Line 238: Priority 20 (direct runtime dependents)
  - Line 251: Priority 25 (risk-flagged neighbors)
  - Line 275: Priority 30 (type-only neighbors)
  - Line 297: Priority 35 (cycle members)
  - Line 324: Priority 40 (second-hop runtime)
  - Line 368: Priority 50 (second-hop type-only)
- `auto-graph/internal/contextpack/builder.go:425-433` — `isRuntimeImport` returns true when no import_kinds attrs exist:
  ```go
  func isRuntimeImport(e graph.Edge) bool {
      kinds := getImportKinds(e)
      for _, k := range kinds { if k != "type_only" { return true } }
      return len(kinds) == 0 // default to runtime if no kinds specified
  }
  ```
- `auto-graph/internal/contextpack/builder.go:40-48` — candidate struct: `{path, role, priority, distance, reason, flags, rel}`
- `auto-graph/internal/contextpack/model.go:5-16` — Pack struct with Files, Relationships, Guidance, OmittedCandidates
- `auto-graph/internal/contextpack/model.go:25-32` — FileEntry struct: `{Path, Role, Reason, EstimatedTokens, Flags, Content}`

### CLI Commands

- `auto-graph/internal/cli/code_graph.go:14-44` — flag definitions at lines 40-41 (format, lang)
- `auto-graph/internal/cli/code_graph.go:69` — calls `codegraph.Build(projectRoot, lang, cmd.ErrOrStderr())`
- `auto-graph/internal/cli/code_context.go:14-46` — flag definitions at lines 38-41 (token-limit, file, format, lang)
- `auto-graph/internal/cli/code_context.go:90` — calls `codegraph.Build`, line 120 calls `contextpack.Build`

### Format Renderers

- `auto-graph/internal/format/dot.go:11-30` — `WriteDOT` iterates `g.Edges`, uses `buildPathMap` for node ID→path lookup. Currently renders all edges as plain `"source" -> "target"` with no shape differentiation.
- `auto-graph/internal/format/mermaid.go:15-30` — `WriteMermaid` iterates `g.Edges`, renders `sourceID[path] --> targetID[path]`. Square brackets = rectangle shape for all nodes.
- `auto-graph/internal/format/json.go:11-15` — `WriteJSON` serializes entire graph via `json.Encoder`; no changes needed.
- `auto-graph/internal/contextpack/markdown.go:44-61` — renders each `FileEntry` as `### path` with Role, Tokens, Flags, then fenced code block. Uses `inferFenceLanguage(path)` for syntax highlighting.

### Autodoc Packages (to wrap)

- `auto-doc/internal/linkscan/linkscan.go:90` — `func ScanFiles(rootDir string) (ScanResult, error)` — shells out to `git ls-files`; errors if not a git worktree
- `auto-doc/internal/linkscan/linkscan.go:65-74` — `Tag{FilePath, Line, DocId, DocHash, ScopeHash, RawTag, ScopeKind}`
- `auto-doc/internal/linkscan/linkscan.go:84-87` — `ScanResult{Tags []Tag, Malformed []MalformedTag}`
- `auto-doc/internal/doctree/doctree.go:97` — `func WalkRepo(rootDir, docsDir string, ignores ...string) ([]Entry, error)`
- `auto-doc/internal/doctree/doctree.go:18-31` — `Entry{RelPath, Id, Title, Summary, ReadWhen, Hash, Body, DocsRootRel, RepoRelPath, AbsPath}`
- `auto-doc/internal/frontmatter/frontmatter.go:27` — `func Parse(content string) Doc` (pure string parsing, no side effects)

### Go Module Structure

- `auto-graph/go.mod` — already uses `replace github.com/mistakenot/auto-shared => ../auto-shared`
- `auto-doc/go.mod` — same pattern: `replace github.com/mistakenot/auto-shared => ../auto-shared`
- Cross-module `replace` directives are an established monorepo pattern; adding `replace github.com/datadyne-io/autodoc => ../auto-doc` follows the same convention.

### Test Patterns

- `auto-graph/internal/contextpack/builder_test.go:14-48` — `setupBuilderFixture(t, files map[string]string, edges []graph.Edge)` creates temp dir with files + synthetic graph. Used by all builder tests.
- `auto-graph/internal/codegraph/build_test.go:12-19` — `fixtureDir(t, name)` resolves to `testdata/fixtures/<name>/`
- `auto-graph/internal/cli/code_graph_test.go` — tests call `runCodeGraph` directly with temp dirs and assert on stdout/stderr
- `auto-graph/testdata/fixtures/` — 13+ fixture dirs including `context-pack/`, `basic-imports/`, `go-basic-imports/`
- `auto-graph/testdata/golden/context-pack/` — golden files: `normal-budget.md`, `normal-budget.json`, `constrained-budget.md`, `constrained-budget.json`

## Patterns

- **Cross-module dependency**: `auto-shared` already shared via `replace` directives across modules. Task 007 extends this pattern to autodoc.
- **Candidate roles**: existing roles are `seed`, `dependency`, `dependent`, `type_dependency`, `type_dependent`, `cycle_neighbor`, `transitive_neighbor`. Task 007 adds `doc`.
- **Risk flags**: computed from import edges only (`side_effect_import`, `dynamic_import`, `reexport`, `cycle`, `high_fan_in`, `high_fan_out`, `entrypoint_like`, `test_like`). Doc nodes will not receive risk flags.
- **Soft-failure pattern**: task 005 established diagnostics-to-stderr for non-fatal issues. Task 007 uses the same pattern for git/doc-walk failures.
- **`linkscan.ScanFiles` ignores**: `.md` files, `_test.go`, and common build dirs are skipped. This means tags are only found in source code files, not in docs themselves.

## Related Tasks

- **Task 004 (context-pack)**: Established the builder, candidate priority system, token budget enforcement, and markdown/JSON rendering. All context pack patterns originate here.
- **Task 005 (alias-reexports)**: Extended the graph model with richer edge metadata (`import_kinds`). Established the pattern of threading diagnostics through `Build()` return values.
- **Task 006 (quote-jsonc-fixes)**: Refined scanner patterns and resolver behavior. No graph model changes.
