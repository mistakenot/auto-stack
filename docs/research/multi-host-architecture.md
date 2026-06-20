---
hash: ""
id: "9649eb7e"
read_when: "planning multi-project UI support, adding RPC to autowatch, designing cross-host connectivity, or extending the bus event envelope"
summary: "Design exploration for supporting multiple projects, users, and hosts through a single auto-ui, with autowatch as the per-user RPC executor and an abstracted transport layer."
title: "Multi-Project / Multi-Host Architecture"
---

# Multi-Project / Multi-Host Architecture

## Motivation

The current auto-ui + bus architecture is single-host, broadcast-everything. Three real scenarios demand multi-project/multi-host support:

1. **Same Linux user, multiple projects** — one developer works across several repos on one machine.
2. **Same host, different Linux users** — shared dev server where each user has their own projects.
3. **Different hosts entirely** — distributed team, CI boxes, GPU machines, laptops.

This doc captures findings from a design exploration (June 2026) that surveyed the existing architecture and mapped a path to all three scenarios.

## Current State Assessment

### What Already Works

- **Project registry** (`~/.auto/projects.json`) lists all registered projects per host, with lookup by ID, path, or normalized git remote.
- **Bus event envelope** (`auto-shared/bus/event.go`) carries `Project`, `Remote`, `Branch`, `Worktree`, `Session` fields — events are already tagged with project identity.
- **Doc RPCs** (`doc.list`, `doc.get`) accept a `project` parameter, so the SPA can browse different project trees.
- **Client-side filtering** — the JS already checks `ev.Project` and discards unmatched events (see `docevents.js:matchesDoc`, `tree.js:387`, `content.js:131`).
- **Loopback guard** (`auto-ui/internal/server/loopback.go`) was explicitly designed for tailscale-serve compatibility — rejects non-loopback peers but allows tailscaled to proxy.

### What's Missing

| Gap | Detail |
|-----|--------|
| No `hostId` on bus events | `~/.auto/host.json` stores it, hooks log it durably, but it's not propagated to `bus.Event` |
| No server-side event filtering | Hub broadcasts everything to all WebSocket clients |
| No project-switcher UI | SPA shows one project at a time, no landing page |
| No cross-user filesystem access | auto-ui reads files directly; can't read other users' project directories |
| No cross-host connectivity | auto-ui binds to 127.0.0.1 only, no event transport between hosts |
| No RPC surface on autowatch | Daemon has no socket/HTTP listener; all control is via CLI + SQLite |

## Architecture Decision: Autowatch as Per-User RPC Executor

### The Insight

Autowatch already runs as a systemd-managed daemon on each host. It already has:

- **Filesystem access** to that user's projects (it runs as the owning user).
- **Project registry awareness** (loads `~/.auto/projects.json` each tick).
- **Task dispatch infrastructure** (worktree creation, tmux launch, run tracking with dedup, exit code reaping, 24h TTL cleanup).
- **A stable daemon lifecycle** (systemd restart, file-lock singleton, signal handling).

Rather than introducing a new sidecar process, autowatch grows a JSON-RPC listener alongside its existing tick loop.

### Resulting Architecture

```
auto-ui (stateless proxy/renderer)
  ├── Serves the SPA
  ├── Routes RPCs to the right autowatch backend (by hostId)
  ├── Aggregates bus events from all backends into the WebSocket hub
  └── Does NO filesystem reads, NO task execution, NO direct project access

autowatch (per-user executor/authority)
  ├── Existing: tick loop, cron/file triggers, task dispatch, run tracking
  ├── New: JSON-RPC listener on unix socket or TCP
  │   ├── doc.list / doc.get   (reads files from local filesystem)
  │   ├── task.dispatch        (queues run into SQLite, wakes tick loop)
  │   ├── task.status/cancel   (queries/controls running tasks)
  │   ├── project.list         (returns this user's registered projects)
  │   └── bus.subscribe        (relays events upstream to auto-ui)
  └── Hooks fire to local autowatch; autowatch relays to central UI
```

### Per-Scenario Mapping

