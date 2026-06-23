# Worker Prompt Template

Workers dispatched via the Agent tool see none of your conversation. The prompt must be self-contained — like briefing a smart colleague who just walked into the room.

This template is structured. Every section is load-bearing.

---

## Template

```
You are a worker implementing <Phase N> of <experiment name>. Your job: <one
sentence stating exactly what the worker delivers>.

## Context

<2-4 sentences setting the larger framing. Why does this experiment exist?
What did upstream phases find? What's the worker's role in the bigger picture?>

Read the design doc fully before starting:
`<absolute path to docs/experiments/YYYY-MM-DD-name/phaseN-topic.md>`

It defines the hypothesis, the metric thresholds, the decision matrix, and the
deliverables. Follow it precisely on the headline shape. Adapt freely on the
implementation.

## Environment

- Working directory: `<absolute path>`
- Python / Go / tools: `<exact versions and how to invoke>`
- API keys: `<absolute path to .env file>` (load via dotenv; do NOT hardcode)
- Caches you can reuse from prior phases: `<list with paths and schemas>`
- Tools available: `<autosearch, autoetl, etc., with example commands>`

## Input data

<Concrete description: which file, which fields, expected scale. Include a
sample row if non-obvious.>

## What to build

<Detailed implementation spec. Break into numbered steps. Each step should be
implementable independently. Include code sketches for non-obvious parts.>

### Step 1: <...>
### Step 2: <...>
### Step 3: <...>

## Success criteria

Reproduce the thresholds from the design doc exactly. The worker should not
re-derive these; copy them.

- Metric 1 passes if ≥ X
- Metric 2 passes if ≤ Y
- ...

## Deliverables (REQUIRED)

The worker must produce these exact files at these exact paths. Your final
report should point at them.

1. `<path>/<script>.py` — the implementation
2. `<path>/<phase>_results.json` — every threshold from "Success criteria"
   becomes a value here. Schema:
   ```json
   {
     "metric_1": {"value": ..., "threshold": X, "pass": bool},
     "metric_2": {...},
     "verdict": "pass" | "partial" | "fail"
   }
   ```
3. `<path>/<phase>_<chart>.png` — diagnostic plot(s)
4. `<path>/<phase>_notes.md` — under 800 words, human-readable narrative

## Practical guidance

- **Cache aggressively.** Every unit of work — every LLM call, every embedding,
  every API response — should be cached in SQLite or a content-addressed JSONL
  file. Idempotent re-runs are critical because workers will fail mid-run and
  the cache is what makes resume free.
- **Concurrency.** Use asyncio with a semaphore of ~10 for any API calls.
  Sequential will be too slow for non-trivial datasets.
- **If a stage looks expensive, scale the dataset DOWN** (e.g. fewer users,
  fewer trials) rather than skipping a metric. Every metric in the success
  criteria must have a real number.
- **Sanity-check LLM transformations on 5-10 samples before running the batch.**
  This is the single highest-leverage practice — catches uniformity / mode-collapse
  failures that cost hours to discover from aggregate results.

## Hard rules (don't break these)

- Do NOT report success without producing actual numbers from real runs.
- Do NOT exit early. If you hit a problem, scale the experiment down and finish.
- Do NOT skip the spot-check on LLM transformations.
- Do NOT spend more than $<budget>. If you find yourself approaching it, stop
  and report.

## Final report to me

Reply with under <300-500> words:

1. **Verdict** (Pass / Partial / Fail) per the success criteria.
2. **Headline numbers** for each metric, including pass/fail vs threshold.
3. **Surprises / caveats** — what should downstream phases know? What
   confounds the result? What did you have to work around?
4. **Methodology choices that matter** — places where you made a judgement call
   that future readers should know about (e.g. how you handled missing data,
   what threshold you picked for a derived parameter, whether you used
   centroid defaults, etc.).
5. **Paths to deliverables** (all absolute).

Begin.
```

---

## What each section is doing

**Context** — gives the worker enough of the bigger picture to make reasonable judgment calls. Without it, workers follow instructions literally and miss obvious adjustments.

**Environment** — eliminates the dozen "how do I…" follow-up questions that would otherwise eat worker tool budget.

**Input data** — concrete enough that the worker doesn't have to discover the schema by trial and error.

**What to build** — broken into numbered steps so the worker can check off progress and so the cache strategy maps to specific stages.

**Success criteria** — copied from the design doc verbatim. The worker should not be inferring thresholds from prose; they should be hitting numbers stated explicitly.

**Deliverables** — explicit, with exact paths. This is what you validate against, not the worker's chat reply.

**Practical guidance** — captures lessons from prior experiments that the worker would otherwise rediscover the hard way.

**Hard rules** — the things that, if violated, mean the experiment didn't actually run. Worth being firm.

**Final report** — structured ask. "Reply with verdict + numbers + caveats + methodology + paths" produces dramatically better reports than "summarize what you found."

## Honesty hooks (sprinkle these throughout the prompt)

Phrases that produce markedly better caveats:

- "Document caveats, surprises, and red flags downstream spikes should know about."
- "Report what actually happened, not what should have happened."
- "If extraction quality is poor, mark it. If the numbers are statistically weak, mark it."
- "Make every methodology choice that wasn't obvious from this prompt explicit in your final report."
- "Do not interpret results — just report them. The orchestrator will interpret."

## Anti-patterns in worker prompts

- **Vague success criteria** ("test whether X works" instead of "pass if R² > 0.5"). Produces mushy reports.
- **No "do not exit early" guard.** Workers will bail at the first obstacle if not explicitly forbidden.
- **No explicit cache strategy.** Workers will not invent good resumability on their own.
- **Mixed design + execution prompt.** Workers asked to both design and run tend to do both badly. Separate the prompts.
- **No deliverable file paths.** Without explicit paths to validate, you can't trust the worker's "done" signal.
- **Asking for interpretation instead of numbers.** "Tell me what this means" → mushy prose. "Report the value of metric M; if it crosses threshold T, mark it as pass" → useful number.
