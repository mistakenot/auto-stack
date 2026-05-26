---
hash: "9a9f0df2"
id: "5f4be445"
read_when: "designing automated skill optimization loops for autoskill"
summary: "Technical implementation notes from SkillOpt (arXiv:2605.23904), focused on what to adopt in auto-skill."
title: "SkillOpt Paper Notes for auto-skill"
---

# SkillOpt Paper Notes for auto-skill

## Why this matters to auto-skill

SkillOpt treats a `SKILL.md`-style artifact as trainable external state for a frozen agent, then optimizes it with controls analogous to deep learning training loops. This is directly relevant to `autoskill`, which already treats skills as first-class artifacts and enforces linting, structure, and portability.

## Paper in one paragraph

The paper proposes a text-space optimizer for skills: run the target model on scored tasks with the current skill, reflect on failures and successes in minibatches, generate bounded edits, and accept edits only when held-out validation improves. The loop includes an edit budget (textual learning rate), a rejected-edit memory buffer, and epoch-level slow/meta updates. Reported results claim best-or-tied performance in 52/52 evaluated cells across six benchmarks, seven target models, and three harnesses (direct chat, Codex, Claude Code), while exporting a single deployable `best_skill.md`.

## Method, as implementable loop

Given:
- Frozen target model `M`
- Optimizer model `O`
- Harness `h`
- Initial skill `s0`
- Splits: train/selection/test

State:
- `s_cur` (current skill)
- `s_best` (best selection-gated skill)
- selection cache keyed by skill hash
- epoch-local step buffer of rejected edits + failure patterns
- optional optimizer-only meta skill memory

Per step:
1. Roll out train batch with `s_cur` using frozen target/harness.
2. Split trajectories into failure and success sets.
3. Reflect in minibatches; produce structured edits.
4. Hierarchically merge proposals (failure-first priority).
5. Rank and clip edits to budget `L_t`.
6. Apply edits to produce `s_candidate`.
7. Evaluate `s_candidate` on selection split.
8. Accept only if score is strictly greater than current score.
9. If rejected, store rejected edits + observed failure patterns in step buffer.

Per epoch:
1. Compare previous-vs-current epoch skill on same sampled tasks.
2. Generate slow-update guidance for protected section.
3. Update optimizer-side meta memory for future reflection/merge/rank.

Output:
- Best validated `best_skill.md` for deployment.
- No optimizer model calls at deployment.

## Technical controls that appear most important

From paper ablations and released code:
- Bounded textual updates: edit budget acts as a learning rate.
- Strict gate: candidate must beat current score (`>`), ties rejected.
- Rejected-edit buffer: stores failed edits and score deltas to reduce repeated bad directions.
- Failure/success separation before merge: protects working behavior while fixing recurring errors.
- Slow/meta updates: introduces longitudinal memory across epochs.
- Protected region markers (`SLOW_UPDATE_START/END`): prevents step-level edits from clobbering slow guidance.

## Default protocol and knobs (paper + code)

Common defaults (reported and/or in public config):
- Epochs: `4`
- Rollout batch size: `40`
- Reflection minibatch size: `8`
- Merge batch size: `8`
- Analyst workers: `16`
- Learning rate (edit budget): `4`
- Min LR floor: `2`
- Scheduler: cosine by default (constant/linear/autonomous available)
- Gate strictness: accept only if held-out selection score improves strictly
- Slow update: enabled, `20` sampled tasks per epoch
- Meta skill: enabled (optimizer-only, not deployed)
- Split convention: typically `2:1:7` train/val/test for dataset-backed runs

Patch operations in patch mode:
- `append`
- `insert_after`
- `replace`
- `delete`

## Main empirical signals to keep in mind

Headline claims:
- Best or tied-best on `52/52` evaluated cells.
- GPT-5.5 average gain over no-skill:
- `+23.5` direct chat
- `+24.8` Codex harness
- `+19.1` Claude Code harness

Transfer:
- Cross-model transfer positive in all reported rows.
- Strong cross-harness transfer on SpreadsheetBench (large positive deltas both directions).
- Positive cross-benchmark math transfer (smaller, but consistently positive).

Artifact efficiency:
- Final skills remain compact (reported up to ~2k tokens).
- Large gains often come from only `1–4` accepted edits.
- Training cost is non-trivial but paid offline; deployment remains a single skill artifact.

## What is directly reusable for auto-skill

Design patterns worth porting:
1. Treat skill optimization as offline training over text artifacts, not runtime prompting.
2. Keep target execution harness fixed during optimization runs.
3. Enforce strict held-out gating as non-negotiable safety.
4. Require machine-parseable edit proposals (JSON patch contract).
5. Persist full audit artifacts per step (candidate skill, ranked edits, apply report, selection score).
6. Separate deployable skill content from optimizer-only memory/state.

CLI concepts that map well to autoskill:
- `autoskill optimize <skill> --dataset ... --harness ...`
- `autoskill optimize resume <run_id>`
- `autoskill optimize eval --skill <path> --split <train|val|test|all>`
- `autoskill optimize report <run_id>` for cost/accept-reject/edit economy summaries

## Important discrepancy to resolve before adopting behavior

Paper text and appendix describe slow-update guidance as selection-gated. Current public code in `skillopt/engine/trainer.py` force-injects slow-update content into current and best skills without step-level gate evaluation at epoch boundary. If we implement this in `autoskill`, we should choose one policy explicitly:
- Conservative: gate slow-update candidates the same way as step candidates.
- Aggressive: force-inject slow update but keep full audit trail and rollback path.

## Suggested autoskill MVP (if we build this)

Phase 1:
- Add run schema, step artifacts, and selection gating.
- Implement patch-mode only (`append/insert_after/replace/delete`).

Phase 2:
- Add rejected-step buffer feedback into reflection prompts.
- Add hierarchical merge and explicit ranking/clip stage.

Phase 3:
- Add epoch-level slow/meta memory with protected section markers.
- Add transfer evaluation mode (same skill across model/harness).

Phase 4:
- Add dashboards and cost metrics:
- accepted edits
- rejected edits
- delta vs baseline
- tokens per point gain

## Integration opportunities across auto-stack

- `auto-etl` / `auto-search`: mine recurrent failure patterns to seed initial optimizer memory and prompt contracts.
- `auto-reflect`: synthesize longitudinal regression themes into slow-update candidates.
- `auto-watch`: trigger re-optimization when skills drift or dependent docs change.
- Existing `autoskill lint`: validate optimized skills before publish/sync.

## Sources

- Paper abstract and metadata: https://arxiv.org/abs/2605.23904
- Paper HTML (v2, 25 May 2026): https://arxiv.org/html/2605.23904
- TeX source (used to recover full tables/numbers): https://arxiv.org/e-print/2605.23904
- Project page: https://microsoft.github.io/SkillOpt/
- Code repository: https://github.com/microsoft/SkillOpt
- Base config defaults: https://github.com/microsoft/SkillOpt/blob/main/configs/_base_/default.yaml
- Trainer loop implementation: https://github.com/microsoft/SkillOpt/blob/main/skillopt/engine/trainer.py
- Gate function (strict `>`): https://github.com/microsoft/SkillOpt/blob/main/skillopt/evaluation/gate.py
- Patch ops and protected region handling: https://github.com/microsoft/SkillOpt/blob/main/skillopt/optimizer/skill.py
