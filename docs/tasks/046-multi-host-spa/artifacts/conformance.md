# 046 Conformance Testing Strategy — multi-host SPA

Browser-driven conformance for the host dimension `(hostId, projectId)` in auto-ui,
in the style of tasks 025–027. **This is a specification** — `evidence/driver.sh`
is the executable companion that realises Parts 1, 4 (single-backend) and the
AC-1/AC-3/AC-6 slices of the two-backend path; the per-AC assertions below are the
full contract.

The **authoritative gate** for the server-side multi-backend behaviour is the Go
test suite (`cd auto-ui && go test ./...`). The browser layer asserts host-tagged
rendering, host-scoped selection/URL, host-scoped event matching, and per-backend
status rendering. Cross-references to the gating Go tests are called out per AC.

## 0. Architecture note (post-042 proxy)

auto-ui no longer reads local files. It dials one or more **autowatch backends**
(`auto ui backends add <uri>`); each backend reports its `hostId` via `daemon.status`
on connect. A true multi-host conformance therefore needs **two real autowatch
daemons** with DISTINCT host ids and DISTINCT RPC sockets, plus auto-ui configured
with both backend URIs. Everything runs under an **isolated `$HOME`** so the real
`~/.auto` is never touched (all auto config — `host.json`, `projects.json`,
`ui/backends.json` — resolves under `$HOME/.auto`).

## 1. Setup

### Build (dual)

```bash
# Embedded (production)
make build                                              # -> ./bin/auto

# Dev mode (live-from-disk assets; run auto from auto-ui/ cwd)
go build -tags dev -o bin/auto-dev ./auto-cli/cmd/auto  # -> ./bin/auto-dev
```

Run every assertion against **both** builds. The dev build resolves `web/static/`
relative to cwd, so launch it from `auto-ui/`.

### Isolated config root

Every process is launched with an overridden `HOME` pointing at a throwaway temp
dir, so the real `~/.auto` is untouched:

```bash
TMP=$(mktemp -d)            # never ~/.auto
export HOME="$TMP/<role>"   # per-role isolated config root
# auto config then lives under $HOME/.auto/{host.json,projects.json,ui/backends.json}
```

> agent-browser's Chrome cache lives under the REAL home — invoke agent-browser with
> `HOME=<real-home>` while the servers use the isolated `HOME` (see `driver.sh` `ab()`).

### Two-backend fixture (AC-1/AC-2/AC-3/AC-4/AC-5/AC-6)

Two isolated autowatch config roots on **distinct fake host ids**, each exposing
≥1 project, with **one project id COLLIDING across hosts** to prove disambiguation:

```
$TMP/da-h/.auto/host.json      -> {"hostId":"alpha-host"}
$TMP/da-h/.auto/projects.json  -> project id "demo", path $TMP/da-repo, remote .../demo.git
$TMP/da-repo/docs/tasks/001-demo/{plan.md,requirements.md,plan.html}

$TMP/db-h/.auto/host.json      -> {"hostId":"beta-host"}
$TMP/db-h/.auto/projects.json  -> project id "demo" (COLLIDES), path $TMP/db-repo, remote .../demo.git
$TMP/db-repo/docs/tasks/001-demo/{plan.md,requirements.md,plan.html}

$TMP/d-ui/.auto/ui/backends.json -> {"backends":[{"uri":"unix://$TMP/da.sock"},
                                                  {"uri":"unix://$TMP/db.sock"}]}
```

`docs/…/plan.html` carries a `<script type="application/json" id="pd-meta">` block so
`doc.list` returns `meta` (lifecycle/branch) for the host-scoped event tests.

### Single-backend fixture (AC-7)

One autowatch root (`hostId="alpha-host"`, project `shared-proj`) and one auto-ui
root whose `backends.json` lists only that one socket.

### Launch (the `--ready-file` / `--port 0` pattern, as in 027)

```bash
# autowatch backend — OS-assigned hook port, RPC on a unix socket in $TMP
HOME=$TMP/da-h  bin/auto-dev watch start \
  --rpc-addr unix://$TMP/da.sock --ready-file $TMP/da.ready --hook-addr 127.0.0.1:0

# auto-ui proxy — OS-assigned HTTP port; run from auto-ui/ for dev assets
( cd auto-ui && HOME=$TMP/d-ui AUTO_UI_DEBUG=1 ../bin/auto-dev ui serve \
    --port 0 --ready-file $TMP/d-ui.ready )
```