**Same user, multiple projects:**
No socket needed — auto-ui and autowatch run as the same user. Can call in-process or over localhost unix socket. All projects in one registry.

**Same host, different users:**
```
alice (auto-ui) ──unix socket──► bob's autowatch (/run/user/1002/auto-watch.sock)
```
Socket permissions via shared group. Alice's UI dispatches tasks that run as bob, in bob's worktrees, with bob's credentials.

**Different hosts:**
```
alice (auto-ui, host-a) ──TCP/tailscale──► autowatch (host-b:9090)
```
Same JSON-RPC protocol, TCP transport. Tailscale handles auth + encryption. Or a future transport (iroh, etc.).

### Wake-on-Demand

The daemon tick loop (`ops.go:86`) currently sleeps on a 60-second ticker. Add a wake channel so RPC-dispatched tasks execute immediately:

```go
ticker := time.NewTicker(60 * time.Second)
for {
    select {
    case <-ticker.C:
        service.Tick(ctx)
    case <-service.Wake():   // RPC handler signals after inserting a pending run
        service.Tick(ctx)
    case <-ctx.Done():
        return
    }
}
```

The RPC handler inserts the pending run into SQLite, then signals the wake channel. The tick loop picks it up — same reap/launch code path, no special case.

## hostId as the Routing Key

### Current State

- `~/.auto/host.json` stores `hostId`, defaulting to `os.Hostname()`.
- Hook producer (`hookscmd.go`) reads `hostId` for the durable log but does **not** attach it to bus events.
- The bus event envelope has no `Host` field.

### Design Decision

`hostId` must be unique per user, not just per machine (since multiple users on one host each need a distinct identity). It is user-chosen during `auto init`, with `hostname-username` as the suggested default.

### Changes Required

1. **`auto init`** prompts for `hostId`, stores in `~/.auto/host.json`.
2. **`bus.Event`** gets a non-optional `Host string` field.
3. **Hook producer** attaches `ev.Host` from config (it already reads it; just not propagated).
4. **Autowatch** attaches `hostId` to any events it produces or relays.
5. **Auto-ui** uses `Host` for RPC routing and display grouping.
6. **Fallback**: if `hostId` is empty (config not initialized), fall back to `os.Hostname()` so events are never unroutable. `auto doctor` flags the missing init.

Updated envelope:
```go
type Event struct {
    SpecVersion string            `json:"specversion"`
    Type        string            `json:"type"`
    Source      string            `json:"source"`
    ID          string            `json:"id"`
    Time        string            `json:"time"`
    Host        string            `json:"host"`                // NEW: unique per user
    Project     string            `json:"project,omitempty"`
    Session     string            `json:"session,omitempty"`
    Remote      string            `json:"remote,omitempty"`
    Branch      string            `json:"branch,omitempty"`
    Worktree    string            `json:"worktree,omitempty"`
    Commit      string            `json:"commit,omitempty"`
    Env         map[string]string `json:"env,omitempty"`
    Data        json.RawMessage   `json:"data,omitempty"`
}
```

## Transport Abstraction

### Motivation

The RPC surface should not care whether the transport is a unix socket, TCP, tailscale, or something exotic like iroh (a Rust-based peer-to-peer QUIC library that handles NAT traversal, relay fallback, and dial-by-public-key). The right move is to keep the transport behind an interface so implementations can be swapped later.

### Interface Shape

```go
// auto-shared/transport/transport.go
type Listener interface {
    Accept() (Conn, error)
    Addr() string
    Close() error
}

type Dialer interface {
    Dial(ctx context.Context, backend string) (Conn, error)
}

type Conn interface {
    io.ReadWriteCloser
}
```

JSON-RPC 2.0 framing sits on top of `Conn`. Method handlers (`doc.list`, `task.dispatch`, `bus.subscribe`) never see the transport.

### Concrete Implementations

| Implementation | Scope | When |
|----------------|-------|------|
| `transport/unix` | Same-host, cross-user | Phase 1 |
| `transport/tcp` | Same-host or LAN (with loopback guard) | Phase 1 |
| `transport/iroh` | Cross-host, NAT-traversing, dial-by-public-key | Future |
| `transport/tailscale` | Cross-host via tailnet | Future |

