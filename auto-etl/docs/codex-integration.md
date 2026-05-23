---
hash: "74375278"
id: "e277113a"
read_when: "adding Codex session ingestion to auto-etl or mapping Codex fields to normalized schema"
summary: "Requirements for ingesting OpenAI Codex session history into auto-etl normalized parquet datasets, covering file discovery, schema mapping, and Codex-specific idiosyncrasies."
title: "Codex Integration Requirements"
---

# Codex Integration Requirements

Add Codex CLI session history as a second ingestion source for auto-etl, producing the same normalized `messages` and `sessions` parquet datasets that the Claude parser already produces.

## Source Data

### Location

- Default path: `~/.codex/sessions/`
- Directory layout: `~/.codex/sessions/{year}/{month}/{day}/rollout-{ISO-timestamp}-{uuid}.jsonl`
- Example: `~/.codex/sessions/2026/04/29/rollout-2026-04-29T13-54-19-019dd984-e826-78b3-a46d-49fe16a7923d.jsonl`
- Override via `--input` flag (same as Claude source), or a new `--codex-input` flag if both sources run in a single invocation.

### Corpus Profile (current local data)

| Metric | Value |
|--------|-------|
| Session files | 52 |
| Total JSONL lines | ~22,000 |
| Total size on disk | 46 MB |
| Date range | 2026-03-16 to 2026-05-10 |
| Originator variants | `codex_cli_rs` (21), `codex-tui` (31) |
| Models observed | `gpt-5.3-codex` (33), `gpt-5.4` (18), `gpt-5.5` (1) |

### Secondary data files (out of scope)

- `~/.codex/history.jsonl` -- user prompt history
- `~/.codex/logs_2.sqlite` -- operational telemetry (application debugging, not session content)
- `~/.codex/state_5.sqlite` -- TUI metadata index over sessions (titles, token totals, git info). Secondary index, not a primary source.

### Codex worktree structure

The full `~/.codex` directory layout (explored from `.tmp/codex` worktree):

```
~/.codex/
├── config.toml              # User config (TOML): model, MCP servers, project trust, hooks, model migrations
├── version.json             # Latest version check: {"latest_version":"0.132.0","last_checked_at":"..."}
├── history.jsonl            # User prompt history (89KB)
├── state_5.sqlite           # Session metadata DB (threads table, 28 columns)
├── logs_2.sqlite            # Operational logs (capped at ~2000 rows)
├── log/
│   └── codex-tui.log        # TUI debug log (19MB, very large)
├── sessions/                # *** PRIMARY ETL SOURCE ***
│   └── {year}/{month}/{day}/rollout-*.jsonl
├── rules/
│   ├── default.rules        # Custom DSL: prefix_rule(pattern=[...], decision="allow")
│   └── ubs.md               # Markdown-formatted user rules
├── skills/
│   ├── {user-skill}/        # User-created skills (SKILL.md, references/, scripts/, subagents/)
│   └── .system/             # Built-in system skills (imagegen, openai-docs, plugin-creator, etc.)
├── memories/                # Empty (just .git subdir)
├── shell_snapshots/         # Empty
└── cache/
    └── codex_apps_tools/    # Cached tool definitions (JSON, 348KB)
```

