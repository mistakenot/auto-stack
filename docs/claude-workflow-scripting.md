---
hash: "e7678a88"
read_when: "understanding how Claude decomposes tasks into multi-agent workflows, or deciding whether auto-etl should ingest workflow scripts and run journals"
summary: "Analysis of Claude Code Workflow .js orchestration scripts and run journals found under ~/.claude, revealing how Claude decomposes tasks into multi-agent harnesses — and that auto-etl ingests the subagent transcripts but not the scripts/journals that orchestrate them."
title: "How Claude Scripts Tasks: Evidence from Workflow Artifacts"
---

# How Claude Scripts Tasks: Evidence from Workflow Artifacts

*Research report — 2026-06-09. Source: Claude Code Workflow artifacts found under `~/.claude/projects/`.*

## Summary

Claude Code's Workflow feature writes a JavaScript orchestration script to disk for
each long-running, multi-agent task, runs it in the background, and records a JSON
journal plus one transcript per spawned subagent. These scripts are the single richest
**declarative record of how Claude decomposes a task** — an explicit phase plan, a
dataflow graph, the verification strategy, and every subagent prompt, all in one file.

This report analyses six real runs found on this machine. The headline findings:

1. **Claude picks its control-flow topology from the task's epistemics**, not its size.
   Implementation tasks become linear phase pipelines; research/discovery tasks become
   fan-out → verify → synthesize graphs with adversarial voting.
2. **Deterministic work stays in JavaScript; only judgment is delegated to subagents.**
   Dedup, ranking, budget accounting, and vote-counting are plain code.
3. **The bulk of the engineering is defensive degradation** — never discarding completed
   work because a later step failed.
4. **These artifacts are invisible to `auto-etl`** (they are `.js`/`.json`, not `.jsonl`),
   even though the subagent transcripts they spawn *are* ingested. We capture the leaves
   and lose the trunk.

## Method

Found via `find ~/.claude -path '*/workflows/*'`. On-disk layout per run:

```
~/.claude/projects/<project>/<session-uuid>/
├── workflows/
│   ├── scripts/<name>-wf_<runid>.js   ← generated orchestration script
│   └── wf_<runid>.json                ← run journal (script, result, logs, metrics)
└── subagents/workflows/wf_<runid>/
    ├── agent-<id>.jsonl               ← one transcript per spawned subagent
    └── agent-<id>.meta.json           ← {"agentType":"workflow-subagent"}
```

Two scripts were read in full (`deep-research`, `clear-otlp-tasks`); journal metrics were
extracted from all six runs.

## The corpus

| Workflow | Task shape | Phases | Agents | Tokens | Tool calls | Wall-clock | Model |
|---|---|---:|---:|---:|---:|---:|---|
| `deep-research` | research | Scope · Search · Fetch · Verify · Synthesize | 105 | 1.98M | 610 | ~18 min | opus-4-8[1m] |
| `arxiv-level8-explore` | research | Explore · Exploit · Synthesise | 13 | 517K | 384 | ~21 min | opus-4-6 |
| `author-anthropic-architect` | authoring | Author nodes | 18 | 733K | 73 | ~1.8 min | opus-4-8[1m] |
| `clear-otlp-tasks` | implementation | Infra · Scripts · E2E · Docs · Close | 11 | 224K | 78 | ~6.8 min | opus-4-6 |
| `build-all-agent-logs` | implementation | 5 build phases | 13 | 259K | 148 | ~7.2 min | opus-4-6 |

Fan-out spans **11 → 105 subagents** and token spend **0.22M → 1.98M** in a single task —
nearly a 10× range, set entirely by task shape rather than nominal difficulty.

## Finding 1: Topology follows epistemics

The two scripts read in full are structurally opposite, and the difference tracks *how
certain Claude is about the steps in advance*.

**Implementation (`clear-otlp-tasks`) — linear phases, fan-out within each:**

```
phase('Infrastructure') → parallel([3 agents])
phase('Scripts')        → parallel([5 agents])
phase('E2E')            → single agent
phase('Docs')           → single agent
phase('Close')          → single agent
return { all phase results }
```

Strictly sequential phases; `parallel()` only *inside* a phase for independent edits. No
schemas, no verification. Prompts are large embedded constants — a shared `OTLP_ARCH`
spec is pasted into every subagent so they work from one source of truth. This is Claude
transcribing a known plan into parallel execution.

**Research (`deep-research`) — a dataflow graph with verification:**

```
Scope → pipeline(Search → URL-dedup → Fetch+Extract) → 3-vote Verify → Synthesize
```

Schema-validated at every hop, adversarial verification, defensive degradation
everywhere. This is Claude designing a *system* to manufacture a trustworthy answer from
unreliable inputs.

> **The rule:** "I know the steps, parallelize them" → linear phases. "Answers are
> uncertain and must be filtered" → pipeline + voting. Task *shape*, not task *size*,
> decides the harness.

## Finding 2: Pipeline over barrier when items are independent

`deep-research` runs Search → Fetch as a `pipeline`, not as two barriered `parallel`
stages, so a fast search angle's sources begin fetching while a slow angle is still
searching. The script comments this explicitly ("no barrier"). The one place it *does*
force a barrier — before Verify — carries a comment justifying it: *"claim pool must be
fully assembled before ranking/verification."*

Claude reserves global barriers for genuine cross-item dependencies and otherwise lets
each item flow through the stages independently, minimizing wall-clock.

## Finding 3: Schemas are the contract between agents

`deep-research` defines five JSON schemas (`SCOPE`, `SEARCH`, `EXTRACT`, `VERDICT`,
`REPORT`), each with `enum` constraints:

- `relevance: high | medium | low`
- `sourceQuality: primary | secondary | blog | forum | unreliable`
- `importance: central | supporting | tangential`

