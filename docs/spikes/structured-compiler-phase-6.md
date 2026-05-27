# Phase 6: Decision-Grade Validation of the Rich-Input + Q&A Regime

Phase 5 + 5b (A1.v2 / A1.v3) updated the spike's verdict from blanket STOP to **STOP for thin-input, KEEP EXPLORING for rich-input + Q&A**. Before any product work, three loose ends need to be tied off. This document plans Phase 6 and writes them up as three self-contained experiments.

## Why these three

Phase 5/5b left three honest caveats:

1. **Contamination risk in v3.** The user-twin saw corrections + final_artifacts when generating Q&A answers. Spot-check showed 3 of 5 sampled answers contained task-specific vocabulary that almost certainly leaked from corrections. The v3 lift on rich-input cases is partly real and partly self-fulfilling. We cannot ship a recommendation until the contamination is controlled.
2. **Schema surgery never tested.** The original A1 report recommended dropping `decision_candidates`, retiring `blast_radius`, and requiring `qualifiers` to carry verbatim source spans. Every run so far used the original schema. Until surgery is tested, the schema's ceiling is unknown.
3. **A2 needs structural rework.** The synthesis recommended (a) holding ground truth out of the confidence labeler and (b) replacing the AND gate with a smooth `expected_regret = (1 - confidence) * regret` score. Phase 5/5b did not touch A2.

A3 is banked and not revisited here.

## Phase 6.1 — A1.v4: contamination-clean user-twin

### Hypothesis
The v3 task_folder CDR lift (0.72 → 0.84) is partly real and partly contamination. With corrections held out of the user-twin's view, **task_folder CDR will land in 0.75–0.85**. If it collapses to 0.55 or below, the v3 result is mostly contamination and the rich-input verdict needs to be revisited.

### Method
- Reuse Phase 5 question generator (already sees only ex-ante signals).
- Modify `answer_questions.py` to feed the user-twin **only** `initial_prompt + planning_docs` (the same input the baseline extractor sees). No corrections. No final_artifacts.
- User-twin must mark `from_ground_truth=false` and answer `"not decided yet"` whenever the answer is not derivable from those two sources.
- Reuse v3's extraction + generation + scoring pipeline. Cache strategy unchanged.

### Pre-registered metrics
- CDR, HAR, NRS, CPR on the full 40-case corpus
- Stratified by source bucket (task_folder, git_commit, autosearch_corrections)
- Paired bootstrap on Δ(v4 − baseline) and Δ(v4 − v3) on CDR
- **Unsure rate is expected to RISE** (no corrections to draw from). Report and correlate with CDR delta.

### Pass/regress thresholds (pre-registered)
- **Decision-grade green:** task_folder CDR ≥ 0.75 AND CDR(v4) ≥ CDR(baseline) overall. → rich-input + Q&A regime is real.
- **Inconclusive:** task_folder CDR in [0.60, 0.75) OR CDR(v4) within ±0.03 of baseline. → contamination explains some of v3's lift but not all.
- **Red:** task_folder CDR < 0.60 OR CDR(v4) materially regresses below baseline. → v3 was contamination-dominated; abandon the rich-input + Q&A path as currently designed.

### Budget
$0.50 max. Warm caches reduce cost; expect $0.20–0.40 in practice.

## Phase 6.2 — A1.v5: schema surgery

### Hypothesis
The three recommended schema changes will lift NRS toward 4.0 without hurting CDR. The current `decision_candidates` bucket is inert (used in 13/40 cases), `blast_radius` always returns medium/high, and `qualifiers` paraphrase instead of quoting. Surgery should:
- not lift CDR (the schema already holds the relevant constraints)
- lift NRS by 0.5–1.0 (verbatim qualifiers preserve exception language)
- not change HAR (already 0)

If NRS does not move, the bottleneck is elsewhere — likely the generator template.

### Method
- Modify `EXTRACTION_PROMPT_TEMPLATE` in a v5 variant:
  - Remove `decision_candidates` entirely. Items that would have lived there go to `soft_preferences` with `confidence < 0.6` and a `value: "TBD"` sentinel.
  - Remove `blast_radius` from `assumptions`. Add a top-level `axis_priorities: {axis: priority_in_[0,1]}` map indicating which axes are worth asking the user about (priority computed by the extractor; downstream uses it but the scorer does not).
  - Modify `qualifiers` schema to require `source_quote` (verbatim) and `source_offset_hint` (≈char range or "from initial_prompt"/"from planning_doc:<path>"). Reject paraphrases at extraction time via prompt instruction.
