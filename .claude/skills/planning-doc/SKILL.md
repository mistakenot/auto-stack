---
name: planning-doc
description: Creates or updates rich single-file HTML planning docs (tabs, mermaid, file-change tree, comment threads). Use when 'planning doc', 'rich doc', 'html plan', 'render the plan as html', or when given a DOC COMMENTS block to merge. Not for markdown task docs (use new-task/new-solution/new-plan).
---

# Planning Doc

Produce a single self-contained HTML file that renders a plan or design as an
interactive document: tabbed pages, mermaid diagrams, a file-change tree, a
clickable phase stepper, and inline review threads. The goal is a doc a human
can absorb in minutes instead of reading pages of text — while the raw HTML
stays lean and semantic enough for an agent to re-read and edit cheaply.

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
`pd-components/dist/llms.txt`; elsewhere, fall back to the cheat sheet at the
bottom of this skill and flag to the user that the reference may be stale.

## Step 2: Author the doc

1. Gather the content: the conversation, files the user points at, or existing
   planning docs (requirements/solution/plan markdown) being rendered richly.
2. Choose the structure. Multi-topic docs get one `<pd-tab>` per concern
   (e.g. Overview / Solution / Plan / Wireframe); small docs skip tabs and put
   sections directly in `<pd-doc>`. Mind the tab roles: **Solution is the end
   state** (human-reviewed — file tree, API outlines, final snippets live
   here); **Plan is the recipe** (agent-consumed — keep it self-sufficient).
3. Prefer the rich components over prose. A plan with phases belongs in a
   `<pd-stepper>` (set each phase's `files` so the file tree highlights);
   file changes belong in `<pd-files>`; flows and architectures belong in
   `<pd-mermaid>`; acceptance criteria in `<pd-ac>` with traceability chips.
   Use only the mermaid diagram types llms.txt lists as supported.
4. Save as `<topic>.html` where the user keeps planning artifacts (for task
   workflows: the task folder's `artifacts/`; otherwise ask or use `docs/`).
   Tell the user to open it in a browser; it works from file:// directly.
5. Lint it before handing off — see Step 4. Always run the linter after writing
   or editing a doc that has phases and a file tree.

Authoring rules that matter most (full set in llms.txt):

- The HTML source is the canonical document. Keep it semantic — structure
  lives in `pd-*` elements and stable kebab-case section ids, not in styled
  divs. Tailwind utility classes are welcome ONLY inside wireframe/freeform
  content.
- Content is **edited in place**; review threads are **append-only**. This
  mirrors the markdown workflow: the artifact stays current and readable,
  while every decision survives in its thread (and surfaces automatically in
  `<pd-decisions>`).
- Record an architectural decision you made yourself — one not dictated by the
  requirements or an existing repo pattern — as a `<pd-decision>` (rationale +
  alternatives + consequences), not buried in prose. It feeds `<pd-decisions>`
  alongside resolved threads.
- When you hit a decision you genuinely cannot resolve — not from the
  requirements, the code, or a sensible default — raise it as a `<pd-question>`
  (status `open`) rather than guessing or burying it in prose. It flags the doc
  as blocked, surfaces in the headless linter as `open-question`, and the human
  answers it via the paste-back flow. Reserve it for answers only the human can
  give; for calls you can make yourself, use `<pd-decision>`.
- Preserve `<script type="application/json" id="pd-meta">` blocks when editing
  existing docs. Never modify, move, or delete the pd-meta block — it tracks
  task lifecycle state managed by the planning workflow.

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
node "$CLAUDE_SKILL_DIR/scripts/pd-lint.mjs" path/to/plan.html
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

Single file → bare result object; multiple files → `{ ok, fileCount, results }`.
Example output:

```json
{ "file": "plan.html", "ok": false, "issueCount": 1,
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

## Emergency cheat sheet (prefer llms.txt — this may be stale)

Classic scripts in head (never `type="module"`):
`https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4` and
`https://cdn.jsdelivr.net/gh/mistakenot/skills@pd-v0.9.0/pd-components/dist/pd.min.js` (defer).

- `<pd-doc title status pr generated next-prompt>` shell · `<pd-tab name>` page
  pr: `"pending"` → placeholder; a URL → clickable badge. Update when PR opens.
  next-prompt: the command the human runs to advance to the next workflow stage
  (e.g. `"/new-solution 042"`) — renders a "Next step" banner with a copy button.
- `<pd-section id title>` titled/anchorable section, freeform body
- `<pd-thread anchor status{unresolved|resolved|rejected} priority{p1|p2|p3} title>`
  with `<pd-comment by{review|author|name}>` children — append-only
- `<pd-question id status{open|answered} priority{p1|p2|p3} for title>` a question
  the human must answer (gates the doc; lints as `open-question`) — answered by
  appending a `<pd-answer by{review|name}>` child and flipping status
- `<pd-files>` + `<pd-file path change{add|edit|delete}>note</pd-file>`
- `<pd-stepper>` + `<pd-phase n title files status{done|active|todo}>`
- `<pd-mermaid caption>` source as text content; flowchart/graph, sequence,
  state, class, ER, xychart only
- `<pd-code lang path lines highlight caption>` highlighted snippet (code as
  text content; use a nested `<script type="text/plain">` if it contains `<>&`)
- `<pd-api kind name lang path>` + `<pd-member kind sig>note</pd-member>` —
  API/outline: signatures + comments only, generic `kind` badges
- `<pd-ac id title phases tests>` acceptance-criteria card
- `<pd-decision id title status{accepted|proposed|superseded} by summary>` authored ADR block
- `<pd-decisions>` auto decision log · `<pd-wire label h>` / `<pd-note>` wireframes
