# Plan: Task 006

## Summary

Fix ast-grep quote sensitivity by duplicating 4 patterns for single-quote variants, add JSONC-tolerant tsconfig parsing with a `stripJSONC` helper, and thread a warning writer through the resolver for parse failures. Cover with unit and e2e tests using committed fixtures.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| ~ | `auto-graph/internal/scanner/typescript.go` | Add single-quote variants for 4 patterns |
| ~ | `auto-graph/internal/resolver/typescript.go` | Add `stripJSONC()`, accept `io.Writer`, warn on parse failure |
| ~ | `auto-graph/internal/codegraph/build.go` | Thread `io.Writer` into `NewTypeScriptResolver` |
| ~ | `auto-graph/internal/cli/code_graph.go` | Pass `cmd.ErrOrStderr()` through `Build()` |
| ~ | `auto-graph/internal/scanner/typescript_test.go` | Add single-quote reexport and side-effect tests |
| ~ | `auto-graph/internal/resolver/typescript_test.go` | Add JSONC and warning tests |
| + | `auto-graph/testdata/fixtures/single-quote-reexports/` | Scanner fixture with single-quoted barrel file |
| + | `auto-graph/testdata/fixtures/jsonc-tsconfig/` | Resolver fixture with trailing commas + comments |
| + | `auto-graph/e2e/testdata/single-quote-jsonc-project/` | E2E fixture combining both bugs |
| + | `auto-graph/e2e/testdata/golden/single-quote-jsonc-project.json` | Golden file for e2e |
| ~ | `auto-graph/e2e/e2e_test.go` | Add e2e test + stderr warning assertion |

## Links

- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test

- [ ] `auto-graph/internal/scanner/typescript_test.go` — TestReexportSingleQuotes, TestSideEffectSingleQuotes
- [ ] `auto-graph/internal/resolver/typescript_test.go` — TestJSONCTrailingCommas, TestJSONCComments, TestMalformedTSConfigWarning
- [ ] `auto-graph/e2e/e2e_test.go` — TestSingleQuoteJSONCProject, TestSingleQuoteJSONCProjectStderrWarning
- [ ] Existing tests: `cd auto-graph && go test ./...` (no regressions)
- [ ] E2E: `cd auto-graph && go test -tags=e2e ./e2e/ -run TestSingleQuote`

## Execution Sequence

```
Phase 1 (Scanner fix + unit tests) --> Phase 3 (E2E fixture + tests)
Phase 2 (Resolver fix + unit tests) --/
```

Phases 1 and 2 are independent. Phase 3 depends on both.

## Plan

### Phase 1: Scanner — quote-agnostic patterns

- [ ] Step 1.1: Create fixture `auto-graph/testdata/fixtures/single-quote-reexports/`
  - `tsconfig.json`: minimal `{}`
  - `index.ts`: barrel with 3 single-quoted re-exports using **distinct target paths**: `export { Widget } from './Widget'`, `export type { WidgetProps } from './types'`, `export * from './widget-utils'`
  - `Widget.tsx`: exports `Widget` function
  - `types.ts`: exports `WidgetProps` type
  - `widget-utils.ts`: exports `widgetLabel` function
  - Verify: files exist, valid TypeScript syntax

- [ ] Step 1.2: Add single-quote pattern variants to `auto-graph/internal/scanner/typescript.go`
  - Duplicate lines 76-79 with single-quote versions:
    - `export { $$$ } from '$_'`
    - `export * from '$_'`
    - `export type { $$$ } from '$_'`
    - `import '$_'`
  - Verify: `cd auto-graph && go build ./...` passes

- [ ] Step 1.3: Add `TestReexportSingleQuotes` in `auto-graph/internal/scanner/typescript_test.go`
  - Uses `fixtureDir(t, "single-quote-reexports")`
  - Asserts 3 matches from `index.ts`: `./Widget` (reexport) + `./types` (reexport) + `./widget-utils` (reexport) — each a distinct path so dedup doesn't collapse them

<!-- RESOLVED(P1): TestReexportSingleQuotes cannot assert duplicate same-path reexports
REVIEW: I checked `auto-graph/internal/scanner/typescript.go`; the scanner's `seenKey` is `(file, importPath, kind)` at lines 86-109. With `export { Widget } from './Widget'` and `export type { WidgetProps } from './Widget'`, both matches have the same key (`index.ts`, `./Widget`, `reexport`), so the scanner will return one `./Widget` match, not two. To verify all three reexport patterns, give the named, type, and star reexports distinct target paths or change the dedupe design explicitly.
AUTHOR: Fixed. Changed the fixture so each re-export variant targets a distinct file: `./Widget` (named), `./types` (type), `./widget-utils` (star). All 3 are now independently assertable.
-->

  - Verify: `go test ./internal/scanner/ -run TestReexportSingleQuotes` passes

- [ ] Step 1.4: Add `TestSideEffectSingleQuotes` in `auto-graph/internal/scanner/typescript_test.go`
  - Create a temp file with `import './side-effect'` (single quotes)
  - Assert the side-effect import is detected with kind `side-effect`
  - Verify: `go test ./internal/scanner/ -run TestSideEffectSingleQuotes` passes

