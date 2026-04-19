---
hash: "a99c802f"
id: "91bc2e67"
read_when: "when implementing autoskill mining, search, and skill distribution"
summary: "Technical reference for the meta_skill Rust CLI: architecture, data model, mining pipeline, search, security, and distribution"
title: "Meta Skill (ms) — Reference"
---

# Meta Skill (ms)

Local-first skill management platform written in Rust. Turns operational knowledge (from coding sessions, docs, manual input) into structured, searchable, reusable skill artifacts.

- **Author:** Jeffrey Emanuel
- **Repo:** github.com/Dicklesworthstone/meta_skill
- **License:** MIT with OpenAI/Anthropic Rider
- **Language:** Rust 2024 edition (requires 1.85+)

## What It Does

- Mines coding agent session transcripts (via CASS) for reusable patterns
- Stores skills as structured SKILL.md files with YAML frontmatter
- Indexes and searches skills via hybrid BM25 + semantic search
- Learns user preferences via Thompson sampling bandit
- Packages and distributes skills as signed bundles
- Runs as MCP (Model Context Protocol) server for direct agent integration
- Detects anti-patterns from session failures

## Architecture Overview

```
CASS Sessions ──→ Mining Pipeline ──→ SkillSpec ──→ SQLite + Git Archive
                                                         │
                                          ┌──────────────┼──────────────┐
                                          ▼              ▼              ▼
                                    BM25 Index    Vector Index    Bandit State
                                          │              │              │
                                          └──────┬───────┘              │
                                                 ▼                      ▼
                                           RRF Fusion            Suggestions
                                                 │                      │
                                                 └──────────────────────┘
                                                          │
                                                    CLI / MCP Server
```

## Dual Persistence

- **SQLite** — fast queries, FTS5 full-text search, metadata, quality scores
- **Git archive** — immutable history, audit trails, rollback, version diffs
- Two-phase commit coordination between both stores via `TxManager`

## Data Model

### Core Types (`src/core/skill.rs`)

- `SkillSpec` — full skill specification (metadata + sections + inheritance)
- `SkillSection` — grouped content under a title (id, title, blocks)
- `SkillBlock` — atomic content unit with `BlockType`:
  - Command, CodeBlock, Prose, Table, Note, Warning, Example, TestCase
- `SkillLayer` — hierarchy: Base → Org → Project → User (higher overrides lower)
- `EvidenceLevel` — Proven, Observational, Theoretical, Unvetted

### Skill Slicing (`src/core/slicing.rs`)

Skills are decomposed into token-aware `SkillSlice` units for progressive disclosure:

| SliceType | Utility Score | Description |
|-----------|--------------|-------------|
| Policy | 0.95 | Cannot be removed under budget pressure |
| Rule | 0.90 | Core behavioral guidance |
| Pitfall | 0.85 | What to avoid |
| Checklist | 0.75 | Step-by-step procedures |
| Command | 0.70 | CLI/tool invocations |
| Example | 0.65 | Concrete demonstrations |
| Overview | 0.55 | High-level context |
| Reference | 0.40 | Links and citations |

Token estimation: `chars / 4` (conservative). Slices packed by priority within a token budget.

### Meta-Skills (`src/meta_skills/`)

Composed bundles that pull slices from multiple base skills:

- `MetaSkill` — bundle definition with slice refs, pin strategy, token budgets
- `MetaSkillSliceRef` — references specific slices with level (Core/Extended/Deep) and priority
- `PinStrategy` — LatestCompatible, ExactVersion, FloatingMajor, LocalInstalled, PerSkill
- `SliceCondition` — conditional inclusion: TechStack, FileExists, EnvVar, DependsOn
- Loaded via `MetaSkillManager`: resolves refs → evaluates conditions → packs within budget

### Skill Resolution (`src/core/resolution.rs`)

