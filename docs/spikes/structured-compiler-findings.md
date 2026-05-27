# Structured Compiler Spike — Consolidated Findings

**Date:** 2026-05-27
**Status:** Spike complete (8 experiments across 6 phases)
**Total cost:** $2.22 OpenAI spend (7.4% of $30 budget)
**Decision needed:** Whether to allocate engineering time to a structured requirements compiler.

This document is the durable record of what was learned. The companion synthesis at `docs/experiments/structured-compiler/summary.md` carries the same numbers; this doc additionally captures the candid product read that doesn't belong in an experimental report.

---

## TL;DR

The original "structured compiler" idea — compile a complete requirements doc from a user prompt by asking smart questions and tracking decisions in a typed schema — **does not work**. Three sub-systems were tested. One ships (incremental recompile, as infrastructure). One is dead in its current framing (regret-aware question policy). One delivers measurable but modest value only in a much narrower scope than originally framed (planning-doc enricher).

**Recommendation: do not allocate engineering time to building this as a standalone feature.** The pieces are useful as components of larger workflows; the integrated product as designed has a ceiling too low and a friction cost too high.

---

## The verdict (per assumption)

| Assumption | Original spike threshold | Best result achieved | Verdict |
|---|---|---|---|
| 1. Schema preserves nuance | CDR ≥ 0.90, NRS ≥ 4.0, CPR ≥ 0.70 | task_folder CDR 0.80 (clean), NRS 3.25, CPR 0.72 | **FAIL general, PASS conditional.** Works on tasks that already have planning docs. Floor of 0.23 CDR on raw user prompts is fundamental — Q&A elicitation cannot synthesize information that doesn't exist. |
| 2. Regret-aware policy beats baselines | WRC reduction ≥ 25%, QA increase ≤ 10%, DA non-inferior | Train: clear win (40% WRC reduction). Test: identical to `confidence_only`. | **FAIL on held-out.** Both the original AND gate and the smooth gate variant fail to beat `confidence_only` on a 12-case test. Could be wrong-shape or could be corpus-too-thin. |
| 3. Incremental recompile is sound | IR ≥ 0.95, SDLR ≤ 0.02, IP ≥ 0.80, RS ≥ 0.40 | IR 1.00, SDLR 0.00, IP 0.81, RS 0.69 | **PASS (safety) / partial (efficiency).** IP CI lower bound (0.76) misses 0.80 but the gap is conservative over-invalidation, not a safety bug. Reusable as infra. |

Detailed numbers and case studies live in:
- `docs/experiments/structured-compiler/assumption_{1,2,3}_report.md` (baselines)
- `docs/experiments/structured-compiler/assumption_1_v{2_v3,4,5}_report.md` (A1 follow-ups)
- `docs/experiments/structured-compiler/assumption_2_v2_report.md` (A2 follow-up)
- `docs/experiments/structured-compiler/summary.md` (synthesis)

---

## Honest product read

The strongest result we can defend is: **on tasks that already have `requirements.md` / `solution.md` / `plan.md` artifacts, a 6-7 question elicitation loop lifts decision recall from 0.72 to 0.80** — about 8 percentage points of additional decisions surfaced, paired-clean against a contamination-controlled twin.

That sounds reasonable until you press on it:

- **The user already had planning docs.** The compiler is being asked to enrich something the user already wrote. The marginal value is whatever 0.72 → 0.80 buys them — incremental, not transformative.
- **The friction is real.** 6-7 clarifying questions per task is meaningful overhead. For the median case in this corpus, the unsure rate is 0.5+, meaning the user is being asked to answer questions whose source materials wouldn't help anyway.
- **The threshold the spike was testing against (CDR ≥ 0.90) is still missed by 10 points** on the best subset.
- **The "ask smarter" subsystem is dead.** The whole pitch of "we ask fewer, better questions" doesn't hold up. `confidence_only` is the floor and we couldn't beat it.
- **The general-purpose surface (compile from raw user prompt) is fundamentally bounded.** CDR floor of 0.23 with or without Q&A. No engineering can recover information that doesn't yet exist.

