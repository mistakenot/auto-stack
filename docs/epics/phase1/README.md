---
hash: "c2c7260b"
id: "06ea4dba"
read_when: "coordinating or reviewing the reflect-playbook Phase 1 implementation across multiple workers"
summary: "Coordinator README for the reflect-playbook Phase 1 integration branch: sub-task assignments, round scheduling, worker rules, and merge checklist for tasks 1.2–1.6."
title: "Phase 1 Implementation — Orchestration"
---

# Phase 1 implementation — orchestration

Implements Phase 1 (1.2–1.6) of `docs/epics/001-reflect-playbook-loop.md` on a single
integration branch (`feat/reflect-playbook-phase1`), delegated across two workers in two
rounds, integrated and PR'd by the coordinator.

## Locked decisions (coordinator, 2026-06-10)

1. **Scope:** all of Phase 1 (1.2–1.6) in one branch / one PR.
2. **Observation command naming:** `observation add` / `observation list` (noun+verb,
   matching `rule create/list/get`).
3. **Draft rules in `retrieve`:** surface **flagged** by default; `--no-drafts` hides them.
   `stale` rules never surface.

## Sub-tasks & rounds

| # | Sub-task | Plan | Round | Worker |
|---|----------|------|-------|--------|
| 1.2 | Lifecycle-aware retrieval | `1.2-lifecycle-retrieval.md` | 1 | A (pane 1) |
| 1.3 | Observation capture | `1.3-observation-capture.md` | 1 | B (pane 3) |
| 1.4 | Consolidation → rules | `1.4-consolidation.md` | 2 | A (pane 1) |
| 1.5 | Reader API over event log | `1.5-reader-api.md` | 2 | B (pane 3) |
| 1.6 | Doc sync + quickstart regen | `1.6-doc-sync.md` | 3 | coordinator |

Round 1 tasks are independent. Round 2 tasks both depend on 1.3 (observations must exist),
so they run only after Round 1 is integrated. 1.6 and the actual release tag are the
coordinator's; the release tag is a user-triggered/billed action and is left for the user.

## Rules for workers

- Work **only** inside your assigned worktree. Do not `git checkout`, `git merge`, `git
  rebase`, push, or open PRs — the coordinator owns all branch/worktree/merge operations.
- Commit your work to the branch already checked out in your worktree, using
  `contextual-commit` style messages.
- Build after every Go file: `go build ./...` in `auto-reflect/`. Run `go vet ./...` and the
  package tests before you report done.
- Stay within the file list in your plan. If you must touch a shared file (`root.go`,
  `quickstart.go`), follow the "Integration notes" in your plan exactly so the coordinator's
  merge is clean.
- Output JSON by default; diagnostics/errors to stderr; every hard error carries a
  remediation hint. Reuse `ExitError`, `normalizeFormat`, `writeJSON`, `writeValidationErrors`.
- Report back: what you changed, test results (paste the `go test` summary), and anything
  ambiguous you had to decide.

## Coordinator integration checklist (per round)

1. `go build ./... && go vet ./... && go test ./...` in each worker's worktree.
2. Merge worker branches into `feat/reflect-playbook-phase1`; resolve `root.go` /
   `quickstart.go` registration conflicts.
3. Re-run full module build + vet + test on the integration branch.
4. Round 2 only after Round 1 is merged; refresh worker worktrees from the integration
   branch before re-delegating.
