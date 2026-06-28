# (Auto)nomous Coding (Stack)

## Agent Interaction Rules

- ALWAYS use the `AskUserQuestion` tool when asking questions about work. Do not ask questions in plain text.
- Break questions into single numbered items. Never combine multiple questions into one paragraph. Ask one question (or up to 4 via `AskUserQuestion`), wait for the answer, then ask the next if needed.
- Speak the project's ubiquitous language: see @docs/concepts/UBIQUITOUS_LANGUAGE.md — use the canonical term for each concept and flag any word on an _Avoid_ list.

## Git Worktree Discipline

- We use branches/worktrees when running via the explicit planning workflow (new-task → execute-task etc.); otherwise assume you can do your work directly on `main` unless specifically instructed otherwise.
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

### Verification

- For E2E testing, pick a stable data set and create test harnesses that populate it on disk, then run the tools as a user would.
- You can use git history from this repo, or create mock code bases as fixtures checked into code.
- If you create mock data on a per-test basis, do it where it wont get checked into git, and clean up after the test.

### API design for CLI tools

- Consider building in two layers: a. low level primitives for flexibility b. higher level functional endpoints for ease of use.
- Be explicit on input validation, with clear error messsages that say a. what's wrong an b. suggest how to resolve it
- Pick the default response format that makes sense for the standard use case (often json, sometimes not)

## Sub-Projects

All tools ship as subcommands of a single `auto` binary.

| Directory       | Command        | Status      | Description                                                        |
|----------------|----------------|-------------|--------------------------------------------------------------------|
| `auto-artifact/`| `auto artifact`| Active      | Upload evidence files to public-read S3, get permanent URLs for PRs |
| `auto-doc/`    | `auto doc`     | Active      | Doc management for AI agents — freshness checking, search, indexing |
| `auto-env/`    | `auto env`     | Active      | Template-based config generation with per-worktree port allocation  |
| `auto-etl/`    | `auto etl`     | Active      | ETL for coding agent session histories (SSH, LXC, local)           |
| `auto-graph/`  | `auto graph`   | Active      | Code context graphs — file-level import graph with ast-grep scanning |
| `auto-reflect/`| `auto reflect` | Early       | Analyze past sessions, extract rules for future ones               |
| `auto-search/` | `auto search`  | Early       | Rich search over normalized session history from auto-etl          |
| `auto-skill/`  | `auto skill`   | Early       | Agent skill management                                             |
| `auto-ui/`     | `auto ui`      | Early       | Local web dashboard + server (self-contained no-build SPA)         |
| `auto-watch/`  | `auto watch`   | Early       | Monitor repo changes, trigger agent prompts in response            |

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

For commands that expose addressable domain data, use the resource-oriented pattern — a noun (resource) plus the verb triad `list` / `describe <id>` / `get <id>`, with `search` for ID-less discovery. Cheap rungs return IDs + metadata only; `get` is full-fidelity by default; truncated output prints the exact command to recover the full version. See `docs/auto-package-patterns.md` → "Resource Subcommands (noun + verb)".

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

<!-- autodoc: start -->
## Documentation Index

*Auto-generated by `autodoc`. Do not edit manually.*

- Run `auto doc quickstart` before first use to learn the workflow.
- Search docs with `auto doc search keyword <query>`.
- Check doc freshness with `auto doc stale`, fix issues with `auto doc fix`.
- Link code to docs with `[autodoc()]` tags — run `auto doc fix` for details.

**auto-artifact/docs**

- [Auto Artifact Requirements](auto-artifact/docs/requirements.md): Requirements for `auto artifact`, a CLI command group for AI agents to upload evidence files (screenshots, videos, logs) to a public-read S3 bucket and get back permanent URLs for embedding in PR comments. Read when: designing or implementing the auto-artifact CLI, its S3 upload/retention model, or evidence-file linking in PRs

**auto-web/docs**

