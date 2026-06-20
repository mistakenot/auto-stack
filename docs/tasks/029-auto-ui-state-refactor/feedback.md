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

## Conformance testing — what I did

This was a **behaviour-preserving refactor of a no-build SPA**, so the verification spine
was *regression*, run in the browser, because the Go toolchain is structurally blind to
this layer (013 lesson). The strategy had three layers and a handful of reusable tactics.

### Strategy: three layers, regression-first
1. **Existing 025/026 harnesses re-run UNEDITED** as the behaviour-preserving oracle. If a
   harness needed editing to pass, the refactor had changed behaviour — a fail. Both ran on
   the `embed` **and** `-tags dev` builds. (025 static explorer; 026 liveness + reveal +
   `reveal-harness.sh`.)
2. **A new `state-harness.sh`** that only asserts what the refactor *adds* (AC-3..AC-8) —
   single store source, deleted mirror, single subscription, no new dep, warm cache,
   cross-route survival.
3. **`go test ./...`** as necessary-but-not-sufficient (backend untouched; `rpc_ingest_test.go`
   still pins `params.data.path`).

### Tactics built into `state-harness.sh`
- **Assert machine-readable state, never rendered text.** Every assertion reads `data-*`
  attributes (`data-doc-count`, `data-revision`, `data-doc-path`/`-type`, `data-conn-status`)
  or `window.__autoui` counters via headless `agent-browser eval` — because a re-fetch can
  leave the DOM textually identical while the underlying state changed.
- **Separate "received" from "rendered" observables.** Used
  `window.__autoui.counters.get('doc.changed')` (a `Map` — `.get()`, not bracket index) only
  to *gate* the ~3s async-propagation poll; it fires once per event at the rpc dispatch layer,
  so it cannot prove subscriber count. The actual outcome is always asserted on the DOM.
- **Grep-as-evidence for structural invariants.** Single-subscription proof is
  `grep -rl 'on("doc.changed"' web/static` matching only `store.js`; "presentational views"
  proof is zero `call(`/data-fetching `useState` in explorer/tree/content; the only `call(`
  sites live in the store. No runtime probe can show these, so the grep *is* the AC.
- **Importmap-diff as the dependency gate.** AC-6 = `git diff --quiet -- index.html` — a
  mechanical proof that no new esm.sh entry / vendored package crept in.
- **A store-level `doc.list` call counter** (`window.__autoui.store.docListCalls`) as the
  *only* valid observable for "warm cache, no redundant fetch" — explicitly **not**
  `/api/debug/recent` (that's the server ingest buffer, not client WS calls) and not
  `window.__autoui` (received notifications, not outbound calls).
- **Delta, not absolute, with bounded polling.** `data-revision` isn't deterministic across
  opens, so assertions record-before / compare-after, and poll up to ~3s after each
  `auto ui emit` (events propagate async over WS).
- **Dual-build, every assertion.** Run on `embed` (baked-in assets) and `-tags dev`
  (disk-served) because asset-delivery mode is itself a defect surface Go can't see.
- **Hermetic fixtures.** Lowercase-kebab project ids (025 registry rule), temp registry via
  `auto ui serve --projects <fixture> --port 0 --ready-file`, two projects with *different*
  doc counts (proj-a=4, proj-b=1) so a switch is observable; never touches `~/.auto`.

### The key meta-lesson (and the harness gap it exposed)
A harness can be **green without proving the thing it names**. AC-8 originally polled
`data-doc-count` to *settle* but only `check`-asserted the store-driven call counter — which
is identical whether or not the component re-renders. So it passed at 20/20 while the tree
could render a stale project's docs (the P1 review bug). The fix was to add real assertions
that the *rendered* `data-doc-count` equals the switched project's count (1 after A→B, 4
after B→A). **A poll-to-settle is not an assertion.** After hardening, the harness is 22/22
on both builds, and the new checks would now fail loudly if the stale-selector regression
returned. This same harness is what caught the AC-4 path regression during Phase 3 — concrete
evidence the browser oracle earns its keep over a green Go suite.

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
