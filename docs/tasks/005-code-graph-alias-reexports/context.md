---
hash: "ac5afa86"
id: "ebd52db4"
read_when: "implementing TypeScript path alias resolution or fixing re-export scanning in auto-graph"
summary: "Verified codebase context for implementing TypeScript path alias resolution and re-export scanning fixes in auto-graph, with key file locations and resolver/scanner code references."
title: "Context: Task 005 — Code Graph Alias Re-exports"
---

# Context: Task 005

This file captures verified codebase context for implementing [solution.md](./solution.md).

## Key Files

- `auto-graph/internal/resolver/resolver.go:3-12` -- `ResolveResult` currently exposes only `ResolvedPath` and `IsExternal`; `Resolver.Resolve` is the shared interface used by graph construction.
- `auto-graph/internal/resolver/typescript.go:10-16` -- `tsconfig` parsing reads only `compilerOptions.baseUrl` and `compilerOptions.paths`, which are the fields needed for this task.
- `auto-graph/internal/resolver/typescript.go:18-22` -- `pathMapping` currently stores only a prefix and target prefixes, despite the comment saying prefix/suffix.
- `auto-graph/internal/resolver/typescript.go:83-111` -- `loadTSConfig` strips everything after `*` from both patterns and targets, then sorts mappings longest-prefix-first.
- `auto-graph/internal/resolver/typescript.go:124-149` -- `Resolve` classifies imports as bare/alias/relative; unresolved aliases return an empty `ResolveResult` that is indistinguishable from other unresolved imports.
- `auto-graph/internal/resolver/typescript.go:152-166` -- `classifyImport` treats any configured mapping prefix match as an alias, including exact mappings that should only match exactly.
- `auto-graph/internal/resolver/typescript.go:168-202` -- `resolveAlias` substitutes `target + rest`, resolves under `baseUrl` or project root, and probes for files.
- `auto-graph/internal/resolver/typescript.go:219-228` -- `probeFile` supports exact paths, `.ts`, `.tsx`, `.js`, `.jsx`, and `index.*` variants.
- `auto-graph/internal/scanner/scanner.go:3-9` -- `ImportMatch` already carries `SourceFile`, raw `ImportPath`, `Kind`, and 1-based `Line`, enough to format unresolved alias diagnostics.
- `auto-graph/internal/scanner/typescript.go:72-80` -- ast-grep patterns cover static imports, dynamic imports, `require`, named re-exports, star re-exports, type re-exports, and side-effect imports.
- `auto-graph/internal/scanner/typescript.go:82-119` -- the scanner runs each pattern for both `ts` and `tsx`, dedupes by `(sourceFile, importPath)`, and records 1-based line numbers.
- `auto-graph/internal/scanner/typescript.go:190-227` -- `extractImportPath` uses `from` extraction for re-exports and quoted-string extraction for dynamic imports / `require`.
- `auto-graph/internal/scanner/typescript.go:230-247` -- `classifyKind` only refines static imports; re-export matches keep kind `reexport`.
- `auto-graph/internal/cli/code_graph.go:51-129` -- `runCodeGraph` validates the project root, checks ast-grep, detects language, discovers files, scans imports, creates the resolver, builds the graph, and writes the requested output format.
- `auto-graph/internal/cli/code_graph.go:88-105` -- discovery and scanning happen before resolver creation; graph construction is the place that combines matches and resolution.
- `auto-graph/internal/cli/code_graph.go:146-195` -- `discoverFiles` walks `.ts` and `.tsx` files, skips hidden directories and `node_modules`, and returns sorted project-relative paths.
- `auto-graph/internal/cli/code_graph.go:197-270` -- `buildGraph` creates all file nodes, resolves import matches, silently skips resolver errors, external imports, unresolved imports, missing nodes, self-imports, and duplicate `(source,target)` edges.
- `auto-graph/internal/cli/code_graph.go:259-266` -- existing edge attrs already include `import_kind` and the raw import specifier.
- `auto-graph/internal/graph/model.go:16-38` -- the graph schema is `Graph{Root, Nodes, Edges}` with optional attrs on nodes/edges; there is no warnings field.
- `auto-graph/internal/format/json.go:10-14` -- JSON output is written directly to the provided writer with a pretty-printing encoder.
- `auto-graph/internal/resolver/typescript_test.go:36-53` -- resolver tests already cover a simple `@/* -> src/*` alias.
- `auto-graph/internal/scanner/typescript_test.go:69-127` -- scanner tests assert import path and kind pairs for static, dynamic, `require`, side-effect, type import, and one named re-export.
- `auto-graph/internal/scanner/typescript_test.go:179-207` -- current re-export scanner coverage only checks one named re-export.
- `auto-graph/internal/cli/code_graph_test.go:11-101` -- CLI tests cover missing ast-grep, language detection, and `--lang` override; there is no graph-level alias/re-export/diagnostic regression test yet.
- `auto-graph/e2e/e2e_test.go:91-180` -- e2e JSON test decodes stdout as JSON, checks graph structure, edge references, duplicate edges, import kind coverage, and a golden file.
- `auto-graph/e2e/e2e_test.go:369-426` -- e2e referential-integrity test runs `autograph code graph` against every fixture directory containing `tsconfig.json`.
- `auto-graph/testdata/fixtures/path-aliases/tsconfig.json:1` -- existing alias fixture uses `baseUrl: "."` and `paths: { "@/*": ["src/*"] }`.
- `auto-graph/testdata/fixtures/path-aliases/src/index.ts:1` -- existing alias fixture only contains one static alias import.
- `auto-graph/testdata/fixtures/all-import-styles/reexport_source.ts:1` -- existing re-export fixture only contains one named re-export.

