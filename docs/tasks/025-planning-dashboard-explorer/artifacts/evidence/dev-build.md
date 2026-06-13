# Dev-build conformance note (Task 025, Step 6.3)

Re-ran the full AC-1..AC-5 conformance suite against the **dev** build
(`go build -tags dev`), served from the `auto-ui/` module root so the
disk-backed `os.DirFS("web/static")` asset FS resolves.

## Build + launch

```
cd auto-cli && go build -tags dev -o /tmp/auto-025-dev ./cmd/auto
cd auto-ui    # cwd matters: dev assets resolve relative to web/static
AUTO_UI_DEBUG=1 /tmp/auto-025-dev ui serve --port 0 --ready-file <tmp>/ready.json --projects <tmp>/projects.json
```

## Result

**36 / 36 assertions PASS, 0 FAIL** — identical to the embed run
(`dev-results.txt` in this directory). Every AC behaves the same as embed; the
only observable differences are the asset-delivery mode and headers below.

## Disk-serving + Cache-Control evidence

`/api/hello` reports `{"mode":"disk"}` (embed reports `{"mode":"embed"}`).

Every static asset is served from disk with `Cache-Control: no-store` (013
feedback: stale assets without no-store):

```
$ curl -sI http://$ADDR/app.js | grep -i cache-control
Cache-Control: no-store
```

Confirmed `no-store` on all SPA modules: `app.js`, `rpc.js`, `explorer.js`,
`content.js`, `tree.js`, `debug.js`, `uistate.js`, and `index.html`. The embed
build emits **no** `Cache-Control` header on `app.js` (served from the embedded
FS), which is the expected mode difference — the dev build adds `no-store` so an
edit-and-reload iteration loop never serves a stale module.

## Notes / assumptions

- The dev and embed builds share the same `web/static/*` sources (embed via
  `//go:embed`, dev via disk), so a single conformance suite covers both; the
  only build-tag-sensitive surface is the asset handler (mode + Cache-Control),
  which is asserted here.
- The emit client for the AC-4/AC-5 triggers was the dev binary
  (`EMIT_BIN=/tmp/auto-025-dev`); emit only POSTs to the running server's port,
  so the client binary is immaterial — both builds derive one `doc.changed`.