- **Single inheritance** via `extends` — child merges parent sections, with replace flags per section type
- **Composition** via `includes` — pull specific sections from other skills (prepend/append)
- Cycle detection with MAX_INHERITANCE_DEPTH = 5

### Anti-Patterns (`src/antipatterns/types.rs`)

Negative patterns mined from session failures:

- `AntiPattern` — description, confidence, evidence, failure modes, negative rules
- `NegativeRule` — severity: Advisory (1 evidence), Warning (2), Blocking (3+)
- `RollbackType` — GitReset, GitRevert, FileRestore, ManualUndo, ExplicitCorrection
- `FailureSignalType` — TestFailure, BuildError, RuntimeException, UserRejection, Timeout

## Mining Pipeline

### Build Command (`src/cli/commands/build.rs`)

State machine phases:

```
SearchSessions (15%)
  → QualityFilter (15%)
  → ExtractPatterns (30%)
  → FilterPatterns (15%)
  → Synthesize (25%)
  → Complete
```

Quality gates: `min_session_quality` and `min_pattern_confidence` (0.0–1.0).

### Pattern Extraction (`src/cass/mining.rs`)

- `PatternType` — CommandPattern, CodePattern, WorkflowPattern, DecisionPattern, ErrorPattern, RefactorPattern, ConfigPattern, ToolPattern
- Each `ExtractedPattern` has confidence, frequency, evidence refs, taint labels
- Sessions segmented into phases for structured extraction

### Skill Synthesis (`src/cass/synthesis.rs`)

- Transforms patterns → `SkillDraft` (name, description, content, tags)
- Confidence-weighted content organization
- Pattern type drives skill naming and tag generation

### Brenner Method (`src/cass/brenner.rs`)

Guided mining wizard using cognitive move extraction:

- **8 operator algebra tags:** ProblemSelection, HypothesisSlate, ThirdAlternative, IterativeRefinement, RuthlessKill, Quickie, MaterializationInstinct, InnerTruth
- Wizard state machine: SessionSelection → MoveExtraction → ThirdAlternativeGuard → SkillFormalization → MaterializationTest → Complete
- Checkpointable and resumable

### Import Pipeline (`src/import/`)

Analyzes unstructured text and generates SkillSpec:

- `ContentParser` splits text into logical blocks
- 6 classifiers run in sequence: Metadata, Example, Checklist, Pitfall, Rule, Context
- Each returns confidence (0.0–1.0) and detected signals
- `SkillGenerator` deduplicates, organizes sections, returns GeneratedSkill with warnings

## Search Engine (`src/search/`)

### Hybrid Architecture

- **BM25** — Tantivy full-text index over name, description, content, tags
- **Semantic** — Hash embeddings (FNV-1a, 384 dims, deterministic, no external models)
- **RRF** (Reciprocal Rank Fusion) — combines both rankings with configurable weights
- **Filters** — tags, layer, min quality, deprecation status
- **LRU cache** — query results and embeddings

### Search Index Concurrency

- Dual-mode: writable primary + read-only fallback
- Handles concurrent MCP server access without lock contention

## Suggestion Engine (`src/suggestions/`)

- **Thompson sampling bandit** learns from user feedback
- Feature extraction: 18-dimensional context vector
- Context capture: project type, open files, environment, tools
- Cooldown cache prevents repeating recent suggestions
- Modes: contextual relevance, bandit scoring, exploration bonus, personal history weighting

### Project Detection (`src/context/detector.rs`)

Scans for marker files to identify project type:

- Rust (Cargo.toml), Node (package.json), Python (pyproject.toml), Go (go.mod), Java (pom.xml), etc.
- Each marker has a confidence score (0.90–0.95)
- Returns sorted list of detected project types

## Security (`src/security/`)

### ACIP — Agent Content Injection Prevention

- Classifies content by source: UserMessage, AssistantMessage, ToolOutput, FileContent
- Trust levels: Implicit (trust), Explicit (require approval), Quarantine (block)
- Injection pattern detection and sensitive data scanning
- Quarantine audit trail

