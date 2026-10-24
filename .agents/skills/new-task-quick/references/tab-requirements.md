# Requirements Tab Guidelines

The Requirements tab captures the problem, goals, and scope. It is the first tab in `plan.html`, created by `new-task`.

## Sections (in order)

1. **Problem** (id: `problem`) — 1-2 sentences describing what's wrong or what's needed.
2. **Goals** (id: `goals`) — Bullet list of what this task achieves.
3. **Out of Scope** (id: `out-of-scope`) — Product-level boundaries (what this task explicitly does NOT do).
4. **Open Questions** (id: `open-questions`) — Two kinds of unknown, two vehicles:
   - **A genuine decision you cannot resolve** from the requirements, the code, or a
     sensible default — one whose answer would change the design — is a `<pd-question>`,
     NOT a prose bullet. It gates the doc (the linter reports `open-question` with a
     non-zero exit, the status bar shows "blocked") so an automated/executor step can't
     run past an unmade decision. Always include a `recommendedAnswer` — your best lean —
     so the human can rubber-stamp or override instead of starting cold.
   - **An assumption you are comfortable proceeding on** stays a prose checkbox bullet
     (record it so it can be revisited; mark answered ones with the answer). It does NOT
     gate.
   Rule of thumb: if proceeding on your own guess would be reasonable, it's a prose
   assumption; if a wrong guess would waste real work or need redoing, it's a `<pd-question>`.

## Rules

- No acceptance criteria here — those belong in the Verification tab.
- Every `<pd-question>` must be answered (a `<pd-answer>` + `status="answered"`) before
  proceeding to the next stage; prose assumptions may carry forward.
- Keep it to ~1 page. Split larger work into multiple tasks.
