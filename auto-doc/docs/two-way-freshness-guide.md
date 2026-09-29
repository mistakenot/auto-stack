---
hash: "cd14ff12"
id: "6719b91e"
read_when: "using two-way freshness links to keep documentation and code synchronized"
summary: "Walkthrough of the full lifecycle for keeping docs and code in sync using autodoc two-way freshness links"
title: "Two-Way Freshness End-to-End Guide"
---

# Two-Way Freshness: End-to-End Guide

This guide walks through the full lifecycle of keeping documentation and code in sync using autodoc's two-way freshness feature. It shows how links are created, how staleness is detected, and how fixes are applied.

## The Problem

Documentation drifts from code. Code changes but nobody updates the docs. Docs get rewritten but the code comments still reference the old version. Without automation, this is invisible until someone hits a wrong answer.

Two-way freshness makes drift **visible and fixable** by embedding lightweight hash-based links between code and docs.

## Concepts

- **Doc ID** — a stable 8-char hex identifier in each doc's frontmatter. Immune to file renames.
- **Doc hash** — computed from doc content and frontmatter (excluding `hash` and `id`). Changes when the doc changes.
- **Scope hash** — computed from the indentation-scoped code block below an `[autodoc()]` tag. Changes when the code changes.
- **Autodoc tag** — `[autodoc(docId@docHash, scopeHash)]` embedded in a code comment. Links a code location to a doc.

## Walkthrough

### 1. You have a doc

Say you maintain a doc explaining your project's caching layer:

```markdown
---
id: "e4b7a21f"
hash: "9c3d0e5a"
summary: "How the in-memory LRU cache works, including eviction policy and TTL"
title: "Caching Layer"
---

# Caching Layer

The service uses an in-memory LRU cache with a configurable max size and per-entry TTL.

## Eviction Policy

When the cache reaches `maxEntries`, the least-recently-used entry is evicted. Entries
are also removed when their TTL expires, checked lazily on access.

## Configuration

| Key            | Default | Description              |
|----------------|---------|--------------------------|
| `maxEntries`   | `1000`  | Maximum number of entries |
| `ttlSeconds`   | `300`   | Time-to-live per entry   |
```

File: `docs/caching.md`

### 2. You link it from code

The cache is implemented in `pkg/cache/lru.go`. Place the autodoc tag inside the main struct or function, after the opening brace:

```go
package cache

import (
	"sync"
	"time"
)

// LRUCache is the core caching structure.
type LRUCache struct {
	// docs/caching.md — caching layer design and eviction policy
	// [autodoc(e4b7a21f@9c3d0e5a, 7f2a1b8e)]

	mu         sync.RWMutex
	entries    map[string]*entry
	order      *list
	maxEntries int
	ttl        time.Duration
}

func (c *LRUCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Since(e.created) > c.ttl {
		go c.evict(key)
		return nil, false
	}
	c.order.moveToFront(e.node)
	return e.value, true
}

func (c *LRUCache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.maxEntries {
		c.evictOldest()
	}
	// ...
}
```

In this example:
- `e4b7a21f` is the doc's `id`
- `9c3d0e5a` is the doc's current `hash`
- `7f2a1b8e` is the scope hash — computed from all lines inside `LRUCache struct { ... }` at that indentation level or deeper, with the `[autodoc()]` string stripped out

The human-readable comment `// docs/caching.md — caching layer design and eviction policy` is optional. It's not parsed — just helpful for someone reading the code.

### 3. Everything is in sync

Running `autodoc fix` at this point produces no link errors — just the usual doc summary checks. Both hashes match: the doc content matches its hash, and the code scope matches its scope hash.

### 4. Someone changes the code

A developer adds a `maxBytes` limit to the cache:

```go
type LRUCache struct {
	// docs/caching.md — caching layer design and eviction policy
	// [autodoc(e4b7a21f@9c3d0e5a, 7f2a1b8e)]

	mu         sync.RWMutex
	entries    map[string]*entry
	order      *list
	maxEntries int
	maxBytes   int64        // <-- new field
	curBytes   int64        // <-- new field
	ttl        time.Duration
}
```

Now `autodoc fix` detects the drift. The scope hash changed because the struct body is different. The doc hash is unchanged — the doc hasn't been touched.

### 5. Fix it

`autodoc fix` outputs instructions for an AI agent, including the current correct hashes:

```
LINK STALE: code changed, doc may need updating
  code file: pkg/cache/lru.go:11
  tag:       [autodoc(e4b7a21f@9c3d0e5a, 7f2a1b8e)]
  doc:       docs/caching.md (id: e4b7a21f)
  current doc hash:   9c3d0e5a (unchanged)
  current scope hash: d4e8f23c (was 7f2a1b8e)
  action: Read the code scope and the doc. If the doc is still accurate,
          update the tag to [autodoc(e4b7a21f@9c3d0e5a, d4e8f23c)].
          If the doc needs updating, update the doc content first,
          then run `autodoc fixed docs/caching.md` to get the new doc hash,
          then update the tag with both new hashes.
```

The AI reads the code, sees `maxBytes` and `curBytes` were added, reads the doc, notices the Configuration table doesn't mention byte limits. It:

1. Updates `docs/caching.md` to add `maxBytes` to the configuration table and describe byte-based eviction
2. Runs `autodoc fixed docs/caching.md` → new hash is `b1c4e7a3`
3. Updates the code tag: `[autodoc(e4b7a21f@b1c4e7a3, d4e8f23c)]`

Now both the doc content and code tag are current.

### 6. Someone changes the doc

Later, a tech writer improves the doc's eviction policy section — better wording, same meaning. The doc hash changes but the code hasn't changed.

`autodoc fix` reports:

