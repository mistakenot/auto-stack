# Review Targets — where comment threads go

A review can target a task's planning docs (**task mode**) or any files
(**file mode**: code, markdown, HTML, config). The thread *grammar* is always
the one in [references/review-format.md](references/review-format.md) or
[references/review-format-html.md](references/review-format-html.md); what
changes by file type is **where** the thread is written.

## Sink by file type

| Target file | Sink | Syntax |
| --- | --- | --- |
| `.html` containing `<pd-` elements (`plan.html`, `epic.html`, rich docs) | inline, after the anchored element | `<pd-thread>` (review-format-html.md) |
| `.md` / `.markdown` | inline, below the offending content | `<!-- UNRESOLVED(P1): … -->` (review-format.md) |
| `.html` without `<pd-` elements | inline, after the offending element | `<!-- UNRESOLVED(P1): … -->` (review-format.md) |
| Anything else — code, config, data, tests | sidecar `<file>.review.md` next to the target | `<!-- UNRESOLVED(P1): … -->` plus a `LOCATION:` line |

Never write a thread into a code, config or data file: it would change
behaviour, break parsing (JSON has no comments), and leave review history in
the source.

## Sidecar files

One sidecar per reviewed file, named by appending `.review.md` to the full
filename: `src/api/client.py` → `src/api/client.py.review.md`.

```markdown
# Review: src/api/client.py

<!-- UNRESOLVED(P1): Retry loop never exits on 4xx
LOCATION: src/api/client.py:42 `def fetch`
REVIEW: `fetch` retries every non-200, including 401/404, so a bad token spins
forever. Callers in src/sync.py:88 expect an exception.
-->
```

- **Create if missing, append if present.** Never overwrite a sidecar: earlier
  threads are the decision log. Write the `# Review: <path>` header once.
- **`LOCATION:` is required** — `path:line` plus the enclosing symbol or a short
  quoted snippet, so the thread still points somewhere after edits shift lines.
- Everything else follows review-format.md: status/priority header, `REVIEW:`
  and `AUTHOR:` roles, append-only, one thread per issue.
- **Resolving** a sidecar thread means fixing the *target file* at `LOCATION`,
  then appending the `AUTHOR:` line and status change in the sidecar.
- Sidecars outlive the review. The caller decides whether to keep them (as a
  record) or delete them once every thread is closed.

## After resolution

Plan files keep every thread: there the thread is the decision log. Regular docs
(a README, guide, or plain HTML page) do not: once the caller has verified
resolution it strips their RESOLVED and REJECTED threads with the bundled
`scripts/strip-closed-threads.sh`. A plan file is anything under `docs/tasks/`,
or a pd-* HTML doc. UNRESOLVED threads and sidecars are never stripped.

## Clean-review marker

A review that finds nothing must still leave exactly one thread, so the caller
can tell a clean review from a failed run that wrote nothing:

- **Task mode** — markdown folder: top of `plan.md`, below the title. HTML
  task/epic: a `<pd-thread>` after the first `<pd-section>`.
- **File mode** — in the sink of the **first** target: top of a markdown file
  (below its title); after the first `<pd-section>` of a pd-* doc; after
  `<body>` of plain HTML; otherwise a new sidecar with `LOCATION: <path>`.

```markdown
<!-- RESOLVED(P3): Review complete — no issues found
REVIEW: All targets reviewed against the codebase. No problems detected.
-->
```
