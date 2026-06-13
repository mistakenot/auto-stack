---
hash: "98aa071b"
id: "1c92d183"
read_when: "implementing or reviewing the autograph quote-style and JSONC tsconfig fix requirements"
summary: "Requirements for fixing two silent edge-dropping bugs in autograph: single-quote insensitivity in ast-grep re-export patterns, and strict JSON parsing of JSONC tsconfig.json files."
title: "Requirements: Task 006 — Autograph Quote-Style and JSONC Fixes"
---

# Task 006: Autograph Quote-Style and JSONC Fixes

## Problem

`autograph code graph` has two bugs that silently drop edges in real-world TypeScript projects:

1. **Re-export patterns are quote-sensitive.** The three ast-grep patterns for `export ... from` hardcode double-quoted string literals (`"$_"`). Real TS codebases overwhelmingly use single quotes, so single-quoted re-exports produce zero matches and barrel files appear as disconnected orphans. The test fixtures masked this because they only use double quotes.

2. **tsconfig.json is parsed as strict JSON.** `json.Unmarshal` rejects JSONC features (trailing commas, comments) that TypeScript permits and most projects use. When parsing fails the resolver silently skips all alias resolution — the v0.19.0 alias fix becomes a no-op on real codebases. No warning is emitted.

Both bugs were verified with a minimal reproduction against `autograph v0.19.0`.

## Goals

- Make all ast-grep patterns that include string-literal metavars work regardless of quote style (single, double)
- Parse `tsconfig.json` with a JSONC-tolerant approach (strip comments and trailing commas before unmarshalling)
- Emit a stderr warning when tsconfig parsing fails so silent fallback becomes diagnosable
- Add e2e test coverage that exercises both quote styles and JSONC tsconfig — committed as fixtures, not dependent on `.tmp/`
- Audit all existing ast-grep patterns in the scanner for the same quote-sensitivity issue, not just the re-export ones

## Acceptance Criteria

**AC-1**: Single-quoted re-exports produce edges
- Given: a barrel file `index.ts` containing `export { Widget } from './Widget'` (single quotes)
- When: `autograph code graph .` scans the project
- Then: the graph includes an edge from the barrel file to the resolved target with `attrs.import_kind` equal to `reexport`

**AC-2**: Double-quoted re-exports still produce edges
- Given: a barrel file `index.ts` containing `export { Widget } from "./Widget"` (double quotes)
- When: `autograph code graph .` scans the project
- Then: the graph includes the same edge (no regression from the fix)

**AC-3**: All re-export variants work with both quote styles
- Given: `export { X } from '...'`, `export type { Y } from '...'`, and `export * from '...'` using single quotes
- When: `autograph code graph .` scans the project
- Then: all three produce edges, matching the same behavior already expected for double-quoted variants

**AC-4**: JSONC tsconfig with trailing commas resolves aliases
- Given: a `tsconfig.json` containing trailing commas in `paths` (e.g. `"@/*": ["./src/*"],`)
- When: `autograph code graph .` scans the project
- Then: `@/` alias imports resolve to file paths and produce edges, identical to strict-JSON tsconfig behavior

**AC-5**: JSONC tsconfig with comments resolves aliases
- Given: a `tsconfig.json` containing `//` line comments
- When: `autograph code graph .` scans the project
- Then: alias resolution works normally

**AC-6**: Tsconfig parse failure emits a warning
- Given: a `tsconfig.json` that is genuinely malformed (not just JSONC)
- When: `autograph code graph .` runs
- Then: a warning is emitted to stderr indicating the tsconfig could not be parsed, and the graph is still produced (without alias resolution)

**AC-7**: E2E test coverage for quote styles and JSONC
- Given: committed fixture directories under `auto-graph/` (not `.tmp/`)
- When: `go test ./...` and `go test -tags=e2e ./e2e/` run in `auto-graph`
- Then: unit tests exercise single-quoted re-exports, double-quoted re-exports, JSONC tsconfig with trailing commas, and JSONC tsconfig with comments; e2e tests verify end-to-end graph output against golden files — all passing

<!-- RESOLVED(P2): AC-7 names the wrong command for e2e coverage
REVIEW: I checked `auto-graph/e2e/e2e_test.go` and it is guarded by `//go:build e2e`, so `cd auto-graph && go test ./...` will not run the e2e tests or their golden fixtures. If AC-7 requires e2e coverage, the acceptance command needs to include `go test -tags=e2e ./e2e/`; otherwise this AC can pass while the e2e coverage never runs.
AUTHOR: Fixed. AC-7 now requires both `go test ./...` (unit) and `go test -tags=e2e ./e2e/` (e2e).
-->

**AC-8**: All ast-grep patterns audited for quote sensitivity
- Given: the full set of ast-grep patterns in the TypeScript scanner (import, dynamic, require, reexport, side-effect)
- When: patterns are reviewed
- Then: any pattern using a quoted string metavar (`"$_"`) is made quote-agnostic or duplicated for both styles

**AC-9**: Existing tests still pass
- Given: all existing fixtures and tests in `auto-graph`
- When: `go test ./...` runs
- Then: no regressions

## Out of Scope

- Monorepo or nested tsconfig resolution
- `tsconfig.json` `extends` field support
- Backtick/template-literal import paths
- Symbol-level dependency tracking
- Changes to graph output schema

## Open Questions

- (none)
