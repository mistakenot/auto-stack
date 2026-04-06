---
hash: "2fd626aa"
id: "6f55b37d"
summary: "Full-text keyword search over docs using BM25 scoring via Bluge"
title: "BM25 Keyword Search"
---

# BM25 Keyword Search

Full-text keyword search over documentation files using BM25 scoring via [Bluge](https://github.com/blugelabs/bluge). Lets AI agents and humans quickly find relevant docs by keyword without reading every file.

## Library: Bluge

Pure Go (no CGO), compiles into our single binary. Created by the author of Bleve as a cleaner redesign.

- BM25 scoring out of the box
- Disk-based index stored in `.autodoc/index/` (gitignored)
- Incremental update/delete by document ID (file path) — no full rebuild needed
- Built-in ANSI highlighter for CLI snippet output
- ~0.5-2 MB index for 50-200 docs

## Index Fields

| Field | Bluge Type | Indexed | Stored | Source |
|-------|-----------|---------|--------|--------|
| `_id` | (document ID) | — | — | Relative file path, used for upsert/delete |
| `path` | Keyword | no | yes | Relative file path, returned in results |
| `title` | Text | yes | yes | Frontmatter `title` |
| `summary` | Text | yes | yes | Frontmatter `summary` |
| `headings` | Text | yes | yes | Extracted H1-H3 text, joined by newlines |
| `body` | Text | yes | yes | Markdown-stripped plain text, supports highlighting |
| `_all` | Composite | yes | no | Combines title + summary + headings + body for default search |

Short fields (`title`, `summary`) get a natural BM25 boost from length normalization — no explicit boosting config needed.

## Normalization Pipeline

Before indexing, markdown files go through this pipeline:

1. **Strip frontmatter** — already handled by `internal/frontmatter.Parse()`
2. **Parse markdown AST** — using [goldmark](https://github.com/yuin/goldmark)
3. **Extract headings** — walk AST, collect H1-H3 text into a separate `headings` buffer
4. **Strip markdown syntax** — `#` markers, `**`/`*` emphasis, `[text](url)` → `text`, `![alt](url)` → `alt`, `>` blockquote markers, horizontal rules, table pipe `|` chars
5. **Keep code block content** — users search for function names, config keys, CLI flags. Strip only the fence markers (`` ``` ``)
6. **Collapse whitespace** — multiple newlines/spaces → single space

Do NOT apply stemming or stopword removal at this stage. Bluge's default analyzer handles tokenization internally.

### Implementation

Use goldmark to parse into an AST, then walk it with a custom renderer that routes text to two buffers (headings vs body). Roughly 50-80 lines of Go. The walker:

- On `ast.Heading` enter → switch output to headings buffer
- On `ast.Heading` exit → switch back to body buffer, add newline
- On `ast.Text` / `ast.String` → write to current buffer
- On `ast.Link` → children handle writing the link text automatically
- On `ast.Image` → write alt text
- On `ast.FencedCodeBlock` → write code content to body buffer
- Skip `ast.HTMLBlock` / `ast.RawHTML`

## CLI Commands

### `autodoc search reindex`

Rebuilds the full search index from scratch.

1. Walk all doc files matching config (same as `autodoc tree`), skipping files that match `ignores` glob patterns from `docs.json`
2. For each file: parse frontmatter, run normalization pipeline, index into Bluge
3. Store index at `.autodoc/index/`
4. Print count of indexed files

### `autodoc search keyword <query>`

Run a BM25 keyword search against the index.

```bash
autodoc search keyword "authentication setup"
autodoc search keyword "config"
```

Output is JSON array, sorted by score descending:

```json
[
  {
    "score": 2.341,
    "path": "docs/api/auth.md",
    "title": "Authentication",
    "summary": "How to authenticate API requests",
    "snippet": "...configure authentication by setting the API key in your..."
  },
  {
    "score": 1.102,
    "path": "docs/getting-started.md",
    "title": "Getting Started",
    "summary": "Setup instructions for new users",
    "snippet": "...authentication is required before making any API calls..."
  }
]
```

- `score` — BM25 relevance score
- `path` — relative to project root
- `snippet` — matching text fragment from body with ANSI highlights stripped for JSON (highlighted when outputting to terminal)
- Searches against the `_all` composite field by default
- Returns top 10 results

### Integration with `autodoc fixed`

When `autodoc fixed <filepath>` recalculates a file's hash, it should also re-index that single file:

1. Parse and normalize the file
2. Call `writer.Update(filePath, doc)` to upsert in the Bluge index
3. This keeps the index in sync without a full rebuild

## Index Storage

Index lives at `.autodoc/index/` — a directory managed by Bluge containing segment files. Already covered by the `.gitignore` that `autodoc init` creates in `.autodoc/`. The index is cheap to rebuild from scratch so it doesn't need to be committed.

## Bluge API Patterns

Key patterns used:

```go
// Open/create index
config := bluge.DefaultConfig(".autodoc/index")
writer, _ := bluge.OpenWriter(config)

// Upsert a document (file path as ID)
doc := bluge.NewDocument(relPath).
    AddField(bluge.NewKeywordField("path", relPath).StoreValue()).
    AddField(bluge.NewTextField("title", title).StoreValue()).
    AddField(bluge.NewTextField("summary", summary).StoreValue()).
    AddField(bluge.NewTextField("headings", headings).StoreValue()).
    AddField(bluge.NewTextField("body", body).StoreValue().HighlightMatches()).
    AddField(bluge.NewCompositeFieldExcluding("_all", []string{"_id", "path"}))
writer.Update(doc.ID(), doc)

// Search
reader, _ := writer.Reader()
query := bluge.NewMatchQuery("authentication").SetField("_all")
request := bluge.NewTopNSearch(10, query).
    WithStandardAggregations().
    IncludeLocations()
iter, _ := reader.Search(ctx, request)

// Delete (for removed files)
writer.Delete(bluge.Identifier(relPath))
```

## Acceptance Criteria

### `autodoc search reindex`

- [ ] Creates index at `.autodoc/index/` from all doc files found by the configured `docsDir`
- [ ] Skips files matching `ignores` glob patterns from `docs.json` config
- [ ] Strips YAML frontmatter before indexing body content
- [ ] Runs markdown normalization pipeline (strip syntax, keep code block content, collapse whitespace)
- [ ] Indexes separate fields: `path`, `title`, `summary`, `headings`, `body`, `_all` composite
- [ ] Headings field contains extracted H1-H3 text only
- [ ] Prints count of indexed files on completion
- [ ] Running twice overwrites the previous index cleanly (no duplicates, no stale entries)
- [ ] Works on an empty docs directory (creates empty index, prints 0)
- [ ] Fails gracefully if `.autodoc/` doesn't exist (advises running `autodoc init`)

### `autodoc search keyword <query>`

- [ ] Returns JSON array to stdout sorted by BM25 score descending
- [ ] Each result contains `score`, `path`, `title`, `summary`, `snippet`
- [ ] `path` is relative to project root (e.g. `docs/api/auth.md`)
- [ ] `snippet` contains a relevant text fragment from the matching body content
- [ ] Returns top 10 results max
- [ ] Multi-word queries work (e.g. `"authentication setup"` matches docs containing both terms)
- [ ] Single-word queries work and match across all fields (title, summary, headings, body)
- [ ] Returns empty array `[]` when no results match
- [ ] Fails gracefully if index doesn't exist (advises running `autodoc search reindex`)
- [ ] Valid JSON output parseable by `jq` and other tools

### `autodoc fixed` (search integration)

- [ ] After recalculating hash, also upserts the file into the search index
- [ ] Only re-indexes the single file, not the full corpus
- [ ] If the index doesn't exist yet, skip re-indexing silently (don't error)
- [ ] Updated file is immediately findable by `autodoc search keyword`

### Normalization pipeline

- [ ] Strips `#` header markers but preserves header text
- [ ] Strips `**bold**` and `*italic*` markers, keeps inner text
- [ ] Converts `[text](url)` links to just `text`
- [ ] Converts `![alt](url)` images to just `alt`
- [ ] Strips blockquote `>` markers
- [ ] Strips table `|` chars and alignment rows, keeps cell text
- [ ] Keeps code block content, strips only fence markers
- [ ] Collapses multiple whitespace chars to single space
