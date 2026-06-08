---
hash: "c4317f81"
id: "290481dd"
read_when: "modifying auto-etl message transformation logic or understanding ETL field population"
summary: "Technical reference for Claude Code JSONL message types, content block types, and how each maps to auto-etl normalized parquet fields — including what data is preserved and what is lost"
title: "Claude Message Types and ETL Mapping"
---

# Claude Message Types and ETL Mapping

This doc explains the different message types in Claude Code session logs, the content block types within them, and exactly how auto-etl transforms each one into normalized parquet rows. It also identifies data that is currently lost or not surfaced during the transformation.

## 1. JSONL Line Types

Each Claude Code session is stored as a `.jsonl` file under `~/.claude/projects/`. Every line is a JSON object with a `type` field.

| Line type | Has `message`? | Processed by ETL? | Notes |
|-----------|---------------|-------------------|-------|
| `user` | Yes | Yes | User input or tool results returned to model |
| `assistant` | Yes | Yes | Model response (text, tool calls, thinking) |
| `system` | No (metadata) | Skipped | Turn duration events, not chat messages |
| `progress` | No | Skipped | Hook execution, tool progress events |
| `file-history-snapshot` | No | Skipped | Undo checkpoints |
| `queue-operation` | No | Skipped | Background task enqueue/dequeue |
| `last-prompt` | No | Skipped | Records final user prompt text |

