---
hash: "ef2ee1f7"
id: "6356cfa1"
summary: "High-level product overview of the Auto platform: architecture, data format, tool suite, query examples, security model, and roadmap."
title: "Auto — Agentic Coding Intelligence Platform"
---

# Auto — Agentic Coding Intelligence Platform

> Observability, memory, and continuous improvement for AI coding agents — from a single developer's laptop to an enterprise fleet.

---

## Overview

Modern AI coding agents (Claude Code, Codex, Cursor, etc.) produce a rich stream of behavioural data: every file read, every tool call, every decision, every failure. This data is currently siloed per-tool, per-machine, and effectively discarded.

**Auto** captures, normalises, and indexes this data into a shared intelligence layer that compounds in value over time. At the individual level it surfaces patterns, builds institutional memory, and generates reusable skills. At the team level it becomes an observability and improvement platform for the entire agentic coding fleet.

---

## Problem

### For individual developers
- Coding agents repeat the same mistakes across sessions with no memory of past approaches
- No visibility into which files agents struggle with, which docs are unhelpful, which patterns recur
- Skills and playbooks are hand-authored and quickly go stale
- 6 months of agent session data sits in `~/.claude/projects` effectively unqueried

### For engineering teams
- No cross-agent, cross-developer visibility into where agents succeed or fail
- Onboarding a new agent to a codebase repeats the same discovery process every time
- Documentation quality has no feedback loop from actual agent usage
- No way to propagate learned patterns from one developer's sessions to the team

### For enterprises
- Agent coding tools are proliferating but the observability layer doesn't exist
- Compliance and audit requirements for AI-assisted development are unmet
- No vendor-neutral data layer — locked into whatever each tool vendor provides

---

## Solution

Auto is a suite of small, composable CLI tools that communicate via a shared file-based data format. Each tool does one thing. Together they form a pipeline from raw agent logs to actionable intelligence.

```
raw agent logs
  → auto-etl      normalise + store
    → auto-search   index + query
      → auto-reflect  find patterns
        → auto-skill    generate skills
```

No shared database. No running server required. Each tool depends only on files with versioned schemas.

---

## Architecture

### Design principles

- **Local-first.** Works entirely offline on a single machine. Cloud sync is optional.
- **Vendor-neutral.** Ingests Claude Code, Codex, Cursor, or any future agent via pluggable parsers.
- **File-based contracts.** Tools communicate via Parquet files with versioned schemas, not shared databases or APIs.
- **Append-only where possible.** Raw logs are never modified. Derived data is rebuilt from source.
- **Content-addressed storage.** File content stored by hash — automatic deduplication across sessions.
- **S3-compatible.** The same partition layout that works locally works on S3 with no schema changes.

### Data flow

```
~/.claude/projects/          ~/.codex/sessions/         (other agents)
         │                          │
         └──────────────────────────┘
                      │
                 [auto-etl]
                      │
         ┌────────────┴────────────┐
         │                         │
  normalized/                  raw/
  sessions/*.parquet            (untouched originals)
  messages/*.parquet
  blobs/*/blobs.parquet
  manifest.json
         │
    [auto-search]
         │
    index/
    search.db   (SQLite FTS5)
    vectors.db  (sqlite-vec)
         │
    [auto-reflect]
         │
    reflect/
    patterns/*.md
    suggestions/*.md
         │
    [auto-skill]
         │
    skills/
    *.md   (agent skill files)
```

### Multi-host / S3 architecture

```
[macbook]     auto-etl → s3://bucket/host=macbook/...
[hetzner-vps] auto-etl → s3://bucket/host=hetzner-vps/...
[ec2-agent]   auto-etl → s3://bucket/host=ec2-agent/...

                    ↓ (no coordination between hosts)

[analytics server]
  auto-search reads s3://bucket/host=*/**
  builds unified FTS + vector indexes
  auto-reflect runs against unified index
  auto-skill publishes back to s3://bucket/skills/
```

