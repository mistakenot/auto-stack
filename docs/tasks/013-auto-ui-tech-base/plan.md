# Plan: Task 013

## Summary

Scaffold the `auto-ui/` auto-* package (standard cobra CLI), add a build-tag-split `web` package
that serves a no-build Preact+htm SPA either embedded or live-from-disk, wire a `serve` command and
HTTP server with `/api/hello`, then verify the browser-only behaviour with an agent-browser
conformance run.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| + | auto-ui/go.mod, go.sum | module `github.com/mistakenot/auto-ui`; go 1.26.1; replace auto-shared |
| + | auto-ui/cmd/autoui/main.go | entry point → `cli.Execute` |
| + | auto-ui/internal/app/app.go | `App{Stdout,Stderr,CWD}` |
| + | auto-ui/internal/cli/root.go | `Execute`, `NewRootCmd`, `ExitError`; registers subcommands incl. `serve` |
| + | auto-ui/internal/cli/{init,doctor,quickstart,docs,update}.go | standard subcommands |
| + | auto-ui/internal/cli/serve.go | `serve` command: `--port`, graceful shutdown via `cmd.Context()` |
| + | auto-ui/internal/config/settings.go | `Settings{Port int}`; ensure/load/save/validate |
| + | auto-ui/internal/server/server.go | `New(fsys fs.FS, mode string) http.Handler`; `/api/hello` + file server |
| + | auto-ui/internal/server/server_test.go | server contract tests (AC-3 server, AC-4) |
| + | auto-ui/web/embed_prod.go | `//go:build !dev`; `//go:embed all:static`; `FS()`, `Mode="embed"` |
| + | auto-ui/web/embed_dev.go | `//go:build dev`; `FS()=os.DirFS("web/static")`, `Mode="disk"` |
| + | auto-ui/web/static/index.html | import map (preact/htm/compat) + `#app` + loads app.js |
| + | auto-ui/web/static/app.js | Preact+htm shell, 2 views, counter, fetch |
| + | auto-ui/web/static/router.js | hash parse/serialize + hashchange subscription |
| + | auto-ui/CLAUDE.md | tool description + build/test instructions |
| ~ | Makefile | add auto-ui to PROJECTS + build-ui/dist-ui/install-ui |
| ~ | CLAUDE.md | add auto-ui row to sub-projects table (between auto-skill and auto-watch) |

## Links
- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)
- [Conformance script](./conformance.md)

## How to Test
- [ ] `auto-ui/internal/server/server_test.go` — `GET /` returns 200 + HTML (AC-4); `GET /api/hello` returns JSON with `message` + `mode` (AC-3 server side)
- [ ] `go build ./...` in `auto-ui` (default tags) and `go build -tags dev ./...` — both compile (AC-6)
- [ ] `go vet ./...` in `auto-ui` clean
- [ ] agent-browser conformance run per [conformance.md](./conformance.md) — AC-1, AC-2, AC-3 (UI), AC-5

## Execution Sequence
```
Phase 1 (scaffold) --> Phase 2 (web assets + tag split) --> Phase 3 (server + serve)
                                                                   |
                                              +--------------------+--------------------+
                                              v                                         v
                                     Phase 4 (Go tests)                        Phase 5 (monorepo wiring)
                                              |                                         |
                                              +--------------------+--------------------+
                                                                   v
                                                        Phase 6 (agent-browser conformance)
```

## Plan

### Phase 1: Scaffold package
- [x] Step 1.1: Run worktree discipline — `git fetch origin && git checkout main && git pull origin main`, then create branch `feat/013-auto-ui-tech-base`
- [x] Step 1.2: Scaffold via the `new-package` skill (name `ui`, binary `autoui`) — produces `cmd/autoui/main.go`, `internal/app/app.go`, `internal/cli/{root,init,doctor,quickstart,docs,update}.go`, `internal/config/settings.go`, `go.mod`, `CLAUDE.md`. If the skill is unavailable, hand-copy the auto-graph scaffold (commit `78d2616`).
- [x] Step 1.3: Set `go.mod` to `go 1.26.1`, cobra `v1.10.2`, `replace github.com/mistakenot/auto-shared => ../auto-shared`; run `go mod tidy`
- [x] Step 1.4: Edit `internal/config/settings.go` so the tool settings struct is `Settings{Port int}` (default 8080) under `~/.auto/ui/settings.json`; `validate()` rejects port <1 or >65535 with a `config.ValidationError`
- [x] Step 1.5: Point `doctor` checks at real conditions — settings file present/valid, and a "port" check (informational). `quickstart`/`docs` markdown mention `autoui serve`

