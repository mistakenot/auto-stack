# Task Mode — reviewing a task's planning docs

The review target is a task folder under `docs/tasks/`. Review its planning
docs **as a set**: every claim against the codebase, then every doc against the
others.

## Step 1: Load Context

1. Find the task folder under `docs/tasks/` matching the provided ID or name.
2. Identify the doc layout and read **every** planning doc present. A folder uses one layout or the other, not both:
   - **HTML task** — `plan.html` + `context.md`. `plan.html` is a single self-contained file with one `<pd-tab>` per stage: **Requirements**, **Verification**, **Solution**, **Plan**. Read the full HTML source; sections carry stable kebab-case `id`s that you anchor comments to.
   - **HTML epic** — `epic.html`. Review it the same way, treating its direction / guard-rail / task-breakdown tabs as the units to verify.
   - **Markdown task** — `requirements.md`, `solution.md`, `context.md`, `plan.md`.
3. Read any project docs referenced by the task docs (linked concept docs, how-to guides).

## Step 2: Codebase Verification

Verify each concern against the actual codebase. The same concerns live in different places depending on layout:

| Concern | Markdown file | HTML location (`plan.html`) |
| --- | --- | --- |
| Problem, goals, scope | requirements.md | Requirements tab |
| Acceptance criteria & test coverage | requirements.md (ACs) + solution.md (coverage table) | Verification tab (`<pd-ac>` cards) |
| Approach & file changes | solution.md | Solution tab (`<pd-files>`, `<pd-api>`) |
| Codebase snippets / references | context.md | context.md |
| Execution sequence | plan.md | Plan tab (`<pd-stepper>` / `<pd-phase>`) |

**Requirements / scope** (requirements.md · Requirements tab)
- Are acceptance criteria testable and unambiguous?
- Do referenced features/pages actually exist?
- Are there implicit dependencies not mentioned?
- Does "Out of Scope" make sense, or will the task be incomplete without those items?

**Solution & approach** (solution.md · Solution tab)
- Do listed file paths exist (for `~`/`edit` modified files) or make sense as new files (`+`/`add`)?
- Do referenced types, functions, and services have the described signatures?
- Does the approach match established project patterns?
- Are there security concerns -- auth, tenant isolation, input validation?

**Verification & test coverage** (solution.md coverage table · Verification tab)
- Does the test coverage cover all acceptance criteria?
- For HTML: does each `<pd-ac>` carry accurate `phases`/`tests` traceability chips, not placeholders?

**Context** (context.md)
- Are code snippets accurate to the current state of the files?
- Are line number references correct?
- Are important related files or patterns missing?

**Plan** (plan.md · Plan tab)
- Will the execution sequence work? Are phase dependencies correct?
- Are commands correct (test paths, npm scripts, workspace flags)?
- Do success criteria verify all acceptance criteria?
- Are there missing steps the implementer will need to figure out?

## Step 3: Cross-Document Consistency

- Every acceptance criterion traces to test coverage **and** to plan steps. (Markdown: solution.md coverage table + plan.md steps. HTML: each `<pd-ac>`'s `tests`/`phases` chips → matching `<pd-phase>` in the Plan tab.)
- File paths are consistent across approach, context, and plan. (Markdown: solution.md / context.md / plan.md. HTML: Solution-tab `<pd-files>` vs Plan-tab `<pd-phase files>`.)
- Types/interfaces in context.md match what the solution proposes to use.
- The approach (solution) matches the execution sequence (plan).
- Epic linkage is consistent. (Markdown: if any doc has `epic:` frontmatter, all four must share the same value; if requirements.md has none, none should. HTML: if the task claims an epic, the `pd-meta` block / epic reference must agree across the doc.)

## Step 4: Placement

- **Markdown docs**: insert the comment block directly below the content it addresses, with a blank line above and below.
- **HTML docs**: insert a `<pd-thread>` directly after the anchored element, with its `anchor` set to that element's `id` (or a `<pd-file>`'s `path`), inside the relevant tab. This keeps the doc compatible with the rich-doc workflow, which renders threads in place and surfaces resolved ones in `<pd-decisions>`.

If a single pd-* planning doc is reviewed in file mode, apply Step 2 to the tabs
it contains and Step 3 within that doc only.
