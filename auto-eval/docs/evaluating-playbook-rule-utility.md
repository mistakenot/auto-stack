---
hash: "6183ff97"
id: "eval-rule-utility"
read_when: "designing evals that measure whether auto-reflect playbook rules actually improve agent runs, or building the auto-eval utility-scoring harness"
summary: "Design notes on how to evaluate whether a learned playbook of rules causally improves coding-agent runs — separating good-artifact from causal-utility, the measurement-vs-causation axes, the eval ladder, the evidence-quote-as-oracle trick, the circularity trap of pivotal tasks, utility as a product of benefit and firing-cost, model-relativity and decay, units of analysis, harm as a worst-case, and a recommended first harness."
title: "Evaluating Playbook Rule Utility"
---

# Evaluating Playbook Rule Utility

Design notes for measuring whether the `auto-reflect` playbook — rules mined from session
history — actually makes future agent runs better. Written after an end-to-end dogfood run
(mine → observe → consolidate → 16-rule playbook) surfaced the central gap: we can prove a
rule *describes a real recurring problem*, but we have no evidence its *presence* improves
anything.

This complements the two existing framings:

- `auto-eval/requirements.md` — the compile → run → score harness (replay real tasks under
  modified conditions, A/B/N, N-trial averaging, scoring via auto-etl/auto-search). Its v1
  scopes **context recall** (does the agent read the expected files). This doc is about the
  harder downstream question: does a rule **change outcomes**.
- `auto-reflect/docs/self-improving-playbook-retrieval.md` — the three-layer utility model
  (objective composite → in-vivo proxy → ablation calibration) and the reviewer notes on
  single-user sample-size limits. This doc builds on that, adds the cheaper rungs it
  under-emphasizes, and records where the brainstorm *updated* its recommendations.

## 1. The core problem: two axes that get conflated

Two distinctions do all the work. Most "does the playbook help?" hand-waving collapses them.

**Axis A — compliance ≠ utility.**
- *Compliance*: when a rule was surfaced, did the agent follow it? Checkable from the transcript.
- *Utility*: did the rule's **presence** improve the outcome? Not checkable from one run.
- A rule can be fully complied with and useless (the agent would have done it anyway), or
  ignored and valuable (it was right, the agent erred).

**Axis B — measurement ≠ causation.**
- *Measurement*: how good is the outcome metric? (a coarse success/fail label is the floor;
  an objective composite of process signals is the ceiling.)
- *Causation*: is the rule's presence confounded with task difficulty? Rules get selected
  *because* a task is hard, so "sessions using rule X do worse" can mean X is harmful **or**
  X-tasks are harder.

These are orthogonal. Combining ten objective signals improves **measurement** and does
nothing for **causation** — a hard task drives up churn, test failures, *and* review load
regardless of whether the rule helped. The trap: "I have ten signals now, so I've measured
utility." You've improved measurement. Only randomization (deliberate ablation: same task,
rule present vs absent) breaks the confound.

## 2. Two questions an eval suite must answer separately

1. **Is the rule a good artifact?** Well-formed, retrievable for the right intent, followed
   when surfaced. Cheap, deterministic, makes no causal claim.
2. **Does the rule's presence cause better outcomes?** Expensive, needs randomization.

Rungs 1–3 below answer (1); rungs 4–5 answer (2).

## 3. The eval ladder (cheap → expensive)

| Rung | Question | Cost | Catches | Needs |
|------|----------|------|---------|-------|
| 1. Static audit (LLM judge) | Is the rule actionable, specific, non-conflicting, not over-fit? | trivial | junk/duplicate/contradictory rules | nothing |
| 2. Retrieval eval | Does the right rule surface for a given intent? (precision/recall of `use_when`) | cheap, deterministic | predicate too broad/narrow | intent→expected-rule pairs |
| 3. Compliance eval | When surfaced, was it followed? | medium | vague/unactionable content; surfaced-but-ignored | fresh-reviewer over transcript |
| 4. Recurrence eval | Does the failure mode's frequency drop after the rule lands and is surfaced? | cheap but slow | in-production effect (confounded) | the loop wired into sessions + time |
| 5. Pivotal-task ablation | Does presence *causally* reduce the targeted failure? | expensive | causal utility | task fixtures + oracle + N reruns |

Only **5** is causal ground truth. **4** is the natural in-production signal but confounded
and slow. **1–3** are immediately buildable and gate "is this even a sane artifact."

## 4. Key insight: the evidence quote is a free oracle

Every rule was mined from a concrete failure signature in a real transcript — `"go.mod file
not found"`, `"directory prefix does not contain main module"`, `"parallel golangci-lint is
running"`. That string is exactly the **objective, LLM-free** success/failure detector an
ablation needs. So the pipeline produces its own scoring function:

```
session evidence quote  →  observation  →  rule  →  eval oracle (grep for the failure the rule claims to prevent)
```

You don't invent scoring functions; you reuse the failure signature the observation already
captured. This is the cheapest path to an objective oracle and it falls out of the existing
data for free.

## 5. Rules differ in evaluability — triage by type

The free-oracle trick only works for some rules. Split the playbook:

- **Error-signature rules** (monorepo-go build, parallel-lint, build-after-edit): a clean
  failure string exists → objective oracle → go straight to rung 5.
- **Process / preference rules** (investigate-before-coding, keep-it-simple,
  force-with-lease): no error string; "success" is behavioral/judgment → rung 5 is
  unreliable. Lean on rungs 2–3 and accept noisier signal, or a calibrated judge.

Forcing a clean-oracle method onto a fuzzy rule produces confident nonsense. Part of any
real playbook is **structurally harder to prove** than the rest; say so rather than faking a
number.

## 6. The circularity trap of pivotal tasks

The obvious rung-5 design — build a task *engineered* so rule X is decisive, then show X
prevents X's failure — is nearly tautological. You constructed the task around the exact
failure the rule names. That measures **correctness** (when relevant, does the rule do what
it claims?) — **not prevalence × value** (how often does that relevance arise in real work,
and is it worth the cost?). Synthetic-pivotal evals systematically **over-estimate** utility.

Correction: sample the **real task distribution** instead of hand-building around rules.
Replay the opening prompts of real historical sessions (we have 600+ for this repo), two
arms (playbook vs none), score with the composite. Real prompts, real distribution, proper
held-out set. **This updated the recommendation**: the better *first* eval is a
playbook-level real-prompt replay; per-rule pivotal ablation is better repurposed as the
*promotion gate* later, where some circularity is tolerable because the question narrows to
"does this specific rule mechanically work."

## 7. Utility is a product, not a sum — and the denominator is usually missing

Content quality and retrieval quality are not separable; they multiply:

```
net utility  ≈  P(rule relevant) · benefit_when_relevant   −   P(rule fires) · context_cost
```

Consequences:

- A rule with excellent content and a sloppy `use_when` that fires on everything is
  **net-negative** — it pollutes context 95% of the time to help 5% (cf. Replit's "static
  rules pollute context"; ERL's "irrelevant guidelines degrade Execution").
- The **retrieval eval (rung 2) is half the utility function**, not a warm-up. A rule cannot
  be valued without its firing precision.
- Almost nobody measures the **context-cost denominator**. The eval must include tasks where
  the rule fires *but shouldn't*, and measure the damage; otherwise you optimize benefit while
  ignoring the bill.
- The honest top-line unit is per-**surfacing**, not per-relevant-hit.

## 8. The asset depreciates — utility is marginal-over-model and time-bound

A rule's value is **marginal over what the model already knows**. A strong model may already
do the right thing unprompted; a weaker model or a context-rotted long session does not. So:

- Every eval result is **tagged to a model tier**. A rule scoring ~0 on the strongest model
  is not useless — it is insurance for weaker models and degraded context.
- As base models improve, rules **decay** (the "harness rules become dead weight on better
  models" finding). The playbook is not a fixed asset; its value drifts down and must be
  re-evaluated per model generation.
- Eval cadence is therefore coupled to **model releases**, not just to data volume — and this
  is what makes confidence-decay a real mechanism rather than cosmetic.

## 9. Two units of analysis, chosen by the decision they feed

Don't pick "per-rule vs whole-playbook" — you need both, for different decisions:

- **Playbook-level** (2 arms, with/without the whole playbook): the headline "is this thing
  worth deploying?" Cheap (2 arms not N), honest (no per-rule circularity if tasks are real),
  and it is what you actually ship.
- **Per-rule ablation** (hold one rule out): *marginal* contribution given the rest — the
  right unit for triage and the promotion gate. Combinatorial because rules interact; use a
  fractional-factorial subset, not full factorial.

Design every eval **backward from the action it gates** — the bars differ wildly:

| Action | Eval bar |
|--------|----------|
| promote draft → confirmed | net-positive on the composite |
| edit content / `use_when` | expectation gap localizes the fault (predicate vs content) |
| retire | consistently ≤ 0 or harmful |
| soft → **hard** (structural enforcement) | very high bar — a wrong hard rule *blocks real work*; false-positive cost is asymmetric and severe |

A cheap eval good enough to gate promotion beats a perfect one too expensive to run per
promotion.

## 10. Harm is a worst-case question, not an average one

Average-effect evals wash out the scariest failure: a rule that fires in the wrong context
and pushes the agent toward a **worse** solution. Rare but catastrophic → invisible to mean
deltas. Hunt it deliberately: take a rule and find adjacent tasks where its advice is *wrong*
(e.g. "keep it simple, no migration" on a task that genuinely needs a migration;
force-push guidance surfacing where no push should happen). This is a separate worst-case
eval, and it is what should make you nervous about hard-promotion.

**Negative controls keep the harness honest.** Seed the benchmark with a deliberately useless
or scrambled rule and a task where the rule should not fire. If the harness reports
"everything helps," it is broken.

## 11. Task-suite construction is the bias surface

Whoever builds the tasks controls the result. Three sources, each with a bias:

| Source | Realism | Bias / cost |
|--------|---------|-------------|
| Synthetic-pivotal | low | circular — over-estimates (§6) |
| Replayed historical opening-prompts | high | real distribution; but a fresh replay loses the original *trajectory* (see open questions) |
| Real upcoming work | highest | one-shot; can't run both arms on the same task without contamination; expensive |

Sweet spot: **replay the opening prompt of real historical sessions**. It samples the true
task distribution, the prompts are real, and it yields a proper held-out set. The cost is a
weaker oracle for free-form tasks (no clean pass/fail) — lean on the composite plus
"did-a-known-failure-signature-recur" as the objective backstop.

## 12. Variance & methodology (the difference between signal and noise)

LLM run-to-run variance is large relative to a single rule's effect, so:

- **High-bandwidth dependent variable.** Prefer the objective composite (bash-error count,
  iterations-to-green, file churn, tokens, wall-clock, interrupted calls — the *process*
  signals) over a binary success label. Many rules change *how* the agent works (fewer
  iterations, less churn) without changing *whether* it eventually succeeds; binary success
  is too coarse to see them. More bits per run → fewer reruns to detect a small effect.
- **Variance reduction.** Common-random-numbers / fixed seeds where the harness allows;
  matched task pairs; **interleave** the arms rather than batching them, so model/infra drift
  doesn't align with the arm.
- **Minimum-N is a real precondition.** Each stage declares a minimum-observation threshold
  and *refuses to act* below it rather than emitting a statistically decorative edit. (This is
  also `requirements.md`'s open "statistical minimum number of runs" question — the answer is
  effect-size-dependent and is exactly what a high-bandwidth composite buys down.)

## 13. Recommended first harness: two-arm real-prompt replay

The cheapest thing that honestly answers "is this playbook worth it" and dodges the
circularity trap:

- **Tasks**: ~10 real historical session opening-prompts from this repo (sampled across task
  types).
- **Arms**: playbook injected via `auto reflect retrieve`/`select` vs no playbook —
  **interleaved**, ~3 reps each ≈ 60 runs.
- **Isolation**: one `auto env` worktree per run; mock/seeded fixtures per
  `requirements.md`'s compile step.
- **Score**: the objective composite as primary DV; "did a known failure signature recur"
  (the §4 oracle) as the objective backstop.
- **Read-out**: a playbook-level headline delta with variance; per-task deltas hint at which
  rules carry it. The per-rule pivotal-ablation harness then becomes the **promotion gate**
  reached for *after* the headline says the thing is worth tuning at all.

Infra already present: `auto env` worktrees for parallel isolated reruns, the
`ntm`/sub-agent fan-out, ETL+search for replay and scoring, the event log for provenance.
The harness is those plus a design-of-experiments layer and a per-task oracle.

## 14. From operator instinct to a hardened v1

A natural operator instinct, stated plainly: *replay a real task from a previous commit of
the repo, A/B a small tweak (add a rule), rerun the original execution prompt, and see how
the outcome differs.* That instinct is essentially correct — it independently arrives at §13
and at the compile → run → score model in `requirements.md`. It also gets the hardest thing
right for free:

**Same-task A/B breaks the difficulty confound.** Running both arms on the *same* task,
differing only by the rule, randomizes away the "rules get used on hard tasks" confound (§1,
Axis B) that makes observational utility untrustworthy. Most approaches can't; this one does
by construction. The skeleton is right — what turns it into a *trustworthy* harness is the
methodology around it:

1. **Compare arms, not history.** The baseline is arm A (no rule), not what actually
   happened. The original ran with a human steering it; a fresh solo replay of the execution
   prompt is a different, easier task. Use history at most as a sanity sample, never as the
   metric. (This one reframe removes most of the "fair counterfactual" worry.)
2. **Replay only replayable tasks.** Autonomous prompt→PR runs replay cleanly; heavily
   human-steered ones don't (trajectory loss). Filter to autonomous tasks, or add a
   user-simulator to replay the *interaction* — at the cost of its own bias.
3. **N, not 1.** A single A-vs-B difference is noise; LLM variance dwarfs a small rule's
   effect. Run several **interleaved** reps per arm with a variance-aware comparison (§12).
   Minimum-N is effect-size-dependent.
4. **Objective scoring is the hard half.** "See the outcome" needs a real metric: the process
   composite (§12) plus the evidence-quote oracle (§4) where one exists — not an LLM grading
   vibes, which re-imports the bias removed elsewhere.
5. **Prune answer leakage.** If the starting ref still contains the solution (the task's own
   merged PR, or planning docs describing the fix), both arms cheat. The compile step must
   reset to genuinely *before* and strip downstream artifacts — this is why `requirements.md`
   makes compile hermetic.
6. **Confirm the rule actually fires.** The intervention is *the agent seeing the rule*, not
   the rule existing in the playbook. If retrieval doesn't surface it for the task, arm B ==
   arm A and you measure nothing.
7. **Whole-playbook first, per-rule later.** "One small tweak at a time" is the right unit
   for *attribution* but too expensive to start with at single-user volume. Begin with
   playbook-vs-none (one big, cheap, high-signal contrast), then drop to per-rule ablation to
   explain which rules drove it.
8. **Don't cherry-pick the tasks.** Selecting tasks where you expect the rule to help
   re-imports the circularity of §6. Sample across task types.

Net: the instinct is the correct v1 harness; these eight points harden it rather than replace
it. The first three (compare arms, pick replayable tasks, run N) are the ones most often
skipped and the ones that most determine whether the result means anything.

## 15. Sequencing for single-user scale

The design doc's reviewer notes warn: at ~10 sessions/day, in-vivo A/B and the contrastive
loop won't reach significance before the 90-day confidence half-life decays the rule. So:

1. **Now (no volume needed):** rung 1 (static audit) + rung 2 (retrieval eval) as a daily
   gate; they tune `use_when` and catch junk immediately.
2. **Next:** one-rule **rung-5** harness end-to-end on an error-signature rule (monorepo-go —
   its oracle is trivial) to prove the ablation pattern, then the **playbook-level two-arm
   replay** (§13) for the headline.
3. **Later, free:** rung 4 (recurrence) once the loop is wired into real sessions
   (auto-reflect epic Phase 2) — treat it as confirmation, not the primary signal.
4. **Gate promotion on utility, not occurrence.** Today draft→confirmed gates on ≥2 sessions
   of *evidence* (occurrence). The stronger gate is *utility*: a draft graduates only after it
   passes its ablation. This turns evals from a side activity into the promotion mechanism.

## 16. Open questions (unresolved)

- **Is "replay the opening prompt" a fair counterfactual?** The original session's value came
  from its *whole trajectory*; a fresh replay re-runs only the initial prompt and loses the
  real interaction. You may be scoring a *different* task than the one that actually happened.
  How much does that bias the comparison, and can a "user-simulator" agent (per
  `requirements.md`) replay the *interaction*, not just the prompt, without importing its own
  bias?
- **Are process/preference rules evaluable at all?** Rules with no error signature
  (investigate-first, keep-it-simple) may be provable only by a calibrated judge — which
  re-introduces the LLM-judgment bias we removed elsewhere. Is there a behavioral proxy, or do
  we accept that part of the playbook is governed by weaker evidence?
- **Reviewer calibration cost.** Rung 3 and any judge-scored task need ~160 human-labeled
  examples to calibrate (per the design doc). Who produces those labels at single-user scale,
  and how often must they be refreshed as models change?
- **Whole-playbook interaction effects.** Per-rule ablation measures marginal contribution
  given the rest, but a rule helpful in isolation can be drowned when 15 others are also
  surfaced. How big are interaction effects, and does retrieval top-k gating (top-1/top-3)
  make per-rule ablation cleaner?

## References

- `auto-eval/requirements.md` — compile/run/score harness, context-recall v1, statistical-N question.
- `auto-reflect/docs/self-improving-playbook-retrieval.md` — three-layer utility model, sample-size limits, A/B and ablation design.
- `auto-reflect/docs/decision-mining.md` — passive feedback signals (corrections that stop after a rule lands = the recurrence eval).
- `auto-reflect/docs/research-april-2026.md` — ReasoningBank experimental harness (success + steps + cost), SkillFlow lifecycle metrics, model-transfer/decay findings.
- `docs/signals.md` — the catalog of process signals the Layer-1 composite is assembled from.