Only `user` and `assistant` lines carry the `message` field with role and content. These are the lines ETL processes. Message lines also carry top-level metadata fields (`version`, `permissionMode`, `attributionSkill`, `gitBranch`) that ETL extracts — see [section 6](#6-additional-preserved-fields-schemaversion-6).

## 2. Message Content: Bare String vs Content Array

The `message.content` field has two forms:

### Bare string (common for user messages)

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": "fix the bug in parser.go"
  }
}
```

ETL produces **one row**: `role=user`, `content="fix the bug in parser.go"`.

### Array of content blocks (common for assistant, also used for user tool results)

```json
{
  "type": "assistant",
  "message": {
    "role": "assistant",
    "content": [
      {"type": "text", "text": "Let me read that file."},
      {"type": "tool_use", "id": "toolu_01X...", "name": "Read", "input": {"file_path": "/tmp/f.go"}},
      {"type": "thinking", "thinking": "I should check the imports...", "signature": "..."}
    ]
  }
}
```

ETL produces **one row per content block**. The block type determines what fields get populated.

## 3. Content Block Types and ETL Mapping

### `text` block

**Where it appears:** Both `user` and `assistant` lines.

**Raw JSON:**
```json
{"type": "text", "text": "Let me read that file."}
```

**ETL output row:**

| Field | Value | Source |
|-------|-------|--------|
| `role` | Original line role (`"user"` or `"assistant"`) | `line.Message.Role` |
| `content` | `"Let me read that file."` | `block.Text` — **full, never truncated** |
| `content_truncated` | Same (or mid-truncated at 4096 chars) | `MidTruncate(block.Text, 4096)` |
| `tool_name` | `""` (empty) | Not a tool block |

**Data preserved:** Full text content, searchable in autosearch.

---

### `tool_use` block

**Where it appears:** `assistant` lines only. This is the model calling a tool.

**Raw JSON:**
```json
{
  "type": "tool_use",
  "id": "toolu_01X...",
  "name": "Read",
  "input": {"file_path": "/tmp/f.go", "offset": 1, "limit": 50}
}
```

**ETL output row:**

| Field | Value | Source |
|-------|-------|--------|
| `role` | `"assistant"` | Hardcoded |
| `content` | `""` (empty) | **Nothing extracted** |
| `content_truncated` | `""` (empty) | **Nothing extracted** |
| `tool_name` | `"Read"` | `block.Name` |
| `tool_input` | `{"file_path":"/tmp/f.go","offset":1,"limit":50}` | `string(block.Input)` — raw JSON |
| `tool_file_path` | `"/tmp/f.go"` | Extracted from `input.file_path` |
| `tool_file_start_line` | `1` | Extracted from `input.offset` (Read only) |
| `tool_file_num_lines` | `50` | Extracted from `input.limit` (Read only) |
| `bash_command` | `""` | Only populated for Bash tool |

**Data preserved:** Tool name, input JSON, file path, bash command.

**Data lost:** The `content` and `content_truncated` fields are **empty**. This means:
- For `AskUserQuestion`: the questions, headers, option labels, and descriptions are only in `tool_input` (raw JSON), not in `content`. They are **not searchable** via autosearch full-text search.
- For `Agent`: the prompt text and description are only in `tool_input`.
- For `Write`/`Edit`: the file content being written is only in `tool_input`.

The `tool_input` field preserves the raw JSON, so the data exists in parquet — but autosearch indexes `content`/`content_truncated`, not `tool_input`, so this data is invisible to search.

---

### `tool_result` block

**Where it appears:** `user` lines only. This is the tool's output being returned to the model.

**Raw JSON:**
```json
{
  "type": "tool_result",
  "tool_use_id": "toolu_01X...",
  "content": "     1→package main\n     2→\n     3→import (..."
}
```

The `content` field can be either a plain string or an array of content objects. ETL handles both.

**ETL output row:**

| Field | Value | Source |
|-------|-------|--------|
| `role` | `"tool"` | Hardcoded (not `"user"`) |
| `content` | `"     1→package main\n..."` | `block.Content` — **full, never truncated** |
| `content_truncated` | Mid-truncated version | `MidTruncate(content, 4096)` |
| `tool_name` | `"Read"` | **Looked up** from the matching `tool_use` block via `tool_use_id` |
| `tool_file_path` | `"/tmp/f.go"` | Copied from matching `tool_use` block |
| `bash_command` | `""` or command | Copied from matching `tool_use` block |
| `is_error` | `true`/`false` | `block.IsError` — true when the tool reported an error |

**Data preserved:** Full tool output (file contents, command output, etc.), tool name, file metadata, error status.

**How the lookup works:** ETL scans backward through preceding lines to find the `tool_use` block whose `id` matches this `tool_result`'s `tool_use_id`, then copies the tool name and metadata.

---

### `thinking` block

**Where it appears:** `assistant` lines only. Extended thinking / chain-of-thought.

**Raw JSON:**
```json
{
  "type": "thinking",
  "thinking": "I should check the imports first...",
  "signature": "(encrypted)"
}
```

**ETL output row:**

| Field | Value | Source |
|-------|-------|--------|
| `role` | `"thinking"` | Dedicated role for reasoning blocks |
| `content` | `block.thinking` | Full reasoning text — preserved unmodified |
| `content_truncated` | truncated `content` | Mid-truncated at 4096 chars if over threshold |
| `thinking_signature` | `block.signature` | Opaque per-block verification token |
| `tool_name` | `""` | Not a tool |

`redacted_thinking` blocks are also preserved with `role="thinking"`, `content="[redacted]"` (marker), and `thinking_signature` carrying the encrypted `data` payload.

**Data preserved:** Full reasoning text, thinking signature, and redacted-block data. Thinking rows are excluded from session transcripts but remain canonical in the `messages` dataset. Thinking is excluded from default `search` and `session get` output; use `--role thinking` or `--include-thinking` to access.

## 4. Special Tool Behaviors

### AskUserQuestion

AskUserQuestion is a deferred tool — the agent must first call `ToolSearch` to fetch its schema, then invoke it. The full workflow spans 4 messages across 2 JSONL lines.

#### Tool schema

The `input` JSON schema for AskUserQuestion:

```json
{
  "questions": [
    {
      "question": "string — the question text shown to the user",
      "header": "string — optional section header grouping related questions",
      "multiSelect": "boolean — whether user can select multiple options",
      "options": [
        {
          "label": "string — the option text (this is what gets recorded as the answer)",
          "description": "string — explanatory text shown below the option"
        }
      ]
    }
  ]
}
```

Up to 4 questions per invocation. Each question can have multiple options. The user sees a form-like UI, selects answers, and can optionally add free-text notes.

#### Full workflow example

This example is from a real session (`ce837746`) where the agent presented code review findings as structured questions.

**Step 1 — Agent fetches the deferred tool schema (assistant line):**

```json
{
  "type": "assistant",
  "message": {
    "role": "assistant",
    "content": [
      {
        "type": "tool_use",
        "id": "toolu_01A...",
        "name": "ToolSearch",
        "input": {"query": "select:AskUserQuestion", "max_results": 1}
      }
    ]
  }
}
```

**Step 2 — ToolSearch returns the schema (user line with tool_result):**

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": [
      {
        "type": "tool_result",
        "tool_use_id": "toolu_01A...",
        "content": "<functions>...</functions>"
      }
    ]
  }
}
```

