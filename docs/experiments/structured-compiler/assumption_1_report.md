# Assumption 1 Validation Report

**Spike:** Structured Compiler — Assumption 1 (hybrid structured state can preserve acceptance-critical nuance).
**Date:** 2026-05-27
**Cases:** 40 (all usable)
**Models:** extraction + generation + most judges = `gpt-4o-mini`. NRS judge = `gpt-4o`.
**Pipeline:** `initial_prompt + early planning_docs` → structured state → `requirements.md` draft (no source text) → 4 metric judges against ground truth (planning_docs + corrections + feedback.md + final_artifacts).

---

## Verdict: **FAIL** (3 of 4 metrics miss thresholds, two by a wide margin)

| Metric | Threshold | Mean | Median | 95% CI | Pass? |
|---|---|---|---|---|---|
| **CDR** (Critical Decision Recall) | ≥ 0.90 | **0.36** | 0.32 | [0.26, 0.46] | **FAIL** |
| **HAR** (Hidden Assumption Rate)  | ≤ 0.10 | **0.00** | 0.00 | [0.00, 0.00] | **PASS** |
| **NRS** (Nuance Retention, 1-5)   | ≥ 4.0  | **2.43** | 2.0  | [2.28, 2.58] | **FAIL** |
| **CPR** (Correction Predictability) | ≥ 0.70 | **0.67** | 1.00 | [0.54, 0.80] | **FAIL** (mean), borderline |

The HAR pass is a hollow win: the generator was prompted to mirror only declared assumptions and to invent nothing. It obeyed. The real test is whether the *upstream extractor* declared the right assumptions — which the other metrics show it largely did not.

CPR's median is 1.0 but the mean drags down because ~40% of cases have at least one correction that no part of the structured state touched. CI lower bound (0.54) is well under 0.70.

---

## CDR breakdown by input richness (the headline finding)

| Source signature | n | CDR mean | NRS mean | Notes |
|---|---|---|---|---|
| `task_folder` (real `requirements.md` + `solution.md` + `plan.md`) | 8 | **0.72** | 3.00 | Closest to passing |
| `git_commit` (commit subject + body bullets) | 6 | 0.45 | 2.00 | Sparse input, sparse output |
| `autosearch_corrections` (initial user prompt only) | 26 | **0.23** | 2.35 | Catastrophic |

A structured state extracted from ~one paragraph of user intent does not contain enough material to reconstruct an acceptance-critical spec. This is not surprising — but the *gap* between "rich input" (0.72) and "production input" (0.23) is the dominant signal in the spike. The hypothesis "schema can capture nuance" is conflated with "extractor has nuance to capture". Even with the best input source, CDR sits at 0.72 — 18 points below threshold.

CDR by task type:

| task_type | n | CDR mean |
|---|---|---|
| architecture | 5 | 0.73 |
| docs_skill  | 6 | 0.49 |
| bug_fix     | 4 | 0.42 |
| etl_schema  | 5 | 0.41 |
| refactor    | 3 | 0.40 |
| go_cli_feature | 17 | 0.17 |

`go_cli_feature` collapses because most of those cases come from the session-source bucket — a single line user prompt and no planning artifacts.

---

## Schema utilization audit (per-state field counts, n=40)

| Field | Mean per case | Median | Cases with 0 |
|---|---|---|---|
| hard_constraints | 1.9 | 0 | **21 / 40** |
| soft_preferences | 1.4 | 2 | 17 / 40 |
| decision_candidates | 0.3 | 0 | **27 / 40** |
| qualifiers | 1.0 | 1 | 17 / 40 |
| assumptions | 1.6 | 2 | 0 / 40 |

- `decision_candidates` (the "explicit fork" bucket) is essentially dead — used in 13/40 cases. Users in this corpus rarely articulate explicit candidate sets in early planning; they either commit ("use sqlite") or stay vague ("storage TBD").
- `hard_constraints` is empty more than half the time. The extractor reserves the bucket for things stamped "MUST" or in acceptance-criteria sections — anything not formally stated falls into `soft_preferences` or `assumptions`. For task_folder cases with explicit acceptance criteria sections, the bucket fills (sc_001 has 6). For session cases, almost never.
- `assumptions` is the only field used in every case — but it tends to absorb everything the extractor is uncertain about, including things that arguably should be soft_preferences. Median of 2 assumptions per case is low; corrections per case average 2-3.
- `qualifiers` is used in ~half the cases but the extracted text is often a paraphrase rather than a verbatim carve-out. The NRS judge consistently flagged this.

