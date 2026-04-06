---
hash: "b2750ddd"
id: "3e03dfac"
summary: "V1 functional and non-functional requirements for normalizing raw Claude session history into partitioned parquet datasets."
title: "autoetl — Requirements"
---

# autoetl — Requirements

`docs/user-journey.md` is the source of truth for autoetl v1.

The v1 goal is narrow: produce a normalized local dataset that matches the schema shown in the user journey, is complete enough for DuckDB queries, and unlocks `autosearch`, `autoreflect`, and session/message inspection workflows built on top of it.

## v1 Scope

**In scope:**
- Ingest raw Claude session history from `~/.claude/projects`
- Normalize into exactly two parquet datasets: `messages` and `sessions`
- Match the field contract shown in `docs/user-journey.md`
- Preserve raw transcript content while also materializing dereferenced tool output needed for search and transcript rendering
- Populate enough provenance and tool metadata to support filtering, transcript reconstruction, and analytical queries
- Support full rebuilds safely when the schema/output layout changes

**Deferred:**
- S3 or remote storage backends
- Multi-host sync protocols
- Codex and other source formats
- Semantic embeddings
- Git-history reconstruction
- Higher-order derived metrics such as `lines_changed / tokens_used`

## v1 Success Criteria

- `duckdb DESCRIBE` over `messages/**/*.parquet` and `sessions/**/*.parquet` matches the shape documented in `docs/user-journey.md`
- `sessions.transcript_full` and `sessions.transcript_truncated` are populated and usable as search/index inputs
- File/tool metadata is queryable from `messages` without joining a third table
- Session/message ordering is stable and traceable back to the original JSONL
- A full rebuild produces a clean output tree with no stale `blobs/` dataset or mixed-schema partitions

## Functional Requirements

### R1: Canonical Output Shape

- Autoetl must produce exactly two output datasets: `messages` and `sessions`.
- The `blobs` dataset is not part of v1 output.
- The output schema must include all fields shown in the `DESCRIBE` examples in `docs/user-journey.md`.
- `schema_version` remains `1` for this v1 contract.

### R2: Session Schema

The `sessions` dataset must include, at minimum:
- `id`
- `parent_session_id`
- `host_id`
- `agent`
- `subagent_name`
- `is_subagent`
- `workspace`
- `git_remote`
- `model`
- `source_path`
- `first_message_at`
- `last_message_at`
- `total_input_tokens`
- `total_output_tokens`
- `total_tokens`
- `total_bytes`
- `total_output_bytes`
- `total_input_bytes`
- `transcript_full`
- `transcript_truncated`
- `year`
- `month`
- `schema_version`

### R3: Message Schema

The `messages` dataset must include, at minimum:
- `id`
- `session_id`
- `host_id`
- `index`
- `role`
- `content`
- `content_truncated`
- `timestamp`
- `tool_name`
- `tool_input`
- `tool_file_path`
- `tool_file_start_line`
- `tool_file_num_lines`
- `tool_file_total_lines`
- `bash_command`
- `input_tokens`
- `cache_input_tokens`
- `output_tokens`
- `workspace`
- `git_remote`
- `git_branch`
- `model`
- `parent_session_id`
- `is_subagent`
- `source_line_index`
- `year`
- `week`
- `month`
- `schema_version`

### R4: Raw Claude Parsing Coverage

Autoetl v1 must parse enough of the raw Claude JSONL shape to populate the schema truthfully.

This includes:
- Standard user / assistant / system message lines
- `gitBranch`
- `isMeta`
- top-level `toolUseResult`
- top-level system `content`
- nested `progress.data.message` payloads when they contain assistant or tool activity that belongs in the transcript/session record
- subagent metadata needed to populate `parent_session_id`, `is_subagent`, and `subagent_name`

Unknown or currently-unused raw fields should be ignored without failing the run.

### R5: Tool Result Normalization

Tool-related content must be normalized from the real raw transcript shapes that appear in Claude logs.

V1 must handle:
- plain string `tool_result.content`
- array-shaped `tool_result.content`
- oversized tool output placeholders such as `<persisted-output>`
- top-level `toolUseResult` payloads
- async agent-launch payloads with task/output-file references

The pipeline must preserve the raw transcript value in `messages.content`.

When a fuller tool payload exists outside the raw message content, autoetl must dereference it into separate normalized fields used by transcript rendering and downstream consumers.

