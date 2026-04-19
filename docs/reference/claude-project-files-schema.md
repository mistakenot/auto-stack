---
hash: "6fbb1345"
id: "17bfbcbb"
read_when: "when parsing Claude Code session files or understanding ETL data"
summary: "Reference for the on-disk JSONL file format produced by Claude Code sessions, covering directory structure, line types, content blocks, token usage, subagent files, and tool-results directories."
title: "Claude Code Project Files Schema"
---

# Claude Code Project Files Schema

Reference documentation for the on-disk file format produced by Claude Code sessions. Based on analysis of real session data (179 files, 28,633 lines, Claude Code v2.1.74, March 2026).

## Directory structure

```
~/.claude/projects/{workspace-slug}/
├── {session-uuid}.jsonl                          # Parent session transcript
├── {session-uuid}/
│   ├── subagents/
│   │   ├── agent-{agent-id}.jsonl                # Subagent transcript
│   │   └── agent-{agent-id}.meta.json            # Subagent metadata
│   └── tool-results/
│       └── {tool-use-id-or-hash}.txt             # Large tool result content
└── ...
```

- **Workspace slug**: The absolute working directory path with `/` replaced by `-`, e.g. `/home/vscode/src/notes` becomes `-home-vscode-src-notes`.
- **Session UUID**: A standard v4 UUID identifying the session, e.g. `7690a160-c2e1-4dcd-a30b-d45b8152e097`.
- **Agent ID**: A hex string identifying the subagent, e.g. `a906621c1fcf0c74a`. Some older subagents use a named format like `aside_question-38babac48c0a60a1`.

## JSONL line format

Each `.jsonl` file contains one JSON object per line. Every line has a `type` field that determines the schema.

### Common fields

These fields appear on most line types:

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Line type discriminator (see below) |
| `uuid` | string | Unique ID for this line |
| `parentUuid` | string\|null | UUID of the preceding line in the conversation tree |
| `sessionId` | string | Session UUID (same across parent + all subagent files) |
| `timestamp` | string | ISO 8601 with milliseconds, e.g. `"2026-03-13T11:04:48.775Z"` |
| `cwd` | string | Working directory at time of line |
| `userType` | string | Always `"external"` in observed data |
| `version` | string | Claude Code version, e.g. `"2.1.74"` |
| `gitBranch` | string | Active git branch |
| `isSidechain` | bool | `true` for subagent lines, `false` for parent |

### Line types

Observed in real data, ordered by frequency:

| Type | Count | Has `message`? | Description |
|------|-------|----------------|-------------|
| `progress` | 10,924 | No | Hook execution events, tool progress |
| `assistant` | 9,533 | Yes | Model response (text, tool calls, thinking) |
| `user` | 6,958 | Yes | User input or tool results returned to model |
| `file-history-snapshot` | 786 | No | File state snapshots for undo |
| `system` | 289 | No | Metadata events (e.g. turn duration) |
| `queue-operation` | 128 | No | Background task enqueue/dequeue |
| `last-prompt` | 15 | No | Records the last user prompt text |

## Message lines (type: `user` and `assistant`)

These are the primary conversation content. The `message` field contains the model interaction.

### User line

```json
{
  "type": "user",
  "uuid": "1c0944e0-8bc9-4055-941d-b54152caeca3",
  "parentUuid": null,
  "sessionId": "7690a160-c2e1-4dcd-a30b-d45b8152e097",
  "timestamp": "2026-03-13T11:04:48.775Z",
  "isSidechain": false,
  "promptId": "fae83f0b-0a7e-4222-9824-6151a2da4cba",
  "permissionMode": "bypassPermissions",
  "message": {
    "role": "user",
    "content": "read @projects/ts-explore/ and do online research..."
  },
  "cwd": "/home/vscode/src/notes",
  "userType": "external",
  "version": "2.1.74",
  "gitBranch": "main"
}
```

**Note:** `message.content` can be either:
- A **bare string** — the user's typed input
- An **array of content blocks** — when returning tool results to the model (see below)

### User line with tool results