Each host writes only to its own prefix. No locking, no coordination. S3 is the bus.

---

## Data Format

### Storage layout

```
~/.auto/etl/
  raw/
    host=macbook/claude/session-abc.jsonl    (untouched originals)
    host=macbook/codex/session-def.jsonl

  normalized/
    sessions/
      year=2025/month=03/sessions.parquet
    messages/
      year=2025/month=03/week=10/messages.parquet
      year=2025/month=03/week=11/messages.parquet
      year=2025/month=03/week=12/
        messages.parquet                     (committed)
        staging/                             (current ETL run)
          batch-20250316-1400.parquet
    blobs/
      year=2025/month=03/prefix=de/blobs.parquet
      year=2025/month=01/prefix=de/blobs.parquet  (archived, immutable)
      bloom.bin                              (global hash existence filter)
      dict.zst                              (shared zstd compression dictionary)
    manifest.json
```

### Partition strategy

| Type | Partition | Rationale |
|------|-----------|-----------|
| Sessions | year + month | Small rows, monthly rewrite is trivial |
| Messages | year + month + ISO week | Caps rewrite cost, DuckDB prunes efficiently |
| Blobs | year + month + hash prefix (2 chars) | Content-addressed, 256 natural shards |

### Parquet schemas

All timestamps stored as unix microseconds (DuckDB `epoch_us` compatible). String columns with high repetition use dictionary encoding (`dict` tag).

**SessionRow**
```go
type SessionRow struct {
    ID         string `parquet:"id"`
    HostID     string `parquet:"host_id,dict"`
    Agent      string `parquet:"agent,dict"`
    Model      string `parquet:"model,dict"`
    Workspace  string `parquet:"workspace,dict"`
    SourcePath string `parquet:"source_path"`

    FirstMessageAt  int64 `parquet:"first_message_at"`
    LastMessageAt   int64 `parquet:"last_message_at"`
    DurationSeconds int64 `parquet:"duration_seconds"`

    TotalInputTokens  int64 `parquet:"total_input_tokens"`
    TotalOutputTokens int64 `parquet:"total_output_tokens"`
    TotalCacheTokens  int64 `parquet:"total_cache_tokens"`
    TotalBytes        int64 `parquet:"total_bytes"`
    TotalInputBytes   int64 `parquet:"total_input_bytes"`
    TotalOutputBytes  int64 `parquet:"total_output_bytes"`

    MessageCount    int32 `parquet:"message_count"`
    ToolCallCount   int32 `parquet:"tool_call_count"`
    UniqueFileCount int32 `parquet:"unique_file_count"`
    IsAbandoned     bool  `parquet:"is_abandoned"`

    Year          int32 `parquet:"year"`
    Month         int32 `parquet:"month"`
    SchemaVersion int32 `parquet:"schema_version"`
}
```

**MessageRow**
```go
type MessageRow struct {
    ID        string `parquet:"id"`
    SessionID string `parquet:"session_id,dict"`
    HostID    string `parquet:"host_id,dict"`
    Index     int32  `parquet:"index"`

    Role      string `parquet:"role,dict"`
    Content   string `parquet:"content"`        // truncated to 512 bytes
    Timestamp int64  `parquet:"timestamp"`

    ToolName    string `parquet:"tool_name,dict"`
    ToolFile    string `parquet:"tool_file,dict"`
    BashCommand string `parquet:"bash_command"`
    ToolInput   string `parquet:"tool_input"`    // truncated JSON

    BlobHash    string `parquet:"blob_hash"`     // sha256 hex, empty if not offloaded
    BlobSize    int64  `parquet:"blob_size"`
    IsTruncated bool   `parquet:"is_truncated"`

    InputTokens      int32 `parquet:"input_tokens"`
    CacheInputTokens int32 `parquet:"cache_input_tokens"`
    OutputTokens     int32 `parquet:"output_tokens"`

    Year          int32 `parquet:"year"`
    Month         int32 `parquet:"month"`
    SchemaVersion int32 `parquet:"schema_version"`
}
```

