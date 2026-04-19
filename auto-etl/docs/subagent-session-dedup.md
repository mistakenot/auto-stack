---
hash: "1835330a"
id: "576fcbbf"
read_when: "when implementing subagent session handling or deduplication in ETL"
summary: "Design doc for resolving duplicate session IDs caused by Claude Code subagent files, using agentId-based unique IDs with parent linkage."
title: "Subagent Session Deduplication"
---

# Subagent Session Deduplication

Design doc for fixing issue #1 from `issues.md`: duplicate session IDs in output.

## Problem

The ETL creates one `AgentSession` row per JSONL file. But Claude Code writes subagent sessions to separate JSONL files under a `subagents/` directory, all sharing the parent's `sessionId`. The test corpus has 179 files but only 64 unique session IDs — the 115-file gap is entirely subagent files.

Any downstream query that does `GROUP BY session_id` or `WHERE session_id = X` will silently double-count or return duplicates.

## Approach

**Option 2 from issues.md:** Give subagent files their own unique session ID and add a `parent_session_id` field linking back to the original.

Parent sessions keep their original UUID as the session ID. Subagent sessions use their `agentId` (from JSONL lines) as the session ID, with `parent_session_id` pointing to the parent UUID. Simple FK relationship, no composite keys.

## Changes

### 1. Parser: detect subagent files and read metadata

Add fields to `ParsedSession`:

```go
type ParsedSession struct {
    ID              string
    ParentSessionID string // set for subagent files, empty for parents
    AgentID         string // hex agent ID from JSONL lines (e.g. "a906621c1fcf0c74a")
    SubagentName       string // from .meta.json agentType (e.g. "Explore", "general-purpose")
    IsSubagent     bool   // true if any line has isSidechain: true
    Workspace       string
    Model           string
    SourcePath      string
    Lines           []ParsedLine
}
```

Add to `ParsedLine`:

```go
type ParsedLine struct {
    Type           string
    Timestamp      time.Time
    SessionID      string
    Cwd            string
    IsSubagent    bool   // from line's isSidechain field
    AgentID        string // from line's agentId field
    SourceLineIndex int   // 0-based position in the JSONL file, for stable sorting
    Message        ParsedMessage
}
```

**Detection logic in `ParseSession`:**

- Parse `isSidechain` and `agentId` from each raw line
- If any line has `isSidechain: true`, mark the session as a sidechain
- Extract `agentId` from the first line that has one

**Metadata loading:**

- After parsing a JSONL file, check if a sibling `.meta.json` exists (same filename stem + `.meta.json`)
- If found, read `agentType` field and set `SubagentName`
- Path pattern: `agent-{agentId}.jsonl` -> `agent-{agentId}.meta.json`

**Session ID assignment:**

- If `IsSubagent` is true: set `ID = agentId`, set `ParentSessionID = raw sessionId`
- If `IsSubagent` is false: set `ID = raw sessionId`, leave `ParentSessionID` empty

### 2. Model: new fields

**AgentSession:**

```go
type AgentSession struct {
    ID              string `parquet:"id"`
    ParentSessionID string `parquet:"parent_session_id,dict"`
    HostID          string `parquet:"host_id,dict"`
    Agent           string `parquet:"agent,dict"`
    SubagentName       string `parquet:"subagent_name,dict"`
    IsSubagent     bool   `parquet:"is_subagent"`
    // ... rest unchanged
}
```

- `ParentSessionID`: empty for parent sessions, parent's UUID for subagent sessions
- `SubagentName`: the subagent type from `.meta.json` (e.g. "Explore", "general-purpose", "Plan")
- `IsSubagent`: whether this session came from a subagent file

**AgentMessage** (denormalized for OLAP):

```go
type AgentMessage struct {
    // ...existing fields...
    ParentSessionID string `parquet:"parent_session_id,dict"`
    IsSubagent     bool   `parquet:"is_subagent"`
}
```

- `ParentSessionID`: denormalized from session — enables "all messages for a session tree" without joins
- `IsSubagent`: denormalized from session — enables filtering parent vs subagent messages directly

### 3. Transform: propagate new fields

In `transformSession`:

- Use `raw.ID` (which is now the `agentId` for subagents) for session and message IDs
- Set `ParentSessionID`, `SubagentName`, `IsSubagent` on the session row from `ParsedSession`
- No changes to message or blob logic — they already use `raw.ID` / `session.ID`

