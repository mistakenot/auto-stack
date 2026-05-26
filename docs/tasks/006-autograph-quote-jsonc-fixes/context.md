# Context: Task 006

Codebase context for fixing ast-grep quote sensitivity and JSONC tsconfig parsing in autograph. See [solution.md](./solution.md).

## Key Files

### Scanner — quote-sensitive patterns

- `auto-graph/internal/scanner/typescript.go:72-80` — pattern list with 4 double-quote-only patterns:
  ```go
  {pattern: `export { $$$ } from "$_"`, kind: "reexport"},       // line 76
  {pattern: `export * from "$_"`, kind: "reexport"},             // line 77
  {pattern: `export type { $$$ } from "$_"`, kind: "reexport"},  // line 78
  {pattern: `import "$_"`, kind: "side-effect"},                 // line 79
  ```
  The `import $$$`, `import($$$)`, and `require($$$)` patterns use `$$$` which is quote-agnostic — no change needed.

- `auto-graph/internal/scanner/typescript.go:37-42` — extraction regexes already handle both quote styles:
  ```go
  reFromClause   = regexp.MustCompile(`from\s+['"]([^'"]+)['"]`)
  reSideEffect   = regexp.MustCompile(`import\s+['"]([^'"]+)['"]`)
  reQuotedString = regexp.MustCompile(`['"]([^'"]+)['"]`)
  ```
  Only the ast-grep patterns themselves need duplication; extraction logic works as-is.

- `auto-graph/internal/scanner/typescript.go:86-92` — dedup key is `(file, importPath, kind)`, so adding pattern variants won't create duplicate matches.

- `auto-graph/internal/scanner/typescript.go:94-95` — patterns loop: currently 7 patterns × 2 langs = 14 invocations. Adding 4 patterns raises this to 11 × 2 = 22.

### Resolver — strict JSON parse

- `auto-graph/internal/resolver/typescript.go:77-86` — `loadTSConfig` uses `json.Unmarshal` and silently returns on error:
  ```go
  if err := json.Unmarshal(data, &cfg); err != nil {
      return  // silent, no warning, r.loaded stays false
  }
  ```

- `auto-graph/internal/resolver/typescript.go:57-65` — `TypeScriptResolver` struct has no `io.Writer` field:
  ```go
  type TypeScriptResolver struct {
      mappings []pathMapping
      baseURL  string
      loaded   bool
  }
  ```

- `auto-graph/internal/resolver/typescript.go:70-74` — constructor takes only `projectRoot`:
  ```go
  func NewTypeScriptResolver(projectRoot string) *TypeScriptResolver
  ```

### Build pipeline — threading warnings

- `auto-graph/internal/codegraph/build.go:26` — `Build` signature:
  ```go
  func Build(projectRoot, lang string) (*graph.Graph, []Diagnostic, error)
  ```

- `auto-graph/internal/codegraph/build.go:36` — creates resolver:
  ```go
  res = resolver.NewTypeScriptResolver(projectRoot)
  ```

- `auto-graph/internal/cli/code_graph.go:69-78` — CLI handler already threads stderr for diagnostics:
  ```go
  g, diags, err := codegraph.Build(projectRoot, lang)
  errW := cmd.ErrOrStderr()
  for _, d := range diags { ... }
  ```

### Interfaces

- `auto-graph/internal/scanner/scanner.go` — `Scanner` interface: `Scan(dir string) ([]ImportMatch, error)`
- `auto-graph/internal/resolver/resolver.go` — `Resolver` interface: `Resolve(importPath, sourceFile, projectRoot string) (ResolveResult, error)`

### E2E test helpers

- `auto-graph/e2e/e2e_test.go:88-98` — `runAutograph` captures stdout and stderr separately, returns only stdout. Stderr is printed on failure.
- `auto-graph/e2e/e2e_test.go:621-678` — `TestEdgeReferentialIntegrity` auto-discovers fixtures with `tsconfig.json` or `go.mod`.

## Patterns

### Adding ast-grep patterns
Each `patternSpec` has a `pattern` string and `kind`. The scanner loops over all patterns × all langs (`ts`, `tsx`). Dedup by `(file, importPath, kind)` means identical matches from single-quote and double-quote pattern variants will be collapsed automatically.

### Emitting warnings to stderr
Task 005 established the pattern: `codegraph.Build()` returns `[]Diagnostic`, and the CLI handler writes them to `cmd.ErrOrStderr()`. For tsconfig warnings, the cleanest approach is to add an `io.Writer` to the resolver constructor and write directly when tsconfig parsing fails — this avoids changing the `Diagnostic` type which is specific to unresolved aliases.

### Fixture organization
Existing fixtures live under `auto-graph/testdata/fixtures/` (unit tests) and `auto-graph/e2e/testdata/` (e2e tests). Each fixture is a self-contained directory with `tsconfig.json` and source files. `TestEdgeReferentialIntegrity` automatically picks up new fixtures that have `tsconfig.json`.

### Test fixture quote styles
All existing TS fixtures use **double quotes only**. The `alias-reexports` fixture barrel file (`src/client/my-feature/index.ts`) uses `"./Widget"`. This is why the re-export bug was never caught.

## Related Tasks

- **Task 005** (`docs/tasks/005-code-graph-alias-reexports/`): Added path alias resolution and re-export detection. Established the resolver/scanner/diagnostic architecture that task 006 fixes bugs in. Commit: `5ea0d94`.
- **Task 001** (`docs/tasks/001-ts-import-graph/`): Initial TypeScript import graph. Created the scanner patterns and extraction regexes. Commit: `3d61ecd`.
