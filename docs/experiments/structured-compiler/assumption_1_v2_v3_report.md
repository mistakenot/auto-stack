---
hash: "bb7ba1be"
id: "4d6f9efc"
read_when: "evaluating Q&A augmentation strategies for structured compiler experiments or interpreting assumption 1 results"
summary: "Follow-up spike on Q&A augmentation for the structured compiler: v3 (augment-not-replace) lifts task_folder CDR from 0.72 to 0.84, with contamination caveats noted."
title: "Assumption 1 — Q&A Augmentation Report (v2 + v3)"
---

# Assumption 1 — Q&A Augmentation Report (v2 + v3)

**Spike:** Structured Compiler — Assumption 1 follow-up.
**Date:** 2026-05-27
**Cases:** 40 (one shared corpus)
**Models:** extraction + generation + most judges = `gpt-4o-mini`. NRS judge = `gpt-4o`.
**Pipeline:** baseline vs **v2** (`prompt + Q&A`, planning docs **replaced**) vs **v3** (`prompt + planning_docs + Q&A`, planning docs **augmented**).

---

## What this experiment tested

The original Assumption-1 evaluation hinged on input richness: cases with rich planning docs (`task_folder`, n=8) achieved CDR 0.72; cases with only an initial prompt (`autosearch_corrections`, n=26) collapsed to CDR 0.23. The natural follow-up: **does adding elicited clarifying Q&A to the extractor input lift CDR — especially on thin-input cases — without destroying performance on rich-input cases?**

Phase 5 (v2) tried to test this but had a methodological flaw: v2 replaced planning_docs with Q&A entirely. For task_folder cases that stripped real input, and CDR collapsed from 0.72 → 0.31 — not because Q&A is bad, but because we removed a load-bearing source. Phase 5b (v3) fixes the flaw by appending Q&A to the planning-docs slot instead of overwriting it.

---

## Headline metrics

| Metric | Threshold | Baseline | v2 (replace) | **v3 (augment)** |
|---|---|---|---|---|
| **CDR** (Critical Decision Recall) | ≥ 0.90 | 0.359 [0.262, 0.461] | 0.275 [0.206, 0.357] | **0.413 [0.318, 0.519]** |
| **HAR** (Hidden Assumption Rate)  | ≤ 0.10 | 0.000 | 0.000 | 0.000 |
| **NRS** (Nuance Retention, 1–5)   | ≥ 4.0  | 2.43 [2.28, 2.58] | 2.50 [2.35, 2.65] | **2.58 [2.40, 2.78]** |
| **CPR** (Correction Predictability) | ≥ 0.70 | 0.669 [0.537, 0.801] | 0.583 [0.454, 0.713] | **0.725 [0.611, 0.833]** |

All values are mean and 95% bootstrap CI on n=40 (CPR on n=36 — four cases have no corrections).

### Paired bootstrap on Δ(v3 − baseline) and Δ(v3 − v2)

| Metric | Δ(v3 − baseline) [95% CI] | p(Δ > 0) | Δ(v3 − v2) [95% CI] | p(Δ > 0) |
|---|---|---|---|---|
| CDR | **+0.054** [−0.019, +0.131] | 0.928 | **+0.138** [+0.070, +0.213] | **1.000** |
| NRS | +0.150 [+0.000, +0.325] | 0.962 | +0.075 [−0.025, +0.225] | 0.839 |
| CPR | +0.056 [−0.116, +0.227] | 0.728 | +0.141 [+0.007, +0.280] | 0.979 |

- **v3 unambiguously beats v2 on CDR and CPR.** The methodology fix worked: augment > replace.
- **v3 has a small directional lift over baseline on CDR** (Δ=+0.054, p=0.93). The 95% CI brushes zero, so the average lift is suggestive but not conclusive on the full 40-case corpus.
- **NRS shows a small lift** (Δ=+0.15, p=0.96) suggesting Q&A surfaces qualifier-like nuance.
- **CPR moves in the right direction but not significantly** (p=0.73).

---

## Stratified by source bucket — the key plot

This is where the directional verdict becomes clear.

### `task_folder` (n=8, rich input)

