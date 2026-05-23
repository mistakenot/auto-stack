---
name: code-review
description: "Performs a structured code review of changes with severity labels covering correctness, security, performance, error handling, testing, and architecture. Use when 'code review', 'review this PR', 'review changes', or when a structured review of implementation changes is needed."
---

# Code Review

Structured review of code changes with severity labels.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Severity Levels

- **[blocking]**: Must fix before merge. Bugs, security issues, data loss risks.
- **[important]**: Should fix before merge. Missing error handling, poor patterns, test gaps.
- **[nit]**: Minor style or clarity issue. Fix if convenient.
- **[suggestion]**: Optional improvement idea. Take it or leave it.
- **[praise]**: Something done well. Call it out.

## Process

### Step 1: Read project conventions

Read project docs (CLAUDE.md, contributing guides, style guides) to understand conventions before reviewing.

### Step 2: Read the diff

```bash
git diff main...HEAD
```

Or for a PR: `gh pr diff $NUMBER`

### Step 3: Identify intent

Determine what the change is trying to accomplish. Read linked task docs, PR description, or commit messages.

### Step 4: Identify touched files

List all files added, modified, or deleted. Group by area (backend, frontend, tests, config).

### Step 5: Run automated checks

Run whatever automated tooling the project has:

- Typecheck
- Linter
- Tests (at minimum the affected ones)

Report results. If automated checks fail, note as `[blocking]`.

### Step 6: Manual review

Review each file change against these dimensions:

- **Correctness**: Does the code do what it's supposed to? Edge cases handled?
- **Simplicity**: Is this the minimum code that solves the problem? Flag speculative abstractions, unnecessary configurability, and error handling for impossible scenarios. If 200 lines could be 50, flag it.
- **Security**: Injection risks? Auth checks? Sensitive data exposure?
- **Performance**: N+1 queries? Unnecessary re-renders? Missing indexes?
- **Error handling**: Are errors caught, logged, and surfaced appropriately?
- **Testing**: Are new code paths covered? Are tests meaningful (not just coverage padding)?
- **Architecture**: Does it follow project patterns? Is the abstraction level right?
- **Surgical precision**: Does every changed line trace to the stated goal? Flag unrelated refactors, style changes, or "improvements" to adjacent code.

### Step 7: Summary

End with a verdict and summary:

```markdown
## Review Summary

**Verdict**: Approve / Request Changes / Comment

**Stats**: N blocking, N important, N nit, N suggestion, N praise

### Blocking Issues
- [blocking] file.ts:42 -- description

### Important Issues
- [important] file.ts:15 -- description

### Nits & Suggestions
- [nit] file.ts:88 -- description
- [suggestion] file.ts:100 -- description

### Praise
- [praise] file.ts:60 -- description
```

## Rules

- Be specific: reference file paths and line numbers
- One issue per bullet
- Praise genuine quality -- it reinforces good patterns
- If everything looks good, say so briefly and approve