**Step 3 — Agent invokes AskUserQuestion (assistant line):**

```json
{
  "type": "assistant",
  "message": {
    "role": "assistant",
    "content": [
      {
        "type": "tool_use",
        "id": "toolu_01B...",
        "name": "AskUserQuestion",
        "input": {
          "questions": [
            {
              "question": "There are 3 bugs to fix: (1) AgentSession leaks Postgres connections, (2) asyncio.run() in SlackAdapter crashes in async context, (3) scheduler/tool.py defines every tool twice. Fix all three?",
              "header": "Bugs",
              "multiSelect": false,
              "options": [
                {
                  "label": "Fix all three (Recommended)",
                  "description": "Add close() to AgentSession, fix Slack async, remove duplicated scheduler tools"
                },
                {
                  "label": "Fix #1 and #3 only",
                  "description": "Skip the Slack fix since you may not be actively using it yet"
                },
                {
                  "label": "Skip for now",
                  "description": "Leave bugs as-is, focus on structural changes"
                }
              ]
            },
            {
              "question": "Tool assembly is split: AgentFactory adds search+scheduler tools, but AgentSession independently adds Composio tools. Should we consolidate?",
              "header": "Tools",
              "multiSelect": false,
              "options": [
                {
                  "label": "Yes, consolidate (Recommended)",
                  "description": "Move all tool fetching to AgentFactory, pass flat list to AgentSession"
                },
                {
                  "label": "No, keep split",
                  "description": "Current approach works, consolidate later"
                }
              ]
            },
            {
              "question": "There are 4 dead/unused files. What should we do?",
              "header": "Dead code",
              "multiSelect": false,
              "options": [
                {
                  "label": "Delete all four (Recommended)",
                  "description": "Clean slate — re-add when actually needed"
                },
                {
                  "label": "Keep event_bus.py",
                  "description": "Delete other 3 but keep event bus for future use"
                },
                {
                  "label": "Leave them",
                  "description": "They're not hurting anything"
                }
              ]
            },
            {
              "question": "Should we reorganize loose files under src/tools/?",
              "header": "Structure",
              "multiSelect": false,
              "options": [
                {
                  "label": "Yes, move under tools/",
                  "description": "src/tools/ becomes the single home for all tool integrations"
                },
                {
                  "label": "No, keep flat",
                  "description": "Current layout is fine for the project size"
                }
              ]
            }
          ]
        }
      }
    ]
  }
}
```

**Step 4 — User answers returned as tool_result (user line):**

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": [
      {
        "type": "tool_result",
        "tool_use_id": "toolu_01B...",
        "content": "User has answered your questions: \"There are 3 bugs to fix: (1) AgentSession leaks Postgres connections, (2) asyncio.run() in SlackAdapter crashes in async context, (3) scheduler/tool.py defines every tool twice. Fix all three?\"=\"Fix all three (Recommended)\", \"Tool assembly is split: AgentFactory adds search+scheduler tools, but AgentSession independently adds Composio tools. Should we consolidate?\"=\"Yes, consolidate (Recommended)\", \"There are 4 dead/unused files. What should we do?\"=\"remove workflows keep rest\" user notes: remove workflows keep rest, \"Should we reorganize loose files under src/tools/?\"=\"Yes, move under tools/\". You can now continue with the user's answers in mind."
      }
    ]
  }
}
```

#### ETL mapping for each step

| Step | JSONL line type | Block type | ETL `role` | `tool_name` | `content` | `tool_input` |
|------|----------------|-----------|-----------|-------------|-----------|-------------|
| 1 | assistant | tool_use | assistant | `ToolSearch` | **empty** | `{"query":"select:AskUserQuestion",...}` |
| 2 | user | tool_result | tool | `ToolSearch` | schema XML | empty |
| 3 | assistant | tool_use | assistant | `AskUserQuestion` | **empty** | full questions JSON (see above) |
| 4 | user | tool_result | tool | `AskUserQuestion` | `"User has answered your questions: ..."` | empty |

#### What is searchable vs lost

- **Searchable via autosearch FTS:** The flat answer string in step 4 — e.g. searching `"Fix all three"` finds the tool_result.
- **Not searchable via FTS:** The structured questions from step 3. Searching `"AgentSession leaks Postgres connections"` will **not** find the AskUserQuestion invocation because that text is in `tool_input`, not `content`. The `tool_input` field preserves the full JSON in parquet, but autosearch FTS indexes only `content_truncated`.
- **Also not in FTS:** Option descriptions like `"Add close() to AgentSession, fix Slack async, remove duplicated scheduler tools"` — these are only in `tool_input` JSON.
- **Queryable via SQL (not FTS):** As of `SchemaVersion 3`, the step-4 user answers and per-question annotation notes are captured verbatim in the `tool_use_result_json` column on the step-4 `role=tool` row. They are recoverable structurally — `json_extract(tool_use_result_json, '$.answers')` and `json_extract(tool_use_result_json, '$.annotations.<question text>.notes')` — over both the parquet (DuckDB) and the SQLite index. FTS itself is unchanged: this column is **not** added to the FTS5 index.

#### How to view AskUserQuestion data today

Use `autosearch session get <session_id>` to render the full transcript — this shows both the tool_use JSON and the tool_result string. Example:

```bash
autosearch session get ce837746-905c-45f8-b9fa-9b1514caa006 | grep -B5 -A20 "AskUserQuestion"
```

To query the raw `tool_input` field, use duckdb or python against the parquet files directly:

```bash
duckdb -c "
  SELECT tool_input
  FROM '~/.auto/etl/output/messages/year=*/week=*/messages.parquet'
  WHERE tool_name = 'AskUserQuestion' AND role = 'assistant'
  LIMIT 5
