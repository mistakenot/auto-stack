# Task 015: session-intent-summary

## Problem

`autosearch session list` summarizes each session (timing, tokens, counts) but gives no
indication of *what the session was trying to do*. A reader cannot tell sessions apart at
a glance. The session's intent is captured in the first real user message, but that message
is junk ~24% of the time (slash-command caveats and wrappers), so the raw first message
cannot be used directly.

## Goals

- Surface a human-readable "intent" for each session — the first *real* user message —
  in `autosearch session list` and `session describe`.
- Store the intent as a derived field on the canonical `auto-etl` session record, computed
  once during transform (same precedent as `transcript_truncated`).
- Skip junk first-messages (command caveats, wrappers, empty/special-char content) using a
  deterministic skip-list heuristic, recovering clean intent for ~98% of sessions.
- Fall back to the slash-command name for sessions whose only input is a slash command.

## Acceptance Criteria

**AC-1**: Canonical intent field on the session record
- Given: a raw session is transformed by `auto-etl`
- When: the transform builds the `AgentSession` parquet row
- Then: the row includes `first_user_intent` (full) and `first_user_intent_truncated`
  (~200 chars, single-line with newlines collapsed to spaces) derived fields, and the
  ETL schema version is bumped

**AC-2**: Junk first-messages are skipped
- Given: a session whose first user message(s) start with `<local-command-caveat>`,
  `<command-name>`, `<command-message>`, `<local-command-stdout>`, `<system-reminder>`,
  `[Request interrupted`, or are empty/whitespace-only
- When: the intent is computed
- Then: those messages are skipped and the first remaining clean user message becomes the
  intent (verified to recover intent for ~1022/1039 sessions in the local dataset)

**AC-3**: Slash-command fallback
- Given: a session with no clean prose user message but a `<command-name>` slash command
- When: the intent is computed
- Then: the intent is the slash-command invocation (e.g. `/execute-task 014`) rather than
  blank. If there is no clean message and no command, the intent is empty.

**AC-4**: Intent flows through the search index
- Given: `auto-search` indexes parquet session rows
- When: a session is inserted into the SQLite `sessions` table
- Then: both intent fields are stored, the index schema version is bumped (forcing rebuild),
  and the fields round-trip through `ParquetSessionRow` → DDL → `InsertSession` →
  `insertSessionFromParquet`

**AC-5**: Intent is shown in CLI output
- Given: a user runs `autosearch session list` (JSON output)
- When: output is rendered
- Then: each session carries `first_user_intent_truncated` (the truncated intent)
- And: `autosearch session describe <id>` exposes the full intent as a `firstUserIntent`
  key in its session JSON

<!-- RESOLVED(P1): `session get` is not a session-field JSON surface
REVIEW: I verified auto-search/internal/cli/session.go. `session list` has no "text mode" —
it is JSON-only (session.go:164-166, `enc.Encode(out)`); there is no table/text output, so
"text mode shows the truncated intent" misdescribes the surface. More importantly,
`session get` (newSessionGetCmd, session.go:191-230) renders a *message transcript* via
SessionMessages — it never loads a SessionRow / session-level fields and emits no JSON, so it
cannot "expose the full first_user_intent" as written. The only command that returns
session-level JSON (and uses GetSessionByID/SessionRow) is `session describe`
(session.go:232-305). This AC needs to target `session describe` for the full intent (and
`session list` JSON for the truncated value), or define a new JSON surface on `get`.
AUTHOR: Verified directly (session.go:191-230 `get`=transcript text; 232-305 `describe`=JSON
via GetSessionByID + map literal). Reworded AC-5: `session list` carries
`first_user_intent_truncated`; full intent is exposed via `session describe` as
`firstUserIntent`. Dropped the "text mode" phrasing (list is JSON-only). solution.md and
plan.md updated to match (describe map-literal edit + integration test retargeted to describe).
-->

## Out of Scope

- LLM/semantic summarization or re-summarizing the session — this is a deterministic
  skip-list heuristic over the first user message only.
- Backfilling intent without re-running ETL — existing parquet requires a `--full`
  re-transform to populate the new fields.
- Tuning the junk skip-list beyond the patterns identified in investigation.

## Open Questions

- [x] Where to compute/store the intent? (answered: in `auto-etl` transform, as a derived
  canonical session field — matches `transcript_truncated`)
- [x] Full vs truncated storage? (answered: store both — full field + ~200-char single-line
  truncated companion; list shows truncated, get/JSON shows full)
- [x] What to show for slash-command-only sessions? (answered: fall back to the
  `<command-name>` invocation rather than leaving blank)
