# Context — 038: autowatch serves doc + project RPCs

Grounding for the Verification + Solution tabs. Everything below is verified against the
merged code on `main`. The task **reimplements** auto-ui's doc/project serving inside autowatch
(decision in Requirements), so the byte/shape behaviour of these files is the parity target.

## 1. auto-ui's current doc/project serving (the parity target)

All four operations live in `auto-ui/internal/server/` and are registered in
`server.go`:

- `auto-ui/internal/server/server.go:73` — `d.Register("doc.list", docListHandler(o.regProvider))`
- `auto-ui/internal/server/server.go:74` — `d.Register("doc.get", docGetHandler(o.regProvider))`
- `auto-ui/internal/server/server.go:75` — `d.Register("project.list", projectListHandler(o.regProvider))`
- `auto-ui/internal/server/server.go:99` — `mux.HandleFunc("/api/doc/raw", handleDocRaw(o.regProvider))` (HTTP GET, not RPC)

`regProvider` is `func() config.ProjectsConfig` — a snapshot accessor of the registry.

### doc.list — `docs.go:28-49`, `walkDocs` `docs.go:122-173`
- Param shape: `{project, worktree}` (`docs.go:30-33`).
- Resolves root via `resolveRoot` then `walkDocs(root)`.
- `walkDocs` walks `<root>/docs` (`docs.go:123`). For each file: `.md` → `type:"markdown"`,
  `.html` → `type:"html"`, anything else skipped (`docs.go:133-141`).
- Paths are relativised to root and forward-slashed (`docs.go:142-147`).
- For `.html` entries it opens the file and extracts `PlanMeta` from the first
  `io.LimitReader(f, MaxMetaPrefixBytes)` bytes (`docs.go:150-156`).
- Returns `[]docEntry{ID, Path, Type, Meta *PlanMeta `omitempty`}` (`docs.go:17-23`,
  `158-163`). `ID == Path == rel`.
- Missing `docs/` dir → **empty list, not an error** (`docs.go:166-170`).
- Errors → `&rpcError{Code: codeInternalError, ...}` (`docs.go:40,45`).

### doc.get — `docs.go:54-93`
- Param shape: `{project, path, worktree}` (`docs.go:56-60`).
- `p.Path == ""` → `&rpcError{Code: codeParseError, Message: "path is required"}` (`docs.go:65-67`).
- `cleanDocPath(p.Path, ".md")` — **markdown-only**. `""` → `codeParseError "invalid path"`
  (`docs.go:75-78`).
- `absPath := filepath.Join(root, filepath.FromSlash(cleaned))`; `os.ReadFile`. Read failure →
  `codeInternalError "doc not found"` (deliberately does not leak abs path) (`docs.go:82-86`).
- Success → `map[string]string{"path": cleaned, "markdown": string(data)}` (`docs.go:88-91`).
- **Deliberately refuses HTML** — only `doc.raw` serves `.html`.

### doc.raw — HTTP route `raw.go:18-65`
- Method guard: non-GET → `405` with `Allow: GET` (`raw.go:20-24`).
- Query params: `project, path, worktree`; `path` required → `400 "path is required"` (`raw.go:26-34`).
- `resolveRoot` failure → `400` with the error string (`raw.go:36-40`) — **note: 400, whereas
  doc.get maps the same failure to codeInternalError.**
