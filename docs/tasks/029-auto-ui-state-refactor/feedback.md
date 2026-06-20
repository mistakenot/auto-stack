# Feedback: Task 029 — auto-ui-state-refactor

## Problems faced

1. **The browser, not Go, is the only oracle for this layer.** `go build`/`go test`
   stayed green through every defect — the AC-4 path regression, the P1 stale-selector
   bug, and the P2 worktree cache bug were all invisible to the Go toolchain (013's lesson
   in practice). The `agent-browser` conformance harnesses are the real gate; trust them
   over the test suite for anything under `web/static`.

2. **A passing harness is not a proving harness.** AC-8 originally *settle-polled*
   `data-doc-count` after a project switch but only `check`-asserted the store-level call
   counter — which is driven by the store regardless of whether the component re-renders.
   So the harness reported 20/20 while the tree could have shown stale docs on switch (the
   P1 bug Codex caught). Lesson: a poll-to-settle is not an assertion; assert the rendered
   value, not just the side effect that should have produced it.

3. **`useStore(inlineSelector)` is a footgun when the selector closes over a prop.**
   `useStore`'s `useEffect(subscribe, [])` captures the selector once. A module-level
   selector (`selectConn`, `selectProjects`) is fine, but `s => selectDocs(s, project)`
   captures the first-render `project`; a reused `DocTree` instance then keeps selecting the
   old project's slice. `DocTree` was the *only* site with this shape — the fix was to key
   it by `project+worktree` so a switch remounts it and re-captures the selector.

4. **Param-less routes silently clear mirrored state.** The first cut had `syncFromHash()`
   re-derive `selection` on every route, so navigating to the param-less `#/debug` wiped
   `openDoc.path` and `/debug` showed `—`. `#/debug` is a read-only view; only the explore
   route should drive selection. The pre-refactor `uistate.js` got this for free (it was a
   write-only mirror untouched by navigation).

## Reflections

- **What was tricky?** Distinguishing a real bug from a harness artifact. Both Codex
  threads *looked* plausible and both turned out real, but confirming them meant tracing
  exactly how `useStore`'s effect captures selectors and how `syncFromHash` reads
  `worktree` from the hash — not taking the review at face value, and not dismissing it
  because "the harness passed."
- **What would you tell yourself at the start?** When you delete a write-only mirror
  (`uistate.js`) whose whole job was *surviving navigation untouched*, enumerate every
  property that survived for free and re-prove each one survives under the new model. Path
  survival (AC-4) was the one that didn't.
- **What did you almost do but didn't?** Almost fixed P1 by making `useStore` re-evaluate
  on every render (changing a hook used at 9 sites). Kept it surgical instead — keyed the
  single offending component, the reviewer-endorsed option, with far smaller blast radius.
  Also almost added an HTML comment inside an `htm` template literal to explain the key —
  pulled it out to a JS comment since htm comment handling is a runtime risk Go can't see.

## Useful context

- **025/026 conformance harnesses are the regression spine.** Re-running them *unedited*
  on both `embed` and `-tags dev` builds is what made a behaviour-preserving refactor
  provable. The 029 `state-harness.sh` only needs to assert what the refactor *adds*.
- **The module-singleton store (D-5) made AC-4 unconditional** once `#/debug` stopped
  clearing selection: module-scope state survives any unmount, so cross-route survival no
  longer rides on Preact reconcile.
- **`docsKey(project, worktree)` collapses to the bare project id when no worktree is set**,
  so the worktree-aware cache fix is byte-identical for today's single-worktree flows —
  the safe shape for a latent-but-reachable bug.
- **Deferred:** a *browser* worktree-switch test needs a multi-worktree registry fixture
  (`resolveRoot` only accepts a worktree matching a registered project path). The planned
  in-browser unit-test task is the right home for direct `docsKey`/`selectDocs` unit tests.
