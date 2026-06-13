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

| Directory      | Command        | Status      | Description                                                        |
|----------------|----------------|-------------|--------------------------------------------------------------------|
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
- [Auto — Agentic Coding Intelligence Platform](docs/random.md): High-level product overview of the Auto platform: architecture, data format, tool suite, query examples, security model, and roadmap. Read when: learning the auto-stack architecture and product vision
- [Requirements Mining](docs/requirements-mining.md): Concept for building a reusable requirements playbook by mining past agent sessions to extract standards and rules for fleshing out future task requirements. Read when: designing requirement extraction or requirements playbook systems from agent session history
- [Signals](docs/signals.md): Exploration of how raw coding session data can be transformed into actionable signals indicating what is working well or poorly in a codebase. Read when: designing metrics or feedback signals from session data
- [Review: user-journey.md](docs/user-journey.claude.md): Open questions and action items from reviewing the auto-stack user journey document for consistency and end-to-end coherence. Read when: reviewing user-journey open questions or implementation roadmap
- [User Journey Consistency Review](docs/user-journey.codex.md): Consistency review of the auto-stack user journey, confirming directional decisions and capturing open planning-stage questions for future resolution. Read when: confirming auto-stack direction or resolving planning questions
- [Auto Stack User Journey](docs/user-journey.md): End-to-end walkthrough of the Auto stack: from doc management and session ETL through search, reflection, and automated task scheduling. Read when: understanding the end-to-end auto-stack workflow and architecture

**docs/epics**

- [Epic: Reflect Playbook Loop — Observe, Consolidate, Wire, Grow](docs/epics/001-reflect-playbook-loop.md): Epic plan for making the auto-reflect playbook loop usable by autonomous reflection agents: fix existing gaps first (session identity, lifecycle, observation capture, consolidation, signal readers, doc drift), then wire agent runs, then grow the improvement loop. Centered on a two-step Observe → Consolidate pipeline. Read when: planning or sequencing sub-tasks for the reflect playbook loop epic
- [Epic: Planning Docs Dashboard — Browse, Render, Live, Switch](docs/epics/002-planning-docs-dashboard.md): Epic plan for turning auto-ui into a multi-project planning-docs explorer: a default-landing dashboard that lists every registered project, browses each project's whole docs/ tree, renders markdown inline and self-contained HTML in an iframe, switches projects seamlessly, and live-refreshes both the open doc and the nav tree when an agent edits files. Most plumbing (JSON-RPC/WS, doc.list/doc.get, bus doc.changed, markdown render, project registry) already exists; the epic assembles and extends it rather than building from scratch. Read when: planning or sequencing sub-tasks for the planning-docs dashboard epic

**docs/epics/phase1**

- [Phase 1.2: Lifecycle-Aware Rule Retrieval](docs/epics/phase1/1.2-lifecycle-retrieval.md): Implementation plan to make auto-reflect rule lifecycle (draft/confirmed/stale) affect retrieval filtering, so agents only receive confirmed rules by default. Read when: implementing lifecycle filtering in auto-reflect rule retrieval
- [Phase 1.3 — Observation Capture](docs/epics/phase1/1.3-observation-capture.md): Implementation plan for the observation capture sub-task: schema, CLI commands, validation rules, and test plan for append-only observation events in auto-reflect. Read when: implementing auto-reflect observation add/list commands or extending the reflect event log schema
- [Epic Phase 1.4: Consolidation — Observations to Rules](docs/epics/phase1/1.4-consolidation.md): Implementation spec for the auto reflect consolidate command: a deterministic CLI step that validates, dedupes, and persists clustered observations as draft playbook rules, plus rule promote/retire lifecycle verbs. Read when: implementing the reflect consolidation step or adding rule lifecycle commands (promote/retire)
- [Phase 1.5 — Reader API over the Event Log](docs/epics/phase1/1.5-reader-api.md): Implementation spec for the auto reflect events list command, stats enrichment with rank distribution and outcome counts, and unconsolidated observation backlog count. Read when: implementing the reflect events reader API or extending reflect stats with rank/outcome metrics
- [Phase 1.6: Doc Sync and Quickstart Regen](docs/epics/phase1/1.6-doc-sync.md): Coordinator task to rewrite the auto-reflect quickstart, sweep stale command references across the repo, and update epic status after phases 1.2–1.5 are merged. Read when: completing the reflect playbook loop epic or sweeping stale command references from docs
- [Phase 1 Implementation — Orchestration](docs/epics/phase1/README.md): Coordinator README for the reflect-playbook Phase 1 integration branch: sub-task assignments, round scheduling, worker rules, and merge checklist for tasks 1.2–1.6. Read when: coordinating or reviewing the reflect-playbook Phase 1 implementation across multiple workers

**docs/experiments**

- [Experiment Patterns and Anti-Patterns](docs/experiments/PATTERNS.md): Distilled good patterns and anti-patterns for running multi-phase ML/data experiments, including decision matrices, cache-first design, honesty hooks, synthetic ground truth, and dispatch/end-of-experiment checklists. Read when: designing or running a new multi-phase experiment in docs/experiments/
- [Experiments Index](docs/experiments/README.md): Index and conventions for research-style experiments run against auto-stack, covering orthogonal questioning, co-change query latency, formal verification, and structured compiler investigations. Read when: looking for past experiment results, setting up a new experiment, or understanding the experiment folder conventions

**docs/experiments/2026-05-26-orthogonal-questioning**

- [Orthogonal Questioning — Experiment Synthesis](docs/experiments/2026-05-26-orthogonal-questioning/README.md): Four-phase experiment testing whether a coding agent can compress question-asking from ~15 to ~3 by treating user requirements as a vector space; cosine geometry failed but per-dimension classifiers with active learning achieved sign recovery in ~4 questions. Read when: designing question-selection or requirement-collapse systems, or reviewing orthogonal questioning experiment results
- [Deployable Architecture: Per-Dimension Classifiers + Active Learning](docs/experiments/2026-05-26-orthogonal-questioning/deployable-architecture.md): Practical guidance for taking the orthogonal-questioning experiment findings into production using per-dimension binary classifiers and active learning over user preference data. Read when: designing a production preference-recovery system or evaluating per-dimension classifiers vs cosine-geometry approaches for user preference modeling
- [Embedding Model Research Notes](docs/experiments/2026-05-26-orthogonal-questioning/embedding-model-research.md): Background research on text-embedding-3-small and text-embedding-3-large, HyDE, and enrichment formats to validate embedding model selection for the orthogonal-questioning experiment. Read when: selecting or evaluating embedding models for semantic similarity or question-answering systems
- [Spike: Orthogonal Questioning Validation Experiments](docs/experiments/2026-05-26-orthogonal-questioning/phase1-validation.md): Four-spike experimental validation of the requirement-vector-space framework for orthogonal questioning, with findings from real session data showing partial geometry structure and a pivot to sparse-support recovery. Read when: evaluating decision intelligence approaches, understanding orthogonal questioning experiment results, or designing session-based ML spikes
- [Orthogonal Questioning Phase 2: Context-Rich Embedding Inputs](docs/experiments/2026-05-26-orthogonal-questioning/phase2-context.md): Phase 2 spikes testing richer input formats and project-identity factoring for decision embeddings; turn-window (F4) improved pairwise correlation 7x, Spike 8 factoring dropped n_90 to 26, and raw Q&A embedding (Spike 9) dropped n_90 to 23. Read when: continuing the orthogonal questioning experiment or evaluating embedding input format choices for decision data
- [Spike: Synthetic Latent-Space Recovery (Phase 3)](docs/experiments/2026-05-26-orthogonal-questioning/phase3-synthetic.md): Experiment using synthetic data with known latent structure to test whether embedding formats can recover user preference vectors, finding cosine geometry fails while linear probes extract partial signal. Read when: evaluating embedding-based user preference recovery methods or understanding why cosine-geometry orthogonal questioning fails
- [Spike: Alternatives to Cosine Similarity (Phase 4)](docs/experiments/2026-05-26-orthogonal-questioning/phase4-alternatives.md): Phase 4 spike testing per-dimension binary classifiers and active learning as alternatives to cosine similarity for preference inference from embeddings, achieving 4.17 questions to full sign recovery. Read when: designing preference inference systems or evaluating alternatives to cosine similarity for embedding-based retrieval

