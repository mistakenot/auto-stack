---
hash: "d63dfafb"
id: "d0f44004"
read_when: "designing or implementing the auto-reflect playbook retrieval and self-improvement loop"
summary: "V2 design for auto-reflect's playbook retrieval loop: 5-phase task lifecycle, probe injection, fresh-reviewer reflection, and bidirectional feedback signal captured in an append-only event log."
title: "Self-Improving Playbook Retrieval System"
---

# Self-Improving Playbook Retrieval System (v2)

## Problem Statement

Coding agents need access to accumulated rules and heuristics (playbooks) during task execution. The retrieval problem — which rules to surface for a given task — is typically treated as an IR optimization problem. This design takes a different approach: instead of micro-optimizing retrieval, optimize the *rules themselves* via structured backpressure signal captured during every task.

The core insight: if every task produces bidirectional feedback (what the agent *expected* from a rule vs what it *got*), you can self-improve both `use_when` predicates and `content` over time without manual curation.

## V1 Design (Original)

Rules stored as `{use_when, content}` pairs. The agent-side loop:

1. Agent calls CLI with a search term that matches against `use_when` values
2. System returns NOT content, but all matching `use_when` values with a `retrieval_id`: `[{use_when, retrieval_id}]`
3. Agent compares predicates to its intent, decides which are most interesting
4. Agent calls next command with an array of `retrieval_ids`, sorted most-interesting-first
5. System returns `[{content, feedback_id}]` in the same order
6. Agent completes its coding task
7. **Hard gate**: system won't let the agent complete work (open PR, exit worktree, etc.) unless the next step is done
8. System prompts agent: "rank all rules MOST USEFUL → LEAST USEFUL with a reason why"
9. Agent returns `[{feedback_id, rank_reason}]` — all `feedback_ids` must be accounted for

### Why this works

- Captures bidirectional signal: pre-task interest ranking vs post-task utility ranking
- The delta between these rankings (the "expectation gap") tells you whether `use_when` accurately represents what `content` delivers
- The hard gate ensures complete feedback — no survivorship bias
- Focuses on optimizing rules via backpressure rather than micro-optimizing retrieval

### Weaknesses of V1

