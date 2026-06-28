---
hash: "e015d9ac"
id: "7b3e9d04"
read_when: "planning or sequencing sub-tasks for the planning-docs dashboard epic"
summary: "Epic (complete): auto-ui planning-docs explorer — browse every registered project's docs/ tree, render markdown inline and HTML in an iframe, live-refresh on agent edits. Phases 1–3 shipped; Phase 4 (optional polish) deferred."
title: "Epic: Planning Docs Dashboard — Browse, Render, Live, Switch"
---

# Epic: Planning Docs Dashboard — Browse, Render, Live, Switch

## Status: Complete (updated 2026-06-28)

All core phases shipped. Phase 4 (optional polish) is deferred until daily use demands it.

**Phase 1 (backend) shipped.** Sub-tasks 1.1–1.7 are complete (task 024): `project.list` RPC,
widened doc discovery (`.md`+`.html`) with a parameterized `cleanDocPath`, the `/api/doc/raw`
HTML route, the `.html` `doc.changed` derivation widening, the agent harness flags
(`--port 0`/`--ready-file`/`--projects` + `AUTO_UI_PORT`/`AUTO_PROJECTS_PATH`), the `auto ui emit`
helper, and the gated `/api/debug/recent` server event buffer — each with per-AC tests. The
remaining work is live updates (Phase 3).

**Phase 2 (frontend explorer) shipped.** Sub-tasks 2.1–2.5 are complete (task 025): the
project switcher, the client-grouped doc tree, the type-aware content pane (markdown inline / HTML
iframe), the explorer-as-default-landing cutover (demo `Home`/`Dashboard`/`Doc` retired), the gated
`window.__autoui` ring, and the `/debug` diagnostics page — validated by an agent-browser
conformance harness on **both** the embed and dev builds (36/36 each;
`docs/tasks/025-planning-dashboard-explorer/artifacts/conformance.md`). No Go changes — all of Phase
2 is `//go:embed`-ed `web/static/*`.

This epic is otherwise an assemble-and-extend build: **most of the plumbing already exists in
`auto-ui`**, not a greenfield build.

Already in place (do not rebuild):

- JSON-RPC 2.0 over WebSocket — `auto-ui/internal/server/rpc.go`, `ws.go`; singleton client
  `web/static/rpc.js` (`call`/`on`/`onStatus`, auto-reconnect, `wss://` behind tailscale).
- `doc.list` / `doc.get` RPC methods — `auto-ui/internal/server/docs.go` (walk `docs/**/*.md`,
  read one markdown file). Path-traversal-guarded via `resolveRoot` + `cleanDocPath`.
- **Live open-doc signal is wired server-side, but the client match is broken** — hook →
  `/api/rpc` ingest → `bus.Hub.Broadcast` → `bus.DeriveDocChanged` (`auto-shared/bus/derive.go`)
  → `doc.changed` notification reaches the browser. But the notification `params` is the **full
  event envelope** (`Event.AsNotification`, `event.go:103-109`), so the changed path lives at
  `params.data.path`, while `DocView` matches `ev.path` (`doc.js:61`) — always `undefined`, so
  the re-fetch never fires. A one-line client fix (read `ev.data.path`) makes it work; carried as
  an explicit step in 3.1.

<!-- RESOLVED(P1): "Live open-doc refresh already works" — the client-side match is broken on the wire shape
REVIEW: I traced the full path and the last hop doesn't match. The server delivers notifications via
`Event.AsNotification()` (auto-shared/bus/event.go:103-109), which sets `params` to the FULL event
envelope — so the DocChanged payload fields (`path`, `abs_path`, `branch`) live at `params.data.*`,
not at the top level. The envelope itself has `project` and `worktree` but no `path` field. Meanwhile
`doc.js:61` matches `if (ev.path !== open.path) return;` — `ev.path` is always `undefined`, so the
handler returns early and the re-fetch never fires. No test covers this hop: rpc_ingest_test.go's
TestRPCIngestBroadcastAndDerive asserts only `params.type == "doc.changed"`, never `params.path` or
`params.data.path`. The fix is one line in doc.js (read `ev.data.path` / fall back appropriately),
but the epic should (a) not list this as proven plumbing in Status, and (b) make fixing + e2e-verifying
the match an explicit step (it naturally belongs in or before 3.1, whose "already proven in doc.js"
claim inherits this error).
AUTHOR: Verified against source — the envelope (`event.go:28-41`) has top-level `project`/`worktree`
but no `path`; `path` serializes under `data`, and `AsNotification` (`event.go:103-109`) puts the
whole Event in `params`, so `doc.js:61`'s `ev.path` is always undefined and the re-fetch never fires.
(a) Corrected this Status bullet — it no longer claims "already works"; it states the signal is wired
server-side but the client match is broken. (b) Rewrote 3.1 to make fixing the match (read
`ev.data.path`) and adding the missing `params.data.path` e2e coverage explicit first steps. Gap #5
adjusted to stop implying open-doc refresh currently works.
-->