**docs/experiments/2026-05-28-cochange-query-latency**

- [Phase 1 — Co-change Query Latency & Engine Comparison](docs/experiments/2026-05-28-cochange-query-latency/phase1-engine-latency.md): Performance spike comparing modernc SQLite vs duckdb for the autosearch co-change query engine, with measurements showing pure-Go SQLite is fast enough at typical repo scale and confirming column projection prunes 98.6% of parquet bytes. Read when: evaluating autosearch co-change engine performance, choosing between SQLite and duckdb for parquet queries, or understanding co-change query latency scaling

**docs/experiments/quint-sync-protocol**

- [Tech Spike Report: Quint for ETL Sync Protocol Verification](docs/experiments/quint-sync-protocol/SPIKE-REPORT.md): Spike validating Quint as a tool for specifying and model-checking the auto-etl CRDT merge protocol; all 6 assumptions verified with a GO verdict. Read when: implementing or verifying the auto-etl CRDT merge protocol using formal specification tools
- [ETL Merge Protocol — Formal Specification](docs/experiments/quint-sync-protocol/etl_merge.md): Literate Quint specification proving CRDT-based merge semantics for the auto-etl pipeline: commutativity, associativity, idempotency, monotonicity, and safe concurrent-writer model. Read when: designing ETL merge semantics, verifying CRDT properties for multi-host sync, or understanding the formal correctness model for auto-etl
- [Quint Sync Protocol Phase 2: Model-Based Testing](docs/experiments/quint-sync-protocol/phase2-mbt-verification.md): Experiment verifying that Quint MBT traces can drive Go conformance tests against the CRDT merge implementation; ITF trace generation, Go parser, and test harness all built and validated in a throwaway worktree. Read when: evaluating model-based testing with Quint for Go ETL merge logic, or continuing the quint-sync-protocol experiment

**docs/experiments/structured-compiler**

- [Structured Compiler: Assumption 1 Validation Report](docs/experiments/structured-compiler/assumption_1_report.md): Validation results for whether a hybrid structured schema can preserve acceptance-critical nuance, finding CDR 0.36 overall (FAIL) but 0.72 on rich-input task_folder cases (conditional pass). Read when: evaluating structured requirements compiler results or understanding why input richness dominates schema-based nuance preservation
- [Assumption 1 — Q&A Augmentation Report (v2 + v3)](docs/experiments/structured-compiler/assumption_1_v2_v3_report.md): Follow-up spike on Q&A augmentation for the structured compiler: v3 (augment-not-replace) lifts task_folder CDR from 0.72 to 0.84, with contamination caveats noted. Read when: evaluating Q&A augmentation strategies for structured compiler experiments or interpreting assumption 1 results
- [Structured Compiler Assumption 1 — v4: Contamination-Clean User-Twin](docs/experiments/structured-compiler/assumption_1_v4_report.md): Experiment report for Phase 6.1 of the structured compiler spike: verifies that removing ground-truth contamination from the user-twin yields a real but modest task_folder CDR lift (0.72→0.80) while confirming thin-input gains were contamination-borne. Read when: evaluating structured compiler experiment results, understanding contamination effects in LLM evaluation, or planning Phase 6.2 schema surgery
- [Structured Compiler Assumption 1 — v5 Schema Surgery Report](docs/experiments/structured-compiler/assumption_1_v5_report.md): v5 schema surgery experiment results for the structured compiler: removing decision_candidates/blast_radius and adding verbatim qualifiers/axis_priorities — verdict ABANDON, as NRS dropped 0.25 and CDR dropped 0.118 against v3 baseline. Read when: reviewing structured compiler assumption validation results or understanding why the v5 schema surgery was abandoned
- [Structured Compiler: Assumption 2 — Regret-Aware Question Policy](docs/experiments/structured-compiler/assumption_2_report.md): Validation of whether a regret-aware question policy beats confidence-only baseline, finding WRC reduction threshold FAIL and DA non-inferiority FAIL on the AND-gate formulation. Read when: evaluating question-policy strategies for requirements elicitation or understanding why regret-aware gating fails on small corpora
- [Assumption 2 v2 — Clean Labeler and Smooth Gate](docs/experiments/structured-compiler/assumption_2_v2_report.md): Partial verdict on the two-labeler split and smooth expected-regret gate: regret_score identical to confidence_only on the test set; corpus too thin to distinguish. Read when: evaluating labeling strategies and smooth gate designs for structured compiler assumption 2
- [Structured Compiler Assumption 3 Report — Incremental Recompilation Is Sound](docs/experiments/structured-compiler/assumption_3_report.md): Experiment report validating that incremental recompilation of decision graphs is safe (IR=1.0, SDLR=0.0) with 69% token savings, while noting interface-mutation over-invalidation as a known conservative trade-off. Read when: evaluating incremental decision graph recompilation, understanding structured compiler safety metrics, or planning full-recompile vs incremental modes
- [Structured Compiler Eval Corpus — Phase 0 Summary](docs/experiments/structured-compiler/dataset_summary.md): Summary of the 40-case structured compiler evaluation corpus: task type breakdown (17 go_cli_feature, 6 docs_skill, etc.), corrections per case distribution, and sources (task folders, git commits, autosearch sessions). Read when: understanding the structured compiler evaluation dataset or adding cases to the corpus
- [Structured Compiler Spike — Final Synthesis](docs/experiments/structured-compiler/summary.md): Final synthesis across all eight structured compiler experiments, covering schema utilization, regret-aware question policy, and incremental recompile safety — recommending a scoped planning-doc enricher and abandoning the general-purpose compiler framing. Read when: reviewing the complete structured compiler experiment results, understanding the final product recommendation, or applying the methodology lessons to a future spike

**docs/reference**

- [Claude Code Project Files Schema](docs/reference/claude-project-files-schema.md): Reference for the on-disk JSONL file format produced by Claude Code sessions, covering directory structure, line types, content blocks, token usage, subagent files, and tool-results directories. Read when: parsing Claude Code session files or understanding ETL data

**docs/research**

