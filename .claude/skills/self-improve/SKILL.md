---
name: self-improve
description: 'Run the self-improvement loop on a focus area. Use when the user asks to improve, fix recurring problems, or run self-improve on a tool. Trigger when: "self-improve", "improve auto search", "find and fix problems in". Do not trigger for doc reviews or one-off bug fixes.'
metadata:
    short-description: "Self-improvement loop via OpenProse"
---

## Workflow

1. Ask the user for a **focus** area if not provided (e.g. "auto search", "auto etl", "auto-doc").
2. Invoke OpenProse: `/open-prose run` with the program at `self-improve/index.md` inside the repo `scripts` dir, passing `focus="<focus>"`.
3. OpenProse handles everything: preflight, exploration, analysis, review, consolidation, and implementation. Do not duplicate its phases manually.

## Constraints

- Always pass `focus` — it is required.
- `codebase-context` is optional; omit unless the user provides specific context.
- The program produces up to 3 PRs. Report their URLs and status when done.
- If OpenProse is not available, read the program index and its service files from the `scripts` directory and execute the phases manually as subagents.
