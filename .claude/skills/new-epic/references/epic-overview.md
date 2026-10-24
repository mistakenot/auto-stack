# Epic Overview

An epic is a **contract of intent** between whoever sets the direction and the task planners who will fill it in. It says: here is the user-facing outcome we're aiming at, here are the lines you must not cross, here are the load-bearing interfaces, and here is a sensible decomposition into independently shippable steps. Within those bounds, each task planner has freedom.

This is the `epic.html` produced by `new-epic`, one altitude above the task-level `plan.html`.

## What an epic is for

- **Set direction from the user's point of view.** What does success look like from the seat of the person using the thing? That, not the architecture, is the headline.
- **State the seams.** The load-bearing interfaces/contracts that let tasks proceed independently. A task planner can do anything behind a seam as long as they honor it.
- **State the guard rails.** The functional and non-functional requirements every task inherits — the invariants the initiative must preserve.
- **Break the work into deployable, sequenced tasks — as vertical slices.** Each task is independently deployable (it won't break existing functionality once complete and pushed — it may sit behind a feature gate), and each builds on the ones before it. Slice by thin end-to-end capability, not by layer: the first task is a walking skeleton that runs through every layer and yields an early end-to-end verification signal; later tasks thicken it. Decomposing layer-by-layer (all backend, then all frontend, integrate last) defers integration risk to the end — don't.

## Altitude — the one rule that matters

An epic describes **direction and constraints**, not construction. The decomposition into files, code, and intra-task phases is the task planner's job — leaving it open is what gives them latitude. So in an epic:

- **No files, no code, no intra-task phases.** If you're naming a file or writing a step list, you've dropped to task altitude.
- Talk in **user outcomes, constraints, contracts, and container-level architecture**.
- A statistic like "20 files changed" is the wrong unit. Count user journeys, guard rails, and tasks — never files.

## Inclusion — what's the cost of getting it wrong?

Altitude says what *kind* of thing belongs in an epic. This rule says *which* of those earn a place. For any candidate guard rail, seam, decision, or detail, ask: **what's the cost of getting this wrong?**

- **Include it if a wrong call would break cross-task work** — rework in more than one task, or a change to which tasks exist and in what order.
- **Leave it out if the cost is contained within one task.** That task's planner will ask the same question in their own nested planning loop, at task altitude, and catch it there. Omitting it is what hands them latitude.
- **Leave it out if a task planner would obviously get it right unaided** from the codebase and project docs, even when the cost of getting it wrong is high. Stating the obvious is noise.
- **Defer silently.** Don't write "TBD" or "to be decided later" — that is detail in disguise. The one exception: when a later task depends on a choice an earlier task will make, name that dependency as a seam so the ordering stays visible.

The two rules work together. Cost-of-wrong decides *whether* something is in; altitude decides *how* it appears. Some expensive-to-change things are low-level by nature — a wire format, a public API name, a storage schema. They belong, but as a one-sentence seam or guard rail (the contract), never as a design.

The walking-skeleton rule is this principle applied to sequencing: integration is the most expensive thing to get wrong, so it goes first.

## Components: prefer the specific one, improvise within the altitude

`rich-doc` and its `llms.txt` define the available components. The epic family — `pd-outcome`, `pd-cli`, `pd-journey`, `pd-guardrail`, `pd-task` + `pd-breakdown` — is built for this altitude; [epic-tabs.md](epic-tabs.md) maps each tab to its component. Reach for the most specific one the fetched `llms.txt` offers. Where no component fits the epic-level need, **improvise with the best available primitive** — a diagram, a list, a card — while holding the altitude.

Two hard "don'ts":
- **Never force a task-level component into an epic.** The file tree, the phase stepper, code outlines, and code snippets all pull the reader down to construction detail. They don't belong here.
- **Don't describe in prose what a component shows.** If a journey, diagram, or decision record carries it, use that; keep prose for the *why*.

## Relationship to tasks

The breakdown is the bridge to the task workflow:
- Each breakdown item becomes a task, planned later with `new-task`.
- A child task records its parent in `pd-meta` (`epic: "$EPIC-ID"`); the epic tracks its tasks and their status.
- The epic stays the source of direction; tasks own their implementation.

## Conventions

- Artifact: a single file `docs/epics/epic-$ID-$NAME.html` (3-digit ID, kebab-case name) — no per-epic folder and no separate context file at epic level. The next ID is the highest existing `epic-*.html` number in `docs/epics/` + 1.
- `pd-meta`: `id`, `name`, `kind: "epic"`, `status` (`planning` → `active` → `complete`), `created`.
- `pd-doc status` (draft/in-review/approved) tracks document review state, separate from the `pd-meta` lifecycle.
- Planning happens on `main`. The epic hard-stops for review before any task is planned.
