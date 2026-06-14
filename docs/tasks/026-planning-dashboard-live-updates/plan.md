# Plan: Task 026 — Planning Dashboard Live Updates

## Summary
Wire the existing `doc.changed` WebSocket signal into the explorer: one shared `docevents.js` helper
that reads the path from `ev.data.path` (the bug fix), an open-doc refresh subscription in
`content.js`, a re-list-on-unseen-path subscription in `tree.js`, and a backend test pinning
`params.data.path` — no new server route, no bus change, no file watcher, no new dependency.

## Dependency precondition (blocking)
**026 cannot start until task 025 merges to main.** The files 026 modifies (`content.js`, `tree.js`)
and the surfaces it relies on (`window.__autoui`, `data-revision`, iframe `v=` nonce,
`data-doc-count`, `#/debug`) are 025's and **do not exist on main as of 2026-06-13** (verified: only
`app.js`/`doc.js`/`router.js`/`rpc.js` present). Branch 026 off main **after** 025 is merged, then
`git fetch origin && git checkout main && git pull origin main` before creating the worktree
(CLAUDE.md worktree discipline).

## Changes
| Symbol | File | Description |
|--------|------|-------------|
| + | `auto-ui/web/static/docevents.js` | `parseDocChanged(ev)` + `matchesDoc(ev, target)` — single source of truth for reading the `doc.changed` envelope (`ev.data.path`) |
| ~ | `auto-ui/web/static/content.js` | Add `doc.changed` subscription: `matchesDoc` → existing refresh action (md re-fetch / html `v=` bump); auto-apply immediately |
| ~ | `auto-ui/web/static/tree.js` | Add `doc.changed` subscription: unseen path for active project → `doc.list` re-list + regroup; ensure expansion keyed by stable path |
| ~ | `auto-ui/internal/server/rpc_ingest_test.go` | Extend `TestRPCIngestBroadcastAndDerive` to assert `params.data.path` on the derived `doc.changed` |
| ~ | `auto-ui/CLAUDE.md` | Document liveness wiring, the `params.data.path` wire-shape gotcha, and `docevents.js` |
| ~ | `docs/epics/002-planning-docs-dashboard.md` | Mark sub-tasks 3.1 / 3.2 status = done |
| + | `docs/tasks/026-planning-dashboard-live-updates/artifacts/conformance.md` | agent-browser liveness validation (AC-1..AC-5), embed + dev builds |

## Links
- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test
- [ ] `auto-ui/internal/server/rpc_ingest_test.go` — `go test ./...` asserts `params.data.path` (AC-1 backend half)
- [ ] `docs/tasks/026-.../artifacts/conformance.md` — agent-browser e2e for AC-1 (client match), AC-2 (open-doc md/html refresh + non-match no-op), AC-3 (tree grows + expand preserved), AC-5 (full loop, embed + dev builds)
- [ ] `feedback.md` — records the AC-4 deletion/create-signal verdict (decision record, no automated test)

## Execution Sequence
```
Phase 1 (docevents.js helper) ─┬─> Phase 2 (content.js open-doc refresh) ─┐
                               └─> Phase 3 (tree.js live re-list) ─────────┼─> Phase 5 (conformance + epic status + verdict)
Phase 4 (rpc_ingest_test.go assertion, independent) ───────────────────────┘
```
Phase 1 is the foundation both Phase 2 and Phase 3 import. Phase 4 (backend test) is fully
independent and can run in parallel from the start. Phase 5 ties everything together and must run
last (it validates the whole stack end-to-end).

## Plan

### Phase 1: Shared `doc.changed` helper (the wire-shape fix)
- [x] Step 1.1: Create `auto-ui/web/static/docevents.js` exporting `parseDocChanged(ev)` →
  `{project, path, worktree, branch}` reading **`ev.data.path`** (data-first, envelope fallback for
  project/worktree/branch) and `matchesDoc(ev, target)` (match `{project, path}`; worktree matches
  any when missing on either side). No new import-map specifier (pure JS, no deps).
  - *Verify:* file parses as an ES module (`node --check auto-ui/web/static/docevents.js`);
    `parseDocChanged({type:"doc.changed", project:"p", worktree:"w", data:{path:"docs/x.md", worktree:"w"}}).path === "docs/x.md"`; `parseDocChanged({path:"docs/x.md"}).path === undefined` (top-level `path` is NOT read — proves the bug can't recur).
- [x] Step 1.2: Commit: `feat(026): phase 1 - shared doc.changed envelope helper`

### Phase 2: Open-doc live refresh in `content.js` (AC-2)
> Depends on Phase 1.
- [ ] Step 2.0: **Content-pane seam check (mirrors Step 3.2).** Confirm 025's `content.js` exposes a
  single reusable refresh action covering **both** markdown re-fetch (`doc.get` + re-render) **and**
  the HTML iframe `v=<nonce>` bump — i.e. the callback the `data-testid` refresh button's onClick
  already invokes. If 025 inlines those paths inside the button's JSX handler (or splits md re-fetch
  and HTML reload into separate inline paths), **extract the seam first** so the new `useEffect` can
  call it; only then add the subscription. This turns "add one useEffect" into "extract a `refresh()`
  seam, then subscribe" if needed.
  - *Verify:* `content.js` has one callable refresh action invokable outside the button JSX; both the
    markdown re-fetch and the HTML nonce-bump route through it.