So: **as a standalone feature, I would not ship this.** The math doesn't favor it.

Where it does pay off:
- **As infra inside other workflows.** The schema + Q&A pattern could feed a context-pack builder, a decision-log tracker, or a "compile a decisions.md alongside the requirements.md" workflow — places where the user never directly sees the buckets.
- **The negative result is the deliverable.** $2.22 to learn that the most ambitious framing doesn't work, with reproducible evidence, before anyone built it.
- **A3 (incremental recompile)** is genuinely reusable infrastructure. Safe under tested mutations, conservative over-invalidation, ready to ship behind a feature flag in any product that needs decision-change propagation.

---

## What was tested

| Phase | Experiment | Question being asked | Result |
|---|---|---|---|
| 0 | Dataset assembly | Can we build a 40-case eval corpus from this repo? | Yes. 40 cases across 6 task types; stratified by source (task_folder / git_commit / autosearch_corrections). |
| 1 | A1 baseline | Can the schema hold acceptance-critical nuance? | FAIL overall. But the failure is dominated by thin input on most cases. |
| 2 | A2 baseline | Does `confidence < c AND regret > r` beat `confidence_only`? | FAIL. The AND gate strictly removes axes that `confidence_only` correctly handles. |
| 3 | A3 | Is incremental recompile safe? | PASS safety (IR=1, SDLR=0). RS=0.69. IP=0.81 (CI lower bound misses 0.80 but slack is benign). |
| 5 (v2) | A1 + Q&A substitution | Does replacing planning docs with Q&A help? | NO. task_folder cases collapsed (0.72 → 0.31). Substitution is destructive. |
| 5b (v3) | A1 + Q&A augmentation | Does adding Q&A to existing planning docs help? | Directionally yes (0.72 → 0.84) but partly contamination. |
| 6.1 (v4) | A1 + Q&A + clean twin | What survives when the user-twin is denied access to corrections? | task_folder 0.84 → 0.80. About 2/3 of the v3 lift was real signal; 1/3 was contamination. Thin-input lift was 100% contamination. |
| 6.2 (v5) | A1 + schema surgery | Do the schema recommendations from the baseline report work? | NO. Dropping `decision_candidates` broke CDR by 0.12 (paired p<0.001). Verbatim qualifiers worked perfectly but didn't lift NRS. |
| 6.3 (v2) | A2 + clean labeler + smooth gate | With methodology fixed, does the regret model help? | Mixed. 40% WRC reduction on train, identical to `confidence_only` on n=12 test set. |

---

## Five surprises (the value of running Phase 6)

1. **The schema-surgery recommendations from the baseline A1 report were wrong.** Dropping `decision_candidates` was the first thing I confidently recommended. Phase 6.2 tested it and CDR collapsed by 0.12. The "obvious cleanup" had load-bearing semantics — `decision_candidates` preserves the "committed-with-options" frame the CDR judge looks for. **Lesson:** spike recommendations are hypotheses, not conclusions. Test before adopting.

2. **Contamination was ~1/3 of v3's headline lift, not 30-50% as I estimated.** task_folder CDR was 0.84 contaminated, 0.80 clean. Most of v3's lift was real; the estimate I gave in the chat was too pessimistic. The "ship a planning-doc enricher" recommendation survives, just with a smaller claimed lift than v3 advertised.

3. **Thin-input lift was 100% contamination.** autosearch_corrections cases went 0.23 → 0.30 in v3, then back to 0.23 in v4. With the clean twin honestly saying "unsure" 85% of the time, there is zero recoverable signal from elicited Q&A on one-paragraph user prompts. **Lesson:** before claiming a lift, run the clean variant. A "no-leak" twin should be the default for any RAG/elicitation experiment.

4. **Q&A actively HURTS terse-but-structured input.** git_commit cases regressed from CDR 0.45 → 0.33 once Q&A was added (CI strictly negative). I had expected Q&A to be neutral at worst. It isn't. Adding 6-7 generic-axis questions can drown out tight, on-topic signal. **Lesson:** any future elicitation product should gate Q&A on whether existing input is sparse-with-gaps vs dense-and-focused, not blindly invoke it.

