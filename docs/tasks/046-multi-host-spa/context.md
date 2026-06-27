# Context — 046 multi-host-spa

Grounding for the Solution. File:line refs verified 2026-06-27 against `main`.

## Where the host dimension is missing today

| Concern | Location | Current state |
|---|---|---|
| SPA selection | `auto-ui/web/static/store.js:32` | `selection {project, path, worktree}` — explicit "no host dimension" comment |
| Doc cache key | `store.js:260` `docsKey()` | keyed `project(+"@"worktree)` — no host |
| Event match | `auto-ui/web/static/docevents.js:7-27` | `parseDocChanged`/`matchesDoc` compare `{project, path, worktree}` only |
| Project switcher | `explorer.js:91-107` | `<select data-testid="project-switcher">`, each `<option value=id data-project=id>` renders `name||id`; no host |
| Conn indicator | `explorer.js:32-50` | `ConnIndicator` reflects only the single browser↔auto-ui WS (`onStatus`), not per-backend health |
| raw-doc URL | `content.js:17-24` `rawURL()` | builds `/api/doc/raw?project&path&worktree&v` — no `hostId` |
| RPC calls | `store.js` `fetchDocs`/`fetchOpenDoc` | `call("doc.list"/"doc.get", {project,path,worktree})` — no `hostId` |
| Hash | `router.js:6-19` | `parseHash`→`{view, params:URLSearchParams}`; `setHash(view, params)`. Arbitrary params pass through — adding `host` is transparent |

## Server seams that don't exist yet

1. **Aggregating `project.list`** — `server.go:109` registers `proxyCall(o.mgr, "project.list")`, which `mgr.Resolve("")`s a SINGLE backend → `ErrAmbiguousHost` with >1 backend (`backend/manager.go:393-400`). No fan-out/merge. Each single-backend entry already carries `host` (042, `project_test.go:45`).
2. **Per-backend health RPC** — `Manager.Health() []BackendHealth` exists (`manager.go:405-421`) but is **not** registered as any RPC. Only `ping`, `doc.list`, `doc.get`, `project.list` are registered (`server.go:97-109`).

### Manager internals (for fan-out)
- `conns map[string]*conn` keyed by URI (`manager.go:86`); `conn{uri, peer *rpc.Peer, hostID, connected, relayDegraded, lastErr, ...}` (`manager.go:56-74`).
- `Resolve(host)` returns the peer for a hostID, errors `ErrUnknownHost`/`ErrAmbiguousHost`/`ErrNoBackend` (`manager.go:373-401`).
- `Health()` snapshots ALL backends (connected or pending), sorted by URI.
- `BackendHealth{HostID json:"hostId"; URI json:"uri"; Connected json:"connected"; RelayDegraded json:"relayDegraded,omitempty"; LastErr json:"lastErr,omitempty"}` (`manager.go:101-108`).
- **Fan-out approach:** iterate `Health()` for `Connected` hosts, `Resolve(hostId)` each peer, `peer.Call(ctx,"project.list",nil)`, tag each returned entry's `host=hostId`, merge. A backend whose call errors is skipped (partial results) and surfaced via the health RPC. *(A small `Manager` helper returning `[]{hostID, peer}` for connected backends avoids the double-lock of Health+Resolve — solution decides.)*

## Handler + test harness pattern
- Register: `d.Register("backends.list", func(ctx, params)(any,error){ return o.mgr.Health(), nil })` after `server.go:109`.
- Aggregating handler is a closure over `o.mgr` (like `proxyCall`), not a verbatim proxy.
- Tests: `proxy_test.go` `newProxyServer(t, handlers)` stands up a fake backend over `net.Pipe` (implements `daemon.status` + custom handlers), waits for the manager to learn the hostID; `rpcCall(ctx,t,c,id,method,params)` does a WS round-trip. `project_test.go` is the closest template. Multi-backend tests need >1 fake backend — see `event_flow_test.go`'s `relayFleet` (`event_flow_test.go:90-155`) which already wires multiple backends with distinct hostIDs.

## Conventions
- **GR-F8** (epic.html:76): UI identity = `(hostId, projectId)`; every proxied RPC, raw-doc URL, event filter, SPA hash route carries both; `project.list` responses include `host`.
- **GR-N4** (epic.html:86): hostId from `~/.auto/host.json` (`$hostname-$user`); keys RPC routing + UI grouping.
- **RPC naming**: `noun.verb` lowercase (`doc.list`, `daemon.status`). New health method → **`backends.list`** (enumerate connected backends + health). Fits convention; plural noun mirrors the per-backend list semantics.
- **Bus subscriptions grep gate (029)**: `on("` only in `store.js`; `onAny(` only in `rpc.js`+`store.js`. Host-aware event matching stays inside `store.js`'s existing `on("doc.changed")` — no new subscription site.
- **Conformance style (025–027)**: browser-driven via `agent-browser` (open/eval/select/click/snapshot/screenshot), `data-testid` + `data-*` assertions, `window.__autoui` ring eval (gated `?debug=1`), dual builds (embed+dev), `artifacts/conformance.md` + `evidence/`. Isolated `projects.json` fixture (never `~/.auto`). Go tests for the two server seams.

## Decisions locked (Requirements tab)
- project.list aggregation: **server-side** handler.
- Hash identity: **separate `host=` param**.
- Host badge: **always shown** (even single host).
- New health RPC: **`backends.list`** (this doc's recommendation, resolves OQ4).

## Related Tasks (git history)
- **042 auto-ui-proxy-backends** (`8ea8192`, PR #98) — created `backend/manager.go` (BackendManager, Reconcile, Health), `server/proxy.go` (`proxyCall`, Resolve routing), `config/backends.go`, CLI `backends add/remove/list`; cut auto-ui over from local FS to proxy. Direct dependency: 046 extends its Manager (ConnectedPeers) + registration (server.go). Has `context.md`/`feedback.md`/`plan.html`.
- **045 auto-ui-event-aggregation** (`c27d236`, PR #104) — Manager subscribes each backend + forwards events to the hub via `SetEventSink`→`eventGate`; deterministic derived ids; `event_flow_test.go` multi-backend harness. 046 reuses that two-backend test harness for the aggregation tests; events already carry `Host` (so host-aware `matchesDoc` has data to match on).
- **029 auto-ui-state-refactor** — established the `store.js` single-store + 029 grep gate (`on("` only in store.js). 046 honors it (no new subscription site).
- **Artifact convention** (042/045): each task dir has `context.md` + `feedback.md` (post-merge learnings) + `plan.html`; conformance-heavy tasks (025–027) add `artifacts/conformance.md` + `evidence/`. 046 adds `artifacts/conformance.md`.
- **Drift check:** all target files current on `main` post-042/045; no mid-flight branches. (042-era `project.go`/`docs.go`/`raw.go` were since consolidated — RPC registrations now live in `server.go:107-109`, proxy logic in `proxy.go`.)
