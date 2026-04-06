---
hash: "a854db23"
id: "6450267b"
summary: "Technical design for implementing two-way freshness between docs and code tags in autodoc."
title: "Two-Way Freshness Technical Design"
---

# Two-Way Freshness Technical Design

## Goal

Implement static, deterministic verification between documentation files and code locations so `autodoc fix` can report when:

1. A doc changed and linked code tags are stale.
2. A linked code scope changed and the doc may be stale.
3. A code tag references a missing doc.

This design is based on:

- `docs/two-way-freshness.md`
- `docs/two-way-freshness-guide.md`

## Scope

Included:

- Doc `id` support in frontmatter.
- Parsing and validating `[autodoc(docId@docHash, scopeHash)]` tags in code.
- Indentation-scoped hashing for `scopeHash`.
- Integrating freshness checks into `autodoc fix` output.

Not included:

- AST-aware language parsing.
- Automatic semantic doc updates.
- New standalone freshness commands (feature lives under `autodoc fix`).

## Current Baseline

Existing behavior already in codebase:

- Doc metadata parsing/hashing in `internal/frontmatter`.
- Doc tree traversal in `internal/doctree`.
- AI-oriented fix instructions in `internal/commands/fix.go`.
- Hash rewrite command in `internal/commands/fixed.go`.

The implementation extends those paths instead of replacing them.

## Design Overview

The feature is implemented as four layers:

1. `frontmatter` layer: doc `id` parse/serialize/hash semantics.
2. `freshness` layer: tag discovery, parsing, scope hashing, and mismatch evaluation.
3. `fix` layer: merge existing doc issues with new doc-code link issues into one output.
4. CLI layer: keep `autodoc fix` as the entry point for detection + remediation instructions.

## Data Model Changes

### Frontmatter

Update `internal/frontmatter.Doc`:

```go
type Doc struct {
    ID      string
    Title   string
    Summary string
    Hash    string
    Body    string
}
```

Behavior:

- `Parse`: read optional `id`.
- `Serialize`: include `id` key if present; keep deterministic key ordering.
- `ComputeHash`: continue to hash `title`, `summary`, and `body`; exclude `hash` and `id`.
- `UpdateHash`: unchanged except it preserves `id`.

Invariant:

- Changing only `id` must never change the computed doc hash.

### Doctree Entry

Update `internal/doctree.Entry` to include `ID` so `fix` can build a doc index without re-reading files.

```go
type Entry struct {
    RelPath string
    ID      string
    Title   string
    Summary string
    Hash    string
    Body    string
}
```

### Freshness Types

Add `internal/freshness/types.go`:

- `TagRef`:
  - `FilePath` (repo-relative)
  - `Line` (1-based)
  - `RawTag`
  - `DocID`, `DocHash`, `ScopeHash`
  - `Indent`
- `LinkIssueKind` enum:
  - `DocHashMismatch`
  - `ScopeHashMismatch`
  - `OrphanedTag`
  - `MalformedTag`
- `LinkIssue`:
  - tag location fields
  - linked doc path/id when resolved
  - current correct doc hash and current scope hash when available
  - human action hint

## Tag Format and Parsing

Accepted syntax:

```text
[autodoc(docId@docHash, scopeHash)]
```

All three fields are 8-char lowercase hex.

Parser behavior:

- Scan code lines for `"[autodoc("`.
- Parse with strict regex:
  - `^\[autodoc\(([0-9a-f]{8})@([0-9a-f]{8}),\s*([0-9a-f]{8})\)\]$`
- If a marker is present but does not parse, emit `MalformedTag` with location.

The scanner is language-agnostic and does not parse comment syntax.

## Code File Discovery

Implement `internal/freshness/scanner.go`:

- Enumerate tracked files via `git ls-files` from repo root.
- Ignore known heavy/generated paths (vendor, node_modules, dist/build outputs).
- Read candidate files and keep only those containing `"[autodoc("`.
- For each candidate, emit one `TagRef` per parsed tag.

Output from scanner is deterministic:

- Sort by file path then line number.

## Scope Hash Algorithm

Implement `internal/freshness/scopehash.go`.

For each tag:

1. Let `tagIndent` be count of leading whitespace chars on tag line.
2. Start from the next line.
3. Keep lines with indentation `>= tagIndent`.
4. Stop at first non-blank line with indentation `< tagIndent`.
5. Blank lines do not terminate scope.
6. Strip all `[autodoc(...)]` substrings from collected text.
7. MD5 hash and take first 8 hex chars.

Notes:

- Column-0 tags scope until EOF.
- Tabs are counted as characters (matching docs).
- Line ending normalization is applied before hashing (`\r\n` -> `\n`) for stable results across platforms.

## Freshness Evaluation Logic

Implement `internal/freshness/checker.go`.

Inputs:

- Doc index keyed by `docID`.
- Parsed code tags.

Per-tag evaluation:

1. Resolve `docID` to doc.
2. If missing: `OrphanedTag`.
3. Else compare tag `docHash` to doc `hash`.
4. Compute current `scopeHash` and compare to tag `scopeHash`.
5. Emit issues for mismatches with current-correct values embedded.

If both doc and scope hashes mismatch, emit one combined issue payload with both current values to keep output concise.

Doc-level checks added to `fix`:

- Missing `id` in doc frontmatter.
- Duplicate `id` values across docs.

## `autodoc fix` Integration

`internal/commands/fix.go` becomes a unified checker/reporter with two sections:

1. Existing doc metadata issues:
   - missing frontmatter
   - stale doc hash
   - default/empty title
   - missing doc `id`
2. Link freshness issues:
   - doc hash mismatch in code tag
   - scope hash mismatch
   - orphaned tag
   - malformed tag

Output contract for link issues includes:

- code file + line
- raw tag
- resolved doc path + id when available
- current doc hash
- current scope hash
- explicit remediation action text

`autodoc fix` is allowed one ambiguity-free write: add random `id` values to docs missing `id`.
It does not rewrite code tags or rewrite doc semantic content; those remain instruction-driven.

## `autodoc fixed` Integration

`autodoc fixed <filepath>` remains the hash writer for docs.

Behavior:

- Recompute doc hash after content updates.
- Preserve doc `id`.
- Keep current search-index update behavior.

No code-tag rewriting is done by `autodoc fixed`; tag updates are directed via `autodoc fix` output.

## Error Handling and UX

Rules:

- Missing doc for a tag is a first-class issue, not a warning.
- Malformed tags report exact file and line.
- Duplicate doc IDs report every conflicting file.
- If no link/doc issues exist, preserve current clean message.

Message style is direct and actionable, optimized for AI agent execution.

## Performance Characteristics

- Doc walk: `O(number_of_docs)`.
- Code scan: `O(number_of_tracked_files)` with substring prefilter before regex parsing.
- Scope hash compute: proportional to covered scope size per tag.
- Expected runtime remains fast for medium repos; no network/LLM calls in detection path.

## Test Design

Add or extend tests in these areas.

### Frontmatter Tests

- Parse/serialize with `id`.
- Hash unchanged when only `id` changes.
- `UpdateHash` preserves `id`.

### Freshness Unit Tests

- Valid tag parsing.
- Malformed tag detection.
- Scope hashing for:
  - function-local tag
  - file-level tag
  - blank-line handling
  - mixed tabs/spaces
  - CRLF normalization
- Strip-tag behavior prevents cascade hash changes.

### Checker Tests

- Doc hash mismatch only.
- Scope hash mismatch only.
- Both mismatches together.
- Orphaned tag.
- Duplicate doc IDs.

### Fix Output Tests

- Snapshot-style assertions for link issue sections.
- Verify output includes current-correct hash values.
- Verify clean output when all docs and links are synchronized.

## File-Level Implementation Targets

Modify:

- `internal/frontmatter/frontmatter.go`
- `internal/frontmatter/frontmatter_test.go`
- `internal/doctree/doctree.go`
- `internal/commands/fix.go`
- `internal/commands/fix_test.go`

Add:

- `internal/freshness/types.go`
- `internal/freshness/scanner.go`
- `internal/freshness/parser.go`
- `internal/freshness/scopehash.go`
- `internal/freshness/checker.go`
- `internal/freshness/*_test.go`

Optional docs updates:

- `internal/commands/quickstart.go` help text to mention doc-code freshness checks under `autodoc fix`.