| Metric | Baseline | v2 | v3 | Δ(v3 − base) | p(>0) | Δ(v3 − v2) | p(>0) |
|---|---|---|---|---|---|---|---|
| CDR | 0.72 | 0.31 | **0.84** | **+0.12** [+0.04, +0.19] | **1.000** | +0.52 [+0.42, +0.63] | 1.000 |
| NRS | 3.00 | 3.00 | 3.25 | +0.25 [0.00, +0.75] | 0.65 | +0.25 | 0.65 |
| CPR | 0.64 | 0.31 | 0.70 | +0.06 [−0.15, +0.31] | 0.70 | +0.39 [+0.16, +0.64] | 1.000 |

**Methodology fix worked.** v2 cratered (0.31), v3 not only recovered but exceeded baseline (0.84 vs 0.72). The +0.12 CDR lift on task_folder is **statistically clean** — paired bootstrap p=1.00, CI strictly above zero.

### `autosearch_corrections` (n=26, thin input — the cases we hoped Q&A would rescue)

| Metric | Baseline | v2 | v3 | Δ(v3 − base) | p(>0) | Δ(v3 − v2) | p(>0) |
|---|---|---|---|---|---|---|---|
| CDR | **0.23** | 0.25 | **0.30** | +0.070 [−0.013, +0.173] | 0.94 | +0.047 [−0.003, +0.102] | 0.97 |
| NRS | 2.35 | 2.35 | 2.38 | +0.04 | 0.59 | +0.04 | 0.61 |
| CPR | 0.65 | 0.69 | 0.77 | +0.12 [−0.10, +0.33] | 0.83 | +0.08 | 0.78 |

**Thin-input cases lift, but not enough to clear thresholds.** CDR 0.23 → 0.30 is a directional improvement that approaches significance (p=0.94) but is far from the 0.90 threshold. The thin-input regime is still fundamentally constrained — Q&A helps mostly when the user-twin can answer from ground truth (`from_ground_truth=true`), and for thin-input cases the unsure rate is high (median 0.83 across `autosearch_corrections`, vs 0.33 across `task_folder`).

### `git_commit` (n=6, sparse-but-structured input)

| Metric | Baseline | v2 | v3 | Δ(v3 − base) | p(>0) |
|---|---|---|---|---|---|
| CDR | 0.45 | 0.34 | 0.36 | **−0.094** [−0.317, +0.111] | 0.18 |
| NRS | 2.00 | 2.50 | 2.50 | +0.50 [+0.17, +0.83] | 0.99 |
| CPR | 1.00 (n=2) | 0.25 | 0.25 | −0.75 [−1.0, −0.5] | 0.00 |

**Mild regression on the `git_commit` bucket.** Q&A noise dilutes already-tight commit-message signal. CDR went down (Δ=−0.09) though CI brushes zero. NRS lifted clearly because the qualifier-style Q&A answers surface nuance commit subjects can't. CPR dropped sharply but n=2 makes this barely interpretable.

**Net read:** v3 lifts thin-input and rich-input cases; it slightly hurts the narrow middle (git_commit n=6) where commits are already terse but on-spec.

---

## Stratified by task_type

| task_type | n | CDR base | CDR v2 | CDR v3 |
|---|---|---|---|---|
| architecture | 5 | 0.73 | 0.36 | **0.90** |
| etl_schema | 5 | 0.41 | 0.36 | 0.60 |
| docs_skill | 6 | 0.49 | 0.35 | 0.55 |
| refactor | 3 | 0.40 | 0.33 | 0.30 |
| bug_fix | 4 | 0.43 | 0.36 | 0.38 |
| go_cli_feature | 17 | 0.17 | 0.17 | 0.19 |

| task_type | CPR base | CPR v3 | NRS base | NRS v3 |
|---|---|---|---|---|
| architecture | 0.67 | 0.83 | 3.0 | 3.4 |
| etl_schema | 0.40 | 0.73 | 2.6 | 2.6 |
| docs_skill | 0.71 | 0.63 | 2.83 | 2.67 |
| refactor | 1.00 | 0.75 | 2.0 | 2.67 |
| bug_fix | 1.00 | 0.00 | 2.0 | 2.25 |
| go_cli_feature | 0.68 | 0.76 | 2.24 | 2.35 |

