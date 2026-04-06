---
hash: "d4cb1f0a"
id: "19e64c81"
summary: "Implementation spec for autosearch v1: SQLite FTS5 indexing over autoetl parquet output, BM25 search, and session/message helper commands."
title: "autosearch v1 Technical Solution"
---

# autosearch - Technical Solution

This document describes how `autosearch` v1 will be built. Read it alongside [requirements.md](./requirements.md) and the authoritative [user journey](../../docs/user-journey.md).

## 1. V1 Boundary

The first implementation slice is intentionally narrow. It covers the retrieval building blocks we need before adding reflection-specific features.

In scope for v1:

- `autosearch init`
- `autosearch index`
- `autosearch search "query"`
- `autosearch search "query" --scope sessions`
- `autosearch session get <session_id>`
- `autosearch session describe <session_id>`
- `autosearch message get <message_id>`
- `autosearch message describe <message_id>`

Explicitly deferred to v2:

- `--mode semantic`
- `autosearch message similar <message_id>`
- `autosearch fix`

The v1 goal is simple: given `autoetl` parquet output, build a deterministic local SQLite index that can answer message-level and session-level BM25 queries and return helper views that are safe for agent consumption.

## 2. Main Decisions

These decisions are now locked for implementation:

- `autosearch` will use SQLite plus FTS5 for v1.
- Search input comes directly from Parquet, read with `parquet-go`.
- Search does not expose raw SQLite FTS syntax.
- Search sources are:
  - `messages.content_truncated` for `--scope messages`
  - `sessions.transcript_truncated` for `--scope sessions`
- Retrieval helpers use full content where appropriate:
  - `message get` uses `messages.content`
  - `session get` reconstructs from ordered message rows rather than dumping the session transcript blob
- `--cwd` and `--remote` are mutually exclusive in v1.
- Message-scope sparse-result fallback rewrites positive query terms to prefix form such as `Exit* code*`.
- Prefix fallback is used only for message-scope BM25 search when the first pass returns fewer than 3 hits.
- Helper JSON responses use:
  - `_meta` + `session` for `session describe`
  - `_meta` + `message` for `message describe`
- `auto-search/.tmp/etl-output` is the local developer e2e dataset. It is ignored by git and is not the committed fixture source for CI.

## 3. Project Layout

The package layout will stay small and internal-only:

```text
auto-search/
├── cmd/autosearch/
│   └── main.go
├── internal/
│   ├── cli/
│   │   ├── root.go
│   │   ├── init.go
│   │   ├── index.go
│   │   ├── search.go
│   │   ├── session.go
│   │   └── message.go
│   ├── config/
│   │   └── settings.go
│   ├── etlscan/
│   │   ├── discover.go
│   │   ├── parquet_sessions.go
│   │   └── parquet_messages.go
│   ├── indexdb/
│   │   ├── schema.go
│   │   ├── migrate.go
│   │   ├── state.go
│   │   ├── sessions.go
│   │   └── messages.go
│   ├── query/
│   │   ├── lexer.go
│   │   ├── parser.go
│   │   ├── ast.go
│   │   ├── compile_fts.go
│   │   └── fallback.go
│   ├── search/
│   │   ├── messages.go
│   │   ├── sessions.go
│   │   └── snippets.go
│   ├── render/
│   │   ├── json.go
│   │   ├── markdown_session.go
│   │   └── text_message.go
│   └── model/
│       ├── parquet.go
│       ├── session.go
│       └── message.go
├── docs/
│   ├── requirements.md
│   └── solution.md
├── .tmp/
│   └── etl-output/            # local-only developer fixture snapshot
├── CLAUDE.md
├── go.mod
└── go.sum
```

Important constraint: `autosearch` cannot directly import `auto-etl/internal/model` because Go `internal/` packages are only visible inside the owning module tree. V1 will mirror the parquet schema in `auto-search/internal/model` and treat `autoetl` parquet output as the public contract.

## 4. Runtime Dependencies

V1 should keep dependencies conservative:

- `github.com/spf13/cobra` for the CLI
- `modernc.org/sqlite` for SQLite access
- `github.com/parquet-go/parquet-go` for parquet reads

