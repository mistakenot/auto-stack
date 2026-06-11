# Task 021: auto-bus-standard

## Problem

Auto-stack components are growing ad-hoc, incompatible communication paths: `auto hooks fire` POSTs a bespoke `HookEvent` to an auto-ui endpoint that doesn't exist, auto-watch traps its events in SQLite, and auto-reflect invented its own JSONL envelope. There is no shared standard for push events or two-way RPC, and the web UI has no way to know when an agent changes something it is rendering.

## Goals

- Define one unified communication standard ("auto bus") for all auto-stack components: streaming push events + two-way JSON-RPC over pluggable transports.
- Layering: transports (HTTP POST `/api/rpc` for one-shot publish; WebSocket `/api/ws` for bidirectional — already built) carry JSON-RPC 2.0 frames. Requests/responses = RPC; notifications = events.
- Event envelope: CloudEvents-shaped (`specversion`, `type`, `source`, `id`, `time`, `data` + `project`/`session` extension attributes). Shape only — no library dependency.
- Typing discipline: the **envelope is strongly typed** (a validated Go struct — the stable contract every component compiles against). The **`data` payload is typed only where we author it** — hub-derived events (`doc.changed`) and RPC results (`doc.list`/`doc.get`) get typed structs; **ingested agent hook bodies stay opaque** (`json.RawMessage`, carried verbatim) since their per-agent/per-tool schemas are complex and the bus is lossy anyway. The hub parses only the few fields it needs (e.g. `paths`).
- Preserve-and-normalize tool payloads: for tool events, `data` is **two-layer** — a `raw` field preserves the agent's *original* tool fields verbatim (no fidelity loss; each agent's `FileEdited`/`tool_input` schema differs), plus normalized fields (`tool`, `event`, `paths[]`) that copy the same values under one naming standard so every agent's event **conforms to a single shared interface**. Consumers code against the normalized interface; the raw layer stays available for tool-specific needs.
- Topology: bus implemented as an `auto-shared/bus` package, hosted inside the auto-ui server (it owns the port and WS layer).
- Subscription model v1: broadcast every event to all connected WS clients; filtering is client-side.
- Delivery contract: at-most-once, explicitly lossy. Events are invalidations, not state transfer; clients re-fetch state via RPC on reconnect. Canonical record remains ETL / SQLite.
- Event types: dotted hierarchy (`agent.tool.post`, `agent.session.start`, `doc.changed`, `watch.task.started`, …).
- Hub enrichment: the hub derives higher-level events from raw ones (e.g. `agent.tool.post` whose paths match `docs/**/*.md` in a registered project → `doc.changed {project, path}`) using the shared project registry.
- Prove the standard with the first use case: the web UI renders a planning doc and live-reloads it when a coding agent edits the file.

## Deliverables

1. `docs/auto-bus-spec.md` — envelope, framing, transport bindings, delivery contract, event type registry. Names the auto-watch daemon as the second adopter (implementation out of scope, but envelope sanity-checked on paper against `watch.task.*` shapes).
2. `auto-shared/bus` — envelope types, publisher helper, hub broadcast core.
3. auto-ui: `/api/rpc` ingest handler, `doc.changed` derivation, `doc.list` + `doc.get` RPCs (list = IDs/path metadata only; get = raw markdown, client-rendered), minimal doc-rendering view with live reload.
4. `auto hooks fire` migrated from the bare `HookEvent` POST to the standard envelope on `/api/rpc`.

## Acceptance Criteria

**AC-1**: Spec doc published
- Given: the repo docs tree
- When: `docs/auto-bus-spec.md` is read
- Then: it specifies the JSON-RPC 2.0 framing, the CloudEvents-shaped envelope (with `project`/`session` extensions), both v1 transport bindings, the at-most-once delivery contract, the dotted event-type registry, and a paper mapping of auto-watch `watch.task.*` events onto the envelope

**AC-2**: Shared bus package with typed envelope, opaque payload
- Given: `auto-shared/bus`
- When: a Go component constructs an event and publishes it via the publisher helper
- Then: the envelope is a strongly typed Go struct that validates (required attributes present, dotted type) and serializes as a JSON-RPC notification; `data` is carried as an opaque payload (`json.RawMessage`) so unknown/complex bodies round-trip verbatim, while helpers exist to construct typed payloads for hub-authored events; the `ToolPost` payload carries both a `raw` verbatim layer and normalized fields (`tool`/`event`/`paths`); unit tests cover envelope validation, opaque-payload round-trip, raw+normalized tool-payload round-trip, typed-payload construction, and hub fan-out

**AC-3**: Hub ingest + broadcast
- Given: auto-ui server running with ≥1 connected WS client
- When: a JSON-RPC event notification is POSTed to `/api/rpc`
- Then: every connected WS client receives the event as a JSON-RPC notification; slow clients are dropped, never block the hub; malformed frames get a JSON-RPC error response and are not broadcast

**AC-4**: Hooks publish the standard envelope
- Given: an agent hook payload on stdin
- When: `auto hooks fire --agent claude` runs
- Then: it POSTs a spec-compliant `agent.*` event to `/api/rpc` (fire-and-forget, always exit 0; the POST keeps its existing 150ms timeout, and the added git-provenance work is bounded to ≤2 subprocess calls and best-effort), and the producer's `/api/hooks` URL — a client-side constant only, no such server route was ever registered — is switched to `/api/rpc` in the same change (delete the constant + update its test)