Poll each `--ready-file`, read the assigned port from its `addr`, then give the
manager a few seconds to dial both backends and learn their host ids (it dials on a
reconcile tick and learns `hostId` from each `daemon.status`).

Open the SPA at `http://127.0.0.1:<port>/?debug=1#/explore` (`?debug=1` enables
`window.__autoui`).

## 2. Per-AC assertions (exact selectors as implemented)

### AC-1 — Aggregated, host-tagged list across backends

With both backends connected, the project switcher lists the **union** of both
hosts' projects, each option tagged with its authoritative host:

```
select[data-testid=project-switcher] option            -> exactly 2 (one per host)
  option[data-host-id=alpha-host][data-project=demo]    present
  option[data-host-id=beta-host][data-project=demo]     present
```

No `ambiguous host` error appears (the aggregating handler fans out instead of
`Resolve("")`-ing a single backend).

> **Server gate:** `auto-ui/internal/server/project_aggregate_test.go` ›
> `TestProjectListAggregatesAcrossBackends` — two fake backends (distinct hostIDs),
> asserts merged length + per-entry `host`.

### AC-2 — Partial results on a backend error

If one backend errors on `project.list`, the aggregated list still returns the
healthy backend's projects (the failing backend is skipped, never failing the whole
list), and the failure is surfaced through `backends.list` (AC-6), not as a list
error.

> **Server gate:** `project_aggregate_test.go` › `TestProjectListPartialResultsOnBackendError`
> (skips the erroring backend, returns the rest) and `backends_list_test.go` ›
> `TestBackendsListReportsHealth` (the erroring/unreachable backend is reported
> `connected:false` with a non-empty `lastErr`).

### AC-3 — Host badge + same-name disambiguation

Flat switcher with a visible host badge; two same-named projects are independently
addressable:

```
select[data-testid=project-switcher] option
  - each carries data-host-id AND data-project
  - composite option value is "<hostId>\n<projectId>"  (host + "\n" + id)
  - the two "demo" options differ ONLY by data-host-id (alpha-host vs beta-host)
span[data-testid=host-badge][data-host-id=<activeHost>]   present, text = active hostId
```

Selecting each option routes to a hash with the matching `host=` + `project=`; the
two never collapse onto one another.

> **Browser (executed):** `evidence/dual-backend.txt` — switcher shows two `demo`
> options with `value` `alpha-host\ndemo` / `beta-host\ndemo`; `host-badge` present.

### AC-4 — `(hostId, projectId)` carried through RPCs / URL / hash

* `doc.list` / `doc.get` carry `hostId` — the store sends `params.hostId`
  (`store.js` `fetchDocs` / `fetchOpenDoc`), routed to the matching backend by
  `proxy.go`'s `proxyCall` → `Resolve(hostId)`.
* The raw-doc iframe `src` carries `hostId`:
  ```
  iframe[data-testid=doc-iframe] @src  contains  hostId=<activeHost>
  ```
* The hash carries a separate `host=` param (D-3); selecting a project/doc writes
  `#/explore?host=<h>&project=<p>[&path=<...>]`.
* **Reload restores `(host, project, path)`**: open a deep link, hard-reload, and the
  selection (switcher value, open doc, badge) is identical.

> **Browser (executed):** `evidence/single-backend.txt` — iframe `src` =
> `/api/doc/raw?project=shared-proj&path=…plan.html&v=1&hostId=alpha-host`.

### AC-5 — Host-scoped event matching

A `doc.changed(hostA, P, path)` refreshes the open doc **only** when the current
selection is `(hostA, P, path)`. An identically-named doc on `hostB` does NOT:

1. Select `(alpha-host, demo, docs/tasks/001-demo/plan.md)`; note
   `article[data-revision]`.
2. Emit a `doc.changed` for that path **stamped host=alpha-host** → `data-revision`
   bumps.
3. Emit a `doc.changed` for the same project id + path **stamped host=beta-host** →
   `data-revision` does NOT change.

Host comparison is asymmetric on purpose: a host differs only when **both** sides
carry it (a legacy host-less event still matches by project), so pre-host events keep
working.

