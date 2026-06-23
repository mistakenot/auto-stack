# Plan Tab Guidelines

The Plan tab is the agent-consumed execution recipe. It is the fourth tab in `plan.html`, created by `new-plan`.

## Sections (in order)

1. **Summary** (id: `summary`) — One sentence describing the implementation approach.
2. **Execution Sequence** — A mermaid diagram showing the phase dependency DAG for parallel dispatch.
3. **Phases** — Use `pd-stepper` with one `pd-phase` per execution phase. Each phase has: `n` (number), `title`, `files` (comma-separated paths matching file change entries from the Solution tab), `status="todo"`. Phase body lists concrete steps. Each phase is atomic — one subagent handles it end-to-end.
4. **Success Criteria** (id: `success-criteria`) — Maps back to AC ids (e.g. "AC-1 through AC-3 pass").

## Rules

- Every phase ends with a commit step. Every phase includes verification (typecheck, tests, lint).
- `files` must reference paths already declared in the Solution tab's file changes.

## AC Traceability Backfill

After writing the Plan tab, go back to the Verification tab and update each `pd-ac` card:
- Set `phases` to the comma-separated phase numbers that cover it (e.g. `phases="1,2"`).
- Set `tests` to the comma-separated test file paths from the solution (e.g. `tests="src/feature.test.ts"`).

This links acceptance criteria to the phases that implement them and the tests that verify them.
