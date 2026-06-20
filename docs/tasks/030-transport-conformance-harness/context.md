# Context: Task 030

Codebase grounding for the `auto-shared/transport` + `auto-shared/rpc` libraries and the in-process conformance harness. See [plan.html](plan.html) for requirements, verification, and solution.

## Scope clarification (read first)

`transport` and `rpc` are **library subpackages of the existing `auto-shared` module** — siblings of `bus`, `config`, `git`. They are NOT a new standalone `auto-rpc` binary/module: no `cmd/`, no cobra, no new `go.mod`. This keeps GR-N1 (zero new runtime deps) trivially satisfied — `auto-shared/go.mod` is stdlib-only (`module github.com/mistakenot/auto-shared`, `go 1.26.1`, no `require`) and stays that way.

## Key Files

### Existing JSON-RPC to mirror (do NOT modify — Task 10 retires it)
- `auto-ui/internal/server/rpc.go:33-48` -- canonical `rpcRequest` / `rpcResponse` / `rpcError` struct shapes. `ID json.RawMessage` (so number/string id round-trips), `Result any`, `Error *rpcError`, `Data any`.
- `auto-ui/internal/server/rpc.go:20-26` -- standard error codes: `codeParseError=-32700`, `codeInvalidRequest=-32600`, `codeMethod=-32601`, `codeInvalidParams=-32602`, `codeInternalError=-32603`.
- `auto-ui/internal/server/rpc.go:65-111` -- `dispatch(ctx, raw []byte) (*rpcResponse, bool)`. Handler signature `func(ctx, params json.RawMessage) (any, error)`. Returns `send=false` for notifications (no `id`) and unknown-method notifications. `errors.As` preserves a handler's `*rpcError`; other errors become internal error. Method table is read-only after registration (no lock on dispatch).
- `auto-ui/internal/server/ws.go:33-176` -- the duplex pattern to copy: one `writePump()` goroutine owns all writes (responses + pushes); `readLoop()` dispatches inbound; non-blocking enqueue with drop-on-full; per-conn outbound buffer. `session` implements `bus.Sink`; `Deliver(ev)` enqueues `ev.AsNotification()`.
- `auto-ui/internal/server/rpc_ingest.go:27-92` -- HTTP `/api/rpc` ingest shape (notification frame `{jsonrpc, method, params: bus.Event}`). Reference only; not in this task.

### Transport/listener idioms to follow
- `auto-ui/internal/cli/serve.go:126-141` -- `net.Listen("tcp", addr)`, then `ln.Addr().String()` to discover an OS-assigned port (`--port 0`), and the `--ready-file` JSON line `{"addr":"127.0.0.1:NNNN"}\n`. **Note:** the ready-file/`--rpc-addr` CLI surface belongs to Task 3 (autowatch), not here; this task just needs the listener idiom.
- `auto-ui/internal/cli/serve.go:66-120` -- graceful shutdown: `signal.NotifyContext`, cancel → `Shutdown()` with timeout, `done` channel closed after drain. Our `Listener.Close()` should unblock `Accept()` cleanly the same way.
- No existing `net.Pipe` or unix-socket usage anywhere in the repo — this task introduces both. `net.UnixListener`/`net.Dial("unix", path)` from stdlib.

### Bus envelope the rpc notification channel will carry (Task 6)
- `auto-shared/bus/event.go:28-46` -- `Event` struct (now includes required `Host string`).
- `auto-shared/bus/event.go:101-118` -- `Notification{JSONRPC, Method, Params Event}` and `(e Event) AsNotification()`. The wire shape for a pushed event is exactly this: `{"jsonrpc":"2.0","method":"<type>","params":{...event...}}`. Our `rpc` notification frame must be byte-compatible with this so Task 6 plugs in with no reshaping.
- `auto-shared/bus/hub.go:6-8` -- `Sink interface { Deliver(Event) }`; `Hub.Subscribe(Sink) (cancel func())`, `Hub.Broadcast(ev)` snapshots under RLock and delivers outside the lock.

### Validation pattern (if rpc/transport surface validation)
- `auto-shared/config/validation.go:4-12` -- `ValidationError{Code, Path, Field, Message, Value}` array pattern, per CLAUDE.md. JSON-RPC errors are a different shape (`{code,message,data}`), so this is only relevant if we validate addresses/URIs.

## Patterns

