# Solution: Task 026 — Planning Dashboard Live Updates

> Phase 3 of the Planning Docs Dashboard epic (`docs/epics/002-planning-docs-dashboard.md`).
> **Almost entirely frontend** (subscriptions in `auto-ui/web/static/`), plus **one backend test
> assertion**. Stacks on task 024 (backend + harness, merged) and task 025 (the static explorer:
> `explorer.js` / `tree.js` / `content.js` / `rpc.js` `window.__autoui` / `#/debug`). Adds **no** new
> server endpoint, **no** bus change, **no** file watcher, **no** import-map dependency.

## Approach

Liveness rides the one signal that already exists (epic decision 2): agent edits a doc → hook →
`agent.tool.post` → `/api/rpc` ingest → `bus.DeriveDocChanged` → a `doc.changed` notification on the
WebSocket. 025 deliberately left the explorer **inert** (no `doc.changed` subscription) and did not
carry over `doc.js`'s broken match. 026 writes the **one correct subscription shape**, fixes the
wire-reading bug at its single source, and wires it into the two views that need it.

The whole design is four small pieces:

### 1. Fix the wire-shape read, once, in a shared helper (AC-1)

The bug: `doc.changed` arrives as the **full event envelope** in `params`
(`Event.AsNotification`, `event.go:103-109`). The changed path lives at `params.data.path`
(`DocChanged` payload, `payloads.go:23-30`), while `project`/`worktree` are **top-level** envelope
fields. The retired `doc.js:55-68` matched `ev.path` (always `undefined`), so it never fired.

Rather than re-derive this brittle read in both `content.js` and `tree.js` (where it could regress in
one place), add **one tiny shared module** `docevents.js` that both import:

```js
// auto-ui/web/static/docevents.js
// Normalize a received doc.changed notification (full event envelope) into the
// fields liveness matches on. The path is ALWAYS under ev.data.path; project/worktree
// are top-level envelope fields but also mirrored in data — read data first, fall back.
export function parseDocChanged(ev) {
  const d = (ev && ev.data) || {};
  return {
    project:  d.project  ?? ev?.project,
    path:     d.path,                       // <-- the fix: NOT ev.path
    worktree: d.worktree ?? ev?.worktree,
    branch:   d.branch   ?? ev?.branch,
  };
}

// True when a doc.changed targets the given open/active doc.
// A missing worktree on EITHER side matches any (parity with old doc.js semantics).
export function matchesDoc(ev, target) {
  const c = parseDocChanged(ev);
  if (!target.path || !c.path) return false;
  if (c.project !== target.project) return false;
  if (c.path !== target.path) return false;
  if (target.worktree && c.worktree && c.worktree !== target.worktree) return false;
  return true;
}
```

Backend coverage so the shape can't silently regress: extend `rpc_ingest_test.go`'s
`TestRPCIngestBroadcastAndDerive` (currently asserts only `docParams["type"]`, lines 111-120) to
assert `docParams["data"].(map[string]any)["path"]` equals the emitted path. This pins
`params.data.path` as the contract the client reads.

### 2. Live open-doc refresh in `content.js` (AC-2)

Add one `useEffect` subscription in the content pane (the generalized DocView from 025), keyed on the
open doc's `{project, path, worktree}` (props/hash), using the same `openRef`-style pattern `doc.js`
already used so the handler sees current values:

```js
useEffect(() => {
  const off = on("doc.changed", (ev) => {
    if (!matchesDoc(ev, openRef.current)) return;   // shared helper (piece 1)
    refresh();                                       // auto-apply immediately (decided)
  });
  return off;
}, [project, path, worktree]);
```

`refresh()` is the **same action 025's refresh button already calls** — for **markdown** it re-runs
`doc.get` and re-renders, bumping `data-revision` + `data-last-updated`; for **HTML** it bumps the
observable `v=<nonce>` in the iframe `src`, forcing a cache-busted reload (no `doc.get`; the raw route
serves the bytes). HTML liveness relies on the `.html` derivation coverage already live in
`isDocPath` (024 / 1.4, confirmed in source). A non-matching `doc.changed` returns early → **no**
`data-revision` bump. Decision recorded in requirements: **auto-apply immediately**, no "refresh
available" affordance.

