# Context: Task 040 — autowatch event ingest + relay plumbing

File:line grounding for the delta over Task 3 (031). This task **extends 031, it does not rebuild it**: it adds the `bus.subscribe` hub→connected-peer bridge (031 deferred it) and extends the existing ingest handler to derive + stamp hostId. See [plan.html](./plan.html).

## Key Files — the seam 031 left open

- `auto-shared/bus/hub.go:5-8` — `Sink interface { Deliver(Event) }`. Subscribers implement this.
- `auto-shared/bus/hub.go:30-40` — `Hub.Subscribe(s Sink) (cancel func())` registers a sink and returns a deregister cancel that is safe to call multiple times.
- `auto-shared/bus/hub.go:48-59` — `Hub.Broadcast(ev Event)` snapshots the sink set under RLock, then calls `entry.sink.Deliver(ev)` **synchronously** per sink outside the lock. Doc comment notes the auto-ui session's `Deliver` uses a non-blocking channel send with drop-on-full so the hub never blocks on a slow client. **No `rpc.Peer` is ever subscribed today** — only test sinks (031) and the auto-ui websocket session.
- `auto-shared/rpc/peer.go:384-410` — `Peer.Notify(method string, params any) error` — server→client push (id-less notification). Returns `ErrClosed` after shutdown.
- `auto-shared/rpc/peer.go:416-431` — `Peer.enqueue` offers the frame to a bounded channel (`defaultBufferSize=16`, peer.go:17); **on full it drops the frame, calls `shutdown()` (closes the conn), and returns false** → `Notify` returns `ErrClosed`. This is the at-most-once / drop-on-full primitive — the bridge gets it for free.
- `auto-shared/bus/event.go:112-118` — `Event.AsNotification()` returns `Notification{JSONRPC:"2.0", Method: e.Type, Params: e}`. The wire shape for a relayed event: method = the event type, params = the full envelope. The bridge sink does `peer.Notify(ev.Type, ev)`.

## Key Files — the accept loop (where the bridge must hook in)

- `auto-watch/internal/rpcserver/server.go:33-41` — `New(ln, h, hub, ctlEvents)`; the `Server` already holds `hub *bus.Hub`, so no new constructor arg is needed for `bus.subscribe`.
- `auto-watch/internal/rpcserver/server.go:71-72` — accept loop: `peer := rpc.NewPeer(conn)` then `s.handlers.Register(peer)`. **This is where the `*rpc.Peer` exists** — a plain `rpc.Handler(ctx, params)` has no peer handle, so the peer-bound `bus.subscribe` must be registered here, closing over `peer` + `s.hub`.
- `auto-watch/internal/rpcserver/server.go:85-98` — the `wg.Go` teardown closure: after `peer.Serve(ctx)` returns it does `delete(s.peers, peer)` then (gated) broadcasts `ctl.disconnect`. **This is the exact spot to fire the `Hub.Subscribe` cancel** so a disconnected peer leaves no leaked sink.
- `auto-watch/internal/rpcmethods/methods.go:52-54` — `Handlers.Register(p *rpc.Peer)` (registers `daemon.status`). `Handlers` is a **single shared instance** registered onto every peer, so per-connection subscription state (the cancel func) must NOT live here — it belongs in the per-conn accept-loop scope.

## Key Files — ingest (validate+ack today; gains derive + hostId)

- `auto-watch/internal/rpcserver/ingest.go:22` — `HookIngest(hub *bus.Hub, ctlEvents bool) http.Handler`. Signature must grow a `hostID string` + a registry provider.
- `auto-watch/internal/rpcserver/ingest.go:73-77` — parses `frame.Params` into `ev` and runs `ev.Validate()` → 400 on failure.
- `auto-watch/internal/rpcserver/ingest.go:79-82` — `hub.Broadcast(ev)` of the **raw** event only. **No `DeriveDocChanged`, no hostId stamp.** This is the additive gap to close.
- `auto-watch/internal/rpcserver/ingest.go:84-92` — gated `ctl.log.info` `hook.ingested` emission.
- `auto-shared/bus/event.go:72-95` — `Event.Validate()` **requires `Host` non-empty** (event.go:95). Inbound frames must already carry a Host, so stamping is an authoritative **overwrite**, not a fill-if-empty (validation already rejects empty).
- `auto-shared/bus/event.go:51-67` — `NewEvent` sets `ev.Host = config.HostIDQuietly()` (event.go:59) — the hook producer already stamps the local host at creation.

