---
hash: "0cddab69"
id: "49e918ca"
read_when: "implementing the auto-bus standard envelope, hub, or doc live-reload view"
summary: "CloudEvents-shaped bus envelope in auto-shared/bus, workspace provenance capture at the hook producer, Hub fan-out in auto-ui, doc.changed derivation, and live-reload doc view."
title: "Solution: Task 021 — Auto Bus Standard"
---

# Solution: Task 021

## Approach

1. **Define the envelope in `auto-shared/bus`.** A strongly-typed, validated CloudEvents-shaped `Event` struct. `data` is `json.RawMessage` (opaque round-trip) with typed constructor/decoder helpers for the events we author. Mirror auto-reflect's typed-envelope + `Validate()` pattern, but with dotted type names.

2. **Capture workspace provenance at the producer.** This is the answer to the worktree/branch question: events carry flat provenance attributes so any consumer can locate the physical file *and* identify the logical workload. `auto hooks fire` discovers these once from the cwd via git (best-effort, degrades to omitted):
   - `remote` — normalized git remote URL → **machine-independent repo identity** (the stable join key). **Hard requirement:** always `git.NormalizeRemoteURL(OriginRemote)` before it enters the envelope — the raw origin URL can embed a credential (`https://x-access-token:TOKEN@…`) and the bus broadcasts to every client, so the raw value must never be emitted. A unit test asserts the emitted `remote` contains no `@`/token.
   - `branch` — current branch → **the "feature"/workload**
   - `worktree` — absolute worktree root → **the workspace on this host** (locates files)
   - `commit` — HEAD sha → point-in-time
   - `project` — registry project ID, resolved **by remote** (new `FindProjectByRemote`, which normalizes both sides since the *stored* registry remote may be raw/token-bearing), falling back to `FindProjectByPath(worktree-root)` then cwd — because a worktree path won't prefix-match the registered main path, but its remote will. **Known limitation:** a registry entry with no `remote` (local-only repo, or a registration path that omits origin) resolves via neither remote nor prefix-match, so `project` may be empty and `doc.changed` derives nothing for that worktree until the entry gains a remote.

<!-- RESOLVED(P2): the `remote` attribute can leak a credential onto the broadcast bus — normalize, and test it
REVIEW: The live registry on this host (`~/.auto/projects.json`) stores the auto-stack remote as `https://x-access-token:github_pat_11AB35...@github.com/mistakenot/auto-stack.git` — a real PAT embedded in the URL. `git remote get-url origin` returns the same token-bearing string. The bus broadcasts every event to all WS clients and the UI renders/logs envelope fields, so a raw `remote` would publish that secret. Good news: `git.NormalizeRemoteURL` (normalize.go:38-49) strips `user:token@`, and plan.md 3.1 does say `remote=normalize(git.OriginRemote)`. Make this a hard requirement (never put raw `OriginRemote` in the envelope) and add a unit test asserting the emitted `remote` contains no `@`/no token. Note also: `FindProjectByRemote` must normalize BOTH sides (plan 1.2 does), since the *stored* registry remote is the raw token-bearing value, not normalized.
AUTHOR: Good catch — real PAT exposure. Made normalization a hard requirement in the `remote` bullet ("never emit the raw value") and added a unit test to plan step 3.4 asserting the emitted `remote` contains no `@`/token. `FindProjectByRemote` (plan 1.2) already normalizes both sides, which is now also called out explicitly in the `project` bullet. Flagging this as a reusable rule (normalize remotes before they cross a broadcast/log boundary). -->

