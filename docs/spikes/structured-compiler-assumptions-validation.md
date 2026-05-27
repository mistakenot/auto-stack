# Spike: Structured Compiler Assumptions Validation

Validate the three highest-risk assumptions behind a structured requirements compiler before building production infrastructure.

## Scope

This spike designs experiments only. It does not require implementing a full `autoquestion` product first.

## Experiment Environment

Assume the experimenter agent has:

- Python 3.12 (with `uv` or `pip`)
- this repository checkout
- all `auto-*` CLI tools in PATH (`autosearch`, `autoetl`, `autoreflect`, etc.)
- `OPENAI_API_KEY`

Workspace convention for this spike:

- Put all experiment code under `.tmp/experiments/structured-compiler/`.
- Load environment variables from `.tmp/experiments/.env`.

Example:

```bash
mkdir -p .tmp/experiments/structured-compiler
set -a && source .tmp/experiments/.env && set +a
```

Suggested Python deps:

- `pandas`
- `numpy`
- `scikit-learn`
- `pyarrow`
- `duckdb`
- `openai`
- `scipy`
- `matplotlib` (optional)

## Why These 3 Assumptions

The structured compiler idea fails if any of these are false:

1. We cannot represent real requirements nuance in a structured state without losing acceptance-critical detail.
2. The compiler’s question policy (ask only high-regret unresolved decisions) does not actually beat simpler baselines.
3. Incremental recompilation cannot safely propagate decision changes without stale or over-invalidated outputs.

---

## Shared Dataset Setup

Create one reusable evaluation corpus for all experiments.

### Dataset target

- 40-80 historical tasks/sessions from this repo ecosystem.
- Include mixed domains: CLI, ETL/schema, docs/skills workflow, architecture tasks.
- For each case, store:
  - initial user prompt
  - early planning artifacts (`requirements.md`, `solution.md`, `plan.md`) if present
  - mid-session corrections ("actually", "changed my mind", "undo", etc.)
  - accepted final outcome (merged docs/code state)

### Collection outline

```bash
mkdir -p .tmp/experiments/structured-compiler/{data,artifacts,reports}

# Candidate correction-heavy sessions
autosearch search '"changed my mind" OR "actually" OR "undo" OR "not what i want"' \
  --role user --limit 500 --json \
  > .tmp/experiments/structured-compiler/data/correction_hits.json
```

Use Python to join/search results with additional context from ETL parquet and repo task docs. Persist a normalized file:

```text
.tmp/experiments/structured-compiler/data/eval_cases.jsonl
```

Each line:

```json
{
  "case_id": "sc_042",
  "task_type": "go_cli_feature",
  "initial_prompt": "...",
  "planning_docs": ["docs/tasks/.../requirements.md"],
  "corrections": ["..."],
  "final_artifacts": ["docs/tasks/.../requirements.md", "commit_sha"],
  "notes": "optional"
}
```

---

## Assumption 1: Structured State Can Capture Fuzzy Requirements Nuance

### Assumption

A hybrid structured state (hard constraints + soft preferences + explicit assumptions + open-text qualifiers) can preserve acceptance-critical nuance well enough to generate acceptable requirements drafts.

### Why this is hard

- Human requirements often include conditional language, exceptions, and intent that is not binary.
- Over-structuring can erase nuance; under-structuring becomes untestable prose.

### Experiment

For each case:

1. Convert source materials into a structured decision state via LLM extraction.
2. Generate a requirements draft from structured state only.
3. Compare the generated draft against accepted requirements and correction history.

### Required structured schema (minimum)

```json
{
  "hard_constraints": [{"id":"json_stdout_parseable","value":true,"source":"repo_standard"}],
  "soft_preferences": [{"axis":"test_strategy","value":"e2e","confidence":0.64}],
  "decision_candidates": [{"axis":"storage_backend","candidates":[["postgres",0.58],["sqlite",0.34]]}],
  "qualifiers": [{"axis":"test_strategy","text":"unit is fine for parser-only changes"}],
  "assumptions": [{"id":"A3","text":"single filter mode unless parity requirement appears","blast_radius":"medium"}]
}
```

### Metrics

1. Critical Decision Recall (CDR): fraction of acceptance-critical decisions present in compiled draft.
2. Hidden Assumption Rate (HAR): assumptions introduced in draft that were not declared in structured state.
3. Nuance Retention Score (NRS): rubric score from 1-5 by LLM judge on whether exceptions/conditions were preserved.
4. Correction Predictability Recall (CPR): fraction of historical correction points anticipated in assumptions/questions.

### Pass thresholds

- `CDR >= 0.90`
- `HAR <= 0.10`
- `NRS >= 4.0` average
- `CPR >= 0.70`

### Failure meaning

If this fails, the compiler representation is too rigid. Fix schema before investing in policy optimization.

---

