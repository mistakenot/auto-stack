---
hash: "9c8d32de"
id: "292254ff"
read_when: "merging to main while background jobs or pre-commit hooks may be committing concurrently, or debugging a non-fast-forward / diverged main"
summary: "A routine doc commit hit a three-way divergence of main (remote merge + unpushed local autodoc commit + a fresh feature branch), caused by treating ff-merge as the default without checking for divergence; resolved by linearising in a clean worktree and pushing as a fast-forward."
title: "Postmortem: main-branch divergence during doc commit + merge"
---

# Postmortem: main-branch divergence during a doc commit + merge

- **Date:** 2026-06-28
- **Author:** Claude (background session)
- **Severity:** Low — no data lost, no published artifact corrupted. Cost was operator time and several failed git operations.
- **Status:** Resolved. `origin/main` is linear and contains all three lines of work.

## Summary

A simple request — write a doc, then convert it to HTML, both on `main` — turned
into a multi-step git recovery because `main` moved **twice** underneath the work
from two independent sources while a third branch (the doc work) was in flight. The
first `git merge --ff-only` failed; a naive retry would have either created a messy
merge commit or hit a modify/delete conflict, because one of the competing commits
edited the very file the doc work deleted.

The incident was resolved without losing any commit and without disturbing a
concurrent background job that was live in the primary checkout.

## Impact

- No commits lost; no corruption of `origin/main`.
- One `--ff-only` merge failed and one `git push` was rejected before the cause was understood.
- A short window where the doc work existed only on a local worktree branch.
- No user-facing or runtime impact (docs-only changes).

## Timeline (all on 2026-06-28)

1. **Doc written.** A markdown doc (`docs/auto-ui-hook-subscription.md`) was committed
   and pushed → landed on `origin/main` as `cbf9098`. This part was clean.
2. **Unpushed local commit appears.** An autodoc gate commit `93ad35c`
   ("chore(autodoc): clear all doc drift and make pre-commit block on it") was made on
   the **local** `main` of the primary checkout. It added `title/summary/read_when`
   frontmatter to several docs — **including the doc just committed** — and changed the
   `Makefile` so pre-commit blocks on doc drift. It was **never pushed**.
3. **Remote advances independently.** PR #109 (`09598ae`, auto-skill property tests)
   merged into **`origin/main`**, on top of `cbf9098`.
4. **Third line forks.** The HTML-conversion task started in a fresh worktree branched
   from `origin/main` (`09598ae`) — which did **not** contain the local autodoc commit.
   The conversion **deleted** `auto-ui-hook-subscription.md` and added the `.html`
   (`763493f`).
5. **Merge fails.** `git merge --ff-only` of the conversion branch into the primary
   `main` failed: `fatal: Not possible to fast-forward`. A direct `git push` was also
   rejected (non-fast-forward).
6. **Diagnosis.** Inspection showed three heads off the common base `cbf9098`:
   - `origin/main` = `…cbf9098 → 09598ae` (#109)
   - local `main`  = `…cbf9098 → 93ad35c` (autodoc, unpushed)
   - work branch   = `…09598ae → 763493f` (HTML conversion)
   …and that `93ad35c` edited the same `.md` the conversion deleted — so they could not
   be merged blindly.
7. **A concurrent job is detected.** Files under `auto-skill/internal/cache/`
   (`extract_test.go`, `zz_repro_test.go`) and `.auto/skills/manifest.json` were
   churning in the primary working tree, indicating another background job was actively
   working there. Decision: do **not** rebase/reset the primary checkout.
8. **Resolution.** In a clean worktree, a linear history was rebuilt off `origin/main`:
   `09598ae` → cherry-pick `93ad35c` (kept whole, so its patch-id still matches the
   primary's copy) → cherry-pick the conversion, resolving the expected modify/delete on
   the `.md` by keeping the deletion. Pushed as a fast-forward.
9. **Verified.** `origin/main` linear: `be54dfa` (HTML) → `285970d` (autodoc) →
   `09598ae` (#109). The `.html` is present, the `.md` is gone, the autodoc `Makefile`
   gate and other-doc frontmatter are preserved. Primary checkout left untouched.

## Root cause

**`main` was not a single moving target — it was three.** The work assumed
`merge --ff-only` would be a no-op formality, but between starting and finishing:

1. A **remote merge** advanced `origin/main` (#109), and
2. An **unpushed local commit** (`93ad35c`) advanced the primary's local `main`
   independently (produced by the autodoc pre-commit machinery, not the doc author).

Forking the conversion branch from `origin/main` while the local `main` carried a
different unpushed commit guaranteed divergence. The contributing factor that made it
*sharp* rather than trivial: that unpushed commit **edited the exact file the
conversion deleted**, turning a would-be auto-merge into a modify/delete conflict.

### Why this is a recurring shape

This repo runs automation that commits to `main` (autodoc drift-fixing pre-commit
hooks; dogfooding/background jobs). "`main` moves under you" is therefore a normal
condition, not an anomaly — see existing memory notes on dogfood daemons and parallel
executors moving `main`. Treating ff-merge as the default instead of *checking for
divergence first* is the process gap.

## What went well

- **No data loss.** Every commit was preserved; recovery was non-destructive.
- **Concurrent job protected.** The live job in the primary checkout was never
  disturbed — recovery happened entirely in an isolated worktree, and `origin/main`
  was advanced by push, not by resetting the shared checkout.
- **Patch-id preserved.** Cherry-picking the autodoc commit *whole* (rather than
  dropping its now-moot `.md` hunk) means the primary's original `93ad35c` will rebase
  away cleanly on its next `git pull --rebase`, instead of becoming a near-duplicate
  that conflicts.

## What went wrong

- **ff-merge by default.** The merge was attempted without first checking whether
  `origin/main` or local `main` had advanced — the failure was discovered, not
  predicted.
- **Stale local `main` not noticed early.** The unpushed `93ad35c` on the primary
  checkout was invisible until the merge failed.
- **One avoidable detour.** An attempt to `git stash` + `pull --rebase` the primary
  checkout was started before realising a concurrent job was live there; it was backed
  out cleanly, but it should not have been started.

## Action items

| # | Action | Type |
|---|--------|------|
| 1 | Before any merge to `main`: `git fetch` and check `git merge-base --is-ancestor origin/main <branch>` (and the local `main`) to detect divergence *before* acting, not after a failed ff. | Process |
| 2 | When landing a commit on `main` as a background job, prefer **pushing a fast-forward to `origin/main` from an isolated worktree** over mutating the primary checkout — the primary may host a live job. | Process |
| 3 | Treat churning files in the primary working tree (e.g. `*_test.go`, `manifest.json`) as a signal that a concurrent job is active; never `stash`/`reset`/`rebase` the shared checkout in that state. | Process |
| 4 | When reconciling someone else's commit into a new line, cherry-pick it **whole** to preserve its patch-id, so the original copy rebases away cleanly later. | Technique |
| 5 | Capture this as a reusable memory: "ff-merge is not a formality; verify divergence first; recover in a worktree, push ff." | Knowledge |

## Lessons

- A fast-forward merge is an **assertion about history**, not a formality — verify it
  holds before relying on it.
- In a repo where automation commits to `main`, the safe model is: do your work in an
  isolated worktree forked from `origin/main`, and land it by **fast-forward push to
  the remote**, leaving local checkouts to reconcile themselves via `pull --rebase`.
- Preserving patch-ids during recovery is what keeps the *other* person's later rebase
  clean — recovery is not just about your own branch.
