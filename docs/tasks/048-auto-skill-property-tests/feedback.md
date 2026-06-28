# Feedback: Task 048

## Problems faced
1. **`origin/main` did not compile on a clean checkout** — committed `auto-skill/internal/cli/sync.go` and `update.go` referenced `planMoves`/`planActive`/`staleLabel`, which were defined only in an *untracked* `cli/synctext.go`. Any worktree branched from `origin/main` (including this task branch) failed to build, which would have broken PR CI. Resolved by committing `synctext.go` to main first (landed as `d4fb538`), then rebasing the task branch onto it. Lesson: before creating an execution worktree, check that `origin/main` actually builds — untracked files in the primary checkout silently mask a broken committed tree.
2. **The properties found real bugs on first run** — Phase 1's walking-skeleton properties (T1 idempotence, R4 idempotence) failed immediately against two genuine order-of-operations defects. The walking-skeleton subagent correctly scoped its generators to stay green and *reported* the counterexamples rather than silently masking them, which let the coordinator surface the decision (fix vs. document) to the user.

## Reflections
- **What was tricky?** Distinguishing "property found a real bug" from "property is wrong." The discipline that paid off: a subagent that hits a failing property STOPS and reports the shrunk counterexample instead of narrowing the generator until it passes. Over-narrowing would have shipped green tests that prove nothing.
- **What would you tell yourself at the start?** Run `go build ./...` on the fresh worktree *before* dispatching any phase. The untracked-`synctext.go` blocker cost a detour that would have been a 10-second check up front.
- **What did you almost do but didn't?** Almost ran phases 2/3/4 in parallel (the DAG allows it — each touches a different file). Held to serial dispatch per the known hazard that concurrent subagents sharing one worktree can leak writes into the primary checkout. Phase 3 ran concurrently with an AskUserQuestion block (no other subagent active), which was safe and saved wall-clock.
- A third, narrower edge case surfaced (`CanonicalizeURL` strips only a single `.git`, so `repo.git.git` isn't a fixed point). Deliberately did **not** expand scope to fix it — it's not a real git URL form. Scoped the generator and flagged it as a PR follow-up.

## Useful context
- `context.md` was high-value: the export-status table (which render helpers are unexported) told subagents up front that render tests must use `package render`, and the regex table (`skillNameRE`, `commitHexRE`, `replacementVarRE`) let generators be constructive from the first attempt.
- **Construct-don't-reject** generators were the right call — no "gave up generating inputs" failures across 15 properties.
- **Canonical-form comparison** for the YAML round-trip (S2: marshal → parse → marshal, compare bytes) sidestepped `yaml.Node` metadata noise that a struct deep-equality assertion would have tripped on. The pre-resolved review threads (S2 normalized-equality, T4 file-URL empty host, S6 manifest round-trip) had already de-risked these exact traps before execution.
- `pgregory.net/rapid` auto-manages failure seeds under `testdata/rapid/` (gitignored here), and shrinks to minimal counterexamples — the kill tests produced tiny, legible failures (e.g. `https://0.com/0.git/`, `commit:0000000`).
- A background dogfood autowatch daemon committed *and pushed* `synctext.go` to main mid-session; rebasing the stack onto the updated `origin/main` was necessary.
