---
hash: "fb90274d"
id: "95f73b7d"
read_when: "implementing session intent summary extraction or understanding the first_user_intent field design"
summary: "Deterministic junk-skip heuristic with slash-command fallback and headTruncate on rune boundary, adding first_user_intent and first_user_intent_raw parquet fields to AgentSession."
title: "Solution: Task 015 — Session Intent Summary"
---

# Solution: Task 015

## Approach

The intent is a **derived field computed once in `auto-etl`** (same pattern as
`transcript_truncated`), then carried verbatim through the `auto-search` index into CLI
output. No new infrastructure — just one helper, two parquet columns, and threading.

1. **Compute intent in the ETL transform.** In `transformSession`, after the `messages`
   slice is built, walk user-role messages in order and pick the first "clean" one using a
   deterministic skip-list. If none is clean, fall back to the slash-command invocation
   parsed from a `<command-name>` message. Produce two values: the full intent and a
   single-line head-truncated (~200 char) version.
2. **Add the two fields to the canonical session schema** (`AgentSession`) and bump
   `model.SchemaVersion`.
3. **Mirror the fields in `auto-search`**: `ParquetSessionRow` (read), `sessions` DDL +
   `InsertSession` + `insertSessionFromParquet` (index), bump `indexdb.SchemaVersion` to
   force a rebuild.
4. **Expose in queries/CLI**: add the truncated intent to `SessionListRow` (the
   JSON-only `session list` payload) and the full intent to `session describe` (via
   `SessionRow`/`GetSessionByID`, added to its session map literal).
5. **Roll out**: `autoetl transform --full` to repopulate parquet, then `autosearch index`
   (auto-rebuilds on the schema-version bump).

### Intent heuristic (the core logic)

```go
// In auto-etl/internal/transform/transform.go

// junkPrefixes are user-message content prefixes that are not real intent:
// slash-command caveats/wrappers and harness-injected blocks.
var junkPrefixes = []string{
    "<local-command-caveat>", "<command-name>", "<command-message>",
    "<local-command-stdout>", "<system-reminder>", "[Request interrupted",
}

// firstUserIntent returns (full, truncated) intent for a session.
// Walks user messages in order; returns the first whose content is real prose.
// Falls back to the slash-command invocation (e.g. "/execute-task 014") when no
// prose exists but a <command-name> message is present. Returns ("","") otherwise.
func firstUserIntent(messages []model.AgentMessage, maxChars int) (full, truncated string) {
    var firstCommand string
    for i := range messages {
        if messages[i].Role != string(model.RoleUser) {
            continue
        }
        c := strings.TrimSpace(messages[i].Content)
        if c == "" {
            continue
        }
        if firstCommand == "" {
            if cmd := parseSlashCommand(c); cmd != "" {
                firstCommand = cmd // remember for fallback
            }
        }
        if isJunkIntent(c) {
            continue
        }
        full = c
        truncated = headTruncate(collapseWhitespace(c), maxChars)
        return full, truncated
    }
    // No clean prose — fall back to the slash command if we saw one.
    if firstCommand != "" {
        return firstCommand, firstCommand
    }
    return "", ""
}
```

- `isJunkIntent(c)` → true if `c` has any `junkPrefixes` prefix (case-sensitive; these are
  literal harness strings).
- `parseSlashCommand(c)` → extracts `<command-name>` (+ `<command-args>` when non-empty),
  yielding e.g. `/execute-task 014`. Simple string slicing on the known tags; no XML parser.
- `collapseWhitespace` → `strings.Join(strings.Fields(c), " ")` to make the preview single-line.
- `headTruncate(s, n)` → first `n` **runes** + `…` if longer (NOT `MidTruncate`, which cuts
  the middle — wrong for a leading preview). Cut on a rune boundary via `r := []rune(s); if
  len(r) > n { return string(r[:n]) + "…" }` so a multibyte rune is never split.
  `n = IntentTruncateMaxChars = 200` runes.

