# Feedback: Task 025 — Planning Dashboard Explorer

## Problems faced
1. **Project-id case constraint surfaced only at runtime.** The backend `FindProjectByID`
   (`auto-shared/config/projects.go`) lowercases the lookup and ids must match
   `^[a-z0-9]+(?:-[a-z0-9]+)*$`. The skeleton conformance fixture used `projA`/`projB`, so
   `project.list` rendered the switcher fine but `doc.list` then failed "project not found in
   registry" and the tree never loaded. The explorer code was correct — it passes the hash id
   through faithfully — so this was a fixture/schema constraint, not a UI bug. Fix: rewrite the
   conformance fixture to lowercase ids (`proj-a`/`proj-b`). Worth knowing before writing any
   auto-ui fixture: registry ids must be lowercase-kebab.
2. **Explorer-as-landing races the WebSocket open.** `rpc.js`'s `call()` rejects synchronously
   when the socket isn't `OPEN` (no queue), and `connect()` only *starts* the handshake at module
   load. The demo `Home` landing made no mount RPC, so this was masked; making the explorer the
   default surfaced it — every cold-load `project.list`/`doc.list`/`doc.get` would deterministically
   reject "not connected" until a manual reload. This was caught at *plan review* (a RESOLVED P2
   thread in solution.md), so the fix was designed in up front: a `whenOpen()` readiness promise
   every mount-time fetch awaits, plus per-component reconnect self-heal on a fresh `onStatus("open")`,
   guarded by a dedicated cold-load conformance assertion.
3. **`agent-browser eval` byte-count quirk on multibyte output.** Asserting tree-group presence by
   evaluating strings containing the expand carets (`▾`/`▸`) returned a byte count instead of the
   value. Resolved in the test driver by asserting group presence one ASCII boolean at a time — a
   harness quirk, not a product defect.

## Reflections
- **What was tricky?** The hardest part was cross-route state for `/debug`. Because the App
  re-renders the whole tree on `hashchange`, the explorer components are unmounted on `#/debug`, so
  the "current state" section can't DOM-read `data-revision`/`data-doc-count`. The chosen mechanism —
  a single module-level `uistate.js` snapshot written by explorer/tree/content and read by `/debug` —
  is deliberately minimal (one object + a setter, no reactivity), which kept it from sprawling into a
  state store. Designing that in solution review (another RESOLVED P2 thread) paid off.
- **What would you tell yourself at the start?** The plan-review RESOLVED threads (cold-load race,
  recordError wiring for non-rpc failures, the uistate snapshot) were the three things that would
  have bitten during implementation — read them first, they are the map of the sharp edges. Also:
  for any auto-ui work, use lowercase-kebab project ids in fixtures from the very first run.
- **What did you almost do but didn't?** Almost fixed the inert/broken `doc.changed` `ev.path` match
  while generalizing `doc.js` into `content.js` (it was right there). Held off — requirements pin 025
  as the *static* explorer and liveness (the match fix + live nav refresh) is explicitly task 026.
  Carrying it would have blurred the task boundary and added untested wiring.

## Useful context
- **Stacking discipline:** 024 had already merged to `main`, so basing the worktree on `main`
  (rather than 024's branch) was correct and conflict-free. Verifying the four 024 endpoints actually
  existed on `main` (`project.list`, widened `doc.list` with `type`, `/api/doc/raw`, the harness
  flags) before creating the worktree avoided building against a phantom contract.
- **The `doc.changed` envelope shape** (context.md): the changed path is at `params.data.path`, NOT
  `params.path` — top-level `project`/`worktree` exist but path is nested. The `/debug` event log
  reads it correctly from `params.data?.path`; this is the same nesting that makes the old `doc.js`
  match a no-op.
- **Per-phase pre-commit gate** ran the full repo check (vet/lint/stale-ref/fixture-privacy/doc/beads)
  on every commit, so regressions never accumulated. `go build ./...` always succeeds for this task
  (assets are `//go:embed`-ed), so it is *not* a real check — acceptance is the agent-browser
  conformance run on **both** the embed and dev builds (013 feedback), which is what actually catches
  browser-layer defects (blank page, stale cache, iframe load).
- **`--no-verify` is guard-blocked** in this environment; commit normally and let the hook run.
