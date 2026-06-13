# Conformance Harness: Task 025 — Planning Dashboard Explorer

> **Skeleton** produced at planning time; Phase 6 fills the exact commands + evidence.
> Acceptance for 025 is browser-driven (frontend-only, no Go tests). Drive the served SPA with
> `agent-browser` and assert via `data-*` attributes + `eval` (not rendered-text scraping).
> Re-run on **both** the embed build and the dev (`-tags dev`) build (013 feedback: browser-layer
> defects are invisible to Go tests).

## Preconditions

- Built on a branch **stacked on task 024** — `project.list`, widened `doc.list` (`{path,type}`),
  `/api/doc/raw`, and the harness flags must exist. If these RPCs/routes 404 or `doc.list` lacks
  `type`, stop: 024 is not in place.
- `agent-browser` available headless on this host (confirmed working: `open`/`get`/`eval`/
  `snapshot`/`screenshot`).

## Fixture builder (isolated, non-destructive — CLAUDE.md: populate disk, run as a user, clean up)

Build a temp registry + docs trees so the harness never touches `~/.auto/projects.json` or real docs:

```
TMP=$(mktemp -d)
# Project A: rich docs tree
mkdir -p $TMP/projA/docs/tasks/001-demo/artifacts $TMP/projA/docs/epics $TMP/projA/docs/research
printf '# Demo plan\n\nhello **markdown**\n'      > $TMP/projA/docs/tasks/001-demo/plan.md
printf '# Demo reqs\n'                            > $TMP/projA/docs/tasks/001-demo/requirements.md
printf '# Epic\n'                                 > $TMP/projA/docs/epics/001-epic.md
printf '# Research\n'                             > $TMP/projA/docs/research/notes.md
cat > $TMP/projA/docs/tasks/001-demo/artifacts/doc.html <<'HTML'   # self-contained HTML doc
<!doctype html><html><body><h1 id="hdoc">HTML planning doc</h1></body></html>
HTML

<!-- RESOLVED(P3): Fixture builder writes doc.html into a directory it never creates
REVIEW: Line 24 `mkdir -p` creates `.../001-demo` but not `.../001-demo/artifacts`, then the `cat >
.../001-demo/artifacts/doc.html` heredoc writes into that missing `artifacts/` dir, which `cat` will not create — the
fixture build fails before the harness runs. Add `$TMP/projA/docs/tasks/001-demo/artifacts` to the `mkdir -p` on line
24 (or `mkdir -p "$(dirname ...)"` before the heredoc). Skeleton, but worth fixing so Phase 6 inherits a runnable
builder.
AUTHOR: Fixed — the `mkdir -p` now creates `$TMP/projA/docs/tasks/001-demo/artifacts` so the `doc.html` heredoc
writes into an existing directory.
-->

# Project B: minimal
mkdir -p $TMP/projB/docs
printf '# B root doc\n'                            > $TMP/projB/docs/readme.md
# Registry
cat > $TMP/projects.json <<JSON
{"projects":[
  {"id":"projA","name":"Project A","path":"$TMP/projA","remote":"https://github.com/x/a.git"},
  {"id":"projB","name":"Project B","path":"$TMP/projB","remote":""}
]}
JSON
# Empty registry for the AC-1 empty-state case
EMPTY=$(mktemp -d); printf '{"projects":[]}\n' > $EMPTY/projects.json
```

## Launch (isolated instance, 024 harness)

```
AUTO_UI_DEBUG=1 auto ui serve --port 0 --ready-file $TMP/ready.json --projects $TMP/projects.json &
ADDR=$(node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1])).addr)' $TMP/ready.json)
# Empty-registry instance for AC-1:
AUTO_UI_DEBUG=1 auto ui serve --port 0 --ready-file $EMPTY/ready.json --projects $EMPTY/projects.json &
EADDR=$(node -e '...'$EMPTY/ready.json)
```

