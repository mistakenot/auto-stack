# Solution: Task 007

<!-- REJECTED(P1): Required planning docs are missing
REVIEW: The review workflow requires `requirements.md`, `solution.md`, `context.md`, and `plan.md`, but `docs/tasks/007-autograph-doc-links/` currently contains only `requirements.md` and `solution.md`. Without `context.md` there is no codebase-grounded map of the current signatures and patterns, and without `plan.md` there is no executable phase order or verification command list. Add those docs before implementation.
AUTHOR: This is expected at the current workflow stage. The pipeline is sequential: requirements → solution → plan (with context). We are currently at the solution stage. `context.md` and `plan.md` will be written in the next step (`/new-plan`). Review was requested on the solution doc specifically.
-->

## Approach

### Constraint: Go `internal/` visibility

Autodoc's `linkscan`, `doctree`, and `frontmatter` packages live under `internal/`, which Go prevents other modules from importing. To respect the "import autodoc" decision without restructuring autodoc's internals, we add thin public API wrappers in `auto-doc/pkg/` that re-export just the types and functions autograph needs. Autograph then depends on `github.com/datadyne-io/autodoc` via a `replace` directive pointing to the local monorepo path.

### Steps

1. **Expose autodoc public API** — create `auto-doc/pkg/scan/` and `auto-doc/pkg/docs/` packages that wrap `linkscan.ScanFiles` and `doctree.WalkRepo` with type aliases for their return types. Autodoc's internal code stays untouched.

2. **Add graph model constants** — extend `graph/model.go` with `NodeDoc` kind and `EdgeDocLink` kind. The model already has comments anticipating these additions.

3. **Create `doclink` package in autograph** — new package `auto-graph/internal/doclink/` with two functions:
   - `Scan(projectRoot string, warn io.Writer) ([]Link, error)` — calls autodoc's `ScanFiles` and `WalkRepo`, matches tags to doc entries by ID, returns resolved code→doc links. Only tags whose source file is in the graph are included. **Soft-failure behavior**: if `ScanFiles` fails (e.g. not a git repo) or `WalkRepo` finds no docs, logs a warning to `warn` and returns an empty `[]Link` — never propagates these as errors, so `Enrich` becomes a no-op and existing behavior is preserved.
   - `Enrich(g *graph.Graph, links []Link)` — adds `NodeDoc` nodes and `EdgeDocLink` edges to an existing graph. Deduplicates: multiple tags from the same code file to the same doc produce one edge. No-op when `links` is empty.

<!-- RESOLVED(P1): Default doc scanning can fail on non-git projects
REVIEW: Current `codegraph.Build` uses filesystem discovery and can graph any directory with `go.mod` or `tsconfig.json`. The proposed default path calls autodoc `linkscan.ScanFiles`, whose implementation shells out to `git -C <root> ls-files` and returns an error when the target is not a Git worktree (`auto-doc/internal/linkscan/linkscan.go`). Because docs are enabled by default, `autograph code graph/context <dir>` would start failing before producing current output for non-git projects, violating AC-6 and the existing behavior. The plan needs an explicit no-op/fallback for this error class, or a scanner wrapper that preserves autograph's filesystem support.
AUTHOR: Valid concern. Updated step 3: `doclink.Scan` now treats errors from `linkscan.ScanFiles` (git not available) and `doctree.WalkRepo` (no docs found) as soft failures — it logs a warning to stderr and returns an empty `[]Link`, so `Enrich` becomes a no-op. This preserves AC-6 and existing behavior for non-git directories.
-->

4. **Wire `--no-docs` flag into CLI commands** — both `code graph` and `code context` get a `--no-docs` boolean flag (default false). When false, CLI calls `doclink.Scan` + `doclink.Enrich` after `codegraph.Build`. When true, skips the enrichment step entirely.

5. **Extend context pack candidate collection** — split adjacency map construction: `buildAdjacencyMaps` filters to `EdgeImport` edges only (no behavior change for existing code since all current edges are imports). A new `buildDocAdjacencyMaps` indexes only `EdgeDocLink` edges into separate `docFwd`/`docRev` maps. Add a new step in `collectCandidates` that iterates the doc-specific maps: doc files linked to seed files become candidates at Priority 15 with `role: "doc"`, doc files linked to non-seed included files become candidates at Priority 35 with `role: "doc"`. The builder reads doc file content and includes it in the pack like any other file.

<!-- RESOLVED(P1): `doc_link` edges will be selected as import dependencies
REVIEW: I checked `auto-graph/internal/contextpack/builder.go`: `buildAdjacencyMaps` indexes every graph edge, and `collectCandidates`' direct dependency/dependent loops iterate `fwd`/`rev` without checking `EdgeKind`; `isRuntimeImport` returns true when import attrs are absent. If `EdgeDocLink` edges are added to the same graph, a seed's linked doc will be added first as role `"dependency"` at priority 10, so the proposed priority 15 `role: "doc"` branch will not win. The solution needs to require filtering existing import-neighborhood logic to `graph.EdgeImport` or using separate adjacency maps before adding doc-specific candidates.
AUTHOR: Valid catch — confirmed that `isRuntimeImport` returns true when no import kinds are set (line 432). Fix: `buildAdjacencyMaps` will be updated to filter to `EdgeImport` edges only. A separate `buildDocAdjacencyMaps` function will index only `EdgeDocLink` edges into its own `fwd`/`rev` maps. The doc candidate step in `collectCandidates` will use the doc-specific maps. This ensures import-based traversal never sees doc edges, and doc priority tiers (15/35) are respected. Updated step 5 in the approach.
-->