We do not need DuckDB at runtime. DuckDB remains useful for manual source-data inspection, but `autosearch` itself should index directly from Parquet files.

## 5. SQLite Schema

Each named index is a single SQLite file at `~/.auto/search/<name>.sqlite`.

V1 will create these tables:

- `schema_info`
  - one row with the local index schema version
  - used to force full rebuilds when the index layout changes
- `index_state`
  - one row per indexed parquet file
  - tracks dataset, partition key, source path, size, mtime, source schema version, and `indexed_at`
- `sessions`
  - one row per session
  - stores mirrored metadata plus `transcript_truncated`
- `messages`
  - one row per message
  - stores mirrored metadata plus both `content` and `content_truncated`
- `sessions_fts`
  - FTS5 table over session searchable fields
- `messages_fts`
  - FTS5 table over message searchable fields

Recommended shape:

```sql
CREATE TABLE schema_info (
  schema_version INTEGER NOT NULL
);

CREATE TABLE index_state (
  dataset TEXT NOT NULL,
  partition_key TEXT NOT NULL,
  source_path TEXT NOT NULL PRIMARY KEY,
  source_size_bytes INTEGER NOT NULL,
  source_mtime_unix_ms INTEGER NOT NULL,
  source_schema_version INTEGER NOT NULL,
  indexed_at_unix_ms INTEGER NOT NULL
);

CREATE TABLE sessions (
  doc_id INTEGER PRIMARY KEY,
  partition_source_path TEXT NOT NULL,
  session_id TEXT NOT NULL UNIQUE,
  parent_session_id TEXT NOT NULL,
  host_id TEXT NOT NULL,
  agent TEXT NOT NULL,
  subagent_name TEXT NOT NULL,
  is_subagent INTEGER NOT NULL,
  workspace TEXT NOT NULL,
  git_remote TEXT NOT NULL,
  model TEXT NOT NULL,
  source_path TEXT NOT NULL,
  first_message_at INTEGER NOT NULL,
  last_message_at INTEGER NOT NULL,
  total_input_tokens INTEGER NOT NULL,
  total_output_tokens INTEGER NOT NULL,
  total_tokens INTEGER NOT NULL,
  total_bytes INTEGER NOT NULL,
  total_output_bytes INTEGER NOT NULL,
  total_input_bytes INTEGER NOT NULL,
  transcript_truncated TEXT NOT NULL,
  schema_version INTEGER NOT NULL
);

CREATE TABLE messages (
  doc_id INTEGER PRIMARY KEY,
  partition_source_path TEXT NOT NULL,
  message_id TEXT NOT NULL UNIQUE,
  session_id TEXT NOT NULL,
  host_id TEXT NOT NULL,
  message_index INTEGER NOT NULL,
  role TEXT NOT NULL,
  content TEXT NOT NULL,
  content_truncated TEXT NOT NULL,
  timestamp INTEGER NOT NULL,
  tool_name TEXT NOT NULL,
  tool_input TEXT NOT NULL,
  tool_file_path TEXT NOT NULL,
  tool_file_start_line INTEGER NOT NULL,
  tool_file_num_lines INTEGER NOT NULL,
  tool_file_total_lines INTEGER NOT NULL,
  bash_command TEXT NOT NULL,
  input_tokens INTEGER NOT NULL,
  cache_input_tokens INTEGER NOT NULL,
  output_tokens INTEGER NOT NULL,
  workspace TEXT NOT NULL,
  git_remote TEXT NOT NULL,
  git_branch TEXT NOT NULL,
  model TEXT NOT NULL,
  parent_session_id TEXT NOT NULL,
  is_subagent INTEGER NOT NULL,
  source_line_index INTEGER NOT NULL,
  schema_version INTEGER NOT NULL
);

CREATE INDEX idx_sessions_session_id ON sessions(session_id);
CREATE INDEX idx_sessions_workspace ON sessions(workspace);
CREATE INDEX idx_sessions_git_remote ON sessions(git_remote);
CREATE INDEX idx_sessions_first_message_at ON sessions(first_message_at);

CREATE INDEX idx_messages_message_id ON messages(message_id);
CREATE INDEX idx_messages_session_id_message_index ON messages(session_id, message_index);
CREATE INDEX idx_messages_workspace ON messages(workspace);
CREATE INDEX idx_messages_git_remote ON messages(git_remote);
CREATE INDEX idx_messages_timestamp ON messages(timestamp);

CREATE VIRTUAL TABLE sessions_fts USING fts5(
  transcript_truncated,
  workspace,
  git_remote,
  model,
  content='sessions',
  content_rowid='doc_id',
  tokenize='unicode61'
);

CREATE VIRTUAL TABLE messages_fts USING fts5(
  content_truncated,
  workspace,
  git_remote,
  model,
  content='messages',
  content_rowid='doc_id',
  tokenize='unicode61'
);
```