- Markdown rendering — `marked` + `dompurify` via the esm.sh import map in `index.html`.
- Project registry — `~/.auto/projects.json`, `auto-shared/config/projects.go`
  (`FindProjectByID`, `FindProjectByPath`); `resolveRoot` already accepts a `project` param.

**Phase 3 (live updates) shipped.** Sub-tasks 3.1–3.2 are complete (task 026): the broken
`doc.changed` client match is fixed at its single source (`web/static/docevents.js` reads
**`ev.data.path`**, never `ev.path`), the content pane (`content.js`) auto-refreshes an open
markdown doc (`data-revision++`) and reloads an open HTML iframe (`v=` nonce bump) on a matching
`doc.changed`, and the nav tree (`tree.js`) re-lists + reconciles when a `doc.changed` carries an
unseen path — expansion state survives the reconcile. A backend assertion in `rpc_ingest_test.go`
pins `params.data.path` so the wire shape can't silently regress. Validated by an agent-browser
liveness conformance harness on **both** the embed and dev builds (8/8 each;
`docs/tasks/026-planning-dashboard-live-updates/artifacts/conformance.md`). AC-4 verdict: reuse
`doc.changed` only (covers create+edit); deletions reconcile on the next re-list — no
`doc.created`/`doc.removed` bus change for v1. The only Go change is the test assertion; the rest is
`//go:embed`-ed `web/static/*`.

**Remaining:** Phase 4 polish (optional, deferred) — cross-doc search, breadcrumbs, recently-viewed,
mermaid, dark mode — built only if daily use demands it.

## Goal

I spend most of my time reading planning docs (requirements / solution / context / plan, plus
epics and research) across several projects, and constantly switching IDE panes to do it. The
end goal of this epic is a single web dashboard — the default view of `auto ui` — that lets me
browse and read **all** planning docs across **all** registered projects without leaving the
browser: pick a project, browse its `docs/` tree, read markdown rendered nicely and
self-contained HTML planning docs in an iframe, and have the open doc *and* the tree refresh
live as agents write files.

No auth, no multi-tenant concerns: this runs on a single trusted host on a trusted network
(loopback / `tailscale serve`). Security is explicitly not a v1 priority — but the existing,
cheap path-traversal guard stays, because it costs nothing and prevents accidental reads
outside `docs/`.

## Core architecture decisions

### 1. Two render paths, chosen by file type — markdown inline, HTML in an iframe

Markdown is rendered in-page (the existing `marked` + `dompurify` path). Self-contained HTML
planning docs (the `planning-doc` skill's `pd-components` output, e.g.
`docs/tasks/021-auto-bus-standard/artifacts/auto-bus-standard.html`) are **not** sanitized and
inlined — they pull scripts and components from a CDN and must execute. They render in an
**iframe** whose `src` points at a new raw-bytes HTTP route (`/api/doc/raw?project=…&path=…`)
serving the file verbatim with `Content-Type: text/html`. The iframe is *not* script-sandboxed
(trusted network; pd-components need scripts + CDN). A plain "open in new tab" link is the
explicit fallback if the iframe ever fails. Rationale: the JSON-RPC layer stays for structured
metadata (lists, content for markdown), and raw file bytes ride a dumb HTTP GET that an iframe
can address directly — no base64/srcdoc plumbing, no size ceiling.

### 2. One event path for liveness — reuse the bus `doc.changed` signal

Liveness does **not** add a new file watcher inside auto-ui. It rides the signal that already
exists: agent edits a file → hook → `agent.tool.post` → `DeriveDocChanged` → `doc.changed`
notification on the WebSocket. Both the open-doc refresh **and** the new tree refresh subscribe
to that one stream. A `doc.changed` carrying a path the tree hasn't seen triggers a `doc.list`
re-fetch, so a newly created task dir appears without a manual reload. (Deletions have no
explicit signal today and are reconciled on the next list / manual refresh — see 3.2 for
whether an explicit `doc.created`/`doc.removed` derivation is worth adding.)

