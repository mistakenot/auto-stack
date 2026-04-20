---
hash: "3b345bdb"
id: "11a9fe19"
read_when: "implementing recursive documentation discovery across monorepo structures"
summary: "Technical PRD for autodoc to discover all `docs` directories recursively and index markdown files recursively under each."
title: "Recursive Docs Discovery Technical PRD"
---

# Recursive Docs Discovery Technical PRD

## Goal

Enable `autodoc` to discover documentation across monorepos by default using this rule:

1. Find directories named `docs` recursively from repo root.
2. For each discovered `docs` directory, find `.md` files recursively under that directory.

This behavior must apply consistently to `tree`, `stale`, `fix`, `agents`, and `search reindex`, without changing user-facing CLI/API.

## Problem Statement

Current implementation walks exactly one docs root (`cfg.DocsDir`) and misses docs in sub-projects.

Examples in this repository:

- `docs/...` (repo root docs)
- `auto-doc/docs/...`
- `auto-etl/docs/...`
- `auto-etl-2/docs/...`

Today, only one of these is processed per command run.

## Current Baseline

- Single-root traversal in `doctree.Walk(docsDir, ...)`:
  - `auto-doc/internal/doctree/doctree.go`
- Single-root command wiring:
  - `auto-doc/cmd/autodoc/main.go`
- Single-root assumptions in output/index/link paths:
  - `auto-doc/internal/commands/docsindex.go`
  - `auto-doc/internal/commands/search_reindex.go`
  - `auto-doc/internal/commands/fix.go`
  - `auto-doc/internal/commands/agents.go`

## Requirements

### Functional Requirements

1. Recursive docs-root discovery:
   - Scan repo root recursively for directories where `basename == "docs"`.
2. Recursive markdown discovery:
   - Within each docs root, include files ending in `.md` recursively.
3. No duplicate documents:
   - If nested docs roots would cause duplicate file inclusion, each markdown file is indexed exactly once.
4. Deterministic ordering:
   - Stable sort by repo-relative path for consistent output and tests.
5. Command parity:
   - `tree`, `stale`, `fix`, `agents`, and `search reindex` use the same discovered set.
6. Repo-relative canonical paths:
   - All user-facing and index paths are repo-relative (e.g. `auto-etl-2/docs/reference/normalized-schema.md`).
7. Ignore support:
   - Existing ignore globs continue to work and are evaluated against repo-relative paths.
8. Transparent API:
   - No new commands or required flags.
   - Existing config keys remain valid.
   - Existing command names/arguments remain unchanged.
9. Hierarchical agent index ownership:
   - For `autodoc agents`, each doc is indexed in the nearest ancestor agent memory file, not always the repo-root file.
   - Nearest-file resolution must be deterministic.

### Non-Functional Requirements

1. Performance:
   - Discovery should avoid full filesystem walks in normal git repos.
2. Correctness:
   - No path collisions between identical filenames in different docs roots.
3. Backward compatibility:
   - Existing repositories with only root `docs/` continue to behave as before.
4. Git-optional robustness:
   - If git is unavailable or repo context is missing, commands still function via filesystem fallback.

## Non-Goals

- Supporting non-markdown doc formats.
- Semantic content merging across docs.
- Automatic rewrite of stale code tags (existing `fix` behavior remains advisory).

## Design Overview

Introduce repo-level docs discovery in `internal/doctree` and migrate command layer from single-root APIs to multi-root APIs, while keeping CLI/config surfaces stable.

### 1) Data Model Changes (`doctree.Entry`)

Extend `Entry` to carry canonical repo path information:

```go
type Entry struct {
    // Existing
    RelPath string // relative to docs root (kept for compatibility)
    Id      string
    Title   string
    Summary string
    Hash    string
    Body    string

    // New
    DocsRootRel string // docs root path relative to repo root, e.g. "auto-etl-2/docs"
    RepoRelPath string // file path relative to repo root, e.g. "auto-etl-2/docs/reference/x.md"
    AbsPath     string // absolute file path for writes
}
```

`RepoRelPath` becomes the canonical key for:
- search index document IDs
- CLI output paths
- docs index links inserted into agent files
- write operations in `fix` for missing IDs

### 2) Discovery API (`internal/doctree`)

Add repo-level APIs:

```go
// Discover candidate markdown files in docs roots using git-indexed paths first.
func DiscoverDocsMarkdownPaths(rootDir string) ([]string, error) // returns repo-relative file paths

// Walk all discovered markdown files and return merged entries.
func WalkRepo(rootDir string, ignores ...string) ([]Entry, error)
```

Keep `Walk(docsDir string, ignores ...string)` for compatibility and unit tests that target single-root logic.

### 3) Discovery Rules

Primary strategy (git repositories):

