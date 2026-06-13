# Task 024: Planning Dashboard Backend — Enumeration, Raw Serving & Validation Harness

> Phase 1 of the **Planning Docs Dashboard** epic (`docs/epics/002-planning-docs-dashboard.md`).
> This is the backend precondition for the explorer UI (Phase 2) and live updates (Phase 3).

## Problem

`auto-ui` can list and read markdown docs for a project (`doc.list`/`doc.get`), but it cannot
enumerate the registered projects, cannot see or serve self-contained HTML planning docs, does not
emit `doc.changed` for `.html` edits, and offers no deterministic way for an agent harness to launch
an isolated server and trigger/observe bus events. Without these backend pieces the planning-docs
explorer UI (Phase 2) has no data source and no way to be validated end-to-end.

## Goals

- Enumerate the project registry to the client so a project switcher has a data source.
- Make the whole `docs/` tree discoverable — both `.md` and `.html` — without weakening the
  existing path-traversal guard or changing what `doc.get` returns.
- Serve verbatim HTML doc bytes over a dumb HTTP GET an iframe can point at.
- Extend `doc.changed` derivation coverage to `.html` so HTML docs become live (consumed in Phase 3).
- Give an agent harness deterministic, non-destructive control: isolated launch, one-call event
  emit, and a server-side view of derived events — all gated so production stays clean.

## Acceptance Criteria

**AC-1 (1.1): `project.list` RPC**
- Given: a registry at `~/.auto/projects.json` (or the configured registry path).
- When: a client calls the `project.list` JSON-RPC method (registered beside `doc.list`/`doc.get`).
- Then: it returns one `{id, name, path, remote}` object per registered project (cheap metadata
  only, no doc bodies); an empty registry returns `[]`, not an error.

**AC-2 (1.2): Widen doc discovery + parameterize `cleanDocPath`**
- Given: a project whose `docs/` tree contains both `.md` and `.html` files at any depth.
- When: `doc.list` is called.
- Then: it returns every `.md` **and** `.html` file, each entry carrying `{path, type}` with
  `type ∈ {markdown, html}`; grouping/categorization is left to the client.
- And: `cleanDocPath` takes the allowed-extension set as a **parameter** — discovery allows
  `{.md, .html}`, `doc.get` stays `{.md}`-only (never returns HTML through its markdown payload),
  the raw route (AC-3) allows `{.html}`. The "must be under `docs/`" + traversal guard holds in
  every case.

**AC-3 (1.3): Raw-bytes HTTP route for HTML docs**
- Given: a self-contained HTML planning doc under a project's `docs/` tree.
- When: a client requests `GET /api/doc/raw?project=…&path=…&worktree=…` (worktree optional).
- Then: the verbatim file bytes are served with `Content-Type: text/html`, reusing `resolveRoot`
  (which resolves the optional `worktree`) and the parameterized `cleanDocPath` with set `{.html}`.
- And: `.md` paths and path-traversal attempts are rejected; markdown continues to flow only
  through `doc.get`.

**AC-4 (1.4): Extend `doc.changed` derivation to `.html`**
- Given: an `agent.tool.post` event whose changed path is `docs/**/*.html`.
- When: `DeriveDocChanged` runs.
- Then: a `doc.changed` event is emitted (coverage parity with `.md`); a non-`docs/` `.html` path
  emits nothing; existing `.md` behavior is unchanged. A derive test covers an `.html` path.
- Note: this is a derivation **coverage** change in `auto-shared/bus/derive.go` only — it does not
  touch the event envelope or `doc.changed` wire shape (task 021 owner is merged, #75).

**AC-5 (1.5): Validation launch + isolation harness**
- Given: an agent harness needs a deterministic, non-destructive server instance.
- When: `auto ui serve` is launched.
- Then: `--port 0` binds an OS-assigned port and `--ready-file <path>` writes a JSON line
  `{"addr":"127.0.0.1:NNNN"}` with the actual bound address so the harness reads the real port
  (not a scraped banner); default port stays `8080`.
- And: `AUTO_UI_PORT` is honored by **both** `serve` and `auto hooks fire`'s `uiPort()`, so a
  harness can pin producer and test server to the same ephemeral port.
- And: `--projects <path>` (or `AUTO_PROJECTS_PATH`) points `resolveRoot` at an isolated fixture
  registry, leaving the user's real `~/.auto/projects.json` untouched.

**AC-6 (1.6): Synthetic event-emit helper**
- Given: a registered project and a `docs/**` path.
- When: an agent runs `auto ui emit --project <id> --path docs/.../plan.md [--worktree …]`.
- Then: a valid `agent.tool.post` envelope (RFC3339 `time`, correct shape) is POSTed to `/api/rpc`
  and derives exactly one `doc.changed`, with no hand-assembled JSON.
- And: the **Origin rule** is documented — `/api/rpc` ingest rejects any request carrying an
  `Origin` header, so triggers come from the CLI (no Origin), never a browser `fetch`; the
  companion real-edit recipe (`auto hooks fire` with a canned PostToolUse payload) is documented.

**AC-7 (1.7): Server-side debug event buffer (gated)**
- Given: the server is started with `AUTO_UI_DEBUG=1`.
- When: a client requests `GET /api/debug/recent`.
- Then: the last N broadcast events (raw + derived) are returned, letting an agent confirm the
  server half ("my POST derived one `doc.changed`") independently of any browser.
- And: with `AUTO_UI_DEBUG` unset, the route returns `404`.

## Out of Scope

- **The explorer UI** (project switcher, doc tree, content pane, default landing view) — Phase 2.
- **Client-side liveness** (open-doc refresh, the `doc.js` match fix, tree refresh) — Phase 3.
- **Client debug surfaces** (`window.__autoui`, the `/debug` SPA page) — Phase 2 (2.4/2.5).
- **Auth / multi-tenant / hardening** — single trusted host; only the existing free traversal guard
  is kept.
- **Multi-host aggregation** — projects are local via `projects.json`.
- **Changing the bus envelope / hook production / `doc.changed` wire shape** — the only allowed
  change is the `isDocPath` `.html` coverage widening (AC-4).
- **A new file watcher in auto-ui** — liveness rides the existing bus signal.

## Open Questions
- [x] `--ready-file` output format — bare address or JSON line? (answered: a JSON line
  `{"addr":"127.0.0.1:NNNN"}` — structured and extensible; can add `mode`/`pid` later.)
- [x] Ship 1.1–1.7 as one PR or stacked PRs? (answered: **one PR** — the harness lands with the
  features it validates; reviewed as a single cohesive backend chunk.)
