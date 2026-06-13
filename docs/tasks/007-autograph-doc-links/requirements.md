---
hash: "b6361ace"
id: "1e8dd1dd"
read_when: "implementing doc-link nodes and edges in autograph or extending autograph to surface documentation alongside code"
summary: "Requirements for including autodoc-linked documentation as nodes and edges in autograph code graphs and context bundles, with opt-out flag, zero-config behavior, and support for both JSON/DOT/Mermaid graph formats."
title: "Requirements: Task 007 — Autograph Doc Links"
---

# Task 007: Autograph Doc Links

## Problem

Autograph builds code graphs and context bundles from import relationships, but ignores documentation linked to code files via autodoc's `[autodoc()]` tags. When a developer requests a graph or context bundle for a file, they get the code dependency picture but miss relevant documentation that's been explicitly linked to those files. This means agents and developers lose context that was deliberately connected.

## Goals

- Include autodoc-linked documentation as nodes and edges in the code graph
- Include linked doc files in context bundles alongside code dependencies
- Make doc-linking opt-out (enabled by default) so existing workflows get richer context without config changes
- Keep the feature zero-config when autodoc tags exist in the repo, and invisible when they don't

## Acceptance Criteria

**AC-1**: Doc nodes appear in code graph output
- Given: A project with `[autodoc()]` tags in source files
- When: Running `autograph code graph <dir>`
- Then: The graph includes `"doc"` nodes for linked doc files and `"doc_link"` edges from code files to their linked docs, in all output formats (JSON, DOT, Mermaid)

**AC-2**: Doc links are excluded with a flag
- Given: A project with `[autodoc()]` tags in source files
- When: Running `autograph code graph <dir> --no-docs`
- Then: The graph contains only `"file"` nodes and `"import"` edges (current behavior)

**AC-3**: Context bundle includes linked docs
- Given: A project with `[autodoc()]` tags in seed files or their dependencies
- When: Running `autograph code context <dir> --file <seed> --token-limit <N>`
- Then: Linked doc files are included in the pack output (markdown and JSON), with their content, and count against the token budget

**AC-4**: Doc priority in context budget
- Given: A context bundle build where linked docs compete for budget with code dependencies
- When: The builder selects candidates
- Then: Docs linked to seed files get higher priority than docs linked to non-seed dependencies

**AC-5**: Context bundle excludes docs with a flag
- Given: A project with `[autodoc()]` tags
- When: Running `autograph code context <dir> --file <seed> --token-limit <N> --no-docs`
- Then: The context bundle contains only code files (current behavior)

**AC-6**: Graceful when no tags exist
- Given: A project with no `[autodoc()]` tags in any source files
- When: Running either `code graph` or `code context`
- Then: Output is identical to current behavior; no errors, no empty doc sections

## Out of Scope

- Freshness checking of doc links (stale hash detection) -- that's autodoc's responsibility
- Scanning for doc links in markdown files (doc-to-doc links); only code-to-doc links
- Adding autodoc as a runtime binary dependency (autograph imports autodoc Go packages at build time, not as a CLI dependency)
- Modifying autodoc's tag format or behavior
- Doc link support for languages other than TypeScript and Go (follow existing language support)

## Resolved Questions

- [x] Q1: Integration approach -- autograph will import autodoc's `linkscan` and `doctree` packages as a Go module dependency. This keeps the tag scanning logic DRY and ensures format consistency. Adds a build-time coupling but avoids duplicating frontmatter parsing.
- [x] Q2: Priority tiers -- docs linked to seed files get Priority 15 (between direct-runtime-deps at 10 and direct-runtime-dependents at 20). Docs linked to non-seed included files get Priority 35 (at the cycle-members tier).
- [x] Q3: Rendering -- doc files are interleaved with code files in the existing "Files" section, ordered by priority like any other candidate. Doc entries use `role: "doc"` to distinguish them from code files.