## Patterns

- `auto-graph/CLAUDE.md:5-23` documents module-local build, test, and vet commands: `go build ./cmd/autograph/`, `go test ./...`, and `go vet ./...`.
- `auto-graph/CLAUDE.md:26-29` documents ast-grep as a TypeScript scanner dependency and recommends `autograph doctor` for dependency verification.
- `auto-graph/CLAUDE.md:31-36` lists the package architecture: entrypoint under `cmd/autograph`, runtime context in `internal/app`, Cobra commands in `internal/cli`, and config in `internal/config`.
- `docs/tasks/001-ts-import-graph/requirements.md:17-23` established `autograph code graph` as a TypeScript file-level import graph with JSON default output, DOT/Mermaid formats, ast-grep parsing, tsconfig alias resolution, and checked-in fixtures.
- `docs/tasks/001-ts-import-graph/requirements.md:78-85` scoped out monorepo multi-tsconfig resolution; task 005 keeps the same boundary.
- `docs/tasks/001-ts-import-graph/feedback.md:3-6` records two scanner pitfalls: ast-grep needs both `ts` and `tsx` language modes, and `$$$` is needed for multi-name re-export patterns.
- `docs/tasks/001-ts-import-graph/feedback.md:13-15` records useful ast-grep details: `--globs '!dir'` exclusions and `$$$` versus `$_` matching.
- `docs/tasks/004-context-pack/solution.md:5-13` notes that current graph construction dedupes by `(source,target)` and can lose multiple import kinds; task 005 does not solve that broader metadata merge unless required by its fixture expectations.
- `docs/tasks/004-context-pack/solution.md:37-42` reinforces the CLI contract that successful payloads go to stdout and diagnostics/errors go to stderr.

## Related Tasks

- Task 001: TypeScript import graph. It introduced the scanner/resolver/graph path and required path aliases and re-exports, but task feedback shows re-export pattern issues were caught later.
- Task 004: Context pack. It plans a future extraction of graph construction into `internal/codegraph`, but that package is not present in the current worktree; task 005 should keep changes in the existing CLI/resolver/scanner path.
- Relevant commits from `git log`: `3d61ecd` merged the TypeScript file-level import graph, with earlier commits `d3203f8` for scanner work, `6d95322` for resolver work, and `7748ea9` for PR feedback broadening re-export patterns and ast-grep globs.