**BlobRow**

Parquet's columnar format means metadata queries never read the `Content` or `Embedding` columns — they are skipped entirely unless explicitly selected.

```go
type BlobRow struct {
    Hash           string `parquet:"hash"`
    Content        []byte `parquet:"content"`         // zstd compressed with dict.zst
    Size           int64  `parquet:"size"`
    CompressedSize int64  `parquet:"compressed_size"`

    FilePath    string `parquet:"file_path,dict"`
    FileExt     string `parquet:"file_ext,dict"`
    Language    string `parquet:"language,dict"`
    ContentType string `parquet:"content_type,dict"`

    FirstSeenAt  int64 `parquet:"first_seen_at"`
    LastSeenAt   int64 `parquet:"last_seen_at"`
    AccessCount  int32 `parquet:"access_count"`
    SessionCount int32 `parquet:"session_count"`

    HasEmbedding   bool   `parquet:"has_embedding"`
    EmbeddingModel string `parquet:"embedding_model,dict"`
    EmbeddingDims  int32  `parquet:"embedding_dims"`
    Embedding      []byte `parquet:"embedding"`       // float32 LE bytes

    HashPrefix    string `parquet:"hash_prefix"`      // first 2 hex chars
    Year          int32  `parquet:"year"`
    Month         int32  `parquet:"month"`
    SchemaVersion int32  `parquet:"schema_version"`
}
```

### Blob store

File content is stored content-addressed by SHA-256 hash. Same content read 200 times across sessions is stored once. Properties:

- Immutable by definition — same hash, same content, never updated
- Naturally deduplicated across sessions and hosts
- Compressed with a shared zstd dictionary trained on the corpus (6-8x compression vs uncompressed)
- Existence checks via bloom filter before Parquet scan
- Current month: individual files in staging, compacted to Parquet weekly
- Past months: immutable Parquet partitions, never rewritten

### Manifest

Written by `auto-etl` after each run. Read by `auto-search` to determine what needs re-indexing.

```json
{
  "v": 1,
  "last_run_at": 1710000000000000,
  "host_id": "macbook",
  "sessions_processed": 1247,
  "messages_processed": 84201,
  "blobs_stored": 12483,
  "blobs_deduped": 89201,
  "latest_session_at": "2025-03-16T14:00:00Z",
  "latest_message_at": "2025-03-16T14:22:00Z",
  "latest_blob_at": "2025-03-16T14:22:00Z",
  "dict_path": "blobs/dict.zst",
  "truncate_threshold": 512,
  "is_full_transform": false
}
```

---

## Tools

### auto-etl

Ingests raw agent session logs, normalises to the common schema, writes Parquet + blob files. Non-destructive — never modifies originals.

**Commands**
```
auto-etl init                  global setup, creates ~/.auto/host.json
auto-etl init --project        project-local setup
auto-etl run                   incremental transform (default)
auto-etl run --full            full retransform from raw
auto-etl run --agent claude    only process claude sessions
auto-etl watch                 watch for new sessions, run incrementally
auto-etl doctor                check config, report problems as JSON
auto-etl quickstart            LLM-friendly usage guide
auto-etl docs                  full command reference
```

**Supported agents (v1)**
- Claude Code (`~/.claude/projects/**/*.jsonl`)
- Codex (path configurable)

**Redaction**

Known secret patterns are redacted before writing blobs. Redacted blobs carry `IsRedacted=true` and `RedactedCount`. Raw files under `raw/` always retain full fidelity — only the normalised blobs are redacted. S3 sync sends only normalised (redacted) data; raw stays local.

Redacted patterns (v1):
- AWS access keys (`AKIA[0-9A-Z]{16}`)
- OpenAI/Anthropic style keys (`sk-[a-zA-Z0-9]{32,}`)
- Generic `key=value` patterns with high-entropy values
- `.env` file content (flagged by filename)

### auto-search