- **Self-attribution bias**: the agent that chose the rules also evaluates them — it will rationalize its selections ([Self-Attribution Bias in LLM Monitors](https://arxiv.org/abs/2603.04582))
- **No recall signal**: only measures rules the agent saw, never learns about rules it should have seen but didn't
- **No mechanism for acting on feedback**: captures signal but doesn't specify how to improve rules
- **Scale**: returning all matching `use_when` values can overwhelm context at 500+ rules
- **Gate pressure**: agents may rush through rankings to "unlock" task completion ([TAO-RL](https://arxiv.org/abs/2606.03762) found degenerate sessions should be excluded from training)

## V2 Design (Research-Informed)

### Rule Schema

Enriched from `{use_when, content}` based on converging findings from multiple papers:

```
{
  id:           string
  domain:       string[]        # fast routing layer (ActionNex "Key")
  use_when:     string          # structured predicate
  content:      string          # the actual guidance
  causal_note:  string          # WHY this rule exists (CLIN)
  rule_type:    "hard" | "soft" # admissibility check vs guidance (MPR)
  confidence:   float           # 0-1, starts at 0.5, evolves from feedback
  lifecycle:    "draft" | "probation" | "confirmed" | "stale"
  version:      int             # incremental edits only, never full rewrites (ACE)
}
```

**Why each field:**

- **`domain`** — [ActionNex](https://arxiv.org/abs/2604.03512) uses Key-Condition-Action (KCA) triples where the "Key" is a fast routing layer above the full condition. Coarse domain tags (`["testing", "python"]`) cut the search space by 5-10x before matching against `use_when`. Not a full taxonomy — just enough for fast filtering.

- **`causal_note`** — [CLIN](https://arxiv.org/abs/2310.10134) outperformed Reflexion by 23 points specifically because it stored *causal abstractions* ("this fails because X") rather than generic hints ("be careful with X"). Agents rank better when they understand *why* a rule exists.

- **`rule_type`** — [Meta-Policy Reflexion (MPR)](https://arxiv.org/abs/2509.03990) found that combining soft memory-guided decoding with hard rule admissibility checks improved stability over either alone. Some rules are structural constraints ("always run tests before PR"), others are guidance ("prefer composition over inheritance"). The system enforces hard rules; soft rules are advisory.

- **`confidence`** and **`lifecycle`** — From [FORGE](https://arxiv.org/abs/2605.16233) (population-based rule evolution) and auto-reflect's existing design. Confidence decays over time (90-day half-life from CASS research) and is boosted/penalized by feedback. Lifecycle gates prevent untested rules from being treated as gospel: draft → probation → confirmed → stale.

- **`version`** — [ACE (Agentic Context Engineering)](https://arxiv.org/abs/2510.04618) found that *context collapse* — iterative rewriting eroding accumulated detail — is the primary failure mode of evolving playbooks. Their fix: structured incremental delta updates only. Never rewrite a rule from scratch; edit it. Version history enables rollback.

### The Task Loop (5 Phases)

#### Phase 1: Retrieve (broad match)

**Tool boundary:** rule retrieval is `auto reflect`'s own surface (e.g. `auto reflect retrieve` /
`auto reflect get`), backed by the rule store and the event log. `auto search` is *not* involved in
retrieving rules — it appears only later, when the Phase 5 reviewer reads the session transcript and
when Layer 1 derives outcome signals. The `retrieval_id` / `feedback_id` returned here are what later
link retrieval → usage → feedback across the loop.

Agent calls `auto reflect` with an intent description. It does a BM25/keyword match against `use_when` and applies a non-excluding, IDF-weighted boost from any `domain` tags supplied — rules whose domain intersects the query are lifted by `Σ IDF(tag)` (rare in-domain tags lift more than near-universal ones), while off-domain rules are never dropped and still surface. Returns:

```
[{ use_when, retrieval_id, domain, confidence, rule_type }]
```

No content returned yet. Same two-phase retrieval as v1 (broad recall first, agent-as-reranker second).

**Hard rule injection (from [MPR](https://arxiv.org/abs/2509.03990)):** Hard rules that match on domain are *always* returned regardless of match score. They're admissibility constraints, not suggestions — the agent must see them.

**Scale handling (from [DS-MCM](https://arxiv.org/abs/2601.23188) dual-monitor + StrongDM pyramid summaries):** If the match set exceeds ~30 results, run a fast cluster pass and return cluster summaries first. Agent picks clusters to expand. Keeps context manageable at scale without losing coverage.

#### Phase 2: Select (agent ranks + probe injection)

Agent sorts by interest and calls `auto reflect` again with the sorted `retrieval_ids`. It returns:

```
[{ content, causal_note, feedback_id, rule_type }]
```

**Probe injection (from [FORGE](https://arxiv.org/abs/2605.16233) population broadcast):** System injects 1-2 "probe" rules the agent *didn't* select. Probes are rules with high historical utility but low match scores for this query. The agent doesn't know which are probes — they look identical to selected rules.

This solves v1's recall blind spot. If the agent ranks a probe as useful post-task, the rule's `use_when` is underselling it. If probes are systematically ranked low regardless of actual utility, the agent's feedback may be biased (self-attribution detection).

#### Phase 3: Work

Agent completes the coding task. Hard gate from v1 remains — can't finish without completing Phase 4.

**Hard rule enforcement (from [MPR](https://arxiv.org/abs/2509.03990)):** Hard rules aren't just "read and consider." The system checks compliance before allowing task completion. If a hard rule says "always run tests before opening a PR" and the agent didn't run tests, the system blocks. Structural enforcement, not instruction.

#### Phase 4: Feedback gate (implementer — cheap, factual, on critical path)

The implementer no longer rates the rules it chose. Self-ranking is confabulation under gate
pressure (cf. [TAO-RL](https://arxiv.org/abs/2606.03762) degenerate sessions), and the agent that
selected the rules cannot evaluate them without self-attribution bias. At the gate it reports only
two facts. Both the gap answer and the outcome must be accounted for.

**4a) Gap detection** (recall signal, from [Parthenon](https://arxiv.org/abs/2606.04602) anti-leakage learning):

> "What guidance did you need that no rule covered?"

Free-text response. Tells the system what rules to *write*, not just which to improve — Parthenon's single most valuable signal. Scored failures → task-agnostic edits to skills, tools, knowledge.

**Grounding (quality control):** the gap question is *more* confabulable than a ranking — free-text, no verifiable referent, answered under the same gate pressure used to justify killing self-ranking, and "no gaps" is the lowest-effort exit. TAO-RL filtering covers degenerate rankings but nothing covers degenerate gap reports, and Stage 3 promotes ≥3 similar mentions into draft rules — so low-effort boilerplate ("better error-handling guidance") clusters easily and seeds junk. Two cheap mitigations: (1) require each gap to cite a concrete session moment ("what were you doing when you needed it"); (2) have the Phase 5 reviewer — already reading the transcript — verify the reported gap is actually visible in it before the gap counts toward Stage 3 clustering.

**4b) Task outcome** (enables [TF-TTCL](https://arxiv.org/abs/2604.13552) contrastive analysis):

```json
{ "outcome": "success | partial | fail", "summary": "..." }
```

The outcome enables contrastive analysis across sessions (see Contrastive Loop below). Without it, you know "the agent liked this rule" but not "tasks using this rule succeed more often."

Utility judgment of the consumed rules is **not** collected here — it moves to a fresh reviewer in Phase 5.

#### Phase 5: Post-task reflection (fresh agent, async, off critical path)

A reviewer agent — **not** the implementer — judges the session against the rules. It receives:

- **(a) the session transcript** — `auto search session get <session_id> --include-thinking`
  (full conversation incl. reasoning and inline Read/Write/Edit diffs)
- **(b) the rules surfaced this session** (`feedback_id` + content) — from auto-reflect's feedback log
- **(c) the full domain-matched playbook slice** — from auto-reflect's rule store

It returns observed behavior, not opinion:

```json
{
  "compliance": [{ "feedback_id": "...", "applied": "yes|partial|no", "evidence": "diff/transcript span" }],
  "missed":     [{ "rule_id": "...", "matched_but_not_surfaced": true, "evidence": "..." }],
  "violations": [{ "rule_id": "...", "hard": true, "evidence": "..." }]
}
```

Why a fresh agent: it has no selections to defend (kills self-attribution bias), and it judges
against the transcript record — compliance is *checkable*, not self-reported. Holding the full
playbook slice, it also reports rules that **should** have fired but were never surfaced (the recall
blind spot the implementer structurally cannot see) and verifies hard-rule compliance — replacing
[MPR](https://arxiv.org/abs/2509.03990)'s self-run check, which let the agent grade its own homework.
Note the distinction the rest of the loop depends on: this yields a *compliance* signal (was the rule
applied), **not** a *utility* signal (did its presence help). Utility comes only from Stage 4.

**Timing:** auto search reads ETL parquet, so the reviewer runs *after* the session is transformed
and indexed: `session ends → auto etl run → auto search index → reviewer reads via session get`.
This dependency is why Phase 5 is async and off the implementer's critical path.

### The Improvement Loop (async, periodic)

Runs after every N feedback sessions. This is where signal becomes action.

#### Stage 1: Signal Aggregation

Per-rule computed stats. Note the two-source split: *compliance* (was the rule applied) and *utility* (did its presence change outcomes) are different measurements with different sources.

| Metric | Source | Description |
|---|---|---|
| `selection_rate` | implementer (Phase 2) | matched → selected |
| `compliance_rate` | reviewer (Phase 5) | of sessions where surfaced, how often actually applied |
| `expectation_gap` | interest (Phase 2) vs compliance (Phase 5) | high interest + low compliance = `use_when` oversells or `content` isn't actionable |
| `missed_rate` | reviewer (Phase 5) | how often a matching rule failed to surface — recall signal |
| `utility_score` | probe + A/B only | causal: does presence change outcome (see Stage 4) |
| `outcome_correlation` | observational | CONFOUNDED (rules get picked for hard tasks) — only trust where probe/A-B agrees |
| `probe_hit_rate` | probe injection | unconfounded utility evidence |
| `co_selection_map` | implementer (Phase 2) | which other rules are always selected together |

The **expectation gap** is the primary predicate-tuning signal — now *expected* (interest) vs *observed* (compliance), both clean signals rather than two self-reports:
- Large positive gap (interest > compliance) → `use_when` overpromises, or content isn't actionable enough to apply
- Large negative gap (compliance > interest) → `use_when` undersells the rule
- Near zero → `use_when` accurately represents what `content` delivers

#### Stage 2: Triage (targeted edits from [REVERE](https://arxiv.org/abs/2603.20667))

[REVERE](https://arxiv.org/abs/2603.20667) found that full-prompt rewrites cause knowledge loss. Targeted edits across specific fields (+4.5% on SUPER benchmark) preserve accumulated wisdom while fixing problems. Based on aggregated stats, each rule gets *one* action:

| Signal Pattern | Action | What Changes |
|---|---|---|
| High interest, low `compliance_rate` | Rewrite `content` | sounded relevant but wasn't actionable enough to apply |
| High `compliance_rate`, low match score | Expand `use_when` | applied when found, but predicate undersells it |
| High `missed_rate` | Rewrite `use_when` | should have surfaced and didn't — predicate too narrow; reviewer's `evidence` is the input |
| Low `selection_rate` (matched often, rarely selected) | **Narrow** `use_when` | predicate too broad — fires on irrelevant queries. This is the loop's only *contraction* signal; without it every other `use_when` action widens predicates and Phase 1 precision erodes monotonically (the >30-result cluster path becomes the norm). `selection_rate` is already in Stage 1; consume it here. |
| Low `utility_score` (probe/A-B) despite high compliance | Retire / Split | followed but doesn't change outcomes |
| >80% co-selection with another rule | Merge | combine into single rule, update `use_when` to cover both. **Exclude `rule_type: hard`** — a domain's hard rules are always returned together by design, so they trivially co-select. |
| High utility variance across sessions | Split | more specific variants for different contexts |
| Low utility + low selection for M sessions | Retire | archive, lifecycle → stale |
| Consistently high utility for M sessions | Promote | lifecycle upgrade, possibly soft → hard |

#### Stage 3: Gap-to-Rule Pipeline (from [Parthenon](https://arxiv.org/abs/2606.04602))

1. Cluster the free-text gap reports from Phase 4a across sessions
2. Recurring themes (≥3 mentions of similar gap) → candidate new rules
3. New rules enter lifecycle as "draft" with confidence 0.5
4. Get evaluated over next N sessions through normal feedback flow
5. If they reach "confirmed" lifecycle, they stay; if not, they're retired

This closes the recall loop — the system can grow its rule set based on what agents actually need, not just what rule authors anticipated.

#### Stage 4: Causal validation — the only utility signal

Self-attribution is now handled *structurally*: utility judgment moved to the fresh reviewer in
Phase 5, which has no selections to defend (cf. [paper 2603.04582](https://arxiv.org/abs/2603.04582)).
What remains is the harder problem — **compliance ≠ utility**. Neither the implementer nor the
reviewer can establish that a rule's *presence* improves outcomes, because both observe the same
population of rules that were selected *because* the task was hard. Only randomization breaks that
confound, so probes and A/B are the ONLY source of utility signal.

1. **Probe analysis**: probes are rules injected *without* the agent selecting them — the one place
   rule presence is decoupled from the agent's interest. If probes earn high compliance/outcome
   despite low match scores, their `use_when` undersells them. The randomization is real, but probes
   are a *stratified* sample (chosen as "high historical utility, low match score"), so probe stats
   describe that stratum, not the whole rulebook — they're an unconfounded read *for probed rules*,
   not a universal utility oracle. Note also that probes have a production cost: you're injecting
   rules the matcher scored as poorly-matched into real tasks, and a misleading probe can degrade the
   session it lands in (bounded — these are historically-useful rules, not noise). Log every probe
   injection against the Layer-1 outcome composite so that harm surfaces rather than hiding.

2. **A/B deployment** (from [RHO](https://arxiv.org/abs/2606.05922)): New rule versions enter as "probation" alongside the old version. System randomly serves old or new version to different sessions. Compare utility scores over N sessions. Only promote if new version statistically wins. RHO's core idea — zero external labels, self-preference selection — applied to individual rules rather than whole harnesses.

Probes and A/B are the *in-vivo* (cheap, confounded) end of utility. The causal ground truth comes from deliberate ablation — see **Utility: Measurement vs. Causation** below, which also separates the outcome-*measurement* problem (the objective composite signal) from the *causation* problem (breaking the difficulty confound).

### The Contrastive Loop (periodic, deeper)

Runs less frequently (weekly/monthly) over accumulated session data. Adapted from [TF-TTCL](https://arxiv.org/abs/2604.13552)'s "Explore-Reflect-Steer" loop, which consistently outperformed both zero-shot baselines and standard test-time adaptation through contrastive experience distillation.

1. Group completed tasks by similarity (domain, task type)
2. Within each group, partition: sessions that succeeded vs sessions that failed
3. Diff the rule sets used in each partition
4. Rules disproportionately present in successes → boost confidence
5. Rules disproportionately present in failures → investigate (may be actively harmful)
6. **Generate new candidate rules from the contrast**: "sessions that succeeded did X that sessions that failed didn't — distill X into a rule"

This is more powerful than per-session feedback because it uses cross-session evidence. It also addresses [TAO-RL](https://arxiv.org/abs/2606.03762)'s concern about degenerate sessions: population-level patterns wash out individual rushed/uniform rankings.

### Utility: Measurement vs. Causation (three layers)

Stage 4 establishes that **compliance ≠ utility** and that only randomization yields a causal
utility signal. But "did this rule's presence improve outcomes" has two separable problems that are
easy to conflate:

- **Measurement** — how good is the outcome metric? (a coarse self-reported `success|partial|fail`
  is the floor; an objective composite is the ceiling)
- **Causation** — is rule *presence* confounded with task difficulty? (observational correlation is
  the floor; deliberate ablation is the ceiling)

These are **orthogonal axes**. A richer outcome metric does *not* break the confound — combining ten
observational signals is still observational, because a hard task drives up churn, test failures,
*and* review load regardless of whether the rule helped, and hard tasks are exactly where rules get
selected. The trap to avoid: "I have ten objective signals now, so I've solved utility." You've
improved measurement, not causation. The design needs both axes, in three layers.

#### Layer 1: Outcome instrumentation (objective composite signal)

Replace the self-reported outcome label with an objective composite assembled from process signals
that auto-etl/auto-search already capture on **every** session — no reruns required:

| Signal | Source primitive |
|---|---|
| test pass / iterations-to-green | bash tool outputs, `auto search --min-errors` |
| bash error count | `auto search search "" --min-errors` |
| file churn (edits-per-path-per-session) | count Edit/Write per path within a session |
| review-comment volume | PR review threads |
| revert / reopen within N days | git history, PR state |
| interrupted tool calls | `auto search --interrupted` |
| rework on the same lines | successive diffs to identical spans |

See `docs/signals.md` for the signal catalog and derivation patterns — this layer is mostly wiring
those signals together, not new infrastructure.

**Caveats — every component is bidirectional, so don't hand-weight:**
- Low churn = clean work *or* timid under-editing; few review comments = good code *or* absent
  reviewer; tests pass = good code *or* weak tests.
- **Learn the weights against a hard anchor.** Revert-within-N-days / PR-rejected / reopened-bug are
  relatively unambiguous *negative* outcomes — treat them as labels, the soft signals as features.
- **Normalize by task difficulty/size** (or compare only within the contrastive loop's task
  clusters), or big tasks always look "worse" and you re-import the difficulty confound into the
  metric itself.
- **Degrade gracefully** when components are missing (no PR, no reviewer).
- **Goodhart:** keep the composite *diagnostic*, periodically re-anchored — never let rules be tuned
  to game "low churn + few comments" while quality rots.

#### Layer 2: In-vivo proxy (cheap, continuous, confounded)

Compute `probe_hit_rate`, A/B (Stage 4), and `outcome_correlation` against the Layer-1 composite
instead of a coarse label. More powerful and continuous, but still confounded by task difficulty —
treat as a proxy, never as proof.

#### Layer 3: Ablation calibration (expensive, causal, periodic)

Re-run a fixed benchmark of **oracle-scored** tasks (test suites give objective truth — fuzzy tasks
that need an LLM judge re-import the bias removed in Stage 4) with rules blanked out, holding the task
constant and randomizing rule presence. The outcome difference is *causal*. Design notes:

- **Power, not principle, is the limit.** A single rule's effect is small relative to agent
  run-to-run variance. The Layer-1 composite is the high-bandwidth dependent variable that makes this
  affordable: more bits per run → fewer reruns per cell to detect a small effect.
- **Don't ablate everything.** Combinatorial blow-up + rule interactions (`co_selection_map`) make
  full factorial impossible. Use a fractional-factorial design over rule groups; ablate the subset
  worth the compute (hard/high-stakes rules, ambiguous in-vivo signal, promotion/retirement
  candidates).
- **Ablation measures *marginal* utility over the model's baseline** — a rule restating what the
  model already knows scores ~0, correctly. This makes utility **model-relative**; tag scores by
  model tier (weaker models need rules more — cf. the Dunning-Kruger finding).
- **Variance reduction:** pair with/without-rule runs on fixed seeds where the harness allows
  (common-random-numbers); targeted tasks designed so the rule is *pivotal* raise effect size and cut
  reruns (measures correctness, not prevalence).
- **Staleness:** results are a snapshot of this model × rule set × task distribution; re-run
  periodically (ties to confidence decay).

**The point of Layer 3 is to calibrate Layer 2, not to grade every rule.** Establish counterfactual
ground truth on a subset, then check whether the cheap in-vivo proxy tracks it. If it does, trust the
proxy everywhere; if it doesn't, you've learned your everyday signal is junk — the most valuable thing
to know. This is the utility analogue of the [2606.05122](https://arxiv.org/abs/2606.05122)
"~160 labeled examples calibrate the cheap signal" pattern used to validate the Phase 5 reviewer's
compliance judgments.

Infrastructure already present: worktree isolation (`auto env`) for parallel isolated reruns,
mock-repo fixtures (the CLAUDE.md E2E pattern), and ETL+search for replay and scoring. The ablation
harness is those plus a DoE layer and a per-task oracle.

### Quality Controls

**Degenerate session filtering** (from [TAO-RL](https://arxiv.org/abs/2606.03762)): TAO-RL found that all-pass and all-fail sessions have degenerate advantage estimates and should be excluded from training. Applied here: discard feedback from sessions where rankings are uniform (agent rushed through gate), task failed for external reasons (infra failure, wrong spec), or session was under a minimum duration threshold.

**Context collapse prevention** (from [ACE](https://arxiv.org/abs/2510.04618)): Incremental delta updates only. Every edit creates a new version. Changelog per rule enables review/rollback. Maximum rule count cap with oldest-stale-first pruning. ACE found that structured incremental updates prevent the erosion of domain insights that plagues iterative rewriting.

**Population broadcast** (from [FORGE](https://arxiv.org/abs/2605.16233)): When running parallel agents (e.g. auto-stack implementers), broadcast the current best rule set to all agents at the start of each batch. FORGE found population broadcast was the single most important mechanism — more impactful than graduation or individual reflection. Rules need ~40% fewer tokens than few-shot examples while achieving comparable or better performance.

**Iterative refinement is critical** (from [CEL](https://arxiv.org/abs/2509.25052)): CEL's ablation study showed that removing the iterative Rule Induction + Strategy Summarization loop killed performance. The improvement loop must run repeatedly, not as a one-shot optimization. Each cycle refines rules based on new feedback, and the refinement itself produces new feedback to refine further.

### Persistence: event-sourced log (Non-Functional Requirement)

All auto-reflect state — every retrieval, selection, probe injection, gap report, outcome, reviewer
reflection, and triage decision — is persisted as **append-only JSONL events**. No in-place
mutation, no data loss. This is the canonical store. It aligns with the stack's non-destructive
philosophy (auto-etl never rewrites raw logs) and with [ACE](https://arxiv.org/abs/2510.04618)'s
version-history requirement for rollback.

- **Events are canonical; rules are a derived projection.** Current rule state (`content`,
  `confidence`, `lifecycle`, `version`) is a *fold* over the event log. Keep a materialized snapshot
  as a rebuildable cache so the CLI doesn't replay the whole log on every invocation — but the
  snapshot is disposable; the log is truth.
- **Every event carries `type` + `schema_version` + timestamp + monotonic sequence.** Append-only
  means old events keep their old schema forever, so the fold must tolerate evolution. Pairs with the
  per-rule `version` field.
- **Shard the log; never append all writers to one file.** A single `log.jsonl` appended from
  multiple worktrees/hosts collides on the file tail under git merge (and risks concurrent-write
  leakage). Shard by host+day (or session) — a *directory* of append-only files, mirroring
  auto-etl's partitioning — so concurrent appends never touch the same bytes.
- **Two scopes, mirroring the docs/etl convention.** Project-scoped events in
  `.auto/reflect/events/` are committed to git, so rules about *this* repo travel with it and are
  reviewable in PRs; global cross-repo events live in `~/.auto/reflect/events/`.
- **Sanitize before write.** Events committed to git are permanent in history — run the existing
  feedback sanitizer (see `bugfix-git-remote-credential-leak.md`) on every event so no secret or
  credential leaks into the log.

### V1 → V2 Comparison

| Aspect | V1 | V2 |
|---|---|---|
| Rule schema | `{use_when, content}` | + domain, causal_note, rule_type, confidence, lifecycle, version |
| Recall signal | None | Probe injection (FORGE) + gap detection prompt (Parthenon) |
| Hard vs soft | All rules are guidance | Hard rules structurally enforced (MPR) |
| Feedback quality | Trust the implementer's ranking | Fresh-agent compliance from transcript (observed, not self-reported) + probes/A-B for utility + degenerate session filtering (TAO-RL) |
| Improvement mechanism | Unspecified | Targeted edit triage (REVERE) + gap-to-rule pipeline (Parthenon) + A/B deployment (RHO) |
| Cross-session learning | None | Contrastive loop comparing success vs failure rule sets (TF-TTCL) |
| Scale handling | All `use_when` values returned | Domain routing (ActionNex) + cluster summaries above 30 matches |
| Multi-agent | Single agent | Population broadcast of best rule set (FORGE) |
| Context collapse | No protection | Incremental delta updates + version history (ACE) |
| Persistence | Unspecified | Event-sourced append-only JSONL; rules are a derived projection (no data loss) |
| Utility signal | Implied by self-ranking | Three layers: objective composite (signals.md) → in-vivo proxy (probes/A-B) → ablation calibration on oracle-scored tasks |

### Implementation Sequence

If building into auto-stack:

1. **Rule schema + event log + basic loop** — v1 design + domain tags + causal_note. Get retrieval + feedback events flowing (see Persistence NFR below).
2. **Gap detection prompt** (Phase 4a) — Cheapest recall signal. One free-text question at the gate.
3. **Fresh-agent reflection** (Phase 5) — The highest-value upgrade after the basic loop: compliance + recall signal from the transcript, via `auto search session get`. No new infra beyond one reviewer call plus the existing ETL/index pipeline.
4. **Signal aggregation** — Compute per-rule stats (`compliance_rate`, `expectation_gap`, `missed_rate`) by folding the event log, even if triage is manual at first.
5. **Outcome instrumentation** (Utility Layer 1) — Assemble the objective composite from `docs/signals.md` + `auto search` primitives. Cheap, every session, and the dependent variable everything else scores against.
6. **Probe injection** (Utility Layer 2) — One extra rule per session. The cheap in-vivo utility proxy (still confounded — treat as proxy, not proof).
7. **Automated triage** (Stage 2) — Once stats are trustworthy, automate the rewrite/merge/retire decisions. Gate utility-derived triage on ablation calibration, not the proxy alone.
8. **Contrastive loop** — Requires sufficient session history. Build last, biggest payoff at scale.
9. **Ablation calibration** (Utility Layer 3) — Expensive, periodic, oracle-scored. Build once you have a benchmark task set; use it to validate that the cheap proxy tracks causal truth.

**Minimum-data preconditions (steps 6–9 are data-hungry — this matters at single-user scale).** This
is a fleet-scale design; the likely deployment is one user's session stream (aggregated across tools
and hosts, but still single-user — so task *clusters* stay thin). Back-of-envelope: ~10 sessions/day,
a rule surfacing in 5% of them ≈ 15 observations/month; per-rule A/B splits that to ~7/arm against a
noisy composite, which will not reach significance before the 90-day confidence half-life decays the
rule. So gate the later stages on data, not calendar:

- Each stage declares a minimum-observation threshold (e.g. A/B requires ≥N surfacings/arm; the
  contrastive loop requires clusters with ≥M successes *and* ≥M failures).
- Below threshold, the improvement loop **refuses to act** rather than acting on noise — surface
  "insufficient data" instead of a confident-looking but statistically decorative edit.
- This is exactly what the event-sourced log buys you: the data-hungry stages can stay dormant
  without losing the signal they'll eventually fold over once volume arrives.

Hard/soft distinction and population broadcast layer on whenever parallel agents are running through the system.

## References

| Paper | Key Technique Used | Link |
|---|---|---|
| **ACE** (Agentic Context Engineering) | Incremental delta updates, context collapse prevention, evolving playbooks | [arxiv.org/abs/2510.04618](https://arxiv.org/abs/2510.04618) |
| **ActionNex** | Key-Condition-Action hierarchy, domain routing, implicit feedback from executed actions | [arxiv.org/abs/2604.03512](https://arxiv.org/abs/2604.03512) |
| **CEL** (Cogito, ergo ludo) | Iterative Rule Induction + Strategy Summarization, ablation proving iteration is critical | [arxiv.org/abs/2509.25052](https://arxiv.org/abs/2509.25052) |
| **CLIN** | Causal abstractions over generic hints, persistent textual memory, cross-environment transfer | [arxiv.org/abs/2310.10134](https://arxiv.org/abs/2310.10134) |
| **DS-MCM** | Dual-monitor (fast consistency + slow experience-driven), selective activation | [arxiv.org/abs/2601.23188](https://arxiv.org/abs/2601.23188) |
| **FORGE** | Population broadcast, rules vs examples token cost, graduation criterion | [arxiv.org/abs/2605.16233](https://arxiv.org/abs/2605.16233) |
| **Meta-Policy Reflexion (MPR)** | Predicate-like rule memory, soft guidance + hard admissibility checks, no weight updates | [arxiv.org/abs/2509.03990](https://arxiv.org/abs/2509.03990) |
| **Parthenon** | Anti-leakage learning loop, scored failures → task-agnostic skill/tool/knowledge edits | [arxiv.org/abs/2606.04602](https://arxiv.org/abs/2606.04602) |
| **REVERE** | Targeted edits across mutable fields, cross-repository pattern recognition, cumulative cheatsheet | [arxiv.org/abs/2603.20667](https://arxiv.org/abs/2603.20667) |
| **RHO** (Retrospective Harness Optimization) | Zero external labels, self-preference harness update selection, A/B evaluation | [arxiv.org/abs/2606.05922](https://arxiv.org/abs/2606.05922) |
| **Self-Attribution Bias** | LLM monitors rate own actions as safer/more-correct, cross-session evaluation framing | [arxiv.org/abs/2603.04582](https://arxiv.org/abs/2603.04582) |
| **TAO-RL** | Degenerate session filtering (all-pass/all-fail), entropy-guided diversity | [arxiv.org/abs/2606.03762](https://arxiv.org/abs/2606.03762) |
| **TF-TTCL** | Contrastive experience distillation, Explore-Reflect-Steer loop, training-free rule extraction | [arxiv.org/abs/2604.13552](https://arxiv.org/abs/2604.13552) |

<!-- ============================================================
     APPENDIX added 2026-06-09 by Charlie (notes repo research sweep).
     arXiv evidence sweep on whether LLM agents can reliably self-evaluate
     rule usefulness. 18 papers reviewed. This section provides the empirical
     backing for why V2 moved utility judgment away from the implementer
     (Phase 4 → Phase 5 fresh reviewer) and why probes/A-B are the only
     unconfounded utility signal.
     Full report: notes/projects/agentic-coding/research/2026-06-09-agent-self-evaluation-reliability.md
     ============================================================ -->

## Appendix: Evidence — Can You Trust an Agent to Rate Its Own Rules?

This appendix documents the empirical case for and against trusting an implementer agent's post-task self-evaluation of rule usefulness. It provides the research backing for V2's key structural decision: moving utility judgment to a fresh reviewer (Phase 5) and treating probes/A-B as the only unconfounded signal (Stage 4).

### The Critique

> "You can't trust the implementer agent to accurately rate/rank usefulness of the rules it consumed at the end of the session."

V1 relied entirely on implementer self-ranking. V2 has already addressed this architecturally (fresh reviewer + causal validation), but the evidence base for *why* is worth documenting.

### Evidence AGAINST Trusting Implementer Self-Evaluation

#### 1. Self-preference bias is mechanistic, not just behavioral

LLMs systematically favor their own outputs. The root cause is **perplexity preference** — LLMs assign higher evaluations to text with lower perplexity (more familiar), regardless of whether it was self-generated ([Panickssery et al., 2410.21819](https://arxiv.org/abs/2410.21819)). There is a **linear, likely causal** correlation between a model's ability to recognize its own outputs and the strength of its self-preference bias ([Ye et al., 2404.13076](https://arxiv.org/abs/2404.13076)).

Relevance: if an implementer wrote code following Rule A's guidance, the resulting code style is more "familiar" — it may rate Rule A higher not because it was more useful, but because the code it produced has lower perplexity.

#### 2. Actor-Observer Asymmetry corrupts self-reflection

When reflecting on their own work (as actor), LLM agents attribute failures to **external factors**; when auditing others' work (as observer), they attribute the same errors to **internal faults**. This bias triggers in >20% of cases from swapping perspective alone ([Taming AOA, 2604.19548](https://arxiv.org/abs/2604.19548)).

Relevance: during post-task ranking, if the task partially failed, the implementer blames the rules rather than its own execution. V2's fix (fresh reviewer as observer) is directly supported by this finding.

#### 3. Overconfidence is structural, not fixable by prompting

Specific MLP blocks and attention heads in middle-to-late layers **causally write the overconfidence signal** ([Wired for Overconfidence, 2604.01457](https://arxiv.org/abs/2604.01457)). RLHF training amplifies this because reward models have **inherent bias toward high-confidence scores** regardless of response quality ([Taming Overconfidence, 2410.09724](https://arxiv.org/abs/2410.09724)). Increasing reasoning budget **consistently impairs** calibration — more thinking = more overconfidence ([Don't Think Twice, 2508.15050](https://arxiv.org/abs/2508.15050)).

Relevance: implementer rankings are expressed with unwarranted certainty. The difference between rank #1 and #3 may not reflect real utility differences.

#### 4. Dunning-Kruger extends to code models

Poorly performing models display markedly higher overconfidence: ECE=0.726 at 23.3% accuracy vs ECE=0.122 at 75.4% accuracy across 24K trials ([DK in LLMs, 2603.09985](https://arxiv.org/abs/2603.09985)). Lower-performing code models consistently overestimate their capabilities ([DK in Code Models, 2510.05457](https://arxiv.org/abs/2510.05457)).

Relevance: the agents that need rules most (harder tasks, weaker models) produce the noisiest feedback. Failure-session feedback is the least reliable, which is exactly where you need the most accurate signal.

#### 5. Self-assessment is stable but doesn't predict performance

Psychometric self-assessment scores across 10 LLMs are highly stable across runs but **do not reliably predict actual performance** — some low-scoring models answered accurately while high-scoring ones produced weaker output ([Simulated Self-Assessment, 2511.19872](https://arxiv.org/abs/2511.19872)).

Relevance: implementer rankings may be internally consistent (repeatable) but uncorrelated with actual utility. Consistency without validity is dangerous because it looks trustworthy.

#### 6. Multi-agent debate amplifies rather than corrects bias

Multi-agent debate **amplifies biases sharply after the initial round** and sustains them. Meta-judge approaches show greater resistance ([Judging with Many Minds, 2505.19477](https://arxiv.org/abs/2505.19477)).

Relevance: population broadcast (FORGE) should propagate the *best rule set*, not aggregated rankings from multiple biased self-evaluators.

### Evidence FOR Structured Self-Evaluation (with mitigations)

#### 1. The playbook task is not standard self-evaluation

Most self-preference research studies "Is my output good?" The playbook question — "Which external documents helped me?" — is fundamentally different. Much apparent self-preference is **legitimate** (stronger models genuinely produce better outputs). **Harmful** self-preference only manifests when evaluator models err as generators — they struggle to recognize when they themselves are wrong. CoT at evaluation time reduces this ([Do LLM Evaluators Prefer Themselves for a Reason?, 2504.03846](https://arxiv.org/abs/2504.03846)).

#### 2. Self-evaluation capability is latent and elicitable

Base LLMs already possess latent self-evaluation capability. Only 160 examples needed for good calibration. The capability **transfers across unseen judges** — it's a generalizable quality notion, not judge-specific ([Self-Evaluation Is Already There, 2606.05122](https://arxiv.org/abs/2606.05122)).

#### 3. The "narcissism" finding is ~50% methodological confound

When controlling for evaluator quality baseline (comparing incorrect self-votes against incorrect cross-votes), **49% of self-preference findings lose statistical significance** ([Are LLM Evaluators Really Narcissists?, 2601.22548](https://arxiv.org/abs/2601.22548)).

#### 4. Self-reflection demonstrably improves coding performance

Self-reflection significantly improves problem-solving (p < 0.001) across 9 LLMs ([Self-Reflection in LLM Agents, 2405.06682](https://arxiv.org/abs/2405.06682)). Converting reasoning experiences into reusable meta-knowledge produces gains that **accumulate** over time ([Metacognitive Consolidation, 2604.17399](https://arxiv.org/abs/2604.17399)). If self-evaluation were pure noise, neither of these could work.

#### 5. Structured multi-dimensional assessment outperforms confidence

Asking about "effort" and "ability" rather than just "confidence" gives better predictive signal. The effort dimension yields **less overoptimistic estimates** and remains **stable across model sizes** ([Beyond Confidence, 2605.07806](https://arxiv.org/abs/2605.07806)).

Relevance: V2's structured feedback (gap detection + outcome + reviewer compliance evidence) activates different evaluation circuits than a simple "rate this rule 1-5."

#### 6. Grounded evaluation is categorically more reliable

RL-trained self-assessment generalizes well OOD and provides practically useful signal when grounded in **observable outcomes** ([Capability Self-Assessment, 2606.00251](https://arxiv.org/abs/2606.00251)).

### Verdict: V2's Architecture Is Empirically Justified

| What V2 does | Evidence supporting it |
|---|---|
| Moved utility judgment to fresh reviewer (Phase 5) | Actor-Observer Asymmetry: observer perspective is more accurate. Self-preference: fresh agent has no selections to defend. |
| Implementer only reports gap + outcome (Phase 4) | Dunning-Kruger: failure sessions produce noisiest rankings. Overconfidence: ranking magnitudes are unreliable. |
| Probes + A/B are the only utility signal (Stage 4) | Self-preference is mechanistic (perplexity-driven). Compliance ≠ utility. Only randomization breaks the confound. |
| Reviewer judges from transcript, not self-report | Self-assessment doesn't predict performance. Compliance is checkable; utility rating is confabulation under gate pressure. |
| Degenerate session filtering (TAO-RL) | DK effect: worst sessions = worst feedback. Multi-agent debate amplifies rather than corrects. |

### Additional References (this appendix)

| Paper | Key Finding | Link |
|---|---|---|
| **Actor-Observer Asymmetry** | Self-reflecting agents externalize blame; observers internalize. >20% bias from perspective swap | [2604.19548](https://arxiv.org/abs/2604.19548) |
| **Are LLM Evaluators Really Narcissists?** | 49% of self-preference findings lose significance when controlling for evaluator quality | [2601.22548](https://arxiv.org/abs/2601.22548) |
| **Beyond Confidence** | "Effort"/"ability" outperform confidence for failure prediction across 12 LLMs, 38 tasks | [2605.07806](https://arxiv.org/abs/2605.07806) |
| **Capability Self-Assessment** | Models overestimate competence; RL fixes this; transfers OOD | [2606.00251](https://arxiv.org/abs/2606.00251) |
| **Do Code Models Suffer from Dunning-Kruger?** | Lower-performing code models consistently overestimate capabilities | [2510.05457](https://arxiv.org/abs/2510.05457) |
| **Do LLM Evaluators Prefer Themselves for a Reason?** | Harmful self-preference only when evaluator errs as generator; CoT reduces it | [2504.03846](https://arxiv.org/abs/2504.03846) |
| **Don't Think Twice** | More reasoning impairs calibration; information access matters more | [2508.15050](https://arxiv.org/abs/2508.15050) |
| **Dunning-Kruger in LLMs** | 24K trials: poorly performing models are markedly more overconfident | [2603.09985](https://arxiv.org/abs/2603.09985) |
| **Judging with Many Minds** | Multi-agent debate amplifies biases; meta-judge is more resistant | [2505.19477](https://arxiv.org/abs/2505.19477) |
| **LLM Evaluators Recognize Own Generations** | Causal link: self-recognition ability → self-preference strength | [2404.13076](https://arxiv.org/abs/2404.13076) |
| **Metacognitive Consolidation** | Experience → reusable meta-knowledge; gains accumulate over time | [2604.17399](https://arxiv.org/abs/2604.17399) |
| **Self-Evaluation Is Already There** | Base LLMs have latent evaluation; 160 examples to elicit; transfers across judges | [2606.05122](https://arxiv.org/abs/2606.05122) |
| **Self-Preference Bias** (Panickssery) | Root cause is perplexity preference — LLMs prefer familiar text | [2410.21819](https://arxiv.org/abs/2410.21819) |
| **Self-Reflection in LLM Agents** | Reflection significantly improves problem-solving (p < 0.001) across 9 LLMs | [2405.06682](https://arxiv.org/abs/2405.06682) |
| **Simulated Self-Assessment** | Self-assessment is stable but doesn't predict actual performance | [2511.19872](https://arxiv.org/abs/2511.19872) |
| **Taming Overconfidence** | RLHF reward models have inherent bias toward high-confidence scores | [2410.09724](https://arxiv.org/abs/2410.09724) |
| **Wired for Overconfidence** | Specific MLP/attention circuits causally drive overconfidence | [2604.01457](https://arxiv.org/abs/2604.01457) |

<!-- ============================================================
     Response to rebuttal, 2026-06-09.
     ============================================================ -->

### Response to Rebuttal

The critique of this appendix identified real overclaims, misfiled evidence, and a one-sided verdict. Accepted in full where warranted, pushed back where not.

#### Accepted: the verdict is one-sided

The verdict table maps only AGAINST findings onto V2 decisions and ignores the FOR section entirely. The FOR evidence (structured grounded assessment works, self-reflection improves performance, effort/ability dimensions outperform confidence) is what licenses Phase 4 *still existing*. The correct synthesis is narrower and more precise than what the verdict states:

- **AGAINST** justifies removing the subjective utility ranking from the implementer.
- **FOR** justifies keeping the structured grounded signal (gap detection + outcome).
- The Phase 4 / Phase 5 split maps one-to-one onto the AGAINST/FOR split.

The evidence supports "subjective utility ranking post-failure is untrustworthy," not the broader "implementer judgment is untrustworthy." The appendix overstates its conclusion.

#### Accepted: most self-preference citations apply by analogy, not directly

Panickssery's perplexity-preference ([2410.21819](https://arxiv.org/abs/2410.21819)) and the self-recognition→self-preference causal link ([2404.13076](https://arxiv.org/abs/2404.13076)) study rating your own generation vs another model's generation. The implementer rates external rules it consumed — the rules aren't its output. The bridge ("code written following Rule A is lower-perplexity, so I over-rate Rule A") is a speculative second-order effect presented as if it were the direct finding. This is the weakest link doing visible work in AGAINST #1.

Furthermore, perplexity-preference doesn't cleanly favor the reviewer. The reviewer reads the same transcript and diffs — it inherits the same familiarity signal. The actual asymmetry the design relies on is actor-vs-observer attribution, not perplexity. Citing perplexity here muddies the argument.

#### Accepted: the reviewer is never validated

This is the biggest gap. The appendix assumes the reviewer is accurate because it's an "observer." But:

- **Actor-Observer Asymmetry says the observer is less blame-externalizing, not correct.** The verdict says "observer perspective is more accurate"; the evidence only supports "less self-serving." Overclaim.
- **"Wired for Overconfidence" and Dunning-Kruger apply to the reviewer too.** It's still an LLM with the same overconfidence circuits. Different perspective, same architecture.
- **"Don't Think Twice" is especially damning for the reviewer.** The reviewer does heavy reasoning over full transcript + thinking tokens + playbook — exactly the high-reasoning-budget configuration that paper warns degrades calibration. The appendix cites this against the implementer but it cuts harder against the reviewer.

The fix is in the appendix's own FOR evidence: [Self-Evaluation Is Already There (2606.05122)](https://arxiv.org/abs/2606.05122) shows ~160 labeled examples calibrate self-eval and transfer across judges. That's a cheap, concrete path to validate the reviewer against human compliance labels. **Action item: build a small labeled set of (session transcript, rule, compliance judgment) triples and measure reviewer accuracy before trusting the improvement loop's triage decisions.**

#### Accepted: Dunning-Kruger is misfiled

"Harder tasks / weaker models produce noisiest feedback" is true, but it's a property of task difficulty, not of who reads the transcript. A reviewer judging a messy failed session also produces degraded compliance calls. Dunning-Kruger supports TAO-RL's degenerate session filtering (which V2 already has), not the Phase 4→5 move. It should be refiled as support for the quality controls section, not the self-eval decision.

#### Accepted: multi-agent debate point is padding

V2's reviewer isn't a debate. Population broadcast doesn't aggregate rankings. The relevance note rewrites the finding into advice about broadcasting the best rule set, which is fine but isn't evidence for the self-eval decision. This inflates the AGAINST column without supporting the argument.

#### Accepted: CoT/reasoning conflict unflagged

"CoT reduces harmful self-preference" ([2504.03846](https://arxiv.org/abs/2504.03846)) and "more reasoning impairs calibration" ([2508.15050](https://arxiv.org/abs/2508.15050)) appear to conflict and are cited on adjacent sides without reconciliation. They measure different things — pairwise self-preference (does the model favor its own output over another's?) vs confidence calibration (does the model know when it's wrong?) — but the distinction matters and should be flagged explicitly. CoT helps the model be fairer in A/B comparisons while simultaneously making it more overconfident in its own correctness. Both can be true; the appendix should say so.

#### Pushed back: "consistency without validity" framing

The rebuttal calls this "the single most valuable line in the doc." Agreed — but the implication goes further than acknowledged. The rebuttal frames it as "V1's rankings would have been repeatable, looked trustworthy, and been uncorrelated with utility." That's correct, and it's exactly why **the reviewer also needs validation.** If consistency-without-validity is the trap, the reviewer's compliance judgments could fall into the same trap: stable across runs, internally coherent, and wrong. This reinforces the action item above — you need ground truth labels, not just a less-biased reader.

#### Pushed back: "the argument only needs two citations"

The rebuttal argues the core case is carried by Actor-Observer Asymmetry + consistency-without-validity, and the rest is overweight. Structurally true for the Phase 4→5 decision, but the mechanistic overconfidence evidence ([2604.01457](https://arxiv.org/abs/2604.01457), [2410.09724](https://arxiv.org/abs/2410.09724), [2508.15050](https://arxiv.org/abs/2508.15050)) serves a different purpose: it justifies a *structural* fix over a *prompting* fix. Without it, a reasonable reader could say "just add a prompt telling the implementer to be honest and self-critical." The mechanistic evidence forecloses that escape hatch. Three citations is the right count for that sub-argument.

### Limitations (added in response to rebuttal)

1. **The reviewer is an uncalibrated LLM.** Observer perspective reduces self-attribution bias but does not eliminate overconfidence, Dunning-Kruger effects, or perplexity preference. The reviewer's compliance judgments should be validated against human labels before the improvement loop's triage decisions are trusted at scale. [2606.05122](https://arxiv.org/abs/2606.05122) suggests ~160 labeled examples is sufficient — a concrete next step.

2. **Self-preference citations apply by analogy.** The direct findings study "is my output good?" The playbook question — "which external document helped me?" — is a different task. The perplexity-preference bridge (familiar code → over-rated rule) is plausible but speculative.

3. **Dunning-Kruger is a task property, not a reader property.** Hard sessions degrade signal quality regardless of whether the implementer or reviewer evaluates them. The mitigation is degenerate session filtering (TAO-RL), not the choice of evaluator.

4. **CoT helps fairness but hurts calibration.** Chain-of-thought reduces pairwise self-preference bias (fairer A/B comparisons) while simultaneously increasing overconfidence in the model's own correctness. These are independent effects on different aspects of judgment quality. The reviewer benefits from the first but is vulnerable to the second.

5. **The appendix demonstrates the design is *less biased* than V1, not that it is *accurate*.** Accuracy requires ground truth. The event-sourced log (Persistence NFR) makes retrospective validation possible — compare reviewer compliance judgments against human audit on a sample of sessions.

<!-- ============================================================
     REVIEWER SECTION added 2026-06-09 by Claude (fable-5), acting as
     an independent reviewer at Charlie's request. Everything below is
     review commentary, not design content.
     ============================================================ -->

## Reviewer Notes (Claude, 2026-06-09)

I read the full document including the appendix and rebuttal-response. The intellectual hygiene here is genuinely above average — the doc repeatedly catches its own errors (compliance ≠ utility, measurement ≠ causation, "less biased ≠ accurate") and the rebuttal cycle accepted real criticism rather than defending turf. What follows is what I think still survives that scrutiny: first what's strong, then problems I believe are new, i.e. not already covered by the appendix or limitations.

### What's actually good

1. **The core inversion is the best idea in the doc.** "Optimize the rules, not the retrieval" reframes a stale IR problem into a control problem with a feedback channel that exists anyway (the agent must read the rules to use them). The expectation-gap signal — interest vs. observed compliance — is cheap, well-defined, and directly actionable per the triage table. Most playbook systems have *no* per-rule signal at all; this one gets two clean ones per session.

2. **The compliance/utility split is maintained with unusual discipline.** It would have been easy to let `compliance_rate` quietly stand in for "this rule is good." The doc names the confound (rules get selected *because* tasks are hard), refuses observational utility, and routes all utility claims through randomization. The three-layer measurement/causation section, especially "ten objective signals improve measurement, not causation," preempts the exact mistake most teams make.

3. **Layer 3's framing — calibrate the proxy, don't grade every rule — is the right economics.** Ablation per rule is unaffordable; ablation as a periodic audit of whether the cheap signal is junk is affordable and is the single highest-information experiment in the design.

4. **The persistence NFR is quietly load-bearing and correct.** Append-only events with rules as a fold means every future question you haven't thought of yet ("did rule X's v3 actually change compliance?") is answerable retroactively. Sharding by host+day to avoid merge collisions shows the design was written by someone who has actually been bitten by concurrent JSONL appends.

5. **The implementation sequence is honest.** Cheap signals first, automation gated on validated stats, expensive causal machinery last. Many designs of this ambition front-load the fun parts.

### Problems not yet covered by the appendix

1. **Sample-size arithmetic is missing, and it may sink half the machinery at this deployment's scale.** This is a single-developer stack (~6 months ≈ 1GB of logs; `doc-file-usage-findings.md` analyzed 420 sessions). Suppose 10 sessions/day and a rule that surfaces in 5% of sessions: ~15 observations/month. Per-rule A/B (Stage 4.2) splits that to ~7 per arm, against a noisy composite outcome — you will not reach significance before the 90-day confidence half-life decays the rule anyway. The contrastive loop needs *clusters* of similar tasks with both successes and failures; at single-user volume most clusters will have n<5. The design reads as if written for a fleet; nothing in it is wrong at fleet scale, but the implementation sequence should state minimum-data preconditions per stage, or steps 6–9 risk producing statistically decorative output. Concretely: define "enough data" thresholds (e.g. A/B requires ≥N surfacings/arm) and have the triage loop *refuse* to act below them rather than act on noise.

2. **The triage table is an asymmetric ratchet toward predicate bloat.** Look at the `use_when` actions available: high `compliance_rate` + low match → *expand*; high `missed_rate` → *rewrite because too narrow*. There is no row that *narrows* a `use_when`. The `missed` signal comes from a reviewer making an uncalibrated counterfactual judgment ("this should have fired"), which is exactly the kind of judgment LLMs over-produce — the appendix's own overconfidence evidence applies. So expansion signals are plentiful and cheap; contraction signals don't exist. Run this loop for a year and every predicate widens, match sets grow, the >30-results cluster path becomes the norm, and Phase 1 precision quietly degrades. The fix is cheap: add a triage row driven by `selection_rate` — *matched often but rarely selected → `use_when` too broad, narrow it*. That signal is already in the Stage 1 table; it's just never consumed.

3. **`probe_hit_rate` is labeled "unconfounded" but probes are a biased sample of rules.** Probes are chosen as "high historical utility but low match scores" — so probe statistics describe *that stratum*, not the rulebook. The randomization is real (presence decoupled from agent interest), but the doc generalizes from it as "your only unconfounded read on whether a rule is worth having," which overstates what a selected-on-historical-utility sample can tell you. Also unexamined: probes have *side effects on production work*. You are injecting rules the matching system judged irrelevant into real tasks; a misleading probe can actively degrade the session it was injected into. That cost is plausibly small but it is nonzero and unmeasured — at minimum, log probe injections against the Layer-1 outcome composite so harm shows up.

4. **Phase 4a (gap detection) has no quality control, and it's under the same gate pressure the doc uses to kill self-ranking.** The doc's argument for removing utility ranking from the gate is that gate-pressured answers are confabulation. But the gap question — free text, no verifiable referent, answered by an agent that wants its PR unlocked — is *more* confabulable than a ranking, not less. The empty answer ("no gaps") is also the lowest-effort gate-exit. TAO-RL filtering covers degenerate *rankings*; nothing filters degenerate gap reports. Since Stage 3 turns ≥3 similar mentions into draft rules, low-effort boilerplate gaps ("better error-handling guidance") will cluster *easily* and seed junk rules. Mitigation candidates: require gap reports to cite a concrete moment in the session ("what were you doing when you needed it"), and have the Phase 5 reviewer — who reads the transcript anyway — grade whether the reported gap is visible in the transcript.

5. **`confidence` is defined but never consumed.** The schema gives it a starting value, decay, and feedback coupling; no later section uses it. Phase 1 returns it to the agent (unstated whether the agent should weight it), retrieval doesn't rank by it, the triage table modulates `lifecycle` instead, and hard-rule injection ignores it. Either specify where confidence changes behavior (retrieval ranking? probe eligibility? triage thresholds?) or fold it into `lifecycle` and delete it. As written it's a number that accumulates updates and influences nothing — the kind of field that survives three refactors because everyone assumes someone else reads it.

6. **Phase 5 reviewer cost is unbounded and unprioritized.** The reviewer ingests a full transcript *including thinking* plus a playbook slice, per session. The doc's own data says 90% of log volume is Read/Write/Edit noise; a long session's transcript can exceed a sensible context budget outright. Nothing says whether every session gets reviewed or how transcripts are truncated (and truncation interacts badly with "evidence: transcript span"). At minimum: sample sessions rather than reviewing all (weight toward sessions that surfaced probation rules or hard rules — that's where judgments change decisions), and use `transcript_truncated` views with on-demand expansion rather than full-fidelity ingestion.

7. **A/B versioning makes the playbook non-deterministic for the human reading it.** Stage 4.2 serves old/new rule versions randomly across sessions. Project-scoped rules are committed to git and "reviewable in PRs" — but which version is canonical in the working tree while an A/B is live? A developer reading the playbook sees one version; half their agent's sessions ran the other. This is solvable (events are canonical, snapshot can carry both versions tagged as variants) but it's a real UX/trust wrinkle the persistence section should address explicitly, because "the rule file I read isn't the rule my agent got" is the kind of thing that erodes confidence in the whole system.

8. **Rule conflict is unmodeled.** `co_selection_map` captures co-occurrence, and merge handles redundancy — but nothing detects two rules giving *contradictory* guidance (one says "prefer X," another "avoid X" in overlapping domains). At small rule counts humans catch this; the gap-to-rule pipeline auto-generating drafts at scale is precisely what produces it. A cheap mitigation exists inside the existing machinery: when the Phase 5 reviewer reports a violation of rule A in a session where rule B was complied with, that pair is a conflict candidate.

### Smaller notes

- The **>80% co-selection → merge** rule will false-positive on hard rules: a domain's hard rules are *always* returned together by design, so they trivially co-select. Exclude `rule_type: hard` from the merge heuristic.
- Stage 2 says each rule gets "*one* action" per cycle but multiple rows can match simultaneously (high missed_rate *and* high co-selection). Specify a precedence order, or the triage implementation will pick one arbitrarily and the choice will be load-bearing.
- The doc would benefit from one worked end-to-end example — a single concrete rule traced from draft → probe → expectation gap → triage edit → A/B → confirmed. Every individual mechanism is clear; the *composition* is where a reader (or implementer) has to hold nine moving parts in their head.

### Overall

The design's weakest claims were already amputated by its own rebuttal cycle, which is rare and to its credit. What remains is a genuinely sound architecture whose main unacknowledged risk is **statistical, not structural**: nearly every loop in it is data-hungry, and the deployment context is one developer's session stream. I'd build steps 1–5 of the implementation sequence exactly as written, add the predicate-narrowing triage row and gap-report grounding before turning on automation, and treat steps 6–9 as dormant until the event log proves there's enough volume to feed them.

<!-- ============================================================
     DRAFT worked example added 2026-06-09. Traces one rule end-to-end
     to make the composition of the nine mechanisms legible. Illustrative
     numbers, not measured. Review/refine before treating as canonical.
     ============================================================ -->

## Appendix: Worked Example — One Rule, End to End (DRAFT)

Every mechanism above is individually clear; the *composition* is where a reader has to hold nine
moving parts at once. This traces a single rule from birth to "confirmed," showing what each phase,
stage, and layer actually does to it. Numbers are illustrative, not measured.

### The rule

The rule itself is *born* from the system (Stage 3), not hand-authored — start there.

**Day 0 — birth via gap-to-rule (Stage 3).** Across two weeks, several Go sessions file Phase-4a gap
reports that cluster: "kept hitting compile errors late," "didn't realize the package broke until I'd
edited four files." Three grounded mentions (each citing a concrete session moment, per the Phase-4a
QC) → a candidate rule. It enters as:

```
{
  id: "r_go_build_incremental",
  domain: ["go", "build"],
  use_when: "editing Go source files",
  content: "After modifying a Go file, run `go build ./...` before moving on.",
  causal_note: "Unbuilt files hide compile errors that surface later as a confusing batch.",
  rule_type: "soft",
  confidence: 0.5,
  lifecycle: "draft",
  version: 1
}
```

Event appended: `{type: "rule_created", id: "r_go_build_incremental", source: "gap_cluster", gap_ids: [...], ts, seq}`.

### One session through the loop

**Phase 1 — Retrieve.** An agent starts a task: *"add a `--json` output flag to `auto env`."* It calls
`auto reflect` with that intent (rule retrieval is auto-reflect's surface — `auto search` plays no
part here). Domain filter narrows to `["go", "cli", "env"]`; BM25 over `use_when` returns the draft
rule alongside ~6 others. The agent sees `{use_when, retrieval_id, domain, confidence: 0.5,
rule_type: "soft"}` — no content yet. (A hard rule in `["go"]`, say "run `gofmt`," is force-injected
regardless of score.)

**Phase 2 — Select + probe.** The agent ranks our rule **interest #2 of 5** and asks for content. The
system returns `{content, causal_note, feedback_id}` for the selected rules **plus one probe** the
agent didn't pick — `r_json_stderr` ("in JSON mode, send diagnostics to stderr"), a high-historical-
utility rule with a low match score here. The agent can't tell which is the probe.

**Phase 3 — Work.** The agent writes the flag. The hard `gofmt` rule is enforced at the gate (checkable);
our soft rule is not.

**Phase 4 — Feedback gate (cheap, factual).**
- *4a gap (grounded):* "Needed to know how to scope the build to one module — `go build ./...` from
  the repo root rebuilt everything and was slow." Cites the moment (the slow build step).
- *4b outcome:* `{outcome: "success", summary: "flag added, tests pass"}`.
- No utility ranking is collected (that died in V2).

Events: `retrieval`, `selection` (incl. probe `r_json_stderr`), `gap_report`, `outcome` — all appended.

**Phase 5 — Reflection (fresh reviewer, async, after `etl run` → `search index`).** A reviewer reads
the transcript via `auto search session get <id> --include-thinking`. It reports:
```json
{
  "compliance": [
    { "feedback_id": "...r_go_build_incremental", "applied": "partial",
      "evidence": "ran `go build ./...` once at the end, not after each of 3 file edits" },
    { "feedback_id": "...r_json_stderr(probe)", "applied": "no",
      "evidence": "diagnostics written to stdout at env.go:212" }
  ],
  "missed": [], "violations": []
}
```
Event: `reflection` appended. Note the two signals this one session produced: **selected + only
partially applied** (our rule), and a **probe that was ignored and would have caught a real bug**
(stdout/stderr) — recall signal the implementer could never have given.

### After N sessions — the loop acts

**Stage 1 — Aggregation (fold over the event log).** For `r_go_build_incremental`:

| Metric | Value | Reading |
|---|---|---|
| `selection_rate` | 0.78 | agents pick it often |
| `compliance_rate` | 0.31 | but rarely *fully* apply it |
| `expectation_gap` | +large (interest ≫ compliance) | `use_when` oversells or content isn't actionable |
| `missed_rate` | low | surfaces when relevant |
| gap cluster | "scope build to module" ×6 | content is the problem, not the predicate |

**Stage 2 — Triage (one action).** Pattern = *high interest, low `compliance_rate`* → **Rewrite
`content`** (not `use_when` — matching is fine; the guidance just isn't followable in a monorepo).
Incremental delta only (ACE), `version: 1 → 2`:

> content v2: "After modifying a Go file, run `go build ./...` **from that file's module directory**
> (not the repo root) before moving on."

Event: `rule_edited {id, field: "content", from_version: 1, to_version: 2, evidence: gap_ids, ts, seq}`.
(Contrast: had the metric been *high match, low `selection_rate`*, triage would instead **narrow**
`use_when` — the contraction row that stops predicate bloat. Not triggered here.)

**Stage 4 — A/B (only once min-data precondition is met).** v2 enters `lifecycle: "probation"`
alongside v1. The system serves v1/v2 at random across sessions and compares utility via the **Layer-1
composite** (build-error count, iterations-to-green, file churn) — *not* self-report. Precondition
gate from the implementation sequence: hold until ≥N surfacings/arm; below that the loop reports
"insufficient data" rather than promoting on noise. After enough sessions, v2 shows lower late-build
churn and higher compliance → **v2 wins**.

**Layer 3 — Ablation calibration (periodic, oracle-scored).** On a benchmark Go task with a
deliberately-planted type error in module B: run *with* the rule vs *with it blanked out*, many reruns.
With the rule, the agent catches the error at first build (fewer iterations, lower churn); without, it
surfaces at the end. Causal utility confirmed **> 0**. Crucially this also checks that the cheap
in-vivo `probe_hit_rate`/A-B proxy *tracked* the ablation truth — if it did, trust the proxy for
rules you don't ablate.

**Lifecycle promotion.** Draft → probation → **confirmed** after M sessions of positive compliance +
the ablation confirmation; `confidence` climbs from 0.5 toward ~0.85. Because compliance and causal
utility are both high and the rule is a clean structural check, it's a candidate for **soft → hard**
promotion (which would then enforce it at the Phase-3 gate).

### What this shows about the composition

- The **expectation gap did its job**: it distinguished "wrong predicate" (would rewrite `use_when`)
  from "right predicate, useless content" (rewrote `content`) — using compliance, not opinion.
- The **probe** surfaced a real recall miss (stdout/stderr bug) that no implementer self-report
  could, and its ignored-status is logged against outcomes so a harmful probe would show up.
- **Measurement vs. causation stayed separate**: A/B and the composite said v2 *correlates* with
  better outcomes; only the ablation said the rule *causes* them.
- Every transition is an **append-only event**; the rule object is just the current fold, and the
  whole history (v1 → v2, every probe, every reflection) is replayable for any future question.

### Open threads this example surfaces (not yet resolved in the design)

- **A/B vs. git determinism (Reviewer #7):** while v1/v2 are both live, which version is canonical in
  the git-committed `.auto/reflect/` snapshot a human reviews? Needs an explicit answer.
- **`confidence` (Reviewer #5):** the climb from 0.5 → 0.85 is narrated here, but no other section
  actually *consumes* `confidence` — does it gate retrieval ranking, probe eligibility, or just
  mirror `lifecycle`? Pin this down before implementing.
- **Conflict detection (Reviewer #8):** if a later rule said "defer builds to CI to keep local loops
  fast," it would contradict this one — the design has no detector for that yet.
