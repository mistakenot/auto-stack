# Solution: Task 024

> Phase 1 of the Planning Docs Dashboard epic (`docs/epics/002-planning-docs-dashboard.md`).
> Backend additions to `auto-ui`, a one-line bus derivation-coverage change in `auto-shared/bus`,
> and an `AUTO_UI_PORT` tweak in `auto-cli`. Ships as **one PR**.

## Approach

The design follows the epic's pinned decisions. Each sub-task is a small, additive change that
reuses existing seams (`resolveRoot`, the registry provider option, the `handleRPC` ingest, the
`web.FS()`/`web.Mode` split). Nothing in the task-021-owned signal path (`Hub`, `Event` envelope,
`DeriveDocChanged` *rules*) changes except the single allowed `isDocPath` coverage widening (1.4).

1. **Parameterize doc-path validation (1.2 — land first).** Change `cleanDocPath(p)` →
   `cleanDocPath(p string, allowed ...string)` so each caller picks its extension policy, and
   change `walkDocs` to collect `.md` **and** `.html`, tagging each `docEntry` with a `Type`
   field. `doc.list` allows `{.md, .html}`; `doc.get` keeps `{.md}` (never returns HTML through
   its `markdown` field); the raw route (1.3) allows `{.html}`. The "under `docs/`" + traversal
   guard is unchanged in every case. This shared validator lands before 1.3 so the route builds on
   it rather than re-editing the same function.

2. **`project.list` RPC (1.1).** New `projectListHandler(regProvider)` registered beside
   `doc.list`/`doc.get` in `server.go`. Maps the registry's `[]ProjectRef` to
   `{id, name, path, remote}`; empty registry → `[]` (not an error). **The `remote` field is passed
   through `git.NormalizeRemoteURL` before it leaves the handler** — `project.list` is a UI boundary,
   so a credentialed registry value (e.g. `https://user:token@github.com/…`) must never reach the
   browser. This matches `FindProjectByRemote` (`projects.go:226,234`), which already normalizes
   both sides, and the standing project rule (normalize remotes before any bus/log/UI boundary).

<!-- RESOLVED(P2): project.list emits `remote` to the UI without normalizing — boundary rule dropped
REVIEW: `context.md:90-92` explicitly flags this: "remote must be normalized (git.NormalizeRemoteURL)
before entering any envelope/bus/UI boundary ... project.list re-emits the stored remote ... treat it
as a boundary." But this step (and plan.md step 2.1) just map the stored `remote` through verbatim —
no `git.NormalizeRemoteURL` call. `auto-shared/git/normalize.go:16` exists and is already used
defensively elsewhere in the registry (`projects.go:226,234` normalize both sides in
`FindProjectByRemote`). If a registry entry ever stored a credentialed remote (e.g.
`https://user:token@github.com/...`), `project.list` would leak it to the browser. This also matches
the standing project rule (normalize remotes before a bus/log/UI boundary). Either call
`git.NormalizeRemoteURL(ref.Remote)` in `projectListHandler` and add a test asserting a credentialed
remote is stripped, or state explicitly why trusting the stored value is acceptable here. The Files
table / Key signatures and plan Phase 2 should agree once decided.
AUTHOR: Adopted — `projectListHandler` now applies `git.NormalizeRemoteURL(ref.Remote)`
(`github.com/mistakenot/auto-shared/git`, confirmed at `normalize.go:16`) before emitting. Updated
this step, the Key-signatures note for `project.go`, and plan.md Phase 2 (step 2.1 normalizes; new
step asserts a credentialed remote is stripped to credential-free form). Empty registry still → `[]`.
-->


3. **Raw-bytes HTTP route (1.3).** New `handleDocRaw(regProvider)` mounted at `GET /api/doc/raw`,
   reusing `resolveRoot` (resolves optional `worktree`) and `cleanDocPath(path, ".html")`. Serves
   verbatim bytes with `Content-Type: text/html`. Rejects `.md`, traversal, and missing params.

4. **Widen `doc.changed` derivation to `.html` (1.4).** One-line change to `isDocPath` in
   `auto-shared/bus/derive.go` to accept `.html` as well as `.md`. Add an `.html` derive-test case.
   No envelope/wire-shape change — coverage only.

