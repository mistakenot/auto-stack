# Feedback: Task 063

## Problems faced

1. **The planning docs were written against an unmerged branch, and two commits landed after.**
   `context.md` cited `auto-mail/` paths read in the task-062 worktree at `fbefad8`, but
   PR #146's post-review sweep (`94cc014`) then reordered `AddressForBinding` by log `seq`
   and bumped `schemaVersion` to 2. D-063-7 anticipated exactly this and made a re-verify the
   gate; running that gate found two false claims and seven drifted line ranges. **The gate
   earned its keep — do not skip it when docs precede a merge.**

2. **Reading a working tree instead of the committed tree produced a phantom correction.**
   Mid-execution I checked `handle.go`, found `describeHandleError`, and told the user the
   rename had landed — retracting a correct earlier report. It had not: `git show 3c2580c:…`
   still had `handleError`. A subagent was mid-rework and I read its uncommitted edit.
   **Verify a claim with `git show <commit>:<path>`, never the working tree, while agents
   are active in it.**

3. **A phase agent kept working after I marked its phase complete.**
   Phase 2 woke on a later message and committed `6d2dca2` while phase 3 was running in the
   same worktree. No damage — history stayed linear — but the shared-worktree concurrency
   hazard was live. **Tell a phase agent explicitly that it is closed.**

4. **`make check` flagged a file the branch never touched.**
   Local Go is 1.27, CI expects 1.26, and the two `gofmt` differ. Reformatting
   `auto-etl/internal/git/extract_test.go` would have been wrong. Lint and commit with the
   1.26 toolchain first on `PATH`.

5. **Review found a real defect the design had not considered.**
   A supervisor shares its children's Binding, so while a child is live the supervisor's own
   sends are stamped as the child's. D-063-4 had reasoned the race safe *for the recipient*,
   and that conclusion was carried across to the *sender*, where it does not hold.

## Reflections

**What was tricky.** The load-bearing question was never "does it work" but "does the test
prove anything". Three assertions passed for the wrong reason before being fixed: a shared
`*sql.DB` serialised by `database/sql` (proves nothing about concurrency), a `chmod` on
`$HOME` that wasn't a break because `~/.auto/mail` already existed and stayed writable, and
an envelope implication that was vacuous until attributes existed.

**What I'd tell myself at the start.** Make each phase break its own tests on purpose.
Phase 3 reverted the atomic marker write and watched a live marker vanish on read 7 of 500;
phase 4 flipped `CallerSender` to "pick the newest" and got a concrete wrong answer. Both
turned "I believe this is right" into evidence. The phases that did this produced the task's
real findings. Equally: when an agent says it deviated, check the claim — one self-reported
deviation (`Sender.Ambiguous` clearing `AgentID`) turned out to conform, because the plan
names three attribute keys and no `senderAgentId`.

**What I almost did but didn't.** Suppressed `senderAgentType` entirely in response to the
P1. It would have made the field dead code — a live marker is exactly what makes a caller
read as a Subagent — deleting AC-6 to fix an edge case in a field the docs already call a
hint rather than proof. The user's instinct that the attribute layer may be premature is the
better frame: ship the Handle, use it, let observed need decide.

## Useful context

- **`D-063-7`'s execution gate.** A planning doc written against an unmerged branch must
  re-verify every citation at execution time. It caught real drift twice.
- **The `files=` attribute per phase.** Every phase stayed inside it, which is why
  `git diff origin/main` over T1's six shared test files shows **zero removed lines** across
  all five phases — the property AC-15 exists to protect.
- **`export_test.go` is package-local.** AC-7's "count store opens through `openStore`" is
  unreachable from `auto-cli`; the counted form belongs in `auto-mail`, with `auto-cli`
  asserting the equivalent (the store file never comes into existence).
- **The G11 seam guard greps prose.** A doc comment naming `database/sql` failed
  `TestNothingOutsideTheMailPackageOpensTheStore`. Reword the comment; never the guard.
- **Harness pytest runs from `harness/`**, not the repo root (`ModuleNotFoundError`).
- **A Subagent's environment is byte-identical to its supervisor's** — same
  `CLAUDE_CODE_SESSION_ID`, same `CLAUDE_PID`, no agent-id variable. Any caller
  discriminator must be built, not read. This is the blocker for closing the attribution gap.
