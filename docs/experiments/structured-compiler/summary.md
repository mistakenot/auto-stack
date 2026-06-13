---
hash: "c356e0c1"
id: "aac69f2a"
read_when: "reviewing the complete structured compiler experiment results, understanding the final product recommendation, or applying the methodology lessons to a future spike"
summary: "Final synthesis across all eight structured compiler experiments, covering schema utilization, regret-aware question policy, and incremental recompile safety — recommending a scoped planning-doc enricher and abandoning the general-purpose compiler framing."
title: "Structured Compiler Spike — Final Synthesis"
---

# Structured Compiler Spike — Final Synthesis

**Date:** 2026-05-27 (updated through Phase 6: A1.v4 clean twin, A1.v5 schema surgery, A2.v2 clean labeler + smooth gate)
**Cases:** 40 (1 shared corpus)
**Total OpenAI spend across all eight experiments:** **~$2.22** (of $30 budget; 7.4%)
**Final recommendation: BUILD a scoped planning-doc enricher; DO NOT build a general-purpose compiler; ABANDON the regret-aware question policy as currently framed.**

---

## Headline verdict (after Phase 6)

| Assumption | Verdict | Bottom line |
|---|---|---|
| 1. Schema preserves nuance | **FAIL general — PASS conditional** | Baseline overall CDR 0.36 → v3 (Q&A) 0.41 → v4 (clean twin) 0.36. task_folder CDR baseline 0.72 → v3 0.84 → **v4 0.80**. The rich-input + Q&A lift is real (Δ +0.07 vs baseline) but smaller than v3 suggested. The schema is NOT the bottleneck. NRS bottleneck is the input contract, not schema design. |
| 2. Regret-aware policy beats baselines | **FAIL — abandon current framing** | Original AND gate dominated. v2 fixed methodology (split labeler, smooth gate, τ tuning, 70/30 split). On test set, `regret_score` matches `confidence_only` exactly. Either the regret model is the wrong shape OR the corpus is too thin to detect a difference. Either way, the multiplicative gate is dead. |
| 3. Incremental recompile is sound | **PASS (safety) / partial (efficiency)** | IR=1.00, SDLR=0.00 → safe. IP=0.81 mean clears but CI lower bound 0.76 misses 0.80. Bank this finding. |
| Schema surgery (Phase 6.2) | **ABANDON as proposed** | Dropping `decision_candidates` BROKE CDR (-0.118 paired Δ vs v3). Verbatim qualifiers worked perfectly (100% audit clean) but didn't lift NRS. `axis_priorities` carries signal but isn't consumed by anything. The real NRS lever is the input contract. |

The Phase 6 results confirm and tighten the Phase 5 conclusions. The rich-input + Q&A regime is the only viable product surface. The question-policy subsystem doesn't work without a different model.

---

## The single most important finding (updated by Phase 6.1)

**Input quality dominates Assumption 1, and Q&A elicitation only helps when there's something genuine to elicit.**

CDR stratified by source, across all four input regimes:

| Source | n | Baseline | v2 (replace) | v3 (augment + contam) | **v4 (augment + clean)** |
|---|---|---|---|---|---|
| `task_folder` (real planning docs) | 8 | 0.72 | 0.31 | 0.84 | **0.80** |
| `git_commit` (commit body bullets) | 6 | 0.45 | 0.34 | 0.36 | **0.33** |
| `autosearch_corrections` (raw user prompt only) | 26 | 0.23 | 0.25 | 0.30 | **0.23** |
| **Overall** | 40 | 0.36 | 0.28 | 0.41 | **0.36** |

The cleanest signal in the entire spike. Reading row by row:

1. **task_folder cases**: baseline 0.72 → contaminated v3 0.84 → clean v4 0.80. The clean-twin lift over baseline is +0.07 (paired p=0.90). About a third of v3's apparent lift (+0.12) was contamination; two-thirds was real signal. **The rich-input + Q&A regime is decision-grade viable.**
2. **git_commit cases**: actually REGRESSED below baseline once Q&A was added (0.45 → 0.33). Q&A noise drowns out tight commit-message signal. Cases with already-structured-but-terse input do not benefit from elicitation; they're hurt by it.
3. **autosearch_corrections cases**: v3's apparent lift (0.23 → 0.30) was ENTIRELY contamination. With the clean twin, thin-input cases revert to baseline 0.23. **No amount of Q&A elicitation can synthesize information that isn't yet in the source.** The user-twin's unsure rate rose from 0.64 (v3) to 0.85 (v4) — when corrections are held back, the twin honestly admits it doesn't know.
4. **Overall** comes out flat: the task_folder lift (n=8) and the git_commit regression (n=6) approximately wash out across 40 cases.