### Registry Integration

`ProjectRef` gets a `Backend string` field — a URI scheme that picks the transport:
- `unix:///run/user/1002/auto-watch.sock`
- `tcp://host-b:9090`
- `iroh://ae3f...c7d2` (future)

## Autowatch RPC Surface

| RPC Method | Direction | What it does |
|---|---|---|
| `task.dispatch` | UI -> autowatch | Insert pending run, wake tick loop |
| `task.cancel` | UI -> autowatch | Kill tmux session, mark failed |
| `task.status` | UI -> autowatch | Query run state from SQLite |
| `task.output` | UI -> autowatch | Stream/tail output.log with byte offset |
| `task.list` | UI -> autowatch | List active/recent runs for a project |
| `project.list` | UI -> autowatch | Return this user's registered projects |
| `doc.list` / `doc.get` | UI -> autowatch | Read docs from this user's filesystem |
| `bus.subscribe` | UI <-> autowatch | Bidirectional event stream over WebSocket |
| `daemon.status` | UI -> autowatch | Health, uptime, trigger stats |

## Iroh Evaluation

[iroh.computer](https://www.iroh.computer/) was evaluated as a potential transport layer. Iroh is a Rust-based peer-to-peer QUIC library offering dial-by-public-key, automatic NAT traversal, relay fallback, and ALPN-based protocol routing.

### Concept Fit

Excellent. The programming model maps directly:

| Our Problem | Iroh Solution |
|---|---|
| Cross-host connectivity | Automatic NAT traversal + relay fallback |
| Discovery | Publish NodeId to DNS; dial by public key, not IP |
| Auth/encryption | Free — ed25519 identity + QUIC TLS |
| Event broadcast | iroh-gossip — pub/sub overlay network |
| Protocol routing | ALPN identifiers map to our JSON-RPC method dispatch |

### Practical Blockers

Iroh is Rust-only. No Go bindings exist (FFI covers Python, JS, Kotlin, Swift). Integration options:

| Approach | Effort | Pain |
|---|---|---|
| CGO + iroh-ffi | Medium | Breaks `go build` simplicity, adds C toolchain dependency |
| Iroh sidecar process | Small | Extra binary to deploy, communicate over local pipe |
| Reimplement in Go | Massive | Not realistic |

### Decision

Not useful now. The transport abstraction ensures iroh (or anything else) can be plugged in later without touching the RPC surface or method handlers. Revisit if Go bindings appear or if the sidecar cost becomes justified by cross-host demand.

## Sequence of Work

Rough task breakdown if this moves to an epic:

1. **Add `Host` field to `bus.Event`** — trivial, unblocks everything downstream.
2. **`auto init` prompts for hostId** — update host config generation.
3. **Hook producer wires `ev.Host`** — already reads it, just attach to the event.
4. **Transport abstraction** — `Listener`/`Dialer` interfaces + unix/tcp implementations.
5. **Autowatch JSON-RPC listener** — socket alongside tick loop, wake channel.
6. **Autowatch serves doc.list/doc.get** — reuse auto-ui's handler code.
7. **Autowatch serves task.dispatch** — insert pending run, wake tick.
8. **Auto-ui proxy layer** — route RPCs to autowatch backends by hostId.
9. **Auto-ui aggregates bus events** — subscribe to each autowatch backend.
10. **Registry `Backend` field** — project -> hostId -> transport URI.
11. **Multi-project SPA** — landing page, project switcher, host grouping.

## Related Existing Work

- **Epic 002** (planning-docs-dashboard) — built auto-ui, bus, hook events, doc RPCs.
- **Task 021** (auto-bus-standard) — defined the CloudEvents envelope, JSON-RPC framing, event type registry.
- **Task 022** (hook-event-log) — durable hook logging with hostId capture.
- **Task 026** (planning-dashboard-live-updates) — WebSocket broadcast, doc.changed derivation, client-side filtering.
