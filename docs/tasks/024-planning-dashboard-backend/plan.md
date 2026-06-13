# Plan: Task 024

## Summary

Add the Planning Docs Dashboard's backend in eight atomic phases — widen doc discovery, add
`project.list`, a raw-bytes HTML route, a gated debug buffer, the `.html` derivation widening, the
agent harness flags, and the `auto ui emit` helper — shipped as one PR.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| ~ | auto-ui/internal/server/docs.go | `docEntry.Type`; `walkDocs` collects `.md`+`.html`; `cleanDocPath(p, allowed...)` parameterized; `doc.get` → `{.md}` |
| + | auto-ui/internal/server/project.go | `projectListHandler` → `[]{id,name,path,remote}` |
| + | auto-ui/internal/server/raw.go | `handleDocRaw`: `GET /api/doc/raw`, `text/html`, `{.html}` only |
| + | auto-ui/internal/server/debug.go | ring buffer + `handleDebugRecent` (gated) |
| ~ | auto-ui/internal/server/server.go | register `project.list`; `WithDebug` option; mount `/api/doc/raw` + `/api/debug/recent`; pass buffer to `handleRPC` |
| ~ | auto-ui/internal/server/rpc_ingest.go | record raw + derived events into the buffer when enabled |
| ~ | auto-shared/bus/derive.go | `isDocPath` accepts `.html` (coverage only) |
| ~ | auto-ui/internal/cli/serve.go | `net.Listen`+`Serve` for `--port 0`; `--ready-file` (JSON); `AUTO_UI_PORT`; `--projects`/`AUTO_PROJECTS_PATH`; `WithDebug(AUTO_UI_DEBUG)` |
| + | auto-ui/internal/cli/emit.go | `auto ui emit` command |
| ~ | auto-ui/internal/cli/root.go | register `emit` |
| ~ | auto-cli/cmd/auto/hookscmd.go | `uiPort()` honors `AUTO_UI_PORT` |
| ~ | auto-ui/internal/cli/quickstart.go, docs.go | document emit, Origin rule, harness flags, `AUTO_UI_DEBUG` |
| +/~ | *_test.go (server, cli, bus) | one test file per AC |

## Links
- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test
- [ ] `auto-ui/internal/server/docs_test.go` — AC-2: `.html` discovery + `type`; `doc.get` rejects `.html`; traversal rejected
- [ ] `auto-ui/internal/server/project_test.go` — AC-1: `project.list` returns entries; empty registry → `[]`; credentialed `remote` emitted credential-free (normalized)
- [ ] `auto-ui/internal/server/raw_test.go` — AC-3: serves verbatim `.html` as `text/html`; rejects `.md`/traversal; `worktree` resolves
- [ ] `auto-ui/internal/server/debug_test.go` — AC-7: enabled returns raw+derived; disabled → 404
- [ ] `auto-shared/bus/derive_test.go` — AC-4: `docs/**/*.html` derives; non-`docs/` `.html` does not; `.md` unchanged
- [ ] `auto-ui/internal/cli/serve_test.go` + `auto-cli/cmd/auto/hookscmd_test.go` — AC-5: `--port 0`+`--ready-file` JSON; `AUTO_UI_PORT` both sides; `--projects` isolation
- [ ] `auto-ui/internal/cli/emit_test.go` — AC-6: envelope POST (no Origin) → one derived `doc.changed`; Origin-bearing request rejected

## Execution Sequence
```
Phase 1 (1.2 discovery) ─> Phase 2 (1.1 project.list) ─> Phase 3 (1.3 raw route) ─> Phase 4 (1.7 debug buffer) ─> Phase 6 (1.5 harness) ─> Phase 7 (1.6 emit) ─> Phase 8 (docs + full verify)
Phase 5 (1.4 isDocPath, auto-shared/bus) ──────────────────────────────────────────────────────────────────────^  (independent; merge any time before Phase 8)
```