<!-- RESOLVED(P3): head-truncate at a byte boundary can split a multibyte rune
REVIEW: "first n chars" via `s[:n]` (the MidTruncate/truncateStr precedent, transform.go:518
& session.go:382) slices bytes, not runes. Intent is user prose — more likely than
transcripts to contain non-ASCII/emoji — so a 200-byte cut can land mid-rune and emit a
broken UTF-8 sequence (rendered as U+FFFD once JSON-encoded). Low impact, but since this is a
short user-facing preview, consider truncating on a rune boundary (`[]rune(s)[:n]` or
`utf8.RuneStart` back-off). Flagging because the helper is new; if you intend to match the
existing byte-slice behavior, note that explicitly.
AUTHOR: Adopted rune-boundary truncation — `headTruncate` now cuts on `[]rune(s)[:n]` (200
runes), so non-ASCII/emoji intents never split mid-rune. Added a unit-test case (Step 1.4) for
a multibyte-prose intent. Deliberately does NOT match MidTruncate's byte behavior.
-->


Validated against the 1,039-session local dataset: recovers clean intent for 1,022 (98.4%);
the remaining handful have no clean user message and correctly yield empty/command-fallback.

**Confirmed design decisions** (from planning Q&A):
- *Subagents*: intent is populated for **all** sessions, including `is_subagent=true`. A
  subagent's first user message is its dispatch prompt (clean prose), so the helper applies
  uniformly — no `is_subagent` gating. Useful when listing with `--subagent`.
- *Searchability*: **display-only** — intent is NOT added to `sessions_fts` (the transcript
  is already searchable). See Out of Scope.
- *Slash fallback*: include **command + args** (`/execute-task 014`), not just the bare name.

## Files

```
~ auto-etl/internal/model/model.go
    # + AgentSession.FirstUserIntent          `parquet:"first_user_intent"`
    # + AgentSession.FirstUserIntentTruncated  `parquet:"first_user_intent_truncated"`
    # + const IntentTruncateMaxChars = 200
    # bump SchemaVersion 4 -> 5

~ auto-etl/internal/transform/transform.go
    # + firstUserIntent/isJunkIntent/parseSlashCommand/headTruncate/collapseWhitespace helpers
    # + junkPrefixes var
    # wire into transformSession: populate the two new AgentSession fields

~ auto-search/internal/model/parquet.go
    # + ParquetSessionRow.FirstUserIntent / .FirstUserIntentTruncated (matching parquet tags)

~ auto-search/internal/indexdb/schema.go
    # + sessions columns: first_user_intent TEXT NOT NULL DEFAULT '',
    #                      first_user_intent_truncated TEXT NOT NULL DEFAULT ''
    # bump SchemaVersion 7 -> 8 (forces full rebuild)

~ auto-search/internal/indexdb/sessions.go
    # InsertSession: + two params, + two INSERT columns/placeholders

~ auto-search/internal/indexdb/indexer.go
    # insertSessionFromParquet: pass r.FirstUserIntent, r.FirstUserIntentTruncated

~ auto-search/internal/indexdb/query_sessions.go
    # SessionListRow: + FirstUserIntentTruncated string `json:"first_user_intent_truncated,omitempty"`
    # ListSessions: SELECT s.first_user_intent_truncated; add to Scan
    # SessionRow: + FirstUserIntent, FirstUserIntentTruncated
    # GetSessionByID: SELECT both; add to Scan

~ auto-search/internal/cli/session.go
    # CODE CHANGE REQUIRED in `session describe` (newSessionDescribeCmd, ~lines 232-305):
    # add "firstUserIntent": sess.FirstUserIntent to the session map[string]any literal
    # (~lines 271-294). `session get` is a transcript renderer (no JSON) — not touched.
    # `session list` needs no extra change: SessionListRow marshals directly (Step 2.5).
```