Notes:

- `doc_id` is the SQLite row identity used by FTS joins. Search results still return `sessionId` and `messageId`.
- `partition_source_path` records which parquet file produced the row so incremental reindex can replace only dirty partitions.
- `content_truncated` and `transcript_truncated` stay in the base tables so snippet logic can operate on the exact raw indexed text.
- `role` maps directly to the response `messageType` field. V1 will not add a separate message-type taxonomy beyond `user|assistant|tool|system`.

## 6. Indexing Flow

### 6.1 Discovery

`autosearch index` will:

1. Resolve the input path.
2. Find parquet files under:
   - `messages/**/*.parquet`
   - `sessions/**/*.parquet`
3. Build partition keys from the directory names, for example:
   - `messages/year=2026/week=12`
   - `sessions/year=2026/month=03`
4. Read filesystem metadata for each parquet file.

### 6.2 Incremental Policy

The incremental policy is intentionally simple and deterministic:

- always reindex the newest `messages` partition
- always reindex the newest `sessions` partition
- reindex any older partition when source path, file size, mtime, or parquet schema version changed
- full rebuild when the local SQLite schema version changes

This avoids clever heuristics while matching the `autoetl` rule that older partitions are usually immutable.

### 6.3 Rebuild Strategy

Two paths are needed:

- full rebuild:
  - create a fresh temp database beside the destination
  - build all tables and FTS contents
  - atomically rename into place
- incremental update:
  - open the existing database
  - `BEGIN IMMEDIATE`
  - remove rows contributed by each dirty parquet source
  - reinsert from the current parquet file
  - update `index_state`
  - commit

Each indexed row should record its `partition_source_path` in the base table so a dirty parquet file can be cleanly reloaded without rebuilding unrelated partitions.

### 6.4 Parquet Read Model

We will define local Go structs that mirror the parquet schema:

- `ParquetSessionRow`
- `ParquetMessageRow`

These structs should match the column names used by `autoetl` today. Missing nullable string-like values should normalize to empty strings in SQLite so downstream search and render paths stay simple.

### 6.5 Session-Derived Fields

The session parquet already contains transcript and token totals, so `sessions` is mostly a direct mirror. One field should be added during indexing:

- `total_messages`

This can either be stored in `sessions` or derived cheaply with `COUNT(*)` from `messages`. V1 can derive it at query time for correctness and simpler indexing.

## 7. Query Language

`autosearch search` uses an app-level query language, not raw SQLite syntax.

V1 grammar:

- bare terms
- quoted phrases
- uppercase `AND`
- uppercase `OR`
- uppercase `NOT`

V1 parser rules:

- adjacent terms are treated as `AND`
- precedence is `NOT` > `AND` > `OR`
- parentheses are not supported in v1

Examples:

- `Exit code 0` -> `Exit AND code AND 0`
- `"auth middleware" retry` -> `"auth middleware" AND retry`
- `flaky AND retry NOT passed`

Implementation plan:

1. Lexer produces term, phrase, and operator tokens.
2. Parser builds a small AST.
3. Compiler escapes user text and emits SQLite FTS5 query syntax.
4. Prefix fallback rewrites only positive term and phrase leaves, never negated leaves.

Keeping this layer explicit is important because it isolates SQLite quirks from the public CLI contract.

## 8. Search Execution

### 8.1 Shared Flow

Both scopes follow the same high-level flow:

1. Validate flags and normalize filters.
2. Parse the app-level query.
3. Compile to an FTS expression.
4. Run the scope-specific SQL query.
5. If the scope is `messages`, the mode is BM25, and the first pass returns fewer than 3 hits, retry with prefix-expanded leaves.
6. Build `_meta`.
7. Render JSON.

`--request-id` is echoed in `_meta.request_id`. If not provided, the field is an empty string.

### 8.2 Filter Semantics

V1 will fail fast when both `--cwd` and `--remote` are supplied.

Filter behavior:

- `--cwd`
  - defaults to the current working directory for commands that support repo filtering
  - filters on `workspace`
- `--remote`
  - filters on `git_remote`
  - intended for cross-host matching when local workspace paths differ
- message-scope time filters use `messages.timestamp`
- session-scope time filters use `sessions.first_message_at`

### 8.3 Message-Scope SQL

Message-scope search should join `messages_fts` to `messages`, compute BM25, and fetch neighboring message ids in one query or one follow-up lookup keyed by `(session_id, message_index)`.

Required fields per hit:

- `id`
- `sessionId`
- `messageId`
- `messageType`
- `score`
- `snippetStartIndex`
- `snippetEndIndex`
- `snippet`
- `previousMessageId`
- `nextMessageId`

Stable `id` generation will use a short hash over:

- scope
- mode
- normalized query
- normalized filters
- matched `message_id`

`request_id` and timing must not be part of the stable hit id.

### 8.4 Session-Scope SQL

Session-scope search joins `sessions_fts` to `sessions`, computes BM25, and returns:

- `id`
- `sessionId`
- `score`
- `workspace`
- `firstMessageAt`
- `lastMessageAt`
- `totalMessages`

Stable ids use the same hashing approach, replacing `message_id` with `session_id`.

### 8.5 Snippets and Highlighting

SQLite's built-in `snippet()` helper is not sufficient because we need raw-text offsets that remain correct even after highlight markers are injected.

V1 snippet rendering will happen in Go:

1. Use the parsed query to collect positive search leaves.
2. Find the earliest matching span in the raw indexed text with case-insensitive matching.
3. Expand to a fixed window around that span.
4. Return `snippetStartIndex` and `snippetEndIndex` against the raw indexed text.
5. If `--highlight` is set, inject `**` markers only into the returned snippet string.

This guarantees that offsets refer to the underlying `content_truncated` or `transcript_truncated` text, not the decorated snippet.

For v1:

- use the first best match rather than trying to highlight every match
- prefer phrase matches over single-term matches when both are present

## 9. Helper Commands

### 9.1 `session get`

`session get` is a renderer, not a search query.

Implementation plan:

- load all messages for the session ordered by `message_index`
- render XML-like wrappers by role:
  - `<user ...>`
  - `<agent ...>`
  - `<tool ...>`
  - `<system ...>`
- mid-truncate very long messages
- if the session is a subagent session, render it the same way when fetched directly
- for parent sessions, keep subagent references explicit rather than auto-inlining the entire child transcript

The first version should optimize for readability and bounded size, not perfect fidelity to every raw tool payload.

### 9.2 `session describe`

`session describe` loads the session row plus aggregated message counts and returns:

- `_meta.request_id`
- `_meta.elapsed_ms`
- `session.id`
- `session.firstMessageAt`
- `session.lastMessageAt`
- `session.totalTokens`
- `session.totalBytes`
- `session.workspace`
- `session.gitRemote`
- `session.model`
- `session.totalMessages`
- `session.toolMessages`
- `session.bashMessages`
- `session.readFileMessages`
- `session.writeFileMessages`
- `session.transcriptSummary`

`transcriptSummary` will be built from:

- the first N characters of the first message content
- the last N characters of the last message content
- `...` between them

### 9.3 `message get`

`message get` returns the full raw message content from `messages.content`.

This command is intentionally simple and should not apply search-time truncation rules.

### 9.4 `message describe`

`message describe` loads one message row plus minimal session metadata and returns:

- `_meta.request_id`
- `_meta.elapsed_ms`
- `message.id`
- `message.sessionId`
- `message.messageIndex`
- `message.messageType`
- `message.timestamp`
- `message.workspace`
- `message.gitRemote`
- `message.model`
- `message.toolName`
- `message.toolFilePath`
- `message.bashCommand`
- `message.preview`
- `message.previousMessageId`
- `message.nextMessageId`
- `message.sessionFirstMessageAt`
- `message.sessionLastMessageAt`

`preview` should come from the first N characters of `content_truncated`.

## 10. Verification and Test Coverage

The test plan needs three layers: pure unit tests, repository-local integration tests, and developer e2e smoke runs against a stable local parquet snapshot.

### 10.1 Unit Tests

Add focused unit tests for:

- query lexer tokenization
- query parser precedence and implicit `AND`
- query compiler escaping
- prefix fallback rewrite rules
- date filter parsing and normalization
- `--cwd` xor `--remote` validation
- hit id generation stability
- snippet window calculation
- highlight insertion without offset drift
- transcript/message mid-truncation helpers

These tests should be table-driven and run without touching disk wherever possible.

### 10.2 Integration Tests

Add integration tests that exercise real SQLite behavior with temporary databases:

- schema creation and migration
- full rebuild into a fresh DB
- incremental reindex of the newest partitions
- dirty partition replacement when source metadata changes
- message-scope BM25 search
- session-scope BM25 search
- wildcard prefix fallback when first-pass hits are fewer than 3
- `session get` reconstruction order
- `session describe` aggregation counts
- `message describe` neighbor lookup

These tests should use committed fixtures under `auto-search/testdata/` rather than the large local `.tmp` snapshot.

### 10.3 Fixture Strategy

We need two fixture tiers:

- `auto-search/testdata/etl-output`
  - small committed parquet fixtures for CI
  - curated to cover:
    - multiple sessions
    - multiple message roles
    - a few file/tool messages
    - at least one repeated term for search ranking
    - at least one subagent session
  - should also include canonical known cases for:
    - a message-scope exact hit query
    - a session-scope transcript hit query
    - a sparse query that triggers prefix fallback
    - one long message that exercises mid-truncation
    - one session/message pair used by `describe` and `get` goldens
  - should be paired with golden JSON/markdown outputs for the main commands so acceptance checks do not depend on ad hoc assertions alone
- `auto-search/.tmp/etl-output`
  - local developer snapshot copied from `~/.auto/etl/output`
  - used for stable manual/e2e runs
  - ignored by git

The local snapshot is useful because it exercises realistic data volume and text shape without forcing a 74 MB fixture into the repository.

Recommended local copy command:

```bash
mkdir -p auto-search/.tmp/etl-output
rsync -a --delete ~/.auto/etl/output/ auto-search/.tmp/etl-output/
```

### 10.4 E2E Smoke Coverage

Add a lightweight e2e script or documented make target that runs against `auto-search/.tmp/etl-output` when it exists.

Suggested flow:

```bash
TMP_HOME="$(mktemp -d)"
cd auto-search
HOME="$TMP_HOME" go run ./cmd/autosearch init
HOME="$TMP_HOME" go run ./cmd/autosearch index --input ./.tmp/etl-output
HOME="$TMP_HOME" go run ./cmd/autosearch search "Exit code 0"
HOME="$TMP_HOME" go run ./cmd/autosearch search "auth middleware" --scope sessions
HOME="$TMP_HOME" go run ./cmd/autosearch session describe <known-session-id>
HOME="$TMP_HOME" go run ./cmd/autosearch message describe <known-message-id>
```

This should stay out of mandatory CI until the interface is stable and the local fixture story is settled.

### 10.5 CI Verification

At minimum, `auto-search` CI should run:

```bash
cd auto-search
go test ./...
```

Once the committed parquet fixtures exist, `go test ./...` should cover schema creation, indexing, and the main search and helper behaviors without relying on `~/.auto/etl/output`.

### 10.6 Manual Verification

Before considering v1 done, manually verify:

- `autosearch index` on an empty input path fails cleanly
- `autosearch index` is idempotent on unchanged input
- message-scope search returns raw offsets that still make sense when `--highlight` is enabled
- session-scope search can find cross-message patterns that would never fit in one message
- `session get` output stays readable on long tool-output sessions
- `--cwd` and `--remote` conflict errors are immediate and clear

