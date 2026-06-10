# Autoui

Local web dashboard and HTTP server for the auto stack. Serves a self-contained, no-build Preact+htm single-page app either embedded in the binary (default) or live-from-disk (dev mode) for fast iteration.

## Build

```bash
cd auto-ui
go build ./...
```

The merged `auto` binary is built from the repo root with `make build` (the UI tool ships as `auto ui`).

## Test

```bash
cd auto-ui
go test ./...
```

## Vet

```bash
cd auto-ui
go vet ./...
```

## Dev server

Live-from-disk assets — edit `web/static/*` and reload the browser with no Go rebuild. Build the `auto` binary with the `dev` tag, then run it from the `auto-ui/` module root (assets resolve relative to cwd):

```bash
go build -tags dev -o bin/auto ./auto-cli/cmd/auto   # from repo root
cd auto-ui && ../bin/auto ui serve
```

## Embedded single binary

The default build of the merged `auto` binary embeds `web/static/` via `//go:embed`, producing a self-contained binary:

```bash
make build && ./bin/auto ui serve
```

## Architecture

- `rootcmd/` — public seam mounted by the merged `auto` binary as `auto ui`
- `internal/app/` — runtime context (stdout, stderr, cwd)
- `internal/cli/` — Cobra commands (init, doctor, quickstart, docs, update, serve)
- `internal/config/` — settings loading and validation (~/.auto/ui/settings.json)
- `internal/server/` — HTTP handler: `/api/hello`, `/api/ws`, plus static file server
  - `rpc.go` — transport-agnostic JSON-RPC 2.0 dispatcher (request/response + notifications)
  - `ws.go` — `/api/ws` WebSocket handler (coder/websocket): per-connection session with a
    single write pump, a 1s server-push `ping` notification, and a client-callable `ping` RPC
- `web/` — build-tag split asset delivery (`embed_prod.go` embeds, `embed_dev.go` reads from disk)
- `web/static/` — no-build Preact+htm SPA (index.html, app.js, router.js)
  - `rpc.js` — singleton JSON-RPC 2.0 client over WebSocket (`call`/`on`/`onStatus`); derives
    `wss://` vs `ws://` from the page origin so it works behind `tailscale serve` (HTTPS)
  - `vendor/pico.min.css` — vendored Pico CSS v2 (embedded; offline-capable)
