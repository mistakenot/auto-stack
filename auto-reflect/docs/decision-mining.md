---
hash: "292f7c20"
id: "decision-mining"
read_when: "designing or improving systems that extract user decisions from coding agent sessions and turn them into reusable rules"
summary: "Pipeline for extracting user decisions from coding agent sessions, calibrating scope, and converting patterns into reusable rules with feedback-driven lifecycles"
title: "Decision Mining from Agent Sessions"
---

# Decision Mining from Agent Sessions

Extracting high-signal user decisions from coding agent session transcripts, finding patterns, and turning them into generalizable rules for future agent behavior.

## The Pipeline

1. **Retrieve candidates** -- keyword-filtered user messages from session history (directive language like "don't", "prefer", "instead of", "we should"; correction signals like "no", "undo", "wrong")
2. **Enrich with context** -- pull 2-3 surrounding messages per candidate to capture what the agent did and why the user reacted
3. **LLM classification** -- extract structured decisions: what was decided, the reasoning, whether it's generalizable
4. **Cluster** -- group similar decisions to find recurring themes (e.g. "user repeatedly pushes back on new dependencies")
5. **Draft rules** -- convert clusters into situated guidance with scope boundaries

## Scope Calibration

The central challenge is finding the right abstraction level for rules:

- **Over-specific**: "don't use ast-grep for lint rules" when the real lesson was "don't add new dependencies when existing tools work." Fires rarely, misses the broader principle.
- **Over-general**: "prefer server-side over client-side" when the real lesson was "move security-sensitive operations server-side." Fires too often, gives bad advice for non-security UI logic.

Guard against both by grounding each candidate rule in **multiple independent examples**. One instance is an anecdote. Three instances with shared context is a pattern worth codifying.

## Knowledge Engineering Concepts

Decision mining is a form of **knowledge engineering** -- extracting expert knowledge and encoding it into systems. Core concepts that apply:

**Tacit vs. explicit knowledge.** The most valuable decisions are tacit -- the user reacted to a situation without articulating a general rule. "i dont want a new dependency" isn't a stated policy, it's a judgment call. The mining task is converting tacit reactions into explicit, reusable guidance. Risk: tacit knowledge is often context-dependent in ways the expert doesn't realize.

**Chunking.** Experts compress many low-level rules into a single intuition. The agent needs the decompressed checklist. Good extraction decompresses expert chunks into component reasoning without losing coherence.

**Boundary conditions.** The most useful part of any rule isn't the rule itself -- it's knowing when it stops applying. "Prefer server-side" is only useful paired with "except for purely presentational logic where latency matters." Spend more time on boundaries than cores.

**Case-based vs. rule-based reasoning.** The field moved away from brittle rule systems toward case-based reasoning in the 90s -- store specific situated cases and let the reasoner find the closest match at decision time. Situated memories with `Why:` and `How to apply:` are closer to case-based reasoning than decontextualized rules. This is more robust for nuanced, context-dependent decisions.

## Closing the Feedback Loop

Rules need a lifecycle with feedback to avoid accumulating stale or harmful guidance.

**Passive signals from sessions:**
- Agent behavior corrected despite a rule existing = rule isn't working (too vague, not visible, or agent can't recognize when it applies)
- Corrections on a topic that stop after a rule was added = evidence the rule helps
- Sessions where a rule was in context with no correction = weak positive signal

**Rule lifecycle:**
- **Draft** -- extracted from pattern, deployed as memory
- **Probation** -- monitored for first N relevant sessions. Did it fire? Did the user accept the outcome?
- **Confirmed** -- multiple sessions show it helping, no corrections caused by it
- **Stale/retired** -- codebase evolved, rule no longer applies, or it causes more corrections than it prevents

**Per-rule tracking:** when created, source sessions, how many relevant sessions since, correction rate on the topic before vs. after introduction.

## Ideas from CASS (cass-memory)

CASS is a CLI tool that creates persistent cross-agent learning from coding sessions. Relevant design ideas:

**Three-layer memory model:** episodic (raw logs) -> working (structured summaries) -> procedural (distilled rules). Same compression hierarchy as the pipeline above.

**Confidence decay with 90-day half-life.** Rules that aren't revalidated lose weight automatically. Shifts the default from "rules persist until pruned" to "rules expire unless reinforced."

**Asymmetric scoring (4x harmful vs helpful).** One correction costs 4x a confirmation. Encodes the reality that a bad rule actively damages trust while a good rule quietly helps.

**Maturity states (candidate -> established -> proven).** New rules start on probation and need multiple validations to graduate. Don't promote single-instance observations to permanent rules.

**Deterministic curation (no LLM in the curator).** Use LLMs for extraction (turning sessions into candidate decisions), but keep promotion/retirement logic deterministic. Prevents context collapse from feedback loops where an LLM generates rules, interprets its own rules, then generates more rules from its own interpretations.

**Anti-pattern inversion.** Rules that accumulate excessive negative feedback automatically convert to warnings ("PITFALL: don't X").

## Tooling

`autosearch` provides the retrieval layer -- BM25 full-text search over session transcripts with role filtering, session context drill-down, and stats aggregation. Useful queries for decision mining:

```bash
# User corrections
autosearch search '"no," OR "wrong" OR "undo"' --role user --since 30d

# User preferences and directives
autosearch search '"prefer" OR "instead of" OR "rather than"' --role user --since 30d

# Architecture decisions
autosearch search '"we should" OR "approach" OR "I want"' --role user --since 30d
```

Enrich hits by reading neighboring messages via `autosearch message get <sessionId>-<index>`.