### 4. Message ordering: stable sort by (timestamp, source, line index)

Add `SourceLineIndex` to `AgentMessage`:

```go
type AgentMessage struct {
    // ...existing fields...
    SourceLineIndex int32 `parquet:"source_line_index"`
}
```

The parser tracks each line's 0-based index within its JSONL file. Transform copies this to messages. This enables stable sorting when timestamps collide:

**Sort key:** `(Timestamp, SourcePath, SourceLineIndex)`

- `Timestamp` — primary, chronological order
- `SourcePath` — secondary, groups same-file messages together on ties
- `SourceLineIndex` — tertiary, preserves JSONL file order within a file

This is deterministic and doesn't require parsing the `parentUuid` chain.

### 5. Tests

#### Unit test fixtures (`internal/parser/testdata/`)

Small handcrafted JSONL files checked into git, covering specific scenarios:

| Fixture | Purpose |
|---------|---------|
| `parent-session/session.jsonl` | Minimal parent session: 2-3 user/assistant lines, no subagents. Verifies baseline parsing, ID assignment, timestamps. |
| `with-subagent/session.jsonl` | Parent session that spawns an Agent tool call. |
| `with-subagent/subagents/agent-abc123.jsonl` | Subagent transcript with `isSidechain: true`, `agentId: "abc123"`. Verifies subagent detection, ID = agentId, ParentSessionID = parent UUID. |
| `with-subagent/subagents/agent-abc123.meta.json` | Meta file with `agentType: "Explore"`. Verifies SubagentName loading. |
| `subagent-no-meta/subagents/agent-def456.jsonl` | Subagent with no `.meta.json` (older format). Verifies graceful handling — SubagentName stays empty. |
| `empty-session/session.jsonl` | File with only non-message lines (progress, system). Verifies session is skipped. |

Each fixture is minimal — just enough lines to test the specific behavior. Real-world complexity is covered by E2E tests.

#### Unit tests

**`internal/parser/parser_test.go`:**

- `TestParseSession_ParentBaseline`: parse parent fixture, assert ID = sessionId from JSONL, ParentSessionID empty, IsSubagent false, SubagentName empty. Also verify Lines, Workspace, Model parse unchanged — regression that subagent changes don't break normal parsing.
- `TestParseSession_SubagentDetection`: parse subagent fixture, assert ID = agentId, ParentSessionID = parent UUID, IsSubagent true
- `TestParseSession_SubagentMetaLoading`: parse subagent with .meta.json, assert SubagentName = "Explore"
- `TestParseSession_SubagentNoMeta`: parse subagent without .meta.json, assert SubagentName empty, no error
- `TestParseSession_SourceLineIndex`: parse fixture, assert each ParsedLine has correct 0-based SourceLineIndex

**`internal/transform/transform_test.go`:**

- `TestTransformSession_SubagentFieldPropagation`: feed a subagent ParsedSession to transformSession, assert session row and all message rows have correct ParentSessionID, IsSubagent
- `TestTransformSession_MessageIDsUseSubagentID`: verify message IDs use the agentId-based session ID, not the parent UUID
- `TestTransformSession_BlobSessionIDsUseSubagentID`: verify blob AgentSessionID uses the agentId

#### Genstats: subagent-aware stats

Currently genstats has no subagent awareness. Add these fields to `stats.json`:

**Top-level stats:**

| Field | Type | Description |
|-------|------|-------------|
| `subagentFiles` | int | Files where any line has `isSidechain: true` |
| `parentFiles` | int | Files without `isSidechain` (= `totalFiles - subagentFiles - emptyFiles - unparseableFiles`) |
| `uniqueAgentIDs` | int | Distinct `agentId` values across all files |
| `subagentNames` | []string | Distinct `agentType` values from `.meta.json` files (e.g. `["Explore", "general-purpose", "Plan"]`) |
| `subagentFilesWithoutMeta` | int | Subagent files with no `.meta.json` sibling |

**Per-file additions to `FileStats`:**

| Field | Type | Description |
|-------|------|-------------|
| `isSubagent` | bool | Whether any line has `isSidechain: true` |
| `agentId` | string | The `agentId` from JSONL lines (empty for parents) |
| `subagentName` | string | From `.meta.json` if present |