6. **Update format renderers** — DOT renderer gives doc nodes a `[shape=note]` attribute. Mermaid renderer uses `{{path}}` (hexagon shape) for doc nodes. JSON renderer needs no changes (it serializes the graph model as-is).

## Files

```
+ auto-doc/pkg/scan/scan.go              # public API: type aliases + ScanFiles wrapper
+ auto-doc/pkg/docs/docs.go              # public API: type aliases + WalkRepo wrapper
~ auto-graph/go.mod                      # add autodoc dependency + replace directive
~ auto-graph/internal/graph/model.go     # add NodeDoc, EdgeDocLink constants
+ auto-graph/internal/doclink/doclink.go # Scan + Enrich functions
~ auto-graph/internal/cli/code_graph.go  # add --no-docs flag, call doclink after Build
~ auto-graph/internal/cli/code_context.go # add --no-docs flag, call doclink after Build
~ auto-graph/internal/contextpack/builder.go # add doc candidate collection step in collectCandidates
~ auto-graph/internal/format/dot.go      # shape=note for doc nodes
~ auto-graph/internal/format/mermaid.go  # hexagon shape for doc nodes
+ auto-graph/internal/doclink/doclink_test.go  # unit tests for Scan + Enrich
~ auto-graph/internal/contextpack/builder_test.go # test doc candidate priorities
~ auto-graph/internal/cli/code_graph_test.go   # test --no-docs flag
~ auto-graph/internal/cli/code_context_test.go # test --no-docs flag + doc inclusion
+ auto-graph/testdata/doclinks/          # fixture project with autodoc tags
```

### Key type outlines

```go
// auto-graph/internal/doclink/doclink.go

type Link struct {
    SourceFile string // relative path of code file containing the tag
    DocFile    string // relative path of the linked doc file
    DocID      string // 8-char hex doc identifier
    DocTitle   string // doc title from frontmatter
}

func Scan(projectRoot string, warn io.Writer) ([]Link, error)
func Enrich(g *graph.Graph, links []Link)
```

```go
// auto-graph/internal/graph/model.go (additions)

const (
    NodeDoc     NodeKind = "doc"
    EdgeDocLink EdgeKind = "doc_link"
)
```

```go
// auto-doc/pkg/scan/scan.go

type Tag = linkscan.Tag          // re-exported via type alias
type ScanResult = linkscan.ScanResult

func ScanFiles(rootDir string) (ScanResult, error)
```

## Test Coverage

| AC  | Test Type   | File                                          |
|-----|-------------|-----------------------------------------------|
| AC-1 | integration | auto-graph/internal/cli/code_graph_test.go    |
| AC-1 | unit        | auto-graph/internal/format/format_test.go     |
| AC-2 | integration | auto-graph/internal/cli/code_graph_test.go    |
| AC-3 | integration | auto-graph/internal/cli/code_context_test.go  |
| AC-3 | unit        | auto-graph/internal/contextpack/builder_test.go |
| AC-4 | unit        | auto-graph/internal/contextpack/builder_test.go |
| AC-5 | integration | auto-graph/internal/cli/code_context_test.go  |
| AC-6 | unit        | auto-graph/internal/doclink/doclink_test.go   |

## Out of Scope

- Freshness checking of doc links (stale hash detection) -- autodoc's responsibility
- Scanning for doc links in markdown files (doc-to-doc links); only code-to-doc links
- Adding autodoc as a runtime binary dependency
- Modifying autodoc's tag format or behavior
- Doc link support for languages other than TypeScript and Go
- Promoting autodoc's full internal packages to public -- only thin wrappers are exposed

## Rejected Alternatives

- **Reimplement tag scanning in autograph**: Would avoid the cross-module dependency, but duplicates the tag regex, frontmatter parsing, and doc walking logic (~150 lines). Rejected because the user explicitly chose the import approach, and the duplication creates a maintenance burden if the tag format evolves.
- **Move autodoc internal packages to public**: Would restructure `internal/linkscan` → `linkscan`, updating all import paths across autodoc. Rejected because it's a larger blast radius for autodoc and the thin `pkg/` wrapper achieves the same result with zero changes to existing code.
- **Shell out to `autodoc graph` CLI**: Would avoid any Go-level coupling by having autograph execute `autodoc graph --format json` and parse the output. Rejected because it adds a runtime binary dependency, is slower, and couples to CLI output format instead of stable Go types.
- **Shared `auto-lib` module**: Extract `linkscan`, `doctree`, `frontmatter` into a new `auto-lib/` module that both autodoc and autograph depend on. Cleanest long-term but premature — only one consumer (autograph) exists today. Can be revisited if more tools need these packages.