- [AskUserQuestion Analytics — Pipeline Investigation](docs/research/askuserquestion-analytics.md): How AskUserQuestion data flows through the auto-etl / auto-search pipeline, where the structured payload is lost, and a phased plan to surface the five target analytics metrics (frequency, question text, options, recommended option, picked option) for tuning Claude's question-asking against latent user intent. Read when: investigating AskUserQuestion analytics, planning ETL schema changes for structured tool envelopes, or scoping autosearch CLI work around tool filtering
- [Research: Blogs](docs/research/blogs.md): Collected blog and reference links relevant to the auto-stack research and development process. Read when: researching external references on agent engineering or tooling
- [Research: Effective Feedback Compute (EFC) as a Session-Quality Signal](docs/research/efc-scaling-laws.md): Distillation of the Effective Feedback Compute (EFC) scaling-law paper into a concrete scoring spec for auto-reflect, mapping the paper's deterministic gate tables to our parquet schema, and naming the success-label gap as the blocker. Read when: designing session-quality signals or scoring agent traces by feedback quality
- [Research: Agent Engineering Principles (Tweets)](docs/research/tweets.md): Curated research notes on agent engineering principles covering progressive disclosure, worktree isolation, spec-first development, architecture enforcement, and integrated feedback loops. Read when: understanding core agent engineering principles

**docs/spikes**

- [Spike: Structured Compiler Assumptions Validation](docs/spikes/structured-compiler-assumptions-validation.md): Experiment design for validating the three highest-risk assumptions behind a structured requirements compiler using Python experiments in .tmp/experiments/structured-compiler/ against the auto-stack session history. Read when: implementing or reviewing structured compiler assumption validation experiments
- [Structured Compiler Spike — Consolidated Findings](docs/spikes/structured-compiler-findings.md): Product decision record for the structured requirements compiler: the general-purpose compile-from-prompt surface does not ship, A3 incremental recompile is reusable infrastructure, and only the scoped planning-doc enricher delivers measurable value. Read when: reviewing the structured compiler product decision or understanding why the general-purpose requirements compiler was not built
- [Structured Compiler Phase 6: Decision-Grade Validation Plan](docs/spikes/structured-compiler-phase-6.md): Plan for three parallel Phase 6 experiments to validate the rich-input and Q&A regime for the structured compiler, following the STOP-for-thin-input verdict from Phase 5. Read when: planning or reviewing Phase 6 validation experiments for the structured compiler spike

**docs/tasks/001-ts-import-graph**

- [Context: Task 001 — TypeScript Import Graph](docs/tasks/001-ts-import-graph/context.md): Verified codebase context for implementing the TypeScript import graph tool in auto-graph, covering key files, patterns, and scaffolding sequence. Read when: implementing auto-graph TypeScript scanner or scaffolding a new auto-* package following the task 001 pattern
- [Feedback: Task 001 — TypeScript Import Graph](docs/tasks/001-ts-import-graph/feedback.md): Post-implementation feedback for task 001: three problems encountered (ast-grep tsx mode, re-export pattern, PR base mismatch) and reflections on the coordinator-subagent pattern and ast-grep gotchas. Read when: reviewing lessons from the TypeScript import graph implementation or debugging ast-grep scanner issues
- [Plan: TypeScript Import Graph (Task 001)](docs/tasks/001-ts-import-graph/plan.md): Phased implementation plan for scaffolding auto-graph and building the TypeScript import graph command using ast-grep, covering six phases from package scaffold through E2E tests and Makefile integration. Read when: implementing the TypeScript import graph feature or reviewing the phase-by-phase build sequence for auto-graph
- [Task 001: TypeScript Import Graph Requirements](docs/tasks/001-ts-import-graph/requirements.md): Acceptance criteria for the autograph TypeScript import graph feature, covering ast-grep scanning, tsconfig path alias resolution, and AC-1 through AC-9. Read when: implementing or reviewing the TypeScript import graph feature in autograph
- [Solution: Task 001 — TypeScript Import Graph](docs/tasks/001-ts-import-graph/solution.md): Design for implementing TypeScript import graph support in auto-graph: ast-grep scanner, tsconfig resolver, language-agnostic graph model, output formatters, and test fixture strategy. Read when: implementing or reviewing the autograph TypeScript code graph command design

**docs/tasks/002-git-history-etl**

- [Context: Task 002 — Git History ETL](docs/tasks/002-git-history-etl/context.md): Verified codebase reference for implementing git history ETL: key files for model, writer, sync state, transform utilities, and CLI with exact line numbers and struct signatures. Read when: implementing git history ETL in auto-etl or understanding the writer/model pattern for new ETL sources
- [Feedback: Git History ETL (Task 002)](docs/tasks/002-git-history-etl/feedback.md): Post-task feedback from implementing git history ETL: PAT token leak in remote URLs, --since unit convention conflict between months and minutes, and CI format check failure from skipping gofmt. Read when: reviewing lessons from the git history ETL task or understanding credential stripping and date convention requirements
- [Plan: Task 002 — Git History ETL](docs/tasks/002-git-history-etl/plan.md): Five-phase implementation plan for adding git history ETL to auto-etl: model structs, normalization, extraction, writer, and CLI wiring. Read when: implementing git history ETL in auto-etl or understanding its phase structure
- [Requirements: Task 002 — Git History ETL](docs/tasks/002-git-history-etl/requirements.md): Requirements and acceptance criteria for extracting git commit history into five parquet datasets (git_repositories, git_refs, commits, commit_files, commit_hunks) via auto-etl. Read when: implementing or reviewing git history ETL requirements and acceptance criteria
- [Solution: Task 002 — Git History ETL](docs/tasks/002-git-history-etl/solution.md): Design for ingesting git repository history into auto-etl parquet: five row structs, git shell extraction, incremental sync-state, repo discovery, URL normalization, and CLI wiring. Read when: implementing git history ETL or designing a new incremental ETL source in auto-etl

**docs/tasks/003-go-import-graph**

- [Context: Go Import Graph (Task 003)](docs/tasks/003-go-import-graph/context.md): Codebase context for adding Go language support to auto-graph, with precise file and line references for the scanner interface, resolver interface, language dispatch, graph building, and E2E fixture patterns. Read when: implementing Go language support in auto-graph or locating specific extension points in the existing TypeScript implementation
- [Feedback: Task 003 — Go Import Graph](docs/tasks/003-go-import-graph/feedback.md): Post-implementation reflections on the Go import graph feature, covering scanner/walker skip-list asymmetry, naive go.mod parsing, and other gotchas encountered. Read when: implementing Go import graph features or debugging autograph's Go scanner
- [Plan: Task 003 — Go Import Graph](docs/tasks/003-go-import-graph/plan.md): Implementation plan for adding Go language support to autograph's code graph command: Go scanner using go/parser, Go resolver using go.mod, test fixtures, and e2e tests. Read when: implementing or reviewing the autograph Go import graph feature plan
- [Requirements: Task 003 — Go Import Graph](docs/tasks/003-go-import-graph/requirements.md): Requirements for adding Go language support to autograph: stdlib-based go/parser scanner, module-path resolver via go.mod, language auto-detection, all Go import styles, and e2e test fixtures. Read when: implementing Go import graph scanning in auto-graph or extending autograph to a new language
- [Solution: Go Import Graph (Task 003)](docs/tasks/003-go-import-graph/solution.md): Solution design for adding Go language support to auto-graph via GoScanner (using go/parser), GoResolver (reading go.mod), package-directory expansion in buildGraph, and language detection from go.mod vs tsconfig.json. Read when: implementing Go scanner/resolver or extending auto-graph language dispatch

