---
hash: "621f8248"
id: "26b2da6c"
read_when: "when implementing auto-etl schema updates or normalizing parquet field population"
summary: "Phased plan to bring auto-etl output schema in line with the user-journey spec, covering blob removal, git metadata, session transcripts, and field population."
title: "Plan: Schema Cleanup — Align with User-Journey Spec"
---

# Plan: Schema cleanup — align with user-journey spec

This plan covers bringing auto-etl's output schema in line with the
[user-journey](../../docs/user-journey.md) spec. The goal is a two-table
schema (messages + sessions) with all fields needed by downstream tools
(autosearch, autoreflect, duckdb ad-hoc queries). Schema version stays
at 1 — all existing output files will be regenerated.

## Decisions

- `content` always stores the full unmodified tool output. `content_truncated`
  is the only derived field. No data loss in `content`.
- `tool_file_blob_id` is removed entirely (clean break, blobs are gone).
- Transcript format uses `[tool:Read]:\n`, `[tool:Bash]:\n` etc — tool name
  included for searchability.
- git_remote cache: empty string for both "no remote" and "workspace missing".
  Simple non-empty check for downstream consumers.
- `tool_file_start_line` / `tool_file_num_lines` only populated from explicit
  tool input fields (Read's `offset`/`limit`). No file I/O inference for Edit.
- `transcript_truncated` also has an overall cap (e.g. 512k chars) with
  mid-truncation applied to the whole transcript string after assembly.
- Keep 1-row-per-content-block split (current behavior). Each tool_use and
  tool_result is its own message row.

## Current state

- Three parquet datasets: `messages`, `sessions`, `blobs`
- Several fields defined in model structs but not populated (`host_id`,
  `tool_file_start_line/num_lines/total_lines`)
- No git metadata, no transcript fields, no truncated content variants
- Blobs table stores file contents separately from messages

## Target state (user-journey spec)

Two datasets only: `messages` and `sessions`. File content is inlined into
messages. All fields the user-journey `DESCRIBE` blocks show must exist and
be populated.

---

## Phase 1: Drop blobs, inline into messages

**Why:** The user-journey schema has no blobs table. Parquet is columnar so
large content values don't penalise queries that skip that column. Keeping
blobs separate adds join complexity for downstream consumers.

### Tasks

1. Remove `AgentFileBlob` struct and `blobs/` writer path from `writer.go`.
2. Remove blob extraction from `transform.go`.
3. For Read/Write/Edit tool results, store the full unmodified content inline
   in `messages.content`. This field is immutable — always the raw value.
4. Add `content_truncated` (string) to `AgentMessage` — derived field with
   long content mid-truncated.
5. Remove `tool_file_blob_id` from `AgentMessage`.
6. Remove blob-related fields from `TransformedRows`.
7. Update tests.

### Truncation rule

Default max: **4096 chars** (configurable, covers ~3k tokens which is
plenty of context for a single tool output while keeping transcripts
manageable).

For any message where `len(content) > maxChars`:
- Calculate `excess = len(content) - maxChars + len(marker)`
- Find midpoint of content
- Remove `excess` chars symmetrically outward from the midpoint
- Insert marker `\n…[truncated {n} chars]…\n` at the cut point
- `content` keeps the full value; `content_truncated` gets the cut version

### Acceptance criteria

- `blobs/` directory not created in output.
- No `AgentFileBlob` type in codebase.
- `go test ./...` passes.

### Verification

**Unit** (`internal/transform/transform_test.go`):
- Truncation function: input below threshold → `content_truncated` equals
  `content`. Input above → output is exactly `maxChars` long, contains
  marker, starts and ends with equal halves of original.
- Edge cases: content exactly at threshold, content one byte over, empty
  content.

**E2E** (`e2e_test.go`):
- Remove `TestE2E_BlobCount` and `TestE2E_BlobsHaveRequiredFields`.
- Update `TestE2E_OutputDirectoriesExist` to check only `messages/` and
  `sessions/`.
- New: `TestE2E_ContentNotTruncated` — for all messages with
  `tool_name` in (Read, Write, Edit), verify `content` contains full
  tool output (not the truncation marker).
- New: `TestE2E_ContentTruncatedField` — for messages where
  `len(content) > 4096`, verify `content_truncated` contains the marker
  and `len(content_truncated) <= 4096 + len(marker)`. For messages where
  `len(content) <= 4096`, verify `content_truncated == content`.
- Update `genstats` to drop blob-related stats, add
  `fileToolContentLengths` distribution for verifying truncation.

**CLI probe**:
```bash
# No blobs directory
test ! -d "$OUTPUT/blobs"

# content_truncated populated
duckdb -c "
  SELECT count(*) as truncated
  FROM '$OUTPUT/messages/**/*.parquet'
  WHERE content_truncated != content
" | grep -v '^0$'

# content is never truncated (no marker in content field)
duckdb -c "
  SELECT count(*) as bad
  FROM '$OUTPUT/messages/**/*.parquet'
  WHERE content LIKE '%…[truncated%chars]…%'
" | grep '^0$'
```

---

## Phase 2: Add git metadata fields

**Why:** The user-journey spec shows `git_remote` on both sessions and
messages, and `git_branch` on messages. These enable cross-host project
identity and per-message branch context.

### What's in the raw JSONL

- **`gitBranch`** — present on nearly every message line. Values include
  `"main"`, `"master"`, `null`, `"HEAD"`, worktree branches like
  `"worktree-agent-a40c490e"`.
- **`cwd`** — workspace path, already extracted.
- **`git_remote`** — NOT in transcripts. Must be derived.

### git_remote resolution and caching

On first encounter of a workspace path, autoetl runs
`git -C <workspace> remote get-url origin`. The result is cached in
`~/.auto/etl/settings.json` under a `remotes` map:

```json
{
  "remotes": {
    "/home/vscode/src/auto-stack": "git@github.com:user/auto-stack.git",
    "/home/vscode/src/notes": "git@github.com:user/notes.git",
    "/home/vscode/old-project": ""
  }
}
```

Resolution rules:
- **Cache hit** — use the stored value (even if empty string).
- **Cache miss, workspace exists on disk** — run git command, store result.
  Empty string if not a git repo or no remote configured.
- **Cache miss, workspace doesn't exist** — store empty string.

Empty string for both "no remote" and "couldn't check" — downstream consumers
just check non-empty.

If a remote genuinely changes (rare), the user can edit `settings.json`
or delete the entry to force re-resolution.

### Tasks

1. Add to `AgentMessage`:
   - `git_branch string` — read directly from `gitBranch` field on each
     JSONL message line (per-message, not per-session — branch can change
     mid-session via worktrees).
   - `git_remote string` — denormalized from session.
2. Add to `AgentSession`:
   - `git_remote string` — looked up from the remotes cache by workspace.
3. Implement remotes cache: load from `~/.auto/etl/settings.json` at start,
   resolve missing workspaces, write back at end of run.
4. Update transform to extract `gitBranch` from raw JSONL lines.
5. Update tests with fixture sessions that have git metadata.

### Acceptance criteria

- Messages have `git_branch` populated for any JSONL line that has `gitBranch`.
- Sessions have `git_remote` populated for workspaces that are git repos
  with a remote.
- `~/.auto/etl/settings.json` updated with new workspace→remote entries
  after a run.
- `go test ./...` passes.

### Verification

**Unit** (`internal/transform/transform_test.go`):
- `gitBranch` extraction: JSONL line with `"gitBranch": "main"` → message
  row has `git_branch == "main"`. Null/missing → empty string.
- Remotes cache: mock settings file with one hit and one miss, verify hit
  uses cached value, miss triggers resolution.

**E2E** (`e2e_test.go`):
- New: `TestE2E_GitBranchPopulated` — count messages with non-empty
  `git_branch`, verify > 0 (test data has `gitBranch` on most lines).
- New: `TestE2E_GitRemoteOnSessions` — for sessions with workspace
  matching current machine's repos, verify `git_remote` is non-empty.
- Update `genstats` to count lines with `gitBranch` field for baseline.

**CLI probe**:
```bash
# git_branch populated on messages
duckdb -c "
  SELECT git_branch, count(*) as n
  FROM '$OUTPUT/messages/**/*.parquet'
  WHERE git_branch != ''
  GROUP BY git_branch
  ORDER BY n DESC
  LIMIT 10
"

# git_remote populated on sessions
duckdb -c "
  SELECT git_remote, count(*) as n
  FROM '$OUTPUT/sessions/**/*.parquet'
  WHERE git_remote != ''
  GROUP BY git_remote
"

# settings.json has remotes
cat ~/.auto/etl/settings.json | jq '.remotes | length'
```

---

## Phase 3: Add session transcript fields

**Why:** The user-journey spec shows `transcript_full` and
`transcript_truncated` on sessions. These power `autosearch` full-text
indexing without requiring message-level joins.

### Tasks

1. Add to `AgentSession`:
   - `transcript_full string` — all message contents concatenated, raw
   - `transcript_truncated string` — same but each message uses
     `content_truncated` where available
2. Build transcripts during session aggregation in transform, after all
   messages for a session are collected.
3. Concatenation format: one message per block, separated by `\n\n`, prefixed
   with `[role]:\n` for user/assistant/system, or `[tool:ToolName]:\n` for
   tool messages.
4. Apply an overall cap to `transcript_truncated` (e.g. 512k chars) using the
   same mid-truncation approach. `transcript_full` is never capped.

### Acceptance criteria

- Every session has non-empty `transcript_full` and `transcript_truncated`.
- `transcript_full` contains all message content, unmodified.
- `transcript_truncated` is always <= 512k chars (or configured cap).
- `go test ./...` passes.

### Verification

**Unit** (`internal/transform/transform_test.go`):
- Build transcript from 3 messages (user, assistant, tool:Read), verify
  format: `[user]:\n...\n\n[assistant]:\n...\n\n[tool:Read]:\n...`.
- Transcript with one huge message: `transcript_full` contains it,
  `transcript_truncated` uses `content_truncated` version.
- Overall cap: build transcript from many messages exceeding 512k total,
  verify `transcript_truncated` is mid-truncated and within cap.

**E2E** (`e2e_test.go`):
- New: `TestE2E_TranscriptsPopulated` — all sessions have non-empty
  `transcript_full` and `transcript_truncated`.
- New: `TestE2E_TranscriptTruncatedWithinCap` — no session's
  `transcript_truncated` exceeds the cap.
- New: `TestE2E_TranscriptContainsRolePrefixes` — `transcript_full`
  contains `[user]:` and `[assistant]:` substrings.

**CLI probe**:
```bash
# Transcripts populated
duckdb -c "
  SELECT count(*) as has_transcript
  FROM '$OUTPUT/sessions/**/*.parquet'
  WHERE transcript_full != '' AND transcript_truncated != ''
"

# Truncated transcripts within cap
duckdb -c "
  SELECT max(length(transcript_truncated)) as max_len
  FROM '$OUTPUT/sessions/**/*.parquet'
"

# Spot-check format
duckdb -c "
  SELECT left(transcript_full, 200)
  FROM '$OUTPUT/sessions/**/*.parquet'
  LIMIT 1
"
```

---

## Phase 4: Populate unpopulated fields

**Why:** Several fields exist in the struct but are never set.

### Tasks

1. **`host_id`** — read from `~/.auto/host.json` at startup, set on every
   session and message row. If file missing, use `os.Hostname()` and warn.
2. **`tool_file_start_line`** — extract from Read tool input JSON `offset`
   field only. Not inferred for Edit (would require file I/O at transform
   time).
3. **`tool_file_num_lines`** — extract from Read tool input JSON `limit`
   field only.
4. **`tool_file_total_lines`** — not reliably available from tool input
   alone; leave as 0 for now, document as best-effort.
5. **`parent_session_id` / `is_subagent`** on messages — already denormalized
   in struct, verify these are populated in transform.

### Acceptance criteria

- Every session and message row has non-empty `host_id`.
- Read tool messages with `offset` in input have `tool_file_start_line > 0`.
- `go test ./...` passes.

### Verification

**Unit** (`internal/transform/transform_test.go`):
- Host ID: transform with `~/.auto/host.json` present → rows have that
  host_id. Missing file → falls back to `os.Hostname()`.
- Read tool input `{"file_path": "...", "offset": 10, "limit": 50}` →
  `tool_file_start_line == 10`, `tool_file_num_lines == 50`.
- Edit tool input → `tool_file_start_line == 0` (not inferred).

**E2E** (`e2e_test.go`):
- New: `TestE2E_HostIDPopulated` — all sessions and all messages have
  non-empty `host_id`.
- New: `TestE2E_ReadToolFileMetadata` — for messages where
  `tool_name == "Read"` and tool input contains `offset`, verify
  `tool_file_start_line` matches the input value.
- Existing `TestE2E_MessageParentSessionIDConsistent` already covers
  parent_session_id denormalization.

**CLI probe**:
```bash
# host_id populated everywhere
duckdb -c "
  SELECT count(*) as missing
  FROM '$OUTPUT/sessions/**/*.parquet'
  WHERE host_id = ''
" | grep '^0$'

# Read tool file metadata
duckdb -c "
  SELECT tool_file_start_line, tool_file_num_lines, count(*)
  FROM '$OUTPUT/messages/**/*.parquet'
  WHERE tool_name = 'Read' AND tool_file_start_line > 0
  GROUP BY 1, 2
  LIMIT 10
"
```

---

## Phase 5: Update reference docs

1. Keep `SchemaVersion` at `1` — this is the first real schema, not a
   migration. All existing output files will be regenerated from source
   JSONL via `autoetl run --full`.
2. Update `normalized-schema.md` reference doc to match new fields.

### Acceptance criteria

- `normalized-schema.md` matches the actual struct fields in `model.go`.
- No references to blobs table in any doc.

### Verification

**CLI probe**:
```bash
# No blob references in docs
! grep -ri 'blob' auto-etl/docs/reference/normalized-schema.md

# Schema describes exactly the fields in the struct
# (manual review — compare doc table rows to struct fields)
```

---

## Ordering and dependencies

```
Phase 1 (drop blobs)
  └─> Phase 3 (transcripts need inline content from phase 1)

Phase 2 (git metadata)  — independent, can run in parallel with phase 1

Phase 4 (populate fields) — independent, can run in parallel

Phase 5 (update docs)   — after all other phases complete
```

## Out of scope

- S3/remote backends
- Codex or other agent source formats
- Semantic embeddings
- `init` / `doctor` / `quickstart` CLI subcommands
- Raw file backup with MessagePack
