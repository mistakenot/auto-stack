---
name: new-task-quick
description: "Plans a task end-to-end in one unattended pass: Requirements, Verification, Solution and Plan tabs, pauses only for open questions, then Codex-reviews the answered doc. Use when 'new task quick', 'quick task', 'plan end-to-end'. Not for stage-by-stage review (use new-task)."
---

# New Task Quick

Run all three planning stages in a single unattended pass, producing the same artifacts as
`/new-task` -> `/new-solution` -> `/new-plan`: a four-tab
`docs/tasks/$ID-$NAME/plan.html` plus `context.md`, then run `/request-codex-review`
over the result. Stop at most twice: once to get open questions answered (skipped when there
are none), and once at the end.

> Part of the planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

Each stage runs the *same* instructions the individual skills run -- they are shared refs, not
copies. This skill supplies the autonomy contract those instructions branch on, and replaces
the three hard-stops with at most one, taken only when there are open questions. The Codex review that would normally follow `/commit-task`
runs here instead, once every open question has an answer, so the reviewed plan is the one
that will actually be built.

## Autonomy Contract

This is in force for the whole run. The stage refs below carry `When interactive` /
`When running autonomously` forks: **you are running autonomously.**

1. **Do not stop between stages.** No hard-stop, no summary-and-wait, no "shall I continue".
   The only permitted stop before the final report is the Answer Gate in Stage 5, and only
   when the doc has open questions.
2. **Do not use `AskUserQuestion`.** There is no one to answer.
3. **Never silently guess a load-bearing decision.** Anything you cannot settle from the
   request, the codebase, or a clear repo convention -- and whose answer would change the
   design -- becomes a `<pd-question>`. Then proceed on your own lean. Do not block on it.
4. **Every `<pd-question>` you write MUST carry a non-empty `recommendedAnswer` attribute.**
   Not a prose lean in the question body -- the attribute. The browser pre-fills the answer
   box from it, which is what turns the review into a one-click rubber-stamp; a lean that
   exists only in your chat message is lost the moment the session scrolls away.

   ```html
   <pd-question id="Q-1" status="open" priority="p1" for="<user>"
                title="Durability: JSON file or SQLite?"
                recommendedAnswer="JSON file — no transactions or indexes needed here.">
   ```

   Put the reasoning in the body as usual; put the *decision* in the attribute.
5. **Assumptions you are comfortable proceeding on stay prose bullets.** The rule of thumb in
   [references/tab-requirements.md](references/tab-requirements.md) decides which vehicle:
   if proceeding on your guess would be reasonable, it is a prose assumption; if a wrong
   guess would waste real work, it is a `<pd-question>`.
6. **Keep a running list of every `<pd-question>` you raise**, across all stages. You
   report it at the Answer Gate, and any raised later at the final report.
7. **Write each tab to `plan.html` before starting the next stage,** so an interrupted run
   leaves usable partial work rather than nothing.
8. **Spawn every subagent unnamed.** The stages below fan work out to subagents. Because
   this run is a single unbroken turn, a *named* agent -- an addressable teammate -- can
   never report back to you: its result is delivered only when the turn ends. Omit the
   `name` parameter, wait for the completion notification, and never ask an agent to
   resend a report that merely looks missing.

## Stage 1: Requirements

Read [references/stage-requirements.md](references/stage-requirements.md) and follow it.
Produces `docs/tasks/$ID-$NAME/plan.html` with the Requirements tab.

Note the task ID and folder you create -- later stages say "find the active task folder", and
that is this one. Do not re-scan for a different folder.

## Stage 2: Verification + Solution

Read [references/stage-solution.md](references/stage-solution.md) and follow it.
Produces `context.md` and the Verification and Solution tabs.

Its step 1 asks that Open Questions be resolved. Under this contract they are not: questions
you recorded in Stage 1 stay open, carrying their `recommendedAnswer`. Proceed on those leans.

## Stage 3: Plan

Read [references/stage-plan.md](references/stage-plan.md) and follow it.
Produces the Plan tab and backfills `phases`/`tests` onto every `<pd-ac>` card in the
Verification tab.

## Stage 4: Self-check

Check your own output, then fix what fails:

- `grep -o '<pd-question[^>]*' plan.html` -- every hit carries a non-empty
  `recommendedAnswer`. Any that doesn't is a question the human has to answer cold; add it.
- Run the pd-lint CLI on `plan.html`. Open questions are expected (that is the gate); any
  *other* issue code is a defect you introduced -- fix it before going on.

Then branch on whether the doc carries open questions:

- **No `<pd-question status="open">`** -> go straight to Stage 6 (Codex Review). The review
  runs on settled decisions and the human sees the doc once, finished.
- **One or more open** -> Stage 5 (Answer Gate) first. Reviewing a doc whose load-bearing
  decisions are still guesses would review the wrong plan; the answers may change it.

## Stage 5: Answer Gate (only when questions are open)

This is the one hard-stop. Address the user and present:

1. The path to `plan.html` and a one-paragraph summary of the plan.
2. **Decisions made on your behalf** -- every `<pd-question>` you raised, one line each, as
   `question -> recommendedAnswer`. This is the list the user is being asked to review; make
   it the prominent part of your message, not a footnote.
3. What you need: an answer to each, either by answering in the browser and pasting the
   `=== DOC COMMENTS` block back, or by replying in chat ("accept all", or per-question
   overrides). Say that the Codex review runs once the answers are in.

Then wait. When the answers arrive:

1. **Merge each answer** into its `<pd-question>` as a `<pd-answer by="<user>">` and set
   `status="answered"`. Never edit or delete the question or its `recommendedAnswer`; they
   are the decision log.
2. **Propagate overrides.** Wherever an answer differs from the `recommendedAnswer`, revisit
   every tab that was written on that lean -- Solution, Verification and Plan -- and rewrite
   what the new answer changes. A stamped answer changes nothing.
3. Re-run the pd-lint CLI; it must now be clean.
4. Continue to Stage 6. Do not stop again before the final report.

## Stage 6: Codex Review

Invoke `/request-codex-review $ID` in the current agent context and follow it to the
end, including its mandatory `/resolve-comments` pass. The autonomy contract still
applies while those two skills run:

- **Resolve or reject every thread yourself.** A thread `resolve-comments` would normally
  "continue unresolved" pending user input is a load-bearing decision. Raise it as a
  `<pd-question>` with a `recommendedAnswer`, proceed on that lean in the doc, and say so in
  the thread's `AUTHOR:` reply. Do not leave `UNRESOLVED(P1)` threads behind --
  `/commit-task` refuses them, and unlike an open question the browser gives the
  human no one-click way to settle one.
- **Codex failing is not a reason to stop early.** If `codex` exits non-zero or leaves no
  comments, note the failure (with the log path) for the final report and continue.

## Final Report

Address the user once more (or for the first time, if Stage 5 was skipped). Present:

1. The path to the finished `plan.html` and, if the user has not seen it yet, a one-paragraph
   summary of the plan.
2. **Codex review outcome** -- comment count by priority, how many were resolved vs
   rejected, and what changed in the doc as a result. If the review failed, say so and give
   the log path; the user can rerun `/request-codex-review $ID` themselves.
3. **Questions the review raised**, if any, as `question -> recommendedAnswer`. These are new
   since the user last looked, so make them prominent. While they are open the doc is
   **blocked**: pd-lint reports `open-question` and `/commit-task` will refuse.
   Tell them to rubber-stamp or override each, then run `/commit-task` followed by
   `/execute-task`.

If the review raised nothing new, say so plainly -- the doc is clean and ready for
`/commit-task` then `/execute-task`.