**docs/tasks/004-context-pack**

- [Context: Task 004 — Context Pack](docs/tasks/004-context-pack/context.md): Verified codebase context for the context-pack task, covering runCodeGraph extraction, import metadata merging, and key file references in autograph. Read when: implementing the autograph context-pack feature or understanding its codebase dependencies
- [Feedback: Task 004 — Context Pack](docs/tasks/004-context-pack/feedback.md): Post-implementation feedback for the context pack task: scanner/builder import-kind mismatch, budget enforcement against partial packs, and merge conflict from stale worktree. Read when: reviewing context pack implementation lessons or understanding import-kind normalization between scanner and builder
- [Plan: Task 004 — Context Pack](docs/tasks/004-context-pack/plan.md): Implementation plan for autograph code context: extracting reusable graph construction, adding context-pack model/builder/validator/token-estimator/renderer, and a new autograph code context command with markdown and JSON output. Read when: implementing the autograph context pack feature or understanding the codegraph/contextpack package layout
- [Requirements: Context Pack (Task 004)](docs/tasks/004-context-pack/requirements.md): Requirements for the autograph context-pack command: token-budgeted context bundle around seed files using the import graph, with markdown default output, JSON option, prioritized file selection, and agent-oriented guidance. Read when: implementing the context-pack command or reviewing token budgeting and file prioritization requirements for autograph
- [Solution: Task 004 — Context Pack](docs/tasks/004-context-pack/solution.md): Design for the autograph code context command using a token-budgeted dependency neighborhood, a reusable internal/codegraph package, and merged import metadata. Read when: implementing or reviewing the autograph context-pack solution design

**docs/tasks/005-code-graph-alias-reexports**

- [Context: Task 005 — Code Graph Alias Re-exports](docs/tasks/005-code-graph-alias-reexports/context.md): Verified codebase context for implementing TypeScript path alias resolution and re-export scanning fixes in auto-graph, with key file locations and resolver/scanner code references. Read when: implementing TypeScript path alias resolution or fixing re-export scanning in auto-graph
- [Feedback: Task 005 — Code Graph Alias Re-exports](docs/tasks/005-code-graph-alias-reexports/feedback.md): Post-implementation feedback for task 005: two problems found (e2e stdout/stderr mixing, zero-length wildcard capture) and reflections on the clean resolver interface and the value of e2e binary invocation testing. Read when: reviewing lessons from the alias re-exports implementation or debugging autograph e2e test output capture
- [Plan: TypeScript Alias Resolution and Re-Export Hardening (Task 005)](docs/tasks/005-code-graph-alias-reexports/plan.md): Phased implementation plan for hardening alias resolution and re-export detection in auto-graph, covering fixture creation, resolver wildcard/exact/baseUrl semantics, CLI diagnostics for unresolved aliases, and full regression testing. Read when: implementing TypeScript alias hardening in auto-graph or reviewing the phase sequence for resolver and scanner coverage
- [Task 005: Code Graph Alias and Re-export Resolution Requirements](docs/tasks/005-code-graph-alias-reexports/requirements.md): Acceptance criteria (AC-1 through AC-5) for adding TypeScript path alias and barrel-file re-export resolution to autograph code graph. Read when: implementing TypeScript path alias or re-export resolution in autograph
- [Solution: Task 005 — Code Graph Alias Re-exports](docs/tasks/005-code-graph-alias-reexports/solution.md): Design for fixing TypeScript path alias resolution and re-export edge detection in auto-graph: hardened path mapping, baseUrl probing, unresolved alias diagnostics to stderr. Read when: implementing TypeScript alias resolution fixes or understanding re-export scanning improvements in auto-graph

**docs/tasks/006-autograph-quote-jsonc-fixes**

- [Context: Task 006 — Autograph Quote and JSONC Fixes](docs/tasks/006-autograph-quote-jsonc-fixes/context.md): Verified codebase context for fixing ast-grep quote sensitivity and JSONC tsconfig parsing in autograph: key file locations, pattern line numbers, and extraction regex signatures. Read when: fixing ast-grep quote-sensitive patterns or adding JSONC tsconfig support in auto-graph
- [Feedback: Quote and JSONC Fixes (Task 006)](docs/tasks/006-autograph-quote-jsonc-fixes/feedback.md): Post-task feedback from the autograph quote/JSONC fix task: golden file kind-name mismatch after canonicalization, single-pass stripJSONC rewrite to prevent string content corruption, and parallel phase dispatch lessons. Read when: reviewing lessons from the quote/JSONC fix task or understanding canonical import kind names and JSONC stripping edge cases
- [Plan: Task 006 — Autograph Quote and JSONC Fixes](docs/tasks/006-autograph-quote-jsonc-fixes/plan.md): Three-phase plan to fix ast-grep single-quote pattern sensitivity and add JSONC-tolerant tsconfig parsing with a stripJSONC helper and warning writer. Read when: implementing ast-grep single-quote patterns or JSONC tsconfig parsing in autograph
- [Requirements: Task 006 — Autograph Quote-Style and JSONC Fixes](docs/tasks/006-autograph-quote-jsonc-fixes/requirements.md): Requirements for fixing two silent edge-dropping bugs in autograph: single-quote insensitivity in ast-grep re-export patterns, and strict JSON parsing of JSONC tsconfig.json files. Read when: implementing or reviewing the autograph quote-style and JSONC tsconfig fix requirements
- [Solution: Task 006 — Autograph Quote and JSONC Fixes](docs/tasks/006-autograph-quote-jsonc-fixes/solution.md): Solution for fixing ast-grep single-quote pattern support and JSONC tsconfig parsing: duplicate four quote-dependent patterns, add stripJSONC helper, add stderr warning on parse failure, and add e2e and unit test fixtures. Read when: implementing quote-agnostic ast-grep scanning or JSONC-tolerant tsconfig loading in auto-graph

**docs/tasks/007-autograph-doc-links**

- [Context: Autograph Doc Links (Task 007)](docs/tasks/007-autograph-doc-links/context.md): Codebase context for integrating autodoc [autodoc()] tags into autograph's graph and context pack, with precise file/line references for the graph model, build pipeline, context pack builder, CLI commands, format renderers, and autodoc packages. Read when: implementing autodoc tag integration in autograph or locating extension points for cross-module doc-link enrichment
- [Feedback: Task 007 — Autograph Doc Links](docs/tasks/007-autograph-doc-links/feedback.md): Post-implementation reflections on autograph doc links, covering cross-module internal/ visibility, unfiltered edge iteration, linkscan git dependency, and triple-backtick collision. Read when: implementing doc link scanning in autograph or debugging edge-type filtering
- [Plan: Task 007 — Autograph Doc Links](docs/tasks/007-autograph-doc-links/plan.md): Implementation plan for adding autodoc doc-link awareness to autograph: public scan/doctree APIs, doclink enrichment layer, --no-docs flag on CLI commands, and doc candidate priorities in context pack builder. Read when: implementing autodoc doc-link enrichment in autograph or wiring auto-doc into the context pack
- [Requirements: Task 007 — Autograph Doc Links](docs/tasks/007-autograph-doc-links/requirements.md): Requirements for including autodoc-linked documentation as nodes and edges in autograph code graphs and context bundles, with opt-out flag, zero-config behavior, and support for both JSON/DOT/Mermaid graph formats. Read when: implementing doc-link nodes and edges in autograph or extending autograph to surface documentation alongside code
- [Solution: Autograph Doc Links (Task 007)](docs/tasks/007-autograph-doc-links/solution.md): Solution design for integrating autodoc [autodoc()] tags into the autograph graph: thin public API wrappers in auto-doc/pkg/, a doclink enrichment package, separate doc adjacency maps in the context pack builder, and DOT/Mermaid shape differentiation for doc nodes. Read when: implementing doc-link graph enrichment in autograph or understanding the cross-module autodoc dependency approach

