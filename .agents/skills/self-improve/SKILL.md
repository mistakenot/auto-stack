---
name: self-improve
description: 'Run the self-improvement loop on a focus area. Use when the user asks to improve, fix recurring problems, or run self-improve on a tool. Trigger when: "self-improve", "improve autosearch", "find and fix problems in". Do not trigger for doc reviews or one-off bug fixes.'
metadata:
    short-description: "Self-improvement loop via OpenProse"
---

## Workflow

1. Ask the user for a **focus** area if not provided (e.g. "autosearch", "autoetl", "auto-doc").
2. Invoke OpenProse: `/open-prose run` with the program at `self-improve/index.md` inside the repo `scripts` dir, passing `focus="<focus>"`.
3. OpenProse handles everything: preflight, exploration, analysis, review, consolidation, and implementation. Do not duplicate its phases manually.

## Constraints

- Always pass `focus` — it is required.
- `codebase-context` is optional; omit unless the user provides specific context.
- The program produces up to 3 PRs. Report their URLs and status when done.
- You MUST invoke the open-prose skill to run the self-improve program. If the Skill tool invocation for open-prose returns an error (tool not found, skill not loaded), report the error to the user and stop — do not attempt to execute manually. The open-prose VM provides contract enforcement and DAG orchestration that cannot be replicated by manual subagent execution.