> **Seriality note:** Phases 1–4 all edit `auto-ui/internal/server/server.go` (the `Register`
> block and the mux), and Phase 6 wires the `WithDebug` option that Phase 4 defines, so they run
> as a serial chain — concurrent subagents sharing one worktree leak writes (project rule). Only
> **Phase 5** is genuinely independent (separate module, no file overlap) and may run in parallel.

## Plan

### Phase 1: Doc discovery + parameterized validation (AC-2)
- [x] Step 1.1: Add `Type string \`json:"type"\`` to `docEntry` (`docs.go:16-19`).
- [x] Step 1.2: Change `cleanDocPath(p)` → `cleanDocPath(p string, allowed ...string)`; replace the hard-coded `.md` suffix check (`docs.go:169`) with membership in `allowed`; keep the `docs/`-prefix + `..` traversal guard. Update `docGetHandler` (`docs.go:73`) to call `cleanDocPath(p.Path, ".md")`.
- [x] Step 1.3: Change `walkDocs` (`docs.go:118-151`) to accept `.md` **and** `.html`, setting `Type` to `markdown`/`html`; keep the empty-`docs/` → `[]` behavior.
- [x] Step 1.4: Write `docs_test.go` cases — a `docs/` tree with a `.md` and a `.html` lists both with correct `type`; `doc.get` on a `.html` path returns an "invalid path" error; `../` traversal still rejected.
- [x] Step 1.5: Verify: `cd auto-ui && go build ./... && go test ./internal/server/`; assert `doc.list` includes the `.html` entry and `doc.get(".../x.html")` errors.
- [x] Step 1.6: Commit: `feat(024): phase 1 — widen doc discovery + parameterize cleanDocPath`

### Phase 2: `project.list` RPC (AC-1)
- [x] Step 2.1: Create `project.go` with `projectListHandler(reg func() config.ProjectsConfig) Handler` returning `[]map[string]string{{"id","name","path","remote"}}` (or a typed struct); empty registry → `[]`. **Apply `git.NormalizeRemoteURL(ref.Remote)` (`github.com/mistakenot/auto-shared/git`) to the `remote` field before emitting** — `project.list` is a UI boundary and must never leak a credentialed remote.
- [x] Step 2.2: Register it in `server.go` beside `doc.get` (`server.go:58`): `d.Register("project.list", projectListHandler(o.regProvider))`.
- [x] Step 2.3: Write `project_test.go` — fixture registry of 2 projects returns 2 entries with all four fields; empty registry returns `[]` (length 0), not an RPC error.
- [x] Step 2.4: Add a `project_test.go` case asserting a fixture entry whose stored `remote` carries credentials (e.g. `https://user:token@github.com/owner/repo.git`) is emitted in credential-free normalized form (no `@`/token; e.g. `https://github.com/owner/repo`).
- [x] Step 2.5: Verify: `cd auto-ui && go build ./... && go test ./internal/server/` green.
- [x] Step 2.6: Commit: `feat(024): phase 2 — project.list RPC`

### Phase 3: Raw-bytes HTTP route (AC-3) — depends on Phase 1
- [x] Step 3.1: Create `raw.go` with `handleDocRaw(reg func() config.ProjectsConfig) http.HandlerFunc`: GET-only; parse `project`/`path`/`worktree`; `resolveRoot`; `cleanDocPath(path, ".html")`; on success `w.Header().Set("Content-Type","text/html; charset=utf-8")` and write the file bytes; reject invalid/`.md`/traversal/missing with the right HTTP status.
- [x] Step 3.2: Mount in `server.go` before the `/` catch-all (after `server.go:79`): `mux.HandleFunc("/api/doc/raw", handleDocRaw(o.regProvider))`.
- [x] Step 3.3: Write `raw_test.go` (httptest) — a fixture `docs/.../x.html` returns 200, `Content-Type: text/html`, verbatim bytes; a `.md` path is rejected; `../etc/passwd` rejected; missing `path` → 4xx; `worktree` param resolves to the worktree root.
- [x] Step 3.4: Verify: `cd auto-ui && go build ./... && go test ./internal/server/`; `curl` parity noted in commit body if run manually.
- [x] Step 3.5: Commit: `feat(024): phase 3 — raw-bytes /api/doc/raw route for HTML docs`