**Both render paths are live.** `DeriveDocChanged` currently emits for `docs/**/*.md` only
(`isDocPath`, `derive.go:70-72`). Task 021 — the bus owner — has merged to main (#75), so this
epic makes the one-line extension widening `isDocPath` to `.html` as well (sub-task 1.4): a
derivation-*coverage* change, not a wire-shape/envelope change. With it, editing either a
markdown or an HTML planning doc emits `doc.changed`, so the open markdown re-renders and the open
HTML iframe reloads (3.1), and the tree reconciles newly created files of either type (3.2).
(Deletions still have no explicit signal — reconciled on the next list / reload.)

<!-- RESOLVED(P1): doc.changed is never derived for .html paths — contradicts 3.1/3.2 and the out-of-scope claim
REVIEW: `isDocPath` (auto-shared/bus/derive.go:70-72) derives doc.changed only for paths matching
`docs/` + `.md` suffix. Edits to self-contained HTML docs therefore produce NO doc.changed event,
so 3.1's "reload the iframe (cache-bust the src)" can never trigger, and 3.2's tree refresh will
never see a newly created .html file. But "Out of scope" says "Changing the bus, hook production,
or the doc.changed wire shape — owned by tasks 020/021/022; this epic only consumes the signal."
These can't both hold. Either (a) widening `isDocPath` to include `.html` is declared an explicit,
small in-scope change to auto-shared/bus/derive.go (it changes derivation coverage, not the wire
shape — arguably fine, but say so and coordinate with task 021's spec/registry), or (b) HTML docs
are accepted as not-live in v1 and 3.1 is scoped to markdown only. Pick one and update decision 2,
3.1, and Out of scope to agree.
AUTHOR: Verified `isDocPath` is `.md`-only (`derive.go:70-72`). Chose **(b)** for v1: HTML docs are
not-live, and `isDocPath` is NOT modified here — `auto-shared/bus/derive.go` belongs to task 021
(established project constraint: the hook→ui→socket event loop is owned by 021; don't fork or
duplicate it). Updated decision 2 (HTML has no live signal; iframe gets a manual refresh; markdown
is the live hot path), scoped 3.1 to markdown live-refresh, and reworded the Out-of-scope bullet
so extending coverage to `.html` is an explicit coordinated ask to 021 rather than in-scope work.
3.1/3.2 no longer assume an HTML `doc.changed`.
AUTHOR (revised): Task 021 has since merged to main (#75, confirmed `git merge-base --is-ancestor`),
so the 021-ownership blocker is gone. Flipped to **(a)**: widening `isDocPath` to `.html` is now an
in-scope one-line coverage change captured as new sub-task **1.4**. Decision 2 now states both
render paths are live; 3.1 reloads the HTML iframe on `doc.changed` (depends on 1.4); 3.2's tree
reconciles new `.html` files; the Out-of-scope bullet carves 1.4 out as the one allowed
derivation-coverage change (still no envelope/wire-shape change). This is a coverage edit, not a
duplicate event loop, so it stays consistent with the 021-ownership constraint.
-->


### 3. Extend, don't rebuild — the explorer is the default view; registry is the project source

The explorer is built on the existing SPA and RPC client and becomes the **landing view**,
replacing the demo `Home`/`Dashboard` pages in `web/static/app.js`. Projects come from
`~/.auto/projects.json` on this one host — no filesystem scanning, no multi-host aggregation in
v1. `resolveRoot` already validates a `project` param against the registry; the only missing
backend piece is enumerating the registry to the client.

## Background

- auto-ui's current shape and every integration point above are confirmed in source as of
  2026-06-11 (see file refs in Status and the gaps below).
- Planning docs follow `docs/tasks/NNN-slug/{requirements,solution,context,plan}.md` (+ optional
  `feedback.md`, `artifacts/*.html`); epics live in `docs/epics/`; research/reference/spikes/
  experiments live under `docs/` siblings. The dashboard browses **all** of a project's `docs/`
  tree (decision: 2026-06-11), grouped client-side by path prefix.
- Self-contained HTML planning docs are produced by the `planning-doc` skill and load
  `pd-components` from `cdn.jsdelivr.net` — hence the iframe + network requirement.
- Related: task 021 (auto-bus-standard) owns the CloudEvents bus this liveness rides; tasks
  020/022 own the hook-event production feeding it. This epic is a **consumer** of that bus, not
  a modifier of it.

## Current gaps (found 2026-06-11)

1. **No project enumeration.** `doc.list`/`doc.get` accept a `project` param, but nothing lists
   the registry to the client — the UI can't populate a project switcher. *(addressed by 1.1)*
2. **Doc discovery is markdown-only.** `walkDocs` filters to `.md` (`docs.go:129`) and
   `cleanDocPath` rejects non-`.md` (`docs.go:169`), so self-contained HTML docs are invisible
   and unservable. *(addressed by 1.2)*
3. **No raw-file serving.** `doc.get` returns sanitized markdown text only; there is no route
   that hands an iframe the verbatim bytes of an HTML doc. *(addressed by 1.3)*
4. **No explorer UI.** `app.js` ships demo `Home`/`Dashboard`/`Doc` views and a flat doc list —
   no project switcher, no grouped doc tree, no default-landing explorer. *(addressed by Phase 2)*
5. **Tree is not live.** `doc.changed` targets only the open doc (`doc.js`) — and that match is
   itself currently broken (see Status / 3.1); creating a new task/doc does not update the nav
   tree until a manual reload. *(addressed by 3.2, with the match fixed in 3.1)*

## Phase 1 — Backend: enumeration + raw serving

The precondition for the UI. Three small, independent RPC/HTTP additions on the existing
server; each reuses `resolveRoot` and the registry. Can land in any order.

### 1.1 `project.list` RPC

Add a `project.list` method (register beside `doc.list`/`doc.get` in
`auto-ui/internal/server/server.go`) that returns the registered projects from
`~/.auto/projects.json` via the existing registry provider — `{id, name, path, remote}` per
project, cheap-rung metadata only (no doc bodies). This is the switcher's data source.

### 1.2 Widen doc discovery to the whole `docs/` tree

Extend `walkDocs` to return **all** files under `docs/` that the dashboard renders — `.md`
**and** `.html` (decision: all of `docs/`) — each entry carrying `{path, type}` where `type` ∈
`{markdown, html}`. Leave grouping/categorization to the client (derive `tasks`/`epics`/
`research`/… from the path prefix) so the backend stays generic. Make `cleanDocPath` take the
allowed-extension set as a **parameter** instead of hard-coding `.md`, so each caller picks its
own policy without widening the others: discovery (`doc.list`) allows `{.md, .html}`, **`doc.get`
stays `.md`-only** (it returns markdown for in-page rendering — never HTML, per decision 1 and
1.3), and the raw route (1.3) allows `{.html}`. Keep the traversal guard and the "must be under
`docs/`" constraint in every case.

<!-- RESOLVED(P2): Widening cleanDocPath also widens doc.get — decide what doc.get does with .html
REVIEW: `cleanDocPath` is shared validation for `doc.get` (docs.go:72), not just discovery. If it
starts accepting `.html`, `doc.get` will happily read an HTML file and return its bytes in a field
named `"markdown"` (docs.go:85-88), which the current DocView then pipes through marked+dompurify —
exactly the sanitize-and-inline path decision 1 rules out. The epic routes HTML through the raw
route, but nothing here says whether doc.get should (a) keep rejecting `.html` (raw route is the
only HTML reader — requires per-caller extension policy rather than one shared cleanDocPath), or
(b) accept it and return a `type` field with a renamed/`content` payload. Also note 1.3 says the
raw route reuses "the same path validation as doc.get" — if validation is shared, the allowed-
extension set must be a parameter. Specify the intended doc.get behavior so 1.2/1.3 don't make
conflicting edits to the same function.
AUTHOR: Took option (a) via a parameterized validator. `cleanDocPath` now accepts the allowed-
extension set as an argument: `doc.get` keeps `{.md}` (never returns HTML through the `"markdown"`
field / sanitize-and-inline path), discovery allows `{.md,.html}`, and the raw route allows
`{.html}`. 1.2 and 1.3 now state this explicitly so they can't make conflicting edits to the same
function. doc.get behavior is pinned: markdown-only, unchanged payload.
-->


### 1.3 Raw-bytes HTTP route for HTML docs

Add `GET /api/doc/raw?project=…&path=…&worktree=…` serving the verbatim file with the right
`Content-Type` (`text/html` for `.html`), reusing `resolveRoot` (which already resolves the
optional `worktree` via `FindProjectByPath`) and the **same parameterized `cleanDocPath`** as
`doc.get` (1.2) but with the allowed set `{.html}`. The `worktree` param is carried for parity
with `doc.list`/`doc.get` so worktree planning docs (written before merge) are viewable. This is
the iframe `src` target for self-contained HTML docs. Markdown continues to flow through
`doc.get` (rendered in-page); this route is for the bytes an iframe needs.

### 1.4 Extend `doc.changed` derivation to `.html`

Widen `isDocPath` (`auto-shared/bus/derive.go:70-72`) so `DeriveDocChanged` emits for
`docs/**/*.html` in addition to `*.md`. Task 021 (the bus owner) is merged to main (#75), so this
is an in-scope one-line *coverage* change — it does **not** touch the event envelope or the
`doc.changed` wire shape. This is what makes HTML docs live (3.1 iframe reload, 3.2 tree
reconcile). Add/extend a derive test covering an `.html` path.

<!-- RESOLVED(P3): Decide whether the raw route (and the explorer hash) carry a worktree param
REVIEW: `doc.list`/`doc.get` both accept `worktree` and `resolveRoot` resolves it via
FindProjectByPath (docs.go:98-105); the existing DocView threads worktree through its URL params
and its doc.changed match. The raw-route query string here (`?project=…&path=…`) and 2.1's hash
format (`#/explore?project=…&path=…`) both omit it. If worktree views are wanted in the explorer
(agents usually write planning docs in worktrees before merge), the raw route and the hash need
the param; if not, that's a deliberate v1 narrowing worth one line — otherwise an implementer
will guess inconsistently across 1.3/2.1/3.1.
AUTHOR: Resolved in favour of carrying `worktree` everywhere — matching the existing DocView /
doc.list / doc.get pattern and the heavy worktree workflow (planning docs are written in worktrees
before merge). Added `worktree` to the raw route (here, resolved via `FindProjectByPath` in
`resolveRoot`), the explorer hash (2.1, optional), and the 3.1 match.
-->


### 1.5 Validation launch + isolation harness

Make `auto ui serve` drivable by an agent harness non-destructively:
- `--port 0` (OS-assigned) plus `--ready-file <path>` that writes the bound
  `127.0.0.1:NNNN` (or a JSON `{"addr":…}` line on stdout) so the agent reads the real
  port instead of scraping the stderr banner. Default stays 8080.
- Honor `AUTO_UI_PORT` in **both** `serve` and `auto hooks fire`'s `uiPort()` so a harness
  can pin the producer and the test server to the same ephemeral port (otherwise real
  hooks post to the settings default and miss the test instance).
- `--projects <path>` (or `AUTO_PROJECTS_PATH`) to point `resolveRoot` at an isolated
  fixture registry + temp `docs/` tree, leaving the user's real `~/.auto/projects.json`
  untouched (CLAUDE.md: populate disk, run as a user, clean up).

### 1.6 Synthetic event-emit helper

A one-shot command that builds a valid `agent.tool.post` envelope and POSTs it to
`/api/rpc`, e.g. `auto ui emit --project <id> --path docs/.../plan.md [--worktree …]` — so
triggering a `doc.changed` is a single deterministic call, not hand-assembled JSON that
silently derives nothing when a precondition (registered project, `docs/**` path, RFC3339
`time`) is missed. Document the companion real-edit recipe (`auto hooks fire` with a canned
PostToolUse payload) and the **Origin rule**: the ingest rejects any request carrying an
`Origin` header, so triggers must come from the CLI (no Origin), never a browser `fetch`
(always Origin) — agents *trigger via CLI, observe via browser*.

### 1.7 Server-side debug event buffer (gated)

Behind `AUTO_UI_DEBUG=1`, expose `GET /api/debug/recent` returning the last N broadcast
events (raw + derived). Lets an agent confirm the **server half** — "my POST derived one
`doc.changed`" — independently of any connected browser, isolating server-derivation bugs
from client-match bugs. Returns 404 in normal operation.

## Phase 2 — Frontend: the explorer (default view)

Depends on Phase 1. Built on the existing SPA + `rpc.js`. This is where the dashboard becomes
usable end to end.

### 2.1 Project switcher

A switcher (sidebar header / dropdown) populated by `project.list`; the active project lives in
the URL hash (`#/explore?project=…&path=…&worktree=…`, `worktree` optional) so views are linkable
and survive reloads, and worktree planning docs are addressable (matching `doc.list`/`doc.get`/
the raw route). Switching projects re-lists docs and clears the content pane. "Switch between
projects seamlessly in one dashboard" is this sub-task.

### 2.2 Doc tree / navigation pane

A collapsible tree built from `doc.list` for the active project, grouped client-side by path
prefix (Tasks → `NNN-slug` → files; Epics; Research; Reference; Experiments; Spikes; root docs).
Selecting a doc routes to it (updates the hash, loads the content pane). Two-pane layout:
tree on the left, content on the right. **Observability (acceptance):** each node carries
`data-testid`, `data-doc-path`, `data-doc-type`, and the tree root carries `data-doc-count`,
so a harness can assert the tree and detect new docs without scraping rendered text.

### 2.3 Content pane + make explorer the default

Render by type: markdown via the existing `marked`+`dompurify` path (generalize `DocView`),
HTML via an `<iframe src="/api/doc/raw?…">` with an "open in new tab" fallback link. Make the
explorer the **landing view** in `app.js`, retiring the demo `Home`/`Dashboard` pages (keep the
`ping`/WS demo only if it still earns its place as a connection indicator). **Observability
(acceptance):** the content pane carries `data-revision` (increments on every re-fetch) +
`data-last-updated`, the iframe carries a `data-testid` and an observable cache-bust nonce in its
`src`, and the key controls (refresh button, project switcher, connection indicator) carry
`data-testid` — so a harness asserts re-render/reload via attributes, not text diffs.

### 2.4 Client debug surface for observation (gated)

Behind `AUTO_UI_DEBUG=1` (query param or settings), expose `window.__autoui` from `rpc.js`: an
ordered ring buffer of every received notification (with payload) plus counters, so an agent can
`agent-browser eval "JSON.stringify(window.__autoui.events.slice(-5))"` and assert *exactly*
which `doc.changed` arrived and whether the match fired — turning liveness validation from DOM
inference into a direct assertion. The single highest-value observation hook; pairs with the
`data-testid`/`data-revision` attributes in 2.1–2.3 and 3.1.

### 2.5 `/debug` page — a rendered diagnostics view

A dedicated `#/debug` route in the SPA that renders, in one screenshot-able place, everything an
agent (or a human) needs to debug the dashboard. Where 2.4 is the *programmatic* tap (`eval` into
a global), this is the *rendered* counterpart: `agent-browser snapshot`/`screenshot`/`get text`
on one page yields the whole picture, no internal-global poking. It shows:
- **Connection** — WS status (connecting/open/closed), reconnect count, `/api/hello` mode
  (embed/disk) + bound port.
- **Event log** — live, reverse-chronological list of received WS notifications (type, time,
  project, path, expandable raw payload); backfilled from `window.__autoui` when `AUTO_UI_DEBUG`
  is on, and appended live from mount regardless.
- **Error log** — centralizes client errors that are otherwise scattered per-view: failed
  `doc.get`/`doc.list`, parse/sanitize failures, iframe load errors, plus `window.onerror` /
  `unhandledrejection` captured into a global sink.
- **Current state** — active project, open doc path/type, content `data-revision`, tree
  `data-doc-count`, last-updated.
- **RPC console** (optional) — buttons to invoke `ping`/`doc.list`/`doc.get` over the existing WS
  (these are *not* Origin-guarded, unlike the `/api/rpc` ingest) and show the raw response.

Rows carry `data-testid` so the page is assertable, not just viewable. The page subscribes to the
live WS itself, so it is useful even with `AUTO_UI_DEBUG` off (it just lacks the pre-mount history
and the server-side `/api/debug/recent` cross-check). Read-only diagnostics on a trusted host, so
the route is always reachable; the *gated* parts are the buffers it surfaces (2.4 / 1.7).

## Phase 3 — Live updates

Depends on Phase 2. Reuses the single `doc.changed` stream (decision 2).

### 3.1 Open-doc live refresh in the explorer, and fix the broken match

**First fix the existing match bug.** `doc.changed` arrives as the full event envelope, so the
handler must read the path from `ev.data.path` (and `ev.data.worktree`/`ev.data.branch`), not
`ev.path` — the current `doc.js:61` compares `ev.path` (always `undefined`) and never re-fetches.
Add the missing e2e/integration coverage (`rpc_ingest_test.go` asserts only `params.type`, never
`params.data.path`) so the match can't silently regress. **Then** wire the explorer's content
pane to the corrected subscription, matching on `{project, path}` (and `worktree` when present):
an open **markdown** doc re-fetches and re-renders on edit, and an open **HTML** doc reloads its
iframe (cache-bust the `src`). HTML liveness depends on 1.4 widening derivation to `.html`.
**Validation (acceptance):** assert liveness via `window.__autoui` (2.4) + the content pane's
`data-revision` / the iframe `src` nonce — not by diffing rendered text.

### 3.2 Live nav-tree refresh

On a `doc.changed` for the active project whose path the tree doesn't yet contain, re-run
`doc.list` and reconcile the tree so newly created tasks/docs appear with no manual reload
("tree + open doc" liveness — decision: 2026-06-11). Evaluate during build whether
`doc.changed`-only is sufficient or whether an explicit `doc.created`/`doc.removed` derivation
in `auto-shared/bus/derive.go` is warranted (deletions have no signal today); prefer reusing
`doc.changed` if it covers the create case acceptably.

## Phase 4 — Polish (optional, build only if the daily-use itch demands it)

Deferred until the core loop is in daily use; each is independently droppable:

- Cross-doc search / filter box over the tree (title + path; full-text is a bigger lift).
- Breadcrumbs, recently-viewed list, keyboard navigation between docs.
- Remember last-open project/doc across sessions (beyond the URL hash).
- Rendering niceties: mermaid in markdown, sticky doc-section TOC, dark mode.

## Validation & instrumentation (cross-cutting)

Agents must be able to validate this dashboard end-to-end themselves. The tool is
**`agent-browser`** (confirmed working headless on this host: `open`/`get`/`eval`/`snapshot`/
`screenshot`), driving the served SPA and introspecting it via `eval`. The loop is: **launch**
(1.5) → **open** in agent-browser → **trigger** a `doc.changed` via the CLI emit helper (1.6) →
**observe** the effect → assert via the injected taps. Two complementary observation surfaces: the
**`/debug` page** (2.5) renders everything in one screenshot-able view (`snapshot`/`get text`),
while **`window.__autoui`** (2.4) is the programmatic ring buffer for precise `eval` assertions.

The hard part is observation: when a doc re-fetches, the DOM can look identical, so liveness must
be *made observable*. All debug surfaces are gated behind a single `AUTO_UI_DEBUG=1` flag so
production stays clean. Required observable hooks, by sub-task (acceptance criteria, not extras):

| Observable hook | Lands in | Why |
|---|---|---|
| `--ready-file` / `--port 0` / `AUTO_UI_PORT` / `--projects` | 1.5 | deterministic, isolated launch |
| `auto ui emit` + the Origin "trigger-via-CLI" rule | 1.6 | one-call deterministic trigger |
| `GET /api/debug/recent` server ring buffer | 1.7 | confirm the server half independently |
| `window.__autoui` WS event ring buffer | 2.4 | assert which notification arrived (`eval`) |
| `/debug` page (events + errors + state) | 2.5 | one screenshot-able diagnostics view (`snapshot`) |
| `data-testid` (+ `data-project`/`data-doc-path`/`data-doc-type`) | 2.1–2.3 | stable selectors for agent-browser |
| `data-revision` + `data-last-updated` (content), `data-doc-count` (tree) | 2.3, 2.2 | "did it re-render?" without diffing text |
| observable iframe cache-bust nonce in `src` | 2.3 / 3.1 | confirm HTML iframe reload via `get attr src` |

Two operational notes for harness authors: (a) the disk-vs-embed asset split is a **build tag**
(`-tags dev`), not a runtime flag — validate the shipped artifact with the embed build, iterate
on the SPA with the dev build; (b) `/api/hello` (returns `{mode}`) doubles as the readiness probe.

## Out of scope

- **Auth / multi-tenant / hardening** — single trusted host, trusted network; the existing
  traversal guard is the only security kept, and only because it's free.
- **Multi-host aggregation** — projects are local to this machine via `projects.json`; no
  cross-host RPC (that's the ETL multi-host model's concern, not this dashboard's).
- **Editing docs in the dashboard** — read-only explorer; authoring stays in the IDE / skills.
- **Changing the bus envelope / hook production / `doc.changed` wire shape** — owned by tasks
  020/021/022; this epic only consumes the signal. The one exception is a one-line extension of
  `isDocPath` derivation *coverage* to `.html` (sub-task 1.4), which adds no envelope/wire-shape
  change; task 021 is merged (#75), so it lands here rather than as a separate ask.
- **A new file watcher in auto-ui** — liveness rides the existing bus signal (decision 2).
- **Indexing/search backed by auto-search** — Phase 4's filter is client-side over listed
  paths; wiring the search index is a later epic if needed.

## Sub-task index

| #   | Sub-task                                   | Depends on   | Status |
|-----|--------------------------------------------|--------------|--------|
| 1.1 | `project.list` RPC                         | —            | done   |
| 1.2 | Widen doc discovery to whole `docs/` tree  | —            | done   |
| 1.3 | Raw-bytes HTTP route for HTML docs         | —            | done   |
| 1.4 | Widen `doc.changed` derivation to `.html`  | —            | done   |
| 1.5 | Validation launch + isolation harness      | —            | done   |
| 1.6 | Synthetic event-emit helper                | —            | done   |
| 1.7 | Server-side debug event buffer (gated)     | —            | done   |
| 2.1 | Project switcher                           | 1.1          | done   |
| 2.2 | Doc tree / navigation pane                 | 1.2          | done   |
| 2.3 | Content pane + explorer as default view    | 1.2, 1.3, 2.2| done   |
| 2.4 | Client debug surface (`window.__autoui`)   | —            | done   |
| 2.5 | `/debug` page (events + errors + state)    | 2.4          | done   |
| 3.1 | Open-doc live refresh in explorer          | 2.3, 1.4     | done   |
| 3.2 | Live nav-tree refresh                      | 2.2, 2.3, 1.4| done   |
| 4.x | Polish (search, breadcrumbs, mermaid, …)   | Phase 3      |        |

## Implementation order (sketch)

> The validation-harness sub-tasks interleave with these feature steps so each *Validate* line is
> actually runnable. Land **1.5** (launch + `--ready-file`/`AUTO_UI_PORT`/`--projects`) and **1.6**
> (`auto ui emit`) early in Phase 1 — before the first browser-validated step (6) — and **2.4**
> (`window.__autoui`) with 2.3, so the liveness checks in steps 9–10 assert on received
> notifications rather than DOM diffs. **1.7** (server debug buffer) can land any time in Phase 1.
> All debug surfaces are gated behind `AUTO_UI_DEBUG=1`.

1. **3.1a — fix the `doc.changed` client match first** (pull the one-line `doc.js` fix +
   `params.data.path` e2e coverage forward out of 3.1): smallest change, de-risks the whole
   liveness story before anything is built on it.
   - *Validate:* new test asserts `params.data.path` on the notification; manually edit a
     `.md` while a doc is open in the current DocView → re-fetch fires.
2. **1.2 — widen doc discovery + parameterize `cleanDocPath`**: the shared validator lands
   first so 1.3 builds on it rather than editing the same function twice.
   - *Validate:* `doc.list` returns `.html` entries with `type`; `doc.get` still rejects
     `.html`; existing traversal-guard tests pass.
3. **1.1 — `project.list` RPC** (independent, can go in parallel with 1.2).
   - *Validate:* RPC call returns `{id, name, path, remote}` for every entry in
     `~/.auto/projects.json`; empty registry returns `[]`, not an error.
4. **1.3 — raw-bytes HTTP route** (after 1.2's parameterized validator).
   - *Validate:* `curl /api/doc/raw?...` on a pd-components HTML doc returns verbatim bytes
     with `Content-Type: text/html`; `.md` paths and traversal attempts are rejected;
     `worktree` param resolves.
5. **1.4 — widen `isDocPath` to `.html`** (independent one-liner; any time in Phase 1).
   - *Validate:* derive test: `docs/**/*.html` emits `doc.changed`, non-`docs/` `.html`
     does not; `.md` behavior unchanged. `go build ./...` + tests in `auto-shared`.
6. **2.1 — project switcher**.
   - *Validate:* in browser — switching projects re-lists docs, clears the pane, updates
     the hash; reload on a `#/explore?project=…` URL restores the view.
7. **2.2 — doc tree**.
   - *Validate:* this repo's `docs/` tree groups correctly (Tasks/Epics/Research/…);
     selecting a doc updates the hash and loads it.
8. **2.3 — content pane + explorer as default**.
   - *Validate:* end-to-end in browser — markdown doc renders inline, HTML doc renders in
     iframe (and via "open in new tab"); `auto ui` lands on the explorer; demo pages gone.
9. **3.1 — open-doc live refresh in the explorer** (match already fixed in step 1; this is
   wiring the explorer pane to it).
   - *Validate:* agent-edit (or simulated `/api/rpc` ingest) of the open markdown doc
     re-renders it; editing the open HTML doc reloads the iframe (needs 1.4).
10. **3.2 — live nav-tree refresh**.
    - *Validate:* create a brand-new task dir + file while the explorer is open → tree
      shows it without reload; record the verdict on whether `doc.changed`-only suffices
      or `doc.created`/`doc.removed` is needed.
11. **4.x — polish**: only after the loop survives real daily use; each item validated by
    "does the itch go away", not upfront.