- `cleanDocPath(reqPath, ".html")` — **html-only**; `""` → `400 "invalid path"` (`raw.go:42-47`).
- Read failure → `404 "doc not found"` (`raw.go:51-56`).
- Success → `Content-Type: text/html; charset=utf-8`, then writes bytes **verbatim**
  (`raw.go:58-63`; `//nolint:gosec` — verbatim HTML is the route's contract). The SPA renders it
  in a sandboxed iframe (`content.js` uses `<iframe src="/api/doc/raw?…">`).

### project.list — `project.go:27-41`
- No params. Maps `reg.Projects` to `projectEntry{ID, Name, Path, Remote}` (`project.go:11-17`).
- `Remote: git.NormalizeRemoteURL(ref.Remote)` — **credential stripping at the UI boundary**
  (`project.go:36`, comment `project.go:24-26`).
- Empty registry → empty array, never null, never error (`project.go:30,39`).
- **No `host` field today** — 038 adds it (decision in Requirements).

### Shared helpers (to be reimplemented in autowatch)
- `resolveRoot(reg, project, worktree)` — `docs.go:101-117`. Validation order:
  1. `worktree != ""` → must match a registered project via `reg.FindProjectByPath(worktree)`;
     returns `filepath.Clean(worktree)`, else `errors.New("worktree not found in registry")`.
  2. `project != ""` → `reg.FindProjectByID(project)`; returns `filepath.Clean(ref.Path)`, else
     `"project not found in registry"`.
  3. else → `"project or worktree is required"`.
  **An arbitrary client path is never accepted as a read root** — it must be registry-backed.
- `cleanDocPath(p, allowed...)` — `docs.go:177-208`. `path.Clean`; reject `..` prefix or `/..`
  (`docs.go:182-184`); strip leading `./` and `/`; **must be under `docs/`** (`docs.go:191-193`);
  must end in an allowed extension (`docs.go:196-205`); returns cleaned forward-slash path or `""`.
- `ExtractPlanMeta(r io.Reader) *PlanMeta` — `planmeta.go:37-128`. Tokenises the bounded prefix,
  extracts `<script id="pd-meta" type="application/json">` body (status/branch/epic/pr/created)
  and the first `<pd-doc status="...">` → `ReviewState`. Tolerant of malformed input; nil if no
  signal. `PlanMeta{Status,Branch,Epic,PR,Created,ReviewState}` all `omitempty` (`planmeta.go:14-21`).
  `MaxMetaPrefixBytes = 8192` (`planmeta.go:26`). Depends on `golang.org/x/net/html`.
- **GR-N1 watch-out:** `golang.org/x/net/html` must already be in `auto-watch`'s module graph or
  reimplementing `ExtractPlanMeta` verbatim would add a dependency. Verify before relying on it
  (see open risk in Verification).

## 2. The merged autowatch RPC handler seam (031)

- `auto-watch/internal/rpcmethods/methods.go` — `Handlers` struct + `New(hostID, version,
  startedAt, hub, ctlEvents)` (`methods.go:40-48`). Currently holds only `hostID`/`version`/
  `startedAt`/`hub`/`ctlEvents` + a per-method dispatch-count `sync.Map` (`methods.go:29-36`).
  **It does not yet hold a project-registry provider — 038 must add one.**
- `Register(p *rpc.Peer)` (`methods.go:52-54`) currently registers only
  `daemon.status` via `h.counted("daemon.status", h.handleStatus)`. 038 adds
  `doc.list`/`doc.get`/`doc.raw`/`project.list` here, each wrapped in `h.counted(...)`.
- `counted(method, inner)` (`methods.go:69-84`) — increments an `atomic.Int64` per method and,
  when `ctlEvents && hub != nil`, broadcasts a `ctl.log.info`/`rpc.served` event with a `method`
  field. New methods get this decorator for free.
- `DispatchCount(method) int` (`methods.go:59-65`) implements `conformance.Observations`.
- Handler signature: `rpc.Handler = func(ctx context.Context, params json.RawMessage) (any, error)`
  (`auto-shared/rpc/message.go:59`). Returning an `*rpc.Error` is passed through verbatim
  (code/data preserved); any other error → generic internal error (`message.go:56-58`).
- `rpc.Error{Code int, Message string, Data any}` (`message.go:47-54`); standard codes
  `ParseError=-32700, InvalidRequest=-32600, MethodNotFound=-32601, InvalidParams=-32602,
  InternalError=-32603` (`message.go:16-20`). The JSON-RPC server-defined range is
  `-32000..-32099` (available for an app code if needed).
- **GR-N3 layering:** `rpcmethods` imports `rpc`/`bus` but **never `transport`** — enforced by
  `auto-watch/internal/rpcmethods/layering_test.go` (031). New doc/project code must respect this;
  it only needs `config` (registry), `os`/`path`/`filepath`, and (maybe) `golang.org/x/net/html`.

## 3. Wiring in the daemon (031)

`auto-watch/internal/cli/ops.go`:
- `ops.go:104` — `hub := bus.NewHub()`
- `ops.go:105` — `hostID := sharedconfig.HostIDQuietly()`
- `ops.go:106` — `handlers := rpcmethods.New(hostID, version.Version, startedAt, hub, ctlEvents)`
- `ops.go:161` — `rpcSrv := rpcserver.New(rpcLn, handlers, hub, ctlEvents)`

038 adds a registry provider into `rpcmethods.New(...)`. The daemon already reads its registry
elsewhere; the provider is `func() config.ProjectsConfig` built from
`config.LoadProjects(path)` (`auto-shared/config/projects.go:78`) so the snapshot is re-read per
call (matching auto-ui's `regProvider` indirection) — registry edits are picked up without a
daemon restart, and an unreadable/missing file degrades to an empty registry.

## 4. Identity + registry primitives

- `config.HostIDQuietly() string` (`auto-shared/config/host.go:34-44`) — loads `~/.auto/host.json`,
  validated against `^[a-z0-9][a-z0-9._-]*$`; falls back to lowercased `os.Hostname()`; `""` if
  even that fails. **This is the value 038 stamps as `host` on every `project.list` entry**
  (GR-F8). `Handlers` already stores it as `h.hostID` (031), so `project.list` reuses it — no
  re-read, consistent with `daemon.status`.
- `git.NormalizeRemoteURL(raw string) string` (`auto-shared/git/normalize.go:16`) — the
  credential-stripping function `project.list` must keep applying to `ref.Remote`.
- `config.ProjectsConfig{Projects []ProjectRef}` (`projects.go:34-36`); `ProjectRef{ID, Path,
  Remote, Name, Tools, RegisteredAt}` (`projects.go:22-31`).
- `(ProjectsConfig).FindProjectByID(id)` (`projects.go:242`) and `.FindProjectByPath(dir)`
  (longest-prefix match, `projects.go:190-207`) — both used by `resolveRoot`.

## 5. Conformance harness (031, to extend)

`auto-watch/internal/rpcserver/conformance_test.go` — `TestMain` builds `./cmd/autowatch` once
(`cmd.Dir = findModuleRoot()`). It defines an in-process `Fixture` (Handlers over a unix/tcp
transport, client `conformance.PeerClient`, `Obs()` = `Handlers.DispatchCount`) and a binary
`Fixture` (seeds a temp `$HOME` with `host.json` + minimal `projects.json` and PATH stubs for
`tmux`/`claude` so the `start` doctor preflight passes, launches `--rpc-addr tcp://127.0.0.1:0
--ready-file`, dials). 038 extends the scenario/fixtures so the new methods are exercised over the
wire — and, critically, seeds the temp `$HOME` project with a real `docs/` tree so doc.* return
non-empty results.