- [Autoweb Requirements](auto-web/docs/requirements.md): Requirements for autoweb, a safe web research portal for AI coding agents with pluggable backends and result deduplication. Read when: designing safe web research portals for coding agents

**docs**

- [Auto API V2 Design](docs/api-v2.md): Long-term aspirational API shape for the auto-stack: unified binary, consistent command structure, and agent-first design principles. Read when: designing the unified auto binary API shape or planning cross-tool command consistency
- [Auto Bus Specification](docs/auto-bus-spec.md): The auto-bus standard: CloudEvents-shaped envelope, JSON-RPC 2.0 framing, HTTP and WebSocket transport bindings, at-most-once delivery contract, dotted event-type registry, and watch.task.* paper mapping. Read when: implementing or consuming bus events, adding a new event type, understanding the wire format or delivery guarantees
- [auto-img Research: Context-Protective Image Access for Coding Agents](docs/auto-img-research.md): Research on optimising image storage and retrieval for AI coding agents, covering token costs, progressive disclosure, and S3 patterns. Read when: designing image storage or progressive disclosure patterns
- [Auto Package Patterns](docs/auto-package-patterns.md): Reference patterns and conventions shared across all auto-* packages in the auto-stack monorepo. Used as the blueprint when creating new packages. Read when: creating a new package in the auto-stack monorepo
- [autostack install-daemon](docs/autostack-install-daemon.md): Design and implementation spec for installing and managing the autowatch daemon as a system systemd service running as a non-root user. Read when: implementing daemon installation or systemd service management
- [Ask Better Questions](docs/better-questions.md): Framework for teaching agents to learn from past decision patterns and ask better, more targeted questions using a four-stage decision maturity pipeline. Read when: designing agent question-asking systems or building decision-pattern learning pipelines
- [Decision Intelligence: Five Novel Directions](docs/claude-decision-intelligence-deep-dive.md): Deep exploration of five advanced approaches to AI decision intelligence — decision compiler, causal DAG, spec mining, git archaeology, and session gym — grounded in auto-stack infrastructure and session data. Read when: designing AI decision intelligence systems, orthogonal questioning frameworks, or session-based learning pipelines
- [How Claude Scripts Tasks: Evidence from Workflow Artifacts](docs/claude-workflow-scripting.md): Analysis of Claude Code Workflow .js orchestration scripts and run journals found under ~/.claude, revealing how Claude decomposes tasks into multi-agent harnesses — and that auto-etl ingests the subagent transcripts but not the scripts/journals that orchestrate them. Read when: understanding how Claude decomposes tasks into multi-agent workflows, or deciding whether auto-etl should ingest workflow scripts and run journals
- [Better Questions: Five Deep Extensions](docs/codex-better-questions-deep-ideas.md): Five advanced extensions to the better-questions framework — decision frontier compiler, regret ledger, workflow wind tunnel, workflow genome compiler, and ghost user critic — plus a rigorous addendum on orthogonal questioning as Bayesian posterior collapse over user intent. Read when: designing question-selection systems, building agent decision intelligence, or extending the auto-reflect planning loop
- [Doc File Usage in Agent Sessions: Findings and Structural Insights](docs/doc-file-usage-findings.md): Analysis of how agents interact with documentation files across 420 coding sessions, revealing that docs are seen constantly but read rarely, discovery bypasses the tooling, and user direction is the primary driver of doc consumption. Read when: analyzing doc discovery patterns or improving doc tooling
- [End-to-End Problems: autosearch session get Rendering](docs/end-to-end-problems.md): Identified rendering problems in autosearch session output, including missing closing tags, absent tool command previews, empty tool-use blocks, and message truncation. Read when: debugging autosearch session rendering or ETL data flow
- [Live Tier Architecture: One Transform, Many Materializations](docs/live-tier-architecture.md): Architecture note resolving the tension between batch ETL (parquet) and live event-stream monitoring: one schema, one transform, many materializations, with a hot tier in front of the same data behind a single query surface. Read when: deciding how to serve live/monitoring queries over hook events without duplicating auto-etl, or designing the hot/cold data tiering and streaming transform spine
- [Auto — Agentic Coding Intelligence Platform](docs/random.md): High-level product overview of the Auto platform: architecture, data format, tool suite, query examples, security model, and roadmap. Read when: learning the auto-stack architecture and product vision
- [Remote Skill Management — Design](docs/remote-skills-design.md): End-to-end design for turning auto-skill into a native tool for installing, customizing, updating, and exporting agent skills from remote repos: a global git cache, deterministic templating with literal/file-ref replacements, section-level doc extraction, a composite skill_version hash, version pinning policy, the skills.yaml + lock.json schemas, the full CLI surface, and a migrate-from-vercel path. Read when: implementing auto-skill's remote install/customize/update/export pipeline, the skills.yaml/lock.json formats, the git cache, deterministic skill hashing, or the migrate-from-vercel command
- [Requirements Mining](docs/requirements-mining.md): Concept for building a reusable requirements playbook by mining past agent sessions to extract standards and rules for fleshing out future task requirements. Read when: designing requirement extraction or requirements playbook systems from agent session history
- [Signals](docs/signals.md): Exploration of how raw coding session data can be transformed into actionable signals indicating what is working well or poorly in a codebase. Read when: designing metrics or feedback signals from session data
- [Review: user-journey.md](docs/user-journey.claude.md): Open questions and action items from reviewing the auto-stack user journey document for consistency and end-to-end coherence. Read when: reviewing user-journey open questions or implementation roadmap
- [User Journey Consistency Review](docs/user-journey.codex.md): Consistency review of the auto-stack user journey, confirming directional decisions and capturing open planning-stage questions for future resolution. Read when: confirming auto-stack direction or resolving planning questions
- [Auto Stack User Journey](docs/user-journey.md): End-to-end walkthrough of the Auto stack: from doc management and session ETL through search, reflection, and automated task scheduling. Read when: understanding the end-to-end auto-stack workflow and architecture

