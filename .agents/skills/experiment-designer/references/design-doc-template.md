# Design Doc Template

A working template for a single experiment phase. Copy, fill in, save at `docs/experiments/YYYY-MM-DD-<name>/<phase>.md`.

The exact section names are less important than the discipline of stating each one explicitly. If a section feels empty, that's a signal — either you have unresolved design work to do, or the section genuinely doesn't apply (rare).

---

```markdown
# Spike: <Topic> (Phase N)

<1-2 sentence framing of what this phase tests within the larger experiment program. If this is a standalone experiment, state the question directly.>

## Hypothesis

<One sentence. "X works for Y" or "method A beats method B on metric M."
If you can't state it in one sentence, the experiment isn't ready.>

## Prerequisites

- <Required tools, data sources, environment>
- <Outputs from prior phases that this one consumes>
- <Cost/time budget for this phase>

## What we're going to do

<3-5 paragraphs describing the experimental setup. Be concrete: which dataset,
which models, which metrics, which method.>

## Success criteria

For each metric, state the pass and fail thresholds. Be specific.

- **Metric 1**: <name>. Pass if ≥X. Fail if <Y. Partial in between.
- **Metric 2**: <name>. Pass if ...
- ...

Anything in between pass and fail is "partial" and needs interpretation, but
the interpretation should be bounded by the matrix below.

## Go/no-go decision matrix

If this experiment has multiple sub-tests, enumerate the outcome combinations
and what each one would mean. This forces commitment before the data biases
interpretation.

| Sub-test A | Sub-test B | Sub-test C | Verdict |
|---|---|---|---|
| Pass | Pass | Pass | <action> |
| Pass | Pass | Fail | <action> |
| Pass | Fail | Pass | <action> |
| ... | ... | ... | ... |

For single-test experiments, the matrix collapses to:

| Result | Verdict |
|---|---|
| Pass | <action> |
| Partial | <action> |
| Fail | <action> |

## Deliverables

The worker should produce:

1. `<script>.py` — the implementation
2. `<phase>_results.json` — full metrics (every threshold from "Success criteria" gets a value here)
3. `<phase>_<chart>.png` — diagnostic plot(s)
4. `<phase>_notes.md` — human-readable summary under 800 words

## Cost budget

<Estimated OpenAI / compute spend. State currency. State hard stop.>

## What we will *not* test in this phase

<Explicit non-goals. Sets expectations and prevents scope creep.>

## Open questions

<Either resolved in writing before dispatch, or marked as TBD with a clear
"default if no answer received" stance.>

---

## Findings (run YYYY-MM-DD)

Appended after the experiment completes. Same doc, not a separate file.

### Headline verdict

<Pass / Partial / Fail against the success criteria. State the verdict against
the pre-committed thresholds — no retroactive goalpost shifting.>

### Headline numbers

| Metric | Threshold | Result | Pass? |
|---|---|---|---|
| ... | ... | ... | ... |

### What we found

<2-5 paragraphs of substantive findings. Lead with the surprises, not the
expected results. Quote actual numbers.>

### Caveats

<Things that complicate the result: methodology limitations, dataset quirks,
worker-flagged concerns, anything a future reader needs to know to interpret
the numbers correctly.>

### What would have changed our mind

<"If these numbers had come back differently we would have done Z instead."
Counterfactual coda. Forces honesty about whether the result actually mattered
to the decision.>

### Implications for next steps

<What this result means for the experiment program / project. If this phase
gates other phases, state which ones become viable or get dropped.>

### Cost

<Actual OpenAI / compute spend for this phase. State cumulative across the
experiment program if multi-phase.>
```