- [ ] Step 1.5: Run full scanner test suite
  - Verify: `cd auto-graph && go test ./internal/scanner/` — all existing tests still pass (AC-9)

- [ ] Step 1.6: Commit: `feat(006): phase 1 — quote-agnostic ast-grep patterns`

### Phase 2: Resolver — JSONC-tolerant tsconfig parsing

- [ ] Step 2.1: Create fixture `auto-graph/testdata/fixtures/jsonc-tsconfig/`
  - `tsconfig.json` with trailing commas and `//` line comments:
    ```jsonc
    {
      // Path alias config
      "compilerOptions": {
        "baseUrl": ".",
        "paths": {
          "@/*": ["./src/*"],  // alias
        },
      }
    }
    ```
  - `src/utils/format.ts`: exports `formatDate`
  - `src/routes/dashboard.tsx`: imports `@/utils/format` and `../utils/format` (to verify both alias and relative work)
  - Verify: files exist

- [ ] Step 2.2: Implement `stripJSONC` in `auto-graph/internal/resolver/typescript.go`
  - Strips `//` line comments (not inside strings)
  - Strips trailing commas before `}` or `]`
  - Returns `[]byte` suitable for `json.Unmarshal`
  - Verify: `cd auto-graph && go build ./...` passes

- [ ] Step 2.3: Add `io.Writer` field to `TypeScriptResolver` struct and update constructor
  - Add `warn io.Writer` field to struct
  - Change `NewTypeScriptResolver(projectRoot string)` to `NewTypeScriptResolver(projectRoot string, warn io.Writer)`
  - Verify: `cd auto-graph && go build ./...` — expect compile errors in callers

- [ ] Step 2.4: Update `loadTSConfig` to use `stripJSONC` and emit warnings
  - Apply `stripJSONC(data)` before `json.Unmarshal`
  - On parse failure after stripping: write warning to `r.warn` if non-nil
  - On success: proceed as before
  - Verify: `cd auto-graph && go build ./...` — still expect caller errors

- [ ] Step 2.5: Update all callers of `NewTypeScriptResolver` and `Build`
  - `auto-graph/internal/codegraph/build.go:36`: change `Build` signature to accept `io.Writer` for warnings, pass to resolver
  - `auto-graph/internal/cli/code_graph.go:69`: pass `cmd.ErrOrStderr()` to `Build`
  - `auto-graph/internal/cli/code_context.go:90`: pass `cmd.ErrOrStderr()` to `Build`
  - `auto-graph/internal/codegraph/build_test.go:118,166`: pass `io.Discard` to `Build`
  - `auto-graph/internal/resolver/typescript_test.go`: pass `io.Writer` (e.g. `io.Discard` or `&bytes.Buffer{}`) in all existing `NewTypeScriptResolver` calls

<!-- RESOLVED(P1): Build signature update misses existing callers
REVIEW: I checked all current `codegraph.Build` callers. Besides `internal/cli/code_graph.go`, `auto-graph/internal/cli/code_context.go:90` calls `codegraph.Build(projectRoot, lang)`, and `auto-graph/internal/codegraph/build_test.go:118` and `:166` call `Build(dir, "typescript")`. If `Build` gains an `io.Writer` parameter, these must be updated or `go build ./...` / `go test ./internal/codegraph/` will not compile.
AUTHOR: Fixed. Added `code_context.go:90` and `build_test.go:118,166` to the caller update list.
-->

  - Verify: `cd auto-graph && go build ./...` passes (all callers updated)

- [ ] Step 2.6: Add `TestStripJSONC` unit test in `auto-graph/internal/resolver/typescript_test.go`
  - Test cases: trailing commas, line comments, both combined, no-op for valid JSON
  - Verify: `go test ./internal/resolver/ -run TestStripJSONC` passes

- [ ] Step 2.7: Add `TestJSONCTrailingCommas` in `auto-graph/internal/resolver/typescript_test.go`
  - Uses `fixtureDir(t, "jsonc-tsconfig")`
  - Creates resolver, resolves `@/utils/format` from a source file
  - Asserts `ResolvedPath` is `src/utils/format.ts` and `MatchedAlias` is true
  - Verify: `go test ./internal/resolver/ -run TestJSONCTrailingCommas` passes

- [ ] Step 2.8: Add `TestJSONCComments` in `auto-graph/internal/resolver/typescript_test.go`
  - Uses `t.TempDir()` with a tsconfig containing only `//` comments (no trailing commas)
  - Asserts alias resolution works
  - Verify: `go test ./internal/resolver/ -run TestJSONCComments` passes

- [ ] Step 2.9: Add `TestMalformedTSConfigWarning` in `auto-graph/internal/resolver/typescript_test.go`
  - Uses `t.TempDir()` with genuinely malformed tsconfig (e.g. `{{{`)
  - Passes `&bytes.Buffer{}` as warn writer
  - Asserts warning was written to the buffer
  - Asserts resolver still works (loaded=false, no alias resolution)
  - Verify: `go test ./internal/resolver/ -run TestMalformedTSConfigWarning` passes