## AC-1 — Project switcher  *(Phase 3)*
- [ ] **Cold-load (WS readiness):** with no prior connection, `agent-browser open http://$ADDR/#/explore` → the switcher **and** the tree populate **without a reload** (asserts `await whenOpen()` gating works — a regression here renders an empty/"not connected" explorer until manual reload).
- [ ] `agent-browser open http://$ADDR/#/explore`
- [ ] `get attr [data-testid=project-switcher] …` → options carry `data-project="projA"`, `"projB"`.
- [ ] Click `data-project="projB"` → `get url` shows `project=projB`; tree re-lists; content pane cleared.
- [ ] `open http://$ADDR/#/explore?project=projA` directly → switcher shows Project A selected, its tree loaded (reload-survival).
- [ ] `open http://$EADDR/#/explore` → `data-testid="no-projects"` present; no error.

## AC-2 — Doc tree  *(Phase 3)*
- [ ] On Project A: tree shows groups **Tasks** (→ `001-demo` → `plan.md`/`requirements.md`/`doc.html`), **Epics**, **Research**.
- [ ] `eval` tree root `data-doc-count` == length of `call("doc.list",{project:"projA"})`.
- [ ] Click the `plan.md` leaf (`data-doc-path="docs/tasks/001-demo/plan.md"`, `data-doc-type="markdown"`) → `get url` gains that `path=`; content pane loads.

## AC-3 — Type-aware content pane + default view  *(Phase 3 + Phase 4)*
- [ ] Markdown: `plan.md` selected → pane shows rendered `<strong>markdown</strong>`; pane root has `data-revision` + `data-last-updated`.
- [ ] HTML: select `doc.html` (`data-doc-type="html"`) → `get attr [data-testid=doc-iframe] src` contains `/api/doc/raw?project=projA&path=docs/tasks/001-demo/artifacts/doc.html` + `&v=<nonce>`; an "open in new tab" `<a target="_blank">` to the same URL exists.
- [ ] Click `data-testid="doc-refresh"` → `data-revision` increments; iframe `v=` nonce changes.
- [ ] Default view: `open http://$ADDR/` (bare) → `get url` ends `#/explore`; no `Home`/`Dashboard` buttons/text; `data-testid="conn-indicator"` reads `connected`.

## AC-4 — `window.__autoui` ring buffer  *(Phase 1, re-asserted Phase 3)*
- [ ] `open http://$ADDR/?debug=1#/explore` → `eval "typeof window.__autoui"` == `"object"`.
- [ ] `open http://$ADDR/#/explore` (no `?debug=1`) → `eval "typeof window.__autoui"` == `"undefined"`.
- [ ] With `?debug=1`: `auto ui emit --project projA --path docs/tasks/001-demo/plan.md` →
      `eval "window.__autoui.events.filter(e=>e.method==='doc.changed').length"` ≥ 1
      (captured even though the static explorer does not re-render).

## AC-5 — `/debug` page  *(Phase 5)*
- [ ] `open http://$ADDR/?debug=1#/debug`; `snapshot` shows four sections with `data-testid`:
      connection, event-log, error-log, current-state.
- [ ] Connection section shows status `open`/`connected`, `/api/hello` `mode`, bound port (`location.host`).
- [ ] `auto ui emit …` → a `doc.changed` row appears in the event log (type/time/project/path).
- [ ] Force an error: `eval` `call("doc.get",{project:"projA",path:"docs/nope.md"})` (rejects) → an
      entry appears in the error-log section.

## Evidence
- [ ] Save `eval` outputs + `screenshot`/`snapshot` per AC under `artifacts/evidence/` (embed run).
- [ ] Repeat the suite on the **dev build** (`go build -tags dev …`, serve from `auto-ui/`); confirm
      identical results + `Cache-Control: no-store` on assets.

## Cleanup
```
kill %1 %2 2>/dev/null; rm -rf $TMP $EMPTY
```
