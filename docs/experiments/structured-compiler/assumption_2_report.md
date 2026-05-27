# Assumption 2 — Regret-Aware Question Policy

**Verdict: PARTIAL**

- Cases: 40
- Decision axes labeled / total: 84 / 85
- Bootstrap resamples: 5000
- Thresholds: CONF_T=0.7, REG_T=0.4
- Total OpenAI cost (labeling only): $0.0207

## Policy comparison

All metrics reported as **point estimate [95% bootstrap CI]**, paired by case_id.

| policy | WRC mean | QA median | DA mean | FCQP median |
|---|---|---|---|---|
| ask_all | 0.000 [0.000, 0.000] | 2.0 [1.0, 3.0] | 1.000 [1.000, 1.000] | 1.0 [1.0, 1.0] |
| frequency_default | 0.015 [0.000, 0.045] | 0.0 [0.0, 0.0] | 0.994 [0.981, 1.000] | 3.0 [2.0, 4.0] |
| confidence_only | 0.000 [0.000, 0.000] | 1.0 [0.0, 1.0] | 1.000 [1.000, 1.000] | 1.5 [1.0, 2.5] |
| regret_aware | 0.007 [0.000, 0.022] | 0.0 [0.0, 1.0] | 0.989 [0.968, 1.000] | 1.5 [1.0, 2.5] |

**WRC** = sum of mismatch×regret over labeled axes (per case, lower better).  
**QA** = questions asked per case (lower better).  
**DA** = decision accuracy over labeled axes per case (higher better).  
**FCQP** = position (1-indexed) of first asked critical (regret>0.5) axis; 
absent critical question penalized as n_axes+1.

## Paired comparison: regret_aware vs confidence_only

Per-case deltas, paired by case_id, bootstrapped.

| delta | point | 95% CI |
|---|---|---|
| Δ WRC (absolute) | 0.0075 | [0.0000, 0.0225] |
| Δ QA (absolute, per case) | -0.100 | [-0.200, -0.025] |
| Δ DA (absolute, per case) | -0.0108 | [-0.0323, 0.0000] |
| WRC relative reduction (cf→ra) | 0.0% | [0.0%, 0.0%] |
| QA relative increase (cf→ra) | -13.4% | [-27.3%, -2.9%] |

**Important caveat on WRC relative reduction:** confidence_only achieved WRC=0 on all 40 cases (it asked every uncertain axis and the user twin always returns ground truth). Dividing by zero baseline is undefined. The 0.0% point estimate above is computed only over bootstrap resamples where the baseline mean was finite-positive; in practice the relative-reduction metric is **not informative for this dataset**. What is informative: the absolute Δ WRC is +0.0075 (regret_aware is WORSE by 0.0075 WRC on average) — the *opposite direction* from a 25% reduction. The first threshold therefore fails by construction.

Positive WRC reduction = improvement. Positive QA increase = MORE questions asked.

## Pass/fail per threshold

| threshold | requirement | result | pass? |
|---|---|---|---|
| WRC reduction ≥ 25% | lower CI bound of rel WRC reduction ≥ 25% | lower bound = 0.0% | FAIL |
| QA increase ≤ 10% | upper CI bound of rel QA increase ≤ 10% | upper bound = -2.9% | PASS |
| DA non-inferior within 2% abs | lower CI bound of Δ DA ≥ -0.02 | lower bound = -0.0323 | FAIL |

## Case studies

### Win — case `sc_009`

regret_aware asked **1** questions vs confidence_only's **2** (Δ QA = -1), WRC unchanged at 0.00.

Axes:
  - `file_discovery_method`  top_conf=0.70, regret=0.60, GT=`filepath.WalkDir`
  - `remote_normalization_method`  top_conf=0.50, regret=0.30, GT=`row.GitRemote`
  - `worktree_directory_method`  top_conf=0.60, regret=0.60, GT=`--git-common-dir`

Interpretation: the axes regret_aware DIDN'T ask have low regret-if-wrong, so even if the default is suboptimal the user is not harmed.

### Loss — case `sc_016`

regret_aware paid +0.30 extra WRC compared to confidence_only, with Δ QA = -1, mismatches 1 vs 0.

Axes:
  - `import_handling_method`  top_conf=0.60, regret=0.60, GT=`ast-grep`
  - `validation_strategy`  top_conf=0.50, regret=0.30, GT=`testing fixtures`
  - `execution_mode`  top_conf=0.90, regret=0.60, GT=`in-band`

Interpretation: an axis with low top-confidence AND low regret was skipped by regret_aware. confidence_only asked it and got the right answer; regret_aware defaulted and paid the regret cost. The regret floor for regret_aware (REG_T=0.4) excluded a genuinely uncertain decision.

## Failure modes and overfitting risks

1. **Ground-truth bleeds into candidate confidences.** The labeler sees both the early signals AND the final outcome (corrections, planning docs). Even with explicit instruction to imagine the outcome is hidden, the labeler likely anchors candidate confidences too close to the ground truth. This artificially compresses the gap confidence_only and regret_aware can exploit. A cleaner design would use an unconditioned planner LLM to score candidate confidences from a held-out subset of the source materials.

2. **Sparse high-regret uncertain axes.** Across 85 axes, only a handful have *both* top_confidence < 0.7 *and* regret > 0.5. The two policies therefore agree on most axes; differences come down to a small number of edge cases.

3. **Synthetic planning_docs for retrospective cases.** Several cases (those with source=git_commit or source=autosearch_corrections) have empty or thin planning_docs. The labeler is forced to infer decision axes from the correction text itself, which biases candidates toward the chosen value.

4. **Threshold sensitivity.** REG_T=0.4 was set ahead of time. The chosen threshold determines how often regret_aware over-skips. A small ablation on this threshold would reveal whether the policy is robust or hand-tuned.

5. **Frequency-default is broken by axis-id collisions.** The frequency policy groups by `axis_id` string, which is LLM-generated and rarely shared across cases. Most axes therefore have a single observation and trivially 'majority' their own value. frequency_default's apparent strong WRC is therefore uninformative — it is essentially `default to top-confidence`. Treat this policy as a sanity baseline, not a serious comparator.

## Notes on metric scope

- Of 85 axes extracted across 40 cases, 84 (98.8%) had a confident ground-truth label (ground_truth_confidence ≥ 0.5). Unlabeled axes are excluded from WRC and DA.
- 9 cases produced 0 decision axes (mostly short retrospective corrections with no planning context). These cases contribute 0 WRC, 0 QA, and are excluded from DA by virtue of having no labeled axes.

## Cost

Total OpenAI spend (labeling only, simulation is deterministic): **$0.0207**
