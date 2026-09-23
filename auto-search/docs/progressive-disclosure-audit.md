---
hash: "341963a2"
id: "db2ed535"
read_when: "improving autosearch progressive disclosure, deciding what session data to capture/surface, or fixing ETL information loss for thinking blocks / skill attribution / full-session retrieval"
summary: "Audit of autosearch progressive disclosure against a real Claude session: the search→session→outline→message drill-down ladder works for message text and tool I/O, but thinking blocks, skill attribution, permission mode, and full-session transcript are dropped or unreachable. Includes a prioritized fix list."
title: "AutoSearch Progressive Disclosure & Information-Loss Audit"
---

# AutoSearch Progressive Disclosure & Information-Loss Audit

**Date:** 2026-06-08
**Method:** Walked the full autosearch disclosure ladder against one real recent session and diffed every rung's output against the original Claude Code JSONL, the ETL canonical model, and the index/render code.

**Test session:** `c9124cdf-f262-470a-87cf-3390e0926be0` — a `/review-task 013` run.
- Raw: `~/.claude/projects/-home-vscode-src-auto-stack/c9124cdf-….jsonl` (82 lines, ~284 KB)
- 50 ETL messages, 19 tool calls (Bash/Read/Edit), **7 populated thinking blocks**, span ~3.5 min, `permissionMode: bypassPermissions`, CLI version 2.1.168, model `claude-opus-4-8`.

This session was chosen because it exercises both ordinary message rendering *and* the rarer non-message line types (`mode`, `permission-mode`, `file-history-snapshot`, `attachment`, `last-prompt`, `system`) that an ETL is most likely to drop.

---

## 1. Verdict

Progressive disclosure **works as designed** for the common case: you can search cheaply, get a session overview, read the transcript, and drill to a single message's full content. The drill-down rungs cross-reference each other correctly (truncated output prints the exact next command to run).

Two structural gaps remain:
1. **No one-shot full-session retrieval** — session-level full transcript is dropped at index time.
2. **A class of metadata/non-message signal is dropped at the ETL boundary** and is therefore unreachable from autosearch at *any* rung — most importantly **thinking blocks** and **skill attribution**.

---

## 2. The disclosure ladder (verified working)

| Rung | Command | Output | Cost |
|---|---|---|---|
| Discover | `search "review-task" --scope sessions` | session hits: IDs + metadata, no bodies | tiny |
| Pinpoint | `search "" --session-id <id> --tool-name Edit` | per-message hits with message IDs (`<id>-48`) + snippets | tiny |
| Overview | `session describe <id>` | tokens, duration, tool/file counts, git remote, model, head+tail summary | small |
| Map | `session outline <id>` | the *shape* of the session: sub-agent spine, timeline cut into labelled segments (`<id>#s<n>`) with counts and index ranges, per-Message ids — **no bodies**; `--depth N` / `--expand <id>` navigate statelessly | small |
| Read | `session get <id>` | all messages as `<user/agent/tool index=N>`; bodies truncated at 2048 ch with `…[truncated — run: autosearch message get <id>-N]…` | medium (42 KB here) |
| Drill | `message get <id>-N` | one message, **full untruncated** (`-1` → 3922 ch vs 2048 ch in `session get`) | scoped |
| Inspect | `message describe <id>-N` | metadata + raw `toolUseResult` envelope (`structuredPatch`, stdout/stderr) | tiny |

**What's good:** `session get` always tells you the exact `message get` command to recover any truncated body. `session outline` extends the same convention upward: a collapsed segment or sub-agent prints its `session outline … --expand <segId>` / `--depth N` command, and each Message leaf prints its `message get <id>` command — so no rung is a dead end. `message get` is full-fidelity by default (no flag needed). Tool rows carry `name`/`cmd`/`path`/`duration_ms`/`interrupted` inline so you can triage without drilling further. This is correct, token-efficient progressive disclosure.

---

## 3. Can you reach full detail?

- **Per-message: yes.** `message get <sid>-<idx>` returns the complete stored `content` (the full `content` column survives JSONL → parquet → sqlite). Message IDs are enumerable from `session get` or search hits.
- **Per-session: no.** The ETL writes `transcript_full` into parquet, but it is **dropped at sqlite index time** (`internal/indexdb/indexer.go:285` inserts only `transcript_truncated`). Rebuilding a whole session requires looping `message get` over every index (~50 calls here) or going back to parquet/raw JSONL.