```
LINK STALE: doc updated, code tag needs refresh
  code file: pkg/cache/lru.go:11
  tag:       [autodoc(e4b7a21f@b1c4e7a3, d4e8f23c)]
  doc:       docs/caching.md (id: e4b7a21f)
  current doc hash:   f5a2d891 (was b1c4e7a3)
  current scope hash: d4e8f23c (unchanged)
  action: Update the docHash in the code tag to f5a2d891.
```

This is a simple fix — the code hasn't changed, so no content review needed. The AI just updates the doc hash in the tag:

```go
// [autodoc(e4b7a21f@f5a2d891, d4e8f23c)]
```

### 7. Code changes but the doc is still correct

A developer renames a local variable or reformats the file. The scope hash changes, but the doc's content is still accurate.

The AI reads both artifacts, confirms they still match, and simply updates the scope hash in the tag. No doc edits needed.

## Multiple Tags in One File

A file can have multiple autodoc tags at different scopes. Each tag hashes only its own scope:

```go
package handlers

// [autodoc(a1b2c3d4@..., ...)]  ← scopes to everything in this file (column 0 = EOF)

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	// [autodoc(e5f6a7b8@..., ...)]  ← scopes to just this function body
	// ...
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	// [autodoc(c9d0e1f2@..., ...)]  ← scopes to just this function body
	// ...
}
```

Changing `HandleLogin` only triggers staleness for the `e5f6a7b8` tag — the `HandleLogout` tag stays clean. The file-level tag (`a1b2c3d4`) would also trigger since it covers everything.

## One Doc, Many Code Files

A doc can be referenced from multiple code files. When the doc changes, `autodoc fix` finds all references via `git ls-files | rg` and reports each one:

```
LINK STALE: doc updated, code tag needs refresh
  code file: pkg/cache/lru.go:11
  ...
LINK STALE: doc updated, code tag needs refresh
  code file: pkg/cache/lru_test.go:8
  ...
LINK STALE: doc updated, code tag needs refresh
  code file: cmd/server/main.go:42
  ...
```

All three tags need their doc hash updated.

## One Doc, One Folder: `.autodoc` Folder Links

When several files in the same folder link to the same doc, each tag needs its own refresh: one doc edit or a sweep of edits across the package produces a diff touching every file just to bump hashes. A **folder link file** collapses them into one line.

Put a file named `.autodoc` in the folder, one tag per line (`#` comments allowed):

```
# pkg/cache/.autodoc — caching layer design
[autodoc(e4b7a21f@9c3d0e5a, 3c9d1e0f)]
```

Its scope is **every code file in the folder's subtree** (recursive): git-tracked plus untracked-but-not-ignored files, with the same exclusions as the tag scanner (`.md`/`.json`/`.yaml` and other data files, `_test.go`, `vendor/`, `testdata/`, `node_modules/`, …) and excluding other `.autodoc` files. The scope hash covers each file's path and content, with inline tags stripped, so adding, removing, renaming or editing any covered file marks the link stale — and only the one line in `.autodoc` needs refreshing.

```
LINK STALE: files under a folder link changed, doc may need updating
  location:  pkg/cache/.autodoc:2
  tag:       [autodoc(e4b7a21f@9c3d0e5a, 3c9d1e0f)]
  scope:     every code file under pkg/cache/ (recursive)
  ...
  action: Review the changes under pkg/cache/ against the doc (e.g. git diff -- pkg/cache/).
          If the doc needs updating, edit it and run `auto doc fixed <docPath>`.
          Then run `auto doc fixed pkg/cache/.autodoc` to refresh every tag in the file.
```

`auto doc fixed <folder>/.autodoc` rewrites every tag in the file to the current doc hash and folder scope hash — no hand-editing hashes. Running it is the acknowledgement that the docs were reviewed.

Guidance:

- Place `.autodoc` in the **lowest folder that covers the files** — a high-level `.autodoc` goes stale on almost any edit beneath it.
- Nested `.autodoc` files are independent: files under a child folder count toward both the child's and the parent's scope.
- Inline tags and folder links may coexist; keep an inline tag when a single function needs its own, tighter link.

### Migrating inline tags to a folder link

`auto doc fix` lists folders where two or more files carry inline tags to the same doc under **Folder Link Suggestions** (`folder_link_suggestion` in `--json`). To migrate one:

1. Create `<folder>/.autodoc` containing `[autodoc(<docId>@00000000, 00000000)]`.
2. Delete the inline tags for that doc from the files under the folder.
3. Run `auto doc fixed <folder>/.autodoc` to write the real hashes.
4. Run `auto doc fix` to confirm the folder is clean and no longer suggested.

## Quick Reference

| Scenario | What changes | What `fix` reports | Fix action |
|----------|-------------|-------------------|------------|
| Code edited | scopeHash mismatch | "code changed, doc may need updating" | AI reads both, updates doc if needed, updates scopeHash |
| Doc edited | docHash mismatch in code tag | "doc updated, code tag needs refresh" | Update docHash in code tag |
| Doc deleted | Doc ID not found | "orphaned tag" | Manual resolution |
| Code reformatted | scopeHash mismatch | "code changed, doc may need updating" | AI confirms doc is still correct, updates scopeHash |
| Doc renamed | Nothing (ID is stable) | Nothing | No action needed |
| File under a `.autodoc` folder added/edited/renamed | folder scopeHash mismatch | "files under a folder link changed" | Review folder diff vs doc, then `auto doc fixed <folder>/.autodoc` |
| Doc linked by a `.autodoc` edited | docHash mismatch in folder link | "doc updated, folder link needs refresh" | `auto doc fixed <folder>/.autodoc` |
| 2+ files in a folder tag the same doc | — | "Folder Link Suggestions" (advisory) | Migrate to one `.autodoc` |
