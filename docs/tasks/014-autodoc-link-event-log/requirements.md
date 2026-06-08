# Task 014: autodoc-link-event-log

## Problem

autodoc's two-way freshness feature stores code↔doc links as inline `[autodoc(docId@docHash, scopeHash)]` tags inside source files (and equivalent markdown-embedded tags between docs). Keeping these in sync rewrites the linked files constantly: every doc edit forces a `docHash` rewrite, every code edit forces a `scopeHash` rewrite. These hash-only commits pollute file git history and corrupt co-change signals (tasks 010/011), which treat any commit touching two files as evidence they change together. We want all link state to live in an append-only event log, with the source/doc files **never decorated or rewritten** by autodoc.

## Core Principle

**autodoc never writes to source or doc files for link bookkeeping.** All link state — creation, verification/sync, removal — lives in `.auto/doc/links.jsonl`. Files are only ever *read* (statically) to resolve anchors and compute hashes. This is a deliberate reversal of the original design's inline-tag approach (which valued in-diff review visibility); we trade that visibility for clean file history, compensating with orphan/drift detection.

## Goals

- Store all link state in `.auto/doc/links.jsonl` as an append-only, immutable event log that merges cleanly (git union-style appends; existing lines never edited). Current state is a fold over the events.
- **Markerless, typed anchors**, stored entirely in the jsonl — no marker written into any file:
  - `symbol` — code anchored to a named symbol, resolved via ast-grep (reusing autograph's AST scanning). Survives line insertions because symbol identity is line-independent.
  - `heading` — markdown anchored to a heading path, scoped by the existing header-depth logic.
  - `file` — whole-file fallback for module-level links or languages ast-grep can't parse.
- Cover both **code↔doc** and **doc↔doc** links.
- **Author links via a CLI command** (`autodoc link add …`) that validates the anchor resolves before appending a `link_added` event. (Schema documented so lines are inspectable, but the command is the blessed path.)
- **Full cutover**: migrate existing inline tags into the log, then strip the inline hash-bearing tags from files and remove the inline-tag code path from autodoc. The log is the single source of truth.
- Preserve the freshness guarantee: `autodoc fix` still detects doc-changed, code-changed, both-changed, and orphaned/unresolvable states with current+expected hashes — but applying a fix appends a `link_synced` event to the log instead of editing any file.

## Acceptance Criteria

**AC-1**: Append-only, merge-safe event log
- Given: a repo with autodoc initialized
- When: a link is added, synced, or removed
- Then: a new line is appended to `.auto/doc/links.jsonl`; no existing line is mutated; folding the stream yields current state (last-writer-wins per link id). The file is git-tracked, with a `.gitattributes` union merge driver (or equivalent) so concurrent appends on different branches merge without conflict.

**AC-2**: Markerless typed anchoring
- Given: a `link_added` event with a `symbol`, `heading`, or `file` anchor
- When: autodoc resolves the link
- Then: it locates the target block by re-reading the current file (ast-grep for `symbol`, heading scan for `heading`, whole file for `file`) and hashes that block's content — without any marker existing in the file, and without depending on line numbers.

**AC-3**: Files are never written for link bookkeeping
- Given: links exist in the log
- When: any autodoc link operation runs (`link add`, `fix` sync, removal)
- Then: no source or doc file is modified by autodoc; the only writes are appends to `.auto/doc/links.jsonl`.

**AC-4**: Line-stable under unrelated edits
- Given: a link to a symbol/heading, and an edit that inserts unrelated lines elsewhere in the file
- When: `autodoc fix` recomputes link state
- Then: the link still resolves to the same block and reports no staleness from the line shift alone.

**AC-5**: Freshness detection preserved, fixes append events
- Given: links stored in the log
- When: a target doc's content changes, an anchored block's content changes, both change, or the target/anchor no longer resolves (doc deleted, symbol/heading renamed or removed)
- Then: `autodoc fix` reports doc-hash-mismatch, content-hash-mismatch, both-mismatch, and orphaned/unresolvable states respectively, with current+expected hashes — same fix-instruction contract — and applying a fix appends a `link_synced` event (no file edit).

**AC-6**: Link authoring command
- Given: a file, an anchor (symbol/heading/file), and a target doc id
- When: the user runs `autodoc link add <file> <anchor> <docId>`
- Then: the command validates that the anchor resolves and the doc id exists, then appends a `link_added` event with the current doc + content hashes; it fails with a clear remediation message if the anchor or doc id can't be resolved.

**AC-7**: Migration with full cutover
- Given: a repo with existing inline `[autodoc(docId@docHash, scopeHash)]` code tags and doc↔doc markdown-embedded tags
- When: the user runs `autodoc links migrate`
- Then: each tag is converted to a resolved anchor + `link_added` event in the log AND the inline tag text is removed from the file (one-time migration commit); the command reports every migrated and every unresolvable/malformed tag (never silently dropped); and it is idempotent (re-running adds nothing).

**AC-8**: No legacy path after cutover
- Given: migration has completed
- When: autodoc checks freshness
- Then: link state is read exclusively from the log; the inline hash-bearing format is no longer written or honored, and any stray inline tag is reported as needing migration rather than silently interpreted.

**AC-9**: Static and fast
- Given: the log and the repo files
- When: freshness is checked
- Then: checking is static only (no AI), and scopes work to the files referenced by anchors rather than reading every file in the repo.

## Out of Scope

- Auto-rewriting doc content/summaries (still AI-driven via `fix`, unchanged).
- Search indexing / BM25 / embeddings.
- Cross-repo links (doc IDs remain repo-scoped).
- Persistent hash cache (hashes recomputed on demand).
- Log compaction/GC (append-only; a later task if the log grows large).
- Sub-symbol / arbitrary-region anchoring beyond symbol/heading/file (use the nearest enclosing symbol or whole-file fallback).

## Open Questions

- [x] Q1 — Code/doc anchoring mechanism (answered: markerless typed anchors — `symbol` via ast-grep, `heading` via header scan, `file` fallback; stored entirely in the jsonl, no marker in any file).
- [x] Q2 — Existing inline-tag format (answered: full replacement — migrate, strip tags from files, remove the inline code path).
- [x] Q3 — Link scope (answered: both code↔doc and doc↔doc).
- [x] Q4 — Link authoring without a marker (answered: `autodoc link add` CLI command that validates the anchor before appending; schema documented for inspection).