**docs/concepts**

- [Ubiquitous Language](docs/concepts/UBIQUITOUS_LANGUAGE.md): The canonical domain vocabulary for auto-stack — one word per concept (Session, Message, Host, Project, Rule, Playbook, Event, TaskDef, Trigger, Skill, Context Pack) with the terms to avoid for each. Read when: naming a domain concept in code, docs, or commits, or unsure which canonical term to use for a concept

**docs/how-to**

- [How to Set Up NTM for Multi-Agent Development](docs/how-to/set-up-ntm.md): Step-by-step guide to spawning planner and worker agent pools using NTM labels, adding workers on demand, and sending prompts to specific pools. Read when: setting up NTM tmux sessions, configuring planner/worker agent pools, or spawning multi-agent sessions with labels

**docs/postmortems**

- [Postmortem: main-branch divergence during doc commit + merge](docs/postmortems/2026-06-28-main-branch-divergence.md): A routine doc commit hit a three-way divergence of main (remote merge + unpushed local autodoc commit + a fresh feature branch), caused by treating ff-merge as the default without checking for divergence; resolved by linearising in a clean worktree and pushing as a fast-forward. Read when: merging to main while background jobs or pre-commit hooks may be committing concurrently, or debugging a non-fast-forward / diverged main

**docs/reference**

- [Claude Code Project Files Schema](docs/reference/claude-project-files-schema.md): Reference for the on-disk JSONL file format produced by Claude Code sessions, covering directory structure, line types, content blocks, token usage, subagent files, and tool-results directories. Read when: parsing Claude Code session files or understanding ETL data

**docs/research**