---

## Representative Case Studies

### Strong: `sc_025` (docs_skill) — CDR 1.0, NRS 3, CPR 1.0

The task: tidy up `read_when` phrasing in `autodoc` help strings and docstrings to avoid "Read when: when …". The user wrote a focused, narrow prompt and there were 2 corrections, both about scope.

Draft excerpt:

> Update any autodoc help, fix, or docstring that instructs on this to match and not repeat 'when'.
> **Preferences:** The change should only apply to instances where 'when' is repeated in the context of 'read_when'.

NRS got 3, not 5, because while the draft technically lists the exception, it doesn't preserve the conditional phrasing the user actually used ("update **all** … to match"). The "we want consistency across all docstrings" intent was softened to a single hard constraint.

Why it works: the task is tiny, all decisions sit on one axis (formatting rule), one assumption suffices. The schema doesn't have to scale here.

### Weak: `sc_027` (go_cli_feature) — CDR 0.0, NRS 2, CPR 0.0

Initial prompt: `"explore this project, tell me what you think of it, be critical"`.

Extracted state:

```json
{
  "hard_constraints": [],
  "soft_preferences": [],
  "decision_candidates": [],
  "qualifiers": [],
  "assumptions": [{"text": "The project is worth exploring if it has potential for improvement or innovation."}]
}
```

Mid-stream correction: *"ok i dont want this to filter any rows, instead just add metrics, so this tool should be very transparent, no filtering at all, when it encounters a new file or a row it shouldnt process, it just records it in metrics and continues"*.

Draft excerpt:

> ## Goals
> - Establish a framework for evaluating project potential.

This is the canonical failure mode the schema can't solve. The initial prompt was a question, not a spec. The first half of the session was the user discovering what they wanted. No structured state extracted from the prompt could anticipate "no filtering, just metrics" because that decision had not been made yet. The compiler is being asked to anticipate decisions that don't exist at extraction time.

### Ambiguous: `sc_001` (architecture, TypeScript Import Graph) — CDR 0.75, NRS 3, CPR 0.33

This case has the full task_folder treatment: requirements.md (89 lines), solution.md, plan.md. Extraction yielded 6 hard_constraints (AC-1 through AC-9), 3 candidates for output_format, 3 qualifiers, 3 assumptions. Best-case schema use.

CDR was 0.75 — the draft picks up "JSON default", "ast-grep dependency check", "tsconfig path alias resolution", but missed "all five import styles (`import`, `import()`, `require`, `export from`, `import type`)" because the schema collapsed it into a single "supports multiple import styles" preference rather than enumerating each style as a constraint.

CPR was 0.33. Of the three corrections in feedback.md:
- `--lang=ts` vs `--lang=tsx` (anticipated: NO — no assumption about ast-grep language modes)
- `export { $_ }` only matches single-name exports (anticipated: NO — patterns were enumerated, not implementation choices)
- PR base SHA off because main was stale (anticipated: NO — workflow concern, outside schema scope)

This is the most instructive case. Even with rich input, the schema captures *what the user said* but not *what they would have answered if asked the right question*. The structured state is essentially a denormalized echo of the source — it doesn't generate new axes or interrogate the user's ast-grep assumptions.

---

## Failure modes observed

1. **Empty-bucket cascade.** When `hard_constraints` is empty (21/40), the generator writes "Hard Constraints: (none)" and the draft loses everything testable. Generation faithfully reflects the state. The bottleneck is upstream.

2. **Qualifier paraphrasing.** The extractor lists qualifiers (e.g. "JSON to stdout with flags for DOT and Mermaid"), but they are *summary* sentences pulled from the source, not the original conditional clauses. The NRS judge can tell — it consistently scored 2-3 even on rich cases.

