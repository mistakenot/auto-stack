# Solution Tab Guidelines

The Solution tab describes the chosen approach and file changes. It is the third tab in `plan.html`, created by `new-solution` (stage 3).

## Sections (in order)

1. **Approach** (id: `approach`) — High-level description of the solution: what changes and why. Keep it high-level — implementation details belong in the Plan tab.
2. **File Changes** — Use `pd-files` with one entry per affected file. Mark each as `add`, `edit`, or `delete` with a brief description. File paths are relative to repo root.
3. **Rejected Alternatives** (id: `rejected-alternatives`) — Bullet list of other options considered and why they were rejected. Include even for simple tasks — they document the decision space.
4. **Decisions** — Place a `pd-decisions` element at the end. It auto-generates a decision log from all resolved threads in the doc.

## Rules

- The Solution tab is the **end state** — it describes what the codebase will look like, not the steps to get there.
- Include rejected alternatives even for simple tasks — they document the decision space.