Reads normalised Parquet files, builds and maintains search indexes. Never writes to `normalized/`.

**Commands**
```
auto-search init               setup index directory
auto-search index              incremental index from manifest watermark
auto-search index --full       full reindex
auto-search query "text"       FTS query, returns matching messages
auto-search similar "text"     semantic similarity search via embeddings
auto-search files              file access heatmap across sessions
auto-search files --path src/  heatmap for a subtree
auto-search watch              watch manifest, auto-reindex on changes
auto-search doctor
auto-search quickstart
auto-search docs
```

**Index types**

| Index | Store | Purpose |
|-------|-------|---------|
| Full-text | SQLite FTS5 | Message content, tool inputs, file paths |
| Vector | sqlite-vec | Semantic search over blob content |
| Metadata | SQLite | Session/message analytics queries |

**Blob content search**

Given any text snippet, find all sessions and files where that content (or semantically similar content) was read or written:

1. Hash exact match — instant, zero cost
2. FTS5 token match — handles whitespace/minor edits
3. Embedding similarity — handles rewrites, renames, restructuring

Deduplication: same hash = one embedding, regardless of how many times it was accessed.

### auto-reflect

Queries the search index to find patterns, generate insights, and suggest improvements. Produces markdown reports.

**Commands**
```
auto-reflect run               full reflection pass
auto-reflect run --since 7d    only consider recent sessions
auto-reflect patterns          recurring tool call sequences
auto-reflect docs              doc quality signals
auto-reflect files             file complexity heatmap
auto-reflect failures          sessions that ended without completing
auto-reflect watch             trigger reflection after each ETL run
auto-reflect doctor
auto-reflect quickstart
auto-reflect docs
```

**Reflection signals**

- Files read many times before a write → high cognitive load, complexity signal
- Files read then immediately followed by a web search → doc is missing something
- Same tool call sequence repeated across sessions → candidate for a skill
- Sessions that read docs then still fail → docs are unclear
- Files that are never read → undiscoverable
- Files always read together → should be merged or cross-linked

### auto-skill

Turns reflection outputs into reusable agent skill files (SKILL.md format).

**Commands**
```
auto-skill generate            generate skills from latest reflection
auto-skill list                list existing skills
auto-skill show <name>         display a skill
auto-skill publish             push skills to shared location (S3 or local)
auto-skill doctor
auto-skill quickstart
auto-skill docs
```

---

## CLI Conventions

All tools follow the same conventions:

- Default output is human-readable text
- `--json` on any command returns structured JSON
- `init` — global setup
- `init --project` — project-local setup
- `quickstart` — LLM-friendly markdown, happy-path end-to-end usage
- `docs` — full reference, all commands in one output
- `doctor` — configuration check, returns JSON with detailed problem descriptions
- `watch` — file-watching continuous mode

**Config files**
```
~/.auto/host.json               host identity, S3 config
~/.auto/{etl,search,...}/settings.json   global tool settings
.auto/{etl,search,...}/project.json      project-local overrides
```

---

## Query Examples

All of these work identically against local files or S3:

```sql
-- Which files does the agent read most often?
SELECT tool_file, COUNT(*) as reads, COUNT(DISTINCT session_id) as sessions
FROM read_parquet('~/.auto/etl/normalized/messages/**/*.parquet')
WHERE tool_name = 'Read'
GROUP BY tool_file
ORDER BY reads DESC
LIMIT 20

-- Token usage trend by week
SELECT year, month, week, SUM(input_tokens + output_tokens) as total_tokens
FROM read_parquet('~/.auto/etl/normalized/messages/**/*.parquet')
WHERE role = 'assistant'
GROUP BY year, month, week
ORDER BY year, month, week

-- Sessions that accessed a specific file
SELECT DISTINCT session_id, first_message_at
FROM read_parquet('~/.auto/etl/normalized/messages/**/*.parquet')
WHERE tool_file LIKE '%payments.go%'
ORDER BY first_message_at DESC

-- Cross-host file access comparison (S3)
SELECT host_id, tool_file, COUNT(*) as reads
FROM read_parquet('s3://bucket/host=*/messages/**/*.parquet')
WHERE tool_name = 'Read'
GROUP BY host_id, tool_file
ORDER BY reads DESC
```

