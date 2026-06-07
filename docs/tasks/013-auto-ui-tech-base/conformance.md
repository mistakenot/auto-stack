# Conformance Script: Task 013 (agent-browser)

Browser-driven conformance for the auto-ui tech base. A Go test cannot exercise a no-build,
browser-run Preact+htm SPA, so **agent-browser** drives a real browser to prove the rendering,
routing, fetch, and dev-mode hot-reload acceptance criteria. Go tests cover only the server
contract (AC-3 server side, AC-4).

## View / URL model under test

Two views, all state in `location.hash`:

```
+-----------------------------------------------------------+
|  auto-ui          [ Home ]  [ Dashboard ]                 |   <- nav updates the hash
+-----------------------------------------------------------+
|  Home                                                     |
|    clicked: 3      [ + ]                                  |   <- counter value lives in ?n=
|                                                           |
|  URL:  #/home?n=3                                         |
+-----------------------------------------------------------+

+-----------------------------------------------------------+
|  Dashboard                                                |
|    [ fetch from go ]                                      |
|    go says: hi from go (mode=disk)                        |   <- from GET /api/hello
|                                                           |
|  URL:  #/dashboard                                        |
+-----------------------------------------------------------+
```

## Preconditions

- `auto-shared` builds; repo at a clean state on the task branch.
- agent-browser available to drive a Chromium-class browser.

## Steps

### Step 1 — AC-1: embedded SPA renders from the single binary
1. `cd auto-ui && go build -o /tmp/autoui ./cmd/autoui` (default tags → embedded assets).
2. Run `/tmp/autoui serve --port 8080` (background).
3. agent-browser: navigate to `http://localhost:8080/`.
4. **Assert**: `#app` contains the rendered shell (nav with "Home"/"Dashboard" visible). No console
   errors; no request to any non-localhost asset server other than the esm.sh CDN module imports.
5. Stop the binary.

### Step 2 — start dev server for the remaining steps
1. `cd auto-ui && go run -tags dev ./cmd/autoui serve --port 8080` (background).
2. **Assert**: stderr shows `assets=disk`.

### Step 3 — AC-3: frontend ↔ Go API round trip
1. agent-browser: navigate to `http://localhost:8080/#/dashboard`.
2. Click **fetch from go**.
3. **Assert**: the DOM shows the message from `GET /api/hello` (e.g. `go says: hi from go (mode=disk)`).

### Step 4 — AC-5: state survives reload via the URL
1. agent-browser: navigate to `#/home`, click **+** three times.
2. **Assert**: counter shows `3` and the URL is `#/home?n=3`.
3. Reload the page.
4. **Assert**: view is still Home and counter is restored to `3` from the hash (not reset to 0).
5. Click **Dashboard**; **assert** URL becomes `#/dashboard` and the Dashboard view renders.

### Step 5 — AC-2: dev-mode edit → refresh (no Go rebuild)
1. While the `-tags dev` server from Step 2 is still running, edit a visible string in
   `auto-ui/web/static/app.js` (e.g. change the nav label "Dashboard" → "Dashboard ✦").
2. agent-browser: reload `http://localhost:8080/`.
3. **Assert**: the new label is visible — confirming the change was picked up with **no Go rebuild
   and no bundler step** (the Go process was never restarted).
4. Revert the edit.

### Teardown
- Stop the dev server.

## Pass criteria

All asserts in Steps 1, 3, 4, 5 pass, and `internal/server/server_test.go` passes for the AC-3
(server side) and AC-4 contract. Record the agent-browser run (screenshots/log) under the task
folder or PR for evidence.
```
