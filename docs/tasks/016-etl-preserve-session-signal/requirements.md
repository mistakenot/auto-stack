---
hash: "cdab6b1a"
id: "2518bf12"
read_when: "adding or reviewing session signal preservation fields in auto-etl parquet output"
summary: "Requirements to stop auto-etl silently dropping signal: thinking blocks, skill attribution, stop_reason, permission mode, and cache token split (AC-1 through AC-6)."
title: "Task 016: ETL Preserve Session Signal Requirements"
---

# Task 016: ETL Preserve Session Signal

## Problem

The auto-etl transform silently drops several pieces of signal present in the raw
Claude JSONL, so they are unreachable from autosearch at any disclosure rung. The
[progressive-disclosure audit](../../../auto-search/docs/progressive-disclosure-audit.md)
proved this against a real session: **assistant thinking blocks** (the highest-value
loss — all 7 in the test session had real content, contradicting the "usually
redacted" assumption that justified dropping them), plus **skill attribution**,
`stop_reason`, `permission_mode`, CLI `version`, the tool_result `is_error` flag,
and the cache-token split. The transform's content-block loop hits `default:
continue` for any block it doesn't recognize (e.g. `thinking`).

## Goals

- Stop discarding assistant `thinking` / `redacted_thinking` blocks; preserve them
  in the canonical parquet `content` and make them reachable end-to-end via autosearch.
- Populate `skill_name` from `attributionSkill` so skill-driven sessions are
  discoverable via `autosearch --skill`.
- Capture the cheap sibling fields the transform currently ignores: `stop_reason`,
  permission mode, CLI `version`, tool_result `is_error`, and the
  `cache_creation` vs `cache_read` token split.
- One `SchemaVersion` bump (5 → 6) covers all additions; a `--full` rebuild backfills history.
- Honor the auto-etl decision principle: keep original data, add fields rather than
  replace/transform existing ones.

## Acceptance Criteria

**AC-1**: Thinking blocks preserved
- Given: a Claude session whose assistant messages contain non-empty `thinking` blocks
- When: `autoetl run` transforms it and `autosearch index` runs
- Then: each thinking block is a message with `role="thinking"` whose `content` holds
  the full reasoning text and whose `thinking_signature` column holds the block's
  signature; `redacted_thinking` blocks are preserved as a marker row.

**AC-2**: Thinking reachable end-to-end via autosearch (opt-in)
- Given: an indexed session with thinking messages
- When: a user runs `autosearch search --role thinking`, `autosearch session get <id> --include-thinking`,
  and `autosearch message get <id>-<idx>`
- Then: thinking is **excluded from default** `search` and `session get`; `--role thinking`
  filters to thinking messages; `session get --include-thinking` renders them as
  `<thinking index=N>…</thinking>`; `message get` returns the full untruncated reasoning.
- Note: "excluded from default" governs the search/get **views** only. Session-level
  aggregates (`session describe` message/role counts, per-session `message_count`) are
  **intentionally inclusive** of thinking — it is a real message in the canonical dataset.
  Counts will rise after the `--full` backfill; this is the expected new baseline, not a regression.

<!-- RESOLVED(P3): "Opt-in exclusion" is a search-time WHERE filter — session aggregates still count thinking
REVIEW: The default-exclude is implemented as `role != 'thinking'` in the message-search / session-get
queries (solution step 6). But the new thinking rows are real messages in the `messages` dataset, so
any COUNT-style aggregate computed independently of those filters will now include them — notably the
per-session `message_count` (transform builds it from len(messages)) and role-based stats in
`session describe`. After the `--full` backfill, session message counts and role tallies will jump,
even though the default search/get views hide thinking. That's probably acceptable ("new baseline" per
context.md gotcha #6), but the requirement frames thinking as "excluded from default" — make explicit
whether session-level counts/describe are also meant to exclude it, or are intentionally inclusive.
The roleTag render path is verified fine: session.go:343-344 default → `tagName=m.Role` and
attrs `index=%d` from MessageIndex (session.go:348), so a thinking row renders `<thinking index=N>`
with NO code change, as claimed.
AUTHOR: Resolved — declared INTENTIONALLY INCLUSIVE. Added a note to AC-2: "excluded from default"
scopes to the search/get VIEWS only; session-level aggregates (`session describe` counts, per-session
`message_count`) count thinking because it is a real canonical message. The post-backfill count rise
is the expected new baseline, not a regression. No extra exclusion logic added to the aggregate paths
(keeps thinking honestly represented in totals; only the human-facing default views hide it).
-->


**AC-3**: Skill attribution populated
- Given: a session where assistant messages carry `attributionSkill` (e.g. a `/review-task` run)
- When: it is transformed and indexed
- Then: `skill_name` is populated from `attributionSkill`, and
  `autosearch search --skill <name>` returns the session's messages (today: 0 hits).

**AC-4**: Sibling fields captured
- Given: raw lines carrying `stop_reason`, permission mode, CLI `version`,
  tool_result `is_error`, and `cache_creation_input_tokens` / `cache_read_input_tokens`
- When: transformed
- Then: `stop_reason` and `is_error` are written on the relevant message rows;
  `permission_mode` and `version` are stored once per **session**; the cache split is
  **added** as two new message columns while the combined `cache_input_tokens` (sum) is kept.

**AC-5**: Schema bump + backfill
- Given: the new fields/role
- When: `SchemaVersion` is bumped to 6 and `autoetl run --full` is run
- Then: historical partitions are rebuilt with the new signal; autosearch reindexes cleanly.

**AC-6**: Tests + docs
- Given: the new behavior
- When: the change lands
- Then: e2e/unit tests assert thinking-role emission and skill_name population, and
  `auto-etl/docs/claude-message-types-and-etl-mapping.md` is updated to reflect what is
  now preserved.

## Out of Scope

- Full-session retrieval escape hatch (`session get --full` / indexing `transcript_full`) — audit P1 #2, a separate task.
- Exposing `source_path` / `source_line_index` in `autosearch describe` — separate task.
- Capturing the conversation graph (`parentUuid`/`uuid` branching) — separate task.
- Preserving `attachment` / `last-prompt` / `file-history-snapshot` line-type payloads.
- Secret/credential masking of thinking content.

## Open Questions

- [x] Should thinking messages appear in **default** `search` results and `session get`, or be opt-in? (answered: **Opt-in** — excluded from default `search`/`session get`; surfaced via `--role thinking` or `session get --include-thinking`)
- [x] Store the thinking `signature` or only the reasoning text? (answered: **Store it** — add a `thinking_signature` column, honoring the keep-everything principle)
- [x] Attribute `permission_mode` and CLI `version` at the **session** or **per-message** level? (answered: **Session level** — one value per session row; capture initial/last-seen if it changes mid-session)
- [x] Cache split: **add** new columns or **replace** the combined one? (answered: **Add** `cache_creation_input_tokens` + `cache_read_input_tokens`, keep the combined `cache_input_tokens` sum)
