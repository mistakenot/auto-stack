---
hash: "f4b9db9d"
id: "7b8cda70"
read_when: "implementing or consuming bus events, adding a new event type, understanding the wire format or delivery guarantees"
summary: "The auto-bus standard: CloudEvents-shaped envelope, JSON-RPC 2.0 framing, HTTP and WebSocket transport bindings, at-most-once delivery contract, dotted event-type registry, and watch.task.* paper mapping."
title: "Auto Bus Specification"
---

# Auto Bus Specification

**Version:** 1.0
**Status:** Implemented (v1)

AC-1 coverage: Envelope | Framing | Bindings | Contract | Type registry | ctl.* events | watch.task.* mapping

---

## 1. Overview

The auto bus is the unified communication standard for all auto-stack components. It provides streaming push events and two-way JSON-RPC over pluggable transports, replacing the ad-hoc, incompatible communication paths that existed previously.

The bus is implemented as the `auto-shared/bus` Go package, hosted inside the auto-ui server (which owns the HTTP port and WebSocket layer). Producers publish events via HTTP; the hub broadcasts to all connected WebSocket clients; consumers filter client-side.

---

## 2. Envelope

Every bus event is a self-contained envelope carrying metadata, workspace provenance, and an opaque data payload. The envelope is a **strongly typed Go struct** (`bus.Event`) -- the stable contract every component compiles against.

### 2.1 Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `specversion` | string | yes | Always `"1.0"`. |
| `type` | string | yes | Dotted hierarchical event type (e.g. `agent.tool.post`). Must match `^[a-z0-9]+(\.[a-z0-9]+)+$`. |
| `source` | string | yes | Producer identity (e.g. `auto/hooks/claude`, `auto/bus/derive`). |
| `id` | string | yes | Unique event identifier (16 hex characters, randomly generated). |
| `time` | string | yes | RFC 3339 UTC timestamp of event creation. |
| `host` | string | yes | Host identity of the producing machine (via `config.HostIDQuietly()`). The autowatch daemon overwrites this with its own hostId on ingest (overwrite-always). |
| `project` | string | no | Registry project ID (e.g. `auto-stack`). Resolved by the producer via `FindProjectByRemote` or `FindProjectByPath`. |
| `session` | string | no | Agent session identifier, when available. |
| `remote` | string | no | Normalized git remote URL. See credential rule below. |
| `branch` | string | no | Current git branch name. |
| `worktree` | string | no | Absolute worktree root path on the producing host. |
| `commit` | string | no | HEAD commit SHA at event time. |
| `data` | JSON | no | Opaque payload. Typed only where the bus authors it (see section 2.3). |

**Exception — derived event ids are deterministic.** Events minted by `bus.DeriveDocChanged` (`source` = `auto/bus/derive`) do **not** carry a random `id`. Their `id` is a deterministic hash of the source event's `id`, the derived `type`, and the derived path (`sha256(srcID:type:path)`, first 8 bytes hex). Non-derived events keep the randomly-generated `id` above. The deterministic id was introduced (task 045) so that when independent sites derived the same `doc.changed` from one source event — back when auto-ui's local `/api/rpc` ingest derived in parallel with autowatch's relay — both minted an **identical** `id`, letting a dedup-by-`id` consumer collapse the duplicate. Task 047 removed auto-ui's local ingest, leaving **autowatch as the sole derive site**, so this collision property is no longer load-bearing for dedup; it remains a stable, reproducible id for derived events.

### 2.2 Workspace provenance attributes

Events carry flat provenance attributes so any consumer can locate the physical file and identify the logical workload:

| Attribute | Purpose | Scope |
|-----------|---------|-------|
| `remote` | Machine-independent repo identity (stable join key) | Cross-host |
| `branch` | The feature/workload identifier | Cross-host |
| `worktree` | The workspace on this host (locates files) | Host-local |
| `commit` | Point-in-time snapshot of HEAD | Cross-host |
| `project` | Registry project ID (logical grouping) | Host-local (registry) |

The combination `(remote, branch)` identifies a workload across hosts. The `worktree` attribute locates files on the producing host. Downstream, ETL reconciles `session -> commit -> branch` as the canonical record; the bus makes `(remote, branch, worktree)` available in real time.

**Known limitation:** `worktree` and absolute paths in `data` are host-local values. Cross-host workspace reconciliation (e.g. a multi-host UI) is out of scope for v1 -- `remote` + `branch` serve as the cross-host key.

### 2.3 Typing discipline

The envelope follows a two-tier typing model:

- **Envelope: strongly typed.** The `Event` struct is a validated Go type with compile-time field guarantees. Every component compiles against this contract. `Validate()` enforces required fields (`specversion`, `type`, `source`, `id`, `time`), dotted type format, and RFC 3339 time format.

