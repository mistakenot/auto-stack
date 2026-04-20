---
hash: "34e92e15"
id: "e8d3cf9c"
read_when: "implementing two-way freshness checking between code and documentation"
summary: "Implementation details for two-way code-doc freshness checks in autodoc."
title: "Two-Way Freshness: Technical Solution"
---

# Two-Way Freshness: Technical Solution

This document describes the implementation for two-way freshness checking in autodoc. It covers all code changes, new types, new functions, modified commands, and edge case handling. Read alongside [two-way-freshness.md](two-way-freshness.md) (design spec) and [two-way-freshness-guide.md](two-way-freshness-guide.md) (walkthrough).

---

## 1. Frontmatter Changes

### New `id` field

The `frontmatter.Doc` struct gains an `Id` field:

```go
type Doc struct {
    Id      string
    Title   string
    Summary string
    Hash    string
    Body    string
}
```

**Hash computation exclusions**: Both `id` and `hash` are excluded from `ComputeHash()`. The hash is computed from sorted remaining frontmatter keys (`summary`, `title`) concatenated with the body. Adding `id` to an existing doc does not change its hash — no stale wave on rollout.

**Parsing/serialization**: `Parse()` reads the `id` key. `Serialize()` writes it in sorted order with the other keys. If `id` is empty, it is omitted from serialized output (backwards compatible with docs that don't have one yet).

**ID generation**: 8 chars of a random hex string (`crypto/rand`, 4 bytes, hex-encoded). `autodoc fix` generates and writes IDs in-place for any docs missing an `id`.

---

## 2. New Package: `internal/linkscan`

This package handles all code-side scanning — finding autodoc tags in source files, parsing them, and computing scope hashes. It has no dependency on `frontmatter` or `doctree`.

### Types

```go
// Tag represents a single parsed [autodoc()] tag found in a source file.
type Tag struct {
    FilePath  string // absolute path to the source file
    Line      int    // 1-indexed line number where the tag appears
    DocId     string // 8-char hex doc ID
    DocHash   string // 8-char hex doc hash snapshot
    ScopeHash string // 8-char hex scope hash snapshot
    RawTag    string // the full [autodoc(...)] string as found
}

// ScanResult holds all tags found across the scanned files.
type ScanResult struct {
    Tags []Tag
}
```

### Functions

#### `ScanFiles(rootDir string) (ScanResult, error)`

Discovers all source files via `git ls-files` (run from `rootDir`), filters out known noise directories (`vendor/`, `node_modules/`, `.git/`, build artifacts), then scans each file line-by-line for `[autodoc(` substrings.

**Why `git ls-files`**: Only scans tracked files. Respects `.gitignore`. Fast — no filesystem walk needed.

**Regex for tag extraction**:

```
\[autodoc\(([0-9a-f]{8})@([0-9a-f]{8}),\s*([0-9a-f]{8})\)\]
```

Captures: docId, docHash, scopeHash. Strict 8-char hex for each field.

If a line contains `[autodoc(` but does not match the strict format, it is recorded as a malformed tag parse error with file + line + raw text.

#### `ComputeScopeHash(filePath string, tagLine int) (string, error)`

Computes the scope hash for a tag at a given line number.

**Algorithm**:

1. Read the file.
2. Find the tag line. Record its indentation (count of leading whitespace characters — tabs count as 1).
3. Starting from the **next line**, collect all lines where:
   - The line is blank (empty or whitespace-only) — always included, never terminates scope.
   - The line's indentation is >= the tag line's indentation.
4. Stop at the first non-blank line with shallower indentation.
5. Join collected lines. Strip all `[autodoc(...)]` substrings from the joined text.
6. Return first 8 chars of MD5 hex digest.

**Column-0 tags**: If the tag is at column 0 (zero indentation), scope extends to EOF. This is the degenerate case for flat files (shell scripts, Makefiles).

**Blank line handling**: Blank lines are transparent — they never terminate the scope. This prevents false triggers from empty lines between struct fields or function statements.

---

## 3. New Package: `internal/linkcheck`

Orchestrates the comparison between code tags and doc frontmatter. This is the core staleness detection logic.

### Types

```go
type LinkStatus int

const (
    LinkOK             LinkStatus = iota
    DocHashMismatch               // doc updated, code tag has old docHash
    ScopeHashMismatch             // code changed, doc may need updating
    BothMismatch                  // both doc and code changed since last sync
    OrphanedTag                   // doc ID not found in any doc
    MalformedTag                  // autodoc marker found but invalid format
)

// LinkIssue represents a single staleness finding.
type LinkIssue struct {
    Status          LinkStatus
    Tag             linkscan.Tag
    DocFile         string // relative path to the doc file (empty if orphaned)
    CurrentDocHash  string // what the doc hash actually is now
    CurrentScopeHash string // what the scope hash actually is now
}
```

### Functions

#### `Check(tags []linkscan.Tag, docs []doctree.Entry) ([]LinkIssue, error)`

For each tag:

1. **Resolve doc ID**: Build a map of `id → Entry` from the docs slice. If the tag's `DocId` is not found, emit `OrphanedTag`.
2. **Compare doc hash**: If `tag.DocHash != doc.Hash`, the doc has been updated since the tag was last synced.
3. **Compute scope hash**: Call `linkscan.ComputeScopeHash(tag.FilePath, tag.Line)`. If the result != `tag.ScopeHash`, the code has changed.
4. **Classify**: Both mismatched → `BothMismatch`. Only doc → `DocHashMismatch`. Only scope → `ScopeHashMismatch`. Neither → `LinkOK`.

Returns only non-OK issues.

Malformed tags are discovered during scanning and added to the final reported issue set as `MalformedTag` entries.

**Performance**: Scope hash computation reads each source file once per tag. For files with multiple tags, the file content is read once and reused (internal caching within the check pass).

---

## 4. Modified Command: `fix`

The existing `Fix()` function gains a new section after the doc freshness checks. The overall flow becomes:

1. **Doc freshness** (existing): Walk docs, identify missing frontmatter / stale hashes / default titles. Group and output instructions.
2. **ID assignment + write-through** (new): For any doc missing an `id`, `fix` generates a random ID and writes it directly into the doc file before continuing.
3. **Link freshness** (new): Scan tracked code files for autodoc tags. Run `linkcheck.Check()`. Output a section titled "Link Freshness" with instructions for each issue.

Ambiguity-free mutation policy: ID write-through is the only automatic file mutation `fix` performs.

### Output format for link issues

Each issue type gets a distinct block:

**Scope hash mismatch** (code changed):
```
LINK STALE: code changed, doc may need updating
  code file: <relative path>:<line>
  tag:       [autodoc(<docId>@<oldDocHash>, <oldScopeHash>)]
  doc:       <doc relative path> (id: <docId>)
  current doc hash:   <currentDocHash> (unchanged)
  current scope hash: <currentScopeHash> (was <oldScopeHash>)
  action: Read the code scope and the doc. If the doc is still accurate,
          update the tag to [autodoc(<docId>@<currentDocHash>, <currentScopeHash>)].
          If the doc needs updating, update the doc content first,
          then run `autodoc fixed <docPath>` to get the new doc hash,
          then update the tag with both new hashes.
```

**Doc hash mismatch** (doc updated):
```
LINK STALE: doc updated, code tag needs refresh
  code file: <relative path>:<line>
  tag:       [autodoc(<docId>@<oldDocHash>, <scopeHash>)]
  doc:       <doc relative path> (id: <docId>)
  current doc hash:   <currentDocHash> (was <oldDocHash>)
  current scope hash: <scopeHash> (unchanged)
  action: Update the docHash in the code tag to <currentDocHash>.
          New tag: [autodoc(<docId>@<currentDocHash>, <scopeHash>)]
```

**Both mismatched (single combined issue block)**:
```
LINK STALE: both code and doc changed since last sync
  code file: <relative path>:<line>
  tag:       [autodoc(<docId>@<oldDocHash>, <oldScopeHash>)]
  doc:       <doc relative path> (id: <docId>)
  current doc hash:   <currentDocHash> (was <oldDocHash>)
  current scope hash: <currentScopeHash> (was <oldScopeHash>)
  action: Read both the code scope and the doc carefully.
          Update the doc if needed, then run `autodoc fixed <docPath>`.
          Update the tag with both current hashes.
```

**Orphaned tag**:
```
LINK ORPHANED: doc not found for id <docId>
  code file: <relative path>:<line>
  tag:       [autodoc(<docId>@<docHash>, <scopeHash>)]
  action: The referenced doc no longer exists. Remove the tag or
          update it to reference a valid doc ID.
```

**Malformed tag**:
```
LINK ERROR: malformed autodoc tag
  code file: <relative path>:<line>
  tag text:  <raw line or extracted marker>
  action: Fix the tag format to:
          [autodoc(<docId>@<docHash>, <scopeHash>)]
```

### Error semantics

- `fix` continues scanning and reporting all results even when malformed tags are found.
- If one or more malformed tags are found, `fix` exits non-zero after printing full output.

### Grouping

Link issues are NOT grouped by parallelism — they are listed sequentially after all doc freshness groups. Rationale: link fixes often depend on doc fixes completing first (e.g., a doc gets a new hash from a content update, then the code tag must reference that new hash). Running them after doc fixes avoids ordering problems.

---

## 5. Modified Command: `stale`

`stale` remains doc-only. It does not check code links. Rationale: `stale` is a fast, focused check for doc hash correctness. Link checking requires `git ls-files` + file I/O across the whole repo, which is a different performance profile. The `fix` command handles both.

No changes to `stale`.

---

## 6. Modified Command: `fixed`

`Fixed()` already recomputes the doc hash. No changes needed — it already excludes `hash` from computation, and will naturally exclude `id` once `ComputeHash` is updated to skip it.

The only change: `ComputeHash` must be updated to exclude `id` from the sorted field set. This is a one-line change — don't include `id` in the `fields` map.

---

## 7. Modified: `testutil.Workspace`

Add helpers for two-way freshness testing:

```go
// WriteDocWithId writes a doc file with id, title, summary, hash, and body.
func (ws *Workspace) WriteDocWithId(relPath, id, title, summary, hash, body string) string

// WriteSourceFile writes a source file (Go, Python, etc.) at the given path.
// Convenience wrapper — same as WriteFile but semantically distinct.
func (ws *Workspace) WriteSourceFile(relPath, content string) string

// InitGitRepo initializes a git repo in the workspace and does an initial commit
// of all files. Required because linkscan uses `git ls-files`.
func (ws *Workspace) InitGitRepo()
```

The `InitGitRepo()` helper is necessary because `ScanFiles` depends on `git ls-files`. Tests that exercise link scanning must have a git repo with committed files.

---

## 8. Edge Cases and Decisions

### Tag inside a comment vs. bare in file

The scanner looks for `[autodoc(...)]` anywhere in a line. It does not parse comment syntax. A tag inside a Go `//` comment, a Python `#` comment, or an HTML `<!-- -->` block all work the same way. The indentation is measured from column 0, not from after the comment marker.

### Multiple tags on the same line

Not supported. One tag per line. If multiple `[autodoc()]` matches appear on one line, only the first is used. This is not expected to occur in practice.

### Tag in a doc file

If an `[autodoc()]` tag appears inside a markdown doc file that is tracked by git, it will be found by the scanner. This is harmless — the tag will either match a valid doc or be flagged as orphaned. No special exclusion needed.

### Binary files

`git ls-files` may return binary files. The line scanner will read them but find no `[autodoc(` substring (binary data won't match the regex). No special handling needed — the cost is a read that finds nothing.

### Very large repos

`git ls-files` output is piped and processed line-by-line. File reading for tag scanning is sequential but fast (just string matching, no parsing). Scope hash computation only reads files that contain tags. For a repo with 10,000 files and 50 tags, this means 10,000 line scans (cheap) + 50 file reads for scope hashing.

### Tabs vs. spaces

Indentation is measured by counting leading whitespace characters. A tab counts as 1 character, same as a space. This means a file mixing tabs and spaces could produce unexpected scope boundaries. This matches the design spec's stated behavior and is acceptable — mixed indentation files are rare and already problematic for other tools.

### Tag line itself excluded from scope hash

The tag line is metadata. It is not part of the code being documented. The scope starts from the line after the tag. This prevents circular hashing (updating the tag's hashes would change the scope hash).

### Autodoc strings stripped before hashing

All `[autodoc(...)]` substrings are removed from the scope text before hashing. This means if a file has two tags at different scopes, updating one tag's hashes does not change the other tag's scope hash. It also means updating your own tag's hashes does not change your own scope hash.

### Doc renames

Handled by the `id` field. Code tags reference the doc by ID, not by file path. `doctree.Walk` returns all docs; `linkcheck.Check` builds an `id → Entry` map. A renamed doc is found as long as its `id` is unchanged. No scanning by path needed.

### Concurrent tag references to the same doc

Multiple code files can reference the same doc ID. When the doc changes, all tags referencing it will have `DocHashMismatch`. Each is reported independently. The fix for each is the same: update the `docHash` field in the tag.

### ID collisions

8 chars of hex = 4 bytes = ~4 billion possible values. Collision probability is negligible for any reasonable number of docs. If a collision occurs, `fix` will report incorrect link status. Manual resolution required — change one doc's ID.

---

## 9. File Changes Summary

| File | Change |
|------|--------|
| `internal/frontmatter/frontmatter.go` | Add `Id` field to `Doc`. Exclude `id` from `ComputeHash`. Parse/serialize `id`. |
| `internal/frontmatter/frontmatter_test.go` | Tests for `id` parsing, serialization, hash exclusion. |
| `internal/linkscan/linkscan.go` | **New file.** `Tag` type, `ScanResult` type, `ScanFiles()`, `ComputeScopeHash()`. |
| `internal/linkscan/linkscan_test.go` | **New file.** Tests for tag parsing, scope hashing, edge cases. |
| `internal/linkcheck/linkcheck.go` | **New file.** `LinkStatus`, `LinkIssue`, `Check()`. |
| `internal/linkcheck/linkcheck_test.go` | **New file.** Tests for all four issue types, multi-tag scenarios. |
| `internal/commands/fix.go` | Add ID generation + in-place write for docs missing `id`. Add link freshness section after doc freshness. Continue on malformed tags and return non-zero at end when malformed tags exist. |
| `internal/commands/fix_test.go` | Tests for ID write-through behavior, link issue output formatting, malformed tag error handling, mixed scenarios. |
| `internal/testutil/fixture.go` | Add `WriteDocWithId()`, `WriteSourceFile()`, `InitGitRepo()`. |
| `cmd/autodoc/e2e_two_way_freshness_test.go` | **New file.** End-to-end CLI lifecycle tests in temp git workspaces. |
| `cmd/autodoc/e2e_helpers_test.go` | **New file.** Shared helpers for binary execution, fixture copy, git init, output parsing. |
| `cmd/autodoc/testdata/two_way_freshness/...` | **New directory.** Minimal doc + code fixtures used by e2e tests. |
| `cmd/autodoc/main.go` | No changes expected — `fix` command signature unchanged. |

---

## 10. End-to-End Test Coverage

Two-way freshness needs black-box CLI e2e tests that exercise the full flow:

- create temp repo workspace
- write explicit doc + code fixture files
- run real `autodoc` commands
- inspect output and resulting file states

### Existing project patterns to reuse

This repo already has two strong patterns:

1. `auto-etl-2/e2e_test.go`: full e2e harness with `TestMain`, build binary once, run commands via `exec.Command`.
2. `auto-etl/cmd/auto-etl/main_test.go`: CLI black-box tests that build a binary and assert stdout/stderr behavior.

Two-way freshness e2e tests should follow the same style.

### New e2e files (autodoc)

Add:

- `cmd/autodoc/e2e_two_way_freshness_test.go`
- `cmd/autodoc/e2e_helpers_test.go`
- `cmd/autodoc/testdata/two_way_freshness/docs/caching.md`
- `cmd/autodoc/testdata/two_way_freshness/pkg/cache/lru.go`

### Harness design

#### Binary build

- Build `autodoc` once in `TestMain` into a temp binary path.
- Share that path across tests to reduce runtime.

#### Workspace setup per test

Each test:

1. Creates a fresh temp directory.
2. Copies fixture files from `cmd/autodoc/testdata/two_way_freshness`.
3. Initializes git repo (`git init`, user/email config, `git add .`, initial commit).
4. Runs commands from workspace cwd via helper:
   - `runCLI(t, cwd, args...) -> (stdout string, stderr string, exitCode int)`

This guarantees `git ls-files` behavior matches real user flow.

### Core e2e scenarios

#### E2E 1: full happy-path lifecycle

1. `autodoc init`
2. Start fixture doc without an `id`
3. `autodoc fix` -> assert `id` is now written to doc file
4. `autodoc fixed docs/caching.md` (establish current doc hash)
5. `autodoc fix` (extract current doc hash + scope hash from output)
6. Rewrite tag in code to those values
7. `autodoc fix` again -> assert no link freshness errors
8. Modify code scope -> `autodoc fix` reports scope mismatch
9. Modify doc content + run `autodoc fixed docs/caching.md` -> `autodoc fix` reports doc-hash mismatch
10. Delete doc file -> `autodoc fix` reports orphaned tag

#### E2E 2: one doc referenced by many code files

1. Two source files contain tags to same doc ID.
2. Change doc + run `autodoc fixed`.
3. `autodoc fix` should report both code locations as stale.

#### E2E 3: multiple tags in one file, isolated scopes

1. Two function-level tags in one file.
2. Edit only one function body.
3. `autodoc fix` should report only that function tag as scope-stale (plus any file-level tag, if present by fixture design).

### E2E helper functions

Add helper functions to keep tests readable:

- `copyFixtureTree(t, src, dst string)`
- `initGitRepo(t, cwd string)`
- `runCLI(t, cwd string, args ...string) (stdout, stderr string, exit int)`
- `extractCurrentHashes(t, fixOutput string) (docHash string, scopeHash string)`
- `rewriteAutodocTag(t, filePath, docID, docHash, scopeHash string)`

### Assertions

For each e2e scenario, assert:

- command exit codes
- docs missing `id` are mutated by `autodoc fix` (new `id` persisted on disk)
- presence/absence of specific issue headers:
  - `LINK STALE: code changed, doc may need updating`
  - `LINK STALE: doc updated, code tag needs refresh`
  - `LINK ORPHANED: doc not found`
  - `LINK ERROR: malformed autodoc tag`
- expected code file path + line presence in output
- updated tag text written exactly once when rewritten

### Test runner behavior

- E2E tests run in normal `go test ./...` (no build tags, no opt-in flag).

### Why e2e is required here

Unit tests validate parsing/hash logic, but only e2e validates integration across:

- git-backed file discovery (`git ls-files`)
- frontmatter ID/hash behavior
- scope hash computation from real files
- fix output formatting contract used by AI agents

This feature crosses package boundaries; e2e is the correctness backstop.

---

## 11. Non-Goals

These are explicitly out of scope for this change:

- **Separate CLI command for link checking** — `fix` handles everything. No `autodoc links` or `autodoc link-stale` command.
- **Auto-fixing code links or doc semantics** — `fix` does not rewrite code tags and does not rewrite doc content/summary/title. It only auto-writes missing doc `id` fields.
- **IDE integration** — No LSP, no editor plugin. The workflow is CLI-driven.
- **Cross-repo links** — Doc IDs are scoped to a single repo. No support for referencing docs in other repositories.
- **Frontmatter `id` in the search index** — The `id` field is not indexed by Bluge. It's only used for link resolution.
- **Scope hash caching** — No persistent cache of scope hashes. They are recomputed on every `fix` run. The computation is fast enough (MD5 of a function body) that caching adds complexity without meaningful benefit.