## Key Files — derivation + the auto-ui analog

- `auto-shared/bus/derive.go:14` — `DeriveDocChanged(ev Event, reg config.ProjectsConfig) []Event` — only derives for `agent.tool.post` events whose project is registered; emits `doc.changed` per `docs/**/*.{md,html}` path.
- `auto-shared/bus/derive.go:76-87` — `newDerived` copies provenance incl. `ev.Host = src.Host` (derive.go:79). So if ingest stamps `ev.Host` **before** deriving, derived events inherit the correct host.
- `auto-ui/internal/server/rpc_ingest.go:25` — `handleRPC(hub, regProvider func() config.ProjectsConfig, buf)` — the registry is read **per request** via a provider func.
- `auto-ui/internal/server/rpc_ingest.go:75-88` — the pattern to mirror: `hub.Broadcast(ev)`, then `derived := bus.DeriveDocChanged(ev, regProvider())`, then `hub.Broadcast(derived[i])` for each.
- `auto-ui/internal/server/ws.go:77-79` — the hub→wire analog: `Deliver(ev)` calls `s.enqueue(s.ctx, ev.AsNotification())`.
- `auto-ui/internal/server/ws.go:85-95` — `enqueue` does a non-blocking channel send; on full it calls `s.cancel()` (drops the connection) — the slow-client policy the peer-sink mirrors.

## Wiring already present in start (ops.go)

- `auto-watch/internal/cli/ops.go:104-106` — `hub := bus.NewHub()`, `hostID := sharedconfig.HostIDQuietly()`, `handlers := rpcmethods.New(hostID, ...)`. The `hostID` is already in scope to thread into `HookIngest`.
- `auto-watch/internal/cli/ops.go:161` — `rpcSrv := rpcserver.New(rpcLn, handlers, hub, ctlEvents)` — already passes the hub; the `bus.subscribe` bridge is internal to `rpcserver.Serve`, so this line is unchanged.
- `auto-watch/internal/cli/ops.go:166` — `Handler: rpcserver.HookIngest(hub, ctlEvents)` — must become `HookIngest(hub, hostID, regProvider, ctlEvents)`.
- `auto-watch/internal/config/global.go:11-15` — `config.LoadProjects(ProjectsPath())` / `EnsureGlobalConfig` load the shared `~/.auto/projects.json` registry — the source for the per-ingest registry provider.

## Patterns / constraints

- **GR-N3 layering:** the `bus.subscribe` bridge (peer sink calling `Peer.Notify`) lives in `rpcserver` (which already imports `transport` + `rpc`). `rpcmethods` stays transport-free and is unchanged for this task — `layering_test.go` (031) still holds.
- **At-most-once / lossy delivery** (auto-bus-spec §5) — no durability is added; the relay is in-transit only.
- **`--ctl-events` gate is at emission (031):** `ctl.*` is `Broadcast` onto the hub only when `ctlEvents` is true (server.go:78, methods.go:74, ingest.go:85). A bridge that relays *everything on the hub* therefore relays `ctl.*` exactly when the flag is on, and data-plane events always — no bridge-side ctl filter required.

## Related Tasks

- **Task 3 (031, MERGED):** built the RPC listener, the validate-and-ack `HookIngest`, the in-process `bus.Hub`, and gated `ctl.*` emission. Decision D-31-3 + the resolved AC-8 thread explicitly deferred the hub→peer bridge to this task.
- **Task 1 (028, MERGED):** added the `Host` envelope field (GR-F3).
- **Task 8 (later):** auto-ui will *call* `bus.subscribe` on each backend and merge relayed events into its own hub. **Task 10 (later):** retargets `auto hooks fire` to autowatch-only and removes auto-ui's `/api/rpc`.
