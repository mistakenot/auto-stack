# Plan: Task 021

## Summary
Build a `auto-shared/bus` package (CloudEvents-shaped typed envelope + Hub broadcast + `doc.changed` derivation), host it in the auto-ui server behind a new `POST /api/rpc` ingest and workspace-aware `doc.list`/`doc.get` RPCs, migrate `auto hooks fire` to publish the standard envelope with git/workspace provenance, add a live-reloading doc view, and write the spec.

## Wire format (decided — drives every phase)
An **event** on the wire is a JSON-RPC **notification**, identical inbound (publish) and outbound (broadcast):
```json
{ "jsonrpc": "2.0", "method": "<event.type>", "params": { …CloudEvents envelope… } }
```
- `method` = the dotted event type (`agent.tool.post`, `doc.changed`) → client `on("doc.changed", …)` works directly; broadcast-everything still holds (every event goes to every client; `on` is just client-side interest).
- `params` = the full self-contained envelope.
- **Publish:** producer POSTs this exact frame to `/api/rpc` (fire-and-forget). Handler parses → validates inner envelope → `Hub.Broadcast` → derives `doc.changed` → broadcasts that too. Malformed → HTTP 400 + JSON-RPC error body, **not broadcast**; valid → 204.
- **RPC** (`doc.list`/`doc.get`/`ping`) are normal id-bearing JSON-RPC requests over `/api/ws`.
- **Type authority + HTTP deviation:** the inbound `method` and `params.type` should agree; the handler treats **`params.type` as authoritative** (broadcast re-emits via `ev.AsNotification()` using `ev.Type`, so inbound `method` is advisory). The malformed-frame **400 + error body on `/api/rpc` is a deliberate deviation** from pure JSON-RPC (a notification normally gets no reply) — justified for an HTTP one-shot binding and stated explicitly in the spec (AC-1 / step 5.1).

<!-- RESOLVED(P3): publish frame is an id-less notification yet gets an error response — document the deviation + method/type authority
REVIEW: Per JSON-RPC 2.0, a notification (no `id`) must NOT receive a response — and the existing WS dispatcher honours this (`dispatch` returns `nil,false` for id-less, rpc.go:87-99). The `/api/rpc` publish frame is id-less, but plan 2.3 returns a 400 + `rpcError` for malformed ones. That's a reasonable pragmatic choice for an HTTP transport, but it IS a deviation that the spec (AC-1) must state explicitly. Also: the wire frame carries both `method` (= event type) and `params.type` (envelope type). Decide and document which is authoritative on ingest — the broadcast re-emits `ev.AsNotification()` using `ev.Type`, so inbound `method` is effectively ignored; the handler should validate they agree (or document that `params.type` wins).
AUTHOR: Both pinned. Added a "Type authority + HTTP deviation" bullet to the Wire format section: `params.type` is authoritative (inbound `method` advisory), and the 400-on-malformed is an explicit, documented deviation for the HTTP binding. Step 2.3 now states the authority rule; step 5.1 requires the spec to call out the deviation. -->