Because subagents are forced into these enums, the orchestrator ranks and sorts results
with plain deterministic code (`relRank`, `qualRank`, `impRank`) instead of asking a model
to "pick the best." The schema is what lets judgment (the agent) and logic (the script)
cleanly divide.

## Finding 4: Determinism in JS, judgment in agents

Everything that does **not** require a model stays in JavaScript:

- **URL normalization + dedup** — strips `www.`/trailing slash, dedupes via a `seen` Map,
  records `dupes` for transparency.
- **Fetch-budget accounting** — a `fetchSlots` counter (max 15) drops low-relevance
  sources once the budget is spent, logging them to `budgetDropped` rather than silently.
- **Ranking** — claims sorted by importance then source quality before verification.
- **Vote-counting** — refutation tallies computed in code.

Agents are invoked only for irreducibly model-shaped work: decompose the question, search,
extract claims, refute a claim, synthesize. *Don't spend a subagent on what a sort can do.*

## Finding 5: Adversarial verification with a skeptical default

Anything load-bearing is checked by an independent quorum. In `deep-research` each claim
faces **3 voters; ≥2 refutations kill it**, and the prompt instructs *"Default to
refuted=true if uncertain."* Claude does not trust a single agent's judgment on a fact that
will appear in the final report.

A subtle correctness guard sits in the tally: a claim survives only if
`valid.length >= REFUTATIONS_REQUIRED && refuted < REFUTATIONS_REQUIRED`. The comment
explains the trap it avoids — if every voter abstains (returns null), naïve counting gives
`refuted == 0` and the unverified claim would *falsely survive*. Abstentions are treated as
"unverified," not "passed."

## Finding 6: The real engineering is graceful degradation

The happy path is the small part. `deep-research` has **five distinct early-return
branches**, each preserving whatever work completed:

| Failure point | Behaviour |
|---|---|
| No question in `args` | Return structured error |
| Scope agent returns null | Return error, no wasted fan-out |
| Zero claims extracted | Return sources + stats anyway |
| All claims refuted | Return the refuted list "for transparency" |
| Synthesis skipped/failed | "Salvage the verified claims raw rather than throwing" |

Plus per-source handling: a fetch failure becomes `sourceQuality: "unreliable", claims: []`
(the pipeline keeps going); a user-skipped agent returns `null` and is filtered out — with
a comment noting it must *not* be thrown into `.catch()` or it would be mislabeled
"unreliable." Every node degrades to "return what we have."

> **Takeaway:** in a long, expensive, parallel job, robustness *is* the design. The
> sophistication is concentrated in never throwing away completed subagent work because a
> downstream step failed.

## Finding 7: Model and concurrency are deliberate knobs

- **Model tier is chosen per workflow.** Research/authoring runs use `opus-4-8[1m]` (the
  `[1m]` denotes a 1M-token context); implementation runs use `opus-4-6`. The default is
  inherited but overridable per agent.
- **Token spend scales with verification breadth, not phase count.** `deep-research` (5
  phases, 105 agents, 1.98M tokens) costs ~9× `clear-otlp-tasks` (5 phases, 11 agents,
  0.22M tokens). The cost driver is the 3-vote fan-out over 25 claims, not the plan depth.

## Implications for the auto-stack

**1. ETL captures the leaves but loses the trunk.** `auto-etl`'s discovery
(`auto-etl/internal/parser/parser.go:383-398`) is a recursive walk matching `*.jsonl` with
no path exclusions, so the **subagent transcripts get ingested** (tagged
`agentType: "workflow-subagent"`, parented to the session). But the **orchestration script
(`.js`) and run journal (`.json`) do not** — they are the wrong extension. We ingest 106
subagent transcripts from `deep-research` and lose the one file that explains how they fit
together.

**2. The journal is a ready-made signal source.** `wf_<runid>.json` already carries
`phases`, `agentCount`, `totalTokens`, `totalToolCalls`, `durationMs`, `defaultModel`,
`status`, and `workflowProgress` — structured task-decomposition telemetry that
`docs/signals.md` is reaching for, available *without* parsing any transcript.

**3. Volume skew risk.** A single workflow can multiply a session's "subagent" count by
100×. Heat-maps and per-session analytics in `auto-search`/`auto-reflect` should group or
filter `workflow-subagent` records, and ideally key them to their `wf_<runid>` so the
fan-out reads as one task rather than 105 sessions.

**4. Opportunity.** Treating the workflow script + journal as a first-class ETL artifact
would let `auto-reflect` mine *how Claude plans*: which topologies it picks, where it
inserts verification, how fan-out and token cost correlate with task shape — exactly the
"what's working / what's expensive" intelligence the stack is built to surface.

## Suggested follow-ups

- Extend `auto-etl` to ingest `workflows/wf_*.json` as a `workflow_run` record type, and
  optionally store the `.js` script as the run's plan-of-record.
- Add a `parent_workflow_id` (= `wf_<runid>`) to subagent session records so the fan-out
  is queryable as one unit.
- Build a "task decomposition" view in `auto-search`: phases, fan-out width, tokens/phase,
  and verification depth per run.

## Appendix: artifacts referenced

- `~/.claude/projects/-home-vscode-src-notes/dcd4c11d-…/workflows/scripts/deep-research-wf_ae475e1d-8f6.js`
- `~/.claude/projects/-home-vscode-src-agent-logs/611e2077-…/workflows/scripts/clear-otlp-tasks-wf_a90c9e55-c6c.js`
- Six run journals under `~/.claude/projects/*/*/workflows/wf_*.json` (metrics table above).
- ETL discovery: `auto-etl/internal/parser/parser.go:383-398` (`findJSONLFiles`), subagent
  detection at `parser.go:230-236, 278-313`.
</content>
</invoke>