#### E2E tests (against `.tmp/` corpus)

**E2E test additions:**

Regression (parent sessions still parse normally):
- `TestE2E_ParentSessionIDsMatchRawSessionID`: verify every non-subagent session's ID equals the `sessionId` from its JSONL — no ID mangling
- `TestE2E_ParentSessionsHaveNoParent`: verify parent sessions have empty `ParentSessionID`, empty `SubagentName`, `IsSubagent == false`
- `TestE2E_ParentMessageAndBlobCounts`: verify total message/blob counts still match `stats.expectedMessages()` / `stats.expectedBlobs()` (existing tests, kept as-is)

Subagent-specific:
- `TestE2E_SessionIDsUnique`: verify no duplicate session IDs in output
- `TestE2E_SubagentSessionCount`: verify count of output sessions where `IsSubagent == true` matches `stats.SubagentFiles` (after filtering for processable files)
- `TestE2E_ParentSessionCount`: verify count of output sessions where `IsSubagent == false` matches `stats.ParentFiles`
- `TestE2E_SubagentSessionsHaveParent`: verify all subagent sessions have non-empty `ParentSessionID`
- `TestE2E_SubagentNamesMatch`: verify `SET(subagent_name)` from output sessions matches `stats.SubagentNames`
- `TestE2E_MessageParentSessionIDConsistent`: verify message `ParentSessionID` matches its session's `ParentSessionID`
- Existing `TestE2E_SessionCount` still passes (same count, just unique IDs now)

## Data examples

**Before (duplicate IDs):**

| id | source_path | parent_session_id |
|----|-------------|-------------------|
| `ab2a6291-...` | `.../ab2a6291-....jsonl` | _(field didn't exist)_ |
| `ab2a6291-...` | `.../subagents/agent-38babac48c0a60a1.jsonl` | _(field didn't exist)_ |

**After (unique IDs with linkage):**

| id | source_path | parent_session_id | subagent_name | is_subagent |
|----|-------------|-------------------|------------|--------------|
| `ab2a6291-d5fb-4aa3-a590-fc3584911d44` | `.../ab2a6291-....jsonl` | _(empty)_ | _(empty)_ | false |
| `aside_question-38babac48c0a60a1` | `.../subagents/agent-38babac48c0a60a1.jsonl` | `ab2a6291-d5fb-4aa3-a590-fc3584911d44` | _(no .meta.json)_ | true |

## Downstream query patterns

```sql
-- All parent sessions (no duplicates)
SELECT * FROM sessions WHERE parent_session_id = ''

-- Subagents for a specific session
SELECT * FROM sessions WHERE parent_session_id = 'ab2a6291-...'

-- All messages for a session tree (no join needed, denormalized)
SELECT * FROM messages
WHERE session_id = 'ab2a6291-...' OR parent_session_id = 'ab2a6291-...'
ORDER BY timestamp, source_line_index

-- Only parent messages
SELECT * FROM messages WHERE NOT is_subagent

-- Only subagent messages for a parent
SELECT * FROM messages WHERE parent_session_id = 'ab2a6291-...'
```

## Files to modify

| File | Change |
|------|--------|
| `internal/model/model.go` | Add `ParentSessionID`, `SubagentName`, `IsSubagent` to `AgentSession`; add `ParentSessionID`, `IsSubagent`, `SourceLineIndex` to `AgentMessage` |
| `internal/parser/parser.go` | Add `IsSubagent`, `AgentID`, `SourceLineIndex` to `ParsedLine`; add `ParentSessionID`, `AgentID`, `SubagentName`, `IsSubagent` to `ParsedSession`; parse new fields from raw JSON; load `.meta.json`; assign session IDs |
| `internal/parser/testdata/` | Handcrafted JSONL fixtures for unit tests (checked into git) |
| `internal/parser/parser_test.go` | Unit tests for subagent detection, ID assignment, meta loading |
| `internal/transform/transform.go` | Propagate new session fields; set `SourceLineIndex` on messages |
| `internal/transform/transform_test.go` | Unit tests for field propagation to session/message/blob rows |
| `cmd/genstats/main.go` | Track subagent detection fields for test expectations |
| `e2e_test.go` | Add uniqueness, parent linkage, and denormalization consistency tests |