- [ ] Step 2.10: Run full resolver and build test suites
  - Verify: `cd auto-graph && go test ./internal/resolver/ ./internal/codegraph/` — all pass (AC-9)

- [ ] Step 2.11: Commit: `feat(006): phase 2 — JSONC-tolerant tsconfig parsing with warnings`

### Phase 3: E2E tests

- [ ] Step 3.1: Create e2e fixture `auto-graph/e2e/testdata/single-quote-jsonc-project/`
  - Copy structure from `.tmp/autograph-repro` but with:
    - `tsconfig.json`: JSONC with trailing commas + a `//` comment
    - `src/feature/index.ts`: single-quoted re-exports
    - `src/routes/dashboard.tsx`: single-quoted alias + relative imports
    - `src/components/Header.tsx`, `src/utils/format.ts`, `src/feature/Widget.tsx`, `src/feature/widget-utils.ts`
  - Verify: files exist, valid TypeScript

- [ ] Step 3.2: Add `TestSingleQuoteJSONCProject` in `auto-graph/e2e/e2e_test.go`
  - Build binary, run against the new fixture
  - Parse JSON output, assert 5 edges:
    - `src/routes/dashboard.tsx` → `src/utils/format.ts` (static, alias)
    - `src/routes/dashboard.tsx` → `src/components/Header.tsx` (static, relative)
    - `src/feature/index.ts` → `src/feature/Widget.tsx` (reexport)
    - `src/feature/index.ts` → `src/feature/Widget.tsx` (reexport, type — deduped with above into 1 edge)
    - `src/feature/index.ts` → `src/feature/widget-utils.ts` (reexport)
  - So expect 4 edges (the two Widget reexports share the same source→target and get merged)
  - Assert all edge sources/targets exist in nodes
  - Assert `import_kind` includes both `static` and `reexport`
  - Generate golden file with `-update` flag
  - Verify: `cd auto-graph && go test -tags=e2e ./e2e/ -run TestSingleQuoteJSONCProject` passes

- [ ] Step 3.3: Add stderr warning test
  - Modify `runAutograph` or add a variant that returns both stdout and stderr
  - Create a test with a genuinely malformed tsconfig that runs autograph and asserts stderr contains a tsconfig warning
  - Verify: test passes

- [ ] Step 3.4: Run full test suite
  - Verify: `cd auto-graph && go test ./...` — all unit tests pass
  - Verify: `cd auto-graph && go test -tags=e2e ./e2e/` — all e2e tests pass (including existing golden files)
  - Verify: `cd auto-graph && go vet ./...` — clean

- [ ] Step 3.5: Manual smoke test
  - Run `autograph code graph .tmp/autograph-repro --format json` — expect 4 edges (2 from dashboard + 2 from barrel, since named and type reexports to `./Widget` merge into one graph edge)

<!-- RESOLVED(P2): Manual smoke expected count still uses pre-dedupe total
REVIEW: This repeats the 5-edge expectation even though Step 3.2 says the two Widget reexports are deduped into one edge. With the current scanner and graph dedupe, this smoke test should expect 4 edges unless the fixture uses distinct targets for all three reexport variants.
AUTHOR: Fixed. Changed to 4 edges with explanation of the merge.
-->

  - Add trailing commas to `.tmp/autograph-repro/tsconfig.json`, re-run — expect same 4 edges (alias still works)
  - Restore `.tmp/autograph-repro/tsconfig.json`

- [ ] Step 3.6: Commit: `feat(006): phase 3 — e2e tests for quote styles and JSONC tsconfig`

## Success Criteria

- [ ] `cd auto-graph && go build ./...` passes
- [ ] `cd auto-graph && go vet ./...` clean
- [ ] `cd auto-graph && go test ./...` — all unit tests pass, including new ones
- [ ] `cd auto-graph && go test -tags=e2e ./e2e/` — all e2e tests pass, including new golden file
- [ ] AC-1: Single-quoted re-exports produce edges (TestReexportSingleQuotes + e2e)
- [ ] AC-2: Double-quoted re-exports still work (TestReexportVariants — existing, unchanged)
- [ ] AC-3: All 3 re-export variants work with single quotes (TestReexportSingleQuotes)
- [ ] AC-4: JSONC tsconfig with trailing commas resolves aliases (TestJSONCTrailingCommas + e2e)
- [ ] AC-5: JSONC tsconfig with comments resolves aliases (TestJSONCComments)
- [ ] AC-6: Malformed tsconfig emits warning (TestMalformedTSConfigWarning + e2e stderr test)
- [ ] AC-7: All new test fixtures are committed under `auto-graph/` (not `.tmp/`)
- [ ] AC-8: Side-effect pattern also made quote-agnostic (TestSideEffectSingleQuotes)
- [ ] AC-9: All existing tests pass (no regressions)

## Open Questions

- (none)