- **Architecture wins big.** CDR 0.73 → 0.90, CPR 0.67 → 0.83, NRS 3.0 → 3.4. With rich planning docs and a high signal-to-noise Q&A, the schema becomes nearly complete.
- **etl_schema also lifts cleanly** (CDR 0.41 → 0.60, CPR 0.40 → 0.73).
- **go_cli_feature barely moves** (0.17 → 0.19). This bucket is the autosearch_corrections corpus mostly — thin input, high unsure rate.
- **bug_fix CPR cratered** (1.0 → 0.0, n=4). Small n, but worth flagging: bug-fix corrections often pivot on a specific implementation choice the Q&A doesn't surface (and may even mis-direct).

---

## Q&A statistics (recomputed from `scores_v2/summary.json` qa_stats block)

| Stat | Mean | Median | 95% CI |
|---|---|---|---|
| Questions per case | 6.3 | 6.0 | [6.18, 6.48] |
| Unsure rate (per case) | 0.64 | 0.67 | [0.56, 0.71] |

- **The user-twin marks the majority of questions as "unsure"** (mean 64%, median 67%). On 8 cases the user-twin was unsure on every question.
- Pearson correlation between unsure_rate and Δ(v3 − baseline) on CDR is **+0.149** — weakly positive. The intuition "high unsure rate = no Q&A signal = no lift" is mostly right but not deterministic. Some unsure answers still encode useful constraints ("not decided yet" implicitly opens a `decision_candidate` axis).

Per-case sample (first 10):

| case | n_q | unsure | rate |
|---|---|---|---|
| sc_001 | 7 | 2 | 0.29 |
| sc_002 | 7 | 1 | 0.14 |
| sc_003 | 6 | 4 | 0.67 |
| sc_004 | 7 | 3 | 0.43 |
| sc_005 | 7 | 2 | 0.29 |
| sc_006 | 6 | 2 | 0.33 |
| sc_007 | 6 | 2 | 0.33 |
| sc_008 | 7 | 7 | 1.00 |
| sc_009 | 6 | 5 | 0.83 |
| sc_010 | 6 | 5 | 0.83 |

The pattern: **task_folder cases (sc_001 – sc_008) have low unsure rate** (rich GT means the user-twin can answer); session cases (sc_009+) have high unsure rate.

---

## Verdict per threshold

| Metric | Threshold | v3 result | Pass? |
|---|---|---|---|
| CDR | ≥ 0.90 | 0.41 (overall), 0.84 (task_folder), 0.30 (autosearch_corrections) | **FAIL** overall. task_folder narrowly misses. |
| HAR | ≤ 0.10 | 0.00 | **PASS** (hollow — generator was told to copy state) |
| NRS | ≥ 4.0 | 2.58 | **FAIL** |
| CPR | ≥ 0.70 | 0.72 | **PASS** (newly — baseline was 0.67) |

**Two of four thresholds met. Baseline met only one (HAR). v3 newly crosses CPR.**

---

## Case studies

### sc_001 (architecture) — the biggest schema win

CDR 0.75 → **1.00**. Baseline extracted 6 hard_constraints; v3 extracted 13. The critical lift: baseline missed "all five import styles" (`import`, `import()`, `require`, `export … from`, `import type`); v3's hard_constraint #12 reads: *"Handle all TypeScript import styles: import ... from \"X\", import(\"X\"), require(\"X\"), export ... from \"X\""*. The Q&A pair for `scope` (`"TypeScript files and their dependencies, but not JavaScript, CSS, or images"`) didn't directly enumerate the styles — the source for the lift came from re-reading the same planning docs with the Q&A context priming attention on import semantics. The schema *could* hold this information all along; the extractor needed the nudge.

### sc_027 (go_cli_feature, weak baseline) — Q&A unlocked a previously empty state

Baseline: 0 hard_constraints, 0 soft_preferences, 0 decision_candidates, 1 generic assumption. CDR 0.00.
v3: 2 hard_constraints, 2 soft_preferences, 2 decision_candidates, 2 qualifiers, 2 assumptions. CDR 0.40.

