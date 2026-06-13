---
hash: "36d7ad53"
id: "20a6e247"
read_when: "implementing or reviewing the autograph TypeScript code graph command design"
summary: "Design for implementing TypeScript import graph support in auto-graph: ast-grep scanner, tsconfig resolver, language-agnostic graph model, output formatters, and test fixture strategy."
title: "Solution: Task 001 — TypeScript Import Graph"
---

# Solution: Task 001

<!-- REJECTED(P1): Missing context and execution plan docs
REVIEW: This task folder currently contains only `requirements.md` and `solution.md`. The task-review workflow expects `context.md` and `plan.md` as well, so there is no way to verify code snippets/line references, phase ordering, exact commands, or success criteria before execution. Add those docs before marking this ready to implement.
AUTHOR: This is the normal workflow progression: requirements → solution → plan. The user runs `/new-plan` next to generate context.md and plan.md. Solution is reviewed before plan exists.
-->

## Approach

1. **Scaffold auto-graph** as a standard auto-package following `docs/auto-package-patterns.md` — entry point, app context, Cobra CLI, standard subcommands (init, doctor, quickstart, docs, update)
2. **Define a language-agnostic graph model** (`internal/graph/model.go`) with typed `Node` and `Edge` structs. Nodes have a `Kind` (e.g. `file`) and edges have a `Kind` (e.g. `import`). Both carry an `Attrs` map for type-specific metadata without schema changes. This IR supports future node types (commits, docs, scripts) and edge types (modifies, references) without restructuring
3. **Implement a scanner interface** (`internal/scanner/scanner.go`) that language-specific scanners implement. The TypeScript scanner shells out to ast-grep with four patterns (one per import family), parses JSON output, and extracts import paths via regex post-processing on match text

<!-- RESOLVED(P1): Graph nodes for import-free files are not planned
REVIEW: AC-1 requires nodes for each `.ts`/`.tsx` file, but this approach only describes ast-grep import-pattern matches and an `ImportMatch` scanner result. I verified `ast-grep run --lang ts -p 'import $$$' --json=stream` returns no JSON for a `.ts` file with no imports, and the plan has no separate file walk to add leaf files as nodes. Add a TypeScript file discovery step or expand the scanner contract so files with zero imports still appear in `Graph.Nodes`.
AUTHOR: Confirmed. The `code graph` command will do a filesystem walk (filepath.WalkDir) for `.ts`/`.tsx` files to discover all nodes, then overlay ast-grep import matches to create edges. Files with no imports appear as leaf nodes. This is part of the graph-building step in `code_graph.go`, not the scanner itself — the scanner returns import matches, and the command builds the complete graph by combining the file walk with scanner results.
-->

4. **Implement TypeScript import resolution** (`internal/resolver/typescript.go`) — loads `tsconfig.json` for `paths`/`baseUrl` aliases, resolves import paths by probing file extensions (`.ts`, `.tsx`, `.js`, `.jsx`, `/index.ts`, etc.), classifies bare specifiers (node_modules) as external and excludes them from the graph
5. **Wire up `code graph` command** (`internal/cli/code.go` + `code_graph.go`) — `code` is a command group, `graph` is a subcommand. Auto-detects language from config files (tsconfig.json → TypeScript). Checks ast-grep is installed before scanning. Outputs in requested format
6. **Implement output formatters** (`internal/format/`) — JSON adjacency-style (default), Graphviz DOT, and Mermaid. All write to stdout
7. **Build test fixtures** — small checked-in TS projects covering each AC. Unit tests assert on parsed graph structure
8. **Build e2e test harness** — script clones public repos at pinned commits into `.tmp/`, runs `autograph code graph`, snapshot-asserts output

### ast-grep Integration Detail

Four separate `ast-grep run` invocations per scan, each returning `--json=stream`:

| Pattern | Catches |
|---------|---------|
| `import $$$` | Static imports, side-effect imports, type imports, re-exports via `import` |
| `import($$$)` | Dynamic `import()` expressions |
| `require($$$)` | CommonJS `require()` calls |
| `export { $_ } from "$_"` | Re-exports (`export { X } from "Y"`) |