### Phase 4: Server-side debug event buffer (AC-7) — depends on Phases 2–3
- [x] Step 4.1: Create `debug.go`: a `debugBuffer` (mutex + fixed-cap ring, e.g. N=100) with `record(bus.Event)` and `recent() []bus.Event`; plus `handleDebugRecent(b *debugBuffer, enabled bool) http.HandlerFunc` returning JSON when `enabled`, else `http.NotFound` (404).
- [x] Step 4.2: Add `WithDebug(enabled bool) Option` and an `options.debug bool` field in `server.go`; when set, construct the buffer, pass it into `handleRPC`, and mount `mux.HandleFunc("/api/debug/recent", handleDebugRecent(buf, o.debug))`.
- [x] Step 4.3: In `rpc_ingest.go`, after `hub.Broadcast(ev)` and each derived broadcast, call `buf.record(...)` when the buffer is non-nil (raw + each derived event).
- [x] Step 4.4: Write `debug_test.go` — with `WithDebug(true)`, POST a valid `agent.tool.post` for a `docs/**/*.md` path, then `GET /api/debug/recent` shows the raw event **and** one derived `doc.changed`; default server (no `WithDebug`) returns 404.
- [x] Step 4.5: Verify: `cd auto-ui && go build ./... && go test ./internal/server/` green; existing `rpc_ingest_test.go` still passes.
- [x] Step 4.6: Commit: `feat(024): phase 4 — gated /api/debug/recent server event buffer`

### Phase 5: Widen `doc.changed` derivation to `.html` (AC-4) — independent (parallel)
- [x] Step 5.1: In `auto-shared/bus/derive.go:70-72`, change `isDocPath` to `strings.HasPrefix(rel,"docs/") && (strings.HasSuffix(rel,".md") || strings.HasSuffix(rel,".html"))`.
- [x] Step 5.2: Add `derive_test.go` cases — `docs/tasks/021/artifacts/x.html` emits one `doc.changed`; a non-`docs/` `.html` (e.g. `web/x.html`) emits none; the existing `.md` test still passes.
- [x] Step 5.3: Verify: `cd auto-shared && go build ./... && go test ./bus/` green.
- [x] Step 5.4: Commit: `feat(024): phase 5 — derive doc.changed for docs/**/*.html`

### Phase 6: Harness launch + isolation (AC-5) — depends on Phase 4
- [x] Step 6.1: In `serve.go`, resolve the port with `AUTO_UI_PORT` as a precedence rung (explicit `--port` flag > `AUTO_UI_PORT` > settings.json > 8080), and accept `--port 0`.
- [x] Step 6.2: Replace `srv.ListenAndServe()` (`serve.go:100`) with `ln, err := net.Listen("tcp", srv.Addr)` then `srv.Serve(ln)`; after a successful `Listen`, if `--ready-file` is set, write `{"addr":"<ln.Addr().String()>"}\n` to that path (and keep the stderr banner using the real bound port).
- [x] Step 6.3: Add `--projects <path>` flag + `AUTO_PROJECTS_PATH` env; in the registry-provider closure (`serve.go:63-73`) load from the resolved path instead of `ProjectsConfigPath()` when provided.
- [x] Step 6.4: Pass `server.WithDebug(os.Getenv("AUTO_UI_DEBUG") == "1")` into `server.New` in `serve.go`.
- [x] Step 6.5: In `auto-cli/cmd/auto/hookscmd.go`, make `uiPort()` (`:325`) return `AUTO_UI_PORT` (parsed, >0) before reading settings.json.
- [x] Step 6.6: Write `serve_test.go` — run the command with `--port 0 --ready-file <tmp> --projects <fixture>`; read the ready-file, assert it is JSON `{"addr":...}` with a real `127.0.0.1:NNNN`; hit `/api/hello` on that port; assert `--projects` isolates from `~/.auto`. Write `hookscmd_test.go` asserting `uiPort()` honors `AUTO_UI_PORT`. Use `t.Setenv` for env isolation.
- [x] Step 6.7: Verify: `cd auto-ui && go build ./... && go test ./...` and `cd auto-cli && go test ./...` green; manual smoke: `AUTO_UI_PORT=… auto ui serve --port 0 --ready-file /tmp/r.json` writes the JSON line.
- [x] Step 6.8: Commit: `feat(024): phase 6 — agent harness launch (--port 0, --ready-file, AUTO_UI_PORT, --projects)`

