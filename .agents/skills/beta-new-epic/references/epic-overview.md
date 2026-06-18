# Epic Overview

An epic is a **contract of intent** between whoever sets the direction and the task planners who will fill it in. It says: here is the user-facing outcome we're aiming at, here are the lines you must not cross, here are the load-bearing interfaces, and here is a sensible decomposition into independently shippable steps. Within those bounds, each task planner has freedom.

This is the `epic.html` produced by `beta-new-epic`, one altitude above the task-level `plan.html`.

## What an epic is for

- **Set direction from the user's point of view.** What does success look like from the seat of the person using the thing? That, not the architecture, is the headline.
- **State the seams.** The load-bearing interfaces/contracts that let tasks proceed independently. A task planner can do anything behind a seam as long as they honor it.
- **State the guard rails.** The functional and non-functional requirements every task inherits — the invariants the initiative must preserve.
- **Break the work into deployable, sequenced tasks.** Each task is independently deployable (it won't break existing functionality once complete and pushed — it may sit behind a feature gate), and each builds on the ones before it.

## Altitude — the one rule that matters

An epic describes **direction and constraints**, not construction. The decomposition into files, code, and intra-task phases is the task planner's job — leaving it open is what gives them latitude. So in an epic:

- **No files, no code, no intra-task phases.** If you're naming a file or writing a step list, you've dropped to task altitude.
- Talk in **user outcomes, constraints, contracts, and container-level architecture**.
- A statistic like "20 files changed" is the wrong unit. Count user journeys, guard rails, and tasks — never files.

## Components: prefer the specific one, improvise within the altitude

`planning-doc` and its `llms.txt` define the available components. For each element, reach for the most specific component the fetched `llms.txt` offers (see [epic-tabs.md](epic-tabs.md) for the mapping). Where no component fits the epic-level need, **improvise with the best available primitive** — a diagram, a list, a card — while holding the altitude.

Two hard "don'ts":
- **Never force a task-level component into an epic.** The file tree, the phase stepper, code outlines, and code snippets all pull the reader down to construction detail. They don't belong here.
- **Don't describe in prose what a component shows.** If a journey, diagram, or decision record carries it, use that; keep prose for the *why*.

## Relationship to tasks

The breakdown is the bridge to the task workflow:
- Each breakdown item becomes a task, planned later with `beta-new-task`.
- A child task records its parent in `pd-meta` (`epic: "$EPIC-ID"`); the epic tracks its tasks and their status.
- The epic stays the source of direction; tasks own their implementation.

## Conventions

- Epic folder: `docs/epics/$ID-$NAME/` (3-digit ID, kebab-case name).
- Artifact: `epic.html` (single file — no separate context file at epic level).
- `pd-meta`: `id`, `name`, `kind: "epic"`, `status` (`planning` → `active` → `complete`), `created`.
- `pd-doc status` (draft/in-review/approved) tracks document review state, separate from the `pd-meta` lifecycle.
- Planning happens on `main`. The epic hard-stops for review before any task is planned.
