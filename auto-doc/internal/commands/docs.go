package commands

import (
	"fmt"
	"io"
)

// Docs writes the complete command reference to w.
func Docs(w io.Writer) {
	fmt.Fprint(w, docsReference)
}

const docsReference = `# auto doc — Complete Command Reference

## Global Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| ` + "`--json`" + ` | bool | false | Output in JSON format. In JSON mode, stdout is strictly parseable JSON; diagnostics go to stderr. |

---

## ` + "`auto doc init`" + `

Initialize global auto doc configuration.

**Creates:**
- ` + "`~/.auto/doc/settings.json`" + ` — global config (supports ` + "`ignores`" + ` array)
- ` + "`~/.auto/host.json`" + ` — host identification (` + "`{\"hostId\": \"<hostname>\"}`" + `)

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| ` + "`--project`" + ` | bool | false | Initialize project-local config instead of global |

**With ` + "`--project`" + `:**
- Creates ` + "`.auto/doc/settings.json`" + ` with defaults (docsDir, agentFiles, parallelism, ignores)
- Creates ` + "`.auto/doc/.gitignore`" + ` (tracks only settings.json)
- Creates ` + "`docs/`" + ` directory if missing
- Runs tree output and advises ` + "`auto doc fix`" + ` if stale files found
- Also runs global init if ` + "`~/.auto/doc/settings.json`" + ` doesn't exist

**Idempotent:** Running twice does not overwrite existing configs.

**Exit codes:** 0 on success.

---

## ` + "`auto doc tree`" + `

Pretty-print all discovered doc files with title and summary in a unified repo-root tree.

**Discovery:** Recursively finds all directories named ` + "`docs`" + ` in the repo, plus the configured ` + "`docsDir`" + ` as a compatibility root. Git submodules are excluded.

**Text output:** ASCII tree with title (quoted) and summary per file, followed by a count summary line (e.g. "14 docs, 3 stale").

**JSON output (` + "`--json`" + `):**
` + "```json" + `
[
  {"path": "docs/auth.md", "id": "abc12345", "title": "Auth", "summary": "Auth guide", "hash": "deadbeef"}
]
` + "```" + `

**Exit codes:** 0 always.

---

## ` + "`auto doc stale`" + `

List files where the hash doesn't match content, or files missing required frontmatter fields.

**Text output:** Same tree as ` + "`tree`" + `, but stale files show "Stale" in red instead of summary. Error count and remediation hint printed to stderr.

**JSON output (` + "`--json`" + `):**
` + "```json" + `
[
  {"path": "docs/old.md", "title": "Old", "summary": "", "hash": "wrong", "issues": ["missing_frontmatter", "stale_hash"]}
]
` + "```" + `

**Issue types:** ` + "`missing_frontmatter`" + `, ` + "`stale_hash`" + `, ` + "`default_title`" + `

**Exit codes:** 0 = no stale files, 1 = stale files found.

---

## ` + "`auto doc fix`" + `

Scan for all documentation and code-link issues, output instructions for an AI agent to fix them.

**What it checks:**
- Missing frontmatter (no title/summary/hash)
- Stale hash (content changed since last ` + "`auto doc fixed`" + `)
- Default/empty title
- ` + "`[autodoc(...)]`" + ` code tag issues: doc hash mismatch, scope hash mismatch, both mismatch, orphaned tags, malformed tags

**Behavior:**
- Auto-assigns 8-char hex doc IDs to files missing them
- Groups doc issues into parallel batches (based on ` + "`parallelism`" + ` config)
- Outputs step-by-step markdown instructions per file

**JSON output (` + "`--json`" + `):**
` + "```json" + `
[
  {"type": "stale_hash", "path": "docs/auth.md", "details": "Hash does not match content"},
  {"type": "orphaned_tag", "path": "src/main.go", "details": "Tag at line 10: doc=deadbeef@cafebabe scope=12345678"}
]
` + "```" + `

**Exit codes:** 0 on success (even with issues), non-zero if malformed tags found.

---

## ` + "`auto doc fixed <filepath>`" + `

Recalculate and write the hash for a single doc file. Also updates the search index if one exists.

**Hash algorithm:** Sort frontmatter keys alphabetically (excluding ` + "`hash`" + ` and ` + "`id`" + `), concatenate values, append body, take first 8 chars of MD5 hex digest.

**JSON output (` + "`--json`" + `):**
` + "```json" + `
{"path": "docs/auth.md", "oldHash": "aabbccdd", "newHash": "11223344"}
` + "```" + `

**Exit codes:** 0 on success.

---

## ` + "`auto doc agents`" + `

Insert auto-generated documentation indexes into agent memory files (AGENTS.md, CLAUDE.md).

**Behavior:**
- Each doc is assigned to the nearest ancestor directory containing a configured agent file
- If both AGENTS.md and CLAUDE.md exist at that level, both are updated
- Content is placed between ` + "`<!-- autodoc: start -->`" + ` / ` + "`<!-- autodoc: end -->`" + ` markers
- If no markers exist, block is appended

**JSON output (` + "`--json`" + `):** Array of updated file paths.

**Exit codes:** 0 on success.

---

## ` + "`auto doc search reindex`" + `

Build or rebuild the full-text BM25 search index from all discovered docs.

**Index location:** ` + "`.auto/doc/index/`" + `

**Indexed fields:** path, title, summary, headings (H1-H3), body (markdown-stripped).

**Behavior:** Removes stale entries not in current discovery set. Idempotent.

**Exit codes:** 0 on success.

---

## ` + "`auto doc search keyword <query>`" + `

Run a BM25 keyword search against the index. Always returns JSON.

**Output:**
` + "```json" + `
[
  {"score": 2.34, "path": "docs/auth.md", "title": "Auth", "summary": "Auth guide", "snippet": "...matching text..."}
]
` + "```" + `

**Limit:** Top 10 results by relevance score.

**Exit codes:** 0 on success.

---

## ` + "`auto doc doctor`" + `

Check configuration health and report problems with remediation hints.

**Checks:**
1. Global config (` + "`~/.auto/doc/settings.json`" + `)
2. Host config (` + "`~/.auto/host.json`" + `)
3. Project config (` + "`.auto/doc/settings.json`" + `) — validates JSON syntax
4. Docs directory exists
5. Search index exists

**Text output:** Pass/fail per check with remediation hint on failure.

**JSON output (` + "`--json`" + `):**
` + "```json" + `
[
  {"check": "global_config", "status": "pass", "message": "/home/user/.auto/doc/settings.json"},
  {"check": "search_index", "status": "fail", "message": ".auto/doc/index not found. Run ` + "`" + `auto doc search reindex` + "`" + ` to create it."}
]
` + "```" + `

**Exit codes:** 0 = all pass, 1 = any check fails.

---

## ` + "`auto doc quickstart`" + `

Output a concise end-to-end tutorial with examples. See ` + "`auto doc quickstart`" + ` for content.

---

## ` + "`auto doc docs`" + `

Output this complete command reference.

---

## Configuration

**Project config:** ` + "`.auto/doc/settings.json`" + `

` + "```json" + `
{
  "docsDir": "./docs",
  "agentFiles": ["AGENTS.md", "CLAUDE.md"],
  "parallelism": 4,
  "ignores": ["draft-*.md"]
}
` + "```" + `

**Global config:** ` + "`~/.auto/doc/settings.json`" + `

` + "```json" + `
{
  "ignores": ["vendor/**"]
}
` + "```" + `

Global ` + "`ignores`" + ` are unioned with project-local ` + "`ignores`" + `.

## Frontmatter Format

` + "```yaml" + `
---
id: "a1b2c3d4"
title: "Document Title"
summary: "One-line summary"
hash: "a1b2c3d4"
---
` + "```" + `

- ` + "`id`" + ` — 8-char hex, auto-assigned by ` + "`fix`" + `
- ` + "`title`" + ` — required, human-readable
- ` + "`summary`" + ` — required, one-line
- ` + "`hash`" + ` — 8-char hex, managed by ` + "`fixed`" + `

## Code-Doc Linking

` + "```" + `
// [autodoc` + `(docId@docHash, scopeHash)]
` + "```" + `

Two-way freshness: ` + "`fix`" + ` detects when doc or code changes, reports the specific drift type, and provides exact remediation steps.
`
