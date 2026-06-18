# Epic Tabs Guidelines

The tabs of `epic.html`, in order. One skill (`beta-new-epic`) authors them all. Each tab stays at epic altitude (see [epic-overview.md](epic-overview.md)): direction and constraints, never files, code, or intra-task phases.

For each element below, the **component to prefer** is named by purpose. Use the exact tag from the `llms.txt` that `planning-doc` fetched; if that version doesn't carry it, improvise within the altitude (a diagram, a grouped list, a card) and note nothing is lost — the experience still reads. Never substitute a task-level component (file tree, phase stepper, code outline, code snippet).

## 1. Vision — the headline

The user-facing final shape. Lead the whole doc with it. *What does success look like from the user's seat?*

- **For a CLI / dev tool / API**: the clearest statement is the commands a user runs and the output they get back, with light comments — reach for the **terminal-transcript component** (`pd-cli`).
- **For an app / service / multi-actor flow**: a light-touch **user journey** from intent to outcome — reach for the **user-journey component** (`pd-journey`).
- Add a short **"why now"** in prose. One paragraph, not a background essay.
- Show delivery status on journey/transcript steps where the component supports it, so the reader sees how much of the experience is real today versus still coming — progress measured in user-visible capability.

Do **not** open with architecture or a component diagram. The experience comes first.

## 2. Guard rails — the inherited constraints

The requirements every task must honor, split into **functional** (must-do behaviors, compatibility, data integrity, correctness invariants) and **non-functional** (performance, security, cost, accessibility, operability, observability). State measurable targets where they exist (e.g. a latency budget, a compatibility promise). Give each a stable id so a breakdown task can reference which rails it must respect.

No dedicated component exists yet — improvise with a clear list grouped functional vs non-functional. Keep each rail to a sentence; it's a constraint, not a design.

## 3. Architecture & seams — at the boundary level

The shape of the system and the seams that let tasks proceed independently — described at the **container / boundary** level, never individual files or code.

- **Structure**: a diagram of the major containers/components and how they relate — reach for the **diagram component** (`pd-mermaid`), flowchart for components/containers. Use an ER diagram when the epic is data-shaped.
- **Seams**: name the load-bearing contracts explicitly — the interfaces a task must not break. These are the edges in the diagram; call out the important ones in prose or a short list.

If you find yourself naming files or functions, you've dropped altitude — pull back up to containers and contracts.

## 4. Breakdown — deployable, sequenced tasks

The decomposition into tasks. This is the bridge to `beta-new-task`. For each task:

- It is **independently deployable** — it won't break shipped functionality once complete and pushed (it may sit behind a feature gate).
- It **builds on its predecessors** — show the sequence/dependencies.
- It advances a named **Vision outcome** or **guard rail** — say which.
- Give a short statement of intent and, once planned, a link to its task doc and current status. Do **not** list files or implementation steps — that is the task planner's remit.

No dedicated component exists yet — improvise: a **diagram** (`pd-mermaid`) for the dependency/sequence, plus a card or list per task. Keep the ordering legible; the staircase of deployable increments is the point.

## 5. Decisions — the strategic bets

The calls that shape the whole initiative — recorded as **authored decision records** (`pd-decision`), aggregated into the **decision log** (`pd-decisions`). For anything still open, use a **review thread** (`pd-thread`) so the discussion has a home and an audit trail.

## Rules

- **Altitude**: no files, no code, no intra-task phases anywhere in the doc.
- Every breakdown task must be independently deployable and ordered.
- Prefer the specific component per element; improvise within the altitude where there's a gap; never force a task-level component.
- Keep each tab scannable — lead with the visual, collapse detail behind summaries.