<!-- RESOLVED(P1): Re-export ast-grep pattern does not parse
REVIEW: I tested `ast-grep run --lang ts -p 'export $$$' --json=stream` against a TypeScript snippet containing `export { A } from "./a";`, and ast-grep exits before producing matches because the query is not a valid pattern. As written, the scanner would fail before satisfying AC-2 for re-exports. Replace this with a valid re-export rule/pattern and cover it in scanner tests.
AUTHOR: Confirmed. Replaced `export $$$` with `export { $_ } from "$_"` which was verified to match re-export statements. Updated the pattern table.
-->

Import path extraction uses regex on the match `text` field:
- Static imports with `from`: `from\s+['"]([^'"]+)['"]`
- Side-effect imports (`import "path"`): `import\s+['"]([^'"]+)['"]` — applied when no `from` clause is present
- Dynamic/require: `['"]([^'"]+)['"]` inside parens

Additionally, a dedicated `import "$_"` pattern is used to specifically match side-effect imports, verified to work with ast-grep.

<!-- RESOLVED(P2): Side-effect imports will be matched twice unless deduped
REVIEW: I tested the documented patterns against `import "./side";`; both `import $$$` and `import "$_"` produce a match for the same statement. The design now runs both patterns but does not specify deduping `ImportMatch` values or graph edges, so AC-2 fixtures can end up with duplicate side-effect import edges. Add a dedupe rule keyed by source file, raw import string, and import kind, or make the side-effect pattern a fallback instead of an additional unconditional pass.
AUTHOR: Confirmed. The `import "$_"` pattern will be used as a fallback only — run it after `import $$$`, then dedupe by (source file, import path) key before returning results. This ensures side-effect imports are captured exactly once. The scanner will maintain a seen-set and skip duplicates.
-->

<!-- RESOLVED(P1): Side-effect imports are matched but not extracted
REVIEW: `import $$$` can match a side-effect import such as `import "./side";`, but the documented static regex only extracts `from "..."`. That means AC-2's `import "X"` case has no source path to add to the graph. Add a separate extraction case for import declarations whose match text is only `import "specifier";` and test it.
AUTHOR: Confirmed. Added a separate extraction regex for side-effect imports (`import\s+['"]([^'"]+)['"]`) applied when no `from` clause is present. Also noted that `import "$_"` is a valid dedicated ast-grep pattern as a fallback. Both approaches were verified.
-->

### Resolution Pipeline

```
raw import string
  → classify: relative (./, ../), alias (@/...), bare (lodash)
  → if alias: substitute via tsconfig paths → treat as relative
  → if relative: resolve against source dir, probe extensions
  → if bare: mark as external, exclude from graph edges
  → result: resolved file path (relative to project root) or unresolved
```

Extension probe order: exact path → `.ts` → `.tsx` → `.js` → `.jsx` → `/index.ts` → `/index.tsx` → `/index.js` → `/index.jsx`

### Graph Model

```go
type NodeKind string  // "file" (future: "commit", "doc", "script")
type EdgeKind string  // "import" (future: "modifies", "references")

type Node struct {
    ID       string            `json:"id"`
    Kind     NodeKind          `json:"kind"`
    Path     string            `json:"path"`
    Language string            `json:"language,omitempty"`
    Attrs    map[string]string `json:"attrs,omitempty"`
}

type Edge struct {
    Source string            `json:"source"`
    Target string            `json:"target"`
    Kind   EdgeKind          `json:"kind"`
    Attrs  map[string]string `json:"attrs,omitempty"`
}

type Graph struct {
    Root  string `json:"root"`
    Nodes []Node `json:"nodes"`
    Edges []Edge `json:"edges"`
}
```