<!-- RESOLVED(P1): "get emits SessionRow JSON" is incorrect — code change IS required
REVIEW: I checked session.go. `session get` (lines 191-230) renders a message transcript
(SessionMessages → roleTag/messageContent text), not SessionRow and not JSON — so adding
FirstUserIntent to SessionRow surfaces NOWHERE in `get`. SessionRow/GetSessionByID is used
only by `session describe` (lines 232-305), and describe builds an explicit `map[string]any`
literal (lines 271-294) — it does NOT marshal SessionRow directly. So even for describe,
"no logic change" is wrong: you must add e.g. `"firstUserIntent": sess.FirstUserIntent` to
that map. The `session list` path is fine (it marshals SessionListRow directly), but the
full-intent surface needs a concrete decision: add it to `session describe`'s map, since
`get` has no JSON mode to attach it to. Please update this file entry and the Files-section
SessionRow note accordingly.
AUTHOR: Confirmed against session.go. Rewrote the session.go file entry: the full intent is
surfaced via a map-literal edit in `session describe` (`"firstUserIntent": sess.FirstUserIntent`),
not via `get`. `session get` left untouched. plan.md Steps 3.1/3.3 and the AC-5 test target
retargeted to `describe`.
-->


Naming note: `session list` is JSON-only. It carries `first_user_intent_truncated` (the
truncated value); the full untruncated intent is exposed only via `session describe`
(`firstUserIntent` key, from `SessionRow`). Distinct keys avoid one name meaning two things.

<!-- RESOLVED(P2): same JSON key `first_user_intent` would mean two different things
REVIEW: As designed, `session list` emits `first_user_intent` = the *truncated* value
(SessionListRow), while `session get`/describe would emit `first_user_intent` = the *full*
value. A consumer parsing `first_user_intent` across the two commands gets truncated vs full
under one key, which is a silent footgun. Consider naming the list field
`first_user_intent_truncated` (matching its column and value), and reserving
`first_user_intent` for the full value — or otherwise documenting the divergence explicitly.
AUTHOR: Adopted. `session list` field renamed to `first_user_intent_truncated` (snake_case,
matching the column + list convention); `session describe` uses `firstUserIntent` (camelCase,
matching describe's existing keys) for the full value. No shared key, no truncated-vs-full
ambiguity. Updated the Files section and the naming note above.
-->


## Test Coverage

| AC   | Test Type    | File                                                        |
|------|--------------|-------------------------------------------------------------|
| AC-1 | unit         | auto-etl/internal/transform/transform_test.go               |
| AC-2 | unit         | auto-etl/internal/transform/transform_test.go (table: caveat/command/reminder/empty → skipped) |
| AC-3 | unit         | auto-etl/internal/transform/transform_test.go (slash-only → `/execute-task 014`; none → empty) |
| AC-4 | integration  | auto-search/internal/indexdb/indexer_integration_test.go    |
| AC-5 | integration  | auto-search/internal/cli/cli_integration_test.go + testutil/fixtures.go (add intent to fixtures) |

Unit tests for the heuristic are the priority — they pin the skip-list and fallback against
representative content (caveat-then-prose, command-then-prose, system-reminder, empty,
slash-only, prose-first). Fixtures in `auto-search/internal/testutil/fixtures.go` need the
new columns populated so the index/CLI integration tests can assert the field round-trips.

## Out of Scope

- LLM/semantic summarization or re-summarizing the session — deterministic skip-list only.
- Backfill without re-running ETL — existing parquet needs `autoetl transform --full`.
- Tuning the junk skip-list beyond the investigated patterns.
- FTS indexing of the intent field (not added to `sessions_fts`); intent is for display, and
  the transcript is already searchable. Can be added later if search-by-intent is wanted.
- A `--text`/table output mode for `session list` (it is JSON-only today; unchanged here).

## Rejected Alternatives

- **Compute in auto-search at index time** (from the messages table): avoids the parquet
  schema bump and allows heuristic iteration via index rebuild, but puts a derived field
  outside the canonical dataset and requires a message-scan join at session-index time.
  Rejected per the confirmed decision to keep intent canonical (matches `transcript_truncated`).
- **Use the literal first user message**: simplest, but junk ~24% of the time (caveats/wrappers),
  so it fails the "understand intent at a glance" goal.
- **`MidTruncate` for the preview**: reuses the existing helper but cuts the middle out of the
  sentence; a leading preview needs head truncation.
- **LLM-generated summary**: higher quality phrasing, but adds cost, nondeterminism, and a
  model dependency to a batch ETL step — out of scope.
