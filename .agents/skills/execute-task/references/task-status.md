# Task status lifecycle

Every task carries a status that advances through the workflow. On HTML planning
docs it drives the visual banner.

Stages: **draft → pending → executing → complete**. (HTML docs also read as
"blocked" automatically whenever a review thread is unresolved — never set by hand.)

| Stage | Set by | When |
|---|---|---|
| draft | new-task | doc created |
| pending | commit-task | planning docs committed |
| executing | delegate-task | before dispatching to a worker |
| complete | execute-task | when the implementation PR is opened |

## Setting it

Set the `status` attribute on the `<pd-doc>` element in `plan.html`.