3. **Schema-as-bucket assignment problem.** Items that should be hard_constraints often land in soft_preferences (with confidence 0.8) because the extractor can't tell if "JSON output is the default" is a binding spec or a recommendation. Without a typology hint, the assignment is noisy.

4. **Assumption inflation as catch-all.** `assumptions` got used 100% of the time, but ~30% of extracted assumptions are restating the prompt's framing ("we assume the project will be evaluated for potential") rather than identifying genuinely implicit beliefs. The `blast_radius` label was set but did not gate downstream behavior — every blast_radius came back medium or high, never low. The field is essentially unused.

5. **Decision_candidates barely fires.** In 27/40 cases the extractor found zero forks worth flagging. Either users in this corpus don't articulate options, or "is this a decision_candidate vs a soft_preference?" is too fine a distinction for the extractor to make from short text. Either way the bucket is currently inert.

6. **Anticipation requires open-world reasoning.** CPR was the only metric the structured state could plausibly help with, because corrections often touch axes that should have been flagged as open. But the median was 1.0 (often we have at least *some* assumption on a touched axis) while the mean (0.67) shows a heavy left tail of "we had no idea this axis existed". Open-world axes (`ast-grep language modes`, `re-export glob patterns`) are not knowable from initial prompt + planning docs.

---

## Top 3 schema problems to fix before production use

1. **Drop `decision_candidates` or merge it into `soft_preferences`.** 67% non-use rate. The "explicit fork with plausibilities" framing requires users to write in a way they don't. Replace with a single `open_decisions` bucket whose entries can be free-form ("storage backend: TBD, sqlite likely") — easier to fill, downstream judges still match.

2. **Replace `blast_radius` with axis-level `interrogate_priority`.** `blast_radius` always came back medium/high, providing no signal. What the compiler actually needs is a priority score per *axis* (output_format, error_handling, etc.) indicating which assumptions are worth asking the user about. Move from per-assumption ordinal label to per-axis prioritisation.

3. **Make `qualifiers` carry source spans, not paraphrase.** NRS suffered because the extractor's qualifier text is a summary. Require the extractor to quote the source verbatim (with offset references) when populating qualifiers. The cost is moderate (qualifiers per case ≈ 1) and the nuance preservation gain is the difference between NRS 2 and NRS 4.

Secondary: `hard_constraints` and `soft_preferences` are not differentiable by the extractor without a typology hint. Either provide a vocabulary of axes with strict/preferred semantics, or collapse them.

---

## Cost

| Step | Calls | Model | Tokens (prompt / completion) | Cost |
|---|---|---|---|---|
| build_structured_state | 40 | gpt-4o-mini | ~150k / 22k | $0.017 |
| generate_requirements  | 40 | gpt-4o-mini | ~90k / 18k | $0.012 |
| score_assumption_1 (extractors + binary judges) | 576 | gpt-4o-mini | ~170k / ~0 | $0.054 |
| score_assumption_1 (NRS) | 40 | gpt-4o | 68k / 1.9k | $0.190 |
| **Total** |  |  |  | **$0.276** |

Well under the $10 cap. Re-runs from cache cost $0.

---

## What this means for the product

- **Negative result is informative.** The structured compiler cannot fulfil its promise with just the user's first message. Either the extractor needs to see more (mid-session signal, repo conventions, related historical tasks) or the compiler needs to be paired with a question-asking loop that fills the buckets interactively. The schema is not the bottleneck; the input is.
- **Assumption 1 stands or falls with the input model.** Re-running this experiment with sessions that include the user's *answers to clarifying questions* would likely move CDR significantly. That is effectively the Assumption-2 test.
- **Schema needs minor surgery, not redesign.** The fields that fired hard (hard_constraints, assumptions, qualifiers) capture the right axes when there's text to read. Drop `decision_candidates`, fix `qualifiers` to preserve verbatim spans, retire `blast_radius` for an axis-priority signal, and the schema is roughly viable.
- **Recommended next step:** before re-running Assumption 1, redesign the *input contract* — give the extractor (a) the initial prompt, (b) the user's responses to N clarifying questions, (c) a snapshot of the most similar prior task. Then re-measure CDR. If the schema is still the bottleneck, we'll see it cleanly because the input richness will be controlled.
