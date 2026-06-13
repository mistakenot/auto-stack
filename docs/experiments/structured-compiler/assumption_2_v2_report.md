---
hash: "fbef98ff"
id: "0812e142"
read_when: "evaluating labeling strategies and smooth gate designs for structured compiler assumption 2"
summary: "Partial verdict on the two-labeler split and smooth expected-regret gate: regret_score identical to confidence_only on the test set; corpus too thin to distinguish."
title: "Assumption 2 v2 — Clean Labeler and Smooth Gate"
---

# Assumption 2 v2 — Clean Labeler + Smooth Gate

**Verdict: PARTIAL**

- Cases: 40
- Axes proposed by ex_ante labeler: 120
- Axes kept after oracle filter: 94  (dropped 26)
- Axes with confident ground truth: 91
- Bootstrap resamples: 5000
- Train/test split: 28/12 (seed=42, train_frac=0.7)
- Chosen tau: **0.15** (min train WRC among DA-non-inferior taus (DA penalty <= 0.02))
- Total OpenAI cost (both labelers): $0.0291

## Tuning curve (train set)

| tau | WRC mean | QA mean | DA mean | DA penalty vs CF |
|---|---|---|---|---|
| 0.10 | 0.0321 | 0.82 | 0.964 | 0.0000 |
| 0.15 | 0.0321 | 0.50 | 0.964 | 0.0000 |
| 0.20 | 0.0714 | 0.21 | 0.905 | 0.0119 |
| 0.25 | 0.1107 | 0.11 | 0.879 | 0.0373 |
| 0.30 | 0.1286 | 0.04 | 0.863 | 0.0532 |
| 0.35 | 0.1286 | 0.04 | 0.863 | 0.0532 |
| 0.40 | 0.1286 | 0.04 | 0.863 | 0.0532 |

Selection rule: among taus with DA penalty <= 0.02 vs confidence_only on TRAIN, pick min WRC; tiebreak by QA, then tau.

## Per-policy metrics on TEST split

All metrics reported as **point estimate [95% bootstrap CI]**, paired by case_id.

| policy | WRC mean | QA median | DA mean | FCQP median |
|---|---|---|---|---|
| ask_all | 0.000 [0.000, 0.000] | 3.0 [2.5, 3.5] | 1.000 [1.000, 1.000] | 1.0 [1.0, 1.0] |
| frequency_default | 0.117 [0.000, 0.283] | 0.0 [0.0, 0.0] | 0.939 [0.848, 1.000] | 4.0 [4.0, 5.0] |
| confidence_only | 0.333 [0.100, 0.583] | 1.0 [0.0, 1.5] | 0.780 [0.614, 0.924] | 3.0 [1.0, 4.0] |
| regret_score | 0.333 [0.100, 0.583] | 1.0 [0.0, 1.5] | 0.780 [0.614, 0.924] | 3.0 [1.0, 4.0] |

## Per-policy metrics on TRAIN split (for transparency)

| policy | WRC mean | QA median | DA mean | FCQP median |
|---|---|---|---|---|
| ask_all | 0.000 [0.000, 0.000] | 2.0 [2.0, 3.0] | 1.000 [1.000, 1.000] | 1.0 [1.0, 1.0] |
| frequency_default | 0.032 [0.000, 0.086] | 0.0 [0.0, 0.0] | 0.979 [0.945, 1.000] | 3.5 [3.0, 4.0] |
| confidence_only | 0.054 [0.000, 0.139] | 0.0 [0.0, 0.0] | 0.917 [0.786, 1.000] | 3.0 [2.5, 4.0] |
| regret_score | 0.032 [0.000, 0.096] | 0.0 [0.0, 0.5] | 0.964 [0.893, 1.000] | 3.0 [2.0, 3.5] |

## Per-policy metrics on FULL dataset (sanity)