<!-- RESOLVED(P3): doctor "port" check — clarify it's informational, not a "port free" probe
REVIEW: solution.md (Files table, doctor.go line) describes the doctor check as "port free", but this step
softens it to "(informational)". A genuine "is the port free" probe is racy (TOCTOU — free at doctor time,
taken at serve time) and odd for a config doctor. The auto-graph doctor pattern reports config/dependency
status, not live port liveness. Make solution.md and plan agree: recommend an informational check that just
reports the configured port value (and maybe whether it parses to a valid range), not an actual bind/free
test. Update the solution.md "port free" wording to match.
AUTHOR: Agreed — no live bind probe. Updated solution.md Files-table doctor.go line from "port free" to
"configured port in valid range (informational — not a live bind probe)", matching this step's intent and
the auto-graph doctor pattern (reports config/dependency status, not liveness). Step 1.5 wording already
said "informational" and is unchanged.
-->

- [x] Step 1.6: Verify — `cd auto-ui && go build ./... && go vet ./...`; run `go run ./cmd/autoui --version` (prints version), `... quickstart` (prints markdown), `... doctor` (valid JSON array to stdout). **Note:** default-tag build will fail until Phase 2 creates `web/static` if `web` is imported; keep `serve`/`web` import out until Phase 2, OR create an empty `web/static/.gitkeep` placeholder now so `//go:embed` has a target.
- [x] Step 1.7: Commit: `feat(autoui): phase 1 - scaffold package with standard subcommands`

### Phase 2: Web assets + build-tag split  (depends on Phase 1)
- [x] Step 2.1: Create `web/static/index.html` with the import map (preact@10, preact/hooks, htm/preact, plus `react`/`react-dom` → `preact@10/compat` aliases), `<div id="app">`, `<script type="module" src="./app.js">`
- [x] Step 2.2: Create `web/static/router.js` — `parseHash()` → `{view, params}`, `setHash(view, params)`, `onRouteChange(cb)` over the `hashchange` event
- [x] Step 2.3: Create `web/static/app.js` — Preact+htm app: nav (Home/Dashboard) that calls `setHash`; Home shows a counter whose value is read from / written to `?n=`; Dashboard has a "fetch from go" button that GETs `/api/hello` and renders `message`; re-render on `onRouteChange`
- [x] Step 2.4: Create `web/embed_prod.go` (`//go:build !dev`): `//go:embed all:static`, `const Mode = "embed"`, `func FS() fs.FS` via `fs.Sub(content, "static")`
- [x] Step 2.5: Create `web/embed_dev.go` (`//go:build dev`): `const Mode = "disk"`, `func FS() fs.FS { return os.DirFS("web/static") }` (path is relative to module root — `serve` must be run from `auto-ui/`)
- [x] Step 2.6: Verify — `cd auto-ui && go build ./web/ && go build -tags dev ./web/` both compile; `gofmt -l web/*.go` clean. Sanity-check the import map URLs resolve (the conformance run is the real check).
- [x] Step 2.7: Commit: `feat(autoui): phase 2 - no-build SPA assets with dev/embed split`

### Phase 3: HTTP server + serve command  (depends on Phase 2)
- [ ] Step 3.1: Create `internal/server/server.go` — `New(fsys fs.FS, mode string) http.Handler`: mux with `/api/hello` (JSON `{message, mode}`, `Content-Type: application/json`) and `/` → `http.FileServer(http.FS(fsys))`
- [ ] Step 3.2: Create `internal/cli/serve.go` — `serve` command: `--port` (default from settings, fallback 8080); build handler from `web.FS()`/`web.Mode`; start `http.Server`; goroutine `<-cmd.Context().Done()` → `srv.Shutdown`; log `serving on http://localhost:<port> (assets=<mode>)` to **stderr**; treat `http.ErrServerClosed` as clean exit
- [ ] Step 3.3: Register `newServeCmd(application)` in `NewRootCmd` (root.go AddCommand)
- [ ] Step 3.4: Verify — `cd auto-ui && go build ./... && go build -tags dev ./... && go vet ./...`. Manually: `go build -o /tmp/autoui ./cmd/autoui && /tmp/autoui serve --port 8099 &` then `curl -s localhost:8099/api/hello` returns JSON with `mode":"embed"`, `curl -s localhost:8099/` returns the HTML shell; `curl` a missing asset returns 404; kill the process and confirm graceful exit
- [ ] Step 3.5: Commit: `feat(autoui): phase 3 - http server and serve command`

### Phase 4: Go tests  (depends on Phase 3)
- [ ] Step 4.1: Write `internal/server/server_test.go` using `httptest`: (a) `GET /api/hello` → 200, `Content-Type: application/json`, body decodes to a struct with non-empty `message` and `mode` (AC-3 server); (b) `GET /` → 200 with `text/html` body containing `id="app"` (AC-4); (c) `GET /nope.js` → 404. Build the handler with a small in-memory `fstest.MapFS` so the test does not depend on embed tags
- [ ] Step 4.2: Verify — `cd auto-ui && go test ./...` passes; `go test -tags dev ./...` passes
- [ ] Step 4.3: Commit: `feat(autoui): phase 4 - server contract tests`

