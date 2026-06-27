# Context: Task 041

Codebase + docs grounding for [plan.html](./plan.html) — data-plane backbone assurance hardening (race-detector CI gate, transport liveness, conformance depth).

## Key Files

### RPC peer concurrency core (G2 lives here)
- `auto-shared/rpc/peer.go:48-74` — `Peer` struct: `conn io.ReadWriteCloser` (note: **not** `net.Conn`, so `SetReadDeadline` is not available on the field type), `out chan any` (bounded write-pump, default 16 via `WithBufferSize`), `pendingMu sync.Mutex` + `pending map[int64]chan Response`, `closed chan struct{}`, `closeOnce sync.Once`, `pumpDone chan struct{}`.
- `auto-shared/rpc/peer.go:26-42` — Options pattern: `WithHandler`, `WithOnNotify`, `WithBufferSize`. **This is where a `WithKeepAlive(interval, timeout)` option hooks in.**
- `auto-shared/rpc/peer.go:147-188` — `readLoop`: `dec := NewDecoder(p.conn)`; `dec.Decode(&raw)` **blocks indefinitely — no read deadline**. The only unblock is ctx cancellation → a goroutine calls `p.shutdown()` (peer.go:118-124) which closes the conn, failing the Decode. **This is the gap:** a half-open connection never unblocks `readLoop`, so `Serve` never returns.
- `auto-shared/rpc/peer.go:318-381` — `Call`: registers a buffered waiter `ch := make(chan Response, 1)` in `pending`; released on one of three terminal paths — response on `ch`, `<-ctx.Done()` (removes only that waiter), or `<-p.closed` (ErrClosed).
- `auto-shared/rpc/peer.go:473-488` — `shutdown` (idempotent via `closeOnce`): `close(p.closed)`, then snapshots `pending`, replaces with empty map, and `close(ch)` for each waiter → pending callers get `ok=false` → ErrClosed. **No leak by construction once shutdown runs.**

### Codec / framing (G3 decoder test)
- `auto-shared/rpc/codec.go:8-39` — NDJSON via stdlib `json.Encoder`/`json.Decoder`. `Decode` buffers internally and yields exactly one JSON value per call regardless of how bytes are chunked. Framing is a `json.Decoder` property, **not** transport-dependent (consistent with GR-N8 §4).
- `auto-shared/rpc/message.go:27-124` — `Request`/`Response`/`Error` types + `Classify` (validates `jsonrpc=="2.0"`, routes by presence of method/result/error/id).

### Conformance harness (G3 lives here)
- `auto-shared/rpc/conformance/conformance.go:21-80` — interfaces: `RPCClient` (`Call`/`Notify`/`Notifications() <-chan rpc.Request`), `Observations` (`DispatchCount`), `Fixture` (`Client()`/`Obs()`/`Close()`), `FixtureFactory func(testing.TB) Fixture`, `Scenario` (`Name()`/`Run(t, f)`). `RunAcrossFixtures` runs a scenario as a subtest per factory (pipe/unix/tcp).
- `auto-shared/rpc/conformance/fakes.go:135-194` — `CountingConn` / `CountingListener` are **observational only** (count Read/Write/Accept). **They cannot inject faults** (no close-mid-stream, no blocking write). Fault injection is net-new for G3.
<!-- RESOLVED(P2): "fault-injection seam" wording
REVIEW: The bullet says "CountingConn fault-injection seam already exists ... but is unused". I read fakes.go:134-157: the type only does counting in its Read/Write; no fault injection API exists at all. Same inaccuracy appears in plan.html. The wrapper *pattern* can be extended, but the seam for faults does not pre-exist.
AUTHOR: The context.md bullet itself was already accurate ("CountingConn / CountingListener are observational only ... They cannot inject faults ... Fault injection is net-new for G3") — the inaccurate "seam already exists" wording lived in plan.html (Problem §G3) and has been corrected there. No change needed to this bullet; resolving.
-->
- `auto-shared/rpc/conformance/fakes.go:239-386` — `pipeFactory` (`net.Pipe()`), `unixFactory` (unix socket + temp dir), `tcpFactory` (127.0.0.1:0). All build a server+client `Peer` pair, both `Serve` in goroutines, cancel on `Close`.
- `auto-shared/rpc/conformance/conformance_test.go:19-96` — the **single** existing scenario `echoAndPushScenario` (echo round-trip + server push + dispatch counts). G3 adds scenarios alongside it.

### Existing peer tests (patterns to mirror)
- `auto-shared/rpc/peer_test.go:19-47` — `newPipePeers(t, handlers, clientOpts, serverOpts)` helper: connected pair over `net.Pipe`, both Serve in background, returns peers + cancel + err chans.
- `peer_test.go:379-410` `TestStalledReaderTriggersDropAndClose` — stalls by **not** reading the conn, hammers `Notify` until buffer overflows → `ErrClosed`. (Template for slow-consumer scenario.)
- `peer_test.go:412-446` `TestPendingCallReturnsOnEOF` — Call with no responder; close remote end → Call returns `ErrClosed` promptly. (Template for connection-drop scenario.)
- `peer_test.go:936-985` `TestConcurrentCallsNoCrosstalk` — 20 concurrent Calls, identity handler, assert each gets its own idx back. (Template for concurrent-correlation scenario.)

