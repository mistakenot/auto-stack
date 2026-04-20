---
hash: "c854a908"
id: "e712776c"
read_when: "planning or building autosearch CLI surface and query primitives"
summary: "Requirements for autosearch: indexing normalized session history from autoetl and exposing BM25 search, session/message retrieval, and reflection workflow primitives."
title: "AutoSearch Requirements"
---

# AutoSearch Requirements

This document follows [`docs/user-journey.md`](../../docs/user-journey.md), which is the authoritative source for the intended `autosearch` user experience. Older design sketches that describe commands such as `triage`, `list`, `fetch`, `mine`, or `stats` are superseded by the journey and should not be treated as the current CLI contract.

## 1. Purpose

`autosearch` indexes normalized coding-session history from `autoetl` and exposes retrieval primitives that are useful for agents, reflection workflows, and later automation.

Its main jobs are:

- provide one unified `search` command over both message history and session transcripts
- support both precise message-level retrieval and session-level transcript ranking
- provide session/message inspection helpers that fit in an agent context window
- expose a rule-based `fix` mode that highlights suspicious patterns for follow-up

`autosearch` is primarily a background and reflection tool. Coding agents may call it directly, but the main workflow is that it powers later `autoreflect` analysis.

## 2. CLI Principles

- Agents are the primary users.
- Command output defaults to JSON unless explicitly noted otherwise.
- Human-readable and LLM-friendly text output is allowed where the command is specifically about transcript retrieval or guided follow-up.
- Search-style JSON responses use a shared top-level envelope:
  - `_meta` for machine metadata
  - `hits` for result rows
- JSON-returning machine commands may accept `--request-id` and should echo it back in machine-readable metadata.
- Search queries use an app-level query language rather than raw SQLite FTS syntax.
  - support plain text terms by default
  - support quoted phrases
  - support uppercase boolean operators: `AND`, `OR`, `NOT`
  - translate this query language into engine-specific syntax internally
- Date filtering follows the stack-wide convention:

```bash
--since 5m
--since 5d
--since 1w
--after 2026-01-01 --before 2026-02-01
```

- `autosearch init` must create `~/.auto/settings.json` if it does not already exist, then create `~/.auto/search/settings.json`.
- `autosearch` should follow the common stack conventions for `init`, `quickstart`, `docs`, and `doctor`, even though the journey focuses mainly on indexing and retrieval commands.

## 3. Inputs and Derived Storage

`autosearch` reads the partitioned Parquet datasets produced by `autoetl`. The canonical source schema lives in `auto-etl/internal/model/model.go`.

Default input:

```bash
~/.auto/etl/output
```

`autosearch` builds local derived indexes and metadata, but does not mutate the Parquet input.

### Index Lifecycle

- `autosearch init` creates `~/.auto/search/settings.json`.
- `autosearch index` performs an incremental index over the configured Parquet input path.
- The default index name is `default`.
- Named indexes are stored as SQLite files under `~/.auto/search/<name>.sqlite`.
- `autosearch index` defaults to the `default` named index and to `~/.auto/etl/output` as input.
- The journey also expects alternate inputs to be possible, including named indexes over other sources such as S3-backed inputs.
- Search indexes should be built from truncated search-friendly columns only:
  - message-scope search indexes `messages.content_truncated`
  - session-scope search indexes `sessions.transcript_truncated`
- Full untruncated content remains available for retrieval helpers such as `message get`.

Example:

```bash
# creates ~/.auto/search/settings.json
autosearch init

# incremental index over ~/.auto/etl/output
# stores state in ~/.auto/search/default.sqlite
autosearch index

# alternate named index over another input source
# stores state in ~/.auto/search/s3index.sqlite
autosearch index --name s3index --input s3://mybucket --key ~/.ssh/my_key
```

### Minimum Derived Index Shape

The journey currently describes one explicit session-level search concept:

- `sessions_complete`
  - one row per session
  - concatenated session transcript
  - long tool outputs mid-truncated
  - full-text indexing over the transcript and other key fields

That concept makes sense for session-level transcript ranking, but it is not sufficient by itself for the full command surface in the journey. `autosearch` also needs message-level lookup and indexing. The minimum practical derived layout should therefore be:

- `sessions`
  - one row per session
  - key fields mirrored from `autoetl`, including:
    - `id`
    - `parent_session_id`
    - `host_id`
    - `workspace`
    - `git_remote`
    - `model`
    - `first_message_at`
    - `last_message_at`
    - token and byte totals
    - transcript preview fields needed for helper commands