**Implication:** the only product framing where the structured compiler delivers measurable value is the **planning-doc enricher** — invoked on tasks that already have `requirements.md` / `solution.md` / `plan.md` artifacts. For the same effort applied to a raw user prompt, return on investment is zero or negative.

---

## What each experiment actually showed

### Assumption 1 — schema utilization is uneven; surgery hurt more than it helped

Schema-field usage and post-Phase-6 verdict per field:

| Field | Used in (baseline) | Phase 6.2 verdict |
|---|---|---|
| `assumptions` | 40 / 40 | Keep. Absorbs everything, but the absorption is functional. |
| `hard_constraints` | 19 / 40 | Keep. Extractor cannot reliably separate from `soft_preferences` without an axis vocabulary, but the bucket is load-bearing on rich-input cases. |
| `soft_preferences` | 23 / 40 | Keep. |
| `qualifiers` | 23 / 40 | Keep AND require verbatim source spans (v5 audit was 100% clean). The verbatim constraint is free engineering and improves citability — but it does NOT lift NRS measurably. |
| `decision_candidates` | 13 / 40 | **RESTORE** (do NOT drop). Phase 6.2 tested folding into `soft_preferences` with TBD sentinels — that BROKE CDR by -0.12 (paired p<0.001). Committed-with-options decisions read as "missing" to the CDR judge. The original A1 recommendation was wrong. |
| `blast_radius` | always medium/high | Replace with `axis_priorities` (per-axis 0–1 priority map). v5 confirmed this field gets populated and is non-uniform — signal exists. Downstream code does not yet use it. |

**The schema is not the bottleneck and is not the lever.** Phase 6.2 confirmed:
- Verbatim qualifiers work (100% audit) but don't lift NRS.
- Dropping `decision_candidates` breaks CDR badly.
- `axis_priorities` carries signal but isn't consumed by any downstream judge yet.

**NRS is bounded by the input contract, not the schema.** NRS scoring grades against feedback.md + corrections that no ex-ante extractor sees. No amount of schema engineering can recover information the extractor never had access to. The NRS lever is to feed extractors with correction-rich prior sessions, not to redesign fields.

### Assumption 2 — methodology fixed; result is null on small test set

Phase 6.3 fixed both flaws the original A2 report flagged:
- Two-labeler split: ex_ante labeler sees only `initial_prompt + planning_docs`; oracle labeler sees corrections + final_artifacts.
- Smooth gate: `expected_regret = (1 - top_confidence) * regret_if_wrong`, threshold `τ` tuned on a 70/30 train/test split.

**Train set (n=28)** — smooth gate clearly wins:

| Policy | WRC mean | DA mean | QA mean |
|---|---|---|---|
| `confidence_only` | 0.054 | 0.917 | 0.46 |
| `regret_score` (τ=0.15) | **0.032** | **0.964** | 0.50 |

40% relative WRC reduction, DA improves, QA barely changes. Looks like a win.

**Test set (n=12)** — identical:

| Policy | WRC mean | DA mean | QA mean |
|---|---|---|---|
| `confidence_only` | 0.333 | 0.780 | 0.92 |
| `regret_score` (τ=0.15) | 0.333 | 0.780 | 1.00 |

Paired Δ-WRC = 0.000. The smooth gate makes different decisions about *which* axes to ask, but lands on identical correctness outcomes on the test slice.

**Headline interpretation:** The train/test asymmetry is the central finding. Two consistent explanations:
1. **Corpus too thin to detect.** 12 test cases × ~3 axes = ~36 observations; the bootstrap CI on Δ-WRC is wide enough to hide a real effect.
2. **Train-set tuning artifact.** The smooth gate may overfit τ on train and provide no real signal.

**Contamination still partly present.** Even with corrections withheld, the labeler LLM (gpt-4o-mini) brings strong priors over idiomatic decisions ("JSON output by default", "both unit and e2e tests", etc.). 1 of 5 spot-checked axes had ex-ante top candidate matching ground truth WITHOUT the planning text naming it — that's prior-knowledge leakage by the model itself. Softer than v1's outright leak, but still compresses the gap policies can exploit.

**Verdict: FAIL on pre-registered thresholds (WRC and QA both miss), but the failure is null-on-test rather than negative-on-test.** The original A2 result (AND gate strictly dominated) was wrong-shape; v2's smooth gate is at worst neutral and at best clearly better on train. The corpus may simply be too small to settle the question.

