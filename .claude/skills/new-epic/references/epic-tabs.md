# Epic Tabs Guidelines

The tabs of `epic.html`, in order. One skill (`new-epic`) authors them all. Each tab stays at epic altitude (see [epic-overview.md](epic-overview.md)): direction and constraints, never files, code, or intra-task phases.

For each element below, the **component to prefer** is named. Use the exact tag and attributes from the `llms.txt` that `planning-doc` fetched; if that version doesn't carry it, improvise within the altitude (a diagram, a grouped list, a card) and note nothing is lost — the experience still reads. Never substitute a task-level component (file tree, phase stepper, code outline, code snippet).

**The epic components share one id-keyed graph.** Give each journey/transcript an `id`; give each guard rail an `id`; then a task names what it `delivers` (journey/cli ids) and `honors` (guard-rail ids). That wiring is what makes the derived views work — `pd-outcome` flags any guard rail no task honors or journey no task delivers, and selecting a guard rail lights up every task bound by it. Author the ids deliberately.

## 1. Vision — the headline

The user-facing final shape. Lead the whole doc with it. *What does success look like from the user's seat?*

- Open the tab with **`pd-outcome`** — the derived scan strip (journeys, guard rails, tasks, coverage gaps). Zero authoring; it reads the doc.
- **For a CLI / dev tool / API**: the commands a user runs and the output they get back, with light comments — use **`pd-cli`** (with `pd-cmd` / `pd-out`). Give it an `id`.
- **For an app / service / multi-actor flow**: a light-touch user journey from intent to outcome — use **`pd-journey`** (with `pd-leg`). Give it an `id`.
- Add a short **"why now"** in prose. One paragraph, not a background essay.
- Set `status` (done/active/todo) on journey legs and CLI commands, so the reader sees how much of the experience is real today versus still coming — progress measured in user-visible capability.

Do **not** open with architecture or a component diagram. The experience comes first.

## 2. Guard rails — the inherited constraints

The requirements every task must honor, split into **functional** (must-do behaviors, compatibility, data integrity, correctness invariants) and **non-functional** (performance, security, cost, accessibility, operability, observability). State measurable targets where they exist (e.g. a latency budget, a compatibility promise).

Use **`pd-guardrail`** — one per rail, with a stable `id`, a `kind` (functional, or a non-functional kind like performance/security/cost/reliability/operability), an optional `metric` for the measurable target, and a short `title`. `pd-outcome` groups them functional vs non-functional automatically. Keep each rail to a sentence; it's a constraint, not a design.

## 3. Architecture & seams — at the boundary level

The shape of the system and the seams that let tasks proceed independently — described at the **container / boundary** level, never individual files or code.

- **Structure**: a diagram of the major containers/components and how they relate — reach for the **diagram component** (`pd-mermaid`), flowchart for components/containers. Use an ER diagram when the epic is data-shaped.
- **Seams**: name the load-bearing contracts explicitly — the interfaces a task must not break. These are the edges in the diagram; call out the important ones in prose or a short list.

If you find yourself naming files or functions, you've dropped altitude — pull back up to containers and contracts.

## 4. Breakdown — deployable, sequenced tasks

The decomposition into tasks. This is the bridge to `new-task`. For each task:

- It is **independently deployable** — it won't break shipped functionality once complete and pushed (it may sit behind a feature gate).
- It **builds on its predecessors** — show the sequence/dependencies.
- It advances a named **Vision outcome** or **guard rail** — say which.
- Give a short statement of intent and, once planned, a link to its task doc and current status. Do **not** list files or implementation steps — that is the task planner's remit.

**Slice vertically, not horizontally.** Decompose by thin end-to-end capability, not by layer. Resist the default of "build the whole backend, then the whole frontend, then wire them up" — that hides all the integration risk until the end. Instead:

- **Task 1 is a walking skeleton**: the thinnest possible path that touches *every* layer and seam in the architecture and runs end to end, even if it delivers only a sliver of one journey (one hard-coded case, one happy path). Its job is to get all the layers talking and stand up a basic end-to-end verification signal early.
- **Each later task thickens the skeleton** — adds a journey, a branch, an edge case — on top of a system that already runs end to end. Every task should move a real Vision journey from `todo` toward `done`, never just "finish layer X."
- A task that delivers no user-visible end-to-end behavior on its own (a lone data layer, a lone UI with stubs) is a horizontal slice — fold it into the vertical task it serves.

Use **`pd-task`** — one per task, with `id`, `title`, `status`, `depends-on` (other task ids), `delivers` (journey/cli ids), `honors` (guard-rail ids), the `deployable` flag (and `gated` if behind a feature flag), and `href` to its task doc once planned. Body is the one-line intent. Then drop a **`pd-breakdown`** at the top of the section — it derives the dependency DAG (the deployable staircase) from the cards. Keep the ordering legible; that staircase is the point.

## 5. Decisions — the strategic bets

The calls that shape the whole initiative — recorded as **authored decision records** (`pd-decision`), aggregated into the **decision log** (`pd-decisions`). For anything still open, use a **review thread** (`pd-thread`) so the discussion has a home and an audit trail.

## Rules

- **Altitude**: no files, no code, no intra-task phases anywhere in the doc.
- Every breakdown task must be independently deployable and ordered.
- **Vertical slices, walking skeleton first.** Task 1 stands up an end-to-end path through all layers; later tasks add capability on top. Never decompose layer-by-layer with integration deferred to the end.
- Prefer the specific component per element; improvise within the altitude where there's a gap; never force a task-level component.
- Keep each tab scannable — lead with the visual, collapse detail behind summaries.