## Assumption 2: Regret-Aware Question Policy Beats Simpler Baselines

### Assumption

A policy that asks only low-confidence, high-regret questions will reduce user burden without increasing costly misalignment.

### Why this is hard

- Confidence alone often misses rare high-blast-radius decisions.
- Asking fewer questions can silently increase downstream rework.

### Experiment

Build an offline planner simulation with one "user twin" per case (from historical final decisions/corrections). Evaluate policies:

1. `ask_all`: ask every unresolved decision.
2. `frequency_default`: default high-frequency values, ask contested.
3. `confidence_only`: ask when confidence < threshold.
4. `regret_aware` (candidate): ask when `confidence < c` AND `regret_if_wrong > r`.

Each run should produce:

- asked questions
- defaulted decisions
- mismatches against case ground truth
- estimated regret cost

### Scoring

Primary:

- Weighted Regret Cost (WRC): sum of mismatch costs.
- Questions Asked (QA): median per case.

Secondary:

- Decision Accuracy (DA)
- First-Critical-Question Position (FCQP)

### Pass thresholds

Compared to `confidence_only` baseline:

- `WRC` reduced by at least `25%`
- `QA` not increased by more than `10%`
- `DA` non-inferior (within `2%` absolute)

Use bootstrap confidence intervals over cases. Require lower CI bound to satisfy first threshold.

### Failure meaning

If this fails, regret model quality is insufficient or policy is overfitting. Do not ship "ask less" behavior yet.

---

## Assumption 3: Incremental Recompilation Is Sound

### Assumption

When an upstream decision changes, the compiler can invalidate exactly the affected downstream decisions and regenerate a consistent requirements state without stale carryover.

### Why this is hard

- Dependency graphs are incomplete early on.
- Over-invalidation loses efficiency; under-invalidation causes subtle contradictions.

### Experiment

For each case:

1. Build baseline compiled decision graph/state.
2. Apply controlled mutations to high-impact root decisions:
   - storage backend flip (`file -> postgres`, `sqlite -> postgres`)
   - interface contract flip (`single_filter -> composable`)
   - validation strictness flip (`strict -> permissive`)
3. Run:
   - full recompile from scratch (oracle)
   - incremental recompile from mutated node
4. Compare outputs.

### Metrics

1. Invalidation Precision (IP): fraction of incrementally invalidated nodes that truly changed in oracle full recompile.
2. Invalidation Recall (IR): fraction of truly changed nodes that incremental run invalidated.
3. Stale Decision Leak Rate (SDLR): fraction of final outputs containing stale pre-mutation decisions.
4. Recompute Savings (RS): fraction of nodes avoided vs full recompile.

### Pass thresholds

- `IR >= 0.95`
- `IP >= 0.80`
- `SDLR <= 0.02`
- `RS >= 0.40`

### Failure meaning

If `IR` or `SDLR` fails, incremental mode is unsafe; default to full recompile until dependency model improves.

---

## Evaluation Protocol

### Statistical plan

- Minimum 40 cases for initial signal.
- Report mean + median + 95% bootstrap CI for all primary metrics.
- Use paired comparisons per case (same case across policies).

### Judge controls (LLM-based grading)

- Use fixed grading prompts and schema.
- Blind judge to policy label when possible.
- Run dual-judge disagreement sample (10-15% of cases); escalate disagreements for manual review.

### Reproducibility

- Persist every intermediate artifact under:
  - `.tmp/experiments/structured-compiler/artifacts/`
  - `.tmp/experiments/structured-compiler/reports/`
- Save model/version and prompt hash in every output row.

---

## Suggested Experiment File Layout

```text
.tmp/experiments/structured-compiler/
  data/
    eval_cases.jsonl
    decision_events.parquet
  scripts/
    extract_cases.py
    build_structured_state.py
    generate_requirements.py
    simulate_policies.py
    test_incremental_recompile.py
    score_metrics.py
  artifacts/
    states/
    drafts/
    simulations/
    recompiles/
  reports/
    assumption_1_report.md
    assumption_2_report.md
    assumption_3_report.md
    summary.md
```

---

## Go/No-Go Criteria

Proceed to implementation only if:

1. Assumption 1 meets all thresholds.
2. Assumption 2 meets all primary thresholds with positive CI margin.
3. Assumption 3 meets safety thresholds (`IR`, `SDLR`) even if efficiency (`RS`) is initially modest.

If only Assumption 3 fails, ship compiler in full-recompile mode first.
If Assumption 1 fails, redesign schema before building product surfaces.

---

## Fast Start (48-hour Slice)

If you want a minimal first run:

1. Use 20 cases instead of 40-80.
2. Run Assumption 2 only with two policies: `confidence_only` vs `regret_aware`.
3. Run Assumption 3 on one task family (Go CLI).
4. Produce a one-page summary with decision: continue, pivot, or stop.
