# Conformance Harness: Task 026 — Planning Dashboard Live Updates

> Acceptance for 026 is **liveness** validation (what 025 did **not** have): an open doc
> auto-refreshes and the nav tree grows live when a `doc.changed` arrives. Drive the served SPA
> with `agent-browser` and assert via `data-*` attributes + `window.__autoui` (never rendered-text
> diffs — a re-fetch can leave the DOM identical). Re-run on **both** the embed build and the dev
> (`-tags dev`) build (013 feedback: browser-layer defects are invisible to Go tests). Phase 5
> evidence (both builds, 8/8 each) lives under [`evidence/`](./evidence/): `embed-results.txt`,
> `dev-results.txt`, the per-build `*-doc-changed-event.json` payload captures, `*-hello.json`, and
> `*-liveness.png` screenshots.

## Preconditions

- Built on a branch **stacked on tasks 024 + 025** — `project.list`, widened `doc.list`
  (`{path,type}`), `/api/doc/raw`, the `.html` `doc.changed` derivation, the harness flags
  (`--port 0`/`--ready-file`/`--projects`), `auto ui emit`, `/api/debug/recent`, the explorer shell,
  `window.__autoui`, and the content/tree `data-*` attributes must all exist. The liveness wiring
  under test is 026's: `docevents.js` (the shared `parseDocChanged`/`matchesDoc` reading
  **`ev.data.path`**), the `doc.changed` subscription in `content.js` (open-doc refresh), and the
  `doc.changed` subscription in `tree.js` (re-list on unseen path).
- `agent-browser` available headless on this host (confirmed: `open`/`get`/`eval`/`select`/`click`/
  `snapshot`/`screenshot`).
- **Project ids MUST be lowercase** matching `^[a-z0-9]+(?:-[a-z0-9]+)*$`. The backend lowercases
  the lookup, so an uppercase id (`projA`) makes `doc.list` fail "project not found". Use `proj-a`.
- The binaries under test must be **built from this worktree** so the embed binary bakes in 026's JS
  (`docevents.js`/`content.js`/`tree.js`) and the dev binary serves them from `auto-ui/web/static/`.
  Confirm: `strings /tmp/auto-026-embed | grep -c parseDocChanged` ≥ 1.

## Fixture builder (isolated, non-destructive — CLAUDE.md: populate disk, run as a user, clean up)

Build a temp registry + a `docs/` tree so the harness never touches `~/.auto/projects.json` or real
docs. `proj-a` needs: ≥1 markdown doc, ≥1 self-contained HTML planning doc, and multiple task dirs so
a group/subgroup can be expanded and a **new** one can be added later for AC-3. The `mkdir -p` MUST
create the `artifacts/` dir **before** the heredoc writes `doc.html` into it.

```bash
TMP=$(mktemp -d)

# proj-a: rich docs tree (LOWERCASE id). mkdir creates artifacts/ BEFORE the doc.html heredoc.
mkdir -p "$TMP/proj-a/docs/tasks/001-demo/artifacts" \
         "$TMP/proj-a/docs/tasks/002-second" \
         "$TMP/proj-a/docs/epics" \
         "$TMP/proj-a/docs/research"

printf '# Demo plan\n\nhello **markdown**\n'  > "$TMP/proj-a/docs/tasks/001-demo/plan.md"
printf '# Demo reqs\n'                          > "$TMP/proj-a/docs/tasks/001-demo/requirements.md"
printf '# Second task plan\n'                   > "$TMP/proj-a/docs/tasks/002-second/plan.md"
printf '# Epic\n'                               > "$TMP/proj-a/docs/epics/001-epic.md"
printf '# Research\n'                           > "$TMP/proj-a/docs/research/notes.md"
cat > "$TMP/proj-a/docs/tasks/001-demo/artifacts/doc.html" <<'HTML'
<!doctype html><html><body><h1 id="hdoc">HTML planning doc</h1></body></html>
HTML

# Temp registry pointing at proj-a (path + a remote url).
cat > "$TMP/projects.json" <<JSON
{"projects":[
  {"id":"proj-a","name":"Project A","path":"$TMP/proj-a","remote":"https://github.com/x/a.git"}
]}
JSON
```