Initial prompt: `"explore this project, tell me what you think of it, be critical"` (74 chars).
Q&A answer for `scope`: `"critical evaluation of the project's transparency and how it handles new files or rows"` — this is plainly post-hoc information; the user-twin pulled it from the correction `"i dont want this to filter any rows, instead just add metrics, so this tool should be very transparent"`.

So v3's lift here is real on the metric — but it is at least partly **contamination** (see "Honest assessment" below). Without that correction in the user-twin's GT, the Q&A would have come back "not decided yet" for everything.

### sc_025 (docs_skill, strong baseline maintained) — no harm

CDR 1.00 → 1.00. Baseline extracted 1 hard_constraint about `read_when` phrasing; v3 extracted 3 (adding `output_format` and `error_handling` axes) plus 3 soft_preferences and 3 decision_candidates that did not previously exist. The over-elaboration is mild bloat but didn't push CDR off 1.0 because the additional state items don't displace the original constraint. NRS stayed at 3.

This case demonstrates v3 doesn't regress strong baselines.

---

## Honest assessment — is the Q&A contaminated?

**Yes, partially.** Five-case spot check:

| case | Q&A answer | Plausibly leaks GT? |
|---|---|---|
| sc_001 `output_format` | *"JSON format by default, with options for DOT and Mermaid formats"* | **Yes.** Specific format names "DOT" and "Mermaid" likely from planning docs/feedback, not a naïve user prompt response. |
| sc_005 `output_format` | *"JSON format, clearly representing module relationships and import kinds"* | **Yes.** "Import kinds" is task-specific vocabulary from feedback.md. |
| sc_020 `scope` | *"session management aspect, particularly using agent IDs as session IDs and incorporating a parent session ID field"* | **Yes, strongly.** Specific schema decision likely pulled from corrections/feedback. |
| sc_027 `scope` | *"critical evaluation of the project's transparency and how it handles new files or rows"* | **Yes.** Direct paraphrase of a correction. |
| sc_017 (all) | All `"not decided yet"` (unsure_rate=1.0) | **No leak** — user-twin honestly refused. |

The `answer_questions.py` user-twin sees `planning_docs + corrections + final_artifacts` to decide answers. The CDR/CPR judges use the same materials as ground truth. So when the user-twin answers from corrections, and the extractor encodes that answer, and the CDR judge then matches it back from corrections, the chain is partly self-fulfilling.

**Magnitude of the contamination:** the user-twin's `from_ground_truth=true` rate averages 36% (1 − 0.64). On those answers, contamination is plausible. On the 64% "unsure" answers, no contamination. The directional v3 lift is real, but the *magnitude* is inflated by perhaps 30–50% on the strong-lift cases (the architecture / etl_schema cases where the user-twin answered confidently from GT).

A clean re-run would hold corrections + final_artifacts out of the user-twin's view. We expect the CDR lift on task_folder cases to shrink (the answers would be less informative) but still beat baseline, because the planning docs alone often contain decisions the baseline extractor failed to pick out — sc_001's "all five import styles" lift, for example, plausibly came from re-priming attention on planning docs rather than from a specific GT-leaked answer.

---

## Failure modes — where Q&A helped, hurt, or no-op

**Helped (Δ CDR > +0.20, n=9):** sc_020, sc_027, sc_009, sc_016, sc_005, sc_001, sc_017, sc_004, sc_035. Almost all are cases where the baseline state was sparse or empty. Q&A populated the state.

**No-op (|Δ| ≤ 0.05, n=20):** Including sc_025 (already perfect), sc_002, sc_003, sc_007, sc_008 (already strong). Also sc_033 – sc_040 (thin-input cases where unsure_rate=1.0). Q&A neither helps nor hurts when (a) baseline was strong or (b) Q&A returned all "not decided yet".

**Hurt (Δ CDR ≤ −0.10, n=6):** sc_028, sc_014, sc_010, sc_018, sc_026, sc_021. Common pattern: the baseline state was reasonably populated; the Q&A pushed the extractor toward broader categories ("output_format: feedback to be clear and straightforward") that the CDR judge then failed to match against tightly-worded critical decisions ("metrics are recorded without filtering any rows or files"). The extractor regresses to mean axes ("dependencies", "target_platform", "breaking-change posture" — the questions the elicitor likes to ask) and away from task-specific axes the baseline had already identified.