- [ ] Step 2.1: In `content.js`, add a `useEffect` (keyed on `[project, path, worktree]`) that
  `on("doc.changed", ev => { if (!matchesDoc(ev, openRef.current)) return; refresh(); })` and returns
  the unsubscribe. Use the existing `openRef`-style current-value ref (mirror retired `doc.js:55-68`)
  so the handler sees current props. `refresh()` is the **existing** action 025's refresh button
  calls — markdown re-runs `doc.get` + re-render (bumps `data-revision`/`data-last-updated`); HTML
  bumps the iframe `v=<nonce>`. Import `matchesDoc` from `./docevents.js`.
  - *Verify:* `node --check auto-ui/web/static/content.js`; import resolves; for a markdown doc a
    matching `doc.changed` increments `data-revision`; for an HTML doc it changes the iframe `src`
    `v=` nonce; a non-matching `doc.changed` (different path/project) leaves `data-revision` unchanged.
    (Asserted concretely in Phase 5 conformance; here verify wiring compiles and the match guard is
    present.)

<!-- RESOLVED(P2): Phase 2 assumes 025's content.js exposes a reusable refresh() + iframe nonce — no verification step (asymmetric with Step 3.2)
REVIEW: Phase 3 correctly hedges its dependency on 025 with an explicit coupling-check (Step 3.2:
"confirm 025's tree.js keys expansion by stable path, refactor if not"). Phase 2 has the SAME class
of dependency but no equivalent check: it assumes content.js exposes a callable `refresh()` action
that both re-runs `doc.get` (markdown) and bumps the iframe `v=<nonce>` (HTML). 025 is unmerged and
in progress (context.md:119) — context.md:69-73 describes a "`data-testid` refresh button" but does
not establish that the refresh logic is factored as a reusable function the new `useEffect` can
invoke. If 025 inlines the refresh in an onClick handler (or HTML reload and markdown re-fetch are
separate inline paths), Phase 2 is "extract a refresh() seam, then subscribe" — more than "add one
useEffect". Add a Step 2.0 mirroring 3.2: verify content.js exposes a single reusable refresh action
covering both md re-fetch and HTML nonce bump; extract it if not. Same applies to `reloadDocList()`
and `knownPaths` in Step 3.1 — both are asserted as "existing" but are 025 surfaces not yet frozen.
AUTHOR: Agreed — valid asymmetry. Added **Step 2.0** (content-pane seam check, explicitly "mirrors
Step 3.2"): verify content.js exposes one reusable refresh action covering both md re-fetch and the
HTML nonce-bump, and extract the seam first if 025 inlined it in the button's JSX handler. Also
broadened **Step 3.1** to flag `reloadDocList()`/`knownPaths` as 025 surfaces to confirm-or-extract
(same pattern). All three 025-dependent seams (refresh, reloadDocList/knownPaths, path-keyed
expansion) now carry an explicit confirm-or-extract step rather than assuming the surface is frozen.
-->

- [ ] Step 2.2: `gofmt`/build sanity for the module (no Go change) — run `go build ./...` in
  `auto-ui` to confirm embed still builds with the new `.js` (`//go:embed all:static` picks it up).
  - *Verify:* `cd auto-ui && go build ./...` succeeds; `go build -tags dev ./...` succeeds.
- [ ] Step 2.3: Commit: `feat(026): phase 2 - open-doc live refresh in content pane`

### Phase 3: Live nav-tree refresh in `tree.js` (AC-3)
> Depends on Phase 1. Independent of Phase 2.
- [ ] Step 3.1: In `tree.js`, add a `useEffect` (keyed on `[activeProject, worktree]`) that
  `on("doc.changed", ev => {...})`: read `parseDocChanged(ev)`; ignore if `project !== activeProject`;
  ignore if the path is already in the current known-path set; otherwise re-run the list fetch
  (`doc.list` + regroup). Maintain a `knownPaths` ref/set derived from the current `doc.list` result.
  Import `parseDocChanged` from `./docevents.js`. **Seam check (same pattern as Step 2.0):**
  `reloadDocList()` and `knownPaths` are 025 surfaces, not yet frozen — confirm `tree.js` exposes a
  reusable list-fetch action and a place to derive the known-path set; if 025 inlines the list fetch
  in mount-only code, extract that seam first so the subscription can call it.
  - *Verify:* `node --check auto-ui/web/static/tree.js`; a `doc.changed` for an unseen path in the
    active project triggers exactly one `doc.list` re-fetch; a `doc.changed` for a known path or a
    different project triggers none.
- [ ] Step 3.2: **Expansion-state coupling check.** Confirm 025's `tree.js` keys expand/collapse by
  stable group/node **path/prefix** (not array index). If it does, reconcile preserves state for
  free. If it keys by index, refactor expansion to a path-keyed map so a re-list does not collapse
  open groups (AC-3 requirement).
  - *Verify:* after a re-list triggered by a new doc, a previously-expanded group remains expanded
    (asserted in Phase 5 conformance via `data-testid` on the group's expanded state).
- [ ] Step 3.3: Build sanity: `cd auto-ui && go build ./... && go build -tags dev ./...`.
  - *Verify:* both builds succeed.
- [ ] Step 3.4: Commit: `feat(026): phase 3 - live nav-tree refresh on unseen path`

### Phase 4: Backend `params.data.path` assertion (AC-1 backend)
> Independent — can run from the start.
- [ ] Step 4.1: Extend `TestRPCIngestBroadcastAndDerive` in
  `auto-ui/internal/server/rpc_ingest_test.go` (the `doc.changed` block, ~lines 111-120) to assert
  the derived notification carries `params.data.path` equal to the emitted `docs/**` path (read
  `docParams["data"].(map[string]any)["path"]`), in addition to the existing `params.type` assertion.
  - *Verify:* `cd auto-ui && go test ./internal/server/ -run TestRPCIngestBroadcastAndDerive -v`
    passes and fails if the `data.path` assertion is removed/wrong (sanity-check by temporarily
    breaking the expected value).
- [ ] Step 4.2: Run the full server test suite + lint.
  - *Verify:* `cd auto-ui && go test ./... && gofmt -l internal/server/ && go vet ./...` clean.
- [ ] Step 4.3: Commit: `test(026): phase 4 - pin doc.changed params.data.path wire shape`

### Phase 5: Conformance, docs, epic status, verdict (AC-1 e2e, AC-2, AC-3, AC-4, AC-5)
> Depends on Phases 1–4.
- [ ] Step 5.1: Write `docs/tasks/026-.../artifacts/conformance.md` (model on
  `docs/tasks/025-.../artifacts/conformance.md`). Steps: launch isolated
  (`auto ui serve --port 0 --ready-file <tmp> --projects <fixture>`, `AUTO_UI_DEBUG=1`); build a
  fixture project with a `docs/` tree (≥1 markdown + ≥1 self-contained HTML planning doc, plus
  multiple task dirs so a group can be expanded); `agent-browser open
  http://<addr>/?debug=1#/explore?project=…`. Assertions:
  - **AC-1/AC-2 md:** open a markdown doc; `auto ui emit --project … --path <that md>`; assert
    `window.__autoui` shows the `doc.changed` and `data-revision` incremented.
  - **AC-2 html:** open the HTML doc; `emit` its path; assert the iframe `src` `v=` nonce changed
    (`get attr src`).
  - **AC-2 non-match:** `emit` an unrelated path; assert `data-revision` did **not** change.
  - **AC-3:** expand a task group; **write** a brand-new `docs/tasks/NNN-new/plan.md` file into the
    fixture's `docs/` tree on disk, **then** `auto ui emit --project … --path docs/tasks/NNN-new/plan.md`
    for that path (emit only POSTs a `doc.changed` envelope — it does **not** create the file; `tree.js`
    reconciles by re-running `doc.list`, which enumerates the fixture directory on disk, so the file
    must exist first or the re-list finds nothing new); assert tree `data-doc-count` grew, the new
    `data-doc-path` leaf exists, and the expanded group stayed open.

<!-- RESOLVED(P2): AC-3/AC-5 brand-new-doc step must create the file on disk before emitting
REVIEW: `auto ui emit` only builds a `bus.ToolPost` envelope and POSTs it — it does NOT touch the
filesystem (verified `auto-ui/internal/cli/emit.go:38-46`: absPath is computed but no file is
written). Liveness is invalidation-only: `doc.changed` carries no content, so `tree.js` reconciles by
re-running `doc.list`, which enumerates the fixture's `docs/` directory on disk (task 024 backend).
Therefore, for `data-doc-count` to grow and the new `data-doc-path` leaf to appear, the conformance
harness must WRITE `docs/tasks/NNN-new/plan.md` into the fixture BEFORE (or as part of) the emit —
emitting alone will re-list and find nothing new, and the assertion fails. This same omission is
present in requirements AC-3 ("a `doc.changed` for that project whose path the current tree does not
contain") and AC-5 ((c) "a brand-new doc path") — both read as if the event creates the doc. Add an
explicit "create the file in the fixture, then emit" sub-step here (and note it in the AC wording).
AUTHOR: Correct and confirmed against `emit.go` (envelope-only, no fs write; `doc.list` enumerates
disk). Rewrote the Step 5.1 AC-3 sub-step to **write the file into the fixture first, then emit**,
with the rationale inline. Also clarified the AC wording in requirements.md: AC-3 now says "an agent
**writes** a new doc … (the file exists on disk; `doc.changed` is the invalidation signal, not the
creator)", and AC-5(c) now says "writes a brand-new doc file, then triggers `doc.changed`". Same
write-then-emit ordering applies to the markdown/HTML edit cases, which already point at existing
fixture files.
-->

  - **AC-5:** run the whole script against **embed** (`go build`) and **dev** (`-tags dev`) builds.
  - *Verify:* run the conformance script end-to-end against a live `auto ui serve`; capture
    `eval`/`get attr`/screenshot evidence into the artifacts folder; every assertion passes on both
    builds.
- [ ] Step 5.2: Record the **AC-4 verdict** in `feedback.md`: `doc.changed` covers the create case
  (new doc's first write emits `doc.changed` for an unseen path → tree re-list); deletions reconcile
  on the next re-list/navigation; an explicit `doc.created`/`doc.removed` derivation is **not**
  warranted for v1.
  - *Verify:* `feedback.md` contains the verdict with the create-case reasoning.
- [ ] Step 5.3: Update `auto-ui/CLAUDE.md` (liveness wiring + `params.data.path` gotcha +
  `docevents.js`) and mark sub-tasks **3.1 / 3.2 = done** in
  `docs/epics/002-planning-docs-dashboard.md` (Status section + sub-task index table).
  - *Verify:* epic sub-task index rows 3.1/3.2 show `done`; CLAUDE.md documents the helper and gotcha.
- [ ] Step 5.4: Commit: `feat(026): phase 5 - liveness conformance, docs, epic status`

## Success Criteria
- [ ] `cd auto-ui && go build ./... && go build -tags dev ./...` both succeed (new `.js` embeds cleanly).
- [ ] `cd auto-ui && go test ./...` passes, including the extended `rpc_ingest_test.go` asserting
  `params.data.path` (AC-1 backend).
- [ ] `node --check` passes for `docevents.js`, `content.js`, `tree.js`; no new import-map specifier added.
- [ ] Conformance (agent-browser, **both** embed + dev builds) passes every assertion:
  - AC-1: the client reads `ev.data.path` and the match fires (via `window.__autoui` + `data-revision`).
  - AC-2: open **markdown** re-renders (`data-revision++`), open **HTML** reloads (iframe `v=` nonce
    changes), and a non-matching `doc.changed` causes **no** bump.
  - AC-3: an unseen-path `doc.changed` grows tree `data-doc-count` + adds the leaf with no reload, and
    a previously-expanded group stays expanded across the reconcile.
- [ ] AC-4 verdict recorded in `feedback.md` (reuse `doc.changed`; no bus edit).
- [ ] Epic sub-tasks 3.1 / 3.2 marked done; `auto-ui/CLAUDE.md` updated.
- [ ] No new server route, no bus/derive change, no file watcher, no new runtime dependency.

## Open Questions
- (none — requirements' two open questions are resolved: deletions reuse `doc.changed`; open-doc
  auto-applies immediately. One execution-time coupling check remains, captured as Step 3.2: verify
  025's tree keys expansion by path, not index.)