- **Data payload: typed where authored, opaque where carried.** Hub-derived events (`doc.changed`) and RPC results (`doc.list`, `doc.get`) use typed Go structs (`DocChanged`, `docEntry`). Ingested agent hook bodies stay opaque (`json.RawMessage`) -- they round-trip verbatim because per-agent/per-tool schemas are complex and the bus is lossy. The hub parses only the normalized fields it needs (e.g. `ToolPost.Paths` for `docs/**` matching).

For tool events, the data payload is **two-layer**:
- **Normalized fields** (`tool`, `event`, `paths[]`) present a single cross-tool interface that all consumers code against.
- **Raw field** (`raw`, `json.RawMessage`) preserves the agent's original `tool_input` verbatim -- no fidelity loss, each agent's schema differs.

### 2.4 Credential rule: `remote` must be normalized

**Hard requirement:** The `remote` attribute must always be the output of `git.NormalizeRemoteURL()` before it enters the envelope. The raw `git remote get-url origin` value can embed credentials (e.g. `https://x-access-token:github_pat_...@github.com/owner/repo.git`), and the bus broadcasts every event to all connected clients. Emitting the raw value would publish a secret.

Normalization strips credentials, converts SSH to HTTPS, removes trailing `.git`, and lowercases the hostname, producing a stable canonical form like `https://github.com/owner/repo`.

The `FindProjectByRemote` registry lookup normalizes both sides (the stored registry remote may also be raw/token-bearing), so the join works regardless of which form the registry stores.

---

## 3. Framing

### 3.1 JSON-RPC 2.0 notifications

An event on the wire is a **JSON-RPC 2.0 notification** (no `id` field), identical in both directions (inbound publish and outbound broadcast):

```json
{
  "jsonrpc": "2.0",
  "method": "agent.tool.post",
  "params": {
    "specversion": "1.0",
    "type": "agent.tool.post",
    "source": "auto/hooks/claude",
    "id": "a1b2c3d4e5f67890",
    "time": "2026-06-11T10:30:00Z",
    "project": "auto-stack",
    "remote": "https://github.com/owner/repo",
    "branch": "main",
    "worktree": "/home/user/src/repo",
    "commit": "abc1234",
    "data": { "tool": "Edit", "event": "PostToolUse", "paths": [...], "raw": {...} }
  }
}
```

- `method` = the dotted event type. Client-side `on("doc.changed", ...)` works directly against this field.
- `params` = the full self-contained bus envelope.

### 3.2 RPC calls

Two-way RPC (`doc.list`, `doc.get`, `ping`) uses standard id-bearing JSON-RPC 2.0 requests/responses over WebSocket:

```json
{"jsonrpc": "2.0", "id": 1, "method": "doc.list", "params": {"project": "auto-stack"}}
```
```json
{"jsonrpc": "2.0", "id": 1, "result": [{"id": "docs/foo.md", "path": "docs/foo.md"}]}
```

### 3.3 Type authority rule

The inbound frame carries both `method` (JSON-RPC level) and `params.type` (envelope level). **`params.type` is authoritative.** The inbound `method` is advisory. The broadcast re-emits via `ev.AsNotification()` using `ev.Type`, so whatever the producer puts in `method` is overwritten on broadcast. The ingest handler validates the envelope's `type` field; it does not enforce that `method` agrees.

### 3.4 HTTP deviation: error responses for notifications