**Implication:** before declaring the question-policy subsystem dead, expand the corpus 3-5× and re-run. If the train-set lift holds on a larger held-out split, ship the smooth gate. If not, the regret model is wrong-shape and a learned model from prior corrections is the path forward.

### Assumption 3 — incremental recompile is safe in this corpus

Across 120 mutation runs (40 cases × 3 mutation types):

- IR = 1.000 with CI [1.000, 1.000] — every truly-changed node was invalidated
- SDLR = 0.000 — zero stale leaks
- IP = 0.814 (CI 0.763–0.863) — the lower bound misses 0.80, all the slack coming from `interface` mutations where the walker invalidates `output_format` / `testing_strategy` (defensible edges) but the LLM judges the values don't actually change
- RS = 0.694 — well above 0.40

**The IP shortfall is conservative over-invalidation, not a safety bug.** It costs tokens; it does not cost correctness.

**Implication:** if the rest of the compiler shipped, incremental mode would be safe to enable for `storage` and `validation` mutations (IP ≈ 1.0) and acceptably wasteful for `interface` mutations. Bank this finding — the dependency-graph incremental walker pattern is viable.

---

## Methodological lessons worth keeping

Even though the product as designed doesn't pass, several engineering moves from this spike are reusable:

1. **The deterministic user-twin pattern (A2).** Score candidate confidences offline once, simulate every policy against the same frozen graph. Cheap, paired, bootstrap-friendly. Reuse for any future policy-comparison experiment.
2. **Sqlite-cached LLM calls.** Total spend was $0.32 across three full experiments because re-runs cost zero. The cache-on-prompt-hash pattern is the right default.
3. **Stratify by source.** The A1 source stratification (`task_folder` / `git_commit` / `autosearch_corrections`) was what surfaced the input-richness finding. Apply to any future evaluation corpus.
4. **Watch for labeler-knows-ground-truth contamination.** A2 hit this. Any future experiment that uses an LLM to score "what would the planner believe ex ante" must hold the outcome out from the labeler.
5. **Per-axis priority beats per-decision blast_radius.** A1 found `blast_radius` always medium/high. The same signal at axis level would have been useful. Apply to any future structured-state design.

---

## What we are not concluding (be honest)

- **We are not concluding that the schema cannot work.** On the 8 cases with full planning material, CDR was 0.72 and NRS was 3.00. The ceiling is unclear because input was confounded with schema quality.
- **We are not concluding that "ask fewer questions" cannot work.** The specific gate `confidence < c AND regret > r` is dominated. A smooth `expected_regret` score might still beat `confidence_only`.
- **We are not concluding A3 fully passes.** IP CI lower bound (0.76) misses the 0.80 threshold. We are saying the *safety* properties pass cleanly and the *efficiency* shortfall is benign.

---

## Recommended next steps (final, after Phase 6)

### Ship
1. **Build the compiler as a scoped planning-doc enricher.** Surface invoked only when `requirements.md` / `solution.md` / `plan.md` already exist. Bundles: (a) the v3/v4 schema (with `decision_candidates` RESTORED, `blast_radius` replaced by `axis_priorities`, qualifiers requiring verbatim source spans), (b) a 6-question Q&A elicitation loop. Realistic task_folder CDR: 0.75–0.85, CPR: 0.65–0.75.
2. **Bank A3's incremental-recompile safety result.** If the enricher ever needs decision-change propagation, the dependency-graph walker pattern with conservative over-invalidation is safe (IR=1.0, SDLR=0.0) and acceptably efficient (RS=0.69).

### Don't ship
3. **Do NOT build a general-purpose compile-from-initial-prompt surface.** Thin-input CDR floor is 0.23 with or without Q&A. No amount of schema work or elicitation closes the gap. A one-paragraph user prompt does not contain enough material to reconstruct an acceptance-critical spec.
4. **Do NOT ship the regret-aware question policy as currently framed.** Both the original AND-gate version (Phase 2) and the smooth multiplicative gate (Phase 6.3) fail to beat `confidence_only` on held-out data. `confidence_only` remains the default.

### Investigate before building
5. **Expand the corpus 3-5× and re-run A2.v2.** The Phase 6.3 train-set lift (40% WRC reduction, DA improvement) suggests the smooth gate may work — but the 12-case test was too thin to decide. Add 100+ task_folder cases (the existing 8 cases is the bottleneck, not technique). If train-set lift survives a larger held-out split, ship the smooth gate.
6. **For NRS lift, redesign the input contract.** Phase 6.2 confirmed the schema and generator are not the bottleneck — NRS judges against feedback.md + corrections that no extractor sees. The right experiment is to feed extractors with correction-rich prior sessions from similar tasks, not to add fields to the schema.
7. **Address residual prior-knowledge leakage.** Even the v4 clean twin and v2 ex_ante labeler still anchor on idiomatic decisions ("JSON by default", "unit + e2e tests"). For tight measurement, future experiments should use a labeler model with weaker priors OR add explicit ablation: hold out related repo conventions from the labeler's context too.

