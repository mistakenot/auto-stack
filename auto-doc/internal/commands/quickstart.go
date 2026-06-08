package commands

import (
	"fmt"
	"io"
)

// Quickstart writes a comprehensive usage guide to w.
func Quickstart(w io.Writer) {
	fmt.Fprint(w, quickstartDoc)
}

const quickstartDoc = `# auto doc — Documentation Management for AI Agents

Quick reference for all ` + "`auto doc`" + ` commands. Run ` + "`auto doc --help`" + ` for full details.

## Setup

### ` + "`auto doc init`" + `
Initialize a project for autodoc. Creates ` + "`.auto/doc/settings.json`" + ` config and ` + "`docs/`" + ` directory.

` + "```" + `
auto doc init
` + "```" + `

## Viewing Documentation

### ` + "`auto doc tree`" + `
Pretty-print discovered doc files with title and summary in a single repo-root tree.
Discovery scans recursively for directories named ` + "`docs`" + `, then recursively includes markdown files under each.
Configured ` + "`docsDir`" + ` is still included as a compatibility root even if it is not named ` + "`docs`" + `.

` + "```" + `
auto doc tree
` + "```" + `

Output:
` + "```" + `
./
├── docs/
│   └── getting-started.md — "Getting Started" — Setup instructions for new users
└── services/
    └── payments/
        └── docs/
            └── auth.md — "Authentication" — How to authenticate API requests
` + "```" + `

## Checking & Fixing

### ` + "`auto doc stale`" + `
List files where the hash doesn't match content, or files missing frontmatter.
Exit code 0 = all clean, 1 = stale files found.
Uses the same recursive discovery set and unified repo-root tree output as ` + "`auto doc tree`" + `.

` + "```" + `
auto doc stale
` + "```" + `

### ` + "`auto doc fix`" + `
Output instructions for an AI agent to fix all documentation issues (missing frontmatter, stale hashes, default titles).

` + "```" + `
auto doc fix
` + "```" + `

### ` + "`auto doc fixed <filepath>`" + `
Recalculate and write the hash for a single doc file. Also updates the search index if one exists.

` + "```" + `
auto doc fixed docs/api/auth.md
` + "```" + `

## Agent Integration

### ` + "`auto doc agents`" + `
Insert documentation indexes into agent memory files using marker comments for idempotent updates.
Each discovered doc is assigned to the nearest ancestor directory that contains configured agent files.
If both ` + "`AGENTS.md`" + ` and ` + "`CLAUDE.md`" + ` exist at that level (including symlinked pairs), both get the generated index block.
If no ancestor owner exists, ` + "`auto doc agents`" + ` updates existing root agent files or creates root ` + "`AGENTS.md`" + `.

` + "```" + `
auto doc agents
` + "```" + `

## Search

### ` + "`auto doc search reindex`" + `
Build or rebuild the full-text search index from all recursively discovered docs.
Indexed paths are repo-relative and stale index entries are removed on reindex.
Index is stored at ` + "`.auto/doc/index/`" + `.

Note: docs inside git submodules are excluded by default.

` + "```" + `
auto doc search reindex
` + "```" + `

### ` + "`auto doc search keyword <query>`" + `
Run a BM25 keyword search. Returns JSON array sorted by relevance score.

` + "```" + `
auto doc search keyword "authentication setup"
auto doc search keyword "config"
auto doc search keyword "database sql schema"
auto doc search keyword "router middleware protection"
auto doc search keyword "getting started installation"
` + "```" + `

Output:
` + "```json" + `
[
  {
    "score": 2.34,
    "path": "docs/api/auth.md",
    "title": "Authentication",
    "summary": "How to authenticate API requests",
    "snippet": "...configure authentication by setting the API key..."
  }
]
` + "```" + `

## Typical Workflow

` + "```" + `
auto doc init                              # 1. Initialize project
auto doc fix                               # 2. Get fix instructions
auto doc fixed docs/getting-started.md     # 3. Fix individual files
auto doc stale                             # 4. Verify all clean
auto doc agents                            # 5. Update agent files
auto doc search reindex                    # 6. Build search index
auto doc search keyword "auth setup"       # 7. Search docs
` + "```" + `
`