5. **Harness launch + isolation (1.5).** In `serve.go`: switch from `srv.ListenAndServe()` to
   `net.Listen("tcp", addr)` + `srv.Serve(ln)` so `--port 0` yields a real bound port from
   `ln.Addr()`. Add `--ready-file <path>` (writes `{"addr":"127.0.0.1:NNNN"}\n` after bind),
   honor `AUTO_UI_PORT` (env, lowest precedence under explicit `--port`), and add `--projects
   <path>` / `AUTO_PROJECTS_PATH` to point the registry provider at a fixture registry instead of
   `~/.auto/projects.json`.

6. **`AUTO_UI_PORT` on the producer (1.5 cont.).** `auto-cli`'s `uiPort()` honors `AUTO_UI_PORT`
   first so a harness can pin `auto hooks fire` and the test server to the same ephemeral port.

7. **Synthetic emit helper (1.6).** New `auto ui emit --project <id> --path docs/.../x.md
   [--worktree …]` command: builds a valid `agent.tool.post` envelope (`bus.NewEvent`, RFC3339
   time, `ToolPost{paths:[{rel,abs}]}`) and POSTs the JSON-RPC notification to
   `http://127.0.0.1:<port>/api/rpc` with **no `Origin` header** (so the ingest accepts it). Port
   resolves via `--port`/`AUTO_UI_PORT`. Quickstart documents the Origin "trigger-via-CLI" rule
   and the real-edit recipe.

8. **Server-side debug buffer (1.7).** A small fixed-size ring buffer recorded in `handleRPC`
   (the ingest point — keeps the task-021 `Hub` untouched) capturing every raw + derived event.
   Gated by a new `WithDebug(bool)` server option (set by `serve.go` from `AUTO_UI_DEBUG`). Route
   `GET /api/debug/recent` returns the last N events as JSON when enabled, else `404`.

## Files

```
~ auto-ui/internal/server/docs.go         # docEntry +Type; walkDocs collects .md/.html; cleanDocPath(p, allowed...) parameterized; doc.list {.md,.html}, doc.get {.md}
+ auto-ui/internal/server/project.go      # projectListHandler(regProvider) -> []{id,name,path,remote}
+ auto-ui/internal/server/raw.go          # handleDocRaw(regProvider): GET /api/doc/raw, text/html, {.html} only
+ auto-ui/internal/server/debug.go        # ring buffer type + handleDebugRecent (gated)
~ auto-ui/internal/server/server.go       # register project.list; WithDebug option; mount /api/doc/raw + /api/debug/recent; pass buffer to handleRPC
~ auto-ui/internal/server/rpc_ingest.go   # record raw + derived events into debug buffer when enabled (after Broadcast/Derive)
~ auto-shared/bus/derive.go               # isDocPath: accept .html as well as .md (coverage only)
~ auto-shared/bus/derive_test.go          # add .html derive case; assert non-docs/ .html does not derive
~ auto-ui/internal/cli/serve.go           # net.Listen+Serve for --port 0; --ready-file (JSON); AUTO_UI_PORT; --projects / AUTO_PROJECTS_PATH; WithDebug(AUTO_UI_DEBUG)
+ auto-ui/internal/cli/emit.go            # newEmitCmd -> `auto ui emit`
~ auto-ui/internal/cli/root.go            # register emit command
~ auto-ui/internal/cli/quickstart.go      # document emit, Origin rule, harness flags, AUTO_UI_DEBUG
~ auto-ui/internal/cli/docs.go            # document new flags + emit subcommand
~ auto-cli/cmd/auto/hookscmd.go           # uiPort() honors AUTO_UI_PORT first
+ auto-ui/internal/server/project_test.go # AC-1
+ auto-ui/internal/server/raw_test.go     # AC-3
+ auto-ui/internal/server/debug_test.go   # AC-7
~ auto-ui/internal/server/docs_test.go    # AC-2: .html discovery, type field, doc.get still rejects .html, traversal still rejected
+ auto-ui/internal/cli/serve_test.go      # AC-5: --port 0 + --ready-file JSON; AUTO_UI_PORT; --projects isolation
+ auto-ui/internal/cli/emit_test.go       # AC-6: envelope shape + POST (no Origin) derives one doc.changed
~ auto-cli/cmd/auto/hookscmd_test.go      # AC-5: uiPort() honors AUTO_UI_PORT
```

