# Experiment Patterns and Anti-Patterns

Distilled from running multi-phase experiments in this folder. The intent is to make future experiments faster, more honest, and easier to interrogate after the fact. Update this file as new lessons emerge.

## Good patterns to repeat

### 1. Decision matrix at the end of every design doc

Before running anything, commit to what each outcome combination would *mean*. Example from the orthogonal-questioning Phase 1 doc:

```
| Spike 1 | Spike 2 | Spike 3 | Verdict |
|---|---|---|---|
| Pass | Pass | Pass | Full go. Build the GP framework. |
| Fail | Pass | Pass | Pivot embeddings... |
| ... | ... | ... | ... |
```

Without it, "partial fail" results devolve into endless interpretation. The matrix forces the call to be made before the data biases it.

### 2. Cache-first design in every worker

Every script should treat "die mid-execution and resume" as the default execution mode, not an edge case. SQLite or JSONL checkpoint per unit of work. Idempotent re-runs are free.

Phase 1 Spike 0 died at 200/321 messages; the SQLite cache made resume free. Without it, the rerun would have cost the full $0.50 again and another 20 minutes.

### 3. Findings appended in-place, not in a separate file

Keep hypothesis + result in the same markdown doc forever. A reader two years later sees the prediction *and* the outcome side by side. Easier to retain context, harder to misrepresent results.

### 4. Honesty hooks in worker prompts

Workers will report what they think you want unless you explicitly ask for what they think you don't want. Prompts like:

- "If a stage looks expensive, scale the dataset DOWN rather than skipping a metric"
- "Report what actually happened, not what should have happened"
- "Document caveats, surprises, and red flags downstream spikes should know about"

These produce markedly better caveats. Phase 2 Spike 5's worker proactively flagged the session-identity leakage that became Spike 7's headline — because the prompt asked for caveats.

### 5. Synthetic ground truth as a tiebreaker

When proxy metrics start getting argued about (e.g. "is ρ=0.45 real signal or session leakage?"), spin up a synthetic generator and settle it. The orthogonal-questioning Phase 3 took 25 minutes to settle questions Phase 1+2 left open for days. Synthetic data lets you control what the right answer is, so you can isolate "is the structure even there?" from "is our method finding it?"

### 6. Cumulative budget tracking

State "$X budget, Y spent so far, Z remaining" at the top of every dispatch. Makes cost decisions trivial. The orthogonal-questioning arc finished under $2 of a $30 budget because we tracked.

### 7. Foundation serial → ablations parallel → synthesis as the dispatch shape

For any experiment with a generation step followed by independent analyses:

1. Run the generation/foundation step alone (everything downstream depends on it)
2. Dispatch independent analyses in parallel
3. Synthesize findings into a single doc

Reliable shape. Both phases of the orthogonal-questioning experiment used it.

### 8. Pre-registration of what would change the verdict

Before running, write down: "if M1 R² < 0.3 we conclude the framework is dead." Then when the number comes back at 0.276, the verdict isn't negotiable. Phase 1's go/no-go matrix did this implicitly; making it explicit per-metric is even better.

### 9. "What would have changed our mind" coda

End each writeup with: "if these numbers had come back differently we would have done Z instead." Forces counterfactual reasoning, which is when post-hoc rationalization gets caught.

### 10. Date-prefixed experiment folders

`2026-05-26-orthogonal-questioning/` not just `orthogonal-questioning/`. Chronological sort is the default ordering and the date is durable context that's otherwise easy to lose.

## Anti-patterns to avoid

### A1. Trusting a worker's exit signal

**Workers can fail-after-succeeding.** Phase 2 Spike 9 returned "API Error: Overloaded" but had written every deliverable before failing on the final report. Phase 1 Spike 0 returned a confused early-bailout message but had cached most of its work first.

**Rule**: always check the filesystem (and the cache contents) before believing a worker's exit signal. Look at `*_results.json`, `*_notes.md`, and any PNGs before deciding the run failed.

### A2. Open questions left implicit