Per JSON-RPC 2.0 section 4.1, a notification (no `id`) must not receive a response. The hook-ingest HTTP endpoint (autowatch's hook-ingest — section 4.1) **deliberately deviates** from this rule: malformed or invalid frames receive an HTTP 400 response with a JSON-RPC error body. This is justified for an HTTP one-shot binding where the producer needs feedback on structural errors (the producer has no other channel to learn its payload was rejected). Valid frames receive HTTP 204 No Content.

On WebSocket, the standard rule holds: notifications dispatched via the shared `Dispatcher` return no response (the dispatch function returns `nil, false` for id-less frames).

---

## 4. Transport bindings

### Transport model and assumptions (guard rail GR-N8)

**GR-N8 — Connection-oriented, reliable, in-order transports only.** Every transport binding MUST be a connection-oriented, reliable, in-order byte stream: TCP, Unix-domain `SOCK_STREAM`, in-process `net.Pipe()`, and WebSocket (which rides TCP) all qualify. Datagram / lossy / unordered transports (UDP, QUIC datagrams, raw message buses with at-most-once *byte* delivery) are **out of scope** and MUST NOT be added without revisiting the framing and correlation layers.

What this assumption lets us *not* build (and why the code is correct to omit it):

| Concern | Why we ignore it |
|---------|------------------|
| Sequence numbers / reorder buffers | The stream guarantees in-order bytes. |
| Acks / retransmit / NAK | No bytes are lost on a live connection. |
| Byte-level dedup | No byte is duplicated. |
| Corruption / bit-flip detection | TCP/UDS checksum the stream. |

**Sharp edge — reliable+in-order is a *byte* guarantee, not a *message* guarantee.** A single `Read()` may return a partial frame or several coalesced frames. This is handled by the codec, not by assumption: `rpc.Decoder` wraps `json.Decoder`, a streaming tokenizer that buffers internally and yields exactly one JSON value per `Decode()` regardless of how bytes were chunked (see section 3, Framing). No application-level framing logic is needed *because of* this choice — but the choice is load-bearing, so it is recorded here as a guard rail.

**Deployment context (the reason this is safe).** In v1, all cross-host networking happens *within a single cloud VPC* (or a tailnet overlay). Connections are low-latency and highly reliable; there is no public-internet packet loss to design around. This is the explicit justification for not over-building the network layer — we are not hardening against an adversarial or lossy WAN. If the deployment topology ever spans untrusted/high-loss links, this assumption (and the lossy delivery contract in section 5) must be re-evaluated.

**What this does *not* excuse.** Reliable, in-order, intra-VPC delivery says nothing about the *connection staying up* or the *application keeping pace*. These failure modes are fully present on TCP and are handled at the application layer, not assumed away: connection drop / half-open mid-call (pending callers are released — section 5), backpressure / slow consumer (drop-on-full — section 4.2 / 4.3), and concurrent call correlation (the `pending` map). At-most-once delivery (section 5) is an *application* policy delivered by drop-on-full, **not** an inheritance from the transport — TCP is reliable; the relay deliberately is not.

### 4.1 HTTP POST to the autowatch hook-ingest (one-shot publish)

The single ingest endpoint for fire-and-forget event publishing. Hosted by the **autowatch daemon**
(`auto-watch/internal/rpcserver/HookIngest`), mounted as the **root handler** of its hook server, so
producers POST to `http://<hookAddr>/` (any path; default `127.0.0.1:7787`, discoverable from the
daemon's PID metadata). `auto hooks fire` is the producer.

> **Task 047:** this endpoint was previously auto-ui's `POST /api/rpc`. auto-ui no longer hosts a
> local ingest — hooks post to autowatch, which is now the **sole** ingest and derive site; auto-ui
> receives events only over the backend relay (`bus.subscribe`, task 045).

| Aspect | Detail |
|--------|--------|
| Path | Root `/` (any path) on the daemon's hook server |
| Method | `POST` only (405 on others) |
| Content-Type | `application/json` (else 415) |
| Origin | Must be **absent** — a present `Origin` header is rejected (403, anti-CSRF) |
| Auth | Loopback only (`127.0.0.1`/`::1`/`localhost`, else 403); remote access via Tailscale serve |
| Body | A single JSON-RPC notification (see section 3.1) |
| Body limit | 1 MiB |
| Success | `204 No Content` (no body) |
| Error | `400 Bad Request` + JSON-RPC error body (see section 3.4) |

**Processing pipeline:**
1. Reject non-POST (405), non-loopback (403), browser-`Origin` (403), non-JSON `Content-Type` (415).
2. Parse JSON-RPC notification frame; validate `jsonrpc` is `"2.0"`.
3. Extract `params` as `bus.Event`; `params.type` is authoritative. Run `ev.Validate()` -- reject on failure (400).
4. Stamp `ev.Host` with the daemon's hostId (overwrite-always).
5. `hub.Broadcast(ev)` -- fan out the raw event.
6. `DeriveDocChanged(ev, registry)` -- derive secondary `doc.changed` events (sole derive site).
7. `hub.Broadcast(derived)` for each derived event.
8. The hub relays raw + derived events to subscribed peers (e.g. auto-ui). Return 204.

### 4.2 WebSocket `/api/ws` (bidirectional)

Full-duplex JSON-RPC 2.0 over WebSocket, supporting both client-initiated RPC calls and server-push event notifications.

| Aspect | Detail |
|--------|--------|
| Upgrade | Standard WebSocket upgrade; same-origin policy (no explicit CORS) |
| Inbound | JSON-RPC requests (with `id`) dispatched to registered handlers |
| Outbound | JSON-RPC notifications pushed via the hub broadcast |
| Buffering | 16-slot per-connection outbound buffer |
| Slow client | Connection dropped (cancelled) when buffer fills; never blocks the hub |
| Keepalive | Server pushes `ping` notifications every 1 second |

**Registered RPC methods:**
- `ping` -- echo with sequence number
- `doc.list` -- list `docs/**/*.md` files for a project/worktree
- `doc.get` -- read raw markdown content of a single doc

**Session lifecycle:**
1. WebSocket accepted; `session` created with context and outbound channel.
2. Session registered with hub via `hub.Subscribe(session)` (session implements `bus.Sink`).
3. Write pump goroutine drains the outbound channel; ping loop pushes keepalives.
4. Read loop dispatches inbound JSON-RPC to the shared `Dispatcher`.
5. On disconnect (or slow-client drop): cancel context, deregister from hub, close connection.

### 4.3 `bus.subscribe` RPC (broadcast-all relay)

The `bus.subscribe` JSON-RPC method registers a connected peer to receive **all** hub events as server-push notifications. It is parameterless (broadcast-all per GR-N5) -- subscribers receive every event; filtering is client-side.

| Aspect | Detail |
|--------|--------|
| Method | `bus.subscribe` |
| Params | None (broadcast-all) |
| Response | `{"status": "subscribed"}` |
| Idempotent | Yes -- a second call from the same peer is a no-op |
| Cleanup | Subscription is cancelled when the peer disconnects |

**Relay path:** Hub → per-peer `peerSink` (implements `bus.Sink`) → `Peer.Notify(ev.Type, ev)` → JSON-RPC notification on the wire. The notification shape matches `Event.AsNotification()`: `method` = the event type, `params` = the full envelope with every field intact.

**Per-peer drop-on-full / at-most-once:** `peerSink.Deliver` calls `Peer.Notify`, which uses a non-blocking bounded-channel enqueue (`defaultBufferSize=16`). If the buffer is full, the frame is dropped and the connection is closed -- the hub never blocks on a slow subscriber. This is consistent with the at-most-once delivery contract (section 5) and mirrors auto-ui's WebSocket slow-client policy.

**`ctl.*` gating through the relay:** The relay bridge has no type-specific filter. `ctl.*` events reach subscribers only when `--ctl-events` is enabled (the gate is at emission, not relay). Data-plane events (`doc.changed`, `watch.task.*`, `agent.*`) always relay.

**Implementation:** The bridge lives in `rpcserver` (the accept loop), not `rpcmethods`, so the per-connection subscription state (a cancel func) is co-located with the peer lifecycle. `rpcmethods` remains transport-free.

### 4.4 RPC-transport keepalive (TCP / Unix / pipe)

The connection-oriented RPC transport (`rpc.Peer` over TCP, Unix-domain `SOCK_STREAM`, or in-process `net.Pipe()`) carries an **opt-in, application-level liveness** mechanism, mirroring the WebSocket 1-second ping model (section 4.2) at the `rpc.Peer` layer. It exists because GR-N8 makes reliable, in-order transports load-bearing: on such a transport a peer whose process was killed / NAT-dropped / rebooted leaves a **half-open** connection that produces no read error until someone writes. Without liveness nothing reaps it — a dead subscriber lingers in the hub's sink set and a pending `Call` on a non-cancelling context hangs forever.

| Aspect | Detail |
|--------|--------|
| Opt-in | `rpc.WithKeepAlive(interval, timeout)`; default off, so existing callers/tests are behaviorally unchanged |
| Ping/pong | A reserved `$keepalive` ping (no params) is pushed every `interval`; **every** peer that receives a ping immediately echoes a `$keepalive` pong (carrying a `{"pong":true}` marker), regardless of whether it enabled keepalive itself. A pong is never echoed again, so the exchange never loops |
| Ping enqueue | Uses the same bounded, drop-on-full enqueue as `Notify` (a stalled writer closes the conn rather than blocking) |
| Reap | A watchdog closes the connection (via the normal shutdown path) when no inbound frame has been decoded within `timeout` |
| Swallowing | `$keepalive` is consumed by the transport; it is never forwarded to application notification handlers |
| Daemon defaults | 15s ping / 45s reap timeout (gentle, intra-VPC-appropriate) |

**Why application-level (not OS `SetKeepAlive`):** the same mechanism covers all three transports including the in-process `net.Pipe()` fixture, and the timeout is deterministically testable with short durations.

**Why a watchdog-close, not `SetReadDeadline`:** `Peer.conn` is typed `io.ReadWriteCloser` (not `net.Conn`), and a read deadline firing mid-frame leaves `json.Decoder` in a corrupted state. The watchdog calls the existing `shutdown` (closing the conn → the blocked `Decode` returns → `Serve` returns → every pending caller is released as `ErrClosed`), reusing the proven terminal-shutdown path identically across pipe/unix/tcp. This is the section 5 "connection drop / half-open mid-call" failure mode being actively detected rather than waited on.

Any inbound frame — including the remote's `$keepalive` ping or pong — resets the silence window. Because the ping/pong is automatic and symmetric, the side that enables keepalive (e.g. the daemon) detects a dead peer even when the **other** side runs no keepalive and sends no application traffic: the daemon's ping elicits the client's pong, and only the *absence* of that pong (a genuinely half-open / dead peer) trips the watchdog. This is what lets the daemon enable keepalive on every accepted peer — including read-only `bus.subscribe` consumers — without false-reaping healthy idle subscribers.

### 4.5 Future transports (not implemented)

The following are documented as planned extensions:
- **Headless `auto bus serve`** -- standalone bus process without the full auto-ui, for environments that don't need the SPA.

Any future transport MUST satisfy GR-N8 (connection-oriented, reliable, in-order byte stream — see "Transport model and assumptions" above). Datagram/lossy transports are not a permitted extension without reworking the framing and correlation layers.

---

## 5. Delivery contract

The bus provides **at-most-once, explicitly lossy** delivery. This is an *application-layer* policy (drop-on-full at the consumer buffer), not a property of the transport — the underlying transports are reliable and in-order (GR-N8, section 4). Losses come from the bus deliberately shedding load to a slow or absent consumer, not from the network dropping bytes. Because all v1 networking is intra-VPC (low-loss, low-latency), this lossy policy is a simplification we *choose*, not a hazard we are forced to tolerate:

| Property | Guarantee |
|----------|-----------|
| Delivery | At-most-once. Events may be lost (client disconnected, buffer full, UI not running). |
| Ordering | Events broadcast in order per producer POST, but no global ordering across producers. |
| Durability | None. Events exist only in transit. The canonical record is ETL/SQLite. |
| Replay | None. Clients that reconnect must re-fetch state via RPC (e.g. `doc.get`). |
| Acks | None. Producers fire-and-forget; consumers receive or don't. |
| Idempotency | Not guaranteed. Each event has a unique `id`, but no deduplication is performed. |

**No consumer-side dedup (single ingest path).** The bus performs **no deduplication**, and as of **task 047** neither does the auto-ui consumer. Earlier (task 045), auto-ui layered a dedup-by-`id` `eventGate` over a short TTL window because an event could reach it via two routes — its own local `/api/rpc` ingest **and** a relayed backend (`bus.subscribe`). Task 047 removed the local ingest (hooks now post only to autowatch), so a single ingest path remains and no event can arrive twice; the `eventGate` was deleted and auto-ui's `SetEventSink` now broadcasts relayed events straight to its Hub. The deterministic derived `id` (section 2.1) survives but is no longer load-bearing for dedup.

**Events are invalidations, not state transfer.** A `doc.changed` event tells the client "this document changed" -- it does not carry the new content. The client must call `doc.get` to fetch the updated state. This keeps events small and avoids stale-data races when events arrive out of order.

**Slow-client policy:** If a WebSocket client's outbound buffer (16 slots) fills, the connection is cancelled and the client is deregistered. The hub never blocks waiting for a slow consumer.

**Producer resilience:** The `auto hooks fire` producer uses a 150ms HTTP timeout and swallows all errors (autowatch daemon down, timeout, marshal failure). It always exits 0 so it cannot disrupt the agent's critical path. The durable hook-event append (`hooks.Append`) runs **before** the live post and is the canonical record, so a dropped live post never loses the event.

---

## 6. Event-type registry

Event types follow a dotted hierarchy. The first segment is the domain; subsequent segments narrow the category.

### 6.1 `agent.*` -- agent hook events

Published by `auto hooks fire` when a coding agent (Claude, Codex) fires a hook.

| Type | Trigger | Data payload |
|------|---------|-------------|
| `agent.tool.post` | PostToolUse with a tool name | `ToolPost{tool, event, paths[], raw}` |
| `agent.tool.pre` | PreToolUse | `ToolPost{tool, event, paths[], raw}` |
| `agent.session.start` | SessionStart | `ToolPost{event}` |
| `agent.session.end` | SessionEnd / SessionStop | `ToolPost{event}` |
| `agent.posttooluse` | PostToolUse without a tool name | `ToolPost{event}` |
| `agent.<lower(event)>` | Any other hook event | `ToolPost{event, raw}` |

**Source format:** `auto/hooks/<agent>` (e.g. `auto/hooks/claude`, `auto/hooks/codex`).

**Event type mapping** (from `hook_event_name`):
- `PostToolUse` + tool name -> `agent.tool.post`
- `PostToolUse` without tool -> `agent.posttooluse`
- `SessionStart` -> `agent.session.start`
- `SessionEnd` / `SessionStop` -> `agent.session.end`
- `PreToolUse` -> `agent.tool.pre`
- Default: `agent.<lowercase(hook_event_name)>`

**ToolPost data payload:**

```json
{
  "tool": "Edit",
  "event": "PostToolUse",
  "paths": [
    {"rel": "docs/tasks/021/plan.md", "abs": "/home/user/src/repo/docs/tasks/021/plan.md"}
  ],
  "raw": {"file_path": "/home/user/src/repo/docs/tasks/021/plan.md", "old_string": "...", "new_string": "..."}
}
```

- `tool` and `event`: normalized cross-tool interface fields.
- `paths[]`: each entry has `rel` (repo-relative, stable logical identity) and `abs` (host-local, for file reads). `rel` is computed via `filepath.Rel(worktreeRoot, abs)`. Path resolution handles both absolute paths (Claude) and relative paths (Codex) by joining against the payload's `cwd`.
- `raw`: the agent's original `tool_input` JSON, verbatim. Consumers code against the normalized fields; `raw` preserves full fidelity for tool-specific needs.

### 6.2 `doc.*` -- document events

Derived by the hub, not published directly by producers.

| Type | Trigger | Data payload |
|------|---------|-------------|
| `doc.changed` | Hub derives from `agent.tool.post` when a path matches `docs/**/*.md` in a registered project | `DocChanged{project, path, abs_path, worktree, branch}` |

**Source:** `auto/bus/derive`

**Derivation rules:**
1. Only `agent.tool.post` events trigger derivation.
2. The event's `project` must be present in the registry (`reg.FindProjectByID(ev.Project) != nil`). The registry is the authority -- an unregistered or unknown project derives nothing.
3. Each `PathRef.Rel` is cleaned (reject `..` traversal) and checked against `strings.HasPrefix(rel, "docs/") && strings.HasSuffix(rel, ".md")`. This matches `docs/foo.md` (zero subdirectory depth) as well as `docs/tasks/021/plan.md`.
4. One `doc.changed` event is emitted per matching path, carrying the same provenance as the source event.

**DocChanged data payload:**

```json
{
  "project": "auto-stack",
  "path": "docs/tasks/021/plan.md",
  "abs_path": "/home/user/src/repo/docs/tasks/021/plan.md",
  "worktree": "/home/user/src/repo",
  "branch": "main"
}
```

### 6.3 `ctl.*` -- control-plane events

Control-plane events provide daemon-level observability: structured logging, connection lifecycle, and health. They are **gated behind `--ctl-events`** and off by default, so they add no noise in normal operation. The data/control split is: `ctl.*` events are infrastructure-level logs from the daemon process itself; `watch.task.*` events are data-plane domain events about watched tasks.

Delivery follows the same at-most-once contract as all bus events (section 5).

| Type | Description | Data payload |
|------|-------------|-------------|
| `ctl.log.info` | Informational daemon log entry | `CtlLogEvent` |
| `ctl.log.warn` | Warning-level daemon log entry | `CtlLogEvent` |
| `ctl.log.error` | Error-level daemon log entry | `CtlLogEvent` |
| `ctl.connect` | A client connected to the daemon | *(envelope only)* |
| `ctl.disconnect` | A client disconnected from the daemon | *(envelope only)* |
| `ctl.health` | Periodic daemon health heartbeat | *(envelope only)* |

**Source:** `auto/watch/daemon`

**CtlLogEvent data payload:**

```json
{
  "level": "info",
  "op": "rpc.served",
  "message": "served request",
  "fields": {"method": "daemon.status"}
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `level` | string | yes | One of `info`, `warn`, `error`. Matches the dotted type suffix. |
| `op` | string | yes | The operation that produced this log (e.g. `rpc.served`, `slow.client`). |
| `message` | string | yes | Human-readable log message. |
| `fields` | map[string]string | no | Optional structured key-value pairs for machine-readable context. |

**Constructor:** `NewCtlLog(level, op, msg, fields)` maps the level to the appropriate `ctl.log.*` type constant and returns a validated `Event`. Unknown levels return an error.

### 6.4 `watch.*` -- auto-watch daemon events

The auto-watch daemon is the named second adopter of the bus standard. The `watch.task.started`, `watch.task.completed`, and `watch.task.failed` types are **live producers**: they are emitted by the daemon's task RPCs over the bus, in addition to being stored in SQLite via `store.EventInput`. The remaining types are paper mappings — defined here to validate the envelope design, with implementation deferred.

| Bus type | Current `event_type` | Status | Description |
|----------|---------------------|--------|-------------|
| `watch.task.started` | `task_started` | Live | A watched task has begun execution |
| `watch.task.completed` | `task_completed` | Live | A watched task completed successfully |
| `watch.task.failed` | `task_failed` | Live | A watched task failed (exit code != 0, timeout, worker not started) |
| `watch.task.reserved` | `task_reserved` | Paper | A run slot has been reserved for a task |
| `watch.task.skipped` | `task_skipped_dedup` | Paper | A task was skipped due to deduplication |
| `watch.trigger.evaluated` | `trigger_evaluated` | Paper | A trigger was evaluated (fired or not) |
| `watch.worktree.created` | `worktree_created` | Paper | A worktree was created for a task run |
| `watch.worktree.removed` | `worktree_removed` | Paper | A worktree was cleaned up after a run |
| `watch.system.warning` | `system_warning` | Paper | Non-fatal system-level warning |
| `watch.config.warning` | `config_warning` | Paper | Configuration-level warning |

The live types are always-on data-plane events (not gated behind `--ctl-events`). See section 6.5 for the detailed `watch.task.*` envelope mapping.

### 6.5 Envelope mapping: `watch.task.*` onto the bus envelope

This mapping documents the live `watch.task.*` envelope produced by the daemon, and demonstrates that the bus envelope accommodates auto-watch's domain without modification.

#### `watch.task.started`

```json
{
  "jsonrpc": "2.0",
  "method": "watch.task.started",
  "params": {
    "specversion": "1.0",
    "type": "watch.task.started",
    "source": "auto/watch/daemon",
    "id": "f1e2d3c4b5a69780",
    "time": "2026-06-11T10:30:00Z",
    "project": "auto-stack",
    "remote": "https://github.com/owner/repo",
    "branch": "feat/new-feature",
    "worktree": "/home/user/src/repo-worktrees/feat-new-feature",
    "commit": "abc1234def5678",
    "data": {
      "task_id": "build-and-test",
      "run_id": 42,
      "trigger_id": "on-branch-push",
      "session_name": "auto-watch-42",
      "resource_key": "feat/new-feature",
      "message": "task started"
    }
  }
}
```

**Mapping notes:**
- `project`, `remote`, `branch`, `worktree`, `commit`: top-level provenance from the run's project registration and worktree state.
- `data.task_id`, `data.run_id`: domain-specific identifiers that live in the opaque data payload.
- `data.trigger_id`: which trigger caused the task to run.
- `data.session_name`, `data.resource_key`: operational metadata from `store.RunStartUpdate`.
- `data.message`: human-readable status, matching the current `EventInput.Message`.

#### `watch.task.failed`

```json
{
  "jsonrpc": "2.0",
  "method": "watch.task.failed",
  "params": {
    "specversion": "1.0",
    "type": "watch.task.failed",
    "source": "auto/watch/daemon",
    "id": "0a1b2c3d4e5f6789",
    "time": "2026-06-11T10:35:00Z",
    "project": "auto-stack",
    "remote": "https://github.com/owner/repo",
    "branch": "feat/new-feature",
    "worktree": "/home/user/src/repo-worktrees/feat-new-feature",
    "commit": "abc1234def5678",
    "data": {
      "task_id": "build-and-test",
      "run_id": 42,
      "trigger_id": "on-branch-push",
      "exit_code": 1,
      "resource_key": "feat/new-feature",
      "message": "run 42 finished with exit code 1"
    }
  }
}
```

**Mapping notes:**
- Same provenance attributes at the top level.
- `data.exit_code`: from `RunRecord.ExitCode`, the process exit code.
- `data.message`: matches current `EventInput.Message` format.
- The current `EventInput.Level` (`"error"` for failures) is not carried in the bus envelope -- the event type (`watch.task.failed`) is sufficient to convey severity. Consumers can map type to severity locally.

#### `watch.task.completed`

Identical structure to `watch.task.failed` but with `type: "watch.task.completed"`, `exit_code: 0`, and no error information in `message`.

#### Design validation

The envelope mapping confirms:
1. **Provenance fits naturally.** The run's `ProjectID`, `ProjectPath`, `WorktreePath`, and `Branch` map directly to the envelope's `project`, `worktree`, and `branch`. `remote` is resolved from the project registration.
2. **Domain data stays in `data`.** `TaskID`, `RunID`, `TriggerID`, `ExitCode`, `Message`, and `ResourceKey` are domain-specific and belong in the opaque data payload, not the envelope.
3. **No envelope changes needed.** The existing envelope fields accommodate auto-watch's requirements without adding new top-level attributes.
4. **Level/severity is implicit in type.** The dotted type hierarchy (`watch.task.failed` vs `watch.task.completed`) carries severity information that the current flat `Level` field provides.

---

## 7. RPC methods

These are request/response methods available over WebSocket (section 4.2).

### 7.1 `ping`

Health check with echo.

**Request:** `{"seq": 1}`
**Response:** `{"pong": true, "seq": 1}`

### 7.2 `doc.list`

List markdown documentation files for a project.

**Request:**
```json
{"project": "auto-stack", "worktree": "/optional/worktree/path"}
```

**Response:**
```json
[
  {"id": "docs/auto-bus-spec.md", "path": "docs/auto-bus-spec.md"},
  {"id": "docs/user-journey.md", "path": "docs/user-journey.md"}
]
```

- Returns only `docs/**/*.md` files (IDs and relative paths, no bodies -- cheap rung).
- `worktree` overrides the read root (must be a registered project path).
- `project` falls back to the registered project's path.
- At least one of `project` or `worktree` is required.

### 7.3 `doc.get`

Read a single documentation file's raw markdown content.

**Request:**
```json
{"project": "auto-stack", "path": "docs/auto-bus-spec.md", "worktree": "/optional/worktree/path"}
```

**Response:**
```json
{"path": "docs/auto-bus-spec.md", "markdown": "# Auto Bus Specification\n..."}
```

- `path` is required and must be under `docs/` with a `.md` extension.
- Path traversal (`../`) is rejected.
- Reads are restricted to the same `docs/**/*.md` set that `doc.list` exposes.
- `worktree` is validated against the project registry (never accepts an arbitrary client-supplied root).
- Error messages never leak absolute filesystem paths.

---

## 8. Hub architecture

### 8.1 Component diagram

```
Producer (auto hooks fire)
    |
    | HTTP POST http://<hookAddr>/   (autowatch hook-ingest, default 127.0.0.1:7787)
    v
+---------------------------+
| autowatch daemon          |
|                           |
|  HookIngest               |
|    parse -> validate      |
|    stamp ev.Host          |
|    hub.Broadcast(raw)     |
|    DeriveDocChanged()     |
|    hub.Broadcast(derived) |
|                           |
|  Hub  -> relay (bus.subscribe) to subscribed peers
+-----------|---------------+
            |
            | relayed raw + derived events
            v
+---------------------------+
| auto-ui server            |
|                           |
|  BackendManager           |
|    SetEventSink ->        |
|    hub.Broadcast(ev)      |
|    (no local ingest/derive; no eventGate — task 047)
|                           |
|  Hub                      |
|    Subscribe(Sink) cancel |
|    Broadcast(Event)       |
|    [snapshot sinks under  |
|     RLock, deliver        |
|     outside lock]         |
|                           |
|  session (bus.Sink)       |
|    Deliver -> enqueue     |
|    [non-blocking channel  |
|     send; drop on full]   |
+-----|--------|------------+
      |        |
      v        v
   WS client  WS client
   (browser)  (browser)
```

### 8.2 Decoupling

The `bus` package (`auto-shared/bus`) has no dependency on `auto-ui` or `auto-watch`. The `Hub`, `Event`, and `Sink` types are defined in the shared package. Both auto-ui (WebSocket sessions) and auto-watch (RPC peer sinks via `bus.subscribe`) host their own hub and implement `bus.Sink`. This decoupling means other components can construct and validate bus events without importing either server.

### 8.3 Registry provider

The server accepts a registry provider function (`func() config.ProjectsConfig`) via functional options. This provider is called per ingest/doc request, so projects registered after server startup resolve without a restart. Unit tests inject an empty or fixture registry to stay hermetic (they never read the developer's real `~/.auto/projects.json`).

---

## Appendix A: Go type reference

```go
package bus

const SpecVersion = "1.0"

// Event is the canonical bus envelope.
type Event struct {
    SpecVersion string          `json:"specversion"`
    Type        string          `json:"type"`
    Source      string          `json:"source"`
    ID          string          `json:"id"`
    Time        string          `json:"time"`
    Project     string          `json:"project,omitempty"`
    Session     string          `json:"session,omitempty"`
    Remote      string          `json:"remote,omitempty"`
    Branch      string          `json:"branch,omitempty"`
    Worktree    string          `json:"worktree,omitempty"`
    Commit      string          `json:"commit,omitempty"`
    Data        json.RawMessage `json:"data,omitempty"`
}

func NewEvent(typ, source string, data any) (Event, error)
func (e Event) Validate() []ValidationError
func (e Event) AsNotification() Notification

// Notification wraps an event as a JSON-RPC 2.0 notification.
type Notification struct {
    JSONRPC string `json:"jsonrpc"`
    Method  string `json:"method"`
    Params  Event  `json:"params"`
}

// Sink receives broadcast events.
type Sink interface {
    Deliver(Event)
}

// Hub broadcasts events to all registered sinks.
type Hub struct { /* ... */ }
func NewHub() *Hub
func (h *Hub) Subscribe(s Sink) (cancel func())
func (h *Hub) Broadcast(ev Event)

// DeriveDocChanged derives doc.changed events from agent.tool.post.
func DeriveDocChanged(ev Event, reg config.ProjectsConfig) []Event

// PathRef carries repo-relative and absolute paths.
type PathRef struct {
    Rel string `json:"rel"`
    Abs string `json:"abs"`
}

// ToolPost is the data payload for agent.tool.post events.
type ToolPost struct {
    Tool  string          `json:"tool"`
    Event string          `json:"event"`
    Paths []PathRef       `json:"paths"`
    Raw   json.RawMessage `json:"raw,omitempty"`
}

// DocChanged is the data payload for doc.changed events.
type DocChanged struct {
    Project  string `json:"project"`
    Path     string `json:"path"`
    AbsPath  string `json:"abs_path"`
    Worktree string `json:"worktree"`
    Branch   string `json:"branch"`
}

// DecodeData unmarshals ev.Data into T.
func DecodeData[T any](ev Event) (T, error)

// Control-plane event types (gated behind --ctl-events, off by default).
const (
    TypeCtlLogInfo    = "ctl.log.info"
    TypeCtlLogWarn    = "ctl.log.warn"
    TypeCtlLogError   = "ctl.log.error"
    TypeCtlConnect    = "ctl.connect"
    TypeCtlDisconnect = "ctl.disconnect"
    TypeCtlHealth     = "ctl.health"
)

// CtlLogEvent is the data payload for ctl.log.* events.
type CtlLogEvent struct {
    Level   string            `json:"level"`
    Op      string            `json:"op"`
    Message string            `json:"message"`
    Fields  map[string]string `json:"fields,omitempty"`
}

func NewCtlLog(level, op, msg string, fields map[string]string) (Event, error)
```