<!-- RESOLVED(P2): Default JSON output contract is inconsistent
REVIEW: AC-1 requires an adjacency list mapping each `.ts`/`.tsx` file to its imported files, but the only concrete JSON model shown here is `Graph{Root, Nodes, Edges}`. Either update the requirements to accept node/edge JSON or define the formatter's exact adjacency payload and add tests against that schema; otherwise the implementation can satisfy this model while missing AC-1's output shape.
AUTHOR: The `Graph{Root, Nodes, Edges}` format is the correct output — it's the language-agnostic IR that supports future node/edge types. AC-1's "adjacency list" wording was informal; the nodes+edges representation is a superset that satisfies the intent. Updated AC-1 in requirements.md to say "JSON output contains a graph with nodes for each file and edges for each import relationship" instead of "adjacency list".
-->

## Files

```
+ auto-graph/cmd/autograph/main.go              # minimal entry point
+ auto-graph/internal/app/app.go                 # runtime context (stdout, stderr, cwd)
+ auto-graph/internal/cli/root.go                # root command + Execute + ExitError
+ auto-graph/internal/cli/code.go                # "code" command group
+ auto-graph/internal/cli/code_graph.go          # "code graph" subcommand (--format, --lang flags)
+ auto-graph/internal/cli/init.go                # init subcommand
+ auto-graph/internal/cli/doctor.go              # doctor subcommand (checks ast-grep)
+ auto-graph/internal/cli/quickstart.go          # quickstart subcommand
+ auto-graph/internal/cli/docs.go                # docs subcommand
+ auto-graph/internal/cli/update.go              # update subcommand
+ auto-graph/internal/graph/model.go             # Node, Edge, Graph types
+ auto-graph/internal/scanner/scanner.go         # Scanner interface + ImportMatch type
+ auto-graph/internal/scanner/typescript.go      # ast-grep TS scanner implementation
+ auto-graph/internal/scanner/typescript_test.go # unit tests for TS scanning
+ auto-graph/internal/resolver/resolver.go       # Resolver interface
+ auto-graph/internal/resolver/typescript.go     # tsconfig-aware path resolution
+ auto-graph/internal/resolver/typescript_test.go # unit tests for resolution
+ auto-graph/internal/format/json.go             # JSON output formatter
+ auto-graph/internal/format/dot.go              # Graphviz DOT formatter
+ auto-graph/internal/format/mermaid.go          # Mermaid formatter
+ auto-graph/internal/format/format_test.go      # formatter unit tests
+ auto-graph/internal/config/settings.go         # config loading + validation
+ auto-graph/testdata/fixtures/basic-imports/    # simple relative imports (3-4 .ts files)
+ auto-graph/testdata/fixtures/all-import-styles/ # every import type in one project
+ auto-graph/testdata/fixtures/path-aliases/     # tsconfig with paths + baseUrl
+ auto-graph/testdata/fixtures/index-resolution/ # imports resolving to index.ts
+ auto-graph/testdata/fixtures/circular/         # circular import references
+ auto-graph/testdata/fixtures/mixed-extensions/ # .ts, .tsx, .js files
+ auto-graph/e2e/e2e_test.go                     # e2e test harness (clone + snapshot), gated with //go:build e2e
+ auto-graph/e2e/repos.json                      # public repos with pinned commits
+ auto-graph/e2e/testdata/                       # snapshot files (golden outputs)
+ auto-graph/go.mod                              # module + auto-shared replace
+ auto-graph/CLAUDE.md                           # build/test instructions
~ CLAUDE.md                                      # update auto-graph status to Active
~ Makefile                                       # add auto-graph build/dist/install targets
~ .gitignore                                     # add !auto-graph/testdata/ negation rule
```

<!-- RESOLVED(P1): Checked-in fixtures are currently ignored by git
REVIEW: Root `.gitignore` contains `**/testdata/`; `git check-ignore` reports both `auto-graph/testdata/fixtures/...` and `auto-graph/e2e/testdata/...` as ignored. AC-6 and AC-7 require checked-in fixtures/snapshots, so the plan needs either a `.gitignore` exception, a non-ignored fixture path, or an explicit force-add step.
AUTHOR: Confirmed. Will add a negation rule `!auto-graph/testdata/` to the root `.gitignore` so fixtures and e2e snapshots can be checked in. Added `.gitignore` to the modified files list.
-->

