# Conformance Harness: Task 025 — Planning Dashboard Explorer

> Acceptance for 025 is browser-driven (frontend-only, no Go tests). Drive the served SPA with
> `agent-browser` and assert via `data-*` attributes + `eval` (not rendered-text scraping).
> Re-run on **both** the embed build and the dev (`-tags dev`) build (013 feedback: browser-layer
> defects are invisible to Go tests). Phase 6 evidence (both builds, 36/36 each) lives under
> [`evidence/`](./evidence/): `embed-results.txt`, `dev-results.txt`, `dev-build.md`, plus the
> per-AC `*.json`/`*.png` captures.

## Preconditions

- Built on a branch **stacked on task 024** — `project.list`, widened `doc.list` (`{path,type}`),
  `/api/doc/raw`, and the harness flags must exist. If these RPCs/routes 404 or `doc.list` lacks
  `type`, stop: 024 is not in place.
- `agent-browser` available headless on this host (confirmed: `open`/`get`/`eval`/`select`/`click`/
  `snapshot`/`screenshot`).
- **Project ids MUST be lowercase** matching `^[a-z0-9]+(?:-[a-z0-9]+)*$`. The backend lowercases
  the lookup, so an uppercase id (`projA`) makes `doc.list` fail "project not found". Use `proj-a`/
  `proj-b`.

## Fixture builder (isolated, non-destructive — CLAUDE.md: populate disk, run as a user, clean up)

Build a temp registry + docs trees so the harness never touches `~/.auto/projects.json` or real docs.
Each project gets a `docs/` tree with tasks/epics/research, ≥1 `.md`, and ≥1 self-contained `.html`;
a second registry is empty for the AC-1 empty-state case.

```bash
TMP=$(mktemp -d); EMPTY=$(mktemp -d)

# proj-a: rich docs tree (LOWERCASE id) — note the mkdir creates the artifacts/ dir
# the doc.html heredoc writes into.
mkdir -p "$TMP/proj-a/docs/tasks/001-demo/artifacts" "$TMP/proj-a/docs/epics" "$TMP/proj-a/docs/research"
printf '# Demo plan\n\nhello **markdown**\n' > "$TMP/proj-a/docs/tasks/001-demo/plan.md"
printf '# Demo reqs\n'                        > "$TMP/proj-a/docs/tasks/001-demo/requirements.md"
printf '# Epic\n'                             > "$TMP/proj-a/docs/epics/001-epic.md"
printf '# Research\n'                          > "$TMP/proj-a/docs/research/notes.md"
cat > "$TMP/proj-a/docs/tasks/001-demo/artifacts/doc.html" <<'HTML'
<!doctype html><html><body><h1 id="hdoc">HTML planning doc</h1></body></html>
HTML

<!-- RESOLVED(P3): Fixture builder writes doc.html into a directory it never creates
REVIEW: Line 24 `mkdir -p` creates `.../001-demo` but not `.../001-demo/artifacts`, then the `cat >
.../001-demo/artifacts/doc.html` heredoc writes into that missing `artifacts/` dir, which `cat` will not create — the
fixture build fails before the harness runs. Add `$TMP/projA/docs/tasks/001-demo/artifacts` to the `mkdir -p` on line
24 (or `mkdir -p "$(dirname ...)"` before the heredoc). Skeleton, but worth fixing so Phase 6 inherits a runnable
builder.
AUTHOR: Fixed — the `mkdir -p` now creates `$TMP/proj-a/docs/tasks/001-demo/artifacts` so the `doc.html` heredoc
writes into an existing directory.
-->

# proj-b: minimal + its own html (LOWERCASE id)
mkdir -p "$TMP/proj-b/docs/tasks/002-other/artifacts"
printf '# B root doc\n'      > "$TMP/proj-b/docs/readme.md"
printf '# B plan\n\n**bee**\n' > "$TMP/proj-b/docs/tasks/002-other/plan.md"
cat > "$TMP/proj-b/docs/tasks/002-other/artifacts/page.html" <<'HTML'
<!doctype html><html><body><h1>B HTML</h1></body></html>
HTML

# Registry — ids are lowercase to satisfy ^[a-z0-9]+(?:-[a-z0-9]+)*$
cat > "$TMP/projects.json" <<JSON
{"projects":[
  {"id":"proj-a","name":"Project A","path":"$TMP/proj-a","remote":"https://github.com/x/a.git"},
  {"id":"proj-b","name":"Project B","path":"$TMP/proj-b","remote":""}
]}
JSON

# Empty registry for the AC-1 empty-state case
printf '{"projects":[]}\n' > "$EMPTY/projects.json"
```

## Build

```bash
# Embed (default / shipped artifact):
cd auto-cli && go build -o /tmp/auto-025-embed ./cmd/auto
# Dev (-tags dev, disk-served; MUST run from auto-ui/ so web/static resolves):
cd auto-cli && go build -tags dev -o /tmp/auto-025-dev ./cmd/auto
```

## Launch (isolated instance, 024 harness)

`--ready-file` writes `{"addr":"127.0.0.1:NNNN"}` after binding; parse `addr` from it and derive the
port from the tail. `AUTO_UI_DEBUG=1` enables the server debug buffer (1.7); the client
`window.__autoui` is gated separately by the `?debug=1` URL param.

