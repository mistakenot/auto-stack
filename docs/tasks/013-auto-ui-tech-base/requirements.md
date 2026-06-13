---
hash: "c29ea519"
id: "0862adc9"
read_when: "implementing or reviewing the auto-ui technical base requirements and SPA architecture"
summary: "Requirements for the auto-ui package: a Go binary serving a no-build Preact+htm SPA with hash-based routing, embed mode, dev mode hot-reload, and a REST/WebSocket API."
title: "Requirements: Task 013 — Auto UI Tech Base"
---

# Task 013: auto-ui-tech-base

## Problem

The auto-stack has several CLIs producing data (sessions, docs, graphs, search indexes) but no
way to view any of it in a browser. Before building a real dashboard we need to prove the
technical base: a single Go binary that serves a self-contained SPA for **local development
only**, with **no frontend build step** and the fastest possible edit→refresh loop.

## Goals

- A new `auto-ui/` package: one Go process that serves both an HTTP API and a SPA.
- **No bundler / no node_modules / no transpile step.** Frontend is **Preact + htm** authored as
  plain `.js` files the browser runs directly; CDN deps (esm.sh) wired via an import map, with the
  `preact/compat` alias kept ready as an escape hatch for future React libs.
- **Dev mode**: serve `web/` from disk (`os.DirFS`) — edit a file, hit refresh, see the change.
  No Go rebuild needed for frontend edits.
- **Embed mode**: `go build` (default tags) embeds `web/` into the binary via `//go:embed` for a
  single self-contained artifact.
- **State-in-URL**: application state lives in the URL via **hash-based routing** (`#/view?…`) so
  a refresh restores view + state, compensating for the lack of HMR. The server always serves
  `index.html`; no server-side route matching is needed.
- Demonstrate the full stack working end to end: frontend renders, calls a Go `/api/*` endpoint,
  and shows the result; navigating between ≥2 views updates the URL hash and survives reload.

## Acceptance Criteria

**AC-1**: Single binary serves the SPA
- Given: `auto-ui` is built with default build tags
- When: the user runs the binary and opens `http://localhost:<port>/`
- Then: the SPA's own assets (HTML/JS) are served entirely by the Go binary with no separate local
  asset/dev server and no bundler. (Framework deps — preact/htm — still load from the esm.sh CDN at
  runtime; offline vendoring is deferred per Q4, so first load needs network.)

<!-- RESOLVED(P2): AC-1 "without any external asset server" contradicts the CDN dependency
REVIEW: This AC says the SPA renders "without any external asset server", but per Q4 / solution.md
the framework (preact, preact/hooks, htm) is loaded from esm.sh at runtime via the import map — esm.sh
IS an external asset server. conformance.md Step 1 silently carves out an exception ("No request to any
non-localhost asset server other than the esm.sh CDN module imports"), so the criterion as literally
worded is not satisfiable, and AC-1 cannot pass with no network (the page won't render if the esm.sh
imports fail). Reword to the actual intent, e.g. "without a separate local asset/dev server or bundler
(framework deps still load from the esm.sh CDN; offline vendoring is deferred per Q4)".
AUTHOR: Reworded the "Then" clause to scope the claim to the SPA's own assets (served by the Go
binary, no separate local asset server / bundler) and to explicitly acknowledge that framework deps
load from esm.sh and first load needs network (offline vendoring deferred per Q4). This now matches
conformance.md Step 1's carve-out.
-->



**AC-2**: Dev mode hot edit-refresh
- Given: the server is run with the dev build tag (e.g. `go run -tags dev ./cmd/autoui serve`)
- When: a developer edits a file under `web/` and refreshes the browser
- Then: the change is visible with no Go rebuild and no bundler step

**AC-3**: Frontend ↔ Go API round trip
- Given: the SPA is loaded
- When: it issues a `fetch` to a Go `/api/*` endpoint (e.g. `/api/hello`)
- Then: the response is rendered in the UI, proving the client/server contract works

**AC-4**: Server always serves the shell
- Given: hash-based routing is in use (no server-side route matching)
- When: the browser requests `/` (any view lives in the `#` fragment)
- Then: the server serves `index.html`; the client router reads `location.hash` to pick the view

**AC-5**: State survives reload via URL
- Given: the user has navigated to a non-default view and set some view state
- When: the page is reloaded
- Then: the same view and state are restored from the URL hash

**AC-6**: Follows auto-stack package conventions
- Given: the new `auto-ui/` package
- When: a developer inspects it
- Then: it matches the full auto-* layout (cmd/internal split; cobra root with
  init/doctor/quickstart/docs/update + a `serve` command) and registers in the root Makefile +
  CLAUDE.md sub-projects table

## Out of Scope

- Any real dashboard feature / reading actual auto-stack data (sessions, docs, graph). This task
  only proves the serving + framework + state-in-URL base with a trivial demo.
- Production concerns: minification, optimization, auth, TLS, CDN pinning/offline vendoring.
- Hot Module Replacement (explicitly not needed — refresh is acceptable).
- Multi-user / remote deployment. Local-dev / internal only.

## Open Questions

- [x] Q1 — Frontend framework? (answered: **Preact + htm**, with `preact/compat` alias kept ready)
- [x] Q2 — Package structure? (answered: **full auto-* CLI skeleton** —
      init/doctor/quickstart/docs/update + a `serve` command)
- [x] Q3 — Routing model? (answered: **hash-based only**, `#/view?…`; server always serves the shell)
- [x] Q4 — CDN dependency policy? (answered: **load from CDN (esm.sh) at runtime** via import map;
      offline vendoring deferred to a later task)