**The schema is partly to blame for the "hurt" cases.** When the elicitor surfaces 6–7 questions on generic axes (scope, output_format, dependencies, error_handling, performance, target_platform, breaking-change posture), the answers crowd out the task-specific axes already present in the baseline state. The schema's generic-axis bias is reinforced by the Q&A loop.

---

## Cost

| Step | Calls | Model | Cost |
|---|---|---|---|
| v3 extraction | 40 | gpt-4o-mini | $0.0230 |
| v3 generation | 40 | gpt-4o-mini | $0.0174 |
| v3 scoring (mini judges) | ~600 | gpt-4o-mini | $0.070 |
| v3 scoring (NRS gpt-4o) | 40 | gpt-4o | $0.190 |
| **v3 total** |  |  | **$0.300** |

Plus the existing v2 spend ($0.299) and original baseline spend ($0.276 ish) — combined Assumption-1 spend now sits at **~$0.91** lifetime against the $30 pool.

Cache fallback (read v2 and baseline judge caches before issuing) saved ~$0.05–0.10 in `crit_decisions` calls. The vast majority of v3 scoring cost came from CDR and HAR judges, which depend on the v3 draft text and so could not hit existing caches.

---

## Recommendation — does the spike's STOP verdict still hold?

**Yes, but with two material updates.**

1. **The schema is not the bottleneck.** v3 demonstrates that with augmented input, the existing schema **can** capture critical decisions: task_folder CDR went from 0.72 to 0.84, architecture CDR from 0.73 to 0.90, and CPR overall crossed its 0.70 threshold for the first time. The fields the baseline study flagged as inert (`decision_candidates`, `blast_radius`) are still inert in v3 — but they don't block the metric improvement. The schema fix the original Assumption-1 report recommended ("drop `decision_candidates`, fix `qualifiers` to verbatim spans, retire `blast_radius`") is small surgery, not redesign. **Confirmed**.

2. **The bottleneck is input quality, including but not limited to Q&A.** v3 lifts thin-input cases (autosearch_corrections CDR 0.23 → 0.30) but not anywhere near the 0.90 threshold. The Q&A user-twin returns "not decided yet" 64% of the time because the source materials genuinely don't contain the answer. For one-paragraph prompts, no amount of clarifying-question-asking can synthesize information that doesn't exist yet at extraction time. **The STOP verdict for the autosearch_corrections regime stands.**

**However, on the task_folder regime — projects with real planning docs and a willing-to-answer user — v3 results suggest the structured compiler is closer to viable than the original synthesis concluded.**

Specifically:
- task_folder CDR 0.84 + Δ from a clean re-run (holding corrections out of the user-twin) probably lands in the 0.75–0.85 range. Below 0.90 but recoverable with the schema surgery already recommended.
- task_folder CPR 0.70 already meets threshold.
- task_folder NRS 3.25 is the closest the spike has come to 4.0.

**Updated recommendation:**

- **DO NOT build the compiler as a general-purpose surface for arbitrary user prompts.** The 26-case `autosearch_corrections` floor of 0.30 CDR confirms the original STOP.
- **DO consider building the compiler as a planning-doc enricher** that runs over `requirements.md` / `solution.md` / `plan.md` artifacts plus a short Q&A loop. The task_folder + Q&A regime is the only configuration where ≥2 thresholds are met.
- **Before any product work, re-run with corrections held out of the user-twin** to get a contamination-free baseline. Total cost: ~$0.30 (caches still warm). This converts the v3 finding from "directionally encouraging" to "decision-grade".
- **Schema surgery from original report still applies:** drop `decision_candidates`, fix `qualifiers` to verbatim, retire `blast_radius`. The v3 results don't change any of those recommendations.

The original synthesis's STOP verdict was correct for the corpus it tested. v3 narrows the STOP to "STOP for thin-input, KEEP EXPLORING for rich-input + Q&A".