**docs/tasks/008-commit-session-link**

- [Context: Task 008 — Commit-Session Link](docs/tasks/008-commit-session-link/context.md): Verified codebase context for linking git commits to coding sessions, covering the Commit struct, parseTrailers, messages parquet schema, and auto-config patterns. Read when: implementing commit-to-session linkage in auto-etl or auto-search
- [Feedback: Task 008 — Commit Session Link](docs/tasks/008-commit-session-link/feedback.md): Post-implementation feedback for the commit session link task: filepath.Glob recursion bug, remote URL normalization mismatch, and git-common-dir vs git-dir hook path issue. Read when: reviewing commit-session link implementation lessons or understanding parquet partition path and remote URL normalization pitfalls
- [Plan: Task 008 — Commit-Session Link](docs/tasks/008-commit-session-link/plan.md): Implementation plan for linking git commits to agent sessions: add session_id to the Commit parquet row via trailer extraction and fallback message matching, plus a new auto-config package with prepare-commit-msg hook installation. Read when: implementing commit-to-session linking in auto-etl or setting up the prepare-commit-msg hook
- [Requirements: Commit-to-Session Link (Task 008)](docs/tasks/008-commit-session-link/requirements.md): Requirements for adding session_id to the commits parquet table via Session-Id git trailers (authoritative) or bash-command regex fallback, plus a prepare-commit-msg hook installed by autoconfig init --project. Read when: implementing commit-to-session linking in auto-etl or reviewing the two-tier session ID extraction requirements
- [Solution: Task 008 — Commit-Session Link](docs/tasks/008-commit-session-link/solution.md): Three-workstream design for commit-session linking: git trailer extraction, fallback parquet session matcher, and hook installation via auto-config. Read when: implementing commit-to-session linkage or reviewing the solution design for task 008

**docs/tasks/010-autosearch-co-change**

- [Context: Task 010 — Autosearch Co-Change Query](docs/tasks/010-autosearch-co-change/context.md): Verified codebase facts grounding the autosearch co-change command implementation: CLI patterns, parquet discovery, git parquet schema, and SQLite query approach. Read when: implementing or reviewing the autosearch co-change command codebase context
- [Feedback: Task 010 — Autosearch Co-Change](docs/tasks/010-autosearch-co-change/feedback.md): Post-implementation feedback for task 010 (autosearch co-change): four problems found including pre-existing lint debt masking CI, Wn large-commit filter bug, and lessons about asserting shared denominators in tests. Read when: reviewing lessons from the co-change implementation or debugging co-change scoring correctness
- [Plan: Autosearch Co-Change Query (Task 010)](docs/tasks/010-autosearch-co-change/plan.md): Phased implementation plan for the autosearch co-change command: remote normalisation to auto-shared, slim git parquet readers, in-memory SQLite aggregation, weighted scoring, repo resolution, CLI flags, fixture generation with privacy guard, and full test coverage. Read when: implementing the co-change query feature or reviewing the phase sequence for git parquet integration and in-memory SQLite aggregation
- [Task 010: Autosearch Co-Change Query Requirements](docs/tasks/010-autosearch-co-change/requirements.md): Requirements for the auto-search co-change command: lift-weighted confidence scoring, time decay, in-process SQLite engine, and AC-1 through AC-20. Read when: implementing or reviewing the autosearch co-change query feature
- [Solution: Task 010 — Autosearch Co-Change Query](docs/tasks/010-autosearch-co-change/solution.md): Design for the autosearch co-change command: per-query ephemeral SQLite engine over parquet, repo resolution via git remote, weighted co-occurrence scoring with time decay and large-commit penalty. Read when: implementing or reviewing the autosearch co-change query engine and scoring algorithm

**docs/tasks/011-autosearch-co-change-compact-output**

- [Context: Task 011 — Autosearch Co-Change Compact Output](docs/tasks/011-autosearch-co-change-compact-output/context.md): Verified codebase context for the compact text-mode rewrite of autosearch co-change: key files for CLI dispatcher, engine types, large-commit cutoff plumbing, and scoring path with exact line numbers. Read when: implementing compact text output for autosearch co-change or modifying the co-change engine types
- [Feedback: Co-Change Compact Output (Task 011)](docs/tasks/011-autosearch-co-change-compact-output/feedback.md): Post-task feedback from the co-change compact output task: cross-phase compile coupling when deleting struct fields, tree-distance d-label arithmetic, empirical budget fixture tuning, and a multi-line subject bug caught by review. Read when: reviewing lessons from the co-change compact output task or understanding approxTokens, treeDistance, or fixturegen package constraints
- [Plan: Task 011 — Co-Change Compact Output](docs/tasks/011-autosearch-co-change-compact-output/plan.md): Five-phase plan: remove large-commit cutoff, add compact text renderer, flip CLI default to text, rewrite quickstart co-change section, and add E2E scenario fixtures. Read when: implementing compact text output for the autosearch co-change command
- [Requirements: Task 011 — Autosearch Co-Change Compact Output](docs/tasks/011-autosearch-co-change-compact-output/requirements.md): Requirements for making autosearch co-change output compact by default: token-budget cap, directory-tree-distance annotations, continuous inverse-fan-out weighting, and --json flag for verbose detail. Read when: implementing or reviewing the autosearch co-change compact output format requirements
- [Solution: Task 011 — Autosearch Co-Change Compact Output](docs/tasks/011-autosearch-co-change-compact-output/solution.md): Solution for compact co-change text output: remove binary large-commit cutoff, add a text renderer with budget truncation and boring-first trim, normalize score display, and add --text flag to the CLI. Read when: implementing compact text rendering for autosearch co-change or understanding the render.go budget truncation design

**docs/tasks/012-structured-tool-output**

