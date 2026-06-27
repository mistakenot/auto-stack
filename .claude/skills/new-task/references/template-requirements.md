# Requirements Template

```markdown
---
epic: path/to/epic.md  # optional — only if this task belongs to an epic
---

# Task $ID: $NAME

## Problem
1-2 sentences describing what's wrong or what's needed.

## Goals
- Bullet list of what this task achieves

## Acceptance Criteria

**AC-1**: $title
- Given: $precondition
- When: $action
- Then: $expected_result

**AC-2**: ...

## Out of Scope
- Product-level boundaries (what this task explicitly does NOT do)

## Open Questions
- [ ] $question (answered: $answer)
```

## Rules

- ~1 page max; split larger work into multiple tasks
- ACs use Given/When/Then or casual bullets (match team preference)
- Open Questions must be resolved before moving to solution stage
- If the task belongs to an epic, include `epic:` in YAML frontmatter with the path to the epic file relative from the repo root. Omit frontmatter entirely if no epic.
