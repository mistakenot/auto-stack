# (Auto)nomous Coding (Stack)

## Agent Interaction Rules

- ALWAYS use the `AskUserQuestion` tool when asking questions about work. Do not ask questions in plain text.
- Break questions into single numbered items. Never combine multiple questions into one paragraph. Ask one question (or up to 4 via `AskUserQuestion`), wait for the answer, then ask the next if needed.

## Git Worktree Discipline

- ALWAYS run `git fetch origin && git checkout main && git pull origin main` before creating a worktree branch. Worktrees forked from a stale main will cause merge conflicts and divergent history.

## Go Build Discipline

- After writing or modifying a Go file, run `go build ./...` in the relevant module before moving to the next file. Don't accumulate unbuilt files — catch compilation errors immediately.

## Cross-Project Coding Guidance

- Prefer explicit CLI surfaces: one clear command and explicit flags; avoid ambiguous aliases.
- Remove deprecated flags decisively rather than carrying long-term aliases that make behavior unclear.
- Keep default behavior useful: list/read commands should return all items when no filters are provided.
- Default command output to JSON unless a command explicitly documents a different default; provide human-readable text mode via flags where needed.
- In JSON mode, keep `stdout` strictly parseable payload data only; send diagnostics/errors to `stderr`.
- In text mode, print successful results first, then append readable errors and remediation instructions.
- For data-listing commands, return available valid results even when some items are invalid, then exit non-zero if any validation errors occurred.
- Use fail-fast for invalid CLI usage (flag conflicts, bad args) through standard command-framework errors.
- Allow only one filter mode at a time unless combination semantics are explicitly defined and documented.
- Normalize user filter input by default (trim, lowercase, dedupe) and use case-insensitive matching.
- Validate filter values against the same schema rules used for stored data.
- Enforce strict schemas with one shared `validate()` function reused across commands.
- `validate()` should return structured error objects in an array, with fields: `code`, `path`, `field`, `message`, and optional `value`.
- Treat "required" fields as both presence and format constraints, not just non-empty values.
- If constrained identifiers/hashes are used, enforce exact regex formats consistently (for example `^[0-9a-f]{8}$`).
- If controlled tag vocabularies are used, enforce exact tag regex formats consistently (for example `^[a-z0-9]+(?:-[a-z0-9]+)*$`).
- Treat duplicates in normalized sets (for example tags) as validation errors unless dedupe behavior is explicitly specified.
- Treat metadata separately from integrity-critical content: metadata changes should not trigger expensive integrity checks unless explicitly required.
- Keep expensive validation for maintenance commands (for example `fix`/`doctor`), not fast inventory/listing commands.
- When auto-fixing is allowed, keep rewrites deterministic, minimal, and explicitly report each rewritten file.
- For dedupe auto-fixes, preserve original author order unless sorted order is explicitly required.
- Reuse shared discovery/indexing/walk code across commands to keep behavior consistent.
- Every hard error should include a concrete remediation hint (for example, `run <tool> fix`).

## Sub-Projects

| Directory      | Binary        | Status      | Description                                                        |
|----------------|---------------|-------------|--------------------------------------------------------------------|
| `auto-doc/`    | `autodoc`     | Active      | Doc management for AI agents — freshness checking, search, indexing |
| `auto-env/`    | `autoenv`     | Active      | Template-based config generation with per-worktree port allocation  |
| `auto-etl/`    | `autoetl`     | Active      | ETL for coding agent session histories (SSH, LXC, local)           |
| `auto-graph/`  | `autograph`   | Early       | Context graphs built with a coding agent                           |
| `auto-reflect/`| `autoreflect` | Early       | Analyze past sessions, extract rules for future ones               |
| `auto-search/` | `autosearch`  | Early       | Rich search over normalized session history from auto-etl          |
| `auto-skill/`  | `autoskill`   | Early       | Agent skill management                                             |
| `auto-watch/`  | `autowatch`   | Early       | Monitor repo changes, trigger agent prompts in response            |

Each sub-project has its own `CLAUDE.md` with build/test instructions.

## Setup

After cloning, run:

```bash
make install-hooks   # set up pre-commit hooks (format, lint, beads sync)
```

The pre-commit hook auto-formats Go files (`gofmt`), runs `go vet` on all sub-projects, and syncs beads issue state to JSONL.

## Global config folder

Used for settings and data that persist across multiple repositories and projects. The current user journey is authoritative here.

- `~/.auto/settings.json` stores shared host-level defaults such as the current machine hostname / host id. Any tool `init` should create it if missing.
- `~/.auto/{docs,etl,graph,reflect,search,watch,...}/` stores each tool's global settings and data.
- `.auto/{docs,etl,graph,reflect,search,watch,...}/settings.json` stores project-local settings when a tool supports local project configuration.

## Stack

- Golang. Follow standard Golang CLI conventions (e.g. Cobra)

