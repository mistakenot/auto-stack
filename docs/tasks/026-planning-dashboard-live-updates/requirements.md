# Task 026: Planning Dashboard Live Updates — Open-Doc Refresh & Live Nav Tree

> Phase 3 of the **Planning Docs Dashboard** epic (`docs/epics/002-planning-docs-dashboard.md`).
> Adds liveness to the static explorer built by **task 025 (Phase 2)**, on top of the backend from
> **task 024 (Phase 1)**. **Assumes 024 and 025 have landed:** `project.list`, widened `doc.list`
> (`.md`+`.html` with `type`), `GET /api/doc/raw`, the `.html` `doc.changed` derivation, the agent
> validation harness (`--port 0`/`--ready-file`/`--projects`, `auto ui emit`, `/api/debug/recent`),
> and the frontend explorer (`explorer.js` switcher, `tree.js` grouped tree, `content.js` type-aware
> pane, `rpc.js` `window.__autoui` ring, `#/debug` page) all exist.

## Problem

The explorer is **static**: the content pane and nav tree fetch only on navigation or project
switch. When an agent writes a planning doc while the explorer is open, nothing updates until a
manual reload. The plumbing to fix this already exists end-to-end — an agent edit produces a hook
event → `/api/rpc` ingest → `bus.Hub.Broadcast` → `DeriveDocChanged` → a `doc.changed` notification
on the WebSocket — but **no view consumes it**, and the one historical consumer was broken:

- The retired `doc.js` matched the changed path at `ev.path`, which is **always `undefined`** —
  the path serializes under `ev.data.path` because `Event.AsNotification` puts the whole event
  envelope in `params` (`auto-shared/bus/event.go:101-110`; envelope has top-level `project`/
  `worktree` but `path`/`abs_path`/`branch` live under `data`). Task 025 explicitly did **not**
  carry that broken subscription into the explorer — 026 writes the correct one.
- No backend test covers this hop: `rpc_ingest_test.go` asserts only `params.type == "doc.changed"`,
  never `params.data.path`, so the match shape can silently regress.

The goal of this task is liveness for both render paths and the nav tree: an open **markdown** doc
re-fetches and re-renders on edit, an open **HTML** doc reloads its iframe (cache-busted), and the
nav tree reconciles newly created docs — all without a manual reload, validated by the agent-browser
harness from 024/025.

## Goals

- **Fix the `doc.changed` client match** so it reads the changed path from `ev.data.path` (with
  `project`/`worktree` from the envelope), and add the missing backend coverage asserting
  `params.data.path` so the wire shape can't silently regress.
- **Live open-doc refresh** in `content.js`: an open markdown doc re-fetches (`doc.get`) and
  re-renders, and an open HTML doc reloads its `<iframe>` by bumping the `v=<nonce>` cache-bust —
  on a `doc.changed` matching the open doc's `{project, path}` (and `worktree` when present).
- **Live nav-tree refresh** in `tree.js`: a `doc.changed` for the active project whose path the
  tree does not yet contain triggers a `doc.list` re-fetch and tree reconcile, so a newly created
  task/doc appears without a manual reload; client-side expand/collapse state survives the reconcile.
- **Make liveness observable** for agent validation: reuse `window.__autoui` (2.4), the content
  pane's `data-revision` / iframe `src` nonce, and the tree's `data-doc-count` so the harness
  asserts re-render/reload/tree-growth via attributes and the event buffer, not by diffing text.
- **Record a verdict** on whether `doc.changed`-only is sufficient or an explicit
  `doc.created`/`doc.removed` derivation is warranted (deletions have no signal today).

## Acceptance Criteria

**AC-1 (3.1a): Fix the `doc.changed` match + backend coverage**
- Given: a `doc.changed` notification delivered as the full event envelope (path under
  `params.data.path`; `project`/`worktree` top-level).
- When: the explorer's live subscription receives it.
- Then: the changed path is read from `ev.data.path` (not `ev.path`), matched against the open/active
  state by `{project, path}` and by `worktree` when both sides carry one (a missing worktree on
  either side matches any).
- And: a backend test (extending `rpc_ingest_test.go`) asserts the notification carries
  `params.data.path` (and `params.type == "doc.changed"`), so the wire shape is pinned.