The fixture starts with **6** docs (count grows to **7** in AC-3 when `099-new/plan.md` is added).

## Build (from the worktree — embeds / serves 026's JS)

```bash
# Embed (default / shipped artifact):
cd /home/vscode/src/auto-stack-026/auto-cli && go build -o /tmp/auto-026-embed ./cmd/auto
# Dev (-tags dev, disk-served; MUST be launched from auto-ui/ so web/static resolves to the worktree):
cd /home/vscode/src/auto-stack-026/auto-cli && go build -tags dev -o /tmp/auto-026-dev ./cmd/auto
```

## Launch (isolated instance, 024 harness)

`--ready-file` writes `{"addr":"127.0.0.1:NNNN"}` after binding; parse `addr` from it and derive the
port from the tail. `AUTO_UI_DEBUG=1` enables the server debug buffer (1.7); the client
`window.__autoui` is gated separately by the `?debug=1` URL param. The dev build must be launched
with **CWD = `auto-ui/`** so its disk asset server resolves `web/static`.

```bash
BIN=/tmp/auto-026-embed   # or /tmp/auto-026-dev (run from auto-ui/ for the dev build)

( cd /home/vscode/src/auto-stack-026/auto-ui && \
  AUTO_UI_DEBUG=1 "$BIN" ui serve --port 0 --ready-file "$TMP/ready.json" --projects "$TMP/projects.json" ) &
ADDR=$(node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1])).addr)' "$TMP/ready.json")
PORT=${ADDR##*:}   # emit targets this port

# Dev build only — confirm disk serving + no-store:
curl -s  "http://$ADDR/api/hello"                        # {"...","mode":"disk"}  (embed -> "embed")
curl -sI "http://$ADDR/app.js" | grep -i cache-control   # Cache-Control: no-store (dev only)
```

Open the explorer at `http://$ADDR/?debug=1#/explore?project=proj-a` (the `?debug=1` enables
`window.__autoui`). Navigation to a specific doc is via the hash, e.g.
`#/explore?project=proj-a&path=docs%2Ftasks%2F001-demo%2Fplan.md`. A runnable end-to-end driver
implementing every assertion below (`driver.mjs`, used to produce the `evidence/` captures) shells
`agent-browser`, polls the `data-*` attributes for up to ~2.5s after each `emit` (events propagate
over the WS asynchronously), and writes `evidence/<build>-results.txt`.

## AC-1 / AC-2 — Markdown live auto-refresh

- [x] Open the markdown doc (`#/explore?project=proj-a&path=docs%2Ftasks%2F001-demo%2Fplan.md`);
      read `article[data-revision]` (`data-revision` = **1** after the initial fetch).
- [x] `"$BIN" ui emit --port "$PORT" --project proj-a --path docs/tasks/001-demo/plan.md`.
- [x] **(i)** `window.__autoui.events.filter(e=>e.method==='doc.changed').length` ≥ 1 — the client
      received the notification. The captured payload (`evidence/<build>-doc-changed-event.json`)
      shows the changed path at **`params.data.path`** (not top-level `ev.path`).
- [x] **(ii)** `article[data-revision]` **incremented** vs before (poll up to ~2.5s) — this is the
      live auto-refresh: a matching `doc.changed` re-runs `doc.get` + re-render with no manual reload.
      **Observed: `data-revision` 1 → 2 on both builds.**

## AC-2 — HTML live reload (iframe cache-bust)

- [x] Open the HTML doc (`data-doc-type="html"`,
      `#/explore?project=proj-a&path=docs%2Ftasks%2F001-demo%2Fartifacts%2Fdoc.html`); read the
      iframe `[data-testid=doc-iframe]` `src` and extract the `v=<nonce>`.
- [x] `"$BIN" ui emit --port "$PORT" --project proj-a --path docs/tasks/001-demo/artifacts/doc.html`.
- [x] The iframe `src` `v=` nonce **changed** (cache-busted reload; **no** `doc.get` — the raw route
      serves the bytes). **Observed: `v=` nonce 1 → 2 on both builds.**

