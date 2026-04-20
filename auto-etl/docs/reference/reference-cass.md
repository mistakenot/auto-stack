---
hash: "66493236"
id: "19cc4aad"
read_when: "designing cross-agent session normalization or building agent connectors"
summary: "Reference notes on the CASS Rust CLI's session file locations, normalized data model, connector architecture, and patterns reusable for the Go ETL implementation."
title: "Reference: coding_agent_session_search (CASS)"
---

# Reference: coding_agent_session_search (CASS)

Source: `.tmp/coding_agent_session_search` (cloned from github.com/Dicklesworthstone/coding_agent_session_search)

Rust CLI that discovers, normalizes, indexes, and searches coding agent session histories. We're interested in borrowing its extraction and normalization patterns for our Go implementation.

---

## Session File Locations & Formats

### Claude Code
- **Path**: `~/.claude/projects/` (or `$CLAUDE_HOME`)
- **Format**: JSONL (one JSON object per line)
- **Pattern**: `*.jsonl`
- Each file is one session/conversation. Lines contain messages with `role`, `content`, `created_at` fields.

### Codex (OpenAI)
- **Path**: `~/.codex/sessions/` (or `$CODEX_HOME`)
- **Format**: JSONL
- **Pattern**: `rollout-*.jsonl`
- Token counts are in an `event_msg` field and get backfilled to the nearest preceding assistant message.

### Other agents supported (15+ total)
| Agent | Path | Format |
|-------|------|--------|
| Cursor | `~/Library/Application Support/Cursor/User/` | SQLite (state.vscdb) |
| Cline | VS Code global storage | Task directories |
| Gemini CLI | `~/.gemini/tmp` | JSON |
| Aider | `~/.aider.chat.history.md` | Markdown |
| Copilot | VS Code storage | varies |

---

## Normalized Data Model

All agents get converted into a common schema. Key types (from `src/model/types.rs`):

### Conversation (top-level)
| Field | Type | Notes |
|-------|------|-------|
| agent_slug | string | `"claude-code"`, `"codex"`, etc. |
| workspace | path? | Project directory |
| external_id | string? | Agent's own session ID |
| title | string? | Session title |
| source_path | path | Path to the source file |
| started_at | i64? | Unix ms |
| ended_at | i64? | Unix ms |
| approx_tokens | i64? | Estimated token count |
| metadata_json | json | Flexible metadata bag |
| messages | []Message | Ordered conversation turns |
| source_id | string | `"local"` or remote source ID |
| origin_host | string? | Remote hostname (for SSH sources) |

### Message (individual turn)
| Field | Type | Notes |
|-------|------|-------|
| idx | i64 | Position in conversation |
| role | enum | User, Agent, Tool, System, Other(string) |
| author | string? | Human or agent name |
| created_at | i64? | Unix ms |
| content | string | Message text |
| extra_json | json | Overflow fields |
| snippets | []Snippet | Extracted code blocks |

### Snippet (code block within a message)
| Field | Type | Notes |
|-------|------|-------|
| file_path | path? | Referenced file |
| start_line | i64? | |
| end_line | i64? | |
| language | string? | `"go"`, `"python"`, etc. |
| snippet_text | string? | Code content |

### MessageRole enum
`User | Agent | Tool | System | Other(string)`

---

## Architecture: How It Works

### Discovery & Extraction Flow
1. **Detect** — each connector checks if its agent is installed (e.g. `~/.claude/projects` exists)
2. **Scan** — connector reads session files from known paths
3. **Parse** — format-specific parsing (JSONL lines → structs)
4. **Normalize** — convert to `Conversation → Message → Snippet` schema
5. **Ingest** — store in SQLite, build Tantivy full-text index

### Connector interface (trait)
```
detect()  → bool                          # Is this agent installed?
scan(ctx) → Vec<NormalizedConversation>   # Read & normalize all sessions
```

### Streaming indexer
- One producer thread per connector (parallel via rayon)
- Bounded channel (capacity 32) for backpressure
- Consumer ingests batches into SQLite + Tantivy

---

## Key Source Files

| File | What it does |
|------|-------------|
| `src/connectors/claude_code.rs` | Claude Code connector (re-exports from `franken_agent_detection` crate) |
| `src/connectors/codex.rs` | Codex connector (re-exports from `franken_agent_detection` crate) |
| `src/connectors/mod.rs` | Connector registry & trait |
| `src/model/types.rs` | Normalized data model (Conversation, Message, Snippet) |
| `src/indexer/mod.rs` | Streaming discovery + ingestion pipeline (~3000 lines) |
| `src/storage/sqlite.rs` | Database schema & persistence |
| `src/sources/config.rs` | Remote source config (TOML) |
| `src/sources/sync.rs` | SSH/rsync sync engine |

**Note**: The actual Claude/Codex parsing logic lives in an external crate `franken_agent_detection` — the connectors in this repo are thin re-export shims.

---

## What We Can Reuse

For our Go ETL, the most directly useful patterns are:

1. **File locations & glob patterns** — where each agent stores sessions
2. **JSONL parsing** — line-by-line JSON for Claude Code and Codex
3. **Normalized schema** — the Conversation/Message/Snippet model is a solid common format
4. **Connector pattern** — interface with `Detect()` and `Scan()` methods per agent
5. **Codex token backfill** — attaching token counts from `event_msg` to assistant messages

Things we probably don't need to copy:
- Tantivy full-text search (we'll use a database)
- Semantic/vector embeddings
- TUI interface
- HTML export with encryption