---

## Security & Privacy

### Redaction tiers

| Tier | What | Where |
|------|------|-------|
| Raw | Full fidelity, no redaction | `raw/` local only, never synced |
| Normalised | Known secrets redacted | `normalized/` local + S3 |
| Indexed | Same as normalised | `index/` local or analytics server |

### S3 access model

- Each host authenticates to S3 independently
- IAM policy limits each host to its own prefix (`host={hostname}/*`)
- Analytics server has read-only access to `host=*/**`
- Skills bucket is write-accessible to analytics server, read-only to hosts

### Audit trail

- `IsRedacted` and `RedactedCount` on every blob row
- `manifest.json` records every ETL run with counts and timestamps
- Raw logs are never modified — full reconstruction from source always possible

---

## Git Integration

### Overview

Agent session data captures what the agent did. Git captures what actually shipped. Joining the two unlocks a class of insights neither source can provide alone — most importantly, a direct empirical measure of agent output quality.

### What git adds

- Ground truth on which changes were committed vs abandoned
- Commit messages as human-authored intent labels on top of raw diffs
- Branch and PR context — feature, fix, or refactor
- A canonical project timeline to anchor session activity against

### Key joins

**Session → commit:** which sessions resulted in a commit?
```sql
SELECT s.id, s.first_message_at, c.hash, c.message
FROM sessions s
JOIN commits c
  ON c.committed_at BETWEEN s.first_message_at AND s.last_message_at
JOIN commit_files cf ON cf.commit_hash = c.hash
WHERE cf.file_path LIKE '%payments.go%'
```

**Blob → commit file:** did the agent's output survive into the commit unchanged?
```sql
SELECT m.tool_file, m.blob_hash, cf.blob_hash AS committed_hash
FROM messages m
JOIN commit_files cf ON cf.file_path = m.tool_file
WHERE m.tool_name = 'Write'
  AND m.session_id = 'macbook/claude/abc123'
```

When `m.blob_hash = cf.blob_hash` the agent's output was accepted verbatim. Divergence indicates the human edited it after the agent wrote it.

### Agent output quality signal

Over time, tracking what fraction of agent-written content survives into commits unchanged gives a direct empirical quality metric — by file type, by agent model, by developer, by time period. This compounds as the dataset grows and is not available from any other source.

### Queries this enables

- "Which sessions actually resulted in a commit?" — agent success rate
- "How many agent iterations preceded this commit?" — complexity signal per change
- "Which files did the agent struggle with in this commit?" — correlate effort to output
- "Did the agent's final write match what was committed?" — quality signal
- "Find sessions related to this commit message" — natural language → code history
- "Which agent-written files are never committed?" — identify low-value work patterns

### Parquet schemas

**CommitRow** — one row per git commit
```go
type CommitRow struct {
    Hash         string `parquet:"hash"`
    RepoPath     string `parquet:"repo_path,dict"`
    HostID       string `parquet:"host_id,dict"`
    AuthorEmail  string `parquet:"author_email,dict"`
    Message      string `parquet:"message"`
    CommittedAt  int64  `parquet:"committed_at"`
    FilesChanged int32  `parquet:"files_changed"`
    Insertions   int32  `parquet:"insertions"`
    Deletions    int32  `parquet:"deletions"`
    Year         int32  `parquet:"year"`
    Month        int32  `parquet:"month"`
    SchemaVersion int32 `parquet:"schema_version"`
}
```

