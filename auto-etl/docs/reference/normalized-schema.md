---
hash: "36af2aca"
id: "a41ab3e6"
read_when: "querying normalized parquet data or understanding auto-etl output schema"
summary: "Canonical reference for auto-etl normalized parquet datasets, fields, partitions, and population rules."
title: "Auto ETL Normalized Schema Reference"
---

# Auto ETL Normalized Schema Reference

This document describes the **current normalized output schema** produced by `auto-etl`.

Source of truth in code:
- `internal/model/model.go`
- `internal/transform/transform.go`
- `internal/writer/writer.go`

If this doc conflicts with those files, treat the code as canonical.

## Schema Version

- `schema_version` is set from `model.SchemaVersion`
- Current value: `6`

## Output Datasets and Partitions

`auto-etl` writes two parquet datasets:

- `messages/year=YYYY/week=WW/messages.parquet` (weekly)
- `sessions/year=YYYY/month=MM/sessions.parquet` (monthly)

Current period partitions are regenerated; past partitions are skipped when the parquet file already exists.

## `messages` Dataset (`AgentMessage`)

Represents one normalized message block in a session. For content arrays, each content block becomes one row.

| Field | Type | Meaning / Population |
|---|---|---|
| `id` | string | Message row ID, `"<session_id>-<index>"` |
| `session_id` | string | Session ID from parsed transcript |
| `host_id` | string | Host identifier from `~/.auto/host.json`, fallback `os.Hostname()` |
| `index` | int32 | Incrementing message index within transformed session |
| `role` | string | One of `user`, `assistant`, `tool`, `system`, `thinking` |
| `content` | string | Full unmodified text payload (never truncated) |
| `content_truncated` | string | Same as content, mid-truncated at 4096 chars if over threshold |
| `timestamp` | int64 | Unix milliseconds |
| `tool_name` | string | Tool name for `tool_use` blocks; also set on `tool_result` rows |
| `tool_input` | string | Raw tool input JSON (stringified) |
| `tool_file_path` | string | File path extracted from tool input `file_path` |
| `tool_file_start_line` | int32 | From Read tool input `offset` field (0 otherwise) |
| `tool_file_num_lines` | int32 | From Read tool input `limit` field (0 otherwise) |
| `tool_file_total_lines` | int32 | Not reliably available; always 0 for now |
| `bash_command` | string | Bash command extracted when `tool_name == "Bash"` |
| `skill_name` | string | Skill name. Populated from `Skill` tool `input.skill`, or from `attributionSkill` on the JSONL line as fallback (when no Skill-tool skill is set). Covers all content blocks of the attributed turn. |
| `tool_use_result_json` | string | Raw `toolUseResult` envelope (verbatim JSON) from the source JSONL line. Populated on `role=tool` rows whose source line carries the envelope (e.g. `AskUserQuestion` answers/annotations, deferred-tool results); empty string otherwise. Stored unmodified for `json_extract` querying. |
| `input_tokens` | int32 | `usage.input_tokens` |
| `cache_input_tokens` | int32 | `usage.cache_creation_input_tokens + usage.cache_read_input_tokens` (combined sum, retained) |
| `output_tokens` | int32 | `usage.output_tokens` |
| `thinking_signature` | string | Opaque per-block token from thinking blocks. For normal `thinking` blocks: the signature string. For `redacted_thinking` blocks: the encrypted data payload. Empty for non-thinking rows. |
| `stop_reason` | string | API stop reason from assistant message lines (e.g. `end_turn`, `tool_use`, `max_tokens`). Empty on non-assistant rows. |
| `is_error` | bool | True when a `tool_result` block reports an error (`block.is_error`). False otherwise. |
| `cache_creation_input_tokens` | int64 | `usage.cache_creation_input_tokens` — prompt-cache creation tokens (split component) |
| `cache_read_input_tokens` | int64 | `usage.cache_read_input_tokens` — prompt-cache read tokens (split component) |
| `workspace` | string | Session workspace (denormalized) |
| `git_remote` | string | Git remote origin URL (denormalized from session) |
| `git_branch` | string | From JSONL `gitBranch` field per message line |
| `model` | string | Session model (denormalized) |
| `parent_session_id` | string | Set when message belongs to a subagent session |
| `is_subagent` | bool | True for subagent session messages |
| `source_line_index` | int32 | 0-based position in original JSONL file |
| `year` | int32 | Timestamp year |
| `week` | int32 | ISO week from timestamp |
| `month` | int32 | Timestamp month |
| `schema_version` | int32 | Schema version marker |