**config.toml** uses TOML format (unlike Claude's JSON settings). Contains model config (`model = "gpt-5.5"`, `model_reasoning_effort = "xhigh"`), MCP server definitions, project trust levels, notify hooks, and model migration mappings (`"gpt-5.3-codex" = "gpt-5.4"`).

**rules/** contains a custom rule format (not standard TOML/JSON/YAML): `prefix_rule(pattern=["autosearch", "index"], decision="allow")`. Rules can also be plain markdown files. Not relevant for ETL but notable as a Codex-specific format.

**skills/** mirrors Claude Code's skill concept. User-created skills have a `SKILL.md`, `references/`, `scripts/`, and `subagents/` subdirectory. System skills live under `.system/`. Not relevant for session ETL but could inform `auto-skill` cross-tool support.

**For ETL purposes:** only `sessions/` and optionally `state_5.sqlite` matter. Everything else is configuration, caching, or empty.

## JSONL Line Format

Each Codex session file contains JSONL. Every line has a top-level `timestamp` (ISO 8601) and `type` field. The `type` determines the structure of `payload`.

### Line types

| Type | Description | Frequency |
|------|-------------|-----------|
| `session_meta` | Session metadata -- ID, cwd, model, git info, instructions | 1 per file |
| `response_item` | Messages, tool calls, tool results, reasoning blocks | ~65% of lines |
| `event_msg` | Operational events -- exec output, token counts, user messages | ~35% of lines |
| `turn_context` | Per-turn context snapshot -- model, cwd, settings | 1 per turn |

### `session_meta` payload

```json
{
  "id": "019dd984-e826-78b3-a46d-49fe16a7923d",
  "timestamp": "2026-04-29T13:54:19.535Z",
  "cwd": "/home/vscode/src/auto-stack",
  "originator": "codex-tui",
  "cli_version": "0.121.0",
  "source": "cli",
  "model_provider": "openai",
  "base_instructions": { "text": "..." },
  "git": {
    "commit_hash": "d1602e4b...",
    "branch": "main",
    "repository_url": "https://github.com/mistakenot/auto-stack"
  }
}
```

### `response_item` payload types

The `payload.type` field determines the sub-structure:

**`message`** -- conversation messages:
```json
{
  "type": "message",
  "role": "user|assistant|developer",
  "content": [{ "type": "input_text|output_text", "text": "..." }],
  "phase": "commentary"
}
```

**`function_call`** -- tool invocations:
```json
{
  "type": "function_call",
  "name": "exec_command",
  "arguments": "{\"cmd\":\"ls -la\"}",
  "call_id": "call_YBSwmxOFzgEabIb4YktZXvjy"
}
```

**`function_call_output`** -- tool results:
```json
{
  "type": "function_call_output",
  "call_id": "call_YBSwmxOFzgEabIb4YktZXvjy",
  "output": "total 176\ndrwxrwxr-x 30 vscode..."
}
```

**`reasoning`** -- model reasoning (encrypted):
```json
{
  "type": "reasoning",
  "summary": null,
  "content": null,
  "encrypted_content": "gAAAAABp8g2h..."
}
```

### `event_msg` payload types

| `payload.type` | Description | Key fields |
|-----------------|-------------|------------|
| `exec_command_end` | Command completion | `call_id`, `command[]`, `cwd`, `parsed_cmd[]`, `stdout`, `stderr`, `aggregated_output` |
| `token_count` | Rate limit info (no per-message token counts) | `rate_limits.primary.used_percent` |
| `agent_message` | Agent commentary text | `message`, `phase` ("commentary") |
| `patch_apply_end` | File edit result | `call_id`, `success`, `changes{path: {unified_diff}}` |
| `web_search_end` | Web search | `call_id`, `query`, `action` |
| `user_message` | User input | `message`, `images[]` |
| `task_started` | Turn begin | `turn_id`, `started_at` (unix seconds), `model_context_window` |
| `task_complete` | Turn end | `turn_id`, `last_agent_message`, `completed_at` (unix seconds), `duration_ms` |
| `mcp_tool_call_end` | MCP tool result | `call_id`, `invocation`, `duration`, `result` |
| `context_compacted` | Context window compaction event | (no payload fields) |

### `turn_context` payload

```json
{
  "turn_id": "...",
  "cwd": "/home/vscode/src/auto-stack",
  "model": "gpt-5.3-codex",
  "effort": "xhigh",
  "approval_policy": "never",
  "sandbox_policy": { "type": "danger-full-access" }
}
```

## Files Needed

### New files

| File | Purpose |
|------|---------|
| `internal/parser/codex.go` | Codex JSONL parser -- `ScanAndParseCodex()` returning `[]ParsedSession` |
| `internal/parser/codex_test.go` | Unit tests with fixture data |
| `internal/parser/testdata/codex/` | Codex session fixture files |

### Modified files

| File | Change |
|------|--------|
| `internal/parser/parser.go` | Extract shared types (`ParsedSession`, `ParsedLine`, `ParsedMessage`, `ParsedUsage`) if not already generic enough. No Claude-specific changes. |
| `internal/transform/transform.go` | Should work unchanged -- it consumes `[]ParsedSession` regardless of source. May need minor adjustments if Codex `ParsedLine` populates fields differently. |
| `cmd/run.go` | Add `--codex-input` flag (default `~/.codex/sessions`). Wire Codex scan into the pipeline. Add `"codex"` to the `--only` source filter. |
| `internal/model/model.go` | No schema changes expected -- existing fields cover Codex data. |

## Schema Mapping

### Session fields (`AgentSession`)

| Target field | Codex source | Notes |
|--------------|-------------|-------|
| `id` | `session_meta.payload.id` | UUID, already unique |
| `parent_session_id` | — | Codex has no subagent files. Always empty. |
| `host_id` | Runtime hostname (same as Claude) | |
| `agent` | `"codex"` | Hard-coded string |
| `subagent_name` | — | Always empty |
| `is_subagent` | — | Always `false` |
| `workspace` | `session_meta.payload.cwd` | |
| `git_remote` | `session_meta.payload.git.repository_url` | Normalize to match Claude format |
| `model` | `turn_context.payload.model` | May change across turns; use first observed or most frequent |
| `source_path` | Absolute path to the `.jsonl` file | |
| `first_message_at` | Earliest `timestamp` across all lines | Unix ms |
| `last_message_at` | Latest `timestamp` across all lines | Unix ms |
| `total_input_tokens` | — | Not available per-message. See idiosyncrasies. |
| `total_output_tokens` | — | Not available per-message. See idiosyncrasies. |
| `total_tokens` | — | Not available per-message. |
| `total_bytes` | Sum of byte lengths of all line payloads | |
| `total_output_bytes` | Sum of byte lengths of assistant messages + function_call lines | |
| `total_input_bytes` | Sum of byte lengths of user + developer message lines | |
| `transcript_full` | Built from message rendering (same logic as Claude) | |
| `transcript_truncated` | Truncated form of transcript | |

### Message fields (`AgentMessage`)

| Target field | Codex source | Notes |
|--------------|-------------|-------|
| `id` | Generated: `{session_id}-{index}` | Same pattern as Claude |
| `session_id` | From `session_meta.payload.id` | |
| `host_id` | Runtime hostname | |
| `index` | Sequential counter across emitted message rows | |
| `role` | See role mapping below | |
| `content` | See content extraction below | |
| `content_truncated` | Truncated form of `content` | |
| `timestamp` | `line.timestamp` | Parse ISO 8601 to unix ms |
| `tool_name` | `function_call.name` | e.g. `exec_command`, `_fetch_pr_comments` |
| `tool_input` | `function_call.arguments` | Raw JSON string |
| `tool_file_path` | From `patch_apply_end.changes` keys | Only for patch operations |
| `tool_file_start_line` | — | Not available for Codex `exec_command` |
| `tool_file_num_lines` | — | Not available |
| `tool_file_total_lines` | — | Not available |
| `bash_command` | `exec_command` arguments: `JSON.parse(arguments).cmd` | |
| `bash_exit_code` | From matching `exec_command_end` event via `call_id` | Requires join across line types |
| `skill_name` | — | Codex doesn't have skill metadata in the session format |
| `input_tokens` | — | Not available per-message |
| `cache_input_tokens` | — | Not applicable (OpenAI model) |
| `output_tokens` | — | Not available per-message |
| `workspace` | Denormalized from session | |
| `git_remote` | Denormalized from session | |
| `git_branch` | `session_meta.payload.git.branch` | Session-level only, not per-message like Claude |
| `model` | From `turn_context.payload.model` | |
| `parent_session_id` | — | Always empty |
| `is_subagent` | — | Always `false` |
| `source_line_index` | 0-based line number in the JSONL file | |

### Role mapping

| Codex value | Normalized role |
|-------------|----------------|
| `response_item` with `role: "user"` | `user` |
| `response_item` with `role: "assistant"` | `assistant` |
| `response_item` with `role: "developer"` | `system` |
| `response_item` with `type: "function_call"` | `assistant` (tool invocation) |
| `response_item` with `type: "function_call_output"` | `tool` |
| `event_msg` with `type: "user_message"` | `user` |
| `event_msg` with `type: "agent_message"` | `assistant` |

### Content extraction

| Line type | Content source |
|-----------|---------------|
| `message` (user/assistant) | Concatenate `content[].text` fields |
| `function_call` | `arguments` JSON string (raw) |
| `function_call_output` | `output` string |
| `reasoning` | `summary` text if present, otherwise skip (encrypted content is not usable) |
| `event_msg.agent_message` | `payload.message` |
| `event_msg.user_message` | `payload.message` |
| `event_msg.exec_command_end` | Not emitted as a separate message row -- used to enrich the matching `function_call_output` row with exit code and parsed command info |
| `event_msg.patch_apply_end` | Not emitted as a separate message row -- used to enrich the matching `function_call_output` row with file paths and diffs |

## Idiosyncrasies

### 1. No per-message token counts

Claude JSONL includes `usage.input_tokens` and `usage.output_tokens` on each assistant message. Codex does not. The `token_count` event only reports rate-limit percentages, not absolute counts.

**Impact:** `AgentMessage.input_tokens`, `cache_input_tokens`, `output_tokens` will all be 0 for Codex messages. Session-level token totals will also be 0.

**Mitigation:** Document this as a known gap. Downstream queries that filter or sort by token usage should handle 0 gracefully. Byte-based size fields remain populated.

### 2. Tool calls are a separate line type, not embedded in messages

In Claude, tool use is embedded inside assistant message content blocks. In Codex, tool calls are their own `response_item` lines with `type: "function_call"`, separate from the assistant `message` that initiated them.

**Impact:** The parser must emit `function_call` and `function_call_output` as their own message rows rather than extracting them from within an assistant message's content array.

### 3. `call_id` joins across line types

A `function_call` has a `call_id`. The matching result appears in a separate `function_call_output` line (same `call_id`), and operational details appear in `event_msg` lines like `exec_command_end` or `patch_apply_end` (also keyed by `call_id`).

**Impact:** To populate `bash_exit_code`, `bash_command`, and `tool_file_path`, the parser needs a two-pass approach or a lookup map built during parsing:
1. First pass (or accumulation): build a `map[call_id]` of event details.
2. When emitting the `function_call` message row, look up the matching event to enrich fields.

### 4. `exec_command` is the only tool (almost)

Codex uses a single `exec_command` tool for all shell operations. There is no `Read`, `Write`, `Edit`, or `Bash` distinction. File reads and writes happen via shell commands (`cat`, `rg`, `sed`, etc.) inside `exec_command`.

Rare exceptions: `_fetch_pr_comments`, `_search_prs`, `_list_recent_issues`, `_search_issues`, `write_stdin`, `_fetch_pr_comments`.

**Impact:**
- `tool_name` will almost always be `"exec_command"`.
- `tool_file_path` / `tool_file_start_line` / `tool_file_num_lines` are generally not extractable without command parsing (out of scope for v1).
- `bash_command` should be populated from `JSON.parse(arguments).cmd` for `exec_command` calls.

### 5. File edits use `apply_patch` via exec, not a dedicated tool

Codex applies patches by calling `exec_command` with an `apply_patch` heredoc, or via the `patch_apply_end` event which records success, unified diff, and affected file paths.

**Impact:** `patch_apply_end` events are the best source for file edit metadata. Match them to the originating `function_call` via `call_id` to populate `tool_file_path`.

### 6. `developer` role instead of `system`

Claude uses `role: "system"` for system instructions. Codex uses `role: "developer"` for the same purpose. The `developer` role carries permissions, collaboration mode, skill instructions, and AGENTS.md content.

**Impact:** Map `developer` to `system` in the normalized schema. These messages are typically very large (10-50KB of instructions). The existing truncation logic should handle them, but verify that transcript rendering doesn't bloat from instruction dumps.

### 7. Reasoning blocks are encrypted

Codex logs reasoning/chain-of-thought blocks with `type: "reasoning"`, but the `content` field is `null` and `encrypted_content` contains an opaque encrypted blob. The `summary` field is also typically `null`.

**Impact:** Skip reasoning blocks entirely during message row emission. They contain no usable content for the normalized dataset.

### 8. Timestamps are ISO 8601 strings (not unix epoch)

Claude JSONL timestamps are also ISO strings, so this is consistent. However, some `event_msg` payloads use unix seconds (not milliseconds) for fields like `started_at` and `completed_at` in `task_started`/`task_complete`.

**Impact:** Be careful with timestamp parsing. Top-level `timestamp` is ISO 8601 (parse to unix ms). Event payload timestamps like `started_at` are unix seconds (multiply by 1000).

### 9. No subagent / sidechain sessions

Claude Code has a concept of subagent sessions (`isSidechain: true`, separate session files per agent). Codex has no equivalent. All activity in a session file is from a single agent.

**Impact:** `parent_session_id`, `subagent_name`, `is_subagent` are always empty/false. No need for the deduplication logic used for Claude subagent sessions.

### 10. git_branch is session-level only

Claude provides `gitBranch` per JSONL line, allowing branch tracking as the user switches branches mid-session. Codex only records the branch once in `session_meta.payload.git.branch`.

**Impact:** `git_branch` on message rows will be the same value for all messages in a session. Branch switches within a session won't be captured.

### 11. Multiple turns per session file

A single Codex session file can contain multiple user turns (each bracketed by `task_started` / `task_complete` events). This is similar to Claude but worth noting: the file does not represent a single request-response pair.

### 12. Parallel tool calls

Codex frequently issues multiple `function_call` lines in sequence before their `function_call_output` lines arrive. The `call_id` is the only way to match calls to results.

**Impact:** The parser must not assume a strict call-then-result alternation. Use the `call_id` map approach described in idiosyncrasy #3.

### 13. `aggregated_output` in exec_command_end

The `exec_command_end` event has both `stdout`/`stderr` fields (often empty) and an `aggregated_output` field that contains the full command output. The `function_call_output.output` for the same `call_id` typically has a chunked/truncated version.

**Impact:** For `content` on tool result rows, prefer `function_call_output.output` (this is what the model saw). For potential future enrichment, `exec_command_end.aggregated_output` has the untruncated version.

### 14. Session filenames encode the session ID

The filename pattern `rollout-{ISO-timestamp}-{uuid}.jsonl` contains the same UUID as `session_meta.payload.id`. This could be used for fast lookup/filtering without parsing, but the `session_meta` line is the authoritative source.

## Implementation Patterns

Follow the existing Claude ETL patterns. The codebase is structured as a three-phase pipeline: parse -> transform -> write. Codex should slot into the same pipeline by producing the same intermediate types (`ParsedSession` / `ParsedLine`) and reusing transform and write unchanged.

### Parser architecture

The Claude parser (`internal/parser/parser.go`) exposes two public functions:

- `ScanAndParse(inputDir, ...ProgressFunc) ([]ParsedSession, error)` -- walks a directory, parses all `.jsonl` files, returns parsed sessions. Skips unparseable files silently.
- `ParseSession(path) (*ParsedSession, error)` -- parses a single file.

Follow the same pattern for Codex in `internal/parser/codex.go`:

- `ScanAndParseCodex(inputDir, ...ProgressFunc) ([]ParsedSession, error)`
- `ParseCodexSession(path) (*ParsedSession, error)`

Both parsers return `[]ParsedSession` using the same shared types. The transform layer doesn't know or care which parser produced the data.

Key parser conventions to follow:
- Use a 1MB `bufio.Scanner` buffer (line 139 of `parser.go`) -- Codex lines can be large too (developer role messages with full AGENTS.md).
- Skip malformed lines with `continue`, don't fail the whole file.
- Track `lineIndex` manually for `SourceLineIndex` population.
- Extract session-level metadata (workspace, model, ID) from the first line that has it, accumulating as you scan.

### call_id lookup map

The Codex parser needs a pattern the Claude parser doesn't: joining across line types via `call_id`. The Claude parser handles tool_use/tool_result matching within a single content block array using `buildToolUseIndex()` in transform. Codex needs a similar index but at the parser level, since tool calls and results are separate JSONL lines.

Recommended approach: single-pass parsing that builds two structures simultaneously:
1. The `ParsedLine` slice (same as Claude).
2. A `map[string]*callContext` keyed by `call_id`, populated from `exec_command_end` and `patch_apply_end` events.

After the scan loop, enrich `function_call` ParsedLines with data from their matching `callContext` (exit code, cwd, file paths from patches). This avoids a second pass.

### Reuse ParsedSession / ParsedLine

The shared types in `parser.go` already have everything Codex needs:

| Field | Claude usage | Codex usage |
|-------|-------------|-------------|
| `ParsedSession.ID` | From `sessionId` field | From `session_meta.payload.id` |
| `ParsedSession.IsSubagent` | From `isSidechain` | Always `false` |
| `ParsedSession.AgentID` | From `agentId` | Not used |
| `ParsedSession.SubagentName` | From `.meta.json` | Not used |
| `ParsedLine.Type` | `"user"`, `"assistant"`, `"system"` | Map Codex types to these same values |
| `ParsedLine.GitBranch` | Per-line `gitBranch` | Session-level, copied to all lines |
| `ParsedLine.Message.Usage` | Per-assistant-message | Zero (not available) |

The key mapping work happens in the Codex parser: translate Codex's `response_item` / `event_msg` structure into `ParsedLine` with `Type` set to `"user"`, `"assistant"`, or `"system"` so the existing transform recognizes them.

### Which Codex line types become ParsedLines

Not every Codex JSONL line becomes a ParsedLine. Follow the same filtering the Claude parser does (it only keeps lines with `Type` of `user`, `assistant`, or `system`):

| Codex line | Emit as ParsedLine? | Mapped Type |
|------------|---------------------|-------------|
| `response_item` type `message`, role `user` | Yes | `"user"` |
| `response_item` type `message`, role `assistant` | Yes | `"assistant"` |
| `response_item` type `message`, role `developer` | Yes | `"system"` |
| `response_item` type `function_call` | Yes | `"assistant"` |
| `response_item` type `function_call_output` | Yes | `"assistant"` (content block with role `tool`) |
| `response_item` type `reasoning` | No | Skip (encrypted) |
| `event_msg` type `user_message` | Yes | `"user"` |
| `event_msg` type `agent_message` | Yes | `"assistant"` |
| `event_msg` type `exec_command_end` | No | Consumed by call_id map |
| `event_msg` type `patch_apply_end` | No | Consumed by call_id map |
| `event_msg` type `token_count` | No | No usable data |
| `event_msg` type `task_started` / `task_complete` | No | Session-level metadata only |
| `event_msg` type `web_search_end` | No | Consumed by call_id map if needed |
| `turn_context` | No | Session-level metadata only |
| `session_meta` | No | Session-level metadata only |

### Content block format

The Claude transform expects `ParsedMessage.Content` to be either a bare JSON string or an array of `ContentBlock` structs (with types `text`, `tool_use`, `tool_result`). The Codex parser must translate Codex's content format into this same shape.

For Codex `message` items, content is always an array like `[{"type": "input_text", "text": "..."}]` or `[{"type": "output_text", "text": "..."}]`. Concatenate the `.text` fields and emit as a bare JSON string in `ParsedMessage.Content`, e.g. `json.RawMessage(strconv.Quote(joined))`.

For `function_call` items, construct a synthetic `tool_use` content block array so the transform's existing `tool_use` handler picks it up. For `function_call_output` items, construct a synthetic `tool_result` content block array.

This keeps the transform layer completely unchanged.

### Transform reuse

`internal/transform/transform.go` should work as-is for Codex sessions. It:
- Filters lines by `Type` (`"user"`, `"assistant"`, `"system"`) -- Codex parser maps to these.
- Calls `ParseContentBlocks()` which handles bare strings and block arrays -- Codex parser provides these.
- Builds `toolUseIndex` from `tool_use` blocks -- Codex parser synthesizes these.
- Accumulates token usage -- will be zero for Codex, which is fine.
- Sets `Agent` to `"claude"` -- the Codex integration must override this. Either:
  - (a) Add an `Agent` field to `ParsedSession` and use it in `transformSession` instead of hard-coded `"claude"`, or
  - (b) Set it in a post-transform fixup.

Option (a) is cleaner. Add `Agent string` to `ParsedSession`, set it to `"claude"` in the Claude parser and `"codex"` in the Codex parser, and change `transformSession` line 337 from `Agent: "claude"` to `Agent: raw.Agent`.

### Writer reuse

`internal/writer/writer.go` is fully generic over `model.AgentMessage` and `model.AgentSession`. Claude and Codex rows go into the same partitioned parquet files. No changes needed.

This is intentional: downstream tools (autosearch, autoreflect) query a single unified dataset. Mixed Claude + Codex sessions in the same partition are fine.

### CLI surface

Follow the `--only` flag pattern established in `cmd/run.go`:

1. Add `"codex"` to `validOnlyValues`.
2. Add a `--codex-input` flag (default `~/.codex/sessions`).
3. When `sources["codex"]` is true (or when running all sources by default), call `ScanAndParseCodex(codexInputDir, ...)`.
4. Merge the returned `[]ParsedSession` with the Claude sessions before passing to `transform.Transform()`.

The `--only sessions` value should continue to mean Claude sessions. Codex is a new value: `--only codex`. Running without `--only` runs all sources.

Alternatively, rename `"sessions"` to `"claude"` for clarity, but that's a breaking change. Prefer adding `"codex"` alongside `"sessions"` for now.

### Progress reporting

Follow the existing `progress.Bar` pattern. The Claude parse phase shows `parsing [████░░] 142/280`. The Codex parse phase should show a separate bar:

```
parsing        [██████████████████████████████] 280/280
parsing codex  [██████████████████████████████] 52/52
transforming   [██████████████████████████████] 332/332
```

The transform phase processes all sessions together (Claude + Codex merged) in a single worker pool.

### Debug timing

Follow the `if debug { phaseStart = time.Now() }` / `fmt.Fprintf(os.Stderr, "[debug] ...")` pattern from `runSessionETL`. Add a `[debug] parse codex: Xms` line.

### Test fixtures

Follow the existing testdata convention:

```
internal/parser/testdata/
  parent-session/session.jsonl          -- Claude fixture
  with-subagent/...                     -- Claude fixture
  codex-basic/session.jsonl             -- Codex: single turn, one exec_command
  codex-multi-turn/session.jsonl        -- Codex: multiple turns with task_started/task_complete
  codex-parallel-calls/session.jsonl    -- Codex: parallel function_calls with interleaved results
  codex-patch/session.jsonl             -- Codex: patch_apply_end with file diffs
```

Each fixture should be a minimal, hand-crafted JSONL file (not a copy of real data). Include only the lines needed to exercise the behavior under test. Follow the existing fixtures: 2-5 lines each, realistic field values, stable UUIDs.

### Unit test coverage

Follow the existing test structure -- each test focuses on one behavior:

**Parser tests** (`internal/parser/codex_test.go`):
- `TestParseCodexSession_BasicMetadata` -- session ID, workspace, model, agent from `session_meta`
- `TestParseCodexSession_GitMetadata` -- git_branch, git_remote from `session_meta.payload.git`
- `TestParseCodexSession_MessageRoles` -- user, assistant, developer role mapping
- `TestParseCodexSession_FunctionCallEmission` -- `exec_command` becomes a ParsedLine with tool_name and bash_command
- `TestParseCodexSession_FunctionCallOutputJoin` -- `function_call_output` content is populated
- `TestParseCodexSession_CallIdEnrichment` -- `exec_command_end` exit code flows to the function_call line
- `TestParseCodexSession_PatchFilePathExtraction` -- `patch_apply_end` file paths flow to the function_call line
- `TestParseCodexSession_ReasoningSkipped` -- encrypted reasoning blocks produce no ParsedLines
- `TestParseCodexSession_DeveloperMappedToSystem` -- `developer` role becomes `"system"` type
- `TestParseCodexSession_NoSubagentFields` -- `IsSubagent` always false, `ParentSessionID` always empty
- `TestParseCodexSession_SourceLineIndex` -- line indices match JSONL positions

**Transform tests** (`internal/transform/transform_test.go`):
- Add a `makeCodexSession()` helper analogous to `makeParentSession()`.
- `TestTransformSession_CodexAgentField` -- session.Agent == "codex"
- `TestTransformSession_CodexZeroTokens` -- all token fields are 0
- `TestTransformSession_CodexTranscriptPopulated` -- transcript_full and transcript_truncated are non-empty
- `TestTransformSession_CodexBashCommand` -- bash_command populated from exec_command args

### End-to-end testing

The Claude ETL has a robust e2e test framework in `e2e_test.go` that the Codex integration must extend. The pattern:

1. **`genstats`** (`cmd/genstats/main.go`) independently analyzes raw JSONL files and produces `stats.json` with ground-truth metrics (file counts, line types, content blocks, roles, session IDs, subagent info). It intentionally imports zero auto-etl packages — it's a fully independent implementation so it can serve as an oracle.
2. **`TestMain`** builds the binary, runs the full pipeline against `.tmp/claude/projects`, loads `stats.json`, and stores the results for all test functions.
3. **Test functions** read back parquet output and cross-validate against genstats metrics: message counts, session counts, role distributions, required fields, idempotency, truncation, transcripts, git metadata, subagent dedup.

#### genstats-codex

Create `cmd/genstats-codex/main.go` — an independent analyzer for Codex JSONL that produces a `codex-stats.json` with equivalent metrics. This must NOT import any auto-etl packages.

Key differences from the Claude genstats:

| Metric | Claude genstats | Codex genstats |
|--------|----------------|----------------|
| Line types | `type` field on each line (user/assistant/system) | `type` field (session_meta/response_item/event_msg/turn_context) + `payload.type` for subtypes |
| Content blocks | `message.content` array or bare string | `payload.content` array (input_text/output_text) for messages, `payload.arguments` for function_calls, `payload.output` for function_call_output |
| Roles | `message.role` | `payload.role` for messages, inferred for function_call (assistant) and function_call_output (tool) |
| Session ID | `sessionId` field on lines | `session_meta.payload.id` (one per file) |
| Subagents | `isSidechain` + `.meta.json` | None (always parent) |
| Tool uses | content blocks with `type: "tool_use"` | `response_item` lines with `type: "function_call"` |

The stats struct should track:
- `TotalFiles`, `EmptyFiles`, `UnparseableFiles`
- `TotalLines`, `UnparseableLines`
- `LinesByType` (session_meta, response_item, event_msg, turn_context)
- `ResponseItemsByType` (message, function_call, function_call_output, reasoning)
- `EventMsgsByType` (exec_command_end, user_message, agent_message, etc.)
- `MessagesByRole` (user, assistant, developer)
- `FunctionCallsByName` (exec_command, _fetch_pr_comments, etc.)
- `UniqueSessionIDs`, `FilesWithSessionID`
- Per-file details for debugging

Expected message count derivation: each `response_item` with type `message` (user/assistant/developer), `function_call`, or `function_call_output` becomes one message row. Plus each `event_msg` with type `user_message` or `agent_message`. Reasoning blocks are skipped.

#### TestMain changes

Extend `TestMain` in `e2e_test.go` to also run the Codex pipeline:

```go
// After existing Claude pipeline run...
codexInputDir := filepath.Join(".", ".tmp", "codex", "sessions")
if _, err := os.Stat(codexInputDir); err == nil {
    // Run genstats-codex
    codexStatsPath := filepath.Join(".", ".tmp", "codex-stats.json")
    genCodexStats := exec.Command("go", "run", "./cmd/genstats-codex", codexInputDir, codexStatsPath)
    // ...

    // Run pipeline with --codex-input
    run := exec.Command(bin, "run", "--codex-input", codexInputDir, "--output", codexOutputDir, "--only", "codex")
    // ...

    fixtureCodexStats = loadStats(codexStatsPath)
    fixtureCodexOutputDir = codexOutputDir
    fixtureCodexReady = true
}
```

Use a separate output directory for Codex-only runs so counts can be validated independently without Claude data mixed in.

#### E2E test cases for Codex

Mirror the Claude e2e tests, adapted for Codex-specific properties:

**Count validation** (same pattern as `TestE2E_MessageCount` / `TestE2E_SessionCount`):
- `TestE2E_Codex_SessionCount` — parquet session count matches genstats expected sessions
- `TestE2E_Codex_MessageCount` — parquet message count matches genstats expected messages

**Required fields** (same pattern as `TestE2E_SessionsHaveRequiredFields`):
- `TestE2E_Codex_SessionsHaveRequiredFields` — ID, FirstMessageAt, LastMessageAt non-zero
- `TestE2E_Codex_AgentField` — all sessions have `Agent == "codex"`
- `TestE2E_Codex_NoSubagents` — no sessions have `IsSubagent == true`
- `TestE2E_Codex_NoParentSessionID` — all sessions have empty `ParentSessionID`

**Role mapping** (same pattern as `TestE2E_ToolUseBecomesAssistantRole`):
- `TestE2E_Codex_RoleDistribution` — function_calls map to assistant, function_call_output maps to tool, developer maps to system

**Transcripts** (same pattern as `TestE2E_TranscriptsPopulated`):
- `TestE2E_Codex_TranscriptsPopulated` — TranscriptFull and TranscriptTruncated non-empty
- `TestE2E_Codex_TranscriptRolePrefixes` — transcripts contain `[user]:` and `[assistant]:`

**Token fields** (Codex-specific):
- `TestE2E_Codex_ZeroTokens` — all message-level token fields are 0

**Tool metadata** (Codex-specific):
- `TestE2E_Codex_BashCommandPopulated` — messages with `tool_name == "exec_command"` have non-empty `bash_command`

**Git metadata** (same pattern as `TestE2E_GitBranchPopulated`):
- `TestE2E_Codex_GitBranchPopulated` — at least some messages have git_branch

**Idempotency** (same pattern as `TestE2E_Idempotent`):
- `TestE2E_Codex_Idempotent` — running twice produces the same counts

**Empty input** (same pattern as `TestE2E_EmptyInput`):
- `TestE2E_Codex_EmptyInput` — empty codex input dir produces no output

#### Test data

`.tmp/codex/` is already populated with a full Codex worktree (52 session files, 46MB). The e2e tests use `.tmp/codex/sessions/` as the Codex input directory, mirroring how `.tmp/claude/projects/` is used for Claude.

Both directories are gitignored and not checked in. Tests skip gracefully when fixture data is absent (`skipIfNoFixture` pattern).

#### Manual verification with DuckDB

For development iteration (not a substitute for the automated e2e tests above):

```sql
-- Verify codex sessions exist
SELECT agent, count(*) FROM '.tmp/output/sessions/**/*.parquet' GROUP BY agent;

-- Verify bash_command population
SELECT tool_name, bash_command, content_truncated
FROM '.tmp/output/messages/**/*.parquet'
WHERE session_id IN (
  SELECT id FROM '.tmp/output/sessions/**/*.parquet' WHERE agent = 'codex'
)
AND tool_name != ''
LIMIT 10;

-- Verify zero tokens (expected)
SELECT sum(input_tokens), sum(output_tokens)
FROM '.tmp/output/messages/**/*.parquet'
WHERE session_id IN (
  SELECT id FROM '.tmp/output/sessions/**/*.parquet' WHERE agent = 'codex'
);
```

### Performance

The existing pipeline already parallelizes transform across CPUs (`workers := min(runtime.NumCPU(), total)` in `transform.go`). Codex sessions merge into the same worker pool, so no new parallelization is needed.

Watch for:
- **Large developer messages**: the `developer` role messages can be 10-50KB of system instructions. These flow through `MidTruncate` and transcript building. The existing truncation handles this, but verify that transcript rendering with large system messages doesn't produce bloated `transcript_truncated`.
- **call_id map memory**: the per-session `map[call_id]*callContext` stays in scope only for a single session parse. Largest observed session is ~1900 lines with ~300 tool calls -- the map will be small.
- **Scanner buffer**: the 1MB buffer is sufficient. Codex lines are comparable in size to Claude lines.

### Incremental runs

The writer's incrementality logic (skip past partitions, regenerate current period) works identically for mixed Claude+Codex data. Since both sources produce the same `model.AgentMessage` / `model.AgentSession` types, a partition may contain rows from both agents. This is correct -- the partition is time-based, not agent-based.

A `--full` rebuild will regenerate all partitions including Codex data.

## Rollout and Data Safety

### The problem

Source session files can disappear. Claude Code may garbage-collect old sessions, users may reclaim disk space, or a host rebuild may wipe `~/.claude/projects` and `~/.codex/sessions`. Once source files are gone, the autoetl parquet output is the **sole surviving copy** of that session data.

Today's `--full` rebuild is destructive: it deletes the entire output directory and regenerates from source. If any source files were lost between the original run and the rebuild, those sessions are permanently gone. This is already a risk with Claude-only data. Adding Codex doubles the surface area.

### Current state (as of 2026-05-20)

**What exists:**
- Incremental runs skip past partitions if the file exists (`writer.go:33`), so day-to-day runs are non-destructive.
- The GitHub writer (`writer/github.go`) has a read-merge-write pattern for updating existing partitions with deduplication. This pattern exists in the codebase but is not used by the session/message writer.
- All 523 Claude source files currently still exist on disk (verified). No data has been lost yet.

**What does NOT exist:**
- `~/.auto/etl/raw/` is specified in the project CLAUDE.md as the location for immutable raw transcript copies, but the directory does not exist and no code creates it or copies files there.
- No raw file backup step runs during `autoetl run`.
- No merge mode for session/message partition updates (only the GitHub writer has this).
- No safety gate on `--full` -- it calls `os.RemoveAll(outputDir)` (`run.go:68`) unconditionally with no backup, no confirmation, and no check for missing source files.
- No way to detect whether source files have been deleted since the last ETL run.
- No warning if a `--full` rebuild would produce fewer sessions than the existing output.

**Risk:** if Claude Code or the OS cleans up old session files and someone later runs `autoetl run --full`, those sessions are permanently lost. This is a pre-existing gap that applies to Claude data today, independent of Codex. Adding Codex doubles the number of source directories exposed to this risk.

### Rollout order

Adding Codex support touches the parse and CLI layers but not the output schema or partition layout. The rollout is:

1. **Ship the Codex parser and CLI flag.** Incremental runs are safe -- past partitions with Claude-only data are never touched. New Codex sessions appear in the current period's partitions alongside Claude sessions.

2. **Backfill past Codex sessions.** Past Codex sessions (March-April 2026) won't appear in the output until those partitions are regenerated. Use the merge approach (see below), not `--full`.

3. **Never run `--full` without a raw backup in place.** Document this as a hard rule.

### Raw file backup (prerequisite for safe rebuilds)

The project CLAUDE.md already specifies `~/.auto/etl/raw` as the location for immutable copies of raw transcripts. This isn't implemented yet. It must be implemented before `--full` can be considered safe.

The backup step should run as the first phase of `autoetl run`, before parsing:

1. Walk source directories (`~/.claude/projects`, `~/.codex/sessions`).
2. For each JSONL file, copy it to `~/.auto/etl/raw/{agent}/{relative-path}` if not already present.
3. Never overwrite or delete files in `raw/`. It is append-only.
4. When running `--full`, regenerate from `raw/` rather than from the original source locations.

This means:
- First incremental run: copies all source files to `raw/`, then parses from source as usual.
- Source files get deleted: no impact -- `raw/` still has the copies.
- `--full` rebuild: deletes output, regenerates from `raw/` -- no data loss.

Layout:
```
~/.auto/etl/raw/
  claude/
    -home-vscode-src-auto-stack/098819dd-4702-4dcd-b099-92efca0a9b28.jsonl
    -home-vscode-src-auto-stack/098819dd.../subagents/agent-a612e7d4.jsonl
  codex/
    2026/04/29/rollout-2026-04-29T13-54-19-019dd984-e826-78b3-a46d-49fe16a7923d.jsonl
```

### Merge mode for partition updates

For backfilling past Codex sessions into existing partitions, follow the pattern already established by `WriteGitHub` in `writer/github.go`:

1. Read existing rows from the partition's parquet file.
2. Merge with new rows, deduplicating by session/message ID.
3. Write the merged result back.

This avoids the delete-and-rebuild problem entirely. Add a `--backfill` flag or make this the default behavior when past partitions need updating (e.g., when a new source is detected that has data in a time range already covered by existing partitions).

### --full rebuild safety

Until the raw backup is implemented, `--full` should:

1. Check whether `~/.auto/etl/raw/` exists and is populated.
2. If not, refuse to run and print: `error: --full requires raw backups; run without --full first to create them, or use --full --force to proceed without backup`.
3. If `--force` is provided, proceed but print a warning to stderr: `warning: --full without raw backup may lose session data if source files have been deleted`.

### Phased implementation

| Phase | What | Why |
|-------|------|-----|
| 1 | Codex parser + `--codex-input` flag | Core feature. Incremental runs are safe. |
| 2 | Raw file backup in `~/.auto/etl/raw/` | Prerequisite for safe rebuilds. Covers both Claude and Codex. |
| 3 | Merge-mode writer for partition updates | Enables backfill of past Codex sessions without `--full`. |
| 4 | `--full` safety gate | Prevents accidental data loss. |

Phase 1 can ship independently. Phases 2-4 are data-safety improvements that apply to both Claude and Codex and should be implemented before anyone relies on `--full` in a production workflow.

## Out of Scope

- `~/.codex/history.jsonl`, `logs_2.sqlite`, `state_5.sqlite` ingestion (secondary indexes, not primary source)
- `config.toml`, `rules/`, `skills/`, `cache/` ingestion (configuration, not session data)
- Parsing `exec_command` arguments to infer file read/write/edit operations
- Decrypting reasoning blocks
- Token count estimation from content length