**CommitFileRow** — one row per file touched per commit
```go
type CommitFileRow struct {
    CommitHash  string `parquet:"commit_hash"`
    FilePath    string `parquet:"file_path,dict"`
    ChangeType  string `parquet:"change_type,dict"` // "add", "modify", "delete", "rename"
    Insertions  int32  `parquet:"insertions"`
    Deletions   int32  `parquet:"deletions"`
    BlobHash    string `parquet:"blob_hash"` // content after change → joins to blob store
    Year        int32  `parquet:"year"`
    Month       int32  `parquet:"month"`
    SchemaVersion int32 `parquet:"schema_version"`
}
```

`CommitFileRow.BlobHash` is the critical join key — it links git history directly to the blob store, connecting committed file versions to every agent session that read or wrote the same content.

### Storage layout

```
normalized/
  commits/
    year=2025/month=03/commits.parquet
  commit_files/
    year=2025/month=03/commit_files.parquet
```

### Commit message embeddings

Commit messages are human-written descriptions of intent. Embedding them alongside session data bridges natural language queries to code history:

```
auto-search query "fix race condition in payment processor"
→ finds commits with semantically similar messages
→ finds sessions that touched those files in that time window
→ shows exactly what the agent read and wrote during that work
```

### `auto-etl` git ingestion

Git ingestion runs alongside agent log ingestion. Incremental by default — only processes commits since the last run watermark.

```
auto-etl run --git                   ingest git history for current repo
auto-etl run --git --repo /path/to   ingest a specific repo
auto-etl run --git --since 30d       only process recent commits
```

Requires the repo to be accessible on the current host. For multi-host setups each host ingests the repos it has access to; the unified view is assembled by `auto-search` across all host prefixes.

---

## Roadmap

### v0.1 — Local single developer
- `auto-etl` Claude Code ingestion
- Parquet output with session + message schemas
- Basic blob store with deduplication
- `auto-search` FTS5 indexing
- `auto-search files` heatmap

### v0.2 — Multi-agent + analytics
- Codex ingestion
- DuckDB query examples and helper commands
- `auto-reflect` first patterns
- S3 sync support
- Git commit + commit file ingestion
- Session → commit join queries
- Agent output quality metric (blob hash match rate)

### v0.3 — Embeddings + semantic search
- Local embedding model via Ollama (`all-MiniLM-L6-v2`)
- sqlite-vec integration
- `auto-search similar` command
- Blob content semantic search

### v0.4 — Multi-host
- `host.json` identity
- S3 prefix isolation per host
- Cross-host unified analytics
- `auto-search` reading from S3

### v1.0 — Team / enterprise
- `auto-skill` skill generation
- Shared skills via S3
- Web dashboard (read-only, queries Parquet directly via DuckDB WASM)
- Redaction policy configuration
- Usage reporting and compliance export

---

## Scale Reference

| Metric | 1 developer, 6 months | 10 developers, 1 year | 100 developers, 1 year |
|--------|----------------------|----------------------|------------------------|
| Raw logs | ~1 GB | ~20 GB | ~200 GB |
| Normalised (no blobs) | ~100 MB | ~2 GB | ~20 GB |
| Blob store (deduped, compressed) | ~200 MB | ~2 GB | ~15 GB |
| FTS index | ~50 MB | ~500 MB | ~5 GB |
| Vector index | ~100 MB | ~1 GB | ~10 GB |
| S3 cost (est.) | <$1/month | ~$5/month | ~$40/month |

Parquet + zstd dictionary compression on code/doc content achieves 6-8x compression. Content deduplication across sessions typically reduces unique blob count to 10-20% of total accesses.

---

## Implementation Stack

- **Language:** Go, Cobra CLI conventions
- **Parquet:** `github.com/parquet-go/parquet-go`
- **SQLite:** `modernc.org/sqlite` (pure Go, no CGo)
- **FTS:** SQLite FTS5
- **Vector search:** sqlite-vec
- **Compression:** zstd with shared dictionary
- **Bloom filter:** `github.com/bits-and-blooms/bloom`
- **S3:** AWS SDK v2
- **Embeddings:** Ollama local model (optional, for semantic search)