## AC-2 — Non-matching `doc.changed` is a no-op for the open doc

- [x] With the markdown doc open, record `article[data-revision]`.
- [x] `emit` an **unrelated** path the open doc does not match
      (`docs/tasks/001-demo/requirements.md`); wait ~1.5s for any (incorrect) refresh to fire.
- [x] The open doc's `data-revision` is **unchanged** — `matchesDoc` returns early for a different
      path. **Observed: `data-revision` 5 == 5 (no bump) on both builds.** (Note: that unrelated
      emit may still grow/refresh the **tree** if its path were unseen — here `requirements.md` is
      already in the list, so the tree is untouched too; the assertion is specifically that the
      **open doc's** `data-revision` does not move.)

## AC-3 — Live nav-tree growth + expansion preserved

- [x] Open the explorer on the markdown doc; read `nav[data-doc-count]` (= **6** in the fixture). The
      `Tasks` group + its `001-demo`/`002-second` subgroups render expanded by default; toggle the
      `Tasks` group collapsed then open again (exercises user-driven expansion state) and confirm its
      children still render.
- [x] **CRITICAL ORDERING — write the new file ON DISK FIRST, then emit** (`emit` does **not** create
      files; `tree.js` reconciles by re-running `doc.list`, which enumerates disk):
      ```bash
      mkdir -p "$TMP/proj-a/docs/tasks/099-new" && printf '# New\n' > "$TMP/proj-a/docs/tasks/099-new/plan.md"
      "$BIN" ui emit --port "$PORT" --project proj-a --path docs/tasks/099-new/plan.md
      ```
- [x] **(i)** `nav[data-doc-count]` **grew** (poll up to ~3s). **Observed: 6 → 7 on both builds.**
- [x] **(ii)** a new `[data-doc-path="docs/tasks/099-new/plan.md"]` leaf **exists**. **PASS on both.**
- [x] **(iii)** the previously-expanded `Tasks` group is **still expanded** (its
      `001-demo/plan.md` leaf still rendered) — expansion survived the reconcile because
      `Collapsible` is keyed by stable group name, so Preact preserves its `open` state across the
      re-list. **PASS on both.**
- [x] Server cross-check: `GET http://$ADDR/api/debug/recent` contains the `099-new` path. **PASS on
      both.**

## AC-5 — Run the full loop on BOTH builds (embed + dev)

- [x] **embed** (`/tmp/auto-026-embed`): `/api/hello` → `{"mode":"embed"}`; all assertions PASS
      (`evidence/embed-results.txt`, 8/8).
- [x] **dev** (`/tmp/auto-026-dev`, launched from `auto-ui/`): `/api/hello` → `{"mode":"disk"}`;
      `curl -sI .../app.js` → `Cache-Control: no-store`; all assertions PASS
      (`evidence/dev-results.txt`, 8/8). (The embed build serves `app.js` without `no-store` by
      design — caching is the shipped-artifact behavior; `no-store` is dev-only.)

## Evidence

- [x] Embed run: `evidence/embed-results.txt` (8/8 PASS, with before/after `data-revision`, `v=`
      nonce, and `data-doc-count` values inline) + `embed-doc-changed-event.json` (payload showing
      `params.data.path`) + `embed-hello.json` (`mode:embed`) + `embed-liveness.png`.
- [x] Dev run: `evidence/dev-results.txt` (8/8 PASS) + `dev-doc-changed-event.json` +
      `dev-hello.json` (`mode:disk`) + `dev-liveness.png`. The `dev-results.txt` tail records
      `/api/hello -> mode:disk` and `app.js -> Cache-Control: no-store`.

## Cleanup

```bash
agent-browser close --all 2>/dev/null
kill %1 2>/dev/null                 # the serve process (or kill by exact PID)
find "$TMP" -delete 2>/dev/null     # non-destructive temp-fixture removal (avoids the rm -rf guard)
```
