# File Mode — reviewing arbitrary files

The review targets are the files (and directories) the caller named: code,
markdown, HTML, config, tests. Each is reviewed on its own terms, verified
against the rest of the codebase, then checked against the other targets.

## Step 1: Load Context

1. **Expand the targets.** Files as given; a directory expands to
   `rg --files <dir>` minus any `*.review.md`. Read every target in full.
2. **Find the ground truth around each target** — callers and importers
   (`rg -n <symbol>`), the tests that cover it, docs that describe it, configs
   that feed it. Read them as evidence; they are not review targets, so do not
   comment on them.
3. **Apply `--focus`** if the caller gave one: it is the primary lens and sets
   priorities. Still raise any P1 you find outside it.

## Step 2: Review Each File by Kind

| Target kind | What to check |
| --- | --- |
| **Code** | Correctness, including edge cases and error paths; behaviour matches what callers and tests expect; security (input validation, injection, secrets, authz); resource, concurrency and cleanup problems; dead or duplicated logic; risky branches with no test |
| **Tests** | Assertions actually prove the behaviour the test name claims; over-mocking that hides real failures; timing/order flakiness; important cases missing |
| **Markdown / docs / skills** | Every claim holds against the code — paths exist, commands and flags are real (read the source or run `--help`), names and signatures match; instructions are runnable end to end; no internal contradictions or stale sections; links resolve |
| **pd-\* planning doc** (`plan.html`, `epic.html`) | The checklist in [references/review-plan.md](references/review-plan.md), for the tabs present |
| **Plain HTML** | Content as for docs; broken structure, scripts or links that affect what renders |
| **Config / data** (JSON, YAML, TOML, env) | Every key and value is consumed by the code with the expected type; no drift between environments; no committed secrets |

Do not comment on formatting or style unless the focus asks for it.

## Step 3: Cross-File Consistency

When there are several targets: shared names, types and paths agree; a doc and
the code it documents describe the same behaviour; a config and its loader agree
on keys.

## Step 4: Priorities and Placement

- **P1** — wrong behaviour, security hole, or a claim that will mislead the reader into a failure.
- **P2** — likely bug, significant gap, or an inaccuracy with a workaround.
- **P3** — minor improvement.

Write each thread in the sink [references/review-targets.md](references/review-targets.md)
assigns to that file's type: inline for markdown and HTML, a `<file>.review.md`
sidecar for everything else. Every sidecar thread needs a `LOCATION:` line.