| policy | WRC mean | QA median | DA mean | FCQP median |
|---|---|---|---|---|
| ask_all | 0.000 [0.000, 0.000] | 3.0 [2.0, 3.0] | 1.000 [1.000, 1.000] | 1.0 [1.0, 1.0] |
| frequency_default | 0.057 [0.007, 0.123] | 0.0 [0.0, 0.0] | 0.965 [0.928, 0.994] | 4.0 [3.0, 4.0] |
| confidence_only | 0.138 [0.052, 0.250] | 0.0 [0.0, 1.0] | 0.870 [0.766, 0.953] | 3.0 [3.0, 4.0] |
| regret_score | 0.123 [0.037, 0.233] | 0.0 [0.0, 1.0] | 0.901 [0.815, 0.971] | 3.0 [2.0, 3.0] |

## Paired bootstrap: regret_score vs confidence_only (TEST set)

| delta | point | 95% CI |
|---|---|---|
| Delta WRC (absolute) | 0.0000 | [0.0000, 0.0000] |
| Delta QA (absolute, per case) | 0.083 | [-0.167, 0.333] |
| Delta DA (absolute, per case) | 0.0000 | [0.0000, 0.0000] |
| WRC relative reduction (cf->rs) | 0.0% | [0.0%, 0.0%] |
| QA relative increase (cf->rs) | 12.4% | [-20.0%, 66.7%] |

(For train-set comparison see metrics JSON.)

## Pass/fail vs pre-registered thresholds (TEST set)

| threshold | requirement | result | pass? |
|---|---|---|---|
| WRC reduction >= 25% | lower CI bound of rel WRC reduction >= 25% | lower bound = 0.0% | FAIL |
| QA increase <= 10% | upper CI bound of rel QA increase <= 10% | upper bound = 66.7% | FAIL |
| DA non-inferior within 2% abs | lower CI bound of Delta DA >= -0.02 | lower bound = 0.0000 | PASS |

## Sensitivity to tau

WRC, QA and DA on test set at chosen tau and one step in each direction:

| tau | test WRC mean | test QA total | test DA mean | WRC vs chosen | DA delta vs CF |
|---|---|---|---|---|---|
| **0.15** (chosen) | 0.3333 | 12 | 0.780 | 0.0000 | +0.0000 |
| 0.10 | 0.3333 | 22 | 0.780 | +0.0000 | +0.0000 |
| 0.20 | 0.3333 | 10 | 0.780 | +0.0000 | +0.0000 |

If WRC delta vs chosen is small (|delta| < 0.005) across neighbors, the verdict is robust to tau choice. If WRC swings substantially across one step, the policy is hand-tuned and the result should be treated as overfit.

## Contamination audit (spot-check 5 axes)

For each sampled axis, the ex_ante labeler had to produce confidence based on initial_prompt + planning_docs alone. If the top candidate consistently matches the ground-truth value (especially when the planning text does not explicitly name it), the labeler is still anchoring on the outcome and the methodology fix is incomplete.

**Summary: 3 / 5 sampled axes have ex-ante top candidate == ground truth.**

### `sc_015` :: `effectiveness_measurement`

- Description: The method used to assess whether the selected skills led to effective work.
- Top ex-ante candidate: `outcome_based` (conf=0.40 if any)
- Ground truth: `outcome_based` (oracle confidence=0.60)
- Match: YES (potential leakage)
- Regret if wrong: 0.50
- All candidates:
  - `outcome_based` conf=0.40
  - `time_to_completion` conf=0.35
  - `quality_of_output` conf=0.25
- Oracle evidence: The user is interested in whether the skill led to effective work, indicating a focus on outcomes.

### `sc_038` :: `search_scope`

- Description: The scope of the autosearch functionality within git.
- Top ex-ante candidate: `local_repository` (conf=0.40 if any)
- Ground truth: `` (oracle confidence=0.00)
- Match: NO
- Regret if wrong: 0.00
- All candidates:
  - `local_repository` conf=0.40
  - `remote_repository` conf=0.35
  - `both` conf=0.25
- Oracle evidence: No specific search scope was indicated in the corrections or final artifacts.

### `sc_006` :: `test_coverage_scope`

- Description: Determines the extent of test coverage for the implemented features.
- Top ex-ante candidate: `both_unit_and_e2e_tests` (conf=0.50 if any)
- Ground truth: `both_unit_and_e2e_tests` (oracle confidence=0.90)
- Match: YES (potential leakage)
- Regret if wrong: 0.80
- All candidates:
  - `both_unit_and_e2e_tests` conf=0.50
  - `unit_tests_only` conf=0.30
  - `e2e_tests_only` conf=0.20
