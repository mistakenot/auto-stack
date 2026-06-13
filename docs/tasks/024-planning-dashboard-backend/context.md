# Context: Task 024

Verified codebase facts grounding the Phase 1 backend solution (see `solution.md`). All paths
relative to repo root; line numbers confirmed 2026-06-13.

## Key Files

### auto-ui server
- `auto-ui/internal/server/server.go:38-86` — `New(fsys, mode, opts...)`: builds the `Hub`
  (`bus.NewHub()`, :46), the JSON-RPC dispatcher, and the `http.ServeMux`. RPC registration block:
  `d.Register("ping", …)` (:50), `d.Register("doc.list", docListHandler(o.regProvider))` (:57),
  `d.Register("doc.get", docGetHandler(o.regProvider))` (:58). **Add `project.list` here.** Mux:
  `/api/hello` (:61), `/api/ws` → `handleWSWithHub(hub, d)` (:76), `/api/rpc` → `handleRPC(hub,
  o.regProvider)` (:79), static `mux.Handle("/", assets)` (:85). **Mount `/api/doc/raw` and
  `/api/debug/recent` before the `/` catch-all.**
- `auto-ui/internal/server/server.go:13-26` — `type Option func(*options)`; `options{ regProvider
  func() config.ProjectsConfig }`; `WithRegistryProvider(fn)`. **Add `WithDebug(bool)` the same
  way.** Default `regProvider` returns empty `ProjectsConfig{}` (:40).
- `auto-ui/internal/server/docs.go:16-19` — `docEntry{ ID, Path string }` (JSON `id`,`path`).
  **Add `Type string` (`json:"type"`).**
- `auto-ui/internal/server/docs.go:118-151` — `walkDocs(root)` walks `<root>/docs`, skips dirs,
  `if !strings.HasSuffix(d.Name(), ".md")` (:129) → **the markdown-only filter to widen**. Missing
  `docs/` dir returns `[]` (not an error).
- `auto-ui/internal/server/docs.go:155-174` — `cleanDocPath(p)`: `path.Clean`, rejects `..` and
  `/..`, trims `./` and `/`, then `if !strings.HasPrefix(cleaned, "docs/") ||
  !strings.HasSuffix(cleaned, ".md")` (:169) → **parameterize the extension check**.
- `auto-ui/internal/server/docs.go:98-114` — `resolveRoot(reg, project, worktree)`: `worktree` →
  `reg.FindProjectByPath` (returns `filepath.Clean(worktree)`); else `project` →
  `reg.FindProjectByID` (returns `filepath.Clean(ref.Path)`); else error. **Reused verbatim by the
  raw route.**
- `auto-ui/internal/server/docs.go:51-90` — `docGetHandler`: reads file, returns
  `{"path":…, "markdown": string(data)}` — **stays `.md`-only; must never read `.html`.**
- `auto-ui/internal/server/rpc_ingest.go:24-82` — `handleRPC(hub, regProvider)`: POST-only;
  **rejects any request with a non-empty `Origin` header → 403** (:36-39); requires
  `application/json`; parses `{jsonrpc, method, params: bus.Event}`; `ev.Validate()`;
  `hub.Broadcast(ev)`; `DeriveDocChanged(ev, reg)` then broadcasts each derived; returns `204`.
  **Record raw + derived into the debug buffer here (1.7).**

### serve command + config
- `auto-ui/internal/cli/serve.go:22-109` — uses `srv.ListenAndServe()` (:100) on
  `Addr: "127.0.0.1:%d"` (:75) → **cannot recover an OS-assigned port; switch to `net.Listen` +
  `srv.Serve(ln)` and read `ln.Addr()`.** Banner to `application.Stderr` (:99). Port precedence:
  `--port` flag (:107, default 8080) > `~/.auto/ui/settings.json` (:33-43) > 8080. Registry
  provider closure built at :63-73 (`ProjectsConfigPath()` → `LoadProjects`) → **swap the path for
  `--projects`/`AUTO_PROJECTS_PATH` resolution.** Graceful shutdown goroutine (:86-97).