## 11. Acceptance Criteria

Each v1 deliverable is only done when its acceptance criteria pass and the listed verification steps succeed.

### 11.1 `autosearch init`

Acceptance criteria:

- creates `~/.auto/settings.json` when it is missing
- creates `~/.auto/search/settings.json`
- is safe to run repeatedly without changing a valid existing config unexpectedly

Verification steps:

1. `cd auto-search`
2. `TMP_HOME="$(mktemp -d)"`
3. `HOME="$TMP_HOME" go run ./cmd/autosearch init`
4. `test -f "$TMP_HOME/.auto/settings.json"`
5. `test -f "$TMP_HOME/.auto/search/settings.json"`
6. Run `HOME="$TMP_HOME" go run ./cmd/autosearch init` again and confirm exit code `0`
7. Add an integration test that creates a temp HOME, runs init twice, and verifies both files exist after each run

### 11.2 `autosearch index` Full Build

Acceptance criteria:

- creates the named SQLite index file at `~/.auto/search/<name>.sqlite`
- creates all required base tables and FTS tables
- indexes all committed fixture parquet rows exactly once
- makes the database immediately usable by `search`, `session`, and `message` commands

Verification steps:

1. `cd auto-search`
2. `TMP_HOME="$(mktemp -d)"`
3. `HOME="$TMP_HOME" go run ./cmd/autosearch init`
4. `HOME="$TMP_HOME" go run ./cmd/autosearch index --input ./testdata/etl-output`
5. `test -f "$TMP_HOME/.auto/search/default.sqlite"`
6. Inspect the DB and confirm the presence of `schema_info`, `index_state`, `sessions`, `messages`, `sessions_fts`, and `messages_fts`
7. Verify row counts match the known committed fixture counts
8. Add an integration test that builds a fresh DB from fixture parquet and checks schema plus row counts

### 11.3 `autosearch index` Incremental Update

Acceptance criteria:

- rerunning `index` on unchanged input does not duplicate data
- the newest partitions are reprocessed on every run
- older partitions are only reprocessed when metadata or schema version changes
- dirty partition replacement updates only the affected rows and `index_state`

Verification steps:

1. `cd auto-search`
2. `TMP_HOME="$(mktemp -d)"`
3. `HOME="$TMP_HOME" go run ./cmd/autosearch init`
4. `HOME="$TMP_HOME" go run ./cmd/autosearch index --input ./testdata/etl-output`
5. Record row counts for `sessions`, `messages`, and `index_state`
6. Run `HOME="$TMP_HOME" go run ./cmd/autosearch index --input ./testdata/etl-output` again and verify row counts are unchanged
7. In an integration test, copy fixture parquet into a temp directory, modify one source file's mtime or content, rerun `index`, and verify only the affected partition rows change
8. In an integration test, bump the local schema version and verify the next run performs a full rebuild path

### 11.4 `autosearch search` Message Scope

Acceptance criteria:

- `search "query"` defaults to `--scope messages`
- returns strict JSON with top-level `_meta` and `hits`
- hit rows include `sessionId`, `messageId`, `messageType`, `score`, `snippet`, `snippetStartIndex`, `snippetEndIndex`, `previousMessageId`, and `nextMessageId`
- `--request-id` is echoed in `_meta.request_id`
- `--highlight` changes only the snippet text, not the raw offsets
- sparse exact-term searches retry with prefix-expanded terms and set `_meta.wildcard_fallback = true`
- supports date filters on `messages.timestamp`:
  - `--since <duration>` as `timestamp >= now-duration`
  - `--after <time>` and `--before <time>` as inclusive/exclusive bounds
- rejects invalid date-filter input (`--since` mixed with `--after/--before`, invalid formats, invalid ranges) with a non-zero exit and remediation guidance

Verification steps:

1. `cd auto-search`
2. `TMP_HOME="$(mktemp -d)"`
3. `HOME="$TMP_HOME" go run ./cmd/autosearch init`
4. `HOME="$TMP_HOME" go run ./cmd/autosearch index --input ./testdata/etl-output`
5. Run `HOME="$TMP_HOME" go run ./cmd/autosearch search "<fixture message query>"`
6. Validate with `jq` that `._meta.scope == "messages"` and `.hits | length > 0`
7. Validate the first hit contains all required fields
8. Run the same query with `--request-id acceptance-1` and verify `._meta.request_id == "acceptance-1"`
9. Run a highlight query against a known fixture hit and verify the snippet includes `**` while `snippetStartIndex` and `snippetEndIndex` still refer to the raw indexed text
10. Run a sparse query designed to trigger fallback and verify `._meta.wildcard_fallback == true`
11. Run a canonical date-window query, for example `autosearch search "Exit code" --after 2024-03-21T08:00:00Z --before 2024-03-21T09:00:00Z`, and verify only in-window message hits are returned
12. Run `autosearch search "Exit code" --before 2024-03-21T08:35:00Z` and verify the boundary at exactly `2024-03-21T08:35:00Z` is excluded
13. Run an invalid input check, for example `autosearch search "Exit code" --since 7d --after 2024-03-21`, and verify a non-zero exit with a clear conflict message
14. Add integration tests that compare the canonical message-scope query output to a committed golden JSON file

### 11.5 `autosearch search --scope sessions`

Acceptance criteria:

- searches `sessions.transcript_truncated`
- returns strict JSON with top-level `_meta` and `hits`
- hit rows include `sessionId`, `score`, `workspace`, `firstMessageAt`, `lastMessageAt`, and `totalMessages`
- supports the shared date and repo filters
- can match a transcript-level pattern that spans multiple messages in a session
- applies date filters to `sessions.first_message_at` with `--after` inclusive and `--before` exclusive semantics

Verification steps:

1. `cd auto-search`
2. `TMP_HOME="$(mktemp -d)"`
3. `HOME="$TMP_HOME" go run ./cmd/autosearch init`
4. `HOME="$TMP_HOME" go run ./cmd/autosearch index --input ./testdata/etl-output`
5. Run `HOME="$TMP_HOME" go run ./cmd/autosearch search "<fixture session query>" --scope sessions`
6. Validate with `jq` that `._meta.scope == "sessions"` and `.hits | length > 0`
7. Validate the first hit contains all required session-scope fields
8. Run a canonical cross-message query from the committed fixture set and verify it returns a known matching session
9. Run a canonical session date-window query, for example `autosearch search "User" --scope sessions --after 2024-03-21T06:00:00Z --before 2024-03-21T07:00:00Z`, and verify only sessions starting within the window are returned
10. Add integration tests that compare the canonical session-scope query output to a committed golden JSON file

### 11.6 `autosearch session get`

Acceptance criteria:

- reconstructs the transcript from ordered message rows
- emits XML-like wrappers by role
- mid-truncates oversized single messages
- keeps subagent references explicit rather than blindly inlining child transcripts in parent output

Verification steps:

1. `cd auto-search`
2. `TMP_HOME="$(mktemp -d)"`
3. `HOME="$TMP_HOME" go run ./cmd/autosearch init`
4. `HOME="$TMP_HOME" go run ./cmd/autosearch index --input ./testdata/etl-output`
5. Run `HOME="$TMP_HOME" go run ./cmd/autosearch session get <fixture session id>`
6. Verify the output contains ordered role wrappers such as `<user ...>` and `<agent ...>`
7. Verify a canonical long-message fixture is mid-truncated
8. Verify the canonical parent/subagent fixture renders explicit subagent markers rather than an uncontrolled full inline dump
9. Add an integration test that compares `session get` output for a canonical fixture session to a committed golden markdown file

### 11.7 `autosearch session describe`

Acceptance criteria:

- returns strict JSON with top-level `_meta` and `session`
- includes request metadata plus the agreed summary fields
- computes message-category counts from indexed message rows
- returns a bounded `transcriptSummary`

Verification steps:

1. `cd auto-search`
2. `TMP_HOME="$(mktemp -d)"`
3. `HOME="$TMP_HOME" go run ./cmd/autosearch init`
4. `HOME="$TMP_HOME" go run ./cmd/autosearch index --input ./testdata/etl-output`
5. Run `HOME="$TMP_HOME" go run ./cmd/autosearch session describe <fixture session id> --request-id acceptance-session`
6. Validate with `jq` that `._meta.request_id == "acceptance-session"`
7. Validate the `session` object contains `id`, `firstMessageAt`, `lastMessageAt`, `totalTokens`, `totalBytes`, `workspace`, `gitRemote`, `model`, `totalMessages`, and `transcriptSummary`
8. Validate the message-category counts match the known committed fixture values
9. Add an integration test that compares `session describe` output for a canonical fixture session to a committed golden JSON file

### 11.8 `autosearch message get`

Acceptance criteria:

- returns the full raw message content from `messages.content`
- does not substitute the truncated search text
- works for long tool/file-content messages

Verification steps:

1. `cd auto-search`
2. `TMP_HOME="$(mktemp -d)"`
3. `HOME="$TMP_HOME" go run ./cmd/autosearch init`
4. `HOME="$TMP_HOME" go run ./cmd/autosearch index --input ./testdata/etl-output`
5. Run `HOME="$TMP_HOME" go run ./cmd/autosearch message get <fixture message id>`
6. Verify the output matches the full canonical fixture content rather than the truncated preview form
7. Add an integration test that compares `message get` output for a canonical fixture message to a committed golden text file

### 11.9 `autosearch message describe`

Acceptance criteria:

- returns strict JSON with top-level `_meta` and `message`
- includes the agreed denormalized session metadata
- includes `previousMessageId` and `nextMessageId`
- returns preview text from `content_truncated`

Verification steps:

1. `cd auto-search`
2. `TMP_HOME="$(mktemp -d)"`
3. `HOME="$TMP_HOME" go run ./cmd/autosearch init`
4. `HOME="$TMP_HOME" go run ./cmd/autosearch index --input ./testdata/etl-output`
5. Run `HOME="$TMP_HOME" go run ./cmd/autosearch message describe <fixture message id> --request-id acceptance-message`
6. Validate with `jq` that `._meta.request_id == "acceptance-message"`
7. Validate the `message` object contains `id`, `sessionId`, `messageIndex`, `messageType`, `timestamp`, `workspace`, `gitRemote`, `model`, `preview`, `previousMessageId`, and `nextMessageId`
8. Verify the preview matches the truncated fixture form rather than the full raw message content
9. Add an integration test that compares `message describe` output for a canonical fixture message to a committed golden JSON file

### 11.10 V1 Test Gate

Acceptance criteria:

- unit tests cover parser, filter validation, hit id generation, snippet logic, and truncation helpers
- integration tests cover schema, indexing, search, and helper commands against committed fixtures
- `go test ./...` passes from the `auto-search` module

Verification steps:

1. `cd auto-search`
2. Run `go test ./...`
3. Confirm the suite includes both pure unit tests and fixture-backed integration tests
4. When `./.tmp/etl-output` exists, run the documented developer smoke flow and confirm the core commands succeed on realistic data

## 12. Implementation Order

The work should land in this order:

1. Create module skeleton, root command, and settings loader.
2. Add SQLite schema creation and migration.
3. Add parquet discovery and read-model structs.
4. Implement full rebuild indexing.
5. Implement incremental reindex using `index_state`.
6. Implement query lexer, parser, and FTS compiler.
7. Implement message-scope BM25 search and snippet rendering.
8. Implement session-scope BM25 search.
9. Implement `message get` and `message describe`.
10. Implement `session get` and `session describe`.
11. Add committed CI fixtures under `auto-search/testdata/`.
12. Add developer e2e smoke script using `auto-search/.tmp/etl-output`.

This order gets real search working early, then builds the inspection helpers on top of the same indexed data model.

## 13. Out of Scope for This Document

This solution deliberately does not define:

- semantic vector indexing
- message-to-message similarity ranking
- rule bundles for `fix`
- pagination or advanced sort controls
- cross-tool APIs beyond reading `autoetl` parquet output

Those can be added once the v1 SQLite and parquet pipeline is stable.