- Update generator template to render qualifiers as direct quotes.
- Update scoring judges where field-name changes break them — but keep judge prompts otherwise identical.
- Apply v5 to **both baseline-input and v4-input** to disentangle schema effect from input effect.

### Pre-registered metrics
- CDR/HAR/NRS/CPR for `{baseline, v3, v4} × {old_schema, new_schema}`
- 2×3 table; paired bootstrap on Δ-NRS within each input regime
- Track which schema bucket the v5 extractor most uses (sanity check on the surgery)

### Pass/regress thresholds (pre-registered)
- **NRS lifts ≥ 0.5 (paired) on task_folder cases with verbatim qualifiers** → surgery works.
- **CDR holds within ±0.05 of v3** → surgery doesn't break what worked.
- **`axis_priorities` field is populated and non-uniform** → the new field carries signal worth using.

### Budget
$0.80 max. Schema change requires fresh extractions (cache invalidates by prompt hash) but generation/scoring may partially reuse.

## Phase 6.3 — A2.v2: clean labeler + smooth gate

### Hypothesis
The Phase 2 result was confounded by two things: the confidence labeler saw final-state outcomes, and the regret_aware policy used a strict AND gate. With both fixed, `regret_aware` should beat `confidence_only` on WRC by ≥ 25% with non-inferior DA.

### Method
- **Two-labeler split**:
  - `ex_ante_labeler` sees ONLY `initial_prompt + planning_docs` — produces candidate confidence scores.
  - `oracle_labeler` sees corrections + final_artifacts — produces ground truth + regret scores.
  - Cross-contamination is impossible by construction (separate prompts, separate cache namespaces).
- **Smooth-gate policy** `regret_score`:
  - For each axis: `expected_regret = (1 - top_confidence) * regret_if_wrong`
  - Policy asks if `expected_regret > τ`. Sweep τ ∈ {0.1, 0.2, 0.3, 0.4} for tuning curve; pick τ on a 70/30 train/test split.
- Comparators: `ask_all`, `confidence_only` (unchanged threshold 0.7), `frequency_default`, `regret_score`.
- Same metrics: WRC, QA, DA, FCQP. Bootstrap paired by case_id.

### Pre-registered metrics + thresholds
- **WRC reduction (regret_score vs confidence_only) ≥ 25%, lower CI bound > 0** → pass.
- **QA increase ≤ 10%** → pass.
- **DA non-inferior within 2% absolute** → pass.

If all three pass, A2 is rehabilitated. If WRC still goes the wrong direction, the regret model is the wrong shape and any future product needs a learned model trained on prior corrections.

### Budget
$1.50 max. Two-labeler split doubles the labeling cost; the train/test split also re-bills τ tuning.

## Execution plan

Phases 6.1, 6.2, 6.3 are mutually independent and can be executed in parallel by separate sub-agents. Each one is self-contained: reads its own existing artifacts, writes to its own output dirs and report file.

| Phase | Agent | Inputs | Outputs | Budget |
|---|---|---|---|---|
| 6.1 | `a1_v4_clean_twin` | `artifacts/elicited/`, `data/eval_cases.jsonl` | `artifacts/states_v4/`, `artifacts/drafts_v4/`, `artifacts/scores_v4/`, `reports/assumption_1_v4_report.md` | $0.50 |
| 6.2 | `a1_v5_schema_surgery` | same | `artifacts/states_v5/`, `artifacts/drafts_v5/`, `artifacts/scores_v5/`, `reports/assumption_1_v5_report.md` | $0.80 |
| 6.3 | `a2_v2_smooth_gate` | `artifacts/decision_graphs.jsonl`, `data/eval_cases.jsonl` | `artifacts/decision_graphs_v2.jsonl`, `artifacts/simulations_v2/`, `reports/assumption_2_v2_report.md` | $1.50 |

After all three complete, update `reports/summary.md` with the consolidated verdict.

## Stop criteria

If Phase 6.1 returns RED (task_folder CDR < 0.60), abort the rich-input + Q&A direction entirely. Phase 6.2 and 6.3 still complete for completeness but the product surface conclusion is settled negative.

If Phase 6.1 returns GREEN and Phase 6.2 confirms NRS lift, the synthesis should recommend a **scoped product surface**: a planning-doc enricher invoked only when `requirements.md` already exists; not a general-purpose compiler.

If Phase 6.3 also returns GREEN, the question-policy subsystem is salvageable and can ship alongside the enricher.
