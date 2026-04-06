# Auto-Reflect V2: Feedback And Learning Loop

## Purpose

This note captures later-stage `auto-reflect` ideas for turning rule lookup into a real learning loop.

V1 can be useful with basic rule retrieval.

V2 should add the missing loop:

1. retrieve rules before work,
2. capture whether those rules helped or hurt,
3. fold that feedback back into ranking and curation,
4. preserve provenance so the system can explain why a rule is trusted.

## Background

CMS (`.tmp/cass_memory_system`) supports this with three separate paths:

- explicit feedback commands
- inline feedback comments embedded in session content
- implicit outcome-based scoring

For `auto-reflect`, we probably want the same broad shape, but built on top of:

- `auto-etl` canonical session data
- `auto-search` indexes
- `auto-reflect` rule and feedback state

## Proposed V2 Surfaces

### 1. Explicit feedback command

The basic operator and agent surface should be an explicit command:

```bash
autoreflect feedback <rule-id> --helpful
autoreflect feedback <rule-id> --harmful --reason-code outdated
autoreflect feedback <rule-id> --helpful --note "matched repo conventions"
```

Design goals:

- deterministic
- easy for coding agents to call
- JSON by default
- append-only

The first version should require exactly one of:

- `--helpful`
- `--harmful`

Optional flags:

- `--reason-code <code>`
- `--note <text>`

Possible initial reason codes:

- helpful:
  - `saved_time`
  - `avoided_bug`
  - `matched_repo`
- harmful:
  - `outdated`
  - `wrong_scope`
  - `incorrect`
  - `caused_bug`

## Storage Model

Feedback should be stored as append-only events, not only as counters embedded in rule documents.

Example event:

```json
{
  "rule_id": "r-1a2b3c4d",
  "feedback": "helpful",
  "timestamp": "2026-03-21T12:34:56Z",
  "reason_code": "saved_time",
  "note": "pointed me to jwt expiry",
  "session_id": null,
  "agent": null
}
```

This gives us:

- auditability
- replayability
- future rescoring
- later provenance enrichment

Derived rule fields can then be computed from events:

- `helpful_count`
- `harmful_count`
- `last_feedback_at`
- `score`

## Session Provenance

### Why session id is useful

If a feedback event is tied to a canonical session id, we can:

- avoid double-counting the same session repeatedly
- inspect the originating transcript later
- join to ETL metadata such as repo, agent, model, and time
- explain why a rule became trusted or distrusted

### Why not require it in the CLI at first

We likely do not need to force agents to pass `--session-id` in the first implementation.

The user interaction cost is high, and a lot of provenance can be reconstructed later from ETL data if the feedback command itself was issued inside a captured coding session.

### Recommended compromise

For early versions:

- do not require `--session-id`
- store the event immediately anyway
- run later enrichment that tries to attach:
  - `session_id`
  - `agent`
  - `workspace`
  - `git_remote`

This keeps the UX simple without losing the path to stronger provenance.

## Risks Of Implicit-Only Provenance

If we rely only on ETL reconstruction from tool calls later, we introduce several problems:

- shell command parsing is brittle
- aliasing, wrappers, and future MCP/API use make matching less reliable
- duplicate calls inside one session are harder to deduplicate immediately
- provenance only becomes available after ETL catches up
- some feedback may come from contexts not captured as coding sessions

So the recommended design is:

- explicit event logging at command time
- later ETL-based provenance enrichment

Not:

- "derive the entire event later from transcripts"

## 2. Inline feedback markers

Once the basic explicit command exists, a later agent-native path should be inline feedback markers embedded in code or transcript-visible comments.

Example shape:

```ts
// [autoreflect: helpful r-1a2b3c4d] - saved debugging time
// [autoreflect: harmful r-1a2b3c4d] - outdated in this repo
```

These markers should be parsed from transcript content during later reflection runs.

Benefits:

- very low friction for coding agents
- feedback stays close to the work that prompted it
- useful in long coding sessions where explicit command calls feel heavy

This should be a V2 feature, not a V1 requirement.

## 3. Implicit outcome scoring

A later-stage learning loop should support inferred feedback from session outcomes.

This could come from signals such as:

- task succeeded or failed
- retries were needed
- repeated errors occurred
- user sentiment was positive or negative
- large amounts of churn happened before convergence

That should not replace explicit feedback. It should be a weaker, weighted signal layered on top.

Example:

- successful session with low churn: mild helpful signal
- failed session with repeated retries: mild harmful signal

This is probably a V2 or V3 feature, after the event model is stable.

## How This Connects To Lookup

The planned low-level retrieval primitive is:

```bash
autoreflect lookup "<query>"
```

Once feedback exists, `lookup` should return trust signals with each rule.

Example shape:

```json
{
  "query": "auth rate limit jwt",
  "keywords": ["auth", "rate", "limit", "jwt"],
  "scope": "repo",
  "rules": [
    {
      "id": "r-1a2b3c4d",
      "content": "Check JWT expiry before debugging auth failures",
      "category": "security",
      "match_score": 0.82,
      "helpful_count": 4,
      "harmful_count": 1,
      "last_feedback_at": "2026-03-20T18:22:11Z"
    }
  ]
}
```

This is the simplest path from:

- explicit rule creation and retrieval

to:

- retrieval informed by real usage

## Suggested Rollout

### Stage 1

- `autoreflect rule create --content ... --category ...`
- `autoreflect lookup "<query>"`
- explicit playbook creation and retrieval
- no feedback loop yet

### Stage 2

- `autoreflect feedback <rule-id> --helpful|--harmful`
- append-only feedback event log
- `lookup` returns aggregated counts

### Stage 3

- ETL-driven provenance enrichment of feedback events
- deduplication by canonical session id where possible
- rule ranking starts to incorporate feedback score

### Stage 4

- inline feedback marker parsing during reflection
- optional explicit `context` command that combines:
  - matching rules
  - historical session snippets
  - trust and provenance

### Stage 5

- weighted implicit outcome scoring
- stronger rule promotion, demotion, or deprecation logic

## Recommendation

The important bit to capture is this:

`rule create` and `lookup` are not enough.

If `auto-reflect` is meant to improve over time, it needs at least one low-friction feedback path on top of explicit rule creation and retrieval. The best minimal version is an explicit append-only `feedback` command. After that, inline markers and implicit outcomes become natural later-stage additions.

## Related Notes

- `auto-reflect/docs/cass-memory-system-analysis.md`
- `docs/user-journey.md`
- `auto-reflect/CLAUDE.md`