Phase 2's design doc had three "Open Questions for the Designer" at the bottom. I defaulted them silently when dispatching workers. Worked that time, but on a harder experiment the defaults could have been wrong.

**Rule**: resolve open questions in writing before dispatching. Either edit the design doc with the decisions, or document them in the worker prompt.

### A3. Vague success criteria

Early worker prompts that said "test whether X works" without "X works means metric Y crosses threshold Z" produced mushy back-and-forth findings.

**Rule**: every metric in the deliverables JSON should have a stated pass/fail threshold in the design doc.

### A4. Mixing exploration and execution in one prompt

Workers asked to *design and run* simultaneously tend to do both badly.

**Rule**: design phase produces a tight spec (could be inline in the doc), execution phase implements it. Two separate prompts.

### A5. Missing "do not stop early" guards

Workers will occasionally bail at the first hurdle if the prompt doesn't explicitly forbid it.

**Rule**: include explicit guards in worker prompts. "Do NOT report success without producing the actual numbers from real PCA runs." "Do NOT exit until decisions.jsonl exists and has content." "If a stage fails, scale data down rather than skip a metric."

### A6. Premature LLM enrichment

Phase 2's HyDE failure (Spike 5 format F5) was the clearest example. An LLM rewrite *looked* like it should help and was implemented across the full dataset before being interrogated. The rewrites homogenized all decisions into a uniform style — a five-minute sanity check on a few outputs would have caught it.

**Rule**: always inspect 5-10 raw outputs of any LLM transformation before sending the whole batch.

### A7. Workers' final reports getting truncated by the harness

Sub-agent final-report content occasionally arrives as a confused message or "API error" even though the actual work succeeded. We saw this twice across phases.

**Rule**: don't rely on the worker's chat-output reply being complete. Ground-truth findings by reading the `*_notes.md` and `*_results.json` artifacts the worker is asked to produce.

### A8. Spreading findings across chat without capturing them

By far the biggest cost of the orthogonal-questioning experiment: a lot of high-value synthesis happened in conversation (geometric-vs-discriminative explanation, deployable architecture, templates comparison, embedding model research) and would have been lost if we hadn't gone back to capture it.

**Rule**: at the end of every experiment, sweep the conversation for findings that aren't in a markdown doc. If it took more than three sentences to explain, it belongs in a file.

## Proposed dispatch checklist (use before launching a worker)

- [ ] Design doc exists at `docs/experiments/YYYY-MM-DD-name/`
- [ ] Hypothesis stated
- [ ] Success/fail thresholds stated per metric
- [ ] Go/no-go decision matrix written (for multi-step experiments)
- [ ] Budget stated upfront with currency and units
- [ ] Open questions resolved in writing
- [ ] Worker prompt has "do not exit early" guards
- [ ] Worker prompt asks for caveats explicitly
- [ ] Cache strategy specified (SQLite/JSONL/pickle keyed by content hash)
- [ ] Deliverables list ends with both a JSON and a notes.md

## Proposed end-of-experiment checklist

- [ ] All metric numbers landed in the `*_results.json`
- [ ] `*_notes.md` exists and explains the result in human terms
- [ ] Findings section appended to the design doc
- [ ] "What would have changed our mind" coda written
- [ ] Conversation swept for synthesis that's only in chat
- [ ] Cumulative cost across all phases tracked in synthesis README
- [ ] Code and data in `.tmp/`; findings in `docs/experiments/YYYY-MM-DD-name/`

## Recommended folder layout

```
docs/experiments/
  README.md                          # index of experiments
  PATTERNS.md                        # this file
  YYYY-MM-DD-name/
    README.md                        # synthesis across all phases
    phase1-<topic>.md                # per-phase design + findings
    phase2-<topic>.md
    ...
    <topic>-architecture.md          # deployable patterns, if any
    <topic>-research.md              # background research notes, if any
```

Code, data, embeddings, plots stay in `.tmp/experiments/YYYY-MM-DD-name/` (or wherever the worker put them). The git-checked-in artifacts are the markdown ones only.