<!-- RESOLVED(P2): resolving `project` by remote depends on the registry having `remote` populated
REVIEW: `ProjectRef.Remote` is `omitempty` (projects.go:23) and only gets set by the `auto`/`auto watch` init flows (initcmd.go:85, watch init.go:83). A project registered without an origin (local-only repo, or a future registration path that omits it) has `Remote==""`, so `FindProjectByRemote` misses and you fall back to `FindProjectByPath(cwd)` — which solution.md itself says won't match worktree paths. So for any registry entry lacking `remote`, the whole workspace-correct chain silently degrades and `doc.changed` derives nothing for that worktree. Worth calling out as a known limitation (and the fallback should at least try `FindProjectByPath` on the worktree root, not just cwd).
AUTHOR: Both addressed in the `project` bullet above: documented as an explicit Known limitation (no-`remote` registry entries don't resolve worktrees → no derivation until a remote is added), and the fallback now tries `FindProjectByPath(worktree-root)` before cwd. plan step 3.1 updated to match. -->



3. **File payloads carry both paths, and preserve-and-normalize.** For `agent.tool.post`, each touched path is `{ "rel": "docs/tasks/021/plan.md", "abs": "/abs/worktree/docs/tasks/021/plan.md" }`. `rel` (repo-relative, computed via `filepath.Rel(worktree, abs)`) is the **stable logical doc identity** used for `docs/**` matching and UI grouping; `abs` is needed to actually read the file from the right workspace. The payload is **two-layer**: a `raw` (`json.RawMessage`) field preserves the agent's *original* tool fields verbatim (each agent's `tool_input` schema differs — no fidelity loss), while normalized fields (`tool`, `event`, `paths[]`) copy the same values under one naming standard. Consumers code against the normalized interface; the raw layer stays available for anything tool-specific. Same discipline as the envelope: typed/normalized where we author, opaque where we carry.

4. **Build the hub in `auto-shared/bus`, host it in auto-ui.** A `Hub` with a connection-set + `Broadcast(Event)`. Wire a session registry into `ws.go` (register on connect, deregister on `cancel`). Add `POST /api/rpc` to the mux: parse one JSON-RPC frame, validate the envelope, broadcast to all sessions, run derivation. Malformed → JSON-RPC error response, no broadcast. Reuse `session.notify` / `outboundBuffer` slow-client drop unchanged.

5. **Derive `doc.changed` in the hub.** On `agent.tool.post`, for each path whose `rel` matches `docs/**/*.md` and whose `project` resolves in the registry, emit a typed `doc.changed{ project, path(rel), abspath, worktree, branch }`. Broadcast raw event first, then each derived event.

6. **Workspace-aware doc RPCs + view.** `doc.list{ project?, worktree? }` → IDs + rel paths (cheap rung, reads `docs/**/*.md` from the worktree root if given, else the registered project root). `doc.get{ project, path, worktree? }` → raw markdown read from that workspace. SPA `doc` view: `doc.list` to pick, deep-linkable `#/doc?project=…&path=…&worktree=…`, render markdown client-side (`marked` via importmap), `on("doc.changed", …)` → if `{project, path, worktree}` matches the open doc, re-`doc.get` and re-render.

7. **Migrate `auto hooks fire`.** Replace the bare `HookEvent` POST to `/api/hooks` with a `bus.Event` (`type: agent.<event>`, `source: auto/hooks/<agent>`) POSTed to `/api/rpc`. Keep 150ms budget + always-exit-0. Remove the legacy `/api/hooks` path.

8. **Write `docs/auto-bus-spec.md`.** Envelope (with the provenance attributes), JSON-RPC framing, the two v1 bindings, at-most-once contract, dotted type registry, and the paper mapping of `watch.task.*` onto the envelope (`worktree`/`branch`/`commit` provenance + `TaskID`/`RunID` in data).

## Workspace identity model (the linkage answer)

| Question | Answer | Carried as |
|---|---|---|
| Which **repo**? | normalized git remote (machine-independent) | `remote` (envelope) |
| Which **feature/workload**? | `(remote, branch)` | `remote` + `branch` |
| Which **workspace** (physical files, this host)? | worktree root | `worktree` (envelope) |
| Which **logical document**? | `(project, rel-path)` | `project` + `data.rel` |
| Where is the **actual file**? | absolute path | `data.abs` |

So the live-reload loop is workspace-correct: an agent editing `plan.md` in worktree X fires an event tagged `worktree=X`; the UI re-fetches that doc **from worktree X** only if it is displaying worktree X. Edits in a different worktree (or main) don't spuriously reload it. Downstream, ETL still reconciles `session → commit → branch` as the canonical record; the bus just makes `(remote, branch, worktree)` available *live*.

## Files

