---
hash: "1a8075b3"
id: "6f5672e2"
summary: "Step-by-step TDD implementation plan for the autodoc CLI: commands, config, frontmatter, and BM25 search"
title: "autodoc Implementation Plan"
---

# autodoc (`autodoc`) Implementation Plan

## Summary

- Rewrite CLI to match spec: `init`, `tree`, `stale`, `agents`, `fix`, `fixed`
- Fix config to use `docs.json` with full schema (`agentFiles`, `parallelism`)
- Fix frontmatter to use `hash` (not `summaryHash`)
- TDD throughout
- [COMPLETED] Bluge BM25 search index package (`internal/search/index.go`) — wraps Bluge v0.2.2, implements OpenIndex, UpsertDoc, DeleteDoc, Search, IndexExists, Close with full TDD coverage (12 tests)

## TDD Workflow

Every step follows red-green-refactor:

1. **Red**: Write the `_test.go` file first. Create a minimal stub (empty struct, function returning zero values) so it compiles. Run `go test` — all new tests must **fail**.
2. **Green**: Implement just enough code to make the tests pass. No more.
3. **Refactor**: Clean up the implementation. Tests must still pass.
4. **Verify**: `go test ./...` — entire suite green before moving on.

Each step below lists specific test cases to write in the Red phase.

## Out of Scope

- ~~Search/indexing functionality~~ (implemented separately as `internal/search/` package)
- Any AI/LLM integration (fix command outputs text instructions only)
- Remote/network features

## Existing State

- Cobra CLI scaffold with wrong commands (`status`, `search`, `sync` — all stubs)
- Config loader exists but wrong filename (`autodoc.json`) and missing fields
- Test workspace utility exists and works but uses `summaryHash` instead of `hash`
- `spf13/cobra` dependency already in go.mod

## What We Need to Build

### Core packages needed:
- `internal/config` — load `docs.json` (update existing)
- `internal/frontmatter` — parse/write YAML frontmatter, compute hash
- `internal/doctree` — walk docs dir, build tree of doc files
- `internal/commands` — each CLI command as a separate file
- `internal/testutil` — update fixture to use `hash` field

---

## Execution Plan

### Phase 1: Foundation — Config & Frontmatter

#### Step 1a: Fix config package
- **Test**: Load `docs.json` with all three fields. Defaults when absent. Ignore unknown keys.
- **Implement**: Update `Config` struct to add `AgentFiles []string` and `Parallelism int`. Change default filename to `docs.json`. Set defaults (`["AGENTS.md", "CLAUDE.md"]`, `4`).
- **Files**: `internal/config/config.go`, `internal/config/config_test.go`

#### Step 1b: Frontmatter package
- **Test**: Parse frontmatter from markdown string → returns `title`, `summary`, `hash`, and body. Round-trip: parse then serialize preserves content. Missing frontmatter returns zero values. Compute hash: sorted keys (excl hash) → concat values → MD5 → first 8 hex chars.
- **Implement**: Parse/serialize YAML frontmatter. Hash computation function. Write-back function that updates hash in-place on a file.
- **Files**: `internal/frontmatter/frontmatter.go`, `internal/frontmatter/frontmatter_test.go`

#### Step 1c: Update test fixtures
- **Change**: `WriteDoc` to use `hash` instead of `summaryHash`
- **Files**: `internal/testutil/fixture.go`, `internal/testutil/fixture_test.go`

### Phase 2: Doc Tree Walker

#### Step 2: doctree package
- **Test**: Walk a temp docs dir → returns sorted list of doc entries with path, title, summary. Handles nested subdirs. Skips non-`.md` files. Empty dir returns empty list.
- **Implement**: Walk function that reads each `.md` file's frontmatter and returns structured entries preserving directory hierarchy.
- **Files**: `internal/doctree/doctree.go`, `internal/doctree/doctree_test.go`

### Phase 3: Commands (each step is independent after Phase 2)

