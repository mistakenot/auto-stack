---
hash: "cd191ea6"
id: "06d170e6"
read_when: "implementing TypeScript alias hardening in auto-graph or reviewing the phase sequence for resolver and scanner coverage"
summary: "Phased implementation plan for hardening alias resolution and re-export detection in auto-graph, covering fixture creation, resolver wildcard/exact/baseUrl semantics, CLI diagnostics for unresolved aliases, and full regression testing."
title: "Plan: TypeScript Alias Resolution and Re-Export Hardening (Task 005)"
---

# Plan: Task 005

## Summary

Harden the existing TypeScript scanner/resolver/CLI graph path with a focused alias-and-reexport fixture, resolver alias metadata, stderr diagnostics for unresolved aliases, and graph-level regression tests.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| + | `auto-graph/testdata/fixtures/alias-reexports/tsconfig.json` | Focused fixture with `baseUrl` and `@/* -> ./src/*` alias mapping |
| + | `auto-graph/testdata/fixtures/alias-reexports/src/routes/dashboard.tsx` | Fixture source with static alias import, dynamic alias import, relative import, and unresolved alias import |
| + | `auto-graph/testdata/fixtures/alias-reexports/src/utils/format.ts` | Static alias import target |
| + | `auto-graph/testdata/fixtures/alias-reexports/src/components/Header.tsx` | Relative import target |
| + | `auto-graph/testdata/fixtures/alias-reexports/src/services/heavy-service.ts` | Dynamic alias import target |
| + | `auto-graph/testdata/fixtures/alias-reexports/src/client/my-feature/Widget.tsx` | Re-export target |
| + | `auto-graph/testdata/fixtures/alias-reexports/src/client/my-feature/widget.utils.ts` | Star re-export target |
| + | `auto-graph/testdata/fixtures/alias-reexports/src/client/my-feature/index.ts` | Barrel-file fixture with named, type-only, and star re-exports |
| ~ | `auto-graph/internal/resolver/resolver.go` | Add alias-match metadata to `ResolveResult` |
| ~ | `auto-graph/internal/resolver/typescript.go` | Preserve wildcard prefix/suffix mapping, add exact-match semantics, baseUrl probing, and unresolved alias signaling |
| ~ | `auto-graph/internal/resolver/typescript_test.go` | Add resolver tests for wildcard, exact, `./` target, baseUrl fallback, and unresolved alias metadata |
| ~ | `auto-graph/internal/scanner/typescript.go` | Ensure named/type/star re-export variants and quote styles are captured |
| ~ | `auto-graph/internal/scanner/typescript_test.go` | Add scanner assertions for all re-export variants in the focused fixture |
| ~ | `auto-graph/internal/cli/code_graph.go` | Return unresolved alias diagnostics from graph construction and write them to stderr |
| ~ | `auto-graph/internal/cli/code_graph_test.go` | Add graph-level regression coverage for alias edges, dynamic alias edges, re-export edges, diagnostics, and parseable stdout |

## Links

- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test

- [x] `cd auto-graph && go test ./internal/resolver` -- resolver alias semantics and metadata
- [x] `cd auto-graph && go test ./internal/scanner` -- scanner import/re-export matching
- [x] `cd auto-graph && go test ./internal/cli` -- graph construction, stdout/stderr split, and CLI regressions
- [x] `cd auto-graph && go build ./...` -- compile all auto-graph packages after Go edits
- [x] `cd auto-graph && go test ./...` -- full non-e2e regression suite
- [x] `cd auto-graph && go vet ./...` -- vet regression check
- [x] `cd auto-graph && go test -tags=e2e ./e2e` -- optional e2e check if ast-grep and fixture runtime dependencies are available

## Execution Sequence

```
Phase 1 (Fixture + Scanner)
        \
         --> Phase 3 (CLI Diagnostics + Graph Regression) --> Phase 4 (Full Verification)
        /
Phase 2 (Resolver Alias Semantics)
```

## Plan

### Phase 1: Fixture and Scanner Coverage

- [x] Step 1.1: Add `auto-graph/testdata/fixtures/alias-reexports/tsconfig.json` with `compilerOptions.baseUrl` set to `"."` and `paths` mapping `"@/*"` to `"./src/*"`.
  - Verify: `sed -n '1,80p' auto-graph/testdata/fixtures/alias-reexports/tsconfig.json` shows valid JSON with the expected alias mapping.
- [x] Step 1.2: Add fixture source files for `dashboard.tsx`, `format.ts`, `Header.tsx`, `heavy-service.ts`, `Widget.tsx`, `widget.utils.ts`, and `index.ts`.
  - Verify: `find auto-graph/testdata/fixtures/alias-reexports -type f | sort` lists the eight fixture files from the Changes table.
