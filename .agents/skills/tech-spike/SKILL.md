---
name: tech-spike
description: "Run an exploratory tech spike to validate assumptions, investigate edge cases, and stress-test ideas before committing to implementation. Use when 'tech spike', 'spike this', 'explore whether', 'validate assumptions', 'investigate feasibility', 'proof of concept', 'is this possible', 'what are the risks', 'test this approach', or when the user describes an idea they want to de-risk before building. Also use when requirements seem uncertain and the user wants evidence before committing to a design."
---

# Tech Spike

Run a focused, time-boxed exploration to validate assumptions and uncover risks before committing to full implementation. The goal is fast learning, not production code.

## Process

### 1. Set up workspace

Before any exploration:

1. Check if `.tmp/` exists relative to the project root. If not, create it.
2. Check if `.tmp` is in `.gitignore`. If not, append it.
3. All spike work happens inside `.tmp/` — never pollute the main project tree with exploratory code.

### 2. Understand the spike scope

Read what the user has provided. They might give you:
- A feature idea or set of requirements to validate
- Explicit assumptions they want tested
- A technical question they need answered
- A design they're unsure about

If the user hasn't provided explicit assumptions to test, generate them. Think about:
- **Integration assumptions** — "Library X can do Y", "API Z supports this use case"
- **Performance assumptions** — "This approach scales to N items", "Latency stays under X ms"
- **Compatibility assumptions** — "This works across environments/versions"
- **Correctness assumptions** — "Edge case X is handled", "Data format Y is valid"
- **Feasibility assumptions** — "This can be done without modifying Z"

Focus on the **highest-risk, highest-impact** assumptions — the ones that, if wrong, would invalidate the approach entirely. Present 3-7 assumptions ranked by risk, and get user confirmation before proceeding.

### 3. Check tool availability

Before designing the spike process, identify what CLI tools or dependencies the validations will need (compilers, runtimes, test frameworks, network tools, database clients, etc.).

For each required tool:
1. Check if it's installed (`which <tool>` or equivalent)
2. If missing, tell the user what's needed and why, and ask them to approve installation

Do not proceed with spike execution until all required tools are confirmed available.

### 4. Design the spike plan

For each assumption, design a concrete validation approach:

```
## Spike Plan

### Assumption 1: [statement]
- **Risk if wrong:** [what breaks]
- **Validation approach:** [what to build/run]
- **Success criteria:** [how to know it passed]
- **Estimated effort:** [quick/moderate]

### Assumption 2: ...
```

Present this plan to the user for approval. They may want to adjust priorities, add assumptions, or skip low-value ones.

### 5. Execute spikes

Run the explorations inside `.tmp/`. Organize by assumption:

```
.tmp/
  spike-001-<name>/
  spike-002-<name>/
  ...
```

**Leverage sub-agents for parallelism.** Independent assumptions should be validated concurrently by separate sub-agents. Each sub-agent gets:
- The specific assumption to validate
- The validation approach and success criteria
- The working directory to use (`.tmp/spike-NNN-<name>/`)
- Permission to create files, run scripts, and use CLI tools

**Useful techniques for sub-agents:**
- Clone the current repo into `.tmp/` at a specific commit to test against a known state
- Write and execute throwaway scripts that exercise the assumption
- Use actual dependencies/libraries to test integration points
- Generate synthetic data to test scale assumptions
- Run benchmarks or load tests for performance assumptions
- Test across multiple configurations to find edge cases

Each sub-agent should capture:
- What it tried (commands, code, configurations)
- What it observed (outputs, errors, timings, behaviors)
- Whether the assumption held, failed, or was partially true
- Any surprises or secondary findings

### 6. Validate results

After spike execution, run validation criteria against the results:
- Did each assumption pass its success criteria?
- Were there unexpected findings beyond the original assumptions?
- Are there new risks discovered during exploration?
- What confidence level do we have in each finding (high/medium/low)?

For validations that can be automated (benchmarks, test suites, assertion checks), write and run scripts. For subjective assessments, note them clearly as requiring human judgment.

### 7. Write the report

Write `SPIKE-REPORT.md` in the project root with this structure:

```markdown
# Tech Spike Report: [Title]

**Date:** [today's date]
**Scope:** [one-line description of what was explored]
**Verdict:** [GO / NO-GO / CONDITIONAL — one sentence summary]

## Context

[What prompted this spike — the idea, requirements, or question being investigated]

## Assumptions Tested

| # | Assumption | Result | Confidence |
|---|-----------|--------|------------|
| 1 | [statement] | VALIDATED / INVALIDATED / PARTIAL | High/Med/Low |
| 2 | ... | ... | ... |

## Detailed Findings

### Assumption 1: [statement]

**Result:** VALIDATED / INVALIDATED / PARTIAL

**What we did:**
[Approach taken, tools used, code written]

**What we found:**
[Observations, data, outputs]

**Evidence:**
[Key outputs, benchmarks, screenshots, logs — be specific]

**Implications:**
[What this means for the design/implementation]

---

[Repeat for each assumption]

## Surprises & Secondary Findings

[Anything unexpected discovered during exploration that wasn't in the original assumptions]

## Risks Identified

[New risks surfaced by the spike, even if original assumptions held]

## Recommendations

[Based on findings — proceed as planned, modify approach, abandon, or spike further]

## Appendix: Reproduction

[How to re-run the spike. Commands, paths, and prerequisites needed to reproduce findings. Reference files in .tmp/ as needed.]
```

### 8. Present results

After writing the report, give the user a concise summary:
- Overall verdict (go/no-go/conditional)
- Which assumptions held and which didn't
- The most important surprise or risk found
- Your recommendation

Point them to `SPIKE-REPORT.md` for the full details.

## Guidelines

- **Speed over polish.** Spike code is throwaway. Don't write tests for spike code. Don't refactor it. It exists to produce evidence, not to ship.
- **Evidence over opinion.** Every finding should reference concrete output — a benchmark number, an error message, a behavior observed. "I think it works" is not evidence.
- **Fail fast.** If an assumption is clearly invalidated early, stop that spike and report the finding. Don't keep going just to finish the plan.
- **Capture the unexpected.** The most valuable spike findings are often things you weren't looking for. When something surprises you, document it.
- **Scope control.** A spike answers specific questions. If exploration uncovers a rabbit hole, note it as a finding rather than diving in — unless the user explicitly says to explore further.