---

## 4. Information present in the JSONL but lost / unreachable

These exist in the raw file but **never survive ETL**, so no autosearch command (including `message get`) can reach them. Ranked by importance.

1. **Extended thinking — 7 real reasoning blocks, fully populated** (`thinking` text + `signature`). The transform's content-block loop hits `default: continue` for the `thinking` type (`auto-etl/internal/transform/transform.go:346`). Verified: every "thinking"/"signature" string in `session get` output is incidental prose (e.g. `(exact signatures)`), not a reasoning block.
   - ⚠️ The project's own mapping doc (`auto-etl/docs/claude-message-types-and-etl-mapping.md`) justifies dropping these as *"often redacted/empty anyway."* **In this real recent session all 7 had substantial content.** That assumption is outdated. This is the highest-value loss for analyzing *why* an agent acted.
2. **Skill attribution.** Every assistant message carried `attributionSkill: "review-task"`, yet `session describe` reports `skillsUsed: null` / `skillMessages: 0`, and `search --skill review-task --session-id <id>` returns **0 hits**. A skill-driven session is invisible to skill filters. (`skill_name` only populates from an explicit `Skill` tool call, not from attribution.)
3. **`permission-mode` / `mode`.** This session ran `bypassPermissions` — security-relevant execution context — dropped entirely. Not a handled line type.
4. **`stop_reason`** (34×`tool_use`, 2×`end_turn`), **`requestId`**, **Claude Code `version`** — provenance / turn-flow, all dropped (not parsed in `auto-etl/internal/parser/parser.go`).
5. **Conversation graph.** Raw `uuid`/`parentUuid`/`promptId`/`leafUuid` are dropped; ETL substitutes synthetic ordinal IDs. Linear turn order survives; true parent/child branching does not.
6. **`attachment` payloads** — injected `skill_listing` (46 skills), `deferred_tools_delta`, pending MCP servers (`mcp-agent-mail`). The model's visible environment is dropped.
7. **`last-prompt`** (`/review-task 013`), **`isMeta`**, content-block **`is_error`** (parsed into the struct but never written to a model field), and the **cache_creation vs cache_read token split** (merged into one `cache_input_tokens` column).

**Captured in parquet but never *surfaced* by any command:** `git_branch`, and `source_path` / `source_line_index`. The last two are especially useful — they would let a user jump straight back to the raw JSONL, the ultimate disclosure rung.

---

## 5. Prioritized fix list

1. **Store thinking content** in the message `content` (the block is already recognized in the transform; just stop discarding it). Re-validate the "usually redacted" assumption against current data — it no longer holds. *(P1 — highest analytic value)*
2. **Add a full-session escape hatch** — either index `transcript_full`, or add `session get --full` / `--raw` that streams from the source JSONL path. Today session-level fidelity silently degrades to a 600-char summary. *(P1)*
3. **Populate `skill_name` from `attributionSkill`** so skill-driven sessions are discoverable by `--skill`. *(P2)*
4. **Expose `source_path` (and `source_line_index`) in `session describe` / `message describe`** — already stored, just hidden; gives an explicit "here's the original file" rung. *(P2)*
5. **Capture `permission_mode`, `stop_reason`, and `version`** as session/message columns — cheap, high value for security and turn-flow analysis. *(P3)*
6. **Preserve the cache_creation vs cache_read split** and content-block `is_error` instead of merging/discarding. *(P3)*

---

## 6. Key code references

- ETL canonical model: `auto-etl/internal/model/model.go` (`SchemaVersion = 4`; `content` full + `content_truncated` @ 4096 ch; session `transcript_full` + `transcript_truncated` @ 512 KB)
- ETL parser (raw line fields parsed): `auto-etl/internal/parser/parser.go:100-131`
- ETL transform (block dispatch; `default: continue` drops thinking): `auto-etl/internal/transform/transform.go:233-348`
- Project's own preserved-vs-lost mapping: `auto-etl/docs/claude-message-types-and-etl-mapping.md`
- sqlite schema (no `transcript_full` column): `auto-search/internal/indexdb/schema.go:31-92`
- `transcript_full` dropped at index time: `auto-search/internal/indexdb/indexer.go:285-296`
- `session get` (2048-ch per-message truncation): `auto-search/internal/cli/session.go:191-230`
- `message get` (full, untruncated — the escape hatch): `auto-search/internal/cli/message.go:25-55`