## CLI conventions

Most commands default to JSON output unless the command explicitly documents a different default. Human-readable text mode should be available via flags where needed.

Date filtering convention for commands that support it:
```bash
--since 5m             # 5 minutes ago
--since 5d             # 5 days ago
--since 1w             # 1 week ago
--after 2026-01-01 --before 2026-02-01  # ISO 8601 range
```

These are CLI command patterns that most tools will support:
- `init` global setup
- `init --project` project local setup.
- `quickstart` outputs LLM friendly markdown doc string showing happy-path end to end use case
- `docs` full doc string, all functions the tool can do in one output.
- `doctor` check configuration is ok, any problems we report back as json with detailed explanations of what is wrong.

## Shared data

These tools are designed to work together. The way they do that primarilly is through shared data sources.

**Coding session data**

`auto-etl` takes coding history from multiple tools (codex, claude, etc), transforms it into the standard format, and stores partitioned parquet datasets under `~/.auto/etl/output`
- `~/.auto/etl/raw` contains copies of the raw transcripts, unmodified. Can be from multiple hosts.

`auto-search` then indexes the parquet output from `auto-etl`.

Then `auto-reflect` can use this index to find patterns to use as the basis for new playbook rules, etc.

The tool binaries don't depend on each other. They depend on a common data / file format instead.

## Common data format

The canonical schema is defined in `auto-etl/internal/model/model.go`. Two parquet datasets: `messages` and `sessions`. File contents from read/write/edit tools are stored inline in the message `content` column (parquet is column-oriented, so no benefit from a separate join table).

### Common data transformation pipeline
- `etl` tool accepts loosely typed json formats, which may change over time
- converts them to the denormalised standard format
- is non-destructive, never moves / changes original files, copies them then creates new versions that are modified
- incremental, but has flags to do full transform
- `search` then creates indexes based on the denormalised version, embeds blobs, etc.
- this unlocks full text search + advanced analytics
- `watch` triggers a reflect job
- `reflect` can then use this full text search to look for patterns, suggest improvements, etc.
- `skill` can optionally turn the reflect outputs into new agent skills

### Data scale
- 6 months of claude usage has produced 1gb of log files
- But 90% of this is Read/Write/Edit noise
- We dont want to lose that data
- But also want search / indexing to be fast
- So the canonical parquet datasets should preserve the content we need for reconstruction and analysis
- And we can derive truncated views such as `content_truncated` / `transcript_truncated` for search and LLM-friendly outputs
- Any future secondary blob storage must be an optimization layer, not a replacement for the canonical journey format

Idea:
Use claud read tools to build heat maps of what files it's reading a lot, what docs it's reading a lot etc. Also for writes like what files were being edited multiple times during a workflow which usually indicates a problem.

<!-- br-agent-instructions-v1 -->

---

## Beads Workflow Integration