```
+ auto-shared/bus/event.go            # Event envelope, SpecVersion, Validate(), AsNotification(), NewEvent
+ auto-shared/bus/payloads.go         # typed payloads: ToolPost{Tool,Event,Paths[]PathRef,Raw}, PathRef{Rel,Abs}, DocChanged
+ auto-shared/bus/hub.go              # Sink interface, Hub: Subscribe/Broadcast(Event)
+ auto-shared/bus/derive.go           # DeriveDocChanged(ev, registry) []Event  (docs/**/*.md matcher)
+ auto-shared/bus/event_test.go       # validation, opaque + raw/normalized round-trip, AsNotification, typed payloads
+ auto-shared/bus/hub_test.go         # fan-out, slow-sink drop, deregister
+ auto-shared/bus/derive_test.go      # docs/** match; non-md / outside-docs / unregistered → none (AC-5)
~ auto-shared/config/projects.go      # + FindProjectByRemote(remote) *ProjectRef
~ auto-shared/git/detect.go           # + Provenance(dir) — combined rev-parse → root/branch/commit
~ auto-ui/internal/server/server.go   # mount POST /api/rpc; construct Hub; inject registry provider
+ auto-ui/internal/server/rpc_ingest.go # handleRPC: parse frame → validate → broadcast → derive
+ auto-ui/internal/server/rpc_ingest_test.go # POST → WS fan-out + derived doc.changed; malformed → 400
~ auto-ui/internal/server/ws.go       # session implements bus.Sink; register/deregister with Hub
+ auto-ui/internal/server/docs.go     # doc.list / doc.get handlers (workspace-aware, docs/**/*.md)
+ auto-ui/internal/server/docs_test.go # doc.list/get happy path + traversal rejected
+ auto-ui/web/static/doc.js           # DocView module: doc.list picker, doc.get render, live reload
~ auto-ui/web/static/app.js           # import DocView; nav link; render case
~ auto-ui/web/static/index.html       # + marked in importmap
~ auto-cli/cmd/auto/hookscmd.go       # emit bus.Event to /api/rpc; git provenance; rel+abs paths; switch /api/hooks URL
~ auto-cli/cmd/auto/hookscmd_test.go  # assert posted frame is a valid agent.* bus envelope
+ docs/auto-bus-spec.md               # the standard
```

<!-- RESOLVED(P3): Files list diverges from plan.md (doc.js, derive_test.go)
REVIEW: This Files block puts the doc view inside `~ auto-ui/web/static/app.js` and lists only `event_test.go` + `hub_test.go`. plan.md instead adds a separate `+ auto-ui/web/static/doc.js` module AND lists `+ auto-shared/bus/derive_test.go` and `+ rpc_ingest_test.go`/`docs_test.go`. Reconcile so the two docs agree on (a) whether DocView is its own file, and (b) the full test file set — derive logic (AC-5) needs its own test regardless.
AUTHOR: Reconciled the solution Files block to match plan.md: DocView is its own `+ auto-ui/web/static/doc.js` (app.js just imports it), and the full test set is listed — `derive_test.go`, `rpc_ingest_test.go`, `docs_test.go`, `hookscmd_test.go` — plus the new `git/detect.go` Provenance change. -->


