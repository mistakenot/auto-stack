---
hash: "983d19ba"
id: "bc691476"
read_when: "planning or sequencing sub-tasks for the reflect playbook loop epic"
summary: "Epic plan for making the auto-reflect playbook loop usable by autonomous reflection agents: fix existing gaps first (session identity, lifecycle, observation capture, consolidation, signal readers, doc drift), then wire agent runs, then grow the improvement loop. Centered on a two-step Observe → Consolidate pipeline."
title: "Epic: Reflect Playbook Loop — Observe, Consolidate, Wire, Grow"
---

# Epic: Reflect Playbook Loop — Observe, Consolidate, Wire, Grow

## Status (updated 2026-06-11)

**Phase 1 is complete and on `main`.** All blocker sub-tasks shipped:

- 1.1 session identity — PR #66
- 1.2–1.6 (lifecycle retrieval, observation capture, consolidation + promote/retire, reader
  API, doc sync) — PR #68 (squash `4009d3c`), incl. review hardening of the `merge` op and
  `rule promote`.
- **Only Phase 1 remainder:** cut a release so the installed `auto` binary picks up the new
  surface (user-triggered/billed — see 1.6).

**Validated end-to-end (informal dogfood, 2026-06-11).** Ran the full pipeline by hand as the
reflecting engineer: a 5-agent discovery team mined this repo's 625 indexed sessions → 19
evidence-linked observations → `consolidate` → 16 draft rules → promoted 4. Proved the
observe → consolidate → promote → retrieve loop works on real data. (Output lives on the
throwaway `experiment/reflect-dogfood` branch, intentionally not on main.) This is a manual
preview of 2.1/2.2 — the *skills* that automate it are not built yet.

**Eval design captured** in [`auto-eval/docs/evaluating-playbook-rule-utility.md`](../../auto-eval/docs/evaluating-playbook-rule-utility.md): how to test
whether playbook rules actually improve runs (the Phase-3 utility question). Groundwork, not
yet built.

**Next up:** Phase 2 — turn the manual dogfood into repeatable skills (2.1 mining, 2.2
consolidation), add a human review/promotion pass (2.3), and wire `retrieve`/`gate` into live
sessions (2.4) so the loop generates its own telemetry. Phase 3 stays dormant until volume.

## Goal

Set up agent runs that reflect over past coding sessions (via `auto search`) and record
what they find into the repository playbook (via `auto reflect`), so the playbook grows
from real session evidence rather than hand-authoring.

This epic sequences the sub-tasks needed to get there. **Ordering principle: fix existing
problems and gaps first, then wire up agent runs, then add new loop functionality.** Later
phases of the V2 design stay dormant until the event log proves there is enough data volume
to feed them (see minimum-data preconditions in the design doc).

## Core architecture decision: Observe → Consolidate (two steps, not one)

Mining output does **not** become rules directly. The pipeline has two distinct steps with
different owners and different quality bars:

1. **Observe** — agents record *observations*: situated, evidence-linked findings
   (a correction, a recurring failure, a gap, an incident). Low bar, cheap to write,
   append-only, idempotent across re-runs. No generalization required at capture time.
2. **Consolidate** — a later pass clusters observations, and only when a cluster crosses
   an evidence threshold (≥ 2–3 distinct sessions) does it draft a rule — carrying the
   observation IDs as provenance. LLM judgment proposes; deterministic CLI gates
   (thresholds, dedupe, conflict checks) decide what persists.

Why (synthesized from [`cass-memory-system-analysis.md`](../../auto-reflect/docs/cass-memory-system-analysis.md),
[`decision-mining.md`](../../auto-reflect/docs/decision-mining.md),
[`research-april-2026.md`](../../auto-reflect/docs/research-april-2026.md)):

- **The middle memory tier is what's missing.** CMS's shape is episodic (raw logs) →
  working (diary/observations) → procedural (rules). Auto-stack has the episodic tier
  (ETL parquet) and the procedural tier (playbook); the v1 annotation surface
  (`feedback add --kind ...`) *was* the working tier and was removed in the loop rework.
