---
hash: "f53f4150"
id: "cd9c4fd2"
read_when: "reviewing lessons from the alias re-exports implementation or debugging autograph e2e test output capture"
summary: "Post-implementation feedback for task 005: two problems found (e2e stdout/stderr mixing, zero-length wildcard capture) and reflections on the clean resolver interface and the value of e2e binary invocation testing."
title: "Feedback: Task 005 — Code Graph Alias Re-exports"
---

# Feedback: Task 005

## Problems faced
1. e2e `runAutograph` used `CombinedOutput()` -- new stderr diagnostics mixed into JSON stdout causing parse failures in `TestEdgeReferentialIntegrity`. Fixed by splitting to separate stdout/stderr capture.
2. Zero-length wildcard capture (`>=` vs `>`) -- reviewer caught that `@/` (empty capture) would match the `@/*` alias and produce a misleading unresolved-alias warning instead of falling through to bare/external. One-character fix, good catch.

## Reflections
- The resolver refactor was straightforward because the existing code already had clean separation between classify/resolve/probe stages. Adding `MatchedAlias` metadata flowed naturally through the existing interface.
- The e2e stdout/stderr issue was not caught by unit or integration tests because those use `cmd.SetOut`/`cmd.SetErr` buffers (correct separation). Only the e2e binary invocation via `exec.Command` exposed the problem. Lesson: when adding stderr output to a CLI, always check how e2e tests capture output.
- Considered adding warning fields to the JSON graph schema but correctly rejected it -- diagnostics are process-level concerns, not graph data. The stderr approach preserved the existing JSON contract.

## Useful context
- Task 001 feedback (`docs/tasks/001-ts-import-graph/feedback.md`) documented ast-grep pitfalls (dual ts/tsx modes, `$$$` for multi-name patterns) that remained relevant.
- The `pathMapping` struct's comment said "prefix/suffix" but the code only stored prefix -- the comment was aspirational, not descriptive. Context doc caught this discrepancy.