- [Acceptance Results: Structured Tool Output (Task 012)](docs/tasks/012-structured-tool-output/acceptance-results.md): Human acceptance test results for task 012: 16,167 rows with tool_use_result_json populated, 73.8% recommended-acceptance rate (correcting the 55.7% regex baseline), and confirmation that the structured column finds all 61 AskUserQuestion calls with recommendations. Read when: reviewing acceptance test results for structured tool output or understanding why the 73.8% rate corrects the prior 55.7% regex baseline
- [Context: Task 012 — Structured Tool Output](docs/tasks/012-structured-tool-output/context.md): Code-level context for threading tool_use_result_json through the ETL parser, transform pipeline, parquet model, and autosearch describe surface. Read when: implementing or reviewing the structured tool output ETL pipeline changes
- [Feedback: Task 012 — Structured Tool Output](docs/tasks/012-structured-tool-output/feedback.md): Post-implementation feedback for the structured tool output task: autoetl run flag clarification, baseline metric validation methodology, and json_extract vs json_extract_string SQLite/DuckDB difference. Read when: reviewing structured tool output implementation lessons or understanding json_extract SQLite/DuckDB compatibility
- [Plan: Task 012 — Structured Tool Output](docs/tasks/012-structured-tool-output/plan.md): Implementation plan for threading a new tool_use_result_json column from JSONL through auto-etl parquet, autosearch SQLite, and message describe JSON output, with dual schema version bumps and corpus backfill. Read when: implementing structured tool output in auto-etl/auto-search or adding tool_use_result_json to the pipeline
- [Requirements: Structured Tool Output (Task 012)](docs/tasks/012-structured-tool-output/requirements.md): Requirements for capturing the JSONL toolUseResult envelope verbatim into a new tool_use_result_json column in the messages parquet, mirroring it in the SQLite index, and making AskUserQuestion picked/recommended analytics queryable without regex. Read when: implementing structured tool output capture in auto-etl or understanding the tool_use_result_json schema requirements
- [Solution: Task 012 — Structured Tool Output](docs/tasks/012-structured-tool-output/solution.md): Design for capturing tool use result envelopes as one raw JSON parquet column, with a message describe surface to retrieve it without FTS or typed AUQ fields. Read when: implementing structured tool output storage in the auto-etl parquet schema

**docs/tasks/013-auto-ui-tech-base**

- [Conformance Script: Task 013 — Auto UI Tech Base](docs/tasks/013-auto-ui-tech-base/conformance.md): Browser-driven conformance test plan for the auto-ui tech base: agent-browser drives a real browser to verify rendering, routing, fetch, hash-based state, and hot-reload acceptance criteria. Read when: running or extending the auto-ui conformance test suite or verifying SPA routing and fetch behavior
- [Context: Task 013 — Auto UI Tech Base](docs/tasks/013-auto-ui-tech-base/context.md): Verified codebase context for scaffolding auto-ui: exact signatures for auto-shared dependencies, reference package patterns from auto-graph, embed precedent, and server/SPA integration points. Read when: implementing auto-ui or scaffolding a new auto-* package following the auto-graph conventions
- [Feedback: Auto UI Tech Base (Task 013)](docs/tasks/013-auto-ui-tech-base/feedback.md): Post-task feedback from building the auto-ui SPA tech base: blank page from missing htm specifier in import map, stale assets without Cache-Control no-store, go run child outliving parent, pkill pattern collision, and graceful shutdown dead code. Read when: reviewing lessons from the auto-ui tech base task or understanding browser-layer defects invisible to Go tests
- [Plan: Task 013 — Auto UI Tech Base](docs/tasks/013-auto-ui-tech-base/plan.md): Six-phase plan to scaffold the auto-ui package: Preact+htm SPA, embedded static assets, HTTP server, JSON-RPC dispatcher, WebSocket transport, and agent-browser conformance testing. Read when: implementing or reviewing the auto-ui tech foundation scaffolding
- [Requirements: Task 013 — Auto UI Tech Base](docs/tasks/013-auto-ui-tech-base/requirements.md): Requirements for the auto-ui package: a Go binary serving a no-build Preact+htm SPA with hash-based routing, embed mode, dev mode hot-reload, and a REST/WebSocket API. Read when: implementing or reviewing the auto-ui technical base requirements and SPA architecture
- [Solution: Task 013 — Auto UI Tech Base](docs/tasks/013-auto-ui-tech-base/solution.md): Solution for scaffolding auto-ui: full auto-* package layout, build-tag split for embedded/dev static assets, Go HTTP server with JSON API and Preact+htm SPA, hash-based routing, and monorepo wiring. Read when: implementing auto-ui or understanding the embedded no-build SPA architecture

**docs/tasks/014-autodoc-link-event-log**

- [Requirements: Autodoc Link Event Log (Task 014)](docs/tasks/014-autodoc-link-event-log/requirements.md): Requirements for replacing autodoc's inline hash-bearing tags with a markerless append-only link event log in .auto/doc/links.jsonl, covering typed anchors, CLI authoring, migration cutover, and freshness detection without file rewrites. Read when: implementing the autodoc link event log or understanding the markerless anchor design and migration requirements

**docs/tasks/015-session-intent-summary**

- [Context: Task 015 — Session Intent Summary](docs/tasks/015-session-intent-summary/context.md): Verified codebase context for first_user_intent fields in auto-etl, covering AgentSession struct, transform pipeline paths, and query_sessions function signatures. Read when: implementing session intent summary extraction in auto-etl
- [Feedback: Task 015 — Session Intent Summary](docs/tasks/015-session-intent-summary/feedback.md): Post-implementation feedback for the session intent summary task: wrong CLI subcommand reference, InsertSession signature breakage at call sites, and stale parquet fixtures after schema changes. Read when: reviewing session intent summary implementation lessons or understanding auto-search schema migration pitfalls
- [Plan: Task 015 — Session Intent Summary](docs/tasks/015-session-intent-summary/plan.md): Implementation plan for computing a session intent field from the first real user message in auto-etl and threading it through auto-search into session list/describe output. Read when: implementing session intent extraction in auto-etl or adding new session-level derived fields to the pipeline
- [Requirements: Session Intent Summary (Task 015)](docs/tasks/015-session-intent-summary/requirements.md): Requirements for surfacing a human-readable first_user_intent field on ETL session records, skipping junk first messages with a deterministic skip-list heuristic, and exposing the intent in autosearch session list and session describe. Read when: implementing session intent extraction in auto-etl or understanding the junk-skip heuristic and intent field requirements
- [Solution: Task 015 — Session Intent Summary](docs/tasks/015-session-intent-summary/solution.md): Deterministic junk-skip heuristic with slash-command fallback and headTruncate on rune boundary, adding first_user_intent and first_user_intent_raw parquet fields to AgentSession. Read when: implementing session intent summary extraction or understanding the first_user_intent field design

**docs/tasks/016-etl-preserve-session-signal**

- [Context: Task 016 — ETL Preserve Session Signal](docs/tasks/016-etl-preserve-session-signal/context.md): Verified codebase facts for implementing dropped session signal capture in auto-etl and surfacing it in auto-search: thinking blocks, stop_reason, is_error, cache split tokens, skill attribution, and permission mode. Read when: implementing or reviewing the ETL session signal preservation feature with producer/consumer file locations
- [Feedback: Task 016 — ETL Preserve Session Signal](docs/tasks/016-etl-preserve-session-signal/feedback.md): Post-implementation feedback for task 016: two bugs found (ineffectual assignment, inverted test assertion) and reflections on the dual schema version bump pattern and positional Insert signature pitfalls. Read when: reviewing lessons from the ETL session signal preservation implementation or debugging schema version bump patterns
- [Plan: ETL Preserve Session Signal (Task 016)](docs/tasks/016-etl-preserve-session-signal/plan.md): Phased implementation plan for preserving thinking blocks, stop_reason, is_error, cache token split, permission_mode, skill_name attribution, and other dropped ETL signals, with schema bumps and autosearch CLI opt-in for thinking content. Read when: implementing thinking block preservation or any of the dropped ETL signal fields in auto-etl and auto-search
- [Task 016: ETL Preserve Session Signal Requirements](docs/tasks/016-etl-preserve-session-signal/requirements.md): Requirements to stop auto-etl silently dropping signal: thinking blocks, skill attribution, stop_reason, permission mode, and cache token split (AC-1 through AC-6). Read when: adding or reviewing session signal preservation fields in auto-etl parquet output
- [Solution: Task 016 — ETL Preserve Session Signal](docs/tasks/016-etl-preserve-session-signal/solution.md): Two-module solution for preserving dropped session signals: auto-etl captures thinking blocks, stop_reason, is_error, cache split tokens, and skill/permission fields; auto-search surfaces them via schema bump and index rebuild. Read when: implementing the ETL session signal preservation feature or understanding the producer/consumer schema changes