"
```

#### Querying the user's answers via `tool_use_result_json`

As of `SchemaVersion 3`, the verbatim `toolUseResult` envelope is captured into the `tool_use_result_json` column on the `role=tool` row (see the [normalized schema reference](reference/normalized-schema.md)). The user's actual picks live under `$.answers` (keyed by question text), and per-question free-text notes live under `$.annotations.<question text>.notes`. This replaces the regex-over-prose Q5 workaround documented in `docs/research/askuserquestion-analytics.md` — the answers are now structured rather than parsed out of the flat `content` string.

Against the **parquet** files via DuckDB (`json_extract` or `json_extract_string` both work):

```bash
duckdb -c "
  SELECT json_extract(tool_use_result_json, '\$.answers') AS answers
  FROM read_parquet('~/.auto/etl/output/messages/year=*/week=*/messages.parquet')
  WHERE tool_name = 'AskUserQuestion' AND tool_use_result_json != ''
  LIMIT 5
"
```

Against the **autosearch SQLite index** (driver `modernc.org/sqlite` — use plain `json_extract`, which returns the scalar string directly; `json_extract_string` is a DuckDB-only function and is **not** available here):

```bash
sqlite3 ~/.auto/search/default.sqlite "
  SELECT json_extract(tool_use_result_json, '\$.answers') AS answers
  FROM messages
  WHERE tool_name = 'AskUserQuestion' AND tool_use_result_json != ''
  LIMIT 5;