- `sessions_fts`
  - session-level full-text index
  - one searchable document per session
  - transcript text built from the session transcript with long tool outputs mid-truncated
  - used primarily for `autosearch search --scope sessions`
- `messages`
  - one row per message
  - key fields mirrored from `autoetl`, including:
    - `id`
    - `session_id`
    - `index`
    - `role`
    - `timestamp`
    - `content`
    - `content_truncated`
    - `tool_name`
    - `tool_input`
    - `tool_file_path`
    - `bash_command`
    - denormalized session metadata such as `workspace`, `git_remote`, and `model`
  - supports direct lookup by `messageId` and ordered message traversal within a session
- `messages_fts`
  - message-level full-text index
  - used primarily for `autosearch search --scope messages`
  - must support snippet extraction and neighboring message references
- `index_state`
  - index metadata and incremental indexing state
  - includes at least:
    - index name
    - schema version
    - configured input path
    - dataset name
    - partition key
    - source file path
    - source file size
    - source file modification time
    - indexed_at
    - source schema version

### Incremental Indexing Policy

V1 indexing should be deterministic and simple:

- always reindex the newest `messages` partition and newest `sessions` partition
- reindex older partitions only when source path, file size, modification time, or schema version has changed
- force a full rebuild when the local index schema version changes
- keep the incremental state in `index_state`

The internal names may differ, and `sessions_complete` can be implemented as a concrete table, a view, or the logical combination of `sessions` plus `sessions_fts`. What matters is that the derived storage supports:

- session-level transcript ranking for BM25
- message-level references for message-scope hits
- lookup by `sessionId`
- lookup by `messageId`
- neighboring message lookup within a session
- session and message inspection helper commands

## 4. Required Command Surface

The authoritative command surface from the journey is:

```bash
autosearch init
autosearch index [--name NAME] [--input PATH] [--key PATH]

autosearch search "query" [--scope messages|sessions] [--mode bm25|semantic] [--index NAME] [--since T] [--after A --before B] [--cwd PATH] [--remote GIT_REMOTE] [--highlight] [--request-id ID]
autosearch fix [--index NAME] [--since T] [--after A --before B] [--cwd PATH] [--remote GIT_REMOTE]

autosearch session get <session_id>
autosearch session describe <session_id> [--request-id ID]

autosearch message get <message_id>
autosearch message describe <message_id> [--request-id ID]
autosearch message similar <message_id> [--request-id ID]
```

Notes:

- `search` is the one unified retrieval command.
- `--scope messages` is the default scope.
- `--scope sessions` searches full session transcripts.
- `--mode bm25` is the default mode.
- `--mode semantic` is a stretch goal for a later version.
- `--index` defaults to `default`.
- For commands that filter by repository, `--cwd` defaults to the current working directory.
- `--cwd` filters by local workspace path only.
- `--remote` filters by `git_remote` and is the cross-host/project-identity filter.
- `--cwd` and `--remote` are mutually exclusive in v1.
- `--highlight` applies to message-scope search snippets.
- `--request-id` is for machine callers and should be accepted only on JSON-returning commands where it makes sense.
- `fix` is deferred to v2 and is not part of the smallest implementation slice.
- `message similar` is part of the intended CLI contract, but is deferred to v2 because it likely depends on semantic-search work.
- The journey uses `--cwd` rather than `--workspace`; requirements should follow that.

## 5. Search Behavior

### Unified Search Command

`autosearch search` is the primary retrieval primitive. `--scope` controls what is searched, and `--mode` controls how.

Requirements:

- default scope is `messages`
- default mode is `bm25`
- the same top-level filters apply across scopes unless explicitly noted otherwise
- search and `fix` should support both `--cwd` and `--remote`
- if both `--cwd` and `--remote` are provided, fail fast with a standard CLI usage error
- the search response shape is always:

```json
{
  "_meta": {},
  "hits": []
}
```

Shared `_meta` fields:

- `scope`: `"messages"` or `"sessions"`
- `elapsed_ms`: integer timing value
- `total_matches`: total hit count
- `request_id`: echoed from `--request-id`, or the empty string when none was provided
- `wildcard_fallback`: boolean indicating whether automatic wildcard broadening was used

### Message-Scope Search

`autosearch search "Exit code 0"` defaults to message-scope search.

