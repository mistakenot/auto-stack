# Task status lifecycle

Every task carries a status that advances through the workflow. On HTML planning
docs it drives the visual banner; on markdown plans it's plain metadata.

Stages: **draft → pending → executing → complete**. (HTML docs also read as
"blocked" automatically whenever a review thread is unresolved — never set by hand.)

| Stage | Set by | When |
|---|---|---|
| draft | new-plan / new-mini-task / beta-new-task | doc created |
| pending | commit-task | planning docs committed |
| executing | delegate-task | before dispatching to a worker |
| complete | execute-task | when the implementation PR is opened |

## Setting it

- **Markdown task** — set the `status:` field in `plan.md` YAML frontmatter (add it if missing).
- **HTML / beta task** — set the `status` attribute on the `<pd-doc>` element in `plan.html`.

A task uses one format or the other; update whichever exists.
