# Feedback: Task 019

## Problems faced
1. **Full `make check` lint was never run until Phase 4** — the per-phase verify gates only ran
   `gofmt`/`go vet`/`go test`, which let 27 golangci-lint findings (all `rangeValCopy`, `hugeParam`,
   `intrange`, `modernize`, test `errchkjson`/`ineffassign`) accumulate across Phases 1–3 committed
   code. Phase 4 had to fix them all before the gate would pass. This is the *exact* 017/018 lesson
   already recorded in context.md ("real findings surface only via full `make check`") — it recurred
   anyway because the plan's per-step verify commands didn't include `make check`. Fix for next time:
   put a module-level `golangci-lint run` (or `make check`) in every phase's verify step, not just
   the final one.
2. **Plan referenced a nonexistent `init --project` flag** — the auto-reflect CLI is a single
   `auto reflect init` with no `--project`. The Phase 4 subagent correctly adapted to the real
   surface, but the plan/quickstart prose was written against an assumed flag. Verify CLI surfaces
   with `--help` *before* writing them into a plan (Task 015 lesson, also recurred).
3. **Gate exit code collapses under the unified binary** — `auto reflect gate check` sets
   `ExitError{Code:2}`, but the task-017 unified `auto` dispatcher normalizes non-zero codes to 1.
   Still non-zero so AC-3b/AC-7 hold, but a consumer keying on exit code 2 specifically would be
   surprised. The e2e test sidesteps this by asserting `err != nil` against the directly-built
   `autoreflect` binary.

## Reflections
- **What was tricky?** The fold's conflict semantics vs. the one-event-per-edit invariant — a
  multi-field edit *must* be a single `rule_edited` event with a `deltas` array, otherwise sibling
  field changes look identical to a real concurrent-edit conflict. This was caught and resolved at
  the solution-review stage (RESOLVED P2 in Step 2.4); having it pinned in the docs before execution
  made Phase 2 unambiguous. The sharding key (host+day+**worktree**) had the same story — the design
  review caught that host+day alone collides on git-merge for same-host parallel worktrees.
- **What would you tell yourself at the start?** Run `make check` after *every* phase, not just the
  last — the cheap per-step gates give false confidence. And the serial-dispatch + verify-main-clean
  discipline worked perfectly this time: zero leaks into the main worktree across 4 phases, unlike
  017/018.
- **What did you almost do but didn't?** Almost added a `flock` to the snapshot `Load` path to fix
  the concurrent-refold race flagged in review — but the fold is deterministic and the write is
  atomic-via-rename, so duplicate folds produce identical bytes (benign). The real bug was a failed
  cache write blocking reads; fixed with a best-effort write instead. Smaller, correct change.

## Useful context
- **The solution-stage review (before /new-plan) paid for itself.** Six P1–P3 design issues
  (sharding key, seq allocation, snapshot staleness, gate scope, fold conflicts, NaN selection_rate)
  were all caught and resolved in solution.md/requirements.md *before* any code was written, so the
  phase breakdown was stable and no phase had to be re-architected mid-flight.
- **The checked-in golden fixture** (`internal/rules/testdata/events/` → `playbook.golden.json`)
  doubles as the append-only schema-stability guard: any future code that folds the same events to
  different output fails the test. Worth keeping curated as the event schema evolves.
- **context.md's "Reused as-is" vs "new primitive" distinction** was load-bearing: `store.AppendJSONLine`
  is O_APPEND-only and can't allocate a monotonic seq, so `events.AppendEvent` had to be a new
  read-modify-write primitive under the same flock. The doc flagging this up front avoided a wrong
  reuse in Phase 1.
</content>
</invoke>