- [x] Step 1.3: In `dashboard.tsx`, include a static alias import `@/utils/format`, a dynamic alias import `@/services/heavy-service`, a relative import to `../components/Header`, and one intentionally unresolved configured alias import for diagnostics.
  - Verify: `rg -n "@/utils/format|@/services/heavy-service|@/does-not-exist|../components/Header" auto-graph/testdata/fixtures/alias-reexports/src/routes/dashboard.tsx` finds all four import patterns.
- [x] Step 1.4: In `src/client/my-feature/index.ts`, include `export { Widget } from "./Widget"`, `export type { WidgetProps } from "./Widget"`, and `export * from "./widget.utils"`.
  - Verify: `rg -n "export \\{|export type|export \\*" auto-graph/testdata/fixtures/alias-reexports/src/client/my-feature/index.ts` finds all three re-export variants.
- [x] Step 1.5: Update `auto-graph/internal/scanner/typescript.go` only if fixture tests show current ast-grep patterns miss single-quoted or type/star re-export variants; add quote-specific patterns only as needed and rely on existing dedupe.
  - Verify: `cd auto-graph && go test ./internal/scanner -run 'TestReexport|TestAllImportStyles'` passes or skips only because ast-grep is unavailable.
- [x] Step 1.6: Extend `auto-graph/internal/scanner/typescript_test.go` to assert named, type-only, and star re-export matches from `alias-reexports`, all with `Kind == "reexport"`.
  - Verify: `cd auto-graph && go test ./internal/scanner -run TestReexport` passes or skips only because ast-grep is unavailable.
- [x] Step 1.7: Run `cd auto-graph && go build ./...`.
  - Verify: the build exits zero after scanner/test edits.
- [x] Step 1.8: Commit: `feat(005): phase 1 - alias reexport fixture and scanner coverage`.
  - Verify: `git status --short auto-graph/testdata/fixtures/alias-reexports auto-graph/internal/scanner` is clean after the commit.

### Phase 2: Resolver Alias Semantics

- [x] Step 2.1: Extend `auto-graph/internal/resolver/resolver.go` so `ResolveResult` includes alias-match metadata, for example `MatchedAlias bool`.
  - Verify: `rg -n "MatchedAlias" auto-graph/internal/resolver` finds the new field and resolver assertions.
- [x] Step 2.2: Update `pathMapping` in `auto-graph/internal/resolver/typescript.go` to preserve pattern prefix, pattern suffix, whether the pattern has a wildcard, and full target templates instead of only stripped target prefixes.
  - Verify: `rg -n "suffix|hasWildcard|target" auto-graph/internal/resolver/typescript.go` shows the new mapping structure and substitution code.
- [x] Step 2.3: Implement exact mapping semantics: mappings without `*` match only the exact specifier, not arbitrary same-prefix specifiers.
  - Verify: a resolver test proves an exact mapping such as `"@config"` does not match `"@config/extra"`.
- [x] Step 2.4: Implement wildcard capture/substitution with prefix and suffix validation, including targets like `"./src/*"`.
  - Verify: resolver tests prove `"@/utils/format"` resolves to `src/utils/format.ts` with `MatchedAlias == true`.
- [x] Step 2.5: Add `baseUrl` fallback probing for non-relative specifiers that do not match a `paths` alias, while preserving external classification for unresolved package names.
  - Verify: resolver tests prove a local baseUrl import resolves and an unresolved package import still has `IsExternal == true`.
- [x] Step 2.6: Return `MatchedAlias == true` with empty `ResolvedPath` when a configured alias pattern matches but no candidate file resolves.
  - Verify: resolver tests prove an unresolved `@/missing/module` is not external and has `MatchedAlias == true`.
- [x] Step 2.7: Keep relative import and extension/index probing behavior unchanged.
  - Verify: `cd auto-graph && go test ./internal/resolver -run 'TestRelativeResolve|TestIndexResolve|TestExtensionProbing|TestTsxExtensionProbing'` passes.
- [x] Step 2.8: Run `cd auto-graph && go build ./...`.
  - Verify: the build exits zero after resolver edits.
- [x] Step 2.9: Commit: `feat(005): phase 2 - harden TypeScript alias resolution`.
  - Verify: `git status --short auto-graph/internal/resolver` is clean after the commit.

### Phase 3: CLI Diagnostics and Graph Regression

- [x] Step 3.1: Add a small diagnostic type in `auto-graph/internal/cli/code_graph.go` carrying source path, line, and raw import specifier for unresolved configured aliases.
  - Verify: `rg -n "diagnostic|MatchedAlias" auto-graph/internal/cli/code_graph.go` finds the diagnostic type and collection point.
- [x] Step 3.2: Change `buildGraph` to return `(*graph.Graph, []graphDiagnostic)` or equivalent, collecting diagnostics before unresolved alias imports are skipped.
  - Verify: existing callers are updated and `cd auto-graph && go test ./internal/cli -run TestLanguage` compiles and passes.
