# Context: Task 045

Codebase + docs grounding for [plan.html](./plan.html) — epic-003 Task 8: auto-ui subscribes each autowatch backend (`bus.subscribe`), merges relayed events into its Hub (verbatim, deduped by `id`) for browser broadcast.

## Key Files

### The subscription seam (where T8 lives)
- `auto-ui/internal/backend/manager.go:201` — `connect()` builds the peer with `rpc.NewPeer(netConn)` and **no `WithOnNotify`** today; after dial it calls `daemon.status` (handshake) but **never `bus.subscribe`**. T8 adds: a `WithOnNotify` handler + a `bus.subscribe` call here, on every (re)connect.
- `auto-ui/internal/backend/manager.go:68-103` — `Manager` struct (`backendsPath`, `dial`, `interval`, `conns map[string]*conn`) + `NewManager(backendsPath, dial, interval)`. **No Hub/sink reference today** — T8 adds an `onEvent func(bus.Event)` callback.
- `auto-ui/internal/backend/manager.go:135` `Reconcile` / `:107` `Run` — the live-reconcile loop (042) that redials dropped backends; re-subscribe rides this automatically since it re-runs `connect()`.

### Inbound notification delivery (rpc.Peer)
- `auto-shared/rpc/peer.go:71-74` — `WithOnNotify(fn func(Request)) Option`.
- `auto-shared/rpc/peer.go:419-442` — `handleNotification`: swallows the reserved `$keepalive` ping, otherwise fires `onNotify(req)` on the **read-loop goroutine**.
- `auto-shared/rpc/message.go:28-33` — `Request{JSONRPC, ID, Method, Params json.RawMessage}` — `Method` = event type, `Params` = raw `bus.Event` bytes.

### Relay producer side (autowatch, task 040 — frozen)
- `auto-watch/internal/rpcserver/subscribe.go:12-24,42-54` — `bus.subscribe` handler registers a `peerSink{peer}` on autowatch's hub; `peerSink.Deliver(ev)` → `peer.Notify(ev.Type, ev)`. Idempotent (second subscribe is a no-op); cancelled on disconnect.
- Wire shape of a relayed event = JSON-RPC notification: `{"jsonrpc":"2.0","method":"<ev.Type>","params":<full bus.Event>}`.
- **`ctl.*` gating:** relay has no type filter; `ctl.*` reaches subscribers only when autowatch runs `--ctl-events`. Data-plane (`doc.changed`/`watch.task.*`/`agent.*`) always relays (bus-spec §4.3). auto-ui forwards whatever arrives.

### Event + Hub (shared)
- `auto-shared/bus/event.go:28-46` — `Event` struct incl. `Host` (`json:"host"`) and `ID` (`json:"id"`, "16 hex, randomly generated", §2.1). `:101-118` `AsNotification()` (method=Type, params=Event).
- `auto-shared/bus/hub.go:30,56` — `Hub.Subscribe(Sink) cancel` / `Broadcast(ev)` (snapshot sinks under RLock, Deliver outside lock; non-blocking).
- **bus-spec §5:** "at-most-once, explicitly lossy"; "Idempotency | Not guaranteed. Each event has a unique `id`, but **no deduplication is performed**." → T8's consumer-side dedup **extends** (does not contradict) this; document the extension in the spec.

### auto-ui Hub wiring + parsing to reuse
- `auto-ui/internal/server/server.go:66` — `hub := bus.NewHub()`; passed to `/api/ws` and `/api/rpc`. **Hub is created inside `server.New`.**
- `auto-ui/internal/server/rpc_ingest.go:51-92` — `/api/rpc` parses `frame{...Params bus.Event}`, validates, `hub.Broadcast(ev)`, then `DeriveDocChanged` + broadcasts derived. **The relay path must reuse this `params → bus.Event` parse but skip the derive** (autowatch already derived).
- `auto-ui/internal/server/ws.go:44,77-79` — WS session subscribes to the Hub as a `bus.Sink`; `session.Deliver(ev)` → `enqueue(ev.AsNotification())`, 16-slot buffer, drop-on-full.
- `auto-ui/internal/cli/serve.go:105-129` — **construction order:** `mgr := backend.NewManager(...)` then `go mgr.Run(baseCtx)` then `server.New(..., WithBackendManager(mgr))`. The Manager is built **before** the Hub exists → favors an `onEvent` callback that `server.New` sets to the (deduped) broadcaster, over reordering to inject the Hub.

### Tests to reuse/extend
- `auto-ui/internal/backend/manager_test.go:20-47` — `fakeBackend` (rpc.Peer over `net.Pipe` with canned `daemon.status`/`project.list` handlers) + `waitResolve` helpers. **Extend:** give the fake a `bus.Hub` + register a `peerSink` on `bus.subscribe`, broadcast a test event, assert it flows Manager→onEvent→auto-ui Hub→WS client.
- `auto-ui/internal/server/ws_test.go` — WS client test helpers (dial, readUntil).

