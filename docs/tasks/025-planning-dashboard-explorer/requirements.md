# Task 025: Planning Dashboard Explorer — Default-Landing Project & Doc Browser

> Phase 2 of the **Planning Docs Dashboard** epic (`docs/epics/002-planning-docs-dashboard.md`).
> Builds the frontend explorer on top of task 024's backend (Phase 1). **Assumes 024 has landed:**
> `project.list`, widened `doc.list` (`.md`+`.html` with `type`), `GET /api/doc/raw`, the agent
> validation harness (`--port 0`/`--ready-file`/`--projects`, `auto ui emit`, gated debug buffer),
> and the `.html` `doc.changed` derivation are all available.

## Problem

`auto-ui` ships a demo SPA — `Home`/`Dashboard`/`Doc` views and a flat doc picker
(`web/static/app.js`) — not the planning-docs explorer the epic is for. After task 024 the backend
can enumerate projects (`project.list`), list a project's whole `docs/` tree (`.md` + `.html`, each
tagged `{path, type}`), and serve verbatim HTML bytes (`/api/doc/raw`), but **no UI consumes any of
it**: there is no project switcher, no grouped doc tree, no type-aware content pane, and the
explorer is not the landing view. Agents also cannot yet observe the SPA's received events to
validate behavior end-to-end.

The goal of this task is the usable explorer: open `auto ui`, land on a dashboard that lists every
registered project, browse the selected project's entire `docs/` tree grouped sensibly, and read
markdown rendered in-page and self-contained HTML planning docs in an iframe — all linkable via the
URL hash and survivable across reloads.

## Goals

- Make the **explorer the default landing view**, retiring the demo `Home`/`Dashboard` pages.
- Populate a **project switcher** from `project.list`; the active project lives in the URL hash so
  views are linkable and survive reloads.
- Build a **grouped, collapsible doc tree** from `doc.list` for the active project (Tasks → `NNN-slug`
  → files; Epics; Research; Reference; Experiments; Spikes; root docs), grouped client-side by path
  prefix.
- Render docs **by type**: markdown in-page (the existing `marked` + `dompurify` path), self-contained
  HTML via an `<iframe src="/api/doc/raw?…">` with an "open in new tab" fallback.
- Add the **agent-observable debug surfaces** the epic requires as acceptance infrastructure: a
  `window.__autoui` notification ring buffer and a rendered `/debug` diagnostics page, both gated
  behind `AUTO_UI_DEBUG=1` (the page is always reachable; only its buffered history is gated).
- Make every interactive element **agent-assertable** via stable `data-testid` / `data-*` attributes
  so `agent-browser` can drive and verify the dashboard without scraping rendered text.

## Acceptance Criteria

**AC-1 (2.1): Project switcher**
- Given: a registry with one or more projects (served via `project.list`).
- When: the SPA loads.
- Then: a switcher (sidebar header / dropdown) lists every project by name; selecting one sets the
  active project in the URL hash (`#/explore?project=…&path=…&worktree=…`, `worktree` optional),
  re-lists that project's docs, and clears the content pane.
- And: loading a `#/explore?project=…` URL directly restores that project/selection; an empty
  registry renders an empty-state, not an error.
- And: the switcher control carries a `data-testid`, and each project option carries `data-project`.

**AC-2 (2.2): Doc tree / navigation pane**
- Given: an active project whose `docs/` tree contains tasks, epics, research, etc.
- When: the explorer lists it via `doc.list`.
- Then: a collapsible tree is rendered, grouped client-side by path prefix (Tasks → `NNN-slug` →
  files; Epics; Research; Reference; Experiments; Spikes; root docs); selecting a node routes to that
  doc (updates the hash, loads the content pane). Layout is two-pane: tree left, content right.
- And: each leaf node carries `data-testid`, `data-doc-path`, and `data-doc-type`; the tree root
  carries `data-doc-count` (so a harness can assert the tree and count docs without scraping text).

**AC-3 (2.3): Type-aware content pane + explorer as the default view**
- Given: a selected doc of either type.
- When: it is opened.
- Then: a **markdown** doc renders in-page via the existing `marked` + `dompurify` path (generalize
  `DocView`); an **HTML** doc renders in an `<iframe src="/api/doc/raw?project=…&path=…&worktree=…">`
  with a working "open in new tab" link as fallback.