## `sessions` Dataset (`AgentSession`)

One row per parsed session file transformed.

| Field | Type | Meaning / Population |
|---|---|---|
| `id` | string | Session ID (agentId for subagents, sessionId for parents) |
| `parent_session_id` | string | Set for subagent sessions |
| `host_id` | string | Host identifier from `~/.auto/host.json`, fallback `os.Hostname()` |
| `agent` | string | Agent type, currently `"claude"` |
| `subagent_name` | string | From `.meta.json` agentType (e.g. "Explore", "general-purpose") |
| `is_subagent` | bool | True for subagent sessions |
| `workspace` | string | Session working directory |
| `git_remote` | string | Git remote origin URL, cached in `~/.auto/etl/settings.json` |
| `model` | string | Session model from parsed transcript |
| `source_path` | string | Source JSONL path |
| `permission_mode` | string | Claude Code permission mode for the session (e.g. `default`, `bypassPermissions`). Last-seen value from top-level `permissionMode` field on message lines. |
| `version` | string | Claude Code CLI version (e.g. `2.1.168`). Last-seen value from top-level `version` field on message lines. |
| `first_message_at` | int64 | Earliest non-zero message timestamp (Unix ms) |
| `last_message_at` | int64 | Latest non-zero message timestamp (Unix ms) |
| `total_input_tokens` | int64 | Sum of input + cache input tokens |
| `total_output_tokens` | int64 | Sum of output tokens |
| `total_tokens` | int64 | Sum of input + cache + output tokens |
| `total_bytes` | int64 | Total transformed text bytes |
| `total_output_bytes` | int64 | Bytes counted from non-user text blocks |
| `total_input_bytes` | int64 | Bytes counted from user text blocks |
| `transcript_full` | string | All messages concatenated with `[role]:` or `[tool:Name]:` prefixes. Excludes `role="thinking"` rows. |
| `transcript_truncated` | string | Same but per-message uses `content_truncated`, capped at 512k total. Excludes `role="thinking"` rows. |
| `year` | int32 | Year of first non-zero timestamp found |
| `month` | int32 | Month of first non-zero timestamp found |
| `schema_version` | int32 | Schema version marker |

## Truncation Rules

### Per-message truncation (`content_truncated`)

Default threshold: **4096 chars**. For messages exceeding this:
- Calculate excess chars to remove
- Cut symmetrically outward from the midpoint
- Insert marker `\n…[truncated {n} chars]…\n`
- `content` always retains the full unmodified value

### Session transcript truncation (`transcript_truncated`)

Default cap: **512k chars**. Same mid-truncation algorithm applied to the assembled transcript string.

## Git Remote Resolution

`git_remote` is resolved by running `git -C <workspace> remote get-url origin` on first encounter of each workspace. Results are cached in `~/.auto/etl/settings.json` under `remotes`. Empty string cached for non-git workspaces or missing paths.

## Normalization Notes

- Only line types `user`, `assistant`, `system` are normalized into message rows.
- A bare string message becomes one `messages` row.
- A content array message becomes one `messages` row per content block.
- Sessions without valid timestamps are skipped.
- `workspace`, `model`, `git_remote`, `parent_session_id`, `is_subagent` are denormalized onto each message row for easier analytics.
- `git_branch` is extracted per-message from the JSONL `gitBranch` field (can change mid-session via worktrees).