5. **Even "ex-ante" labelers leak via the model's training-data priors.** Phase 6.3's spot-check found 1 of 5 axes where the labeler picked the ground truth value with no in-context evidence — gpt-4o-mini just "knows" what "JSON by default" or "both unit and e2e tests" should be for idiomatic tasks. Methodological splits address explicit leakage; they don't address prior-knowledge leakage. **Lesson:** for tight ex-ante measurement, either use a weaker-priors model or add ablations that hold relevant idioms out of the labeler's context too.

---

## Methodology lessons worth reusing

These are the engineering moves that paid off and should be reused in any future spike:

1. **Sqlite-cached LLM calls keyed by prompt hash.** Total spike spend was $2.22 across 8 experiments because re-runs cost zero. Cache invalidation by changing the prompt is the right default — no manual invalidation, no surprise charges.

2. **Stratified evaluation by input source.** The `task_folder` / `git_commit` / `autosearch_corrections` split surfaced the dominant signal (input richness) that the original go/no-go threshold would have missed.

3. **Deterministic user-twin pattern.** Score candidate confidences offline once, simulate every policy against the same frozen graph. Cheap, paired, bootstrap-friendly. Reusable for any policy-comparison experiment.

4. **70/30 train/test split with pre-registered τ tuning.** Caught the train/test asymmetry in A2.v2 that the original Phase 2 (no split) would have hidden. **Pre-registered splits should be the default for any policy experiment, even on small corpora.**

5. **Pre-registered pass/fail thresholds before running.** Phase 6 used this and the verdicts were unambiguous. Phase 5 didn't, and the v2 substitution-vs-augmentation issue (we tested the wrong hypothesis) would have been caught earlier with explicit pre-registration.

6. **Two-labeler split for ex-ante measurement.** Separate labeler that sees only initial signals from labeler that sees ground truth. Mandatory for any "what would the planner have believed?" experiment. Doesn't address prior-knowledge leakage but eliminates explicit context leakage.

7. **Clone-and-modify scripts, not branching flags.** Every variant (`build_structured_state.py` → `build_structured_state_v5.py`) was a copy with diffs. Made it trivial to A/B and reproduce historic results. The diff in version-control history is the design rationale.

---

## What's still open (recommended follow-up if anyone picks this up)

In priority order:

1. **Expand the corpus 3-5× and re-run A2.v2.** The Phase 6.3 train-set lift (40% WRC reduction) suggests the smooth gate may work. The 12-case test was too thin to settle. Add 100+ task_folder cases from across multiple repos. If train-set lift holds on a larger held-out split, ship the smooth gate; if not, the regret model is wrong-shape and a learned model from prior corrections is the path forward.

2. **For NRS lift: redesign the input contract.** Phase 6.2 confirmed schema and generator are not the bottleneck — NRS judges against feedback.md + corrections that no ex-ante extractor sees. The right experiment is to feed extractors with correction-rich prior sessions from similar tasks. Cost: ~$0.50 with warm caches.

3. **Test the prior-knowledge leakage hypothesis.** Use a weaker-priors model (gpt-3.5-turbo? a non-instruct base model?) as the ex-ante labeler. If train-set lift survives, the labeler-LLM leakage was real. Cost: ~$0.20.

4. **Investigate the git_commit regression.** Why does adding Q&A hurt CDR on cases with commit-message-style input? Hypothesis: generic Q&A axes (scope, error_handling, target_platform) crowd out task-specific axes already present in the baseline state. Worth understanding before any product invokes Q&A unconditionally.

5. **Bank A3.** Don't re-run. If any future product needs decision-change propagation, the dependency-graph walker pattern (artifact: `scripts/test_incremental_recompile.py`) is ready to vendor in.

---

## Cost summary