### 3. Live nav-tree refresh in `tree.js` (AC-3)

Add one `useEffect` subscription in the tree, matching only on the **active project**, that re-lists
when a path the tree doesn't yet know about appears:

```js
useEffect(() => {
  const off = on("doc.changed", (ev) => {
    const c = parseDocChanged(ev);
    if (c.project !== activeProject) return;
    if (knownPaths.current.has(c.path)) return;   // already in tree → tree needs no re-list
    reloadDocList();                              // re-run doc.list + regroup
  });
  return off;
}, [activeProject, worktree]);
```

`reloadDocList()` re-runs `call("doc.list", {project, worktree})` and re-groups client-side (the same
grouping 025 already does). The tree root's `data-doc-count` grows to the new length and the new leaf
appears with `data-doc-path`/`data-doc-type` — no manual reload. Because re-list replaces the list
with fresh server truth, any concurrent deletions also reconcile **at that moment** (we just don't
add a dedicated delete trigger — see piece 4).

**Expand/collapse state must survive the reconcile.** This is a constraint on how the tree holds
expansion: it must be keyed by **stable group/node id (the path/prefix), not array index**, so
re-grouping a fresh `doc.list` preserves which groups are open. If 025's `tree.js` already keys
expansion by path (the natural choice), this is free; 026 verifies it and, if needed, makes the
expansion state a path-keyed map. (Called out as a coupling risk in context.md.)

### 4. Deletion / create-signal verdict (AC-4)

Per the resolved requirements: **reuse `doc.changed` only — no bus edit.** Creates and edits are live
(pieces 2–3); deletions reconcile on the next create-triggered re-list or manual navigation. 026
records the verdict in `feedback.md`: confirm during build that the create case is covered by
`doc.changed` (it is — a new doc's first write emits `doc.changed` for an unseen path), and state
that an explicit `doc.created`/`doc.removed` derivation in `auto-shared/bus/derive.go` is **not**
warranted for v1 (deletions have no hook signal today; adding one is a future, separately-scoped bus
change owned by task 021's surface).

### Why this is the minimal design

- **One subscription shape, one source of truth** for reading `doc.changed` (`docevents.js`) — the
  exact thing whose duplication caused the original bug.
- **Reuses 025's existing refresh/nonce/revision machinery** — the open-doc handler just calls the
  refresh action the button already triggers; nothing new to render.
- **No readiness/cold-load coupling.** `on()` registers synchronously into rpc.js's handler map and
  survives reconnects; the re-fetch only runs when an event arrives (socket already OPEN), so 026
  needs none of 025's `whenOpen()`/onStatus mount-gating. (At-most-once delivery means events during
  a disconnect are lost; resync-on-reconnect is explicitly **out of scope** — consistent with the
  bus's lossy contract and 025's navigation re-fetch.)
- **No new server route, no bus change, no watcher, no new dep.**

## Files

```
+ auto-ui/web/static/docevents.js              # parseDocChanged(ev) + matchesDoc(ev,target): the single wire-shape read (reads ev.data.path)
~ auto-ui/web/static/content.js                # add doc.changed useEffect: matchesDoc → refresh() (md re-fetch / html v=nonce bump); auto-apply immediately
~ auto-ui/web/static/tree.js                   # add doc.changed useEffect: unseen path for active project → reloadDocList()+regroup; expansion keyed by path (preserve on reconcile)
~ auto-ui/internal/server/rpc_ingest_test.go   # extend TestRPCIngestBroadcastAndDerive to assert params.data.path on the derived doc.changed
~ auto-ui/CLAUDE.md                            # document liveness wiring + the params.data.path wire-shape gotcha + docevents.js helper
~ docs/epics/002-planning-docs-dashboard.md    # mark sub-tasks 3.1 / 3.2 status = done
+ docs/tasks/026-planning-dashboard-live-updates/artifacts/conformance.md  # agent-browser liveness validation (AC-1..AC-5), embed + dev builds
```

No new files under `web/static/` require an embed/list change (`//go:embed all:static` picks them up).
No import-map change (no new bare specifiers). The only Go change is a test assertion.

## Test Coverage

| AC   | Test Type                | Where |
|------|--------------------------|-------|
| AC-1 | Go integration + e2e     | `rpc_ingest_test.go` asserts `params.data.path`; conformance.md confirms the client match fires (via `window.__autoui` + `data-revision`) |
| AC-2 | agent-browser e2e        | conformance.md — `emit` on open **md** bumps `data-revision`; on open **html** bumps iframe `v=`; non-matching `emit` causes **no** bump |
| AC-3 | agent-browser e2e        | conformance.md — `emit` a brand-new path → tree `data-doc-count` grows, new `data-doc-path` leaf appears; an open group stays expanded across the reconcile |
| AC-4 | decision record          | `feedback.md` (+ this solution) — verdict: reuse `doc.changed`, no `doc.removed` derivation for v1 |
| AC-5 | agent-browser e2e        | conformance.md — full liveness loop (launch via 024 harness, `?debug=1`, `auto ui emit`, assert via `window.__autoui` + attrs) on **embed** and **dev** builds |

Validation uses the 024 harness exactly as 025 does: `auto ui serve --port 0 --ready-file <tmp>
--projects <fixture>` with `AUTO_UI_DEBUG=1`; open `?debug=1#/explore?project=…`; trigger with
`auto ui emit --project … --path docs/…` (CLI, no Origin); observe via `window.__autoui` and
`data-*` attributes (never text diffs). `/api/debug/recent` cross-checks the server half.

## Out of Scope

- **The explorer shell / switcher / tree grouping / content rendering / `/debug` page / `window.__autoui`**
  — delivered by task 025; 026 adds subscriptions to `content.js`/`tree.js` and the shared helper, it
  does not rebuild those views.
- **Backend enumeration / raw serving / harness / `.html` derivation** — task 024; consumed, not
  modified. The only backend edit is a **test assertion** in `rpc_ingest_test.go`.
- **An explicit `doc.created`/`doc.removed` derivation** — deferred (AC-4 verdict); deletions
  reconcile on the next re-list / navigation.
- **Resync-on-reconnect / replay** — the bus is at-most-once and lossy by contract; events during a
  disconnect are not recovered. Re-fetch happens on navigation/switch (025) and on the next matching
  event.
- **A new file watcher in auto-ui** — liveness rides the existing bus signal (epic decision 2).
- **Changing the bus envelope / hook production / `doc.changed` wire shape** — owned by 020/021/022;
  AC-1's test **pins**, does not change, the shape.
- **Fixing 025's open review items** (cold-load WS race, `recordError` wiring, `/debug` cross-route
  state) — those are 025's to close; 026 does not depend on them.