This project uses [beads_rust](https://github.com/Dicklesworthstone/beads_rust) (`br`/`bd`) for issue tracking. Issues are stored in `.beads/` and tracked in git.

### Essential Commands

```bash
# View ready issues (open, unblocked, not deferred)
br ready              # or: bd ready

# List and search
br list --status=open # All open issues
br show <id>          # Full issue details with dependencies
br search "keyword"   # Full-text search

# Create and update
br create --title="..." --description="..." --type=task --priority=2
br update <id> --status=in_progress
br close <id> --reason="Completed"
br close <id1> <id2>  # Close multiple issues at once

# Sync with git
br sync --flush-only  # Export DB to JSONL
br sync --status      # Check sync status
```

### Workflow Pattern

1. **Start**: Run `br ready` to find actionable work
2. **Claim**: Use `br update <id> --status=in_progress`
3. **Work**: Implement the task
4. **Complete**: Use `br close <id>`
5. **Sync**: Always run `br sync --flush-only` at session end

### Key Concepts

- **Dependencies**: Issues can block other issues. `br ready` shows only open, unblocked work.
- **Priority**: P0=critical, P1=high, P2=medium, P3=low, P4=backlog (use numbers 0-4, not words)
- **Types**: task, bug, feature, epic, chore, docs, question
- **Blocking**: `br dep add <issue> <depends-on>` to add dependencies

### Session Protocol

**Before ending any session, run this checklist:**

```bash
git status              # Check what changed
git add <files>         # Stage code changes
br sync --flush-only    # Export beads changes to JSONL
git commit -m "..."     # Commit everything
git push                # Push to remote
```

### Best Practices

- Check `br ready` at session start to find available work
- Update status as you work (in_progress → closed)
- Create new issues with `br create` when you discover tasks
- Use descriptive titles and set appropriate priority/type
- Always sync before ending session

<!-- end-br-agent-instructions -->

<!-- autodoc: start -->
## Documentation Index

*Auto-generated by `autodoc`. Do not edit manually.*

Use `autodoc` to search and maintain docs.

### Search

Use `autodoc search keyword <query>` to find relevant docs by keyword (BM25 full-text search).

```
autodoc search keyword "auth middleware protection"
```

Run `autodoc search reindex` to rebuild the search index after adding or changing docs.

### Linking Code to Docs

Add an `[autodoc()]` comment above any code block that implements or depends on a doc.
This creates a two-way freshness link — `autodoc fix` will warn when either the doc or the code drifts.

```
// [autodoc(<docId>@<docHash>, <scopeHash>)]
```

- `docId` and `docHash` come from the doc's frontmatter (`id` and `hash` fields).
- `scopeHash` is computed automatically by `autodoc fixed` from the code below the tag.
- Place the comment on the line directly above the relevant function, struct, or block.
- Run `autodoc fix` to validate all links and get instructions for updating stale hashes.

**When to add one:** whenever code implements behavior described in a doc, so that future changes to either side surface as staleness.

**auto-config/docs**

- [AutoConfig Requirements](auto-config/docs/requirements.md): Requirements for autoconfig: validate and manage Claude/Codex agent configuration, set session names, and provide utility functions for coding agent environments. Read when: validating coding agent configuration or setting up development environments

**auto-web/docs**

- [Autoweb Requirements](auto-web/docs/requirements.md): Requirements for autoweb, a safe web research portal for AI coding agents with pluggable backends and result deduplication. Read when: designing safe web research portals for coding agents

**docs**

- [auto-img Research: Context-Protective Image Access for Coding Agents](docs/auto-img-research.md): Research on optimising image storage and retrieval for AI coding agents, covering token costs, progressive disclosure, and S3 patterns. Read when: designing image storage or progressive disclosure patterns
- [Auto Package Patterns](docs/auto-package-patterns.md): Reference patterns and conventions shared across all auto-* packages in the auto-stack monorepo. Used as the blueprint when creating new packages. Read when: creating a new package in the auto-stack monorepo
- [autostack install-daemon](docs/autostack-install-daemon.md): Design and implementation spec for installing and managing the autowatch daemon as a system systemd service running as a non-root user. Read when: implementing daemon installation or systemd service management
- [Doc File Usage in Agent Sessions: Findings and Structural Insights](docs/doc-file-usage-findings.md): Analysis of how agents interact with documentation files across 420 coding sessions, revealing that docs are seen constantly but read rarely, discovery bypasses the tooling, and user direction is the primary driver of doc consumption. Read when: analyzing doc discovery patterns or improving doc tooling
- [End-to-End Problems: autosearch session get Rendering](docs/end-to-end-problems.md): Identified rendering problems in autosearch session output, including missing closing tags, absent tool command previews, empty tool-use blocks, and message truncation. Read when: debugging autosearch session rendering or ETL data flow
- [Auto — Agentic Coding Intelligence Platform](docs/random.md): High-level product overview of the Auto platform: architecture, data format, tool suite, query examples, security model, and roadmap. Read when: learning the auto-stack architecture and product vision
- [Signals](docs/signals.md): Exploration of how raw coding session data can be transformed into actionable signals indicating what is working well or poorly in a codebase. Read when: designing metrics or feedback signals from session data
- [Review: user-journey.md](docs/user-journey.claude.md): Open questions and action items from reviewing the auto-stack user journey document for consistency and end-to-end coherence. Read when: reviewing user-journey open questions or implementation roadmap
- [User Journey Consistency Review](docs/user-journey.codex.md): Consistency review of the auto-stack user journey, confirming directional decisions and capturing open planning-stage questions for future resolution. Read when: confirming auto-stack direction or resolving planning questions
- [Auto Stack User Journey](docs/user-journey.md): End-to-end walkthrough of the Auto stack: from doc management and session ETL through search, reflection, and automated task scheduling. Read when: understanding the end-to-end auto-stack workflow and architecture

**docs/reference**

- [Claude Code Project Files Schema](docs/reference/claude-project-files-schema.md): Reference for the on-disk JSONL file format produced by Claude Code sessions, covering directory structure, line types, content blocks, token usage, subagent files, and tool-results directories. Read when: parsing Claude Code session files or understanding ETL data

**docs/research**

- [Research: Blogs](docs/research/blogs.md): Collected blog and reference links relevant to the auto-stack research and development process. Read when: researching external references on agent engineering or tooling
- [Research: Agent Engineering Principles (Tweets)](docs/research/tweets.md): Curated research notes on agent engineering principles covering progressive disclosure, worktree isolation, spec-first development, architecture enforcement, and integrated feedback loops. Read when: understanding core agent engineering principles
<!-- autodoc: end -->




































**autosearch** — Search past coding agent sessions. Run `autosearch quickstart` to learn more.

**autoskill** — Author and lint reusable agent skills. Run `autoskill quickstart` to learn more.