#### Step 3a: `autodoc tree`
- **Test**: Given workspace with docs, output matches expected tree format with titles and summaries. Nested dirs render correctly.
- **Implement**: Uses doctree to walk, renders tree with box-drawing chars (`├──`, `└──`).
- **Files**: `internal/commands/tree.go`, `internal/commands/tree_test.go`

#### Step 3b: `autodoc stale`
- **Test**: File with correct hash → not stale. File with wrong hash → stale. File missing frontmatter → stale. Exit code 0 when clean, 1 when stale. Output shows "Stale" instead of summary.
- **Implement**: Walks docs, computes hash for each, compares to stored hash.
- **Files**: `internal/commands/stale.go`, `internal/commands/stale_test.go`

#### Step 3c: `autodoc fixed <filepath>`
- **Test**: Given a doc file, recalculates hash and writes it. Verify file content updated correctly. Sorts keys alphabetically.
- **Implement**: Read file, parse frontmatter, compute hash, write back.
- **Files**: `internal/commands/fixed.go`, `internal/commands/fixed_test.go`

#### Step 3d: `autodoc agents`
- **Test**: Creates marker block in AGENTS.md when no markers exist (append). Replaces content between existing markers. Creates AGENTS.md if neither agent file exists. Works with CLAUDE.md too.
- **Implement**: Reads agent files from config, finds/creates markers, inserts tree output.
- **Files**: `internal/commands/agents.go`, `internal/commands/agents_test.go`

#### Step 3e: `autodoc init`
- **Test**: Creates `docs.json` and `docs/` dir when missing. Doesn't overwrite existing `docs.json`. Runs tree output. Advises fix when stale files found.
- **Implement**: Check/create config and docs dir, run tree, check stale.
- **Files**: `internal/commands/init.go`, `internal/commands/init_test.go`

#### Step 3f: `autodoc fix`
- **Test**: Outputs correct instruction text listing files that need fixing, grouped by parallelism setting. Identifies files missing frontmatter, files with stale hashes, files with default titles.
- **Implement**: Walk docs, identify issues, format instruction output with grouping.
- **Files**: `internal/commands/fix.go`, `internal/commands/fix_test.go`

### Phase 3g: Search Normalization Pipeline ✓ [COMPLETED]

#### Step 3g: `internal/search` normalize package
- **Test**: Comprehensive TDD tests for the markdown normalization pipeline (16 tests).
- **Implement**: `Normalize(markdown string) NormalizedDoc` using goldmark AST walker. Routes H1-H3 text to headings buffer, body content to body buffer. Handles: bold/italic (strip markers), links (text only), images (alt text), blockquotes (strip `>`), fenced code blocks (preserve content), tables (cell text, strip pipes), whitespace collapsing.
- **Files**: `internal/search/normalize.go`, `internal/search/normalize_test.go`
- **Notes**: Added `github.com/yuin/goldmark v1.7.16` dependency. Used `goldmark/extension.Table` for GFM table support. H4+ headings go to body, not headings buffer. All 16 tests pass.

### Phase 4: Wire Up CLI

#### Step 4: Replace main.go commands
- **Change**: Remove old `status`, `search`, `sync` commands. Wire up `init`, `tree`, `stale`, `agents`, `fix`, `fixed`. Change binary name from `autodoc` to `autodoc`.
- **Test**: Build and run `autodoc --help` shows all commands.
- **Files**: `cmd/autodoc/main.go`

## How to Test

- `go test ./...` after each step — all tests must pass
- Each command package has its own `_test.go` using the `testutil.Workspace` fixture
- Integration: after Phase 4, run `autodoc init` in a temp dir and verify full workflow

## Execution Sequence

```
Phase 1: [1a, 1b, 1c] — parallel
Phase 2: [2]           — depends on 1b
Phase 3: [3a, 3b, 3c, 3d, 3e, 3f] — parallel (all depend on Phase 2)
Phase 4: [4]           — depends on Phase 3
```