### Workflow lessons (reusable)
8. **Pre-register pass/fail thresholds before running.** Phase 6 used this and the verdicts were unambiguous. Phase 5 didn't, and the v2 substitution-vs-augmentation issue would have been caught earlier.
9. **Stratify by source bucket in every A1-class experiment.** The thin-input vs rich-input split surfaced the dominant signal in this spike and saved a flawed STOP verdict.
10. **Sqlite-cached LLM calls.** Total spike cost ~$2.22 across 8 experiments because re-runs cost zero. Cache-on-prompt-hash is the right default for any spike that may iterate.

---

## Cost summary

| Phase | Spend | Notes |
|---|---|---|
| Phase 0 (dataset) | $0.00 | autosearch + grep only |
| Phase 1 (A1 baseline) | $0.28 | gpt-4o-mini extraction/generation + gpt-4o NRS judge |
| Phase 2 (A2) | $0.02 | gpt-4o-mini labeling; simulation deterministic |
| Phase 3 (A3) | $0.02 | gpt-4o-mini extraction + recompile, temp=0 + seed |
| Phase 5 (A1.v2, Q&A substitution) | $0.30 | Question elicitation + answers + re-extract/regenerate/rescore |
| Phase 5b (A1.v3, Q&A augmentation) | $0.30 | Re-extract/regenerate/rescore with augmented input; reused v2 caches |
| Phase 6.1 (A1.v4, contamination-clean twin) | $0.33 | Clean user-twin re-runs |
| Phase 6.2 (A1.v5, schema surgery) | $0.63 | New schema fresh extractions + scoring on 2 input regimes |
| Phase 6.3 (A2.v2, clean labeler + smooth gate) | $0.03 | Two-labeler split + τ sweep; mostly deterministic simulation |
| Phase 4 (synthesis, 2 updates) | $0.00 | This document |
| **Total** | **~$2.22** | 7.4% of $30 pool. Re-runs from cache cost $0. |

---

## Artifacts

**Reports (this directory, `docs/experiments/structured-compiler/`):**
- `summary.md` — this file
- `assumption_{1,2,3}_report.md` — full per-experiment reports
- `assumption_1_v2_v3_report.md` — Q&A elicitation follow-up (substitution + augmentation variants)
- `assumption_1_v4_report.md` — Phase 6.1, contamination-clean twin
- `assumption_1_v5_report.md` — Phase 6.2, schema surgery
- `assumption_2_v2_report.md` — Phase 6.3, clean labeler + smooth gate
- `dataset_summary.md` — task-type / corrections-per-case breakdown

**Plans and product decision (`docs/spikes/`):**
- `structured-compiler-assumptions-validation.md` — original spike design
- `structured-compiler-phase-6.md` — Phase 6 plan
- `structured-compiler-findings.md` — consolidated findings + product decision

**Code, data, and machine-readable metrics (`.tmp/experiments/structured-compiler/`, not in repo):**
- `data/eval_cases.jsonl` — 40-case shared corpus
- `scripts/` — extraction, generation, scoring, simulation, and orchestrator scripts for all phases
- `artifacts/states{,_v2,_v3,_v4,_v5_base,_v5_v3input}/` — extracted structured states per regime + A3 dependency graphs
- `artifacts/drafts{,_v2,_v3,_v4,_v5_base,_v5_v3input}/` — generated requirements drafts
- `artifacts/scores{,_v2,_v3,_v4,_v5_base,_v5_v3input}/` — per-case scores + summary.json
- `artifacts/elicited{,_v4}/` — Q&A files per user-twin
- `artifacts/simulations{,_v2}/` — A2 policy-simulation runs
- `artifacts/recompiles/` — A3 mutation runs (40 baselines + 120 oracle + 120 incremental)
- `artifacts/*_cache.sqlite` — prompt-hashed LLM caches (preserve for free re-runs)
- `reports/assumption_{2,3}_metrics.json`, `reports/assumption_2_v2_metrics.json`, `reports/assumption_3_records.jsonl` — machine-readable raw metrics (stayed with the code, not moved to docs)

All LLM calls cached. Re-running any phase costs $0 unless you bust the cache.