Envelope outline:
```go
package bus
const SpecVersion = "1.0"
type Event struct {
    SpecVersion string          `json:"specversion"`           // "1.0"
    Type        string          `json:"type"`                  // dotted: agent.tool.post, doc.changed
    Source      string          `json:"source"`                // auto/hooks/claude, auto/ui/hub
    ID          string          `json:"id"`
    Time        string          `json:"time"`                  // RFC3339 UTC
    Project     string          `json:"project,omitempty"`     // registry project ID
    Session     string          `json:"session,omitempty"`
    Remote      string          `json:"remote,omitempty"`      // workspace provenance ↓
    Branch      string          `json:"branch,omitempty"`
    Worktree    string          `json:"worktree,omitempty"`
    Commit      string          `json:"commit,omitempty"`
    Data        json.RawMessage `json:"data,omitempty"`        // opaque, or typed-by-author
}
func (e Event) Validate() []ValidationError   // required: specversion, type (dotted), source, id, time
// Notification is bus-defined (auto-shared can't reference auto-ui's unexported rpcRequest):
type Notification struct { JSONRPC string `json:"jsonrpc"`; Method string `json:"method"`; Params Event `json:"params"` }
func (e Event) AsNotification() Notification  // {jsonrpc:"2.0", method:e.Type, params:e}; session.enqueue/wsjson.Write marshal it

<!-- RESOLVED(P3): `rpcNotification` is undefined and can't be auto-ui's type — bus must define its own
REVIEW: There is no `rpcNotification` type anywhere; auto-ui's notification shape is the *unexported* `rpcRequest` (rpc.go:30, used id-less by `session.notify` in ws.go:96). `auto-shared/bus` is a separate module and cannot reference an unexported auto-ui type. plan.md 1.3 contradicts this outline by returning `any`. Resolve: `bus.AsNotification()` returns a bus-defined struct (or `map[string]any`) `{jsonrpc, method, params}`; the existing `session.enqueue` accepts `any` and `wsjson.Write` marshals it, so this composes — just make the return type concrete and consistent across both docs.
AUTHOR: Defined a concrete bus-owned `Notification{JSONRPC,Method,Params Event}` type and `AsNotification() Notification` in the outline above; `session.enqueue(any)` + `wsjson.Write` marshal it fine. plan step 1.3 updated to return `bus.Notification` (was `any`), so both docs now agree. -->


type PathRef struct { Rel, Abs string }
type ToolPost struct {                              // data for agent.tool.post
    Tool, Event string                              // normalized cross-tool interface
    Paths       []PathRef
    Raw         json.RawMessage                     // agent's original tool fields, verbatim (lossless)
}
type DocChanged struct { Project, Path, AbsPath, Worktree, Branch string } // data for doc.changed
```

## Test Coverage

| AC  | Test Type   | File                                        |
|-----|-------------|---------------------------------------------|
| AC-1 | review (doc) | docs/auto-bus-spec.md (manual checklist)    |
| AC-2 | unit        | auto-shared/bus/event_test.go               |
| AC-3 | unit/integration | auto-shared/bus/hub_test.go + auto-ui/internal/server/rpc_ingest_test.go |
| AC-4 | integration | auto-cli/cmd/auto/hookscmd_test.go (echo payload → assert posted bus.Event shape) |
| AC-5 | unit        | auto-shared/bus/derive_test.go (docs/** match, non-md/outside-docs/unregistered → none) |
| AC-6 | e2e (manual) | scripted hook fire → WS client receives doc.changed → doc.get; documented in spec |

## Out of Scope
- `subscribe()` filtering RPC, unix-socket binding, headless `auto bus serve` — spec-documented future extensions only.
- auto-watch daemon actually publishing to the bus (paper mapping only).
- Durability, replay, ordering, acks — bus is intentionally lossy.
- Auth beyond loopback binding (Tailscale serve handles remote).
- Rich doc browsing (index, search) — one minimal view proves the loop.
- **Cross-host workspace reconciliation** — `worktree`/`abs` are host-local; the spec notes `remote`+`branch` as the cross-host key but multi-host UI is out of scope.
- **Hub-side git enrichment** — provenance is producer-captured in v1; hub re-deriving git from a worktree path is a noted alternative, not built.

## Rejected Alternatives
- **Worktree-root path as the sole workspace key** (Option B): simplest (one `git rev-parse`), but machine-specific, loses branch identity, and worktree paths get reused/recycled. We keep `worktree` for file location but identify the workload by `(remote, branch)`.
- **Session-centric, defer git to ETL** (Option C): zero git work at hook time, but the live UI then can't know branch/workspace in real time — which defeats the live-reload-in-the-right-workspace use case. ETL reconciliation stays as the canonical layer underneath.
- **Hub enriches git provenance** instead of the producer: hub would run git per-event under broadcast load (latency, and the worktree may be mid-mutation). Producer already sits in the worktree — capture once there.
- **Nested `workspace{}` object** instead of flat `remote/branch/worktree/commit`: nesting is cleaner Go but flat top-level extension attributes are CloudEvents-idiomatic and filterable without parsing `data`.
- **Import the CNCF CloudEvents SDK:** adds a dependency for shape we can hand-roll in ~100 lines; requirements call for shape-only.
- **New bespoke event protocol (not JSON-RPC):** the transport-agnostic JSON-RPC 2.0 dispatcher + `rpc.js` client already exist; reuse beats reinvention.