### Phase 7: Synthetic emit helper (AC-6) — depends on Phase 4

<!-- RESOLVED(P3): Phase 7 does not actually depend on Phase 5 (.html derivation)
REVIEW: The header says "depends on Phases 4–5", but the emit AC-6 test (step 7.3) emits for a
`docs/**/*.md` path and asserts one derived `doc.changed` — that needs only Phase 4 (the debug buffer
to observe the derivation), not Phase 5 (the `.html` `isDocPath` widening). Nothing in emit or its
test touches `.html`. The Phase 5 dependency is spurious; it over-constrains the ordering (Phase 5 is
otherwise correctly described as fully independent/parallel). Suggest "depends on Phase 4" only.
AUTHOR: Correct — changed the header to "depends on Phase 4". The emit test uses a `.md` path and
observes derivation via the Phase 4 debug buffer; it never touches `.html`. The Execution Sequence
DAG already places Phase 5 as fully independent, so no DAG change is needed.
-->

- [x] Step 7.1: Create `emit.go`: `auto ui emit --project <id> --path <docs/...> [--worktree …]` builds an `agent.tool.post` via `bus.NewEvent` (RFC3339 `time`, `ToolPost{paths:[{rel:path, abs:absPath}]}`, `source:"auto/ui/emit"`), sets `ev.Project`/`ev.Worktree`, and POSTs `ev.AsNotification()` to `http://127.0.0.1:<port>/api/rpc` with `Content-Type: application/json` and **no `Origin` header**; port via `--port`/`AUTO_UI_PORT`. JSON payload to stdout, diagnostics to stderr; non-zero exit on POST failure. **`abs` resolution (no registry load):** when `--worktree` is given, `absPath = filepath.Join(worktree, path)`; otherwise leave `absPath` empty. emit does **not** take `--projects`/`AUTO_PROJECTS_PATH` — derivation ignores `abs` (see thread).

<!-- RESOLVED(P2): emit's `root` for `abs:filepath.Join(root,path)` is undefined — and the server doesn't need it
REVIEW: This step writes `abs:filepath.Join(root,path)` but never says where `root` comes from in
the emit command. emit runs client-side and is given only `--project`/`--path`/`--worktree`/`--port`.
To turn a `--project <id>` into a filesystem root it would have to load the registry itself
(`ProjectsConfigPath()` / `AUTO_PROJECTS_PATH`) — but emit is not listed as taking `--projects`/
`AUTO_PROJECTS_PATH` (only `serve` is, Phase 6), so in the isolated-fixture harness flow it has no way
to resolve the same root the server uses. Crucially, this dependency is unnecessary: `DeriveDocChanged`
(auto-shared/bus/derive.go:30-46) matches only on `cleanRel(p.Rel)` + `isDocPath(rel)` and carries
`p.Abs` straight into `DocChanged.AbsPath` without validating it — so derivation produces exactly one
`doc.changed` regardless of `abs`. Recommend: when `--worktree` is given, `abs = filepath.Join(worktree, path)`
(no registry needed); otherwise leave `abs` empty (or document that emit loads the registry and add
`--projects`/`AUTO_PROJECTS_PATH` to emit for harness parity). Pin this so the implementer doesn't add
a spurious registry-load just to fill a field the derivation ignores.
AUTHOR: Adopted the recommendation. Step 7.1 now pins `abs = filepath.Join(worktree, path)` when
`--worktree` is given, else empty — and explicitly states emit does NOT take `--projects`/
`AUTO_PROJECTS_PATH` and must not load the registry, since `DeriveDocChanged` carries `p.Abs` into
`DocChanged.AbsPath` without validating it (one `doc.changed` derives regardless of `abs`). The
solution's step-7 outline (solution.md) used `paths:[{rel,abs}]` generically and stays consistent.
-->