### Key signatures (outline)

```go
// docs.go
type docEntry struct {
    ID   string `json:"id"`
    Path string `json:"path"`
    Type string `json:"type"` // "markdown" | "html"
}
func cleanDocPath(p string, allowed ...string) string // "" if not under docs/ or ext not in allowed
func walkDocs(root string) ([]docEntry, error)        // .md -> markdown, .html -> html

// project.go  (remote passed through git.NormalizeRemoteURL before emit — UI boundary)
func projectListHandler(reg func() config.ProjectsConfig) Handler // -> []map{id,name,path,remote}

// raw.go  (GET /api/doc/raw?project=&path=&worktree=)
func handleDocRaw(reg func() config.ProjectsConfig) http.HandlerFunc

// debug.go
type debugBuffer struct{ /* mutex + ring of recorded events, cap N */ }
func (b *debugBuffer) record(ev bus.Event)
func handleDebugRecent(b *debugBuffer, enabled bool) http.HandlerFunc // 404 when !enabled

// server.go
func WithDebug(enabled bool) Option

// derive.go
func isDocPath(rel string) bool {
    return strings.HasPrefix(rel, "docs/") &&
        (strings.HasSuffix(rel, ".md") || strings.HasSuffix(rel, ".html"))
}
```

## Test Coverage

| AC   | Test Type   | File                                         |
|------|-------------|----------------------------------------------|
| AC-1 | integration | auto-ui/internal/server/project_test.go      |
| AC-2 | unit        | auto-ui/internal/server/docs_test.go         |
| AC-3 | integration | auto-ui/internal/server/raw_test.go          |
| AC-4 | unit        | auto-shared/bus/derive_test.go               |
| AC-5 | integration | auto-ui/internal/cli/serve_test.go + auto-cli/cmd/auto/hookscmd_test.go |
| AC-6 | e2e         | auto-ui/internal/cli/emit_test.go            |
| AC-7 | integration | auto-ui/internal/server/debug_test.go        |

## Out of Scope

- The explorer UI, client-side liveness, the `doc.js` match fix, `window.__autoui`, the `/debug`
  SPA page (Phases 2/3).
- Auth / multi-tenant / hardening (only the existing free traversal guard is kept).
- Multi-host aggregation.
- Any bus envelope / hook-production / `doc.changed` wire-shape change — the sole exception is the
  `isDocPath` `.html` coverage widening (1.4).
- A new file watcher in auto-ui.
- **Technical:** no change to `auto-shared/bus` `Hub` or `Event` types; the debug buffer taps the
  ingest handler, not the Hub. No new env-override hook in `auto-shared/config` — `--projects` /
  `AUTO_PROJECTS_PATH` is resolved in `serve.go` and handed to the existing registry-provider
  option.

## Rejected Alternatives

- **Tap the `Hub` for the debug buffer (1.7).** Rejected — would modify task-021-owned
  `auto-shared/bus`. Recording at `handleRPC` captures exactly the ingest→derive result (what 1.7
  needs to confirm) and keeps the Hub untouched.
- **Keep `srv.ListenAndServe()` and scrape the stderr banner for the port.** Rejected — racy and
  brittle; `net.Listen` + `ln.Addr()` gives the real bound port deterministically, which is the
  whole point of `--ready-file`.
- **Add an `AUTO_PROJECTS_PATH` override inside `auto-shared/config`.** Rejected — broadens shared
  config for one consumer; resolving the path in `serve.go` and feeding the existing
  `WithRegistryProvider` option is the minimal change.
- **A separate `bus.Event`-recording middleware/Sink for 1.7.** Rejected as over-engineered for a
  trusted-host debug aid; a small in-handler ring buffer behind one option flag is enough.
- **Make `doc.get` return HTML with a `type` field.** Rejected per epic decision 1 — HTML must not
  flow through the sanitize-and-inline markdown path; it rides the raw route only.
