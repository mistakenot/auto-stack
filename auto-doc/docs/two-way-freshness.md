---
hash: "6d9c10f6"
id: "0dedcb3f"
read_when: "when understanding bidirectional code-documentation freshness and drift detection"
summary: "Design for bidirectional hash-based links between code and docs to detect and fix drift in either direction"
title: "Two-Way Freshness"
---

The goal of this feature is to create links between code locations and documentation files in such a way that we can run an automated checker to check if either the doc file or the code file has changed since either of these two artifacts were last updated. In doc files, we already have the notion of hash codes which are calculated across the content of the doc so when the contents of the docs change, they no longer match the hash code in the front matter that's how we know that it's no longer in sync with the summary in the front matter. We're going to implement a similar thing but for source code files where we will leave code comments and source code files that link back to the docs in such a way that they contain a hash code of both the doc and the code file this way if the hash of the code file doesn't match the hash that is in these links we know that the code has been updated and you know potentially the doc hasn't been updated to match it either. I'll describe how these will look below.

Intention: We want to be able to detect when the linkage between a code file and a doc file changes. We want to detect if the doc file changes but the code file hasn't changed, and we want to detect if the code file has changed but the doc hasn't changed.

The verification process for checking whether files are in sync or not should run fast and just use static checks with no AI.

The process to fix them can then use AI and should be incorporated into the fix command we already have.

## Doc Frontmatter

Docs gain a new `id` field — a random 8-char hex code, unique per doc. This is the stable identifier used in code references (immune to file renames/moves). `autodoc fix` generates the `id` for any doc missing one (same workflow as adding missing title/summary).

The `id` field is **excluded** from the doc hash computation (along with `hash` itself). This means adding an `id` to existing docs doesn't change their hashes — no one-time stale wave on rollout.

```markdown
---
id: "f3a1b09c"
hash: "2fd626aa"
summary: "Full-text keyword search over docs using BM25 scoring via Bluge"
title: "BM25 Keyword Search"
---

# BM25 Keyword Search

Full-text keyword search over documentation files using BM25 scoring via [Bluge](https://gith
```

## Code Tag Format

The tag format uses the doc ID (not path) as the primary reference. It is machine-oriented — human readability is optional via inline comments.

```
[autodoc(docId@docHash, scopeHash)]
```

- `docId` — the `id` frontmatter value from the doc file (8-char hex)
- `docHash` — the `hash` frontmatter value from the doc file (8-char hex)
- `scopeHash` — hash of the indentation-scoped code block this tag covers (8-char hex, see below)

Example in Go:

```go
func run_bm25() {
	// Our bm25 implementation. Refer to docs/bm25-search.md for full reference.
	// [autodoc(f3a1b09c@2fd626aa, a89d6902)]
	// ...
}
```

Users may optionally add a human-readable comment (like the path) on the line above the tag. This is not parsed — it's just for developer convenience.

## Scoped Hashing (`scopeHash`)

**Decision: hashing is per-tag, scoped by indentation — not per-file.**

Each `[autodoc()]` tag hashes only the code within its indentation scope. This means a change to an unrelated function in the same file does NOT trigger staleness for tags in other functions.

### How it works