- [x] Step 7.2: Register the command in `root.go` (alongside `serve`).
- [x] Step 7.3: Write `emit_test.go` (e2e) — start `httptest`/served instance with `WithDebug(true)` + fixture registry; run emit for a `docs/**/*.md` path; assert HTTP 204 and that `/api/debug/recent` shows one derived `doc.changed`. Add a negative assertion that a request carrying an `Origin` header is rejected with 403 (documents the rule).
- [x] Step 7.4: Verify: `cd auto-ui && go build ./... && go test ./...` green.
- [x] Step 7.5: Commit: `feat(024): phase 7 — auto ui emit synthetic event helper`

### Phase 8: Docs + full verification — depends on all
- [ ] Step 8.1: Update `quickstart.go` and cli `docs.go`: document `auto ui emit`, the Origin "trigger-via-CLI / observe-via-browser" rule, the harness flags (`--port 0`, `--ready-file`, `--projects`), `AUTO_UI_PORT`/`AUTO_PROJECTS_PATH`/`AUTO_UI_DEBUG`, and `/api/doc/raw` + `/api/debug/recent`.
- [ ] Step 8.2: Run `make check` (fmt-check, vet, lint, stale-refs) and fix any findings.
- [ ] Step 8.3: Run module tests: `cd auto-ui && go test ./...`, `cd auto-shared && go test ./...`, `cd auto-cli && go test ./...` — all green.
- [ ] Step 8.4: Update the epic sub-task index (`docs/epics/002-planning-docs-dashboard.md`) marking 1.1–1.7 status, and tick this plan's checkboxes.
- [ ] Step 8.5: Commit: `docs(024): document harness + emit; mark epic Phase 1 sub-tasks`

## Success Criteria
- [ ] AC-1: `project.list` returns `{id,name,path,remote}` per registered project with `remote` normalized (no credentials); empty registry → `[]` (verified by `project_test.go`).
- [ ] AC-2: `doc.list` returns `.md`+`.html` entries with `type`; `doc.get` still rejects `.html`; traversal still rejected (`docs_test.go`).
- [ ] AC-3: `GET /api/doc/raw` serves verbatim `.html` as `text/html`; `.md`/traversal rejected; `worktree` resolves (`raw_test.go`).
- [ ] AC-4: `docs/**/*.html` derives `doc.changed`; non-`docs/` `.html` does not; `.md` unchanged (`derive_test.go`).
- [ ] AC-5: `--port 0` + `--ready-file` writes `{"addr":...}` with the real bound port; `AUTO_UI_PORT` honored by `serve` **and** `uiPort()`; `--projects`/`AUTO_PROJECTS_PATH` isolates the registry (`serve_test.go`, `hookscmd_test.go`).
- [ ] AC-6: `auto ui emit` POSTs a valid envelope with no Origin → exactly one derived `doc.changed` (confirmed via `/api/debug/recent`); Origin-bearing requests rejected (`emit_test.go`).
- [ ] AC-7: `GET /api/debug/recent` returns raw+derived events under `AUTO_UI_DEBUG=1`, 404 otherwise (`debug_test.go`).
- [ ] `make check` passes; `auto-ui`, `auto-shared`, `auto-cli` tests all green.
- [ ] No change to `auto-shared/bus` beyond the one-line `isDocPath` widening; no new file watcher; no envelope/wire-shape change.

## Open Questions
- (none — both requirements Open Questions resolved: `--ready-file` writes `{"addr":...}`; ships as one PR)
