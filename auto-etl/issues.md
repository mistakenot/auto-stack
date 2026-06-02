# Known Issues

Discovered 2026-03-16 during E2E test development. The `genstats` tool was built as an independent JSONL analyzer (no ETL imports) to produce raw metrics for test assertions. Comparing those raw metrics against ETL output revealed these discrepancies.

## ~~1. Duplicate session IDs in output~~ (FIXED)

**Status:** Fixed in `01bb8ad`. Subagent files now use `agentId` as session ID with `parent_session_id` FK linking back to the parent. See `docs/subagent-session-dedup.md` for design.

## ~~2. System lines are processed but always empty~~ (FIXED)

**Status:** Fixed by capturing `system / subtype=turn_duration` into `AgentSession.TotalTurnDurationMs`. Other system subtypes still produce no rows, which is correct — they're metadata, not transcript content.

## 3. 9 file tool uses with no extractable blob

**Severity:** Low — minor data loss.

**Problem:** Of 2,927 Read/Write/Edit tool_use blocks, 9 have a `file_path` in their input but no extractable content. The blob extraction returns nil for these.

**Discovered by:** `genstats` reports `fileToolUses: 2927` vs `fileToolUsesWithBlob: 2918`, a gap of 9.

**Likely causes:**
- Read calls where the corresponding `tool_result` block wasn't found in the session (the `toolResultContent` map lookup returns empty). This can happen at subagent boundaries — the tool_use is in one file but the tool_result is in another.
- Edit calls where both `old_string` and `new_string` are missing from the input.

**Fix:** Could log these as warnings during transform to make them visible. May also be related to issue #1 (subagent file splitting).

## 4. 42% of JSONL lines are non-message metadata

**Severity:** Info — no bug, but relevant for performance.

**Problem:** 12,142 of 28,633 lines (42%) have empty `message.content`. These are `progress` (10,924), `file-history-snapshot` (786), `queue-operation` (128), `last-prompt` (15), and `system` (289) line types. The parser reads and unmarshals all of them before the transform skips them.

**Discovered by:** `genstats` reports `emptyContents: 12142` out of `totalLines: 28633`. Cross-referencing with `linesByType` shows the breakdown.

**Possible optimization:** Skip non-message line types during parsing (before JSON unmarshalling the full message body) by checking the `type` field first. This would reduce memory allocation and parse time by ~40%.