- **Editing docs in the dashboard**, auth/hardening, multi-host, Phase 4 polish.
- **Technical:** no new import-map dependency; no `/api/hello` schema change; no embed/list change;
  no new server route.

## Rejected Alternatives

- **Read the path from top-level `ev.path`** — that *is* the original bug; the path serializes under
  `ev.data.path` (envelope in `params`). Confirmed in `event.go:103-109` + `payloads.go:23-30`.
- **Duplicate the match logic inline in `content.js` and `tree.js`** — rejected; duplicating the
  brittle envelope read is exactly how the bug could regress in one view. One shared
  `docevents.js` is the single source of truth (two real consumers, not speculative).
- **Add a file watcher / polling for liveness** — rejected by epic decision 2; the bus `doc.changed`
  signal already exists and is the one event path both views subscribe to.
- **Add `doc.created`/`doc.removed` derivation now** — rejected for v1 (resolved open question);
  reuse `doc.changed`, which covers the create case; deletions reconcile on re-list.
- **Re-list the tree on every `doc.changed` for the project** — rejected; unnecessary `doc.list`
  churn. Re-list only when an **unseen** path arrives (the create case); known-path edits are handled
  by the open-doc refresh, and the tree is already correct.
- **Incrementally diff/patch the tree instead of a full `doc.list` re-list** — rejected; `doc.list`
  is cheap and re-grouping is pure string work, so a full re-list + path-keyed expansion preservation
  is simpler and reconciles deletions for free at re-list time.
- **Resync the whole explorer on every WS reconnect** — deferred; not required by any AC and overlaps
  with 025's onStatus handling; the at-most-once contract makes occasional missed events acceptable.
- **Show a non-disruptive "doc updated — refresh" affordance for the open doc** — rejected per
  resolved open question; auto-apply immediately (matches epic 3.1).