"
```

Per-row inspection of the parsed envelope is also available via `autosearch message describe <message-id>`, which emits the envelope under a `toolUseResult` key when the row carries it.

### Bash

```json
{"type": "tool_use", "name": "Bash", "input": {"command": "go test ./..."}}
```

ETL extracts `bash_command = "go test ./..."` from the input JSON. The command is stored separately from `content` for easy querying. The tool_result contains the command output.

### Read / Write / Edit

ETL extracts `file_path`, `offset` (as `tool_file_start_line`), and `limit` (as `tool_file_num_lines`) from the tool_use input. For Write/Edit, the file content being written is in `tool_input` but not in `content`.

The tool_result for Read contains the full file contents inline in `content`.

### Agent (subagent spawn)

```json
{"type": "tool_use", "name": "Agent", "input": {"prompt": "...", "subagent_type": "Explore"}}
```

The `tool_input` contains the full prompt. The tool_result contains the subagent's final summary. The subagent's own transcript lives in a separate JSONL file under `{session}/subagents/`.

## 5. Complete Mapping Table

One row per content block in the source JSONL:

| Block type | Source line | ETL `role` | `content` | `tool_name` | `tool_input` | Searchable? |
|-----------|------------|-----------|-----------|-------------|-------------|-------------|
| bare string | user | `user` | Full text | empty | empty | Yes |
| `text` | user or assistant | original role | Full text | empty | empty | Yes |
| `tool_use` | assistant | `assistant` | **empty** | tool name | raw JSON | **No** (content empty) |
| `tool_result` | user | `tool` | tool output | looked up | empty | Yes |
| `thinking` | assistant | `thinking` | full reasoning text | empty | empty | Opt-in (`--role thinking` / `--include-thinking`) |

## 6. Additional Preserved Fields (SchemaVersion 6)

Beyond per-block content, ETL extracts additional signal from the JSONL lines:

### Message-level fields

| Field | Type | Source | Notes |
|-------|------|--------|-------|
| `thinking_signature` | string | `block.signature` (thinking) or `block.data` (redacted_thinking) | Opaque per-block verification token |
| `stop_reason` | string | `line.message.stop_reason` | API stop reason on assistant rows (e.g. `end_turn`, `tool_use`, `max_tokens`) |
| `is_error` | bool | `block.is_error` on `tool_result` blocks | True when the tool reported an error |
| `cache_creation_input_tokens` | int64 | `usage.cache_creation_input_tokens` | Prompt-cache creation tokens (split from the combined `cache_input_tokens` sum, which is also retained) |
| `cache_read_input_tokens` | int64 | `usage.cache_read_input_tokens` | Prompt-cache read tokens (split from the combined sum) |
| `skill_name` | string | `Skill` tool `input.skill`, **or** `line.attributionSkill` as fallback | Fallback populates `skill_name` from `attributionSkill` when no Skill-tool skill is set on the row. Covers all content blocks of the attributed turn (text, thinking, tool_use). |

### Session-level fields

| Field | Type | Source | Notes |
|-------|------|--------|-------|
| `permission_mode` | string | Top-level `permissionMode` field on message lines | Last-seen value; environment-level (e.g. `default`, `bypassPermissions`) |
| `version` | string | Top-level `version` field on message lines | CLI version (e.g. `2.1.168`); last-seen value |

Both `permission_mode` and `version` are read from the top-level fields on JSONL message lines (the scan loop decodes every line, so this also captures values from standalone `type:"permission-mode"` lines). They are set on the `AgentSession` record once per session.

## 7. What Autosearch Can and Cannot Find

Autosearch indexes `content_truncated` for full-text search (BM25). This means:

**Searchable via FTS:**
- User's typed messages (bare strings and text blocks)
- Assistant's text responses
- Tool outputs (file contents, command output, error messages)
- AskUserQuestion answers (the flat "User has answered..." string)

**Not searchable via default FTS:**
- Tool invocation inputs (questions asked via AskUserQuestion, bash commands in tool_use blocks, file content being written, Agent prompts)
- Thinking block content — indexed in FTS but excluded from default search results; use `--role thinking` or `--include-thinking` to include
- Tool metadata (file paths, line numbers) — stored in separate fields, not in FTS index

Note: `bash_command` and `tool_file_path` are stored as separate columns in the parquet/sqlite schema, so they could be made searchable with additional indexing, but aren't currently included in the FTS5 index.

**Queryable via structured SQL (not FTS):** AskUserQuestion answers and per-question annotation notes are captured verbatim in the `tool_use_result_json` column (`SchemaVersion 3`). These are recoverable with `json_extract` over `$.answers` and `$.annotations.<question text>.notes` against either the parquet (DuckDB) or the SQLite index — see [section 4](#how-to-view-askuserquestion-data-today). This is a structured-query path, not full-text search: the column is deliberately **not** added to the FTS5 index, so FTS semantics are unchanged.

## 8. Known Gaps and Potential Improvements

1. **tool_use content is empty** — The `content` field for tool_use rows could be populated with a human-readable summary of the tool input (e.g., the question text for AskUserQuestion, the command for Bash, the file path for Read). This would make tool invocations searchable.

2. **thinking blocks are fully preserved** (SchemaVersion 6) — Emitted as `role="thinking"` messages with full reasoning text and `thinking_signature`. Excluded from default search/session-get; use `--role thinking` or `--include-thinking`.

3. **tool_input not indexed** — Autosearch could add `tool_input` to the FTS5 index, but the raw JSON would need pre-processing to be useful for text search.

4. **Write/Edit content not in content field** — When the agent writes a file, the content being written is in `tool_input` JSON but not in the `content` column. This means you can't search for "what did the agent write to file X" via full-text search.
