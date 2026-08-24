---
hash: "ee7eae16"
id: "2f612a7e"
read_when: "evaluating ClickHouse as a storage/query backend for auto-etl, or whether Langfuse instrumentation can replace the ETL pipeline"
summary: "Spike comparing the local Langfuse ClickHouse/MinIO data model with auto-etl's Session/Message pipeline; verdict conditional — strong long-term backend but cannot replace auto-etl's ingestion/parsing semantics today."
title: "Tech Spike: Langfuse ClickHouse vs auto-etl"
---

# Tech Spike Report: Langfuse ClickHouse vs auto-etl

**Date:** 2026-07-11
**Scope:** Compare the local Langfuse ClickHouse/MinIO data model with `auto-etl`'s Session/Message pipeline.
**Verdict:** CONDITIONAL: ClickHouse is a strong long-term storage/query backend for auto-etl-shaped data, but the current Langfuse ClickHouse schema and instrumentation cannot replace auto-etl's ingestion/parsing semantics.

## Context

The question was whether the local ClickHouse instance used by Langfuse could eventually replace `auto-etl`, whether it already has all information that `auto-etl` pulls from Claude session files, and what gaps or advantages exist.

## Assumptions Tested

| # | Assumption | Result | Confidence |
|---|---|---|---|
| 1 | Langfuse ClickHouse contains enough normalized session data to replace `auto-etl` sessions/messages. | PARTIAL | High |
| 2 | Langfuse ClickHouse/MinIO contains the full raw information that `auto-etl` parses from Claude JSONL session files. | INVALIDATED | High |
| 3 | ClickHouse could be the long-term warehouse for `auto-etl` if we keep/port the parser and schema. | VALIDATED | Medium |
| 4 | Langfuse adds advantages that `auto-etl` does not currently provide. | VALIDATED | High |

## Detailed Findings

### 1. Local Langfuse ClickHouse Schema

The local ClickHouse instance is reachable through:

```bash
docker exec langfuse-clickhouse-1 clickhouse-client
```

Version observed: `26.6.1.1193`.

Core Langfuse tables in `default`:

| Table | Engine | Current logical rows observed |
|---|---:|---:|
| `traces` | `ReplacingMergeTree` | 14 |
| `observations` | `ReplacingMergeTree` | 201+ active during inspection |
| `blob_storage_file_log` | `ReplacingMergeTree` | 14 |
| `scores` | `ReplacingMergeTree` | 0 |
| `dataset_run_items` | `ReplacingMergeTree` | 0 |
| `schema_migrations` | `MergeTree` | 72 |

The exact counts moved during inspection because new Langfuse events were being written.

Important columns:

- `traces`: `id`, `timestamp`, `name`, `metadata`, `tags`, `input`, `output`, `session_id`, `project_id`, `environment`, `event_ts`, `is_deleted`.
- `observations`: `id`, `trace_id`, `parent_observation_id`, `type`, `start_time`, `end_time`, `name`, `metadata`, `input`, `output`, `provided_model_name`, usage/cost maps, `tool_definitions`, `tool_calls`, `tool_call_names`.
- `blob_storage_file_log`: `project_id`, `entity_type`, `entity_id`, `event_id`, `bucket_name`, `bucket_path`.

Langfuse migrations confirm that `traces` and `observations` are `ReplacingMergeTree(event_ts, is_deleted)` tables partitioned by trace/observation time.

### 2. What Langfuse Captures Here

For the two traced Claude sessions:

| Session | Langfuse traces | Langfuse observations | Generations | Tools | Spans |
|---|---:|---:|---:|---:|---:|
| `ceb22019-...` | 11 | 169 | 65 | 91 | 13 |
| `3742a172-...` | 3 | 32 | 13 | 16 | 3 |

Langfuse observations captured useful tool names and counts:

- `Bash`, `Read`, `Edit`, `Agent`, `AskUserQuestion`, `Skill`, `Monitor`, `Write`, `ToolSearch`.
- Generation rows have `tool_calls` / `tool_call_names` for filtering.
- Tool rows have JSON `input`, `output`, and metadata such as `tool_id`, `tool_name`, and `output_meta`.
- Spans have start/end timestamps, so tool duration can be computed from `end_time - start_time`.

Langfuse/MinIO event blobs also exist:

- 20 event blobs in the `langfuse/events` bucket.
- 14 standard trace event blobs, one per trace turn.
- 6 OTel blobs containing span payloads with `langfuse.observation.*` attributes.
- Total observed blob size: about 1.5 MiB.