1. Run:
   - `git -C <rootDir> ls-files -z --cached --others --exclude-standard`
2. For each returned repo-relative path:
   - include only `.md` files
   - include only paths containing a segment exactly named `docs`
3. Always include files under configured `docsDir` as compatible behavior, even if `docsDir` is not literally named `docs`.
4. Deduplicate by repo-relative path.
5. Sort by `RepoRelPath`.

Fallback strategy (no git / command fails):

1. Walk filesystem from `rootDir`.
2. Skip common heavy directories:
   - `.git`, `node_modules`, `vendor`, `dist`, `build`, `out`, `target`, `bin`
3. Include `.md` files whose path contains a `docs` segment.
4. Also include `.md` files under configured `docsDir` for compatibility.
5. Deduplicate and sort as above.

### 4) Ignore Matching Semantics

Existing ignore behavior is preserved but broadened:

- For repo mode, match ignores against `RepoRelPath`.
- Continue matching against basename for simple globs (`*.draft.md`).
- Continue supporting path-pattern globs (`tasks/*`, `docs/tasks/*`).

This keeps current user config useful while enabling nested docs roots.

### 5) Agent Index Ownership (Nearest Ancestor)

For `autodoc agents`, assign each discovered doc to exactly one owner file using nearest-ancestor lookup.

Resolution algorithm for each doc (`RepoRelPath`):

1. Start from the doc's directory and walk upward to repo root.
2. At each directory level, check candidate filenames in configured order (`agentFiles`, default `AGENTS.md` then `CLAUDE.md`).
3. First existing candidate is the owner.
4. If no owner is found in ancestors, assign doc to repo-root fallback owner:
   - `<root>/<agentFiles[0]>`
   - create this file if it does not exist (existing behavior).

Write behavior:

- Group docs by owner path.
- Update each owner file's autodoc block with only its assigned docs.
- Do not duplicate docs across multiple owner files.
- Links in each block use `RepoRelPath`.

## Command Behavior Changes

### `autodoc tree`

- Input: all entries from `WalkRepo`.
- Output: single unified tree rooted at repo root (not a single `docs/` root).
- Paths shown as repo-relative.

### `autodoc stale`

- Same discovered entry set as `tree`.
- Stale markers shown using repo-relative tree paths.

### `autodoc fix`

- Run doc freshness and ID write-through across all discovered docs.
- `writeMissingDocIDs` writes directly via `Entry.AbsPath` (not `docsDir + RelPath`).
- Link freshness output resolves doc paths via `RepoRelPath`.

### `autodoc agents`

- Generated index includes all docs across discovered roots.
- Docs are partitioned by nearest owner file (`AGENTS.md` / `CLAUDE.md`) in ancestor hierarchy.
- Each owner file receives only the docs it owns.
- Markdown links use `RepoRelPath` directly.

### `autodoc search reindex`

- Index all discovered docs.
- Index document key (`_id`) is `RepoRelPath`.
- Remove stale index entries not present in the latest discovered set (true rebuild semantics).

## Config and Compatibility

Keep current config shape unchanged for this phase:

```json
{
  "docsDir": "docs",
  "agentFiles": ["AGENTS.md", "CLAUDE.md"],
  "parallelism": 4,
  "ignores": []
}
```

Interpretation update:

- Default behavior is recursive discovery of directories named `docs`.
- `docsDir` remains in config and is still honored as a compatibility inclusion root (important for repos using non-`docs` doc folders).
- No user-facing API changes are required.

No mode flags are introduced in this phase.

## Implementation Plan

### Phase 1: Doctree foundation

1. Extend `Entry` with `DocsRootRel`, `RepoRelPath`, `AbsPath`.
2. Add `DiscoverDocsMarkdownPaths(rootDir)` and `WalkRepo(rootDir, ignores...)`.
3. Implement `git ls-files` discovery path and filesystem fallback.
4. Reuse/adjust ignore matcher to support repo-relative matching.
5. Add unit tests for discovery, fallback, and dedupe.

### Phase 2: Command migration

1. Update `main.go` command wiring to call repo-level walk.
2. Update `tree`/`stale` renderers for repo-root output.
3. Update `fix` to write and display via canonical repo-relative paths.
4. Update `agents` to resolve nearest owner file per doc, group docs by owner, and render per-owner blocks using `RepoRelPath`.
5. Update `search reindex` to index by `RepoRelPath` and remove stale index entries.

### Phase 3: Documentation and test updates

1. Update `CLAUDE.md` and quickstart docs to describe recursive behavior.
2. Add/adjust tests across:
   - `internal/doctree/*`
   - `internal/commands/tree_test.go`
   - `internal/commands/stale_test.go`
   - `internal/commands/fix_test.go`
   - `internal/commands/agents_test.go`
   - `internal/commands/search_reindex_test.go`
