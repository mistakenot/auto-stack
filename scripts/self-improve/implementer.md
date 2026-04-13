---
name: implementer
kind: service
---

requires:
- priority-item: the specific improvement to implement, including scope, files, and acceptance criteria
- item-number: which priority item this is (1, 2, or 3)
- focus: the tool or area being improved
- codebase-context: optional project context

ensures:
- result: a confirmation containing the branch name and PR URL. The long-term record lives in the contextual commit message and PR body — no separate result file needed.

errors:
- tests-failed: implementation complete but tests do not pass after 2 fix attempts
- build-failed: code does not compile after implementation

strategies:
- step 1 — worktree setup:
  - create a new git branch named: improve/{focus}/{item-number} (sanitize focus for branch name)
  - check out a git worktree for isolated work
  - verify the worktree builds and tests pass before making changes

- step 2 — plan and implement:
  - make the code changes described in the plan
  - follow existing code style and conventions
  - run go build after each file change to catch errors immediately
  - do not add features beyond what the plan specifies

- step 3 — test:
  - run the full test suite for the affected module: go test ./...
  - run go vet ./...
  - if tests fail, fix and retry (max 2 attempts)
  - if still failing after retries, signal tests-failed error

- step 4 — commit and PR:
  - commit using contextual commit style: conventional commit subject, then structured action lines in the body capturing:
    - Problem: the original problem from the priority item
    - Scope: files changed and what changed in each
    - Decisions: any judgment calls made during implementation (deviations from plan, alternative approaches considered)
  - push the branch to origin
  - create a PR using gh pr create with:
    - title summarizing the improvement
    - body containing a full workflow summary:
      - ## Problem — the original problem that motivated this change
      - ## Plan — what was planned (from step 2)
      - ## What changed — files and changes, with brief rationale
      - ## Decisions — anything that deviated from the plan, alternatives considered, trade-offs made
      - ## Problems encountered — anything that went wrong during implementation (build failures, test issues, unexpected code structure)
      - ## Test results — which test suites ran, pass/fail status
  - the PR body IS the long-term record — it gets indexed by autoetl and is queryable by future self-improve runs
  - return the PR URL and branch name

invariants:
- never modify files outside the scope defined in the priority item
- never skip tests — every implementation must have passing tests before PR
- the PR description traces back to the original problem that motivated the change
- existing tests must continue to pass (no regressions)
- no separate result.md file — the commit message and PR body are the durable artifacts
- the PR body must be complete enough that a future self-improve run can understand what was done and why without reading the code diff