- Oracle evidence: Golden file assertions and e2e test assertions were updated, indicating both unit and e2e tests are covered.

### `sc_015` :: `skill_selection_criteria`

- Description: The criteria used to determine if the right skills are being picked up and used.
- Top ex-ante candidate: `relevance_to_task` (conf=0.50 if any)
- Ground truth: `relevance_to_task` (oracle confidence=0.70)
- Match: YES (potential leakage)
- Regret if wrong: 0.60
- All candidates:
  - `relevance_to_task` conf=0.50
  - `historical_performance` conf=0.30
  - `user_feedback` conf=0.20
- Oracle evidence: The user focused on why a specific skill isn't showing up more and suggested checking the ETL data for tracking issues.

### `sc_004` :: `token_budget_enforcement`

- Description: The strategy for enforcing the token budget during context pack generation.
- Top ex-ante candidate: `deterministic_estimation` (conf=0.70 if any)
- Ground truth: `dynamic_adjustment` (oracle confidence=0.90)
- Match: NO
- Regret if wrong: 0.60
- All candidates:
  - `deterministic_estimation` conf=0.70
  - `dynamic_adjustment` conf=0.30
- Oracle evidence: Fixed by recomputing derived sections before each budget check via refreshDerived().

**Interpretation guide.**
- If the planning text *explicitly named* the GT value: the match is correct and non-leaky; high confidence is appropriate.
- If the planning text was silent but the ex-ante labeler still confidently picked the right answer: prior-knowledge leakage (the LLM "knows the right answer" from training data, not from in-context signals). This is a softer form of contamination than the v1 design (which literally saw corrections) but it still compresses the gap policies can exploit.

## Failure modes still present

1. **Prior-knowledge leakage by the labeler LLM.** Even with corrections and final artifacts withheld, gpt-4o-mini brings strong priors over "the right way" to implement common tasks. For cases that match training-data idioms (CLI flag conventions, JSON-by-default, etc.) the ex_ante labeler will still anchor candidate confidences near the true outcome. See the contamination audit above.

2. **Thin corpus on high-regret uncertain axes.** Even with smooth gating, the interesting region is axes with `(1 - top_conf) * regret > tau`, which remains sparse on a 40-case dataset. Detection power is limited.

3. **Oracle regret is a single LLM call's opinion.** Blast-radius scoring is self-judged by one model from the corrections text. Real-world product use would need either deterministic blast-radius signals (irreversibility, schema files touched, public API surface delta) or a learned model.

4. **Train/test split is small.** 28/12 case split. Test-set CIs are wide.

## Verdict interpretation

Read carefully — the train/test asymmetry is the headline:

- **On TRAIN (28 cases): regret_score wins.** WRC 0.0321 vs cf 0.0536, DA 0.964 vs cf 0.917, comparable QA. The smooth-gate idea is plausibly working in the right direction.
- **On TEST (12 cases): the two policies are identical.** WRC 0.3333 for both; per-case inspection shows that on every test case regret_score and confidence_only end up with identical labeled-mismatch counts. They differ only in *which axes they ask about*, not in the outcomes those choices produce. The pre-registered relative-WRC reduction is therefore exactly 0%.

Two interpretations are consistent with this pattern:

- **Regret model is the wrong shape.** Multiplicative `(1-conf)*regret` produces identical decisions to confidence-only on the test set's specific axes. Alternatives worth exploring: a regret floor (`asked iff conf<tau1 AND regret>tau2`) tuned per case bucket, or a learned model trained on prior corrections.
- **Corpus is too thin to detect a difference.** 12 test cases, of which 9 have at least one labeled axis, of which only ~3-4 have a high expected_regret. Confidence intervals are inherently wide; the train signal that *is* present may be overfit noise. A larger or more decision-dense corpus is the cheapest way to disambiguate.

## Cost

Total OpenAI spend (two-labeler split): **$0.0291**
