---
name: experiment-designer
description: "Design and run a formal hypothesis-driven experiment end-to-end — write the design doc with go/no-go criteria, dispatch workers, validate their output, and capture findings. Use when 'design an experiment', 'experiment-designer', 'run an experiment to test', 'set up a hypothesis test', 'fan out experiments to figure out', or when the user wants a structured multi-phase investigation rather than a quick exploration. Distinct from tech-spike, which is for fast informal exploration; use experiment-designer when the question deserves a written hypothesis, explicit pass/fail thresholds per metric, and findings that get committed to git as long-lived artifacts."
---

# Experiment Designer

Design and run a formal experiment with stated hypothesis, explicit pass/fail thresholds, and findings that get checked into git as durable artifacts. Optimized for the case where a research question is too important to settle by intuition and the answer is worth keeping.

## When to use vs. neighbors

- **experiment-designer** (this skill): formal research question with hypothesis + thresholds + worker dispatch + findings committed to `docs/experiments/`. Multi-phase OK.
- **tech-spike**: quick informal exploration, scratch work in `.tmp/`, no formal writeup.
- **new-task / new-solution / new-plan**: planning to build a known feature, not testing a research question.

Trigger experiment-designer when: the user states a question with multiple plausible answers, the cost of guessing wrong is high enough that a $1-30 OpenAI bill is a good trade, and the answer would inform a real design decision.

## The full lifecycle

Four phases. Each gates the next.

### Phase 1: Design

1. **Capture the hypothesis in one sentence.** "X works for Y" or "method A beats method B on metric M." If you cannot state it crisply, the experiment isn't ready yet — go back to the user with clarifying questions.

2. **Pick the metric(s) and the pass/fail threshold for each.** Vague success criteria produce vague reports. For each metric: write the number that would make you conclude "yes" and the number that would make you conclude "no." Anything in between is "partial" and needs interpretation. The act of pre-committing to thresholds is what makes the experiment formal.

3. **Write the go/no-go decision matrix.** If the experiment has multiple sub-tests, enumerate the outcome combinations and what each one would mean. Example:

   ```
   | Spike 1 | Spike 2 | Spike 3 | Verdict |
   | Pass    | Pass    | Pass    | Full go. Build the thing. |
   | Pass    | Fail    | Pass    | Pivot scope. Restrict to subset. |
   | Fail    | Pass    | Pass    | Pivot model. Different mechanism. |
   | ...     | ...     | ...     | ... |
   ```

   This is load-bearing. Without it, partial results devolve into endless interpretation. Decide upfront what each outcome means.

4. **State the budget.** Cost (dollars), time (worker hours), and what to do if it's exceeded. "If you spend more than $5, stop and report" is a useful guardrail in worker prompts.

5. **Write the design doc** at `docs/experiments/YYYY-MM-DD-<name>/<phase>.md` using the structure in [references/design-doc-template.md](references/design-doc-template.md). Date-prefix the folder.

6. **Resolve open questions in writing before dispatching.** If the design has "open questions for the designer," answer them in the doc (or in the worker prompt) — don't default them silently.

### Phase 2: Execute

7. **Dispatch workers via the Agent tool, one experiment per worker.** Each worker prompt should be self-contained — the worker has no context from your conversation. Follow [references/worker-prompt-template.md](references/worker-prompt-template.md).

8. **For multi-step experiments, foundation serial → ablations parallel → synthesis.** Run the generation/foundation step alone (everything downstream depends on it), then dispatch independent analyses in parallel, then synthesize. Multiple Agent calls in a single message to actually parallelize.

9. **Insist on cache-first design.** Every script the worker writes should treat "die and resume" as the default execution mode. SQLite or JSONL checkpoint per unit of work. This is non-negotiable — workers WILL fail mid-run, and the cache is what makes retries free.

10. **Honesty hooks in the prompt.** Phrases that produce dramatically better caveats:
    - "Document caveats, surprises, and red flags downstream spikes should know about."
    - "Report what actually happened, not what should have happened."
    - "If a stage looks expensive, scale the dataset DOWN rather than skipping a metric."
    - "Do NOT report success without producing actual numbers from real runs."

### Phase 3: Validate

Workers will sometimes return confusing messages, "API error" replies, or abbreviated reports while having actually completed the work. **Never trust the worker's exit signal alone.**

11. **Check the filesystem for the deliverables before believing any worker report.** Every worker prompt should list explicit deliverable files (a `*_results.json`, a `*_notes.md`, plots). Confirm these exist and have meaningful content.

12. **Read the worker's `*_notes.md` directly.** That's where the honest narrative lives, including caveats the chat reply might have truncated.