### Server-side relay (G2 dead-peer leak surfaces here)
- `auto-watch/internal/rpcserver/server.go:71-102` — accept loop: `rpc.NewPeer(conn)`, `handlers.Register(peer)`, `registerSubscribe(peer, hub, sub)`, track in `peers` map; Serve goroutine calls `peer.Serve(ctx)` then `sub.teardown()` then removes from map and broadcasts `ctl.disconnect`. **`teardown` runs only after `Serve` returns** — so the leak is caused by `Serve` never returning (the G2 gap), not by missing teardown. Harden: make `sub.teardown()` a `defer`.
- `auto-watch/internal/rpcserver/subscribe.go:15-54` — `peerSink.Deliver` → `peer.Notify(ev.Type, ev)` (non-blocking, ignores ErrClosed); `registerSubscribe` subscribes the sink to the hub and stores the cancel; `subscription.teardown()` is idempotent.

### Test / CI wiring (G1 lives here)
- `Makefile:15` — `PROJECTS := auto-doc auto-env auto-etl auto-watch auto-search auto-reflect auto-skill auto-graph auto-ui auto-config auto-cli` (note: `auto-shared` is **not** in PROJECTS).
- `Makefile:128-132` — `test:` target loops `PROJECTS` running `go test ./...`. **No `-race` anywhere** in the Makefile.
- `.github/workflows/ci.yml:32-33` — single test step `run: make test`. Go 1.26, ubuntu-latest.
- `go.work:3-16` — 12 modules incl. `auto-shared`. Multi-module workspace; a race target must `cd` into each target module.
- **Constraint:** the Go race detector requires `CGO_ENABLED=1` + a C compiler. ubuntu-latest has gcc; local dev may not — `test-race` must not be a hard dep of plain `test` if it would break cgo-less envs.

## Patterns
- **Terminal-shutdown / race ACs already specified by Task 030** (`docs/tasks/030-transport-conformance-harness/plan.html` AC-4): single write-pump goroutine owns all writes; every pending `Call` returns *promptly on a bounded timeout* (not on an unrelated caller deadline) for each of EOF / ctx-cancel / overflow-drop / explicit-Close; **"a poll-to-settle is not an assertion"** — assert an observable (frame, error code, counter delta), never quiescence. Task 041 extends this, not re-does it.
- **Drop-on-full / at-most-once is application policy**, not transport (bus-spec §5, GR-N8). Slow subscriber → connection dropped, hub never blocks. New scenarios must assert this exact semantic.
- **Liveness model precedent:** auto-ui WebSocket pushes a `ping` every 1s with a 16-slot buffer + slow-client drop (bus-spec §4.2). The new RPC-transport keepalive should mirror this model at the `rpc.Peer` layer.
- **Opt-in options:** all peer behavior is via `With*` options; keepalive should be opt-in so existing `net.Pipe` tests and current callers are unaffected, with autowatch's server enabling it explicitly.
- **Guard rails:** GR-N1 (zero new runtime deps — stdlib only), GR-N3 (handlers stay transport-agnostic), GR-N6 (in-process conformance tier is the right home for G3; binary tier is for autowatch↔auto-ui, out of scope here), GR-N7 (API-level coverage), GR-N8 (connection-oriented only — deletes unreliable-transport test classes).

## Related Tasks
- **Task 030** (transport-conformance-harness, MERGED): built the `Listener`/`Dialer`/`Conn` abstraction + the conformance scaffold (`RunAcrossFixtures`, fixtures, fakes) this task deepens. Established the race-free + terminal-shutdown ACs.
- **Task 031** (autowatch-rpc-listener, MERGED): stood up the daemon listener + accept loop + binary-tier fixture (`TestMain` builds `./cmd/autowatch`). "Listener health = daemon health" (bind failure → non-zero exit). Deferred the hub→peer bridge to 040.
- **Task 040** (autowatch-event-ingest-relay, MERGED): added `bus.subscribe` + `peerSink` relay + host-stamping. The slow-subscriber drop-on-full path this task must conformance-test lives here.

## Git History & Drift Check (CB3)
- **No drift:** all context.md line references confirmed current (peer.go struct@48, Serve@106, readLoop/Decode@147-151, Call@318, shutdown@473; conformance.go Fixture@40/FixtureFactory@51/Scenario@55/RunAcrossFixtures@65; fakes.go CountingConn@135, factories@239/263/327; server.go Serve@45, peer.Serve@88 → sub.teardown@90).
- **Last-touched-by:** `auto-shared/rpc/*` + `conformance/` → `233f7cd` feat(030); `rpcserver/server.go`+`subscribe.go` + `bus/hub.go` → `f3430aa` feat(040); `Makefile` → `e712ce8`; `ci.yml` → `79c4382`.
- **`hub.SinkCount()` already exists** — `auto-shared/bus/hub.go:44-48` (`RLock`; `len(h.sinks)`), added in feat(040) for test observability. AC-4's dead-subscriber-reap test uses it directly; **no `subscribe.go` change needed.**
- **Keepalive precedent to mirror:** `auto-ui/internal/server/ws.go:119-135` — `const pingInterval = time.Second`, ticker in a dedicated goroutine, `s.notify(ctx, "ping", {seq,ts})` per tick, drop-on-full via `s.cancel()` on enqueue overflow. The RPC-layer keepalive mirrors this shape (Notify already enqueues + drops on full).
- **Makefile gate targets** all iterate `PROJECTS` (line 15): `test:@128`, `vet:@104`, `lint:@73`, `vulncheck:@111`, `fmt-check:@63` — so adding `auto-shared` to `PROJECTS` extends *all* of them (the basis for the D-2 "may surface pre-existing findings" risk).
- **Feedback lessons:** 030 — write-pump serialization is the `-race` guarantee; shutdown is drain-first; the fixture seam "write once, run across 3 transports" is the model 041 extends. 040 — per-peer subscription state lives in `rpcserver` accept-loop scope, not `rpcmethods` (keeps handlers transport-free, GR-N3).
