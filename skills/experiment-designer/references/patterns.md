# Patterns and Anti-Patterns

Distilled from running multi-phase experiments. See `docs/experiments/PATTERNS.md` for the project's canonical version of this list — this file is a skill-local copy kept synchronized.

## Good patterns to repeat

### Decision matrix at the end of every design doc

Before running anything, commit to what each outcome combination would *mean*. Without it, "partial fail" results devolve into endless interpretation. The matrix forces the call to be made before the data biases it.

### Cache-first design in every worker

Every script should treat "die mid-execution and resume" as the default execution mode, not an edge case. SQLite or JSONL checkpoint per unit of work. Idempotent re-runs are free. Workers WILL fail mid-run; the cache is what makes retries trivial instead of expensive.

### Findings appended in-place, not in a separate file

Keep hypothesis + result in the same markdown doc forever. A reader two years later sees the prediction *and* the outcome side by side. Easier to retain context, harder to misrepresent results.

### Honesty hooks in worker prompts

Workers will report what they think you want unless you explicitly ask for what they think you don't want. Prompts like:

- "If a stage looks expensive, scale the dataset DOWN rather than skipping a metric"
- "Report what actually happened, not what should have happened"
- "Document caveats, surprises, and red flags downstream spikes should know about"

These produce markedly better caveats. Real example: Spike 5's worker proactively flagged the session-identity leakage that became Spike 7's headline finding — directly because the prompt asked for caveats.

### Synthetic ground truth as a tiebreaker

When proxy metrics start getting argued about (e.g. "is ρ=0.45 real signal or session leakage?"), spin up a synthetic generator and settle it. Synthetic data lets you control what the right answer is, so you can isolate "is the structure even there?" from "is our method finding it?" Real example: Phase 3 took 25 minutes to settle questions Phase 1+2 left open for days.

### Cumulative budget tracking

State "$X budget, Y spent so far, Z remaining" at the top of every dispatch. Makes cost decisions trivial.

### Foundation serial → ablations parallel → synthesis

For any experiment with a generation step followed by independent analyses:

1. Run the generation/foundation step alone (everything downstream depends on it)
2. Dispatch independent analyses in parallel
3. Synthesize findings into a single doc

Reliable shape. Use multiple Agent tool calls in one message to actually parallelize.

### Pre-registration of what would change the verdict

Before running, write down: "if M1 R² < 0.3 we conclude the framework is dead." Then when the number comes back at 0.276, the verdict isn't negotiable. Go/no-go matrices do this implicitly; making it explicit per-metric is even better.

### "What would have changed our mind" coda

End each writeup with: "if these numbers had come back differently we would have done Z instead." Forces counterfactual reasoning, which is when post-hoc rationalization gets caught.

### Date-prefixed experiment folders

`2026-05-26-orthogonal-questioning/` not just `orthogonal-questioning/`. Chronological sort is the default ordering and the date is durable context that's otherwise easy to lose.

## Anti-patterns to avoid

### A1. Trusting a worker's exit signal

**Workers can fail-after-succeeding.** Real examples:
- A worker returned "API Error: Overloaded" but had written every deliverable before failing on the final report
- Another returned a confused early-bailout message but had cached most of its work first

**Rule**: always check the filesystem (and the cache contents) before believing a worker's exit signal. Look at `*_results.json`, `*_notes.md`, and any PNGs before deciding the run failed.

### A2. Open questions left implicit

Defaulting silently on open design questions worked once but is fragile. Resolve them in writing before dispatching workers.

### A3. Vague success criteria

Worker prompts that said "test whether X works" without "X works means metric Y crosses threshold Z" got mushy back-and-forth findings. Every metric in the deliverables JSON should have a stated pass/fail threshold in the design doc.

### A4. Mixing exploration and execution in one prompt

Workers asked to *design and run* simultaneously tend to do both badly. Design phase produces a tight spec, execution phase implements it. Two separate prompts.

### A5. Missing "do not stop early" guards

Workers will occasionally bail at the first hurdle if the prompt doesn't explicitly forbid it. Include explicit guards: "Do NOT report success without producing the actual numbers from real PCA runs." "Do NOT exit until decisions.jsonl exists and has content."

### A6. Premature LLM enrichment

A real example: an LLM-rewrite format *looked* like it should help and was implemented across the full dataset before being interrogated. The rewrites homogenized all decisions into a uniform style — a five-minute sanity check on a few outputs would have caught it.

**Rule**: always inspect 5-10 raw outputs of any LLM transformation before sending the whole batch.

### A7. Workers' final reports getting truncated

Sub-agent final-report content occasionally arrives as a confused message or "API error" even though the actual work succeeded. Don't rely on the worker's chat-output reply being complete. Ground-truth findings by reading the `*_notes.md` and `*_results.json` artifacts the worker produces.

### A8. Spreading findings across chat without capturing them

The biggest cost of long experiments: high-value synthesis happens in conversation and gets lost if not captured. At the end of every experiment, sweep the conversation for findings that aren't in a markdown doc. If it took more than three sentences to explain, it belongs in a file.

## Dispatch checklist

Before launching a worker:

- [ ] Design doc exists at `docs/experiments/YYYY-MM-DD-name/`
- [ ] Hypothesis stated
- [ ] Success/fail thresholds stated per metric
- [ ] Go/no-go decision matrix written (for multi-step experiments)
- [ ] Budget stated upfront with currency and units
- [ ] Open questions resolved in writing
- [ ] Worker prompt has "do not exit early" guards
- [ ] Worker prompt asks for caveats explicitly
- [ ] Cache strategy specified (SQLite/JSONL keyed by content hash)
- [ ] Deliverables list ends with both a JSON and a notes.md

## End-of-experiment checklist

- [ ] All metric numbers landed in `*_results.json`
- [ ] `*_notes.md` exists and explains the result in human terms
- [ ] Findings section appended to the design doc
- [ ] "What would have changed our mind" coda written
- [ ] Conversation swept for synthesis that's only in chat
- [ ] Cumulative cost across all phases tracked in synthesis README
- [ ] Code and data in `.tmp/`; findings in `docs/experiments/YYYY-MM-DD-name/`
