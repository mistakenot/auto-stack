---
name: implementer
kind: service
shape:
  delegates:
    plan-reviewer: [review implementation plan]
---

requires:
- priority-item: the specific improvement to implement, including scope, files, and acceptance criteria
- item-number: which priority item this is (1, 2, or 3)
- focus: the tool or area being improved
- codebase-context: optional project context

ensures:
- result: a report containing the branch name, PR URL, and summary of changes made. If the PR could not be created, includes the reason and what was accomplished.

errors:
- tests-failed: implementation complete but tests do not pass after 2 fix attempts
- build-failed: code does not compile after implementation

strategies:
- step 1 — worktree setup:
  - create a new git branch named: improve/{focus}/{item-number} (sanitize focus for branch name)
  - check out a git worktree for isolated work
  - verify the worktree builds and tests pass before making changes

- step 2 — plan:
  - read the priority item's scope, files, and acceptance criteria
  - read the relevant source code in the worktree
  - write a short implementation plan document to the workspace
  - the plan should list: files to change, what changes in each, new tests to add, and how to verify

- step 3 — plan review:
  - delegate to plan-reviewer with the plan document
  - if plan-reviewer says "revise", update the plan and re-delegate (max 1 revision)
  - proceed with the approved plan

- step 4 — implement:
  - make the code changes described in the plan
  - follow existing code style and conventions
  - run go build after each file change to catch errors immediately
  - do not add features beyond what the plan specifies

- step 5 — test:
  - run the full test suite for the affected module: go test ./...
  - run go vet ./...
  - if tests fail, fix and retry (max 2 attempts)
  - if still failing after retries, signal tests-failed error

- step 6 — PR:
  - commit all changes with a descriptive commit message
  - push the branch to origin
  - create a PR using gh pr create with:
    - title summarizing the improvement
    - body containing: the original problem, what was changed, test results
  - return the PR URL and branch name

invariants:
- never modify files outside the scope defined in the priority item
- never skip tests — every implementation must have passing tests before PR
- the PR description traces back to the original problem that motivated the change
- existing tests must continue to pass (no regressions)