> **Source gate:** `auto-ui/web/static/docevents.js` `matchesDoc` —
> `if (target.host && c.host && c.host !== target.host) return false;` plus the
> mirrored host-gate on the re-list path in `store.js`'s `on("doc.changed")`.
> 045 stamps `Host` on relayed events, so two same-named projects on different hosts
> never cross-refresh.

### AC-6 — Per-backend connection-status indicator

`backends.list` returns `Manager.Health()`; the topbar renders one row per backend:

```
[data-testid=backend-health-list]
  span[data-testid=backend-health][data-host-id=<h>][data-connected=true|false][data-state=connected|degraded|disconnected]
```

* Both backends connected → two rows, both `data-connected="true"`.
* Drop a backend → its row flips to `data-connected="false"` / `data-state="disconnected"`.

**Important nuance (honest):** the SPA fetches `backends.list` only on initial load
and on a **store (re)connect** (the rpc `onStatus` open transition) — there is NO
event-driven re-fetch on a passive backend drop, and a hash-only navigation does not
re-init the store. So the row update is observed after the next store reconnect or a
**real page reload** (distinct query string / `location.reload()`), not autonomously
the instant a backend dies. The *server* reflects the drop immediately — verify with
a direct `backends.list` WS probe (`evidence/dual-backend.txt` "SERVER TRUTH").

> **Server gate:** `auto-ui/internal/server/backends_list_test.go` ›
> `TestBackendsListReportsHealth` (shape + connected/`lastErr`), and
> `auto-ui/internal/backend/manager_test.go` › `TestBackendReconnectsAfterDisconnect`
> (an established peer's transport drop flips the conn unhealthy; a later tick
> reconnects).
> **Browser (executed):** `evidence/dual-backend.txt` — two connected rows; after
> dropping beta-host + a real reload, the beta row reads `data-connected="false"`,
> `data-state="disconnected"`, matching the server probe.

### AC-7 — Single-backend parity (badge shown)

With exactly one backend, list / open / live-refresh behave identically to pre-T9;
the only visible change is the always-shown host badge:

```
select[data-testid=project-switcher] option   -> 1 option, data-host-id=<host>
span[data-testid=host-badge]                   -> present (D-4: always shown)
nav @data-doc-count                            -> >= 1 (docs listed via the backend)
[data-testid=backend-health][data-connected=true]  -> the one backend, connected
[data-testid=conn-indicator] @data-conn-status -> "open"
```

Open a doc → it renders (markdown inline via `doc.get`, html via the raw iframe whose
`src` carries `hostId`); the `doc-refresh` button + `doc.changed` live refresh work as
in 026/027.

> **Server gate:** `project_test.go` (single-backend `project.list` carries `host`).
> **Browser (executed):** `evidence/single-backend.{txt,png}` — 1 host-tagged option,
> badge present (`hostId=alpha-host`), `data-doc-count=3`, backend-health connected,
> conn `open`, iframe `src` carries `hostId=alpha-host`.

## 3. Residual cosmetic flash edge (documented in Phase 2 commit)

A `doc.changed` for a path that exists under the active project on an **inactive**
host can momentarily flash the active tree node (the flash/expand signal in
`store.js` is keyed on the changed path, intentionally project/host-agnostic so the
tree can highlight any touched path). It NEVER triggers a cross-host re-list or a
cross-host open-doc refresh — those paths are host-gated (`matchesDoc` + the re-list
host guard). The flash is purely cosmetic and self-clears.

## 4. Grep gates (029 structural invariant)

```bash
grep -c 'on("' auto-ui/web/static/store.js            # expect 1 (doc.changed; the on("ping") was removed on main in 4898d01)
grep -rl 'onAny(' auto-ui/web/static/ | sort          # expect ONLY rpc.js + store.js
```

Any additional subscription site is a conformance failure (host-aware event matching
stays inside `store.js`'s existing `on("doc.changed")`). Note: `activity.js` carries a
pre-existing `on("doc.changed")` (task 027); 046 added no new subscription site.

## 5. Teardown

All fixtures live under `$TMP`; kill the watch + ui processes (by their unique socket
/ ready-file paths) and `find "$TMP" -delete`. Never write to or delete under
`~/.auto`.