```bash
BIN=/tmp/auto-025-embed   # or /tmp/auto-025-dev, run from auto-ui/ for the dev build

AUTO_UI_DEBUG=1 "$BIN" ui serve --port 0 --ready-file "$TMP/ready.json"   --projects "$TMP/projects.json"   &
AUTO_UI_DEBUG=1 "$BIN" ui serve --port 0 --ready-file "$EMPTY/ready.json" --projects "$EMPTY/projects.json" &
# Wait for both ready files, then read the bound addrs:
ADDR=$(node  -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1])).addr)' "$TMP/ready.json")
EADDR=$(node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1])).addr)' "$EMPTY/ready.json")
PORT=${ADDR##*:}   # emit targets this port

# Dev build only — confirm disk serving + no-store:
curl -s  "http://$ADDR/api/hello"            # {"mode":"disk"}  (embed -> "embed")
curl -sI "http://$ADDR/app.js" | grep -i cache-control   # Cache-Control: no-store
```

A runnable end-to-end driver implementing every assertion below (used for the Phase 6 evidence) is
checked in nearby reasoning, but the canonical commands are inline here.

## AC-1 — Project switcher  *(Phase 3)*

- [x] **Cold-load (WS readiness):** `agent-browser open http://$ADDR/?debug=1#/explore` with no prior
      connection → the switcher **and** the tree populate **without a reload**
      (`nav[data-doc-count]` > 0). Asserts `await whenOpen()` gating — a regression renders an
      empty/"not connected" explorer until a manual reload.
- [x] `get attr [data-testid=project-switcher]` options carry `data-project="proj-a"`, `"proj-b"`.
- [x] `agent-browser select [data-testid=project-switcher] proj-b` → `get url` shows `project=proj-b`;
      tree re-lists; content pane shows the "Select a document" placeholder (cleared).
- [x] `open http://$ADDR/?debug=1#/explore?project=proj-a` directly → switcher `.value === "proj-a"`,
      its tree loaded (reload-survival).
- [x] `open http://$EADDR/#/explore` → `data-testid="no-projects"` present; body contains no "failed"
      error text.

## AC-2 — Doc tree  *(Phase 3)*

- [x] On proj-a the tree shows groups **Tasks** (→ `001-demo` → `plan.md`/`requirements.md`/
      `doc.html`), **Epics**, **Research** (assert each group label is present in the `nav`).
- [x] `nav[data-doc-count]` == length of `call("doc.list",{project:"proj-a"})` (both `5` in the
      fixture).
- [x] The `plan.md` leaf carries `data-doc-path="docs/tasks/001-demo/plan.md"`,
      `data-doc-type="markdown"`. Click it → `get url` gains
      `path=docs%2Ftasks%2F001-demo%2Fplan.md`; the content pane loads.

## AC-3 — Type-aware content pane + default view  *(Phase 3 + Phase 4)*

- [x] Markdown: `plan.md` selected → the pane `innerHTML` contains `<strong>markdown</strong>`; the
      `article` root carries `data-revision` + a non-empty `data-last-updated`.
- [x] HTML: click `doc.html` (`data-doc-type="html"`) → `[data-testid=doc-iframe]` `src` contains
      `/api/doc/raw?project=proj-a&path=docs%2Ftasks%2F001-demo%2Fartifacts%2Fdoc.html` + `&v=<nonce>`;
      an `<a target="_blank">open in new tab</a>` to the same URL exists.
- [x] Click `data-testid="doc-refresh"` → `data-revision` increments; the iframe `v=` nonce changes.
- [x] Default view: `open http://$ADDR/` (bare) → `get url` ends `#/explore`; no `Home`/`Dashboard`
      buttons/text; `[data-testid=conn-indicator]` `data-conn-status === "open"`.

## AC-4 — `window.__autoui` ring buffer  *(Phase 1, re-asserted Phase 3)*

- [x] `open http://$ADDR/?debug=1#/explore` → `eval "typeof window.__autoui"` == `"object"`.
- [x] `open http://$ADDR/#/explore` (no `?debug=1`) → `eval "typeof window.__autoui"` == `"undefined"`.
- [x] With `?debug=1`: `"$BIN" ui emit --port "$PORT" --project proj-a --path docs/tasks/001-demo/plan.md`
      → `eval "window.__autoui.events.filter(e=>e.method==='doc.changed').length"` ≥ 1
      (captured even though the static explorer does not re-render). The captured payload carries the
      changed path at `params.data.path` (see `evidence/embed-doc-changed-event.json`).

## AC-5 — `/debug` page  *(Phase 5)*

- [x] `open http://$ADDR/?debug=1#/debug` → all four sections present by testid:
      `debug-connection`, `debug-event-log`, `debug-error-log`, `debug-current-state`.
- [x] Connection rows show status `connected` (raw `open`), `/api/hello` `mode`
      (`embed`/`disk`), and the bound host (`location.host`).
- [x] `"$BIN" ui emit --port "$PORT" --project proj-a --path docs/tasks/001-demo/requirements.md`
      → a `doc.changed` row appears in `[data-testid=debug-event-row]` (type/time/project/path).
- [x] Force an error: `eval` importing `./rpc.js` and calling
      `call("doc.get",{project:"proj-a",path:"docs/nope.md"})` (rejects) → a row appears in
      `[data-testid=debug-error-row]`.

## Evidence

- [x] Embed run: `evidence/embed-results.txt` (36/36 PASS) + `embed-debug-page.png`,
      `embed-doc-changed-event.json`, `embed-emit.json`, `embed-error-rows.json`.
- [x] Dev run: `evidence/dev-results.txt` (36/36 PASS) + `dev-build.md` (disk-serving +
      `Cache-Control: no-store` confirmation) + the matching `dev-*.json`/`.png` captures.

## Cleanup

```bash
kill %1 %2 2>/dev/null                 # the two serve processes (or kill by exact PID)
agent-browser close --all 2>/dev/null
rm -rf "$TMP" "$EMPTY"                  # if an rm -rf guard blocks, use: find "$TMP" -delete
```