Requirements:

- searches individual messages and returns precise message-level hits
- executes against the message-level search corpus
- supports `--since`, `--after`, `--before`, `--index`, `--cwd`, `--remote`, `--highlight`, and `--request-id`
- time filters are based on message timestamp
- the searchable text source is `messages.content_truncated`
- each hit includes a stable `id` derived from the query, parameters, and result set
- each hit identifies the matching session and the matching message context
- `--highlight` wraps matching terms in `**bold**` markers in `snippet`
- `snippetStartIndex` and `snippetEndIndex` refer to the raw underlying message or transcript text, not the highlighted string after markers are inserted
- if the initial message-scope BM25/text search returns fewer than 3 hits, `autosearch` automatically retries with prefix-expanded terms and sets `_meta.wildcard_fallback = true`
- the fallback rewrite should append trailing `*` per token rather than using leading wildcards
- wildcard auto-fallback does not apply to semantic mode
- wildcard auto-fallback is currently required only for message-scope search, not session-scope search

Expected hit shape:

```json
{
  "_meta": {
    "scope": "messages",
    "elapsed_ms": 12,
    "total_matches": 3,
    "request_id": "reflect-run-42",
    "wildcard_fallback": false
  },
  "hits": [
    {
      "id": "stable-hit-id",
      "sessionId": "abc123",
      "messageId": "abc123-17",
      "messageType": "tool",
      "score": 1.8,
      "snippetStartIndex": 40,
      "snippetEndIndex": 60,
      "snippet": "Exit code 0",
      "previousMessageId": "abc123-16",
      "nextMessageId": "abc123-18"
    }
  ]
}
```

### Session-Scope Search

`autosearch search "auth middleware e2e" --scope sessions` performs transcript-level ranking over full session transcripts.

Requirements:

- searches full session transcripts and returns session-level hits
- executes against the session-level search corpus rather than individual message rows
- supports `--since`, `--after`, `--before`, `--index`, `--cwd`, `--remote`, and `--request-id`
- time filters are based on session start time
- the searchable text source is `sessions.transcript_truncated`
- supports cross-message and cross-turn matching because a full session transcript is one searchable document
- returns sessions ranked by relevance

Expected hit shape:

```json
{
  "_meta": {
    "scope": "sessions",
    "elapsed_ms": 25,
    "total_matches": 5,
    "request_id": "reflect-run-42",
    "wildcard_fallback": false
  },
  "hits": [
    {
      "id": "stable-hit-id",
      "sessionId": "abc123",
      "score": 2.4,
      "workspace": "/home/charlie/src/my-project",
      "firstMessageAt": 1709312400000,
      "lastMessageAt": 1709316000000,
      "totalMessages": 42
    }
  ]
}
```

### Semantic Mode

`autosearch search "Auth tests are failing" --mode semantic` is a later-version goal.

Requirements:

- keep the unified `search` command shape and response family compatible with the journey
- support both `--scope messages` and `--scope sessions`
- return the same `_meta`/`hits` envelope as BM25 mode
- allow additional search-specific fields such as similarity scores
- wildcard auto-fallback does not apply to semantic mode

Expected hit shape:

```json
{
  "_meta": {
    "scope": "messages",
    "elapsed_ms": 18,
    "total_matches": 4,
    "request_id": "",
    "wildcard_fallback": false
  },
  "hits": [
    {
      "id": "stable-hit-id",
      "sessionId": "abc123",
      "messageId": "abc123-17",
      "similarityScore": 0.87
    }
  ]
}
```

### Shared Search Rules

`autosearch fix` runs bundled search rules to help an agent spot suspicious sessions or message patterns.

Requirements:

- accepts the same time and repository filters as the search commands
- may combine multiple internal searches across both scopes under the hood
- returns LLM-friendly output rather than strict JSON
- this command is deferred to v2 and is not required for the smallest implementation slice
- includes:
  - which messages or sessions were flagged
  - which rule flagged them
  - what the agent could consider doing next

This is intentionally similar in spirit to `autodoc fix`: a guidance-oriented command rather than a low-level raw query.

## 6. Session and Message Helpers

These helper commands are part of the user journey and are required even though they are not pure search operations.

### `autosearch session get <session_id>`

This command outputs a readable Markdown transcript rather than JSON.

Requirements:

- reconstructs output from underlying ordered message rows rather than dumping the pre-concatenated session transcript blob directly
- emits a transcript that fits comfortably in an agent context window
- wraps messages in XML-like tags, for example:
  - `<user id="$messageId" ts="...">...</user>`
  - `<agent id="$messageId" ts="...">...</agent>`
- mid-truncates very long single messages
- represents subagent activity explicitly rather than always inlining full subagent logs
- supports the same style of output when the given session is itself a subagent session

### `autosearch session describe <session_id>`

This command returns JSON summary information for a session.

Required fields and behaviors:

- may accept `--request-id`
- returns a JSON object with a top-level `_meta` object and a top-level `session` object
- `_meta` should include at least:
  - `request_id`, echoed from `--request-id` or the empty string
  - `elapsed_ms`
- first and last message time
- byte counts
- counts of messages by category where possible
- high-level session metadata
- `transcriptSummary`, formed from the first part of the first message and the last part of the last message with `...` in the middle

Expected response shape:

```json
{
  "_meta": {
    "request_id": "",
    "elapsed_ms": 12
  },
  "session": {
    "id": "abc123",
    "firstMessageAt": 1709312400000,
    "lastMessageAt": 1709316000000,
    "transcriptSummary": "first ... last"
  }
}
```

### `autosearch message get <message_id>`

This command returns the full message body as text rather than JSON.

### `autosearch message describe <message_id>`

This command returns JSON metadata and a content preview.

Requirements:

- may accept `--request-id`
- returns a JSON object with a top-level `_meta` object and a top-level `message` object
- `_meta` should include at least:
  - `request_id`, echoed from `--request-id` or the empty string
  - `elapsed_ms`
- preview of message content
- denormalized session metadata alongside the message metadata
- enough information for an agent to decide whether it needs `message get`
- enough ordering metadata to link the message back to its session position

Expected response shape:

```json
{
  "_meta": {
    "request_id": "",
    "elapsed_ms": 7
  },
  "message": {
    "id": "abc123-17",
    "sessionId": "abc123",
    "preview": "Exit code 0 ..."
  }
}
```

### `autosearch message similar <message_id>`

This command finds messages similar to the given message.

Requirements:

- this command is deferred to v2 and is not required for the smallest implementation slice
- returns the shared search-style `_meta`/`hits` envelope because it is a search-like result
- should use `_meta.scope = "messages"`
- supports `--request-id`
- each hit should follow the message-scope search hit shape, not the `message describe` object shape
- each hit should include:
  - `sessionId`
  - `messageId`
  - `messageType`
  - `score` or `similarityScore`
  - `snippet`
  - `snippetStartIndex`
  - `snippetEndIndex`
  - `previousMessageId`
  - `nextMessageId`
- is allowed to use BM25, vectors, or another similarity method internally
- exact implementation is still open, but the command itself is part of the current contract

Expected response shape:

```json
{
  "_meta": {
    "scope": "messages",
    "elapsed_ms": 9,
    "total_matches": 4,
    "request_id": "",
    "wildcard_fallback": false
  },
  "hits": [
    {
      "id": "stable-hit-id",
      "sessionId": "abc123",
      "messageId": "abc123-18",
      "messageType": "tool",
      "similarityScore": 0.81,
      "snippetStartIndex": 10,
      "snippetEndIndex": 44,
      "snippet": "Exit code 1 from npm run e2e",
      "previousMessageId": "abc123-17",
      "nextMessageId": "abc123-19"
    }
  ]
}
```

## 7. Reflection Workflow Requirements

The journey expects `autosearch` to provide the building blocks for a reflection loop:

1. narrow down to one project
2. look for recent errors or failures
3. probe and explore wider context
4. decide on a rule or improvement
5. write that rule into project memory later via reflection tooling

One explicit journey example is:

1. search for `npm run e2e` with `--scope sessions`
2. collect matching `sessionId` values from the session hits
3. use those sessions for further ranking or inspection
4. drill into errors with message-scope search
5. combine `autosearch` results with DuckDB queries over `autoetl` data

This means `autosearch` must preserve stable links between:

- query results
- session identifiers
- message identifiers
- the underlying session and message metadata from `autoetl`

## 8. Open Design Questions Preserved from the Journey

The journey is authoritative, but it intentionally leaves some details open. This requirements file should preserve those open questions rather than inventing a conflicting contract.

Open questions still present in the journey:

- how much extra metadata each search mode should expose beyond the base required fields

Those questions should be resolved in later design work without changing the user-facing direction captured here.