**AC-2 (3.1): Live open-doc refresh — markdown and HTML**
- Given: a doc is open in the explorer content pane.
- When: a matching `doc.changed` arrives (open doc's `{project, path}`, `worktree` when present).
- Then: an open **markdown** doc re-fetches via `doc.get` and re-renders, and the content pane's
  `data-revision` increments and `data-last-updated` updates.
- And: an open **HTML** doc reloads its `<iframe>` by bumping the observable `v=<nonce>` in its
  `src` (no `doc.get`; the raw route serves the bytes). HTML liveness relies on the `.html`
  derivation coverage already shipped in 024 (sub-task 1.4).
- And: a `doc.changed` that does **not** match the open doc causes **no** re-fetch/reload (no
  `data-revision` bump).

**AC-3 (3.2): Live nav-tree refresh**
- Given: the explorer is open on an active project.
- When: an agent **writes** a new doc file under that project's `docs/` tree (the file now exists on
  disk) and a `doc.changed` arrives carrying that path, which the current tree does **not** contain.
  (`doc.changed` is the invalidation signal, not the file creator — `tree.js` reconciles by re-running
  `doc.list`, which enumerates the project's `docs/` directory on disk.)
- Then: `tree.js` re-runs `doc.list` for the active project and reconciles the tree so the new
  node appears with no manual reload; the tree root's `data-doc-count` increases to match the new
  `doc.list` length; the new leaf carries `data-doc-path`/`data-doc-type`.
- And: existing client-side expand/collapse state is preserved across the reconcile (a reconcile
  triggered by an unrelated new doc does not collapse the user's open groups).
- And: a `doc.changed` for a path the tree **already** contains does not require a re-list to keep
  the tree correct (it is already present); the open-doc refresh (AC-2) still fires if it matches.

**AC-4 (3.2 verdict): Deletion / create-signal evaluation**
- Given: `doc.changed` is the only live signal; deletions have no derivation today.
- When: 026 is implemented.
- Then: the task records (in `feedback.md` / solution) an explicit verdict on whether reusing
  `doc.changed` covers the **create** case acceptably and whether an explicit
  `doc.created`/`doc.removed` derivation in `auto-shared/bus/derive.go` is warranted — defaulting to
  **reuse `doc.changed`** and reconciling deletions on the next manual refresh / navigation unless
  the build surfaces a concrete reason to add the derivation (see Open Questions).

**AC-5: End-to-end liveness validation (agent-browser)**
- Given: an isolated instance launched via the 024 harness
  (`auto ui serve --port 0 --ready-file <tmp> --projects <fixture>` with `AUTO_UI_DEBUG=1`) and the
  explorer opened at `?debug=1#/explore?project=…`.
- When: the harness triggers a `doc.changed` via `auto ui emit --project … --path docs/…` (CLI, no
  Origin) for (a) the open markdown doc, (b) the open HTML doc, and (c) a brand-new doc that the
  harness **writes into the fixture's `docs/` tree first** (emit does not create files), then emits.
- Then: (a) bumps `data-revision`; (b) bumps the iframe `v=` nonce; (c) grows the tree
  `data-doc-count` and adds the leaf — each confirmed against `window.__autoui` showing the
  `doc.changed` arrived and matched. The conformance script (artifact) is run against both the
  **embed** and **dev** builds.

## Out of Scope

- **Backend enumeration / raw serving / harness / `.html` derivation** — delivered by task 024;
  consumed here, not modified. (Exception: extending the existing `rpc_ingest_test.go` assertion in
  AC-1, and *only if* AC-4's verdict says so, a coverage-only `doc.created`/`doc.removed` derivation —
  decided in Open Questions before any bus edit.)
- **The explorer shell / switcher / tree grouping / content rendering / `/debug` page** — delivered
  by task 025; 026 adds live subscriptions to `content.js` and `tree.js` and the match fix, it does
  not rebuild those views.
- **A new file watcher in auto-ui** — liveness rides the existing bus `doc.changed` signal
  (epic decision 2); no polling, no fs-notify.
- **Changing the bus envelope / hook production / `doc.changed` wire shape** — owned by tasks
  020/021/022; 026 only consumes the signal (and the AC-1 test pins, not changes, the shape).
- **Editing docs in the dashboard**, auth/multi-tenant/hardening, multi-host aggregation.
- **Phase 4 polish** — cross-doc search, breadcrumbs, recently-viewed, mermaid, dark mode, persisted
  last-open.

## Open Questions

- [x] **Deletion liveness.** Resolved (2026-06-13): **defer — reuse `doc.changed` only.** No edit to
  `auto-shared/bus/derive.go`; 026 stays a pure consumer of the existing signal. Creates and edits
  appear live (AC-2/AC-3); deletions are reconciled on the next manual refresh / navigation. AC-4's
  verdict defaults to "reuse `doc.changed`" unless the build surfaces a concrete reason otherwise.
- [x] **Open-doc refresh behaviour.** Resolved (2026-06-13): **auto-apply immediately** (matches epic
  3.1). A matching `doc.changed` re-fetches + re-renders markdown and bumps the HTML iframe `v=`
  nonce the moment it arrives (`data-revision++` immediately) — no intermediate "refresh" affordance.
  The live tree reconcile (AC-3) is non-disruptive regardless.