- `auto-ui/internal/config/settings.go:28-31,49-55,113-122,89-100` — `Settings{ Port int }`;
  `UISettingsPath()` → `~/.auto/ui/settings.json`; `LoadUISettings` (strict decode + validate);
  port range [1,65535].

### project registry (auto-shared)
- `auto-shared/config/projects.go:22-31` — `ProjectRef{ ID, Path, Remote, Name string; Tools
  []string; RegisteredAt string }` (JSON `id`,`path`,`remote`,`name`,`tools`,`registeredAt`).
- `auto-shared/config/projects.go:34-36` — `ProjectsConfig{ Projects []ProjectRef }`.
- `auto-shared/config/projects.go:241-250` — `FindProjectByID(id)` (normalizes id);
  `:192-206` — `FindProjectByPath(dir)` longest-prefix match; `:39-45` — `ProjectsConfigPath()` →
  `~/.auto/projects.json`. **No env override exists today.**

### bus (auto-shared) — task-021-owned; only `isDocPath` widens
- `auto-shared/bus/derive.go:70-72` — `isDocPath(rel)`: `strings.HasPrefix(rel,"docs/") &&
  strings.HasSuffix(rel,".md")` → **the one-line change (add `.html`).**
- `auto-shared/bus/derive.go:10-49` — `DeriveDocChanged(ev, reg)`: only `agent.tool.post`;
  requires `ev.Project` to resolve in registry; decodes `ToolPost`; per matching path emits
  `DocChanged{Project, Path(rel), AbsPath, Worktree, Branch}` via `newDerived(ev,"doc.changed",dc)`.
- `auto-shared/bus/event.go:28-41` — `Event{ specversion, type, source, id, time, project,
  session, remote, branch, worktree, commit, data }`. `:103-109` — `AsNotification()` →
  `{jsonrpc:"2.0", method: e.Type, params: e}` (whole envelope under `params`).
- `auto-shared/bus/payloads.go:24-30` — `DocChanged{ Project, Path, AbsPath(`abs_path`), Worktree,
  Branch }`; `ToolPost{ tool, event, paths[]{rel,abs}, raw }`.
- `auto-shared/bus/derive_test.go:34-61` — `TestDeriveDocChangedFromDocsMd` is the pattern to copy
  for an `.html` case (`toolPostEvent(project, []PathRef{...})` → assert one derived `doc.changed`).

### hooks producer (auto-cli)
- `auto-cli/cmd/auto/hookscmd.go:325-342` — `uiPort()` reads `~/.auto/ui/settings.json` port, else
  `defaultUIPort` (8080, :29). **Add `AUTO_UI_PORT` env as first precedence.**
- `auto-cli/cmd/auto/hookscmd.go:344-366` — `postBusEvent(port, ev)`: `json.Marshal(ev.AsNotification())`,
  POST to `http://127.0.0.1:%d/api/rpc`, `Content-Type: application/json`, **no Origin header set**
  (150ms timeout). Pattern to mirror for `auto ui emit`.
- `auto-cli/cmd/auto/hookscmd.go:143-224` — `buildBusEvent`: `bus.NewEvent(eventType, source, tp)`
  then sets `ev.Project/Worktree/Branch/Commit/Remote/Session`. Envelope-construction reference
  for the emit helper.

## Patterns

- **Functional options on the server** (`WithRegistryProvider`) — add `WithDebug(bool)` the same
  way; keep `New` pure (read `AUTO_UI_DEBUG` in `serve.go`, not in `New`) so server tests stay
  hermetic.
- **Registry provider is a `func() config.ProjectsConfig`** called per request, so a fixture
  registry is injected by swapping the closure — no global state. Tests inject empty/fixture
  registries; never read the developer's real `~/.auto/projects.json`.
- **`remote` must be normalized** (`git.NormalizeRemoteURL`) before entering any envelope/bus/UI
  boundary — credentials must never broadcast. `project.list` re-emits the stored `remote`; the
  registry value should already be normalized, but treat it as a boundary.