### R6: Transcript Materialization

Autoetl must build session-level transcripts during transform.

- `transcript_full` must contain a search-friendly concatenation of all session messages using full resolved content
- `transcript_truncated` must contain the same transcript, but with long message/tool payloads mid-truncated
- Transcript rendering must include tool semantics, not only bare message text
- Tool rendering must include relevant metadata such as tool name, bash command, and file path where applicable

The transcript is a first-class output of ETL because downstream indexing is session-based.

### R7: Content and Truncation Rules

- `messages.content` stores the full raw content for the normalized message row
- `messages.content_truncated` stores a mid-truncated form suitable for search previews and transcript rendering
- `sessions.transcript_truncated` must be built from truncated message renderings
- Truncation must preserve the start and end of large payloads and replace the middle with a marker indicating truncation

### R8: Provenance and Project Identity

- `host_id` is the hostname of the machine where `autoetl run` is executed
- `git_remote` must be populated on sessions and denormalized to messages
- `git_branch` must be captured per message from the raw transcript
- `source_path` and `source_line_index` must allow a normalized row to be traced back to the original raw input

### R9: Tool/File Metadata

The messages table must preserve enough tool metadata to unlock downstream analytics and reflection.

At minimum:
- `tool_name`
- `tool_input`
- `bash_command`
- `tool_file_path`
- `tool_file_start_line`
- `tool_file_num_lines`
- `tool_file_total_lines`

For v1, requested line coordinates are acceptable for file reads, writes, and updates when actual observed coordinates are not reliably available.

### R10: Ordering and Identity

- Session IDs must be stable
- Subagent sessions use the subagent/session identity rules described in the user journey and current corpus expectations
- Message IDs must be stable within a full rebuild
- Message ordering must be deterministic and preserve the original JSONL ordering within a session

### R11: Rebuild Safety

- Autoetl must support a full rebuild mode that removes old output before writing new data
- When the on-disk output shape is incompatible with the v1 contract, the tool must not silently leave stale partitions in place
- A v1 rebuild must leave only `messages/` and `sessions/` under the output root

## Non-Functional Requirements

### N1: Fully Agent-Automated Verification

- V1 implementation work must be verifiable end-to-end by the coding agent without requiring manual human inspection to establish correctness.
- The agent must automatically run the relevant verification steps before considering work complete.
- "Looks right" based only on code review is insufficient for completion.

### N2: Unit Test Coverage

- V1 must include unit tests for the core normalization pipeline.
- Unit tests must cover parser behavior, transform behavior, truncation behavior, and schema/output-specific edge cases.
- Unit tests must include representative raw transcript fixtures for the Claude JSONL shapes required by v1.

### N3: End-to-End Test Coverage

- V1 must include end-to-end tests that run the real ETL pipeline against fixture input data and validate the resulting parquet output.
- End-to-end coverage must verify:
  - the two-dataset output layout
  - schema shape for `messages` and `sessions`
  - transcript population
  - provenance/tool metadata population
  - rebuild behavior for full runs

### N4: Runtime Probing Via Claude Code

- Verification must include real probing of the produced output using the same workflows an agent will rely on in practice.
- At minimum, this includes running `autoetl run` against fixture or sample data and probing the resulting parquet with tools such as `duckdb`.
- For v1, probing via Claude Code against the bundled sample corpus is part of the expected verification loop, not an optional manual check.

### N5: Automatic Verification For Every Material Change

- Any material change to parsing, transformation, schema population, truncation, transcript rendering, or output writing must be followed by automatic verification.
- The agent must verify all completed work before closing the task.
- If a required verification step cannot be run, that is a delivery gap and must be reported explicitly.

## Downstream Requirements Enabled By v1

V1 must be sufficient to unlock the workflows described in `docs/user-journey.md`:
- DuckDB inspection of normalized `messages` and `sessions`
- Session-level BM25/keyword indexing over `transcript_full` / `transcript_truncated`
- Session transcript rendering for `autosearch session get`
- Message/session describe and fetch workflows
- Querying hottest files, repeated edits, bash usage, errors, and subagent-linked activity

## Explicit Non-Goals For v1

- Perfect reconstruction of historical file contents at prior git revisions
- Perfect inference of edit-affected line ranges
- Embeddings or semantic search indexes
- Remote sync and encryption pipelines
- Support for every future agent transcript format