### Phase 5: Monorepo wiring  (depends on Phase 3)
- [ ] Step 5.1: `Makefile` — append `auto-ui` to `PROJECTS` (line 15); add `auto-ui_BIN := autoui` / `auto-ui_ENTRY := ./cmd/autoui`; add `build-ui`, `dist-ui`, `install-ui` targets mirroring `*-graph`; add `cp $(BUILD_DIR)/autoui $(INSTALL_DIR)/` to the aggregate install
- [ ] Step 5.2: Root `CLAUDE.md` — add `| auto-ui/ | autoui | Early | Local web dashboard + server (self-contained no-build SPA) |` row between `auto-skill/` and `auto-watch/` (alphabetical)
- [ ] Step 5.3: `auto-ui/CLAUDE.md` — fill in description + build/test (`go build ./cmd/autoui`, `go test ./...`, dev: `go run -tags dev ./cmd/autoui serve`)
- [ ] Step 5.4: Verify — `make build-ui` produces `bin/autoui`; `./bin/autoui --version` works; `make build` (all) still succeeds; root `go vet`/pre-commit hook clean

<!-- RESOLVED(P3): wrong output path — binaries land in bin/, not build/
REVIEW: The root Makefile sets `BUILD_DIR := bin` (Makefile:8), so `make build-ui` produces `bin/autoui`
and the install copies are `cp $(BUILD_DIR)/autoui ...` → `bin/autoui`. This step says `build/autoui` and
`./build/autoui --version`, which won't exist. Change to `bin/autoui` / `./bin/autoui --version`.
AUTHOR: Confirmed `BUILD_DIR := bin` at Makefile:8. Updated Step 5.4 to `bin/autoui` / `./bin/autoui
--version`.
-->

- [ ] Step 5.5: Commit: `feat(autoui): phase 5 - makefile and docs registration`

### Phase 6: agent-browser conformance  (depends on Phases 2-5)
- [ ] Step 6.1: Default-tag run — `go build -o /tmp/autoui ./cmd/autoui && /tmp/autoui serve --port 8080`; agent-browser loads `/` and asserts the shell renders (AC-1)
- [ ] Step 6.2: Dev-tag run — from `auto-ui/`, `go run -tags dev ./cmd/autoui serve --port 8080`; agent-browser: Dashboard → click fetch → assert `/api/hello` message in DOM (AC-3 UI)
- [ ] Step 6.3: agent-browser: Home → click `+` ×3 → assert `#/home?n=3`; reload → assert counter still 3 (AC-5)
- [ ] Step 6.4: agent-browser: edit a label string in `web/static/app.js`, reload (no Go restart) → assert new label visible (AC-2); revert the edit
- [ ] Step 6.5: Capture evidence (screenshots/log) into the task folder or PR description
- [ ] Step 6.6: Commit any conformance notes: `test(autoui): phase 6 - agent-browser conformance evidence`

## Success Criteria
- [ ] `cd auto-ui && go build ./...` and `go build -tags dev ./...` both succeed (AC-6)
- [ ] `cd auto-ui && go test ./...` and `go test -tags dev ./...` pass (AC-3 server, AC-4)
- [ ] `go vet ./...` clean; gofmt clean
- [ ] `make build-ui` produces a runnable `autoui` binary; `make build` (all) unaffected (AC-6)
- [ ] agent-browser conformance: shell renders from embedded binary (AC-1); fetch result renders (AC-3); counter+view restored from hash on reload (AC-5); dev-mode file edit visible on reload with no Go rebuild (AC-2)
- [ ] auto-ui registered in root Makefile + CLAUDE.md sub-projects table (AC-6)

## Open Questions
- (none — all resolved in requirements Q1–Q4)

## Notes / Risks
- **Dev-mode cwd dependency**: `os.DirFS("web/static")` resolves relative to the process cwd, so the
  `-tags dev` server must be launched from the `auto-ui/` module root. The startup log states the
  mode; if assets 404 in dev, wrong-cwd is the first suspect. (A `--web-dir` override is a possible
  future refinement, out of scope here.)
- **CDN dependency at runtime**: the SPA imports preact/htm from esm.sh, so the conformance browser
  needs network access on first load. Offline vendoring is deferred (Q4).
- **`//go:embed all:static` needs a non-empty target at default-tag build time** — ensure
  `web/static/` contains files (index.html etc.) before any default-tag `go build`; the Phase 1
  placeholder note covers the ordering.
