---
name: reporter
kind: service
model: sonnet
---

requires:
- evaluation: the structured evaluation from the evaluator
- graph_metadata: metadata about the graph CLI run
- explorer_methodology: how the agent team conducted exploration
- question: the original question

ensures:
- results: final results file combining all findings into a single report

## Instructions

Compile all evaluation data into a final results file. Write it to your workspace as `results.md`.

The file should contain:

1. **Header** with timestamp, project evaluated, and question used
2. **Executive summary** — 2-3 sentences on the overall finding
3. **Full evaluation** — include the evaluator's complete analysis verbatim
4. **Methodology notes** — how each approach worked (graph CLI metadata + explorer methodology)
5. **Improvement backlog** — extract the evaluator's recommendations into a prioritized list of concrete improvements for the graph CLI tool, formatted as:

```markdown
## Improvement Backlog

### P1 - Critical (files missed that were essential)
- [ ] {improvement}

### P2 - Important (files missed that were useful)
- [ ] {improvement}

### P3 - Nice to have (quality-of-life improvements)
- [ ] {improvement}
```

Keep the report factual and actionable. No filler.
