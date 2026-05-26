# Solution: Task 006

## Approach

1. **Fix ast-grep quote sensitivity** — duplicate each quote-dependent pattern so both single- and double-quoted variants are covered. The affected patterns are the three re-export patterns (`export { $$$ } from "$_"`, `export * from "$_"`, `export type { $$$ } from "$_"`) and the side-effect pattern (`import "$_"`). The `import $$$`, `import($$$)`, and `require($$$)` patterns use `$$$` which is quote-agnostic and need no change.

2. **Add JSONC-tolerant tsconfig parsing** — write a `stripJSONC` helper function in `resolver/typescript.go` that strips `//` line comments and trailing commas before calling `json.Unmarshal`. This avoids adding a third-party dependency. Apply it to the raw bytes in `loadTSConfig`.

3. **Add stderr warning on tsconfig parse failure** — change `NewTypeScriptResolver` to accept an `io.Writer` for warnings. When `loadTSConfig` reads a tsconfig but fails to parse it (even after JSONC stripping), write a diagnostic to the writer. Thread the writer through from `Build()`, which already has access to the call chain.

4. **Create e2e fixture** — copy the `.tmp/autograph-repro` layout into `auto-graph/e2e/testdata/single-quote-jsonc-project/` with single-quoted re-exports and a JSONC tsconfig (trailing commas + comments). Add a golden file for the expected 4-edge output (the named and type re-exports to `./Widget` share the same `(source, target)` pair and merge into one graph edge).

<!-- RESOLVED(P2): Expected e2e edge count conflicts with graph dedupe
REVIEW: I checked `auto-graph/internal/scanner/typescript.go`: the scanner dedupes by `(file, importPath, kind)`, and `auto-graph/internal/codegraph/build.go` merges graph edges by `(source, target)`. The `.tmp/autograph-repro` barrel has both normal and type reexports to `./Widget`, so the proposed e2e fixture should produce 4 graph edges, not a 5-edge golden file. The plan later says "So expect 4 edges", so the solution and golden-file description need to agree.
AUTHOR: Fixed. Changed "5-edge" to "4-edge" and added explanation of the merge.
-->

5. **Add unit test fixtures** — add a `single-quote-reexports` scanner fixture under `auto-graph/testdata/fixtures/` with single-quoted re-export source files. Add a `jsonc-tsconfig` resolver fixture with trailing commas and comments. Add a `jsonc-malformed` resolver fixture to test the warning path.

6. **Audit and update existing fixtures** — the existing `alias-reexports` fixture uses only double quotes and passes today. Leave it as-is (it becomes the double-quote regression guard). The new fixtures cover single quotes.

## Files

```
~ auto-graph/internal/scanner/typescript.go     # duplicate 4 quote-dependent patterns for single-quote variant
~ auto-graph/internal/resolver/typescript.go     # add stripJSONC(), accept io.Writer for warnings, use in loadTSConfig
~ auto-graph/internal/codegraph/build.go         # thread io.Writer (stderr) into NewTypeScriptResolver
~ auto-graph/internal/scanner/typescript_test.go # add TestReexportSingleQuotes, TestSideEffectSingleQuotes
~ auto-graph/internal/resolver/typescript_test.go # add TestJSONCTrailingCommas, TestJSONCComments, TestMalformedTSConfigWarning
+ auto-graph/testdata/fixtures/single-quote-reexports/             # scanner fixture: barrel file with single-quoted re-exports
+ auto-graph/testdata/fixtures/single-quote-reexports/tsconfig.json
+ auto-graph/testdata/fixtures/single-quote-reexports/index.ts
+ auto-graph/testdata/fixtures/single-quote-reexports/Widget.tsx
+ auto-graph/testdata/fixtures/single-quote-reexports/widget-utils.ts
+ auto-graph/testdata/fixtures/jsonc-tsconfig/                     # resolver fixture: trailing commas + comments
+ auto-graph/testdata/fixtures/jsonc-tsconfig/tsconfig.json
+ auto-graph/testdata/fixtures/jsonc-tsconfig/src/utils/format.ts
+ auto-graph/testdata/fixtures/jsonc-tsconfig/src/routes/dashboard.tsx
+ auto-graph/e2e/testdata/single-quote-jsonc-project/              # e2e fixture: combined single-quote + JSONC
+ auto-graph/e2e/testdata/single-quote-jsonc-project/tsconfig.json
+ auto-graph/e2e/testdata/single-quote-jsonc-project/src/components/Header.tsx
+ auto-graph/e2e/testdata/single-quote-jsonc-project/src/routes/dashboard.tsx
+ auto-graph/e2e/testdata/single-quote-jsonc-project/src/utils/format.ts
+ auto-graph/e2e/testdata/single-quote-jsonc-project/src/feature/index.ts
+ auto-graph/e2e/testdata/single-quote-jsonc-project/src/feature/Widget.tsx
+ auto-graph/e2e/testdata/single-quote-jsonc-project/src/feature/widget-utils.ts
+ auto-graph/e2e/testdata/golden/single-quote-jsonc-project.json   # golden file: 4 edges expected
~ auto-graph/e2e/e2e_test.go                    # add TestSingleQuoteJSONCProject e2e test
```