When the user line carries tool results back to the model, `message.content` is an array:

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": [
      {
        "type": "tool_result",
        "tool_use_id": "toolu_0172jyNwB14WK17sAtUL53am",
        "content": "     1→package main\n     2→\n     3→import (..."
      }
    ]
  },
  "toolUseResult": {
    "type": "text",
    "file": {
      "filePath": "/home/vscode/src/notes/projects/ts-explore/CLAUDE.md",
      "content": "full file content here...",
      "numLines": 1,
      "startLine": 1,
      "totalLines": 1
    }
  },
  "sourceToolAssistantUUID": "78e651d8-6b92-4ba9-9e5e-c8e59f2f7d03"
}
```

The `toolUseResult` field provides structured metadata about the tool result, including the full file content for Read operations. The `sourceToolAssistantUUID` links back to the assistant line that issued the tool call.

### Assistant line

```json
{
  "type": "assistant",
  "uuid": "78e651d8-6b92-4ba9-9e5e-c8e59f2f7d03",
  "parentUuid": "dcbb71c2-7bf1-44ed-9398-e0f9d10a8bd0",
  "sessionId": "7690a160-c2e1-4dcd-a30b-d45b8152e097",
  "timestamp": "2026-03-13T11:04:52.357Z",
  "requestId": "req_011CYzyBYLkbCqtKRddgNksz",
  "slug": "prancy-percolating-breeze",
  "message": {
    "model": "claude-opus-4-6",
    "id": "msg_01XCWPsXZzHNW3xdFYDUaTxY",
    "type": "message",
    "role": "assistant",
    "content": [ ... ],
    "stop_reason": "tool_use",
    "stop_sequence": null,
    "usage": { ... }
  }
}
```

Assistant-specific fields:
- `requestId` — Anthropic API request ID
- `slug` — Human-readable session slug (appears after first assistant response)
- `message.model` — Model ID, e.g. `"claude-opus-4-6"`
- `message.stop_reason` — `"end_turn"`, `"tool_use"`, or `null` (streaming)
- `message.usage` — Token accounting (see below)

## Content blocks

The `message.content` array on assistant lines contains typed blocks:

### text

```json
{
  "type": "text",
  "text": "Let me read that file for you."
}
```

### tool_use

```json
{
  "type": "tool_use",
  "id": "toolu_0172jyNwB14WK17sAtUL53am",
  "name": "Read",
  "input": {
    "file_path": "/home/vscode/src/notes/projects/ts-explore/CLAUDE.md"
  },
  "caller": {
    "type": "direct"
  }
}
```

The `id` field links to the corresponding `tool_result` block (via `tool_use_id`). The `name` field is the tool name. Common tools: `Read`, `Write`, `Edit`, `Bash`, `Glob`, `Grep`, `Agent`, `WebSearch`, `WebFetch`, `AskUserQuestion`, `Skill`, `ToolSearch`, `TaskOutput`, `EnterPlanMode`, `ExitPlanMode`.

Tool input schemas vary by tool. Key ones for file operations:

| Tool | Key input fields |
|------|-----------------|
| `Read` | `file_path` |
| `Write` | `file_path`, `content` |
| `Edit` | `file_path`, `old_string`, `new_string` |
| `Bash` | `command` |
| `Agent` | `prompt`, `subagent_type`, `description` |

### tool_result

Appears on `user` type lines, returning tool output to the model:

```json
{
  "type": "tool_result",
  "tool_use_id": "toolu_0172jyNwB14WK17sAtUL53am",
  "content": "file contents or command output..."
}
```

The `content` field can be a string or (less commonly) an array of content objects.

### thinking

```json
{
  "type": "thinking",
  "thinking": "",
  "signature": "(encrypted signature)"
}
```

Extended thinking blocks. The `thinking` field may be empty (redacted in logs) or contain the model's chain-of-thought. The `signature` is always present. These appear on assistant lines alongside other content blocks.

## Token usage

The `message.usage` object on assistant lines:

```json
{
  "input_tokens": 3,
  "output_tokens": 109,
  "cache_creation_input_tokens": 8803,
  "cache_read_input_tokens": 8809,
  "cache_creation": {
    "ephemeral_1h_input_tokens": 8803,
    "ephemeral_5m_input_tokens": 0
  },
  "server_tool_use": {
    "web_search_requests": 0,
    "web_fetch_requests": 0
  },
  "service_tier": "standard",
  "inference_geo": "",
  "speed": "standard"
}
```

Key fields for token accounting:
- `input_tokens` — Non-cached input tokens
- `output_tokens` — Generated output tokens
- `cache_creation_input_tokens` — Tokens written to cache this turn
- `cache_read_input_tokens` — Tokens read from cache this turn

## Non-message line types

### progress

Hook execution and tool progress events. No `message` field.

```json
{
  "type": "progress",
  "data": {
    "type": "hook_progress",
    "hookEvent": "PostToolUse",
    "hookName": "PostToolUse:Read",
    "command": "callback"
  },
  "parentToolUseID": "toolu_0172jyNwB14WK17sAtUL53am",
  "toolUseID": "toolu_0172jyNwB14WK17sAtUL53am",
  "timestamp": "2026-03-13T11:04:52.366Z"
}
```

### file-history-snapshot

Checkpoint of tracked file state for undo operations. No `message` field.

```json
{
  "type": "file-history-snapshot",
  "messageId": "1c0944e0-8bc9-4055-941d-b54152caeca3",
  "snapshot": {
    "messageId": "1c0944e0-8bc9-4055-941d-b54152caeca3",
    "trackedFileBackups": { ... },
    "timestamp": "2026-03-13T11:04:48.776Z"
  },
  "isSnapshotUpdate": false
}
```

### system

Metadata events, not chat messages. No `message.role` or `message.content`. Despite the name, these are not system prompts.

```json
{
  "type": "system",
  "subtype": "turn_duration",
  "durationMs": 1066017,
  "agentId": "aside_question-38babac48c0a60a1",
  "timestamp": "2026-03-13T02:17:26.101Z",
  "isMeta": false
}
```

### queue-operation

Background task lifecycle events.

```json
{
  "type": "queue-operation",
  "operation": "enqueue",
  "sessionId": "ab2a6291-d5fb-4aa3-a590-fc3584911d44",
  "content": "<task-notification>...</task-notification>",
  "timestamp": "2026-03-13T02:17:17.712Z"
}
```

### last-prompt

Records the final user prompt for a session. Minimal structure.

```json
{
  "type": "last-prompt",
  "lastPrompt": "dont run it, but what should i run to set it up in this dir",
  "sessionId": "8a1d85ad-39c3-4f08-9011-09231cc87baf"
}
```

## Subagent files

When the model uses the `Agent` tool, Claude Code spawns a subagent in a separate JSONL file.

### Relationship to parent

1. Parent session has an `Agent` tool_use block:
   ```json
   {
     "type": "tool_use",
     "name": "Agent",
     "input": {
       "subagent_type": "general-purpose",
       "description": "Research TS Go compiler APIs",
       "prompt": "Do web research to answer..."
     }
   }
   ```

2. Subagent runs in `{session-uuid}/subagents/agent-{agent-id}.jsonl`

3. The `tool_result` back in the parent contains the subagent's final output.

### Subagent JSONL differences

| Field | Parent lines | Subagent lines |
|-------|-------------|----------------|
| `sessionId` | Own UUID | **Same as parent** (shared) |
| `agentId` | Absent | Present on **every** line |
| `isSidechain` | `false` | `true` on **every** line |

Example subagent line:

```json
{
  "type": "user",
  "sessionId": "7690a160-c2e1-4dcd-a30b-d45b8152e097",
  "agentId": "a906621c1fcf0c74a",
  "isSidechain": true,
  "message": {
    "role": "user",
    "content": "whats in this repo?"
  },
  "timestamp": "2026-03-13T11:05:03.100Z"
}
```

### Meta files

Sibling `.meta.json` files contain subagent configuration:

```json
{
  "agentType": "general-purpose"
}
```

Some include a `worktreePath` for agents that ran in git worktrees:

```json
{
  "agentType": "general-purpose",
  "worktreePath": "/home/vscode/src/project/.claude/worktrees/agent-acbcec19"
}
```

Observed `agentType` values: `Explore` (58), `general-purpose` (51), `claude-code-guide` (1), `Plan` (1).

**Note:** 4 subagent JSONL files (all `aside_question-*` pattern) had no corresponding `.meta.json`. These appear to be an older subagent format.

### Identifying subagent files

A file is a subagent transcript if **any** of:
- Its path contains `/subagents/`
- Its filename matches `agent-*.jsonl`
- Lines contain `"agentId"` field
- Lines have `"isSidechain": true`

## Tool results directory

Some sessions have a `tool-results/` directory containing large tool outputs stored as separate files:

```
{session-uuid}/tool-results/toolu_01PdFf6f7WXqdciXiHE7BeJ6.txt
{session-uuid}/tool-results/bqcukcy07.txt
```

These are plain text files containing the full output of tool calls (e.g. file contents from Read operations). File names are either the `tool_use_id` or a short hash. Observed across 18 sessions with 37 total files.

The ETL does not currently process these files. The tool result content is also available inline in `tool_result` content blocks within the JSONL.

## Data statistics (from test corpus)

From 179 JSONL files across 64 unique sessions:

| Metric | Value |
|--------|-------|
| Total JSONL lines | 28,633 |
| Message lines (user+assistant) | 16,491 |
| Non-message metadata lines | 12,142 (42%) |
| Content blocks (in message arrays) | 15,657 |
| Bare string contents | 834 |
| Thinking blocks | 309 |
| Unique tool names | 15 |
| File tool uses (Read/Write/Edit) | 2,927 |
| Subagent files | 115 |
| Subagent meta files | 111 |
| Tool-results directories | 18 |
| Tool-results files | 37 |