- [AskUserQuestion Analytics — Pipeline Investigation](docs/research/askuserquestion-analytics.md): How AskUserQuestion data flows through the auto-etl / auto-search pipeline, where the structured payload is lost, and a phased plan to surface the five target analytics metrics (frequency, question text, options, recommended option, picked option) for tuning Claude's question-asking against latent user intent. Read when: investigating AskUserQuestion analytics, planning ETL schema changes for structured tool envelopes, or scoping autosearch CLI work around tool filtering
- [Autonomous Planning for Software Factories — 2026 Update](docs/research/better-planning-autonomy.md): Recency-biased survey of 2026 spec-driven development and autonomous-planning practice (Spec Kit, OpenSpec, Antigravity artifacts, human-on-the-loop, software constitutions) with concrete moves to reduce gatekeeping. Read when: designing the planning pipeline, autonomy levels, or review/gatekeeping policy for an agent software factory
- [Research: Blogs](docs/research/blogs.md): Collected blog and reference links relevant to the auto-stack research and development process. Read when: researching external references on agent engineering or tooling
- [Research: Effective Feedback Compute (EFC) as a Session-Quality Signal](docs/research/efc-scaling-laws.md): Distillation of the Effective Feedback Compute (EFC) scaling-law paper into a concrete scoring spec for auto-reflect, mapping the paper's deterministic gate tables to our parquet schema, and naming the success-label gap as the blocker. Read when: designing session-quality signals or scoring agent traces by feedback quality
- [Multi-Project / Multi-Host Architecture](docs/research/multi-host-architecture.md): Design exploration for supporting multiple projects, users, and hosts through a single auto-ui, with autowatch as the per-user RPC executor and an abstracted transport layer. Read when: planning multi-project UI support, adding RPC to autowatch, designing cross-host connectivity, or extending the bus event envelope
- [Research: Agent Engineering Principles (Tweets)](docs/research/tweets.md): Curated research notes on agent engineering principles covering progressive disclosure, worktree isolation, spec-first development, architecture enforcement, and integrated feedback loops. Read when: understanding core agent engineering principles
<!-- autodoc: end -->

**auto search** — Search past coding agent sessions. Run `auto search quickstart` to learn more.

**auto skill** — Author and lint reusable agent skills. Run `auto skill quickstart` to learn more.

**auto env** — Standalone dev environments for worktree branches. Run `auto env quickstart` to learn how to stand up an isolated environment.

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
# Use /contextual-commit to commit (never bare git commit -m)
git push                # Push to remote
```

## Skill Usage

The following skills are installed and should be used when their trigger conditions are met.

**Note:** The recall skill works best when contextual-commit has been used consistently, because it reconstructs context from the structured action lines in commit bodies.

<important if="committing code, finishing a task, or ending a session">
Use the /contextual-commit skill instead of bare `git commit -m`. Stage files first with `git add`, then invoke the skill. Never use `--no-verify`.
</important>

<important if="releasing, tagging, publishing, or creating a new version">
Use the /release skill. Do not manually run `git tag` or `gh release create`.
</important>

<important if="starting a session, resuming work on a branch, or needing to understand recent changes">
Use the /recall skill to reconstruct development context from contextual commits.
</important>

<important if="editing, updating, or improving a CLAUDE.md file">
Use the /improve-claude-md skill. It uses `<important if>` blocks for better instruction adherence.
</important>

<important if="analyzing past sessions, finding recurring problems, or reviewing agent workflow effectiveness">
Use the /reflect-on-agent-sessions skill to search session history and produce evidence-backed reports.
</important>

<important if="searching coding agent session history across multiple tools">
Use the /cass skill for cross-tool session indexing and search. Complements autosearch with TUI and multi-tool support.
</important>

### Best Practices

- Check `br ready` at session start to find available work
- Update status as you work (in_progress → closed)
- Create new issues with `br create` when you discover tasks
- Use descriptive titles and set appropriate priority/type
- Always sync before ending session

<!-- end-br-agent-instructions -->

<!-- BEGIN auto-artifact (managed) -->
Use `auto artifact quickstart` to upload evidence files (screenshots, videos, logs) to S3 and get a permanent public URL you can embed in a PR comment.
<!-- END auto-artifact (managed) -->