## Patterns
- **Forward verbatim:** relay `OnNotify` handler = parse `req.Params`→`bus.Event`→`onEvent(ev)`; **no `DeriveDocChanged`** (autowatch relays raw+derived). Local `/api/rpc` keeps its derive.
- **onEvent callback wiring:** `Manager` invokes `onEvent(ev)` for each relayed event; `server.New` sets it to a deduping broadcaster wrapping `hub.Broadcast`. Looser coupling than reordering serve.go; trivially unit-testable.
- **Dedup-by-id (consumer-side):** a small TTL/ring set of recently-seen `id`s in front of `hub.Broadcast`, shared by both the `/api/rpc` ingest and the relay forward, so an event reaching the Hub via both paths (T8→T10 window) broadcasts once. Lives in auto-ui (NOT the shared Hub — keeps the documented bus contract unchanged for autowatch).
- **Subscribe-on-connect / re-subscribe-on-redial:** `bus.subscribe` is called inside `connect()` after the handshake, so 042's reconcile redial re-subscribes after a blip. A `bus.subscribe` failure leaves the backend usable for proxied RPCs (relay-degraded), retried on the next tick.
- **Non-blocking:** `onNotify` runs on the read goroutine; `hub.Broadcast` snapshots + each WS sink is drop-on-full, so a slow browser can't wedge a backend's relay.
- **Guard rails:** GR-F3 (all envelope fields incl. `Host` intact through the relay — no field loss), GR-N5 (client-side filtering — SPA filters, no server-side filter, no SPA change in T8), GR-F1 (live refresh now works for remote hosts too). GR-N7 (API-first ~90% via the fake-backend harness).

## Related Tasks
- **Task 040** (event-ingest-relay, MERGED): built the autowatch producer side (`bus.subscribe`, `peerSink`) this task consumes.
- **Task 042** (auto-ui-proxy-backends, MERGED): built the `BackendManager` + live reconcile this task extends; its fake-backend test harness is the foundation for T8's event-flow tests.
- **Task 030** (conformance harness, MERGED): the in-process `net.Pipe` tier pattern (GR-N6) used for the fake backend.
- **Task 10** (hook retarget, FUTURE): removes auto-ui's `/api/rpc` ingest + the hook dual-post, making the relay the **sole** event path. Until then both paths can be live → T8's dedup-by-id guards the window. T8 does **not** touch `/api/rpc` (it stays).

## Git History & Drift Check (CB3)
- **Zero drift** — all Solution/context.md line refs confirmed on current main (post-042): `manager.go` connect()@184-257 (`rpc.NewPeer(netConn)` @**201**), Manager struct@68-75, NewManager@93, Health/BackendHealth@77-83/310-325, conn struct@49-62; `server.go` New@?, `hub := bus.NewHub()`@66, `/api/ws`@107, `/api/rpc`@110, WithBackendManager@36-38; `rpc_ingest.go` handleRPC@27, `hub.Broadcast`@75, derive@82-87; `peer.go` WithOnNotify@72-74, handleNotification@422; `bus/event.go` Event@28-46/AsNotification@112; `bus/hub.go` Subscribe@30/Broadcast@56/SinkCount@44; `subscribe.go` registerSubscribe@45.
- **Last-touched-by:** manager.go/server.go/manager_test.go/doctor.go → `8ea8192` feat(042); rpc_ingest.go/peer.go/subscribe.go → `f3430aa` feat(040); bus/event.go+hub.go → `e3a635b` feat(021). No commits after 042 touch these.
- **doctor.go already has `runBackendDoctorChecks()`** (042, `:104-166`, called from `runDoctorChecks` `:94`) — **extend** it for the relay-degraded check rather than adding a new function.
- **Test harness to extend (not replace):** `manager_test.go` has `fakeBackend{hostID,peer,cancel}` (`:20-47`), `newFakeBackend` (`:28`, registers handlers via `rpc.WithHandler`), `fakeFleet` (`:49-112`, dial interceptor), `waitResolve`/`waitResolveFails` (`:138/:155`). Add a `bus.Hub` + a `bus.subscribe`→peerSink handler to the fake (mirroring `subscribe.go:45-54`). WS helpers `dialWS`/`readUntil` in `ws_test.go:16/:35` reusable for the end-to-end test.
- **Construction order (serve.go ~105-129):** `mgr := NewManager(...)` → `go mgr.Run(baseCtx)` → `server.New(..., WithBackendManager(mgr))` (Hub created inside New@66). Confirms the `SetEventSink` callback + nil-guard choice (D-1).
