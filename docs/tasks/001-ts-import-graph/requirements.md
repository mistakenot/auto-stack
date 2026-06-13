---
hash: "be79b1c1"
id: "6d8ecad5"
read_when: "implementing or reviewing the TypeScript import graph feature in autograph"
summary: "Acceptance criteria for the autograph TypeScript import graph feature, covering ast-grep scanning, tsconfig path alias resolution, and AC-1 through AC-9."
title: "Task 001: TypeScript Import Graph Requirements"
---

# Task 001: TypeScript Import Graph

## Background

autograph's long-term goal is to be a **graph-based context engine** for coding agents and developers. Given a task, it should be able to assemble a relevant bundle of context — the files likely to need changes, the files affected by those changes, related documentation, recent commit history — so that an agent starts work with full situational awareness rather than discovering dependencies mid-flight.

Building that engine requires a rich, queryable graph where nodes aren't just code files but can eventually include git commits, doc files, scripts, config, and other artifacts. The **intermediate representation must be language-agnostic** and flexible enough to support arbitrary node and edge types as the tool matures.

This task is step one: teaching autograph to parse TypeScript projects (the predominant language we use) and produce a file-level import graph. TypeScript is the hard case — complex import syntax, path aliases, multiple file extensions — so getting this right establishes the parsing foundation. The graph structure itself should not be TypeScript-specific; TS imports are just the first edge type populating a general-purpose graph.

## Problem

auto-graph has no implementation yet. We need a tool that can quickly scan a TypeScript codebase and produce a file-level import graph showing how files reference each other. This is the first concrete step toward the context engine described above.

## Goals

- Add a `code graph` subcommand to autograph that scans a directory and outputs a file-level import graph
- Auto-detect language (starting with TypeScript, extensible to others later)
- Handle all TypeScript import styles: `import ... from "X"`, `import("X")` (dynamic), `require("X")`, `export ... from "X"`
- Resolve path aliases from `tsconfig.json` (`paths`, `baseUrl`)
- Use ast-grep as the parsing engine (check it's installed, fail with remediation hint if not)
- Output JSON by default, with flags for DOT and Mermaid formats
- Validate with both checked-in fixture projects and e2e tests against public repos

## Acceptance Criteria

**AC-1**: Basic import scanning
- Given: a TypeScript project with files importing each other via `import ... from "./foo"`
- When: `autograph code graph ./project`
- Then: JSON output contains a graph with nodes for each `.ts`/`.tsx` file and edges for each import relationship, with resolved relative paths

**AC-2**: All import styles recognized
- Given: a file using `import "X"`, `import("X")`, `require("X")`, `export { a } from "X"`, and `import type { T } from "X"`
- When: the file is scanned
- Then: all five import sources are captured in the graph

**AC-3**: tsconfig.json path alias resolution
- Given: a project with `tsconfig.json` containing `"paths": { "@/*": ["src/*"] }` and `"baseUrl": "."`
- When: a file imports `@/components/Button`
- Then: the graph resolves this to the actual file path (e.g. `src/components/Button.ts`)

**AC-4**: ast-grep dependency check
- Given: ast-grep is not installed on the machine
- When: `autograph code graph ./project`
- Then: exits with a clear error and remediation hint (e.g. "ast-grep not found: install with `npm i -g @ast-grep/cli` or `brew install ast-grep`")

**AC-5**: Multiple output formats
- Given: a scanned project
- When: `autograph code graph ./project` (no flags)
- Then: output is JSON to stdout
- When: `autograph code graph ./project --format=dot`
- Then: output is Graphviz DOT format
- When: `autograph code graph ./project --format=mermaid`
- Then: output is Mermaid graph syntax

**AC-6**: Fixture-based unit tests
- Given: checked-in TypeScript fixture projects under `auto-graph/testdata/fixtures/`
- When: `go test ./...` runs
- Then: tests validate import detection, alias resolution, and edge cases against expected graph output

**AC-7**: E2E tests against public repos
- Given: an e2e test script
- When: the script runs, it clones specific public TypeScript repos at pinned commits into `.tmp/` (gitignored)
- Then: runs `autograph code graph` on each and performs snapshot assertions on the output

**AC-8**: Language auto-detection
- Given: a directory containing `tsconfig.json`
- When: `autograph code graph ./directory` (no `--lang` flag)
- Then: TypeScript scanning is automatically selected
- When: no recognized config file is found
- Then: exits with a clear error suggesting `--lang=typescript` or listing supported languages

**AC-9**: Performance
- Given: a medium-sized TypeScript project (~500 files)
- When: `autograph code graph ./project`
- Then: completes in under 5 seconds on a modern machine

## Out of Scope

- Symbol-level import tracking (only file-level for now)
- Languages other than TypeScript (architecture should be extensible, but only TS is implemented)
- Caching or incremental scanning
- Watch mode / live updates
- Graph storage or persistence (output only)
- Monorepo multi-tsconfig resolution (single tsconfig.json at root for now)

## Open Questions

- (none, all resolved)