- [x] Step 3.3: In `runCodeGraph`, write diagnostics to `cmd.ErrOrStderr()` after graph construction and before or after payload writing, without writing diagnostics to stdout.
  - Verify: a CLI test captures separate stdout/stderr buffers and proves stdout still starts with `{` and parses as JSON.
- [x] Step 3.4: Add `auto-graph/internal/cli/code_graph_test.go` regression coverage for the focused fixture: static alias edge from `src/routes/dashboard.tsx` to `src/utils/format.ts` with raw `@/utils/format`.
  - Verify: `cd auto-graph && go test ./internal/cli -run TestCodeGraphAliasReexports` passes.
- [x] Step 3.5: Add regression coverage for dynamic alias edge to `src/services/heavy-service.ts` with `attrs.import_kind == "dynamic"`.
  - Verify: the same CLI test fails if the dynamic edge is missing or has the wrong kind.
- [x] Step 3.6: Add regression coverage for re-export edges from `src/client/my-feature/index.ts` to `Widget.tsx` and `widget.utils.ts`, with `attrs.import_kind == "reexport"`.
  - Verify: the same CLI test fails if either re-export target is missing or has the wrong kind.
- [x] Step 3.7: Add regression coverage for unresolved alias diagnostics: stderr contains the source file, line number, raw unresolved alias, and remediation hint; stdout JSON remains parseable.
  - Verify: `cd auto-graph && go test ./internal/cli -run TestCodeGraphAliasReexports` passes and asserts both streams.
- [x] Step 3.8: Ensure e2e referential-integrity coverage automatically includes the new fixture because it contains `tsconfig.json`.
  - Verify: `cd auto-graph && go test -tags=e2e ./e2e -run TestEdgeReferentialIntegrity` passes when ast-grep is available.
- [x] Step 3.9: Run `cd auto-graph && go build ./...`.
  - Verify: the build exits zero after CLI edits.
- [x] Step 3.10: Commit: `feat(005): phase 3 - report unresolved alias graph diagnostics`.
  - Verify: `git status --short auto-graph/internal/cli` is clean after the commit.

### Phase 4: Full Verification and Polish

- [x] Step 4.1: Run focused package tests for resolver, scanner, and CLI.
  - Verify: `cd auto-graph && go test ./internal/resolver ./internal/scanner ./internal/cli` exits zero, with scanner tests skipping only if ast-grep is unavailable.
- [x] Step 4.2: Run the full auto-graph build.
  - Verify: `cd auto-graph && go build ./...` exits zero.
- [x] Step 4.3: Run the full auto-graph test suite.
  - Verify: `cd auto-graph && go test ./...` exits zero.
- [x] Step 4.4: Run vet.
  - Verify: `cd auto-graph && go vet ./...` exits zero.
- [x] Step 4.5: Run optional e2e tests if ast-grep is available in the environment.
  - Verify: `cd auto-graph && go test -tags=e2e ./e2e` exits zero, or record that it was skipped because ast-grep was unavailable.
- [x] Step 4.6: Manually smoke the new fixture with JSON output and separate stderr.
  - Verify: `cd auto-graph && go run ./cmd/autograph code graph ./testdata/fixtures/alias-reexports --format=json` prints parseable JSON on stdout and unresolved alias diagnostics on stderr.
- [x] Step 4.7: Confirm no graph schema changes were introduced.
  - Verify: `git diff -- auto-graph/internal/graph/model.go auto-graph/internal/format/json.go` is empty.
- [x] Step 4.8: Commit: `feat(005): phase 4 - verify alias reexport graph fixes`.
  - Verify: `git status --short auto-graph docs/tasks/005-code-graph-alias-reexports` shows only expected planning-doc changes, if any.

## Success Criteria

- [x] AC-1: `go test ./internal/cli -run TestCodeGraphAliasReexports` proves `@/utils/format` creates an edge from `src/routes/dashboard.tsx` to `src/utils/format.ts` with `attrs.raw == "@/utils/format"`.
- [x] AC-2: `go test ./internal/cli -run TestCodeGraphAliasReexports` proves `@/services/heavy-service` creates a dynamic edge to `src/services/heavy-service.ts`.
- [x] AC-3: `go test ./internal/scanner -run TestReexport` and `go test ./internal/cli -run TestCodeGraphAliasReexports` prove named, type-only, and star re-exports are detected and produce graph edges with `import_kind == "reexport"`.
- [x] AC-4: `cd auto-graph && go build ./... && go test ./... && go vet ./...` all exit zero, and e2e tests pass when available.
- [x] AC-5: CLI regression tests prove unresolved configured aliases are reported on stderr and JSON stdout remains parseable.

## Open Questions

- (none, all resolved)