<!-- RESOLVED(P3): "legacy /api/hooks path" only exists on the producer, not the server
REVIEW: I grepped for `/api/hooks` across the repo: the only references are `auto-cli/cmd/auto/hookscmd.go:203` (the producer URL constant) and `hookscmd_test.go:57` (the test). `auto-ui/internal/server/server.go:18-43` never registers an `/api/hooks` route — the current producer POSTs to an endpoint that 404s (which requirements.md itself notes: "an auto-ui endpoint that doesn't exist"). So "remove the legacy path" means deleting a client-side URL + updating one test, NOT removing a server handler. Reword to avoid sending the implementer hunting for a server route that was never built.
AUTHOR: Reworded AC-4 — clarified `/api/hooks` is a producer-side URL constant only (no server route was ever registered), so the change is "switch the constant to /api/rpc + update its test", not "remove a server handler". plan step 3.3 already phrases it as deleting the URL + grep-confirming no references remain. -->

<!-- RESOLVED(P3): 150ms "budget" only covers the HTTP POST, not the git provenance work added by this task
REVIEW: The existing 150ms budget (hookscmd.go:22 `hookPostTimeout`) bounds only the POST. solution.md step 2 / plan.md step 3.1 add four sequential git subprocess spawns per fire — `RepoRoot`, `CurrentBranch`, `CurrentCommit`, `OriginRemote` (each is a separate `exec.Command("git", ...)` via `runGit`, detect.go:34). PostToolUse fires after *every* tool call, so this is real added latency on the agent's hot path that the "150ms budget" wording hides. Consider bounding the git work too (one combined `git rev-parse --show-toplevel --abbrev-ref HEAD HEAD`, drop `commit` if not needed for v1, or cache per-cwd), and state the real overall budget.
AUTHOR: Took the combined-call suggestion. AC-4 wording now separates the POST's 150ms timeout from the provenance work, and bounds the latter: plan step 1.1 replaces the two separate helpers with one `git.Provenance(dir)` that runs a single `git rev-parse --show-toplevel --abbrev-ref HEAD HEAD` (root+branch+commit), so a fire spawns ≤2 git processes (Provenance + OriginRemote) instead of 4 — all best-effort, omit-on-error. -->


**AC-5**: doc.changed derivation
- Given: the hub receives `agent.tool.post` with `paths` containing a markdown file under `docs/` in a registered project
- When: the event is ingested
- Then: the hub broadcasts the raw event plus a derived `doc.changed {project, path}` event; non-markdown paths, markdown outside `docs/`, and unregistered projects derive nothing

**AC-6**: Live doc reload end-to-end
- Given: the auto-ui doc view rendering a planning doc in a browser
- When: a simulated hook fire reports an Edit to that doc's path
- Then: the view re-fetches via `doc.get` and re-renders the updated content without a manual refresh; edits to *other* docs do not trigger a reload of the open doc

<!-- RESOLVED(P2): worktree-strict matching will break the documented e2e (open doc won't reload)
REVIEW: plan.md step 4.2 matches `doc.changed` against the open doc on the triple `{project, path, worktree}`. But the e2e in plan.md step 5.2 (and the deep-link in 4.4) opens `#/doc?project=auto-stack&path=docs/.../plan.md` with NO `worktree` param. The hook fire in 5.2 runs from the repo and emits `worktree=/home/vscode/src/auto-stack`. A strict triple match then compares `undefined !== "/home/vscode/src/auto-stack"` → no reload → AC-6 fails in its own scripted scenario. Define the match precisely: treat a missing `worktree` on the open doc as "the registered project root", or make `worktree` optional in the match (match on `{project, path}` when worktree is absent).
AUTHOR: Pinned the match rule in plan step 4.2: reload when `project` and `path` match AND (`open.worktree` is absent OR equals `ev.worktree`). A missing `worktree` on the open doc matches any worktree, so the Phase 5 e2e (deep-linked without `worktree`) reloads correctly while a two-worktree scenario still discriminates. -->


## Out of Scope

- `subscribe()` filtering RPC, unix-socket binding, headless `auto bus serve` — documented as future extensions in the spec only.
- auto-watch daemon publishing to the bus (named second adopter; paper exercise only).
- Durability, replay, ordering guarantees, acks — the bus is intentionally lossy.
- Auth beyond loopback-only binding (remote access remains Tailscale serve's job).
- Rich doc browsing UI (index pages, search) — one minimal doc view is enough to prove the loop.

## Open Questions

- [x] Q1: Which files count as "docs" for `doc.changed` derivation and the v1 doc view — all `*.md` in a registered project, or only `docs/**`? (answered: only `docs/**/*.md` — keeps noisy README/CLAUDE.md churn off the bus's derived events)
- [x] Q2: Should the doc view in v1 navigate by URL only (`#/doc?project=…&path=…`) or also include a minimal doc list RPC (`doc.list`)? (answered: include minimal `doc.list` — IDs + path metadata only, the cheap rung per the resource pattern; plus deep-linkable hash URLs)
- [x] Q3: `doc.get` returns raw markdown rendered client-side, or server-rendered HTML? (answered: raw markdown, rendered client-side — keeps the RPC a pure data surface, consistent with JSON-output conventions; SPA uses a no-build ESM markdown renderer)