### 3. What auto-etl Captures

`auto-etl` currently produces multiple datasets, not just sessions:

| Dataset | Current local rows |
|---|---:|
| `messages` | 180,263 |
| `sessions` | 2,214 |
| `hooks` | 52,486 |
| `git_repositories` | 2 |
| `git_refs` | 1,203 |
| `commits` | 870 |
| `commit_files` | 8,098 |
| `commit_hunks` | 11,925 |
| `pull_requests` | 85 |
| `pull_request_comments` | 443 |

The canonical message schema includes fields Langfuse does not currently provide as typed columns:

- `source_line_index`
- `tool_use_id`
- `duration_ms`
- `interrupted`
- `is_error`
- `bash_exit_code`
- `tool_use_result_json`
- `thinking_signature`
- `skill_name`
- `permission_mode`
- `version`
- `parent_session_id`
- `is_subagent`
- `subagent_name`
- `transcript_full`
- `first_user_intent`

`auto-etl` also stores full unmodified `content` and separately stores `content_truncated`.

### 4. Hard Gaps

#### Raw Claude JSONL Is Not Fully In ClickHouse

The raw Claude session files include many line types:

- `assistant`
- `user`
- `system`
- `attachment`
- `file-history-snapshot`
- `permission-mode`
- `mode`
- `queue-operation`
- `last-prompt`
- `ai-title`
- `custom-title`
- `agent-name`

`auto-etl` parses the JSONL grammar directly. The current Langfuse ClickHouse tables only contain the Langfuse-observability projection: traces, observations, and blob pointers to Langfuse ingestion events.

#### Content Block Semantics Are Different

`auto-etl` splits Claude content arrays into one Message row per block:

- `text`
- `thinking`
- `redacted_thinking`
- `tool_use`
- `tool_result`

Langfuse stores a trace tree:

- `GENERATION`
- `TOOL`
- `SPAN`

That is excellent for observability, but it is not the same lossless representation.

#### Some Langfuse Tool Output Is Truncated

Observed in ClickHouse:

- `3 / 107` tool rows had `output_meta.truncated = true`.
- Tool output was capped around 20k characters in those rows.
- `output_meta` keeps metadata such as `orig_len`, `kept_len`, and `sha256`, but not the missing bytes.

By contrast, the existing local `auto-etl` output has:

- `1,528` message rows with full `content` over 20k characters.
- Maximum observed `content` length: `113,661`.

This is a direct blocker for replacing `auto-etl` with current Langfuse data.

#### Non-session auto-etl Sources Are Missing

Langfuse ClickHouse currently does not cover:

- hook event logs with verbatim `raw_json`
- git commit/file/hunk history
- GitHub PRs and review comments
- position-stable hook IDs from source file + byte offset

ClickHouse could store these, but Langfuse does not currently ingest them.

## Advantages Of ClickHouse

ClickHouse is attractive as the warehouse layer:

- Fast analytical SQL over large append-heavy event datasets.
- Natural fit for time-partitioned Message, Session, Hook, git, and PR tables.
- Better live-ingest story than rewriting parquet partitions.
- Compression works well for large text columns with ZSTD-style storage.
- Materialized views can maintain rollups such as tool counts, token totals, session duration, file heat maps, and error rates.
- Langfuse already demonstrates useful trace tree modeling and OTel ingestion.
- It gives us one backend that can serve both observability-style traces and warehouse-style analytics.

## Recommendation

Do not replace `auto-etl` with Langfuse ClickHouse as-is.

Do consider replacing the parquet output layer with ClickHouse tables shaped like `auto-etl`'s canonical model:

1. Keep or port the `auto-etl` parser/transform logic.
2. Add raw tables for source preservation:
   - `raw_session_lines`
   - `raw_hook_events`
   - `raw_git_commits` or equivalent import provenance tables
3. Add normalized ClickHouse tables mirroring:
   - `messages`
   - `sessions`
   - `hooks`
   - `git_repositories`
   - `git_refs`
   - `commits`
   - `commit_files`
   - `commit_hunks`
   - `pull_requests`
   - `pull_request_comments`
4. Treat Langfuse traces as a supplemental live observability source, not the canonical source of coding-agent session truth.
5. If Langfuse remains part of the path, remove or redesign the 20k output truncation for any data intended to replace `auto-etl`.

## Bottom Line

ClickHouse can probably replace parquet as the long-term storage engine for `auto-etl`.

Langfuse's existing ClickHouse data cannot replace `auto-etl` because it is a projected observability model, not a lossless Claude/Codex session model.