- **auto-shared subpackage style** (`bus/event.go:1-6`, `git/detect.go:1-8`): a package doc comment stating the single responsibility; exported core types top-level; `New*` constructors; behavior as methods; lowercase private helpers; white-box `_test.go` in the same package; table-driven tests with test-local fixtures and interface-implementing test helpers (`bus/hub_test.go:30-60` `collectSink`).
- **NDJSON framing** (the chosen format): `json.NewEncoder(conn)` / `json.NewDecoder(conn)` stream newline-delimited JSON natively over any `io.ReadWriteCloser`. `json.Decoder` tolerates the trailing newline and reads one value at a time — no manual buffering. Encoder appends `\n` per `Encode`. This is the whole framing layer.
- **Layered API** (CLAUDE.md "API design for CLI tools"): low-level `Listener`/`Dialer`/`Conn` primitives (transport) ↔ JSON-RPC codec + dispatcher (rpc) ↔ method handlers (downstream tasks). Each layer testable in isolation; handlers never import transport (GR-N3 — enforceable by a grep test).
- **Conformance harness lessons** (`docs/tasks/029-auto-ui-state-refactor/feedback.md`): "a poll-to-settle is not an assertion" — assert the actual observable outcome (a received frame / a counter delta), not that something settled. Wire counters into fake adapters (`acceptCount`, read/write counts, `dispatchedMethodCount`). Use `go test`/`net.Pipe` for hermetic, port-free, race-free wiring. Gate deps with `git diff --quiet -- go.mod`.

## Constraints (from epic 003 guard rails / seams)

- **Seam #1** (`docs/epics/003-multi-host-architecture/epic.html`): `Listener`/`Dialer`/`Conn` is the abstraction between JSON-RPC handlers and the wire; every transport (unix, tcp, future ws/iroh) satisfies it; **handlers never import a transport package**.
- **GR-N3**: transport-agnostic RPC — codec/dispatcher operate on decoded messages, not the transport.
- **GR-N6**: two-tier harness. This task ships the **in-process (`net.Pipe`) tier + reusable scenario interface + fake/stub adapters** only; the binary tier (subprocess + `--ready-file`) is deferred to Task 3 (no RPC binary exists yet — resolved decision in plan.html Open Questions).
- **GR-N7**: API-first — the harness is the primary coverage layer, exercising the RPC API directly.
- **GR-N1**: stdlib + already-imported libs only.
- **Bus delivery** (`docs/auto-bus-spec.md` §5): at-most-once, lossy, no acks/replay/ordering-across-producers. The rpc notification channel inherits this — drop-on-full, never block the producer (mirror `ws.go`).

## Related Tasks

- **Task 1 / task 028** (merged): added required `Host` field to `bus.Event` + `config.HostIDQuietly()`. The notification frame this task defines must carry the full envelope incl. `Host`.
- **Task 3** (next, depends on this): plugs the real autowatch JSON-RPC listener into the harness; owns the `--rpc-addr` / `--ready-file` CLI surface and the binary-tier harness.
- **Task 6**: relays `bus.Event`s over the rpc notification channel (`bus.subscribe`) — why duplex + id-less notifications are required from day one.
- **Task 7 / Task 10**: auto-ui proxy adopts the `rpc` client; Task 10 retires `auto-ui/internal/server/rpc*.go` and `/api/rpc`. Until then, leave that code untouched.

## Git-history notes (enrichment)

- **Commit granularity** — `auto-shared/bus` (task 021, commit `e3a635b`) and task 028 (`e3a3d18`) both land **code + tests together, one commit per phase**. Mirror that: each phase here commits its implementation and its `_test.go` together; `go build ./...` + `go test -race ./...` green before the commit (CLAUDE.md Go discipline).
- **`params.type` is authoritative over frame `method`** (task 021 `feedback.md`) — for an event notification `{jsonrpc, method, params:Event}`, the inbound `method` is advisory; `params.type` is the real type. Our codec must not lose either field on round-trip (AC-5); routing-on-`method` vs `params.type` is a Task 6 decision, not this task's — we just preserve both.
- **No path drift** — all Solution/context file:line refs verified current against HEAD: `auto-ui/internal/server/rpc.go` (codes 20-26, structs 33-48, dispatch 65-111), `ws.go` (33-176), `auto-shared/bus/event.go` (Event 28-46 *with* `Host`, `AsNotification` 101-118), `bus/hub.go` (6-8), `config/validation.go` (4-12), `auto-ui/internal/cli/serve.go` (listen+ready-file 126-141).
- **Ready-file + graceful shutdown idiom** (`serve.go:62-120,126-141`) is owned by **Task 3**, not this task — noted so the listener interface here stays compatible with it (clean `Close()` → `Accept()` unblock).