### Key code outlines

**scanner/typescript.go — pattern list becomes:**
```go
patterns := []patternSpec{
    {pattern: "import $$$", kind: "static"},
    {pattern: "import($$$)", kind: "dynamic"},
    {pattern: "require($$$)", kind: "require"},
    {pattern: `export { $$$ } from "$_"`, kind: "reexport"},
    {pattern: `export { $$$ } from '$_'`, kind: "reexport"},
    {pattern: `export * from "$_"`, kind: "reexport"},
    {pattern: `export * from '$_'`, kind: "reexport"},
    {pattern: `export type { $$$ } from "$_"`, kind: "reexport"},
    {pattern: `export type { $$$ } from '$_'`, kind: "reexport"},
    {pattern: `import "$_"`, kind: "side-effect"},
    {pattern: `import '$_'`, kind: "side-effect"},
}
```

**resolver/typescript.go — stripJSONC:**
```go
func stripJSONC(data []byte) []byte {
    // 1. Remove // line comments (not inside strings)
    // 2. Remove trailing commas before } or ]
    // Returns valid JSON suitable for json.Unmarshal
}
```

**resolver/typescript.go — constructor change:**
```go
func NewTypeScriptResolver(projectRoot string, warn io.Writer) *TypeScriptResolver {
    r := &TypeScriptResolver{warn: warn}
    r.loadTSConfig(projectRoot)
    return r
}
```

## Test Coverage

| AC   | Test Type   | File                                          |
|------|-------------|-----------------------------------------------|
| AC-1 | e2e         | e2e/e2e_test.go (TestSingleQuoteJSONCProject) |
| AC-1 | unit        | scanner/typescript_test.go (TestReexportSingleQuotes) |
| AC-2 | unit        | scanner/typescript_test.go (TestReexportVariants — existing, unchanged) |
| AC-3 | e2e         | e2e/e2e_test.go (TestSingleQuoteJSONCProject) |
| AC-3 | unit        | scanner/typescript_test.go (TestReexportSingleQuotes) |
| AC-4 | e2e         | e2e/e2e_test.go (TestSingleQuoteJSONCProject) |
| AC-4 | unit        | resolver/typescript_test.go (TestJSONCTrailingCommas) |
| AC-5 | unit        | resolver/typescript_test.go (TestJSONCComments) |
| AC-6 | unit        | resolver/typescript_test.go (TestMalformedTSConfigWarning) |
| AC-7 | e2e + unit  | all new test files/fixtures listed above       |
| AC-8 | unit        | scanner/typescript_test.go (TestSideEffectSingleQuotes) |
| AC-9 | existing    | all existing tests (go test ./...)             |

## Out of Scope

- Monorepo or nested tsconfig resolution
- `tsconfig.json` `extends` field support
- Backtick/template-literal import paths
- Symbol-level dependency tracking
- Changes to graph output schema
- Third-party JSONC parser dependency (hand-rolled stripping is sufficient for tsconfig)

## Rejected Alternatives

- **Third-party JSONC library** (e.g. `github.com/tidwall/jsonc`): adds a dependency for a ~20 line helper. tsconfig JSONC usage is limited to line comments and trailing commas — a simple regex/scan approach covers it without bloating the module.
- **Single combined ast-grep pattern with regex alternation**: ast-grep patterns don't support `["']` alternation syntax. The only option is separate patterns per quote style.
- **Drop `import "$_"` pattern entirely** (since `import $$$` already catches side-effects): would work today but is fragile — if the `import $$$` extraction logic changes, side-effects silently break. Keeping both patterns with both quote styles is cheap (ast-grep runs are fast) and defensive.
- **Use `ast-grep --config` YAML rules instead of `-p` patterns**: would require shipping embedded YAML config. Current `-p` pattern approach is simpler and already works for all other patterns.