### DCG — Deterministic Command Gate

- Command safety classification: Allowed, Denied, RequiresApproval
- Audit logging of all safety events

### Secret Scanner

- Detects API keys, passwords, tokens, connection strings
- `scan_secrets()` / `redact_secrets()` pipeline

### Path Safety

- Symlink escape prevention
- Directory traversal blocking
- Path normalization and validation

## Bundler & Distribution (`src/bundler/`)

- **Bundle format** — manifest (TOML/YAML) + content-addressed blob store
- **Ed25519 signing** — cryptographic bundle integrity verification
- **Conflict detection** — hash-based file status (Modified, Untracked, Deleted, Conflicted)
- **Install strategies** — Skip, Overwrite, Merge, Abort, Interactive
- **GitHub integration** — download bundles from GitHub releases
- **Size limits** — manifest 1MB, max 10K blobs, 100MB per blob

## Integration Points

### MCP Server (`src/cli/commands/mcp.rs`)

- Stdio and optional TCP transport
- ANSI output stripping and JSON validation for safety
- Agents interact as first-class citizens

### CASS Client (`src/cass/client.rs`)

- Session search, expansion, and fingerprinting
- Incremental processing via `FingerprintCache`
- Health checking and capability detection

### Agent Detection (`src/agent_detection/`)

Detects installed agents: Claude Code, Codex, Gemini CLI, Cursor, Cline, OpenCode, Aider, Windsurf, Continue

- Detection methods: config files, binaries, running processes, env vars, VSCode extensions
- Integration status: NotConfigured → PartiallyConfigured → FullyConfigured → Outdated

## CLI Commands

| Category | Commands |
|----------|----------|
| Init & Config | `ms init`, `ms config`, `ms doctor` |
| Indexing | `ms index`, `ms list`, `ms show` |
| Search | `ms search "query"` (hybrid, bm25, semantic) |
| Mining | `ms build` (autonomous/guided/TUI) |
| Suggestions | `ms suggest`, `ms load --auto` |
| Quality | `ms quality`, `ms lint`, `ms validate` |
| Graph | `ms graph insights`, `ms graph cycles`, `ms graph keystones` |
| Security | `ms security scan`, `ms safety check` |
| Bundles | `ms bundle create`, `ms bundle install` |
| Sync | `ms remote add`, `ms sync` |
| Anti-patterns | `ms antipatterns` |
| MCP | `ms mcp serve` (stdio or `--port`) |

## Configuration (`src/config.rs`)

Layered config loading: explicit path → env var → global (~/.config/ms/config.toml) → project (.ms/config.toml) → env overrides.

Key config sections:
- `skill_paths` — global, project, community, local discovery paths
- `layers` — priority order, auto-detect, project overrides
- `disclosure` — default level, token budget, auto-suggest, cooldown
- `search` — backend, embedding dimensions, BM25/semantic weights
- `cass` — auto-detect, path, session pattern
- `security` — ACIP trust boundaries
- `safety` — DCG binary path, explain format

## Key Dependencies

- `clap` — CLI parsing
- `tokio` — async runtime
- `rusqlite` — SQLite with FTS5
- `tantivy` — full-text search
- `git2` — Git operations
- `serde` / `serde_json` / `toml` / `serde_yaml` — serialization
- `ratatui` — TUI framework
- `rayon` — parallelism
- `keyring` — credential storage

## Relevance to auto-skill

meta_skill is a reference implementation for skill management. Key patterns applicable to autoskill:

- **Skill slicing with utility scores** — progressive disclosure under token budgets
- **Hybrid search** — BM25 + embeddings with RRF fusion (no external model dependency)
- **CASS integration** — mining patterns from coding session transcripts
- **Layered resolution** — Base → Org → Project → User override hierarchy
- **Anti-pattern mining** — extracting negative patterns from failures
- **Bundle format** — signed, content-addressed skill distribution