- And: `auto ui` **lands on the explorer** (`#/explore`) by default; the demo `Home`/`Dashboard`
  pages are removed. A small **always-visible WS connection indicator** (e.g. a green/red dot +
  "connected"/"reconnecting" label, distilled from the demo `ping`/WS code) is kept in the explorer
  chrome; detailed connection state lives on the `/debug` page (AC-5).
- And: the content pane carries `data-revision` (increments on every (re-)fetch) and
  `data-last-updated`; the iframe carries a `data-testid` and an observable cache-bust nonce in its
  `src`; the refresh button, project switcher, and connection indicator each carry a `data-testid`.

**AC-4 (2.4): Client debug surface — `window.__autoui` (gated)**
- Given: the SPA is loaded with `AUTO_UI_DEBUG=1` (query param or settings).
- When: WebSocket notifications arrive.
- Then: `rpc.js` exposes `window.__autoui` — an ordered ring buffer of every received notification
  (with payload) plus counters — so an agent can
  `agent-browser eval "JSON.stringify(window.__autoui.events.slice(-5))"` and assert exactly which
  notification arrived.
- And: with `AUTO_UI_DEBUG` unset, `window.__autoui` is not exposed (production stays clean).

**AC-5 (2.5): `/debug` diagnostics page**
- Given: any running server (the route is always reachable on this trusted host).
- When: an agent or human opens `#/debug`.
- Then: one screenshot-able page renders: **connection** (WS status, reconnect count, `/api/hello`
  mode + bound port), an **event log** (reverse-chronological received WS notifications: type, time,
  project, path, expandable raw payload), an **error log** (failed `doc.get`/`doc.list`,
  parse/sanitize/iframe-load failures, plus `window.onerror` / `unhandledrejection`), and **current
  state** (active project, open doc path/type, content `data-revision`, tree `data-doc-count`,
  last-updated).
- And: rows carry `data-testid`; the page subscribes to the live WS from mount (useful even with
  `AUTO_UI_DEBUG` off — it then lacks only the pre-mount history backfilled from `window.__autoui`).

## Out of Scope

- **Backend enumeration / raw serving / harness** — delivered by task 024 (Phase 1); this task
  consumes those endpoints, it does not add or modify them.
- **Live updates** — open-doc live refresh, the `doc.js` `ev.data.path` match fix, and live nav-tree
  refresh are **Phase 3**, split out as **task 026** (decided 2026-06-13). 025 is the *static*
  explorer; the content pane and tree fetch on navigation/switch only, not on `doc.changed`.
- **Editing docs in the dashboard** — read-only explorer; authoring stays in the IDE / skills.
- **Auth / multi-tenant / hardening** — single trusted host, trusted network; the existing
  path-traversal guard (in 024) is the only security kept.
- **Multi-host aggregation** — projects are local to this machine via `projects.json`.
- **Cross-doc search / breadcrumbs / mermaid / dark mode / persisted last-open** — Phase 4 polish,
  built only if daily use demands it.
- **Any bus / hook-production / `doc.changed` wire-shape change** — owned by tasks 020/021/022.

## Open Questions

- [x] **Phase 3 liveness scope.** Resolved (2026-06-13): **static explorer only.** This task is
  Phase 2 — browse + render, no liveness. All of Phase 3 (open-doc live refresh, the `doc.js`
  `ev.data.path` match fix, live nav-tree refresh) is split out as **task 026**. Implication: when
  generalizing `DocView` into the explorer content pane (AC-3), the existing (broken) `doc.changed`
  subscription is **carried over inert / left untouched, not fixed here** — fixing and wiring it is
  026's job.
- [x] **Keep a connection indicator from the demo UI?** Resolved (2026-06-13): **keep a small
  always-visible connection indicator** distilled from the `ping`/WS demo (green/red dot + label) in
  the explorer chrome; the rest of `Home`/`Dashboard` is removed. Detailed connection state lives on
  the `/debug` page (AC-5).