## Changes
| Symbol | File | Description |
|--------|------|-------------|
| + | auto-shared/bus/event.go | `Event` envelope, `SpecVersion`, `Validate() []ValidationError`, `AsNotification()`, `NewEvent` ctor |
| + | auto-shared/bus/payloads.go | `PathRef{Rel,Abs}`, `ToolPost{Tool,Event,Paths,Raw}` (normalized interface + verbatim raw), `DocChanged{Project,Path,AbsPath,Worktree,Branch}` + encode/decode helpers |
| + | auto-shared/bus/hub.go | `Sink` interface, `Hub{Subscribe(Sink) func(); Broadcast(Event)}` (decoupled from auto-ui) |
| + | auto-shared/bus/derive.go | `DeriveDocChanged(ev Event, reg ProjectsConfig) []Event` — `docs/**/*.md` matcher |
| + | auto-shared/bus/*_test.go | event/hub/derive unit tests |
| ~ | auto-shared/config/projects.go | + `FindProjectByRemote(remote string) *ProjectRef` |
| ~ | auto-shared/git/detect.go | + `Provenance(dir)` — combined `rev-parse` → root/branch/commit (≤1 subprocess) |
| ~ | auto-ui/internal/server/server.go | build `Hub` + shared `Dispatcher` (register `doc.list`/`doc.get`/`ping`), load registry, mount `POST /api/rpc`, pass hub+dispatcher into `handleWS` |
| ~ | auto-ui/internal/server/ws.go | `session` implements `bus.Sink`; register/deregister with hub on connect/teardown; use shared dispatcher |
| + | auto-ui/internal/server/rpc_ingest.go | `handleRPC`: parse frame → validate → broadcast → derive → broadcast |
| + | auto-ui/internal/server/docs.go | `doc.list`/`doc.get` handlers (workspace-aware, `docs/**/*.md`, path-traversal guard) |
| ~ | auto-cli/cmd/auto/hookscmd.go | emit `bus.Event` (provenance + rel/abs paths) to `/api/rpc`; drop `/api/hooks` |
| ~ | auto-cli/cmd/auto/hookscmd_test.go | assert posted frame is a valid `agent.*` bus envelope |
| ~ | auto-ui/web/static/index.html | add `marked` to importmap |
| ~ | auto-ui/web/static/app.js | `DocView` + nav link + render case + `on("doc.changed")` reload |
| + | auto-ui/web/static/doc.js | doc view module (`doc.list` picker, `doc.get` render, live reload) |
| + | docs/auto-bus-spec.md | the standard: envelope, framing, bindings, contract, type registry, `watch.task.*` paper map |

## Links
- [Requirements](./requirements.md) · [Solution](./solution.md) · [Context](./context.md)

## How to Test
- [ ] `auto-shared/bus/event_test.go` — envelope validation (required fields, dotted type), opaque `data` round-trip, `AsNotification` shape, typed payload encode/decode, `ToolPost` raw+normalized round-trip
- [ ] `auto-shared/bus/hub_test.go` — fan-out to N sinks, slow-sink drop never blocks, deregister
- [ ] `auto-shared/bus/derive_test.go` — `docs/**/*.md` match → `doc.changed`; non-md / outside-`docs/` / unregistered-project → none
- [ ] `auto-shared/config/projects_test.go` — `FindProjectByRemote` hit/miss/normalization
- [ ] `auto-ui/internal/server/rpc_ingest_test.go` — POST valid frame → connected WS client receives raw event + derived `doc.changed`; malformed → 400, no broadcast
- [ ] `auto-ui/internal/server/docs_test.go` — `doc.list` returns rel paths under `docs/`; `doc.get` returns raw markdown; traversal (`../`) rejected; `worktree` param reads from that root
- [ ] `auto-cli/cmd/auto/hookscmd_test.go` — `echo <payload> | auto hooks fire --agent claude` posts a valid `agent.tool.post` envelope with provenance + `{rel,abs}` paths + `raw` verbatim passthrough; still exits 0 when UI down
- [ ] Manual e2e (Phase 5): dev server + simulated hook fire → open doc reloads; other-doc edit does not

## Execution Sequence
```
Phase 1 (auto-shared/bus lib) ──┬──> Phase 2 (auto-ui hub + /api/rpc + doc RPCs) ──> Phase 4 (UI doc view) ──┐
                                └──> Phase 3 (auto-cli producer migration) ──────────────────────────────────┴──> Phase 5 (spec + e2e dogfood)
