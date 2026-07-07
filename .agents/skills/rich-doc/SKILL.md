---
name: rich-doc
description: Creates or updates rich single-file HTML docs (tabs, mermaid, code, file trees, comment threads) for designs, plans, proposals or reports. Use when 'rich doc', 'html doc', 'planning doc', 'render as html', or given a DOC COMMENTS block to merge. Not for markdown task docs (use new-task/new-solution/new-plan).
---

# Rich Doc

Produce a single self-contained HTML file that renders a document as an
interactive, scan-first artifact: tabbed pages, mermaid diagrams, highlighted
code, API/type outlines, wireframes, and inline review threads. The goal is a
doc a human can absorb in minutes instead of reading pages of text — while the
raw HTML stays lean and semantic enough for an agent to re-read and edit cheaply.

This is a **general** rich-document renderer, not a planning-only tool. Use it
for any document that benefits from structure and rich rendering — a design
doc, a proposal, an RFC, an architecture tour, a research report, a runbook, or
a plan. It ships extra components tuned for task/plan and epic docs (file-change
trees, phase steppers, acceptance criteria); those are optional vocabulary, not
a requirement — reach for them when the doc is a plan, ignore them otherwise.

The doc uses the `pd-*` web component library, loaded from a CDN. You do not
write any CSS or JavaScript — you write semantic markup; the components do the
rendering.

## Step 1: Fetch the component reference

The component library is versioned with this skill. Fetch the reference for the
exact release this skill targets — an immutable tag, so it never drifts or
serves a stale cache:

```bash
curl -fsSL https://cdn.jsdelivr.net/gh/mistakenot/skills@pd-v0.9.0/pd-components/dist/llms.txt
```

It contains the page boilerplate, the release tag to pin, every component with
attributes and examples, the authoring rules, and the comment-merge protocol.
Follow it exactly — in particular, import the bundle pinned to the release tag
it names (`pd-v0.9.0`), never `@main`. To move to a newer component
release, update this skill (`npx skills install …`) — the new tag rides along.

If the fetch fails (offline/sandboxed): inside the skills repo itself, read
`pd-components/dist/llms.txt`; elsewhere, fall back to the component index
below (attributes/examples may be stale) and flag that to the user.

## Step 2: Author the doc

1. Gather the content: the conversation, files the user points at, or an
   existing document being rendered richly.
2. Choose the structure. Multi-topic docs get one `<pd-tab>` per concern
   (e.g. Overview / Design / Details, or Requirements / Solution / Plan); small
   docs skip tabs and put sections directly in `<pd-doc>`. For **plan** docs
   mind the tab roles: **Solution is the end state** (human-reviewed — file
   tree, API outlines, final snippets live here); **Plan is the recipe**
   (agent-consumed — keep it self-sufficient).