3. Expand impacted existing test suites with new cases:
   - `internal/commands/tree_test.go`
     - render repo-relative nested paths across multiple docs roots
     - deterministic ordering when two docs roots have same leaf filenames
   - `internal/commands/stale_test.go`
     - stale detection across multi-root entry set
     - stale output uses repo-relative paths
   - `internal/commands/fix_test.go`
     - missing ID write-through uses absolute path (`Entry.AbsPath`) for nested docs
     - link freshness output prints `RepoRelPath` doc paths
   - `internal/commands/agents_test.go`
     - nearest-owner assignment (nested owner files)
     - no-duplication guarantee (doc appears in one owner block only)
     - precedence when both `AGENTS.md` and `CLAUDE.md` exist at same level
   - `internal/commands/search_reindex_test.go`
     - indexes docs from multiple docs roots
     - removed docs are deleted from index on reindex
   - `internal/doctree/doctree_test.go`
     - `git ls-files` discovery path
     - filesystem fallback path
     - dedupe safety when multiple inclusion rules overlap

### Phase 4: End-to-End test updates

Add explicit E2E coverage in `auto-doc/e2e` to validate full CLI behavior in realistic repo layouts.

E2E scenarios:

1. Recursive multi-root discovery:
   - Fixture with `docs/`, `auto-doc/docs/`, `auto-etl-2/docs/`.
   - Assert `tree`, `stale`, `fix`, and `search reindex` observe all docs.
2. Git-indexed discovery behavior:
   - Assert untracked docs are included via `git ls-files --cached --others --exclude-standard`.
   - Assert git-ignored docs are excluded.
3. Non-git fallback:
   - Run same fixture without git repo.
   - Assert fallback filesystem discovery still finds docs.
4. Nearest owner routing for `agents`:
   - With local `AGENTS.md` / `CLAUDE.md` files at different depths, assert docs are written to nearest owner file only.
   - Assert no duplication across owner files.
5. Owner precedence at same level:
   - When both `AGENTS.md` and `CLAUDE.md` exist in the same directory, assert configured `agentFiles` order decides owner.
6. Root fallback owner:
   - If no ancestor owner exists, assert root fallback owner (`agentFiles[0]`) is created/updated.
7. Search reindex stale removal:
   - Remove a doc, rerun reindex, assert removed doc path no longer appears in search results.
8. Explicit nested-structure routing scenario:
   - Fixture:
     - `AGENTS.md` at repo root
     - `services/payments/CLAUDE.md`
     - `services/payments/docs/payments.md`
     - `services/payments/api/docs/endpoints.md`
     - `services/identity/docs/identity.md`
   - Assertions:
     - payments docs are indexed into `services/payments/CLAUDE.md`
     - identity docs are indexed into root `AGENTS.md` (fallback to nearest ancestor)
     - root file does not duplicate docs already owned by nested file
     - links are repo-relative and stable

## Acceptance Criteria

1. Given repo with:
   - `docs/a.md`
   - `auto-doc/docs/b.md`
   - `auto-etl-2/docs/c.md`
   all commands (`tree`, `stale`, `fix`, `agents`, `search reindex`) see all three docs in one run.
2. Search results paths are repo-relative and collision-free.
3. `fix` missing-ID writes correct files in nested docs roots.
4. Existing single-root repositories still pass existing behavior tests.
5. In a git repo, untracked docs files are included in command outputs.
6. In a non-git directory, discovery still works via fallback walk.
7. Given:
   - `auto-etl-2/docs/...` and `auto-etl-2/CLAUDE.md`
   - `auto-doc/docs/...` and `auto-doc/AGENTS.md`
   running `autodoc agents` updates those nearest files with their local docs, instead of pushing all docs to repo-root agent files.
8. E2E suite includes explicit coverage for recursive discovery, nearest-owner routing, and git/non-git discovery paths.

## Risks and Mitigations

1. Path collision risk:
   - Mitigation: use `RepoRelPath` as canonical identifier everywhere.
2. Performance on large monorepos:
   - Mitigation: use `git ls-files` as primary discovery mechanism; skip heavy directories in fallback walk.
3. Behavior change risk for users expecting single-root only:
   - Mitigation: preserve command surface and preserve `docsDir` compatibility inclusion behavior.
4. Git command dependency risk:
   - Mitigation: filesystem fallback path when git is unavailable.
5. Ownership ambiguity risk when both `AGENTS.md` and `CLAUDE.md` exist at same level:
   - Mitigation: use configured `agentFiles` order as strict precedence.

## Open Questions

1. Should tree/stale display grouped by docs root, or a single repo-root tree only?
2. Should we include git submodule docs by default (if parent `ls-files` does not enumerate them)?