1. Find the line containing the `[autodoc()]` tag. Note its indentation level.
2. Start collecting from the **next line** (the tag line itself is metadata, not part of the scope).
3. Collect all subsequent lines that have the same or greater indentation level as the tag line.
4. Stop when hitting a line with *shallower* indentation (fewer leading whitespace characters).
5. **Blank lines** (zero indentation) are skipped — they do not terminate the scope.
6. Strip all `[autodoc(...)]` strings from the collected text (so updating hashes doesn't cascade).
7. Hash the result — first 8 chars of MD5 hex digest.

### Placement guidance

- **Inside a function** (recommended for Go/Java/C): Place the tag on the first line after the opening brace, indented one level. This scopes the hash to just that function body.
- **Top of file** (column 0): Scopes to everything from that line to EOF. Use this for module-level docs or cross-cutting concerns.
- **Nested blocks**: The tag covers everything at its indentation level or deeper.

Go-specific note: Because Go function signatures and closing braces are at column 0, placing the autodoc comment *inside* the function body (indented) is the correct approach. A column-0 comment above a func would hash until the next column-0 line, which captures more than intended.

### Edge cases

- Files with no indentation (shell scripts, Makefiles, flat config): the heuristic degenerates to "hash everything from this line to EOF." This is documented expected behavior.
- Language-agnostic: the scanner only cares about `[autodoc(...)]` strings and leading whitespace. It doesn't parse comment syntax.

## Staleness Detection and Fixing

There is no separate `stale` command. `autodoc fix` handles both detection and fix instructions in one pass. It scans code files by running `git ls-files` then grepping for `[autodoc()]` tags (automatically skips vendor/, node_modules/, and build artifacts).

`autodoc fix` now handles both its preexisting responsibility (checking doc summaries are correct) AND doc-code link freshness. The output is text instructions for an AI agent.

It detects:

1. **Doc hash mismatch**: The `hash` in doc frontmatter doesn't match computed hash → doc content changed, summary may be stale.
2. **Doc hash in code doesn't match**: The `docHash` in an `[autodoc()]` tag doesn't match the doc's current `hash` → doc was updated, code tag needs refresh.
3. **Scope hash mismatch**: The `scopeHash` in an `[autodoc()]` tag doesn't match the computed hash of the scoped code block → code changed, doc may need updating.
4. **Orphaned tags**: An `[autodoc()]` tag references a doc ID that doesn't exist in any doc file → doc was deleted or ID changed.

For each link error, `fix` outputs the **current correct hashes** for both the doc and the scoped code block. This way the AI agent consolidating fixes knows exactly what values to write — it doesn't need to recompute hashes itself.

Example fix output for a scope hash mismatch:

```
LINK STALE: code changed, doc may need updating
  code file: cmd/search/bm25.go:14
  tag:       [autodoc(f3a1b09c@2fd626aa, a89d6902)]
  doc:       docs/bm25-search.md (id: f3a1b09c)
  current doc hash:   2fd626aa (unchanged)
  current scope hash: b7e3f104 (was a89d6902)
  action: Read the code scope and the doc. If the doc is still accurate,
          update the tag to [autodoc(f3a1b09c@2fd626aa, b7e3f104)].
          If the doc needs updating, update the doc content first,
          then run `autodoc fixed docs/bm25-search.md` to get the new doc hash,
          then update the tag with both new hashes.
```

Error types and their fix instructions:

- **Doc hash mismatch in code tag** (doc updated, code tag stale): Output the current correct doc hash. AI updates the `docHash` in the code tag. No content changes needed.
- **Scope hash mismatch** (code changed, doc may be stale): Output the current correct scope hash. AI reads both artifacts, determines if doc needs updating. If doc is still accurate, AI just updates the scopeHash. If doc is stale, AI updates doc content, runs `autodoc fixed` on the doc, then updates both hashes in the tag.
- **Orphaned tag** (doc ID not found): Flag for manual resolution — the referenced doc no longer exists.
- **Stale doc summary** (preexisting): Same as before — AI updates the summary and title, then runs `autodoc fixed` to recompute the doc hash.

There is no separate `rehash` command. The AI-driven fix handles the "code changed but doc is still correct" case by reading both artifacts, confirming they still match, and updating the code hashes accordingly.

## Resolved Design Questions

1. **Many-to-many**: Multiple code files can reference the same doc, and one code file can have multiple autodoc tags. `rg` is fast enough to grep the whole repo for tag references.

2. **Who writes the initial links**: AI coders write these during the design/implementation stage.

3. **Language-agnostic comment syntax**: The scanner only looks for `[autodoc(...)]` strings. It doesn't need to understand comment syntax. For indentation, it counts leading whitespace characters.

4. **Diff/commit noise**: Acceptable tradeoff. Hash updates will be done in a single commit by the AI coder.

5. **File renames/moves**: Handled by the `id` field in doc frontmatter. Code tags reference the stable ID, not the file path. Renaming a doc doesn't break code references.

6. **Merge conflicts in tags**: Acceptable. We'll see how this goes in practice.

7. **Sidecar file alternative**: Rejected. Inline comments create social pressure to maintain them — they're visible in code review. A sidecar file would rot.
