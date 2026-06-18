# Task 027: Plan Lifecycle Status & Executor Liveness in the Planning-Docs Tree

> Epic 002 (`docs/epics/002-planning-docs-dashboard.md`) — Phase 4 polish.
> Builds on completed tasks **024** (backend: `doc.list`/`project.list`/raw HTML serving),
> **025** (explorer SPA), and **026** (document liveness: open-doc refresh on edit).
> This task adds *plan-state* and *executor-activity* signal to the nav tree — a different
> axis from 026's *document* liveness.

## Problem

HTML planning docs now carry a machine-managed `<script type="application/json" id="pd-meta">`
block (`{id, name, status, branch, epic, created, pr}`, lifecycle `planning → executing → merged`)
plus a `<pd-doc status="draft|in-review|approved">` review-state attribute. The auto-ui doc tree
(`tree.js`, fed by `doc.list`/`walkDocs`) ignores all of it — `doc.list` returns only
`{id, path, type}`. So the explorer cannot show which plans are in-flight vs. done, and gives no
sign of whether an agent is actively working a plan right now.

## Goals

- Parse `pd-meta` + `<pd-doc status>` **server-side** in `walkDocs` and attach an optional `meta`
  object to each `doc.list` entry — without changing `doc.get` or the raw route, and without
  breaking markdown / plain-HTML docs.
- Show **lifecycle status** in the tree: a spinner when `meta.status == "executing"`, plus a
  small review-state pill from `<pd-doc status>`.
- Show **executor liveness**: a freshness indicator ("active Ns ago" / heartbeat) next to a plan
  whose `meta.branch` matches the branch of recently-received live hook events — reusing the
  existing bus stream, no new backend signal.
- **Backfill** the two pre-`pd-meta` HTML plans (021, 022) so they render with correct state.

## Acceptance Criteria

**AC-1: Server-side `pd-meta` extraction**
- Given: a `docs/` tree containing `.html` planning docs (some with a `pd-meta` block, some
  without), `.md` docs, and non-plan `.html`.
- When: `doc.list` is called.
- Then: each `.html` entry that has a valid `pd-meta` block carries an optional
  `meta: {status, branch, epic, pr, created, reviewState}` object; entries without a parseable
  block omit `meta` entirely. `.md` entries are unchanged. Parsing uses `golang.org/x/net/html`
  (new approved dependency) and reads a bounded prefix of the file (the block lives in `<head>`).

**AC-2: `<pd-doc status>` review state**
- Given: an `.html` plan whose `<body>` contains `<pd-doc status="draft|in-review|approved" ...>`.
- When: `doc.list` is called.
- Then: `meta.reviewState` reflects that attribute. This is distinct from `meta.status`
  (lifecycle) — a doc may be `reviewState: approved` while `status: planning`.

**AC-3: Malformed / missing tolerance**
- Given: an `.html` doc with no `pd-meta`, malformed JSON in the block, or no `<pd-doc>`.
- When: `doc.list` is called.
- Then: the call succeeds, the entry is still returned, and only the missing fields are omitted
  (no entry is dropped, no error surfaced to the client). Server logs a debug-level note at most.

**AC-4: Lifecycle status in the tree**
- Given: `doc.list` entries carrying `meta`.
- When: the tree renders.
- Then: a plan node with `meta.status == "executing"` shows a spinner affordance; a node with a
  `meta.reviewState` shows a small pill; nodes without `meta` render exactly as today. Status
  affordances update live when a `doc.changed` for that path arrives (re-fetch `doc.list`),
  so a `planning → executing` edit repaints without manual reload.

**AC-5: Executor liveness from the live hook stream**
- Given: the explorer is open and an executor is firing hooks while working a plan's branch
  (raw bus events reach WS clients via `rpc_ingest` `hub.Broadcast(ev)`, carrying top-level
  `branch`/`worktree`/`project`).
- When: the client receives such events.
- Then: the client maintains a `(project, branch) → lastEventTs` map from the live stream and
  renders a freshness indicator next to any plan node whose `meta.branch` matches (same project),
  e.g. "active 12s ago". Any agent hook on the matched branch counts as activity (interactive or
  autonomous — no attempt to distinguish). After a freshness threshold the row decays to an
  "idle Nm ago" / dimmed state rather than silently dropping the timer. No persistence: on reload
  the map starts empty and re-populates from subsequent events.

**AC-5b: Shared-branch exclusion**
- Given: a plan whose `meta.branch` is `main`, `master`, or null/empty.
- When: liveness is computed.
- Then: no liveness join is performed for that plan (these branches are touched by many
  unrelated sessions and would falsely light up). Only distinct feature branches drive the
  indicator. The lifecycle spinner (AC-4) is unaffected — it derives from `meta.status` only.

**AC-6: Backfill 021 / 022**
- Given: `docs/tasks/021-auto-bus-standard/artifacts/auto-bus-standard.html` and
  `docs/tasks/022-hook-event-log/artifacts/hook-event-log.html` lack a `pd-meta` block.
- When: this task lands.
- Then: both carry a correct `pd-meta` block (status `merged`, accurate `created`/`pr`, `epic`
  null) so they render with a merged/done state rather than blank.

**AC-7: Tests**
- Given: the new parsing + rendering paths.
- When: tests run.
- Then: Go unit tests cover `pd-meta`/`pd-doc` extraction (present / absent / malformed /
  bounded-prefix) on `walkDocs`/`doc.list`; existing `doc.list` behavior for `.md` is unchanged;
  client behavior is validated via the existing agent-browser harness (024/025) for the spinner,
  pill, and liveness indicator. `go build ./...` clean in `auto-ui`.

## Out of Scope

- Changing the planning-doc skill — it already emits `pd-meta`.
- Server-side persistence or historical backfill of hook activity (liveness is live-only,
  best-effort; if the UI is down, nothing is recorded).
- A new file watcher in auto-ui — status updates ride the existing `doc.changed` signal.
- Other Phase 4 polish items: cross-doc search, breadcrumbs, recently-viewed, mermaid.
- Changing `doc.get` or `/api/doc/raw` payloads.

## Open Questions

- [x] Q1 — Stale-executing display (answered: show an explicit "idle Nm ago" / dimmed state when
  `status == executing` but no recent matching hooks, rather than silently dropping the timer —
  see AC-5).
- [x] Q2 — Liveness breadth (answered: any agent hook on the matched branch counts as activity,
  interactive or autonomous; no attempt to distinguish — see AC-5).
- [x] Q3 — Shared-branch noise (answered: exclude `main`/`master`/null from the liveness join;
  only distinct feature branches drive the indicator — see AC-5b).
