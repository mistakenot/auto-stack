# Task 019: playbook-retrieval-loop

Distilled from `auto-reflect/docs/self-improving-playbook-retrieval.md` (v2 design). This task is
**steps 1–2 of that doc's implementation sequence**: enriched rule schema + event-sourced log + the
basic two-phase retrieval/feedback loop + gap detection. Everything else in the design (probes,
fresh reviewer, A/B, triage automation, contrastive loop, ablation) is deliberately deferred — but
the event log must capture enough signal that those stages can later fold over data collected
starting now.

## Problem

`auto reflect` today stores flat `{content, category, tags}` rules with one-shot keyword lookup. It
captures no signal about which rules were retrieved, selected, applied, or missing — so rules can
never improve. The v2 design defines a self-improving loop, but it needs a minimal end-to-end
increment running in real sessions to start accumulating the event data every later stage depends on.

## Goals

- Replace the rule schema with the v2 shape: `{id, domain[], use_when, content, causal_note, rule_type (hard|soft), lifecycle, version}` (no `confidence` — reviewer note #5: defined but never consumed).
- Persist all state as append-only JSONL events sharded to avoid concurrent-write collisions; the rule snapshot is a derived, rebuildable projection. Events are canonical.
- Implement the two-phase retrieval loop as CLI commands an agent can drive:
  1. `retrieve <intent>` → domain filter + keyword match over `use_when`; returns `[{use_when, retrieval_id, domain, rule_type}]`, content withheld. Domain-matched hard rules always included.
  2. `select <retrieval_ids…>` (interest-ordered) → `[{content, causal_note, feedback_id, rule_type}]`. (Named `select`, not `get`, so the loop verb stays distinct from the `rule get` resource command.)
  3. A feedback command that closes the loop: grounded gap report + task outcome + the implementer's usefulness ranking (recorded as known-biased event data for later study), validating all outstanding `feedback_ids` are accounted for.
  4. `gate check` — exits non-zero while feedback is outstanding for the session, so skills/hooks (e.g. complete-task) can enforce the gate without hard coupling.
- Capture events for: rule create/edit, retrieval, selection (with order), feedback (gap + outcome + ranking). Rule edits are incremental versioned deltas, never rewrites.
- Provide a minimal read surface over the captured signal (e.g. `stats`: per-rule surfaced/selected counts, `selection_rate`) so we can verify real-world data is flowing — analysis stays manual.
- Update `quickstart` so an agent can run the whole loop from the doc string alone.

## Acceptance Criteria

**AC-1**: Two-phase retrieval
- Given: a playbook with rules whose `use_when`/`domain` match an intent
- When: agent runs `auto reflect retrieve "<intent>"`
- Then: JSON list of `{use_when, retrieval_id, domain, rule_type}` — no `content`; matching hard rules present regardless of match score; a `retrieval` event is appended

**AC-2**: Content fetch preserves order and links feedback
- Given: a prior `retrieve` returning retrieval_ids
- When: agent runs `select` with ids sorted most-interesting-first
- Then: `{content, causal_note, feedback_id, rule_type}` returned in the same order; a `selection` event records the ordering

**AC-3**: Feedback gate completeness
- Given: a session with outstanding `feedback_ids`
- When: agent submits feedback missing some ids, or a gap report with no session-moment grounding
- Then: command exits non-zero with a structured error naming the missing pieces and remediation; only a complete submission (per-id usefulness ranking + grounded gap-or-none + outcome `success|partial|fail|abandoned`) appends a feedback event and reports the gate closed

**AC-3b**: Checkable gate
- Given: a session with consumed rules and no feedback submitted
- When: `auto reflect gate check` runs
- Then: exits non-zero with the outstanding `feedback_ids` and the exact remediation command; after a complete feedback submission, exits zero (a session with no rules consumed also passes)

**AC-4**: Events canonical, snapshot derived
- Given: an events directory and a deleted/corrupted rule snapshot
- When: any command runs (or an explicit rebuild is invoked)
- Then: snapshot is rebuilt by folding the event log to identical state; events carry `type`, `schema_version`, timestamp, monotonic sequence; two worktrees on the same host on the same day write to *different* shard files (sharded by host+day+worktree-path-hash), so merging their branches never conflicts on an event-file path

<!-- RESOLVED(P1): "concurrent appends from two worktrees never write the same file (sharded by host+day)" is unverifiable for the primary use case
REVIEW: See the detailed comment in solution.md §1. Two worktrees on the SAME host on the SAME day
resolve to the identical shard name `<host>-<date>.jsonl`. On disk they're separate checkouts so no
corruption, but on git-merge-back their divergent tails conflict — which is half the reason for
sharding (design doc line 370). This AC is the testable contract; as written ("sharded by host+day")
it cannot be satisfied for same-host/same-day parallel worktrees. Either the shard key must include
a session/worktree discriminator, or this AC must be narrowed (e.g. "concurrent appends never
corrupt on disk" and explicitly accept merge conflicts for same-host/same-day worktrees).
AUTHOR: Shard key changed to host+day+worktree (`<host>-<YYYY-MM-DD>-<wt8>.jsonl`, wt8 =
SHA-256(worktree root path)[:8]) — see solution.md §1 thread for the rationale vs a session-keyed
shard. AC reworded into the now-testable contract: same host + same day + two worktrees → two files,
merge never conflicts on an event path.
-->


**AC-5**: Versioned incremental edits
- Given: an existing rule
- When: one or more fields are edited via a single CLI invocation
- Then: ONE `rule_edited` event records `{from_version, to_version, deltas: [{field, old, new}]}` (one version bump per edit, however many fields); full history of the rule is reconstructable from events

**AC-6**: Signal visible
- Given: ≥2 sessions of loop events
- When: `auto reflect stats` runs
- Then: per-rule surfaced count, selected count, and `selection_rate` are returned as JSON

**AC-7**: E2E usable by an agent
- Given: a fresh repo with `auto reflect init --project`
- When: an agent follows `auto reflect quickstart` only
- Then: it can create a rule, run retrieve → select → feedback end-to-end, and events land on disk (E2E test harness exercises this as a user would)

## Out of Scope

- Probe injection, fresh-agent Phase 5 reviewer, signal-driven triage, A/B versions, contrastive loop, ablation calibration, cluster summaries (>30 matches), population broadcast — all deferred until event volume exists.
- `confidence` field and confidence decay.
- Automated hard-rule *enforcement* (compliance checking); v1 only guarantees hard rules are always surfaced.
- BM25/embedding retrieval quality work — simple keyword/domain matching is fine; retrieval is explicitly not the thing being optimized.
- Acting on gap reports (gap-to-rule pipeline); v1 only captures them.
- Migrating existing rules — old schema and commands are deleted, not converted.
- Global scope (`~/.auto/reflect/`) for rules/events.
- Stop-hook/skill wiring of the gate — v1 ships the checkable command only; consumers integrate it.

## Open Questions

- [x] Q1: Migration — replace the existing `rule create`/`lookup` schema and commands outright? (answered: replace outright, breaking change; existing functionality is barely used — fine to delete/rebuild. No migration of existing rules.)
- [x] Q2: Gate enforcement in v1? (answered: checkable `gate check` command that exits non-zero while feedback is outstanding; skills/hooks wire it up themselves. Stop-hook integration deferred.)
- [x] Q3: Feedback payload? (answered: gap + outcome **plus** the implementer's usefulness ranking, recorded as known-biased event data — append-only log means it can be studied or discarded later, e.g. to measure the bias empirically once Phase 5 exists.)
- [x] Q4: Scope? (answered: project-local only — `.auto/reflect/` committed to git. Global `~/.auto/reflect/` deferred.)
- [x] Q5 (raised during solution design): What happens to the legacy `feedback add/list` annotation system (helpful/harmful/missing file spans, `feedback.jsonl`)? (answered: delete it too — full rebuild; the `feedback` noun is reused for the loop's gate submission. Gap reports + outcome cover the "missing" use case; span/git-provenance code is retained where reusable.)