```
Phases 2 and 3 both depend only on Phase 1 and touch disjoint modules (auto-ui vs auto-cli) — but **dispatch serially in the shared worktree** and verify files on disk before the next (task 017/018 lesson). Phase 5 depends on 2, 3, 4.

## Plan

### Phase 1: Shared bus library + lookups (auto-shared)
- [x] Step 1.1: `auto-shared/git/detect.go` — add `Provenance(dir) (root, branch, commit string, err error)` via a single `git rev-parse --show-toplevel --abbrev-ref HEAD HEAD` (one subprocess for all three), tolerating unborn HEAD / non-repo (empties, mirror `RepoRoot` error tolerance). This bounds the per-fire git overhead to ≤2 spawns (`Provenance` + `OriginRemote`). Verify: `cd auto-shared && go build ./git/...`.
- [x] Step 1.2: `auto-shared/config/projects.go` — add `FindProjectByRemote(remote string) *ProjectRef` (normalize both sides via `git.NormalizeRemoteURL`, exact match, nil on miss). Verify: `go build ./config/...`.
- [x] Step 1.3: `auto-shared/bus/event.go` — `const SpecVersion="1.0"`; `Event` struct (specversion, type, source, id, time + project, session, remote, branch, worktree, commit + `Data json.RawMessage`); `NewEvent(type,source string, data any) (Event,error)` (sets specversion/id/time, marshals data); `Validate() []ValidationError` (require specversion, dotted `type` via `^[a-z0-9]+(\.[a-z0-9]+)+$`, source, id, time-RFC3339); a bus-defined `Notification{JSONRPC,Method,Params Event}` type + `AsNotification() Notification` → `{jsonrpc:"2.0",method:Type,params:Event}` (concrete type — auto-shared cannot reference auto-ui's unexported `rpcRequest`; `session.enqueue(any)`/`wsjson.Write` marshal it). Reuse `config.ValidationError`. Verify: `go build ./bus/...`.
- [x] Step 1.4: `auto-shared/bus/payloads.go` — `PathRef{Rel,Abs string}`; `ToolPost{Tool,Event string; Paths []PathRef; Raw json.RawMessage}` (normalized cross-tool interface + verbatim raw passthrough of the agent's original tool fields); `DocChanged{Project,Path,AbsPath,Worktree,Branch string}`; `Decode[T](Event)(T,error)` helper (or per-type `AsToolPost`). Verify: `go build ./bus/...`.
- [x] Step 1.5: `auto-shared/bus/hub.go` — `type Sink interface { Deliver(Event) }`; `Hub` with `sync.RWMutex` + `map[*sub]struct{}`; `NewHub()`, `Subscribe(Sink) (cancel func())`, `Broadcast(Event)` (snapshot under RLock, call `Deliver` outside lock). No auto-ui imports. Verify: `go build ./bus/...`.
- [x] Step 1.6: `auto-shared/bus/derive.go` — `DeriveDocChanged(ev Event, reg config.ProjectsConfig) []Event`: only for `type=="agent.tool.post"`; **`reg` is the authority** — derive only when `reg.FindProjectByID(ev.Project) != nil` (do not trust a non-empty inbound `ev.Project` the hub's registry doesn't contain); decode `ToolPost`; for each `PathRef` whose `Rel` (cleaned, reject `..`) satisfies `strings.HasPrefix(rel,"docs/") && strings.HasSuffix(rel,".md")` (so `docs/foo.md` with no subdir matches too), emit a `doc.changed` Event carrying `DocChanged` + same provenance. Verify: `go build ./bus/...`.

<!-- RESOLVED(P3): "ev.Project!="" (or resolves via reg)" is ambiguous — pin the registered-project check
REVIEW: AC-5 requires "unregistered projects derive nothing". The producer already sets `ev.Project` via `FindProjectByRemote`/`FindProjectByPath` (step 3.1), so trusting a non-empty `ev.Project` alone would derive for a project id the hub's `reg` doesn't actually contain (e.g. registry drift, or a producer on a host with a different registry). Spell out the rule: derive only when the project is present in `reg` (`reg.FindProjectByID(ev.Project) != nil`), using `reg` as the authority rather than trusting the inbound `ev.Project`. Also confirm `docs/foo.md` (markdown directly under `docs/`, no subdir) matches — a naive `**` glob or a `HasPrefix("docs/")+HasSuffix(".md")` check both work, but state which so the zero-subdir case isn't missed.
AUTHOR: Step 1.6 reworded: `reg` is the authority — derive only when `reg.FindProjectByID(ev.Project) != nil`; and the match is `strings.HasPrefix(rel,"docs/") && strings.HasSuffix(rel,".md")` (cleaned, reject `..`), which explicitly covers the zero-subdir `docs/foo.md` case. -->

- [x] Step 1.7: Write `event_test.go`, `hub_test.go`, `derive_test.go`, extend `projects_test.go` (per How to Test). Verify: `cd auto-shared && go test ./...` passes; `gofmt -l .` empty.
- [x] Step 1.8: Commit: `feat(021): phase 1 — auto-shared/bus envelope, hub, derive + registry/git lookups`

### Phase 2: auto-ui hub host + ingest + doc RPCs (auto-ui)
- [x] Step 2.1: `server.go` — add an injection seam: `New(fsys, mode, opts...)` accepts a registry provider `func() config.ProjectsConfig`; the default (no opt) returns an **empty** registry so existing `server_test.go`/`ws_test.go` stay hermetic (never read the dev's real `~/.auto/projects.json`, which holds a live PAT). `serve.go:62` injects a provider that **re-reads** `config.ProjectsConfigPath()` per ingest/doc call (the JSON is tiny) so projects registered *after* startup resolve — the server is long-lived. In `New`: `hub := bus.NewHub()`, `d := newDispatcher()`, register `ping` (move from ws.go), `doc.list`, `doc.get`; mount `mux.HandleFunc("/api/rpc", handleRPC(hub, regProvider))`; pass `hub`+`d` into `handleWS`. Verify: `cd auto-ui && go build ./...`.

<!-- RESOLVED(P2): loading the registry inside New() breaks unit-test hermeticity
REVIEW: `server.New(fsys, mode)` is called by `server_test.go` (5×) and `ws_test.go` (2×) with no registry. If `New` internally calls `config.LoadProjects(config.ProjectsConfigPath())`, those tests will read the developer's real `~/.auto/projects.json` (which on this host contains the live auto-stack project + a real PAT). The new `rpc_ingest_test`/`docs_test` would then pass or fail depending on host state, not the fixture. Add an injection seam: pass the registry (and/or the `*bus.Hub`) into `New` (extra param or functional option) so tests inject a `config.ProjectsConfig` fixture, and have `serve.go:62` do the real load.
AUTHOR: Step 2.1 now adds a functional-option registry provider `func() config.ProjectsConfig`. Default (no opt) returns an empty registry, so existing `server_test.go`/`ws_test.go` stay hermetic and the new tests inject a fixture; only `serve.go:62` wires the real load. -->

<!-- RESOLVED(P2): registry is loaded once at startup and never refreshed
REVIEW: `server.New` runs once per `auto ui serve` (serve.go:62) and there's no reload mechanism. A project registered *after* the server starts (`auto watch init`, `auto init`) won't resolve: `doc.changed` derivation and `doc.list`/`doc.get` (which call `FindProjectByID`/`FindProjectByRemote`) will silently miss it until restart. Given the dogfooding daemon model ([[project_dogfood_daemon_commits]]), the server is long-lived. State this as a known v1 limitation, or reload the registry per-request / on file change. The Phase 5 e2e happens to work only because auto-stack is already registered.
AUTHOR: Solved by the same seam as the hermeticity fix: the injected provider is a `func() config.ProjectsConfig` that re-reads `config.ProjectsConfigPath()` per ingest/doc call (the JSON is tiny), so post-startup registrations resolve without a restart. Step 2.1 + 2.3 updated to call `regProvider()` per request rather than capturing a snapshot. -->

- [x] Step 2.2: `ws.go` — give `session` a stored `ctx`; add `Deliver(ev bus.Event)` calling `s.enqueue(s.ctx, ev.AsNotification())`; in `handleWS` use the shared dispatcher (param) instead of building one; `cancel := hub.Subscribe(s)` after `newSession`, `defer cancel()` alongside teardown. Verify: `go build ./...`; existing `ws_test.go`/`rpc_test.go` still pass (`go test ./internal/server/...`).
- [x] Step 2.3: `rpc_ingest.go` — `handleRPC(hub, regProvider) http.HandlerFunc`: POST only; read body; unmarshal JSON-RPC notification; extract `params`→`bus.Event` (**`params.type` is authoritative**; if the inbound frame's `method` disagrees, prefer `params.type`); `Validate()` → on error write 400 + `rpcError` (a deliberate HTTP deviation from JSON-RPC notification-gets-no-reply semantics — documented in the spec, step 5.1); else `hub.Broadcast(ev)`, then `for _, d := range bus.DeriveDocChanged(ev, regProvider()) { hub.Broadcast(d) }`; write 204. Verify: `go build ./...`.
- [x] Step 2.4: `docs.go` — `doc.list` handler `{project?, worktree?}` → resolve root, walk and return only `docs/**/*.md` as `[{id, path(rel)}]` (no bodies); `doc.get` `{project, path, worktree?}` → restrict to the **same `docs/**/*.md` set** (reject anything outside `docs/` or non-`.md`, plus clean+guard `..` within root) so a loopback client can't read `.env`/secrets, return `{path, markdown}` (raw). **Root resolution validates `worktree`**: it must be a registered project/worktree path (resolve via `FindProjectByRemote`, or fall back to `FindProjectByID(project).Path`) — never accept an arbitrary client-supplied absolute path as the read root. Diagnostics never leak absolute FS errors beyond message. Verify: `go build ./...`.

<!-- RESOLVED(P3): doc.get read surface is wider than "docs markdown" and `worktree` is an unvalidated FS root
REVIEW: As written, `doc.get` "clean+guard path within root" confines reads to the resolved root and rejects `../`, but does not restrict to `docs/**/*.md`. A loopback client could then read any file under the root (e.g. `.env`, `auto-shared/...secrets`). Restrict `doc.get` to the same `docs/**/*.md` set `doc.list` exposes. Separately, the `worktree` param is a client-supplied absolute path used directly as the root — a client can point it at any directory on the box. Loopback + same-user limits the blast radius, but consider validating `worktree` against a known project/worktree (e.g. it must resolve via `FindProjectByRemote` or be a registered path) rather than accepting arbitrary roots.
AUTHOR: Step 2.4 now (a) restricts `doc.get` to the same `docs/**/*.md` set as `doc.list` (reject outside-`docs/`/non-`.md` in addition to the `..` guard), and (b) validates `worktree` against a registered project/worktree (via `FindProjectByRemote` or `FindProjectByID(project).Path`) instead of accepting an arbitrary client-supplied root. -->


- [x] Step 2.5: `rpc_ingest_test.go` + `docs_test.go` — httptest server + `websocket.Dial` client: POST valid frame, assert client receives `agent.tool.post` then `doc.changed` (via `readUntil`); POST malformed → 400, client receives nothing; `doc.list`/`doc.get` happy + traversal-rejected. Verify: `cd auto-ui && go test ./...`; `gofmt -l .` empty.
- [x] Step 2.6: Commit: `feat(021): phase 2 — hub broadcast, /api/rpc ingest, doc.changed derivation, doc.list/get`

### Phase 3: Migrate auto hooks fire to the bus envelope (auto-cli)
- [ ] Step 3.1: `hookscmd.go` — replace `HookEvent` build with `bus.Event`: `type="agent."+lower(event-class)` (e.g. `agent.tool.post` for PostToolUse with a tool, `agent.session.start` for SessionStart; map via small switch, default `agent.<lower(event)>`), `source="auto/hooks/"+agent`; populate provenance from the **payload `cwd`** (not the hook process cwd): `root,branch,commit = git.Provenance(cwd)`, `worktree=root`, and `remote = git.NormalizeRemoteURL(git.OriginRemote(cwd))` — **always normalized; never emit the raw origin URL** (it can embed a PAT and the bus broadcasts to all clients); resolve `project` via `FindProjectByRemote(remote)`, falling back to `FindProjectByPath(root)` then `cwd`. Verify: `cd auto-cli && go build ./...`.
- [ ] Step 3.2: `hookscmd.go` — evolve path extraction to `[]bus.PathRef{Rel,Abs}`: resolve each tool path against the **payload `cwd`** — `abs = p if filepath.IsAbs(p) else filepath.Join(cwd, p)` (Claude's `file_path` is usually absolute, Codex may be relative — handle both), then `Rel = filepath.Rel(root, abs)` with `root` from step 3.1's `Provenance(cwd)`. Build `ToolPost` with the normalized fields **and** set `Raw` to the agent's original `tool_name`/`tool_input` (verbatim `json.RawMessage`); pack as `data`. Best-effort: the POST keeps its 150ms timeout, git work is ≤2 spawns (step 1.1) and omits-on-error. Verify: `go build ./...`.

<!-- RESOLVED(P3): resolve `abs` against the payload cwd, not the hook process cwd
REVIEW: `filepath.Abs(p)` resolves a relative path against the *hook process's* working directory, which is not guaranteed to equal the agent's `cwd` (the hook may be spawned from elsewhere). buildHookEvent already reads `cwd` from the payload (hookscmd.go:107) and falls back to `os.Getwd()`. For path resolution, join relative tool paths against that payload `cwd` (and derive `root` from the same `cwd` via `RepoRoot(cwd)`), then `Rel=filepath.Rel(root, abs)`. Otherwise `rel` can come out wrong and the `docs/**` match (and live-reload) silently fails. Note Claude's `tool_input.file_path` is usually already absolute; Codex may differ — handle both.
AUTHOR: Step 3.2 reworked: `abs = p if filepath.IsAbs(p) else filepath.Join(cwd, p)` resolves against the payload `cwd` (handles Claude-absolute and Codex-relative), and `root` comes from `Provenance(cwd)` in step 3.1 — both keyed off the payload `cwd`, not the hook process cwd, so `rel` is correct. -->

- [ ] Step 3.3: `hookscmd.go` — `postHookEvent` posts `ev.AsNotification()` JSON to `http://127.0.0.1:<port>/api/rpc`; delete the `/api/hooks` URL. Confirm no remaining `/api/hooks` reference anywhere (`grep -rn '/api/hooks'` → only removed). Verify: `go build ./...`.
- [ ] Step 3.4: Update `hookscmd_test.go`: `TestPostHookEventDelivers` asserts the received body unmarshals to a valid `bus.Event` with `type` prefix `agent.`, populated paths `{rel,abs}`; keep `TestFireExitsZeroWhenUIDown`. Verify: `cd auto-cli && go test ./...`; `gofmt -l .` empty.
- [ ] Step 3.5: Commit: `feat(021): phase 3 — auto hooks fire publishes standard bus envelope to /api/rpc`

### Phase 4: Live-reloading doc view (auto-ui SPA)
- [ ] Step 4.1: `index.html` — add `"marked":"https://esm.sh/marked@13.0.0"` to the importmap. Verify: served page loads marked without console error (checked in 4.4).
- [ ] Step 4.2: `doc.js` — export `DocView({params})`: read `project`/`path`/`worktree` from params; on mount `call("doc.list",{project,worktree})` for the picker and, if `path` set, `call("doc.get",{project,path,worktree})` → render `marked.parse(markdown)`; subscribe `on("doc.changed", ev => { reload when ev.project===open.project && ev.path===open.path && (!open.worktree || ev.worktree===open.worktree) })` — a missing `worktree` on the open doc matches any worktree, so a deep-link opened without `worktree` still reloads (the Phase 5 e2e), while a two-worktree scenario still discriminates; on a match re-`doc.get`+re-render; unsubscribe on unmount. Verify: `cd auto-ui && go build -tags dev ...` serves the file.
- [ ] Step 4.3: `app.js` — import `DocView`, add `link("doc","Docs")` to `Nav`, add `view==="doc"` render case passing `params`. Verify: hash `#/doc?...` renders the view.
- [ ] Step 4.4: Manual smoke (dev server): `go build -tags dev -o bin/auto ./auto-cli/cmd/auto`; `cd auto-ui && ../bin/auto ui serve`; open `#/doc?project=auto-stack&path=docs/tasks/021-auto-bus-standard/plan.md` → renders; `doc.list` lists docs. Verify: doc content visible, no console errors.
- [ ] Step 4.5: Commit: `feat(021): phase 4 — doc view with client-side markdown + live reload on doc.changed`

### Phase 5: Spec doc + end-to-end dogfood
- [ ] Step 5.1: Write `docs/auto-bus-spec.md` — sections: Envelope (fields + provenance attrs + typing discipline + the `remote`-must-be-normalized credential rule), Framing (JSON-RPC 2.0, the wire-format above, incl. the `/api/rpc` 400-on-malformed HTTP deviation from notification-gets-no-reply, and `params.type` authority), Bindings (HTTP POST `/api/rpc`, WS `/api/ws`; future: unix socket, `subscribe()`, headless `auto bus serve`), Delivery contract (at-most-once, invalidations-not-state), Event-type registry (`agent.*`, `doc.*`, `watch.*`), and a paper mapping of `watch.task.started`/`watch.task.failed` onto the envelope (provenance + `TaskID`/`RunID` in data). Verify: all 6 AC-1 elements present (self-checklist at top of doc).
- [ ] Step 5.2: End-to-end: with dev server running and `bin/auto` on PATH, open the doc view on `plan.md`, then run `echo '{"hook_event_name":"PostToolUse","tool_name":"Edit","cwd":"<repo>","tool_input":{"file_path":"<repo>/docs/tasks/021-auto-bus-standard/plan.md"}}' | bin/auto hooks fire --agent claude` after touching the file → browser reloads the doc. Run again for a *different* docs file → open doc does **not** reload. Verify: both behaviors observed (satisfies AC-6).
- [ ] Step 5.3: `auto doc fix` to regenerate the root CLAUDE.md Documentation Index (don't hand-edit). Verify: index lists `auto-bus-spec.md`; `git diff` shows only generated index change.
- [ ] Step 5.4: Full gate: `make build && make test && make vet`. Verify: all green.
- [ ] Step 5.5: Commit: `feat(021): phase 5 — auto-bus spec + end-to-end live-reload dogfood`

## Success Criteria
- [ ] `make build`, `make test`, `make vet` all pass
- [ ] AC-1: `docs/auto-bus-spec.md` covers framing, envelope+provenance, both bindings, at-most-once contract, type registry, and the `watch.task.*` paper map
- [ ] AC-2: `auto-shared/bus` envelope validates + serializes as a JSON-RPC notification; `data` round-trips opaque; typed payload helpers exist; unit tests green
- [ ] AC-3: POST to `/api/rpc` fans out to every WS client; slow clients dropped (never block); malformed → 400, no broadcast
- [ ] AC-4: `auto hooks fire --agent claude` posts a spec-compliant `agent.*` envelope to `/api/rpc`, exits 0, and `/api/hooks` is gone
- [ ] AC-5: `agent.tool.post` with a `docs/**/*.md` path in a registered project derives `doc.changed`; non-md / outside-`docs/` / unregistered derive nothing
- [ ] AC-6: editing the open doc live-reloads the view; editing a different doc does not
- [ ] Workspace-correct: events carry `{remote, branch, worktree, commit}` + `{rel, abs}` paths; project resolves for worktree paths via `FindProjectByRemote`

## Open Questions
- (none — all resolved in requirements.md Q1–Q3 and the two workspace-identity decisions in solution.md)