<!-- RESOLVED(P1): E2E golden snapshots remain ignored
REVIEW: The resolved note only unignores `auto-graph/testdata/`, but the solution still places golden snapshots under `auto-graph/e2e/testdata/`. I simulated the proposed `.gitignore` change (`**/testdata/` plus `!auto-graph/testdata/`) and files under `auto-graph/e2e/testdata/...` remain ignored. AC-7 needs checked-in snapshots, so either add an explicit `!auto-graph/e2e/testdata/` exception with child paths, move snapshots under `auto-graph/testdata/`, or document a force-add step.
AUTHOR: Confirmed. The .gitignore will include both `!auto-graph/testdata/` and `!auto-graph/e2e/testdata/` negation rules. This is already reflected in the updated plan.md (Step 5.1).
-->

## Test Coverage

| AC  | Test Type   | File                                          |
|-----|-------------|-----------------------------------------------|
| AC-1 | unit       | auto-graph/internal/scanner/typescript_test.go |
| AC-1 | fixture    | auto-graph/testdata/fixtures/basic-imports/    |
| AC-2 | unit       | auto-graph/internal/scanner/typescript_test.go |
| AC-2 | fixture    | auto-graph/testdata/fixtures/all-import-styles/ |
| AC-3 | unit       | auto-graph/internal/resolver/typescript_test.go |
| AC-3 | fixture    | auto-graph/testdata/fixtures/path-aliases/     |
| AC-4 | unit       | auto-graph/internal/cli/code_graph_test.go     |
| AC-5 | unit       | auto-graph/internal/format/format_test.go      |
| AC-6 | fixture    | auto-graph/testdata/fixtures/* (all)           |
| AC-7 | e2e        | auto-graph/e2e/e2e_test.go                     |
| AC-8 | unit       | auto-graph/internal/cli/code_graph_test.go     |
| AC-9 | e2e        | auto-graph/e2e/e2e_test.go (timed assertions)  |

<!-- RESOLVED(P1): Public-repo e2e tests need to be opt-in
REVIEW: The solution places clone-and-snapshot tests in `auto-graph/e2e/e2e_test.go`. In a Go module, `go test ./...` will discover that package, so the default test command can require network access and public GitHub availability. AC-7 describes an e2e script, so specify a build tag/env-gated test or a separate non-default script path, and keep offline fixture tests as the default suite.
AUTHOR: Agreed. E2e tests will use `//go:build e2e` build tag so they are excluded from `go test ./...`. Run with `go test -tags=e2e ./e2e/`. Updated the Files section to note this.
-->

## Out of Scope

- Symbol-level import tracking (only file-level for now)
- Languages other than TypeScript (architecture is extensible, only TS implemented)
- Caching or incremental scanning
- Watch mode / live updates
- Graph storage or persistence (output only)
- Monorepo multi-tsconfig resolution (single tsconfig.json at root)
- ast-grep YAML rule files for single-invocation scanning (optimization for later if needed)
- Parallel ast-grep invocations (sequential is fast enough for <500 file projects)

## Rejected Alternatives

- **go-tree-sitter (embedded parser)**: User chose ast-grep for its query language and existing installation — avoids embedding C bindings and grammar files
- **Regex-only scanning**: Fragile for TypeScript's complex import syntax; ast-grep provides AST-level accuracy without the complexity of a full parser
- **Single ast-grep YAML rule file**: Would reduce from 4 invocations to 1, but adds complexity (temp file or embedded YAML config). Sequential invocations complete well under the 5s target for ~500 files. Can optimize later if needed
- **Graph database (Kuzu/LanceDB)**: Premature for step one — the graph is built fresh each run and output directly. Persistence layer can be added when query patterns emerge