- **Capture is cheap; generalization is hard.** Scope calibration (over-specific vs
  over-general) needs multiple examples. Single-shot `rule create` locks in an abstraction
  level chosen from one anecdote; a consolidator looking at 3 observations with shared
  context picks the right altitude.
- **Evidence thresholds become structural.** "Frequency ≥ 2 distinct sessions before
  promoting" stops being a policed convention and becomes the consolidation trigger.
- **LLM extracts, deterministic logic promotes.** CMS ("NO LLM in this stage — pure
  deterministic logic to prevent context collapse"), ACE, and AutoManual all converge on
  this split. Consolidation output is a reviewable delta (add / merge / deprecate /
  attach-evidence), never a silent playbook write.
- **Idempotent mining.** Re-observing the same session dedupes by session+subject;
  direct rule creation makes every mining re-run a near-duplicate-rule generator.

Known risks, with mitigations baked into the sub-tasks below:

- *Latency to effect* — run consolidation frequently (cheap at this scale); severity
  bypass lets a single catastrophic incident become a rule immediately.
- *Write-only graveyard* — `stats`/`doctor` must surface the unconsolidated-observation
  backlog so rot is visible.
- *Over-engineering* — observations are a new **event type on the existing event-sourced
  log** (sharded JSONL, fold, readers all exist), not a new store.

## Background

- `auto search` is in good shape for the mining side — no blockers there.
- `auto reflect` has implemented step 1 of the V2 design (event-sourced rule store +
  retrieve → select → feedback → gate loop + basic stats), but a gap analysis (2026-06-10)
  found blockers that make the loop unusable for autonomous runs today.
- Design references:
  - [`auto-reflect/docs/self-improving-playbook-retrieval.md`](../../auto-reflect/docs/self-improving-playbook-retrieval.md) (V2 design + reviewer notes)
  - [`auto-reflect/docs/decision-mining.md`](../../auto-reflect/docs/decision-mining.md) (mining pipeline)
  - [`auto-reflect/docs/cass-memory-system-analysis.md`](../../auto-reflect/docs/cass-memory-system-analysis.md) (three-tier memory, delta model,
    deterministic curation)
  - [`auto-reflect/docs/trauma-candidate-promotion-pattern.md`](../../auto-reflect/docs/trauma-candidate-promotion-pattern.md) (discover → promote boundary)
  - [`auto-reflect/docs/research-april-2026.md`](../../auto-reflect/docs/research-april-2026.md) (evidence thresholds, lifecycle discipline)

## Current gaps (found 2026-06-10)

1. **Session-ID detection mismatch.** *(fixed in PR #66 — see 1.1)* `internal/events/session.go`
   looked for `CLAUDE_SESSION_ID`; Claude Code actually exports `CLAUDE_CODE_SESSION_ID`.
   Detection always returned empty in real sessions, so events could never be joined to
   ETL'd transcripts and the gate fell back to host+worktree+24h scoping.
2. **Lifecycle is cosmetic.** `rule create` defaults to `draft`, but retrieval
   (`internal/rules/match.go`) never consumes lifecycle — draft and stale rules surface
   identically to confirmed ones. There is no discover → promote boundary.
3. **No working-memory tier.** The v1 annotation surface
   (`feedback add --kind helpful|harmful|missing`) was removed in the loop rework; the
   only write surface left is `rule create`, which lands directly in the live playbook —
   observations get turned into rules straight away, with no provenance.
4. **No reader API over loop signal.** Gap reports, rankings + reasons, and outcomes are
   only reachable by hand-parsing sharded JSONL under `.auto/reflect/events/`.
5. **Stale binary + quickstart drift.** The installed release predates the loop; its
   quickstart teaches removed commands (`lookup`, `feedback add`). For autonomous runs
   the quickstart is the API contract.
6. **No harness wiring.** Nothing invokes `retrieve` at session start or `gate check` at
   completion, so the loop collects zero telemetry. `.auto/reflect/` in this repo is empty.

## Phase 1 — Fix existing problems (blockers)

Sequenced; each sub-task unblocks the ones after it.

### 1.1 Fix session identity detection — ✅ done (PR #66, 2026-06-10)

Detect `CLAUDE_CODE_SESSION_ID` (keep existing keys for codex/manual override; document
precedence). Verify the detected ID matches the session UUID that `auto etl` / `auto search`
index, so reflect events join to transcripts. Smallest task, unblocks everything downstream.

Shipped: `CLAUDE_CODE_SESSION_ID` appended to the detection precedence list
(`AUTO_SESSION_ID` stays the manual override), new `session_test.go` coverage, and the
gate-fallback test now clears the new key so it doesn't pick up the real session ID when
the suite runs inside Claude Code.

### 1.2 Make retrieval lifecycle-aware — ✅ done (merged to main, PR #68)

`retrieve` excludes `stale` rules and either excludes or explicitly flags `draft` rules
(flagged is preferred: implementers can opt in, and drafts need exposure to graduate).
Decide and document the default. This turns `draft` into a real candidate state.

### 1.3 Observation capture (the Observe step) — ✅ done (merged to main, PR #68)

Restore the working-memory tier as a new `observation` event type on the existing event
log, plus a write surface (`auto reflect observe` or `observation add`) and a reader
(`observation list --kind --domain --since --unconsolidated`). Schema sketch:

```
{ kind: correction|pattern|gap|incident,
  subject: short statement of what was observed,
  evidence: [{ session_id, message_id?, quote }],
  context: what the agent/user was doing,
  suggested_generalization: optional free text (not a rule),
  domain: tags, severity: normal|high }
```

Design constraints: evidence-linked (≥ 1 session ID required), judgment-light at capture,
deduped by session+subject so re-mining is idempotent. Phase-4a gap reports should be
recorded as (or trivially mappable to) observations of kind `gap`, unifying the Stage-3
gap-to-rule input with mining output.

### 1.4 Consolidation → rules (the Consolidate step) — ✅ done (merged to main, PR #68)

`auto reflect consolidate` turns clustered observations into playbook changes. The CLI
side is deterministic: evidence threshold (≥ 2 distinct sessions; `--force` /
`severity: high` bypass for incident-class lessons), dedupe against existing rules (via
`retrieve` with the candidate `use_when`), and conflict candidates flagged for review.
The LLM side (clustering judgment, drafting `use_when`/`content`/`causal_note`) lives in
the calling skill and proposes **deltas** (create-draft / attach-evidence-to-existing /
merge / deprecate) rather than writing directly. Rules created here carry
`observation_ids` as provenance — this is where rule provenance lands; the folded
projection and `rule get` expose it. Add explicit `rule promote <r-id>` /
`rule retire <r-id>` verbs; promote refuses (without `--force`) when the provenance chain
covers < 2 distinct sessions.

### 1.5 Reader API over the event log — ✅ done (merged to main, PR #68)

Expose loop signal without hand-parsing JSONL:

- `events list --type <t> --since <d>` — raw event access (resource-oriented, JSON default)
- observation backlog: unconsolidated count surfaced in `stats` (and `doctor` when it
  exists) so the write-only-graveyard risk is visible
- richer `stats`: per-rule rank distribution and outcome counts alongside
  surfaced/selected/selection_rate

### 1.6 Release + doc sync — ◑ doc sync done (PR #68); release tag pending (user-triggered)

Cut a release so the installed binary matches source; regenerate quickstarts; sweep
root `CLAUDE.md` and any docs referencing removed commands (`lookup`,
`feedback add --kind ...`). Acceptance: an agent following only `auto reflect quickstart`
can run observe → consolidate → retrieve → feedback against the released binary.

## Phase 2 — Wire up agent runs

Depends on Phase 1. This is where the epic's goal is first achieved end to end.

### 2.1 Mining skill (sessions → observations)

A skill/prompt that runs the decision-mining pipeline: refresh data
(`auto etl run` → `auto search index`), mine sessions for recurring patterns
(corrections, repeated failures, gaps), and record **observations** with session-ID
evidence — not rules. Run manually several times to shake out the workflow before any
scheduling. (Builds on the existing `reflect-on-agent-sessions` skill, which produces
reports but writes nothing.)

### 2.2 Consolidation skill (observations → draft rules)

A skill that reads the unconsolidated backlog, clusters it, and drives
`auto reflect consolidate` to propose deltas — drafting rules only where the evidence
threshold is met. Output is a small reviewable set of draft rules + attached evidence.
Can run in the same scheduled job as 2.1 or on its own cadence.

### 2.3 Human review + promotion pass

A lightweight recurring ritual (or skill) that lists draft rules with their observation
provenance and promotes/retires them. Keeps the human on the promote side of the boundary
while volume is low.

### 2.4 Harness wiring for the live loop

Wire `retrieve`/`select` into session start (skill or hook) and `gate check` into
completion (e.g. `complete-task` / Stop hook) so implementer sessions start generating
retrieval/selection/feedback telemetry. Only worth doing once promoted rules exist to
retrieve (after 2.1–2.3 have produced some).

### 2.5 Scheduled reflection runs

Once 2.1/2.2 are reliable manually, schedule them (cron / `auto watch` trigger after new
ETL data). Include an ETL-freshness check so the run knows the index covers the sessions
it reflects over.

## Phase 3 — Grow the loop (new functionality, data-gated)

From the V2 design's implementation sequence; each stage declares a minimum-data
precondition and stays dormant below it. Sequence within this phase per the design doc:

1. **Phase-5 reviewer** — fresh-agent compliance check from transcripts
   (`auto search session get`); the highest-value upgrade after the basics. Requires 1.1.
2. **Signal aggregation** — `compliance_rate`, `expectation_gap`, `missed_rate` folded
   from the event log; triage stays manual at first.
3. **Outcome instrumentation** — the Layer-1 objective composite from [`docs/signals.md`](../signals.md)
   + `auto search` primitives.
4. **Scoring and decay** — asymmetric helpful/harmful scoring (harmful ×4) and
   confidence decay with revalidation (90-day half-life), per the CMS analysis; feeds
   automatic deprecation and anti-pattern inversion.
5. **Probe injection, automated triage, contrastive loop, ablation calibration** —
   explicitly deferred; revisit when the event log shows sufficient volume per the
   minimum-data preconditions in the design doc.

Also deferred to this phase (build only when need is demonstrated):

- Global cross-repo scope (`~/.auto/reflect/events/`)
- Rule conflict detection beyond consolidation-time flagging
- Smarter matching (only once `missed_rate` exists to measure whether keyword overlap
  is actually failing)
- `why <rule-id>` provenance explainer (cheap once observation provenance exists;
  the CMS access-pattern study rates it critical for trust)

## Out of scope

- Replacing the keyword matcher with embeddings/BM25 ahead of evidence it's needed
- A second observation store outside the event log (observations are events)
- Multi-agent population broadcast, hard-rule structural enforcement beyond `gate check`
- CMS's full command surface (context/similar/top/stale/onboard/trauma/...) — borrow
  access patterns one at a time as need is demonstrated

## Sub-task index

| # | Sub-task | Depends on | Status |
|---|----------|------------|--------|
| 1.1 | Fix session identity detection | — | ✅ done (PR #66) |
| 1.2 | Lifecycle-aware retrieval | — | ✅ done (#68) |
| 1.3 | Observation capture (Observe) | — | ✅ done (#68) |
| 1.4 | Consolidation → rules (Consolidate) | 1.2, 1.3 | ✅ done (#68) |
| 1.5 | Reader API over event log | 1.3 | ✅ done (#68) |
| 1.6 | Release + doc sync | 1.1–1.5 | ◑ docs done; release pending |
| 2.1 | Mining skill (sessions → observations) | 1.3, 1.6 | |
| 2.2 | Consolidation skill (observations → draft rules) | 1.4, 2.1 | |
| 2.3 | Review + promotion pass | 2.2 | |
| 2.4 | Harness wiring for live loop | 1.1, 1.6, 2.3 | |
| 2.5 | Scheduled reflection runs | 2.1, 2.2 | |
| 3.x | Loop growth (reviewer, aggregation, outcomes, scoring, …) | Phase 2 + data volume | |