**docs/tasks/017-unify-binaries-into-auto**

- [Context: Task 017 — Unify Binaries into Auto](docs/tasks/017-unify-binaries-into-auto/context.md): Verified codebase context for merging 10 tool binaries into a single auto binary: go.work feasibility, public command-tree constructors per tool, and the three non-standard tools that need normalization. Read when: implementing the unified auto binary or adding a new tool to the auto-cli umbrella module
- [Feedback: Unify Binaries into Auto (Task 017)](docs/tasks/017-unify-binaries-into-auto/feedback.md): Post-task feedback from unifying separate tool binaries into the auto umbrella: concurrent subagent write leaks into main worktree, non-re-entrant init() refactor causing panic, hallucinated docs-sweep agent, and go.work sync version rewriting. Read when: reviewing lessons from the binary unification task or understanding the rootcmd.New() seam pattern and concurrent subagent isolation requirements
- [Plan: Task 017 — Unify Binaries into Auto](docs/tasks/017-unify-binaries-into-auto/plan.md): Six-phase plan to merge 10 separate tool binaries into one auto binary via a go.work workspace, per-tool rootcmd wrappers, auto-cli umbrella module, and stale-ref guard. Read when: implementing the auto binary unification or understanding the go.work workspace structure
- [Requirements: Task 017 — Unify Binaries into Auto](docs/tasks/017-unify-binaries-into-auto/requirements.md): Requirements for merging all auto-stack tool binaries into a single `auto` binary with subcommands, including autoconfig and autoui, with a hard cutover from per-tool binaries. Read when: implementing or reviewing the auto binary unification requirements and migration scope
- [Solution: Task 017 — Unify Binaries into Auto](docs/tasks/017-unify-binaries-into-auto/solution.md): Solution for merging 10 tool binaries into one auto binary via go.work workspace and thin rootcmd public seam per tool, with normalization of three non-standard tools and rename of root Use: values. Read when: implementing the unified auto binary, adding a new rootcmd seam, or normalizing a tool's command structure

**docs/tasks/018-auto-watch-easy-daemon**

- [Context: Auto Watch Easy Daemon (Task 018)](docs/tasks/018-auto-watch-easy-daemon/context.md): Codebase context for the user-first daemon install task, with precise file/line references for the daemoninstall package, CLI flags, update flow, doctor checks, and the system-vs-user scope design tension to resolve. Read when: implementing user-scope systemd daemon support in auto-watch or locating the Manager, Runner, and template extension points
- [Feedback: Task 018 — Auto Watch Easy Daemon](docs/tasks/018-auto-watch-easy-daemon/feedback.md): Post-implementation reflections on the autowatch easy daemon: subagent main-worktree leaks, golangci-lint cross-worktree cache pollution, and scope-switch pattern for user-level systemd. Read when: implementing systemd daemon installation in auto-watch or debugging worktree subagent isolation
- [Plan: Task 018 — Auto Watch Easy Daemon](docs/tasks/018-auto-watch-easy-daemon/plan.md): Implementation plan for parameterizing the auto-watch daemon installer with user/system scope, adding no-sudo user-level install with enable-linger, doctor unit check, and install.sh restart hook. Read when: implementing or reviewing the auto-watch daemon scope parameterization and user-level systemd install plan
- [Requirements: Task 018 — Auto Watch Easy Daemon](docs/tasks/018-auto-watch-easy-daemon/requirements.md): Requirements for making auto watch daemon install work without sudo using a user-level systemctl --user service, with single-command updates, optional --system mode, idempotent/self-healing install, and accurate docs. Read when: implementing or troubleshooting the auto watch daemon install command
- [Solution: Auto Watch Easy Daemon (Task 018)](docs/tasks/018-auto-watch-easy-daemon/solution.md): Solution for defaulting auto watch daemon install to user-scope systemd, adding --system opt-in, wiring loginctl enable-linger for boot persistence, extending install.sh for one-command update, and adding doctor unit checks. Read when: implementing user-scope daemon installation in auto-watch or understanding the Scope parameter design and XDG_RUNTIME_DIR requirement

**docs/tasks/019-playbook-retrieval-loop**

- [Auto Reflect Mock README — Task 019 API Preview](docs/tasks/019-playbook-retrieval-loop/README.md): Artifact visualizing the final auto-reflect CLI surface for the playbook retrieval loop: rule authoring, retrieve/select/feedback/gate workflow, storage model, and stats. Read when: understanding the auto-reflect playbook retrieval loop CLI surface or designing its API
- [Context: Task 019 — Playbook Retrieval Loop](docs/tasks/019-playbook-retrieval-loop/context.md): Codebase context for the playbook retrieval loop task, noting that plan.md is intentionally absent at this review stage and open design questions were resolved before /new-plan. Read when: reviewing the playbook retrieval loop task context or understanding why plan.md was absent at solution review
- [Feedback: Task 019 — Playbook Retrieval Loop](docs/tasks/019-playbook-retrieval-loop/feedback.md): Post-implementation feedback for task 019: three problems (lint debt, nonexistent CLI flag in plan, exit code normalization) and reflections on worktree shard key design and the need to run make check every phase. Read when: reviewing lessons from the playbook retrieval loop implementation or implementing a new auto-reflect feature
- [Retrieval Loop Flow (Task 019)](docs/tasks/019-playbook-retrieval-loop/loop-flow.md): Command-to-event-to-projection flow diagram for the reflect retrieval loop: retrieve, select, feedback, gate check commands mapped to the append-only event log, snapshot folding, and read surfaces. Read when: implementing or understanding the auto reflect retrieval loop, event log structure, or snapshot projection mechanics
- [Plan: Task 019 — Playbook Retrieval Loop](docs/tasks/019-playbook-retrieval-loop/plan.md): Four-phase serial implementation plan for the auto-reflect event-sourced playbook retrieval loop: events package, rules projection and CLI cutover, loop package with feedback gate, and E2E tests. Read when: implementing the auto-reflect playbook retrieval loop or reviewing its phase structure
- [Requirements: Task 019 — Playbook Retrieval Loop](docs/tasks/019-playbook-retrieval-loop/requirements.md): Requirements for implementing the auto-reflect v2 playbook retrieval loop: enriched rule schema with lifecycle tracking, append-only event log, and two-phase retrieve/feedback loop for self-improving rules. Read when: implementing or reviewing the auto-reflect playbook retrieval loop requirements and rule schema
- [Solution: Task 019 — Playbook Retrieval Loop](docs/tasks/019-playbook-retrieval-loop/solution.md): Solution for the auto-reflect playbook retrieval loop: append-only JSONL event store sharded by host+day+worktree, rule lifecycle commands, gate check, retrieve, and observation capture commands. Read when: implementing the auto-reflect event store, rule lifecycle, or retrieve/gate commands

**docs/tasks/020-auto-hooks-install**