| Phase | Spend | Notes |
|---|---|---|
| Phase 0 (dataset) | $0.00 | autosearch + grep only |
| Phase 1 (A1 baseline) | $0.28 | gpt-4o-mini extraction/generation + gpt-4o NRS judge |
| Phase 2 (A2 baseline) | $0.02 | gpt-4o-mini labeling; simulation deterministic |
| Phase 3 (A3) | $0.02 | gpt-4o-mini extraction + recompile, temp=0 + seed |
| Phase 5 (A1.v2, Q&A substitution) | $0.30 | Question elicitation + answers + re-extract/regenerate/rescore |
| Phase 5b (A1.v3, Q&A augmentation) | $0.30 | Re-extract/regenerate/rescore with augmented input |
| Phase 6.1 (A1.v4, contamination-clean twin) | $0.33 | Clean user-twin re-runs |
| Phase 6.2 (A1.v5, schema surgery) | $0.63 | New schema fresh extractions + scoring on 2 input regimes |
| Phase 6.3 (A2.v2, clean labeler + smooth gate) | $0.03 | Two-labeler split + τ sweep; mostly deterministic simulation |
| Phase 4 (synthesis, 2 updates) | $0.00 | Writing |
| **Total** | **$2.22** | 7.4% of $30 pool. Re-runs from cache cost $0. |

Cache files: ~20 sqlite databases under `.tmp/experiments/structured-compiler/artifacts/` and `scripts/`. Total ~6 MB. Preserve these if anyone re-runs — they save the entire spike's spend.

---

## Where the evidence lives

```
/home/vscode/src/auto-stack/
├── docs/
│   ├── spikes/                                          <- plans + product decision (in repo)
│   │   ├── structured-compiler-assumptions-validation.md  <- original design
│   │   ├── structured-compiler-phase-6.md                 <- Phase 6 plan
│   │   └── structured-compiler-findings.md                <- THIS DOC (decision record)
│   └── experiments/structured-compiler/                 <- experimental reports (in repo)
│       ├── summary.md                                     <- experimental synthesis
│       ├── assumption_1_report.md                         <- A1 baseline
│       ├── assumption_1_v2_v3_report.md                   <- A1 Q&A
│       ├── assumption_1_v4_report.md                      <- A1 clean twin
│       ├── assumption_1_v5_report.md                      <- A1 schema surgery
│       ├── assumption_2_report.md                         <- A2 baseline
│       ├── assumption_2_v2_report.md                      <- A2 v2
│       ├── assumption_3_report.md                         <- A3
│       └── dataset_summary.md                             <- corpus breakdown
└── .tmp/experiments/structured-compiler/                <- code, data, caches (NOT in repo)
    ├── data/
    │   └── eval_cases.jsonl                              <- 40-case shared corpus
    ├── scripts/
    │   ├── extract_cases.py
    │   ├── build_structured_state{,_v5}.py
    │   ├── build_decision_graph{,_a3,_v2}.py
    │   ├── elicit_questions.py
    │   ├── answer_questions{,_v4}.py
    │   ├── generate_requirements{,_v5}.py
    │   ├── run_a1_v{2,3,4,5}.py
    │   ├── simulate_policies{,_v2}.py
    │   ├── test_incremental_recompile.py
    │   ├── score_assumption_1{,_v5}.py
    │   └── score_assumption_{2,3}{,_v2}.py
    ├── artifacts/
    │   ├── states{,_v2,_v3,_v4,_v5_base,_v5_v3input}/    <- 40 per regime
    │   ├── drafts{,_v2,_v3,_v4,_v5_base,_v5_v3input}/    <- 40 per regime
    │   ├── scores{,_v2,_v3,_v4,_v5_base,_v5_v3input}/    <- 40 per regime
    │   ├── elicited{,_v4}/                               <- 40 Q&A files per twin
    │   ├── decision_graphs{,_v2}.jsonl                   <- A2 graphs
    │   ├── simulations{,_v2}/                            <- A2 policy runs
    │   ├── recompiles/                                   <- A3 mutation runs
    │   └── *_cache.sqlite                                <- prompt-hashed LLM cache
    └── reports/
        ├── README.md                                     <- pointer to docs/experiments/
        └── *_metrics.json / *_records.jsonl              <- raw machine-readable numbers
```

All markdown lives in the repo under `docs/` and survives `.tmp/` cleanup. The experimental code, raw cases, caches, and per-case artifacts live under `.tmp/` and are reproducible from the scripts if lost.