13. **If the worker bailed early, resume don't restart.** Check the cache (sqlite/jsonl). If most work is done, run the script yourself to finish; the cache makes resuming nearly free.

14. **Sanity-check LLM transformations on 5-10 samples before trusting the batch.** This is the single highest-leverage validation step. The Phase 2 HyDE failure (an LLM rewrite that homogenized all decisions into a uniform style) would have been caught in 5 minutes by inspecting raw outputs.

### Phase 4: Report

15. **Append findings to the design doc in place** rather than writing a separate findings file. Keeps hypothesis + result in one place forever. A reader two years later sees the prediction and the outcome side by side.

16. **State the verdict against the pre-committed thresholds.** No retroactive goalpost shifting. If a metric came in at 0.276 and the threshold was 0.3, that's a fail — note it as such, then discuss interpretation.

17. **Include a "what would have changed our mind" coda.** "If these numbers had come back differently we would have done Z instead." Forces counterfactual reasoning, which is where post-hoc rationalization gets caught.

18. **Update the experiment folder's `README.md`** if the experiment is part of a multi-phase program — synthesize the new phase into the cross-phase narrative.

19. **Sweep the conversation for findings that didn't make it into markdown.** Concepts, syntheses, recommendations, comparisons that took more than three sentences to explain in chat belong in a file. The git-checked-in artifacts are the durable record; chat is not.

## Folder convention

```
docs/experiments/
  README.md                          # index of all experiments
  PATTERNS.md                        # see docs/experiments/PATTERNS.md if it exists
  YYYY-MM-DD-<name>/
    README.md                        # cross-phase synthesis (multi-phase only)
    phase1-<topic>.md                # design + findings appended in place
    phase2-<topic>.md
    ...
    <topic>-architecture.md          # deployable artifacts, if any
    <topic>-research.md              # background research, if any

.tmp/experiments/YYYY-MM-DD-<name>/  # code, data, embeddings, plots — NOT in git
```

The git-checked-in artifacts are the markdown ones in `docs/experiments/`. Everything else stays in `.tmp/`.

## Anti-patterns to avoid

These have all bitten real experiments in this codebase. Treat them as hard rules:

1. **Don't trust a worker's exit signal.** Always check the filesystem. Workers can fail-after-succeeding.
2. **Don't leave open questions implicit.** If the design doc has open questions, resolve them in writing before dispatching.
3. **Don't write vague success criteria.** Every metric needs a stated pass/fail threshold in the design doc.
4. **Don't mix exploration and execution in one prompt.** Design produces a tight spec; execution implements it. Two separate prompts.
5. **Don't skip the "do not stop early" guard in worker prompts.** Workers will occasionally bail at the first hurdle if not explicitly forbidden.
6. **Don't trust LLM transformations without inspecting samples.** 5-minute sanity check beats a wasted $5 batch.
7. **Don't spread findings across chat without capturing them.** Sweep the conversation at the end and write the captured findings to markdown.

See [references/patterns.md](references/patterns.md) for the full list and rationale.

## Pre-dispatch checklist

Before launching any worker, the design doc should have:

- [ ] Hypothesis stated in one sentence
- [ ] Success/fail thresholds stated per metric
- [ ] Go/no-go decision matrix (for multi-step experiments)
- [ ] Cost budget stated upfront with currency and units
- [ ] Open questions resolved in writing
- [ ] Cache strategy specified (sqlite/jsonl keyed by content hash)
- [ ] Deliverables list ends with both a `*_results.json` and a `*_notes.md`
- [ ] Worker prompt has "do not exit early" guards
- [ ] Worker prompt asks for caveats explicitly

## End-of-experiment checklist

- [ ] All metric numbers landed in the `*_results.json`
- [ ] `*_notes.md` exists and explains the result in human terms
- [ ] Findings appended to the design doc (not a separate file)
- [ ] "What would have changed our mind" coda written
- [ ] Conversation swept for synthesis that's only in chat
- [ ] Cumulative cost across all phases tracked in the synthesis README
- [ ] Code and data in `.tmp/`; findings in `docs/experiments/YYYY-MM-DD-name/`

## Example to study

A complete worked example of this pattern, including four phases with all anti-patterns observed in the wild, is at `docs/experiments/2026-05-26-orthogonal-questioning/`. Read the `README.md` synthesis first, then the per-phase docs. The patterns in this skill come directly from running that experiment program.

## Templates

- [references/design-doc-template.md](references/design-doc-template.md) — the structure for each phase doc
- [references/worker-prompt-template.md](references/worker-prompt-template.md) — how to dispatch a worker
- [references/patterns.md](references/patterns.md) — full patterns and anti-patterns from real experiments