- [Context: Auto Hooks Install (Task 020)](docs/tasks/020-auto-hooks-install/context.md): Codebase context for the auto hooks install command, with precise references for hookscmd.go, JSON config helpers, git.RepoRoot, the agent hook shapes for Claude and Codex, and the test harness pattern. Read when: implementing auto hooks install or locating the hookscmd registration point and JSON merge patterns
- [Feedback: Task 020 — Auto Hooks Install](docs/tasks/020-auto-hooks-install/feedback.md): Post-implementation reflections on the auto hooks install task: stray .codex file shadowing, pre-existing gofmt drift on main, and the generic map merge design for lossless field preservation. Read when: implementing hooks installation or debugging .codex/.claude directory creation issues
- [Plan: Task 020 — Auto Hooks Install](docs/tasks/020-auto-hooks-install/plan.md): Implementation plan for the auto hooks install subcommand: merging fire hook commands into .claude/settings.json and .codex/hooks.json idempotently for all documented hook events. Read when: implementing or reviewing the auto hooks install command plan and hook-merging logic
- [Requirements: Task 020 — Auto Hooks Install](docs/tasks/020-auto-hooks-install/requirements.md): Requirements for auto hooks install: automatically wiring auto hooks fire as a command hook for a curated telemetry-safe allowlist of Claude Code and Codex events, merging idempotently into existing project-local hook config. Read when: implementing auto hooks install or extending the hook event allowlist
- [Solution: Auto Hooks Install (Task 020)](docs/tasks/020-auto-hooks-install/solution.md): Solution for auto hooks install: generic map[string]any JSON merge for both Claude (.claude/settings.json) and Codex (.codex/hooks.json) hook configs, preserving all existing fields including unknown handler fields like timeout and statusMessage. Read when: implementing the hooks install command or understanding the lossless generic JSON merge approach for agent hook configs

**docs/tasks/021-auto-bus-standard**

- [Context: Task 021 — Auto Bus Standard](docs/tasks/021-auto-bus-standard/context.md): Verified codebase facts grounding the auto-bus implementation: auto-ui JSON-RPC/WebSocket transport, auto-shared/git provenance helpers, auto-cli hook producer, and SPA consumer signatures. Read when: implementing the auto-bus standard or understanding its codebase dependencies
- [Feedback: Task 021 — Auto Bus Standard](docs/tasks/021-auto-bus-standard/feedback.md): Post-implementation feedback for the auto bus standard task: worktree rebase conflict with untracked planning docs, registry hermeticity in tests, and security fixes for XSS, DNS-rebinding, and timestamp parsing. Read when: reviewing auto bus standard implementation lessons or understanding CloudEvents envelope and JSON-RPC security considerations
- [Plan: Task 021 — Auto Bus Standard](docs/tasks/021-auto-bus-standard/plan.md): Implementation plan for the auto-bus standard: auto-shared/bus package (CloudEvents envelope, Hub broadcast, doc.changed derivation), auto-ui POST /api/rpc ingest, WebSocket broadcast, doc.list/get RPCs, hooks fire migration, live-reload doc view, and spec. Read when: implementing the auto-bus event system, adding new event types, or wiring hooks fire to the bus
- [Requirements: Auto Bus Standard (Task 021)](docs/tasks/021-auto-bus-standard/requirements.md): Requirements for the unified auto-bus communication standard: CloudEvents-shaped envelope, JSON-RPC 2.0 framing, HTTP and WebSocket transports, at-most-once delivery, doc.changed derivation, live doc reload, and hooks fire migration to the standard envelope. Read when: implementing the auto-bus standard, extending bus event types, or understanding the wire format and delivery guarantees
- [Solution: Task 021 — Auto Bus Standard](docs/tasks/021-auto-bus-standard/solution.md): CloudEvents-shaped bus envelope in auto-shared/bus, workspace provenance capture at the hook producer, Hub fan-out in auto-ui, doc.changed derivation, and live-reload doc view. Read when: implementing the auto-bus standard envelope, hub, or doc live-reload view

**docs/tasks/022-hook-event-log**

- [Context: Task 022 — Hook Event Log](docs/tasks/022-hook-event-log/context.md): Verified codebase facts for implementing the durable hook event log: producer side in auto-cli hookscmd.go where verbatim raw bytes are available, and key file locations for the append path. Read when: implementing or reviewing the hook event log feature and understanding where hook payloads are produced in auto-cli
- [Plan: Task 022 — Hook Event Log](docs/tasks/022-hook-event-log/plan.md): Implementation plan for a durable daily-partitioned JSONL hook log written by auto hooks fire, and a new incremental hooks ETL source that ingests it into monthly-partitioned parquet using the proven git-ETL watermark pattern. Read when: implementing hook event logging or the hooks ETL ingest pipeline
- [Requirements: Hook Event Log (Task 022)](docs/tasks/022-hook-event-log/requirements.md): Requirements for adding a durable append-only JSONL hook event log to auto hooks fire, plus an auto-etl hooks source that ingests the log into a normalized parquet dataset with raw_json preservation. Read when: implementing the hook event log or understanding the durable capture and ETL ingestion requirements for agent hook payloads
- [Solution: Task 022 — Hook Event Log](docs/tasks/022-hook-event-log/solution.md): Durable hook event log design: auto-shared/hooks envelope format, append-first producer, incremental ETL consumer with watermark sync-state, and monthly merge-by-ID parquet writer. Read when: implementing durable hook event logging or the auto-etl hooks ingest pipeline

**docs/tasks/023-reflect-miner-queue**

- [Context: Task 023 — Reflect Miner Queue](docs/tasks/023-reflect-miner-queue/context.md): Verified codebase facts for implementing the reflect miner queue: event log type constants, envelope schema, ETL parquet reader locations, and reflect stats command surface. Read when: implementing or reviewing the reflect miner queue feature and understanding the event log and parquet schema touchpoints
- [Plan: Task 023 — Reflect Miner Queue](docs/tasks/023-reflect-miner-queue/plan.md): Implementation plan for hoisting the parquet schema into auto-shared/model, then building a miner command group in auto-reflect that reads session parquet, ranks unmined sessions by text signals, and tracks coverage via session_mined events. Read when: implementing the reflect miner queue or understanding the auto-shared/model schema relocation
- [Requirements: Reflect Miner Queue (Task 023)](docs/tasks/023-reflect-miner-queue/requirements.md): Requirements for the auto reflect miner command group: a deterministic work-queue with priority scoring over coding sessions, append-only coverage tracking via the reflect event log, GitRemote-scoped session filtering, and status visibility. Read when: implementing the reflect miner queue or understanding session priority scoring, ack versioning, and coverage tracking requirements
- [Solution: Task 023 — Reflect Miner Queue](docs/tasks/023-reflect-miner-queue/solution.md): Design for auto-reflect miner queue: shared-model extraction to auto-shared/model, session_mined event type, priority scoring from message signals, and miner next/ack/status/describe CLI. Read when: implementing the auto-reflect miner queue or the shared parquet schema extraction
<!-- autodoc: end -->

**auto search** — Search past coding agent sessions. Run `auto search quickstart` to learn more.

**auto skill** — Author and lint reusable agent skills. Run `auto skill quickstart` to learn more.

**auto env** — Standalone dev environments for worktree branches. Run `auto env quickstart` to learn how to stand up an isolated environment.
