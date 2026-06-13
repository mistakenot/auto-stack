---
hash: "3f4112b4"
id: "21b06aa7"
read_when: "implementing TypeScript path alias or re-export resolution in autograph"
summary: "Acceptance criteria (AC-1 through AC-5) for adding TypeScript path alias and barrel-file re-export resolution to autograph code graph."
title: "Task 005: Code Graph Alias and Re-export Resolution Requirements"
---

# Task 005: Code Graph Alias and Re-export Resolution

## Problem

`autograph code graph` silently misses common TypeScript dependency edges when projects use `tsconfig.json` path aliases or barrel-file re-exports. This makes large alias-heavy projects look only partially connected and can leave route and module entrypoint files incorrectly orphaned.

## Goals

- Resolve `tsconfig.json` `compilerOptions.baseUrl` and `compilerOptions.paths` aliases such as `@/*` to real project files before creating graph edges
- Capture `export ... from` re-export dependencies, including named, star, and type-only re-exports
- Preserve existing support for relative static imports, dynamic imports, `require`, type imports, extension probing, and JSON/DOT/Mermaid output
- Add focused fixtures and tests that reproduce the reported missing-edge cases
- Avoid silently dropping project-local alias imports when they match configured aliases but cannot resolve to a file

## Acceptance Criteria

**AC-1**: Path alias imports create edges
- Given: a TypeScript project with `tsconfig.json` containing `"baseUrl": "."` and `"paths": { "@/*": ["./src/*"] }`
- When: `src/routes/dashboard.tsx` imports `@/utils/format`
- Then: `autograph code graph .` includes an edge from `src/routes/dashboard.tsx` to `src/utils/format.ts` with `attrs.raw` set to `@/utils/format`

**AC-2**: Dynamic path alias imports create edges
- Given: the same alias configuration
- When: a source file calls `await import("@/services/heavy-service")`
- Then: the graph includes an edge to the resolved service file and marks it with `attrs.import_kind` equal to `dynamic`

**AC-3**: Re-export dependencies create edges
- Given: a barrel file containing `export { Widget } from "./Widget"`, `export type { WidgetProps } from "./Widget"`, and `export * from "./widget.utils"`
- When: `autograph code graph .` scans the project
- Then: the graph includes edges from the barrel file to each resolved re-export target, with `attrs.import_kind` equal to `reexport`

**AC-4**: Existing TypeScript graph behavior is preserved
- Given: existing checked-in fixtures for relative imports, type imports, side-effect imports, dynamic imports, `require`, extension probing, and output formats
- When: `go test ./...` runs in `auto-graph`
- Then: those fixtures still pass and JSON output remains parseable on stdout

**AC-5**: Alias failures are not silent
- Given: an import specifier matches a configured `tsconfig.json` path alias but no target file can be resolved
- When: `autograph code graph .` runs
- Then: the command reports a clear diagnostic to stderr while keeping any JSON graph payload on stdout parseable

## Out of Scope

- Monorepo or nested `tsconfig.json` resolution beyond the project-root config
- Full TypeScript compiler module resolution parity for package `exports`, `imports`, or npm dependency graphing
- Symbol-level dependency tracking
- Creating edges to external packages in `node_modules`
- Changing graph output schemas beyond any diagnostic mechanism needed for unresolved alias reporting

## Open Questions

- (none, all resolved)