- **Asset delivery is a build-tag split** (`web/embed_prod.go` `//go:build !dev` embeds;
  `web/embed_dev.go` `-tags dev` reads disk), both exposing `web.FS()`/`web.Mode`. Validate the
  shipped artifact with the embed build; iterate the SPA with `-tags dev`. Dev mode sends
  `Cache-Control: no-store` (`noStore`, server.go:83).
- **CLI conventions** (CLAUDE.md): JSON default, stdout = parseable payload only, diagnostics to
  stderr; `quickstart`/`docs` kept current; E2E = populate disk + run as a user + clean up, off the
  real `~/.auto`.
- **No env-var gating exists in auto-ui yet**; the repo standard is `os.Getenv(name)` (+
  `strings.TrimSpace`). `AUTO_UI_DEBUG=1`, `AUTO_UI_PORT`, `AUTO_PROJECTS_PATH` follow it.

## Related Tasks
- **Task 021 (auto-bus-standard, merged #75 — commit `e3a635b`)** — owns the `Hub`, `Event`
  envelope, `DeriveDocChanged`, `/api/rpc` ingest, and the Origin guard (introduced in its phase 3,
  `902779a`; CSRF/XSS/RFC3339-nano hardening in `60f2946`). This task only *consumes* the signal;
  the sole allowed change to its code is the `isDocPath` `.html` coverage widening (1.4). Established
  constraint: do not fork/duplicate the hook→ui→socket loop.
- **Task 013 (auto-ui tech base, #56 — commit `d3a659d`)** — created `serve.go` and the SPA/server,
  the embed-vs-dev build tag split, and graceful shutdown. **`--ready-file` does NOT exist yet**
  (confirmed: `git log -S "ready-file" -- auto-ui/` is empty; `serve.go` has only `--port` at
  :107) — it is added fresh in this task. Gotchas: `Cache-Control: no-store` in dev, build an
  explicit `-tags dev` binary (don't `go run`).
- **Tasks 020 / 022** — own hook-event production feeding `/api/rpc`; `auto ui emit` (1.6) imitates
  their `agent.tool.post` POST but is a test trigger, not a new producer.

## History, Conventions & Tooling (verified 2026-06-13)
- **Commit style:** Conventional Commits with a task-number scope — `feat(024): phase N — <desc>`,
  `fix(024): …`, `docs(024): …`, `style: …`; PRs carry `(#NN)`. Co-author trailer
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` (+ `Session-Id:` where used).
- **Verification:** `make check` = `fmt-check` (gofmt) → `vet` (per module) → `lint`
  (golangci-lint per module) → `stale-refs` (no old binary names). `make test` loops
  `go test ./...` over every module (incl. `auto-ui`, `auto-shared`, `auto-cli`). Targeted:
  `cd auto-ui && go test ./...`.
- **Server test idiom** (`auto-ui/internal/server/server_test.go`, `rpc_ingest_test.go`,
  `ws_test.go`): construct `server.New(newTestFS(), "test", server.WithRegistryProvider(fn))`,
  drive HTTP via `httptest.NewRequest`+`httptest.NewRecorder` (handler tests) or
  `httptest.NewServer(handler)` + a `dialWS`/`readUntil` WebSocket client (ingest/broadcast tests).
  `newTestFS()` is an in-memory `fstest.MapFS`; registries are injected (never the real
  `~/.auto/projects.json`).
- **No CLI command tests exist yet** under `auto-ui/internal/cli/` — `serve_test.go`/`emit_test.go`
  are the first; drive the cobra command via its `RunE` with an isolated temp registry + temp
  `docs/` tree (CLAUDE.md: populate disk, run as a user, clean up).
- **Recent activity** on `auto-ui/`/`auto-shared/bus/` is from #56/#65/#67/#75 plus the two June-13
  lint sweeps (`71b0484`, `07bdf33`); no in-flight branches → low conflict risk.