3. Prefer the rich components over prose — pick from the index below. A flow or
   architecture belongs in a `<pd-mermaid>`; code in `<pd-code>`; an interface
   in `<pd-api>`/`<pd-unit>`; a CLI's shape in `<pd-cli>`. For plans, phases
   belong in a `<pd-stepper>` (set each phase's `files`), file changes in
   `<pd-files>`, acceptance criteria in `<pd-ac>`. Use only the mermaid diagram
   types llms.txt lists as supported.
4. Save as `<topic>.html` where the user keeps the artifact (for task
   workflows: the task folder; otherwise ask or use `docs/`). Tell the user to
   open it in a browser; it works from file:// directly.
5. Lint it before handing off — see Step 4. Always run the linter after writing
   or editing a doc that has phases and a file tree.

Authoring rules that matter most (full set in llms.txt):

- The HTML source is the canonical document. Keep it semantic — structure
  lives in `pd-*` elements and stable kebab-case section ids, not in styled
  divs. Tailwind utility classes are welcome ONLY inside wireframe/freeform
  content.
- Content is **edited in place**; review threads are **append-only**. The
  artifact stays current and readable, while every decision survives in its
  thread (and surfaces automatically in `<pd-decisions>`).
- Record an architectural decision you made yourself — one not dictated by the
  requirements or an existing repo pattern — as a `<pd-decision>` (rationale +
  alternatives + consequences), not buried in prose. It feeds `<pd-decisions>`
  alongside resolved threads.
- When you hit a decision you genuinely cannot resolve — not from the source
  material, the code, or a sensible default — raise it as a `<pd-question>`
  (status `open`) rather than guessing. It flags the doc as blocked, surfaces
  in the headless linter as `open-question`, and the human answers it via the
  paste-back flow. Reserve it for answers only the human can give; for calls
  you can make yourself, use `<pd-decision>`.
- Preserve `<script type="application/json" id="pd-meta">` blocks when editing
  existing docs. Never modify, move, or delete the pd-meta block — it tracks
  task lifecycle state managed by the planning workflow.

## Component index

Grouped by where each component is *typically* used — the grouping is a guide,
not a restriction. **Any component works in any doc**: use `pd-mermaid`,
`pd-cli`, `pd-decision`, or a wireframe in a proposal or research report just as
freely as in a plan. llms.txt is canonical for exact attributes and examples.

**Document shell & structure** (every doc)
- `pd-doc` — the document shell: `title status pr generated next-prompt`
- `pd-tab` — one tabbed page (`name`); omit for small single-topic docs
- `pd-section` — a titled, anchorable section (`id title summary`)
- `md` — markdown inside any element (wrap `<>&` in a nested `<script type="text/plain">`)
- `pd-collapse` — ad-hoc disclosure wrapper (`summary`, `open`); body stays in the DOM

**Diagrams, code & interfaces** (any technical doc)
- `pd-mermaid` — a diagram (`caption`); flowchart/graph, sequence, state, class, ER, xychart only
- `pd-code` — a highlighted code snippet (`lang path lines highlight caption`)
- `pd-api` / `pd-member` — signatures-and-comments API/outline with generic `kind` badges
- `pd-unit` / `pd-dep` / `pd-fn` / `pd-prop` — richer type/unit outline with dependencies
- `pd-cli` / `pd-cmd` / `pd-out` — terminal transcript: the final shape of a CLI/API from the user's view
- `pd-wire` / `pd-note` — wireframe placeholder box + annotation, for mockups

**Review, decisions & questions** (any doc under review)
- `pd-thread` / `pd-comment` — append-only review thread (`anchor status priority title`)
- `pd-question` / `pd-answer` — a gate only the human can answer (`status open|answered`); lints as `open-question`
- `pd-decision` — an authored ADR: decision + rationale + alternatives + consequences
- `pd-decisions` — auto decision log: every pd-decision + every resolved/rejected thread

**Planning & task components** (plan / task docs)
- `pd-files` / `pd-file` — the file-change tree (`path change=add|edit|delete`)
- `pd-stepper` / `pd-phase` — clickable phase stepper (`n title files status depends-on`)
- `pd-dag` — phase dependency graph, derived from `pd-phase depends-on` (replaces hand-drawn sequence diagrams)
- `pd-scope` — derived at-a-glance summary strip (phase/file/AC/thread counts) — drop at the top of the first tab
- `pd-ac` — an acceptance-criteria card (`id title phases tests`) with traceability chips
- `pd-ac-check-*` — inert executable checks nested in a `pd-ac` (`command`/`output`/`test`/`file-exists`/`file-contains`)
- `pd-contract` — derived completion banner: `n/m ACs proved` across the ACs that carry checks
- `pd-trace` — derived AC × phase × test traceability matrix; flags coverage gaps

**Epic components** (epic-altitude docs)
- `pd-journey` / `pd-leg` — a user journey to an `outcome`, told from the user's view
- `pd-guardrail` — an invariant every task inherits (`kind metric`); tasks bind to it via `honors`
- `pd-task` / `pd-breakdown` — epic decomposition cards (`depends-on delivers honors deployable`) + their derived DAG
- `pd-outcome` — derived epic-altitude scan strip (journeys, guard rails, tasks) with coverage gaps

## Step 3: Iterate

When the user asks for changes, edit the HTML in place — do not regenerate
the whole file (that would orphan threads and churn the diff). Keep section
ids stable so existing threads and deep links keep pointing at the right
content. Re-run the linter (Step 4) after editing.

## Step 4: Lint before handing off

After writing or editing any doc with phases and a file tree, lint it and fix
what it reports before telling the user it's ready. The same consistency checks
the doc runs in the browser are available as a headless CLI — no browser, no
paste-back round-trip, and it bundles its own HTML parser (no `npm install`):

```bash
node "$CLAUDE_SKILL_DIR/scripts/pd-lint.mjs" path/to/doc.html
```

(Use the absolute path to this skill's `scripts/pd-lint.mjs`.) It prints JSON and
exits non-zero when a file has issues. The checks are derived from attributes the
doc already carries — no extra authoring:

- **unplanned-file** — a `<pd-file>` in the tree that no `<pd-phase files>` touches
- **untracked-file** — a phase touches a file missing from the `<pd-files>` tree
- **missing-dep** — a `depends-on` points at a phase `n` that doesn't exist
- **dependency-cycle** — phases form a `depends-on` cycle
- **open-question** — a `<pd-question>` still awaiting a human answer. This is a
  *gate*, not a defect: don't try to "fix" it — surface it to the user and wait.
  The non-zero exit lets an automated step refuse to proceed while questions are open.

(The file/phase checks only apply to docs that carry a file tree and phases —
a plain design doc or report simply has nothing for them to flag.)

Single file → bare result object; multiple files → `{ ok, fileCount, results }`.
Example output:

```json
{ "file": "doc.html", "ok": false, "issueCount": 1,
  "issues": [{ "code": "untracked-file",
    "message": "Phase 2 touches src/x.ts, which is missing from the file tree." }] }
```

## Merging reviewer comments

Reviewers comment in the browser and paste back an export block delimited by
`=== DOC COMMENTS` / `=== END DOC COMMENTS ===`. When you receive one:

- `REPLY to thread "X"` → append `<pd-comment by="review">` with the text to
  that thread. If you can address it, do so, append your `<pd-comment
  by="author">` explaining what changed, and set the thread's
  `status="resolved"` (or `"rejected"` with reasoning).
- `NEW <priority> comment on "X" (anchor: id)` → insert a new
  `<pd-thread anchor="<id>" priority="<priority>" title="<short summary you
  write>">` directly after the anchored element, containing the comment.
- `ANSWER to question "X" (anchor: id)` → append a `<pd-answer by="review">`
  with the text to that `<pd-question>` and set its `status="answered"`.
- Never edit or delete existing comments or answers — threads are the decision log.
- Report what you merged and resolved; the user clears their pending copy
  with the doc's Clear button.
