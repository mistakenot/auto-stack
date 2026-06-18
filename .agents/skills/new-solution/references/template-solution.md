# Solution Template

```markdown
---
epic: path/to/epic.md  # optional — only if this task belongs to an epic
---

# Solution: Task $ID

## Approach
1. High-level step
2. High-level step
3. ...

## Files
```
+ path/to/new/file.ts          # brief description
~ path/to/modified/file.ts     # what changes
```

## Test Coverage

| AC  | Test Type   | File                        |
|-----|-------------|-----------------------------|
| AC-1 | e2e        | e2e/tests/feature.spec.ts   |
| AC-2 | integration | src/__specs__/svc.spec.ts   |
| AC-3 | unit       | src/feature.test.ts          |

## Out of Scope
- (copy from requirements + add technical boundaries)

## Rejected Alternatives
- **Option B**: one-line reason it was rejected
- **Option C**: one-line reason it was rejected
```

## Rules

- Files section uses `+` for new files, `~` for modified, `-` for deletion
- Test Coverage table maps every AC to a test type and file
- Include bare-bones code outlines (types, signatures) in Files section for complex changes
- If requirements.md has `epic:` frontmatter, copy it to this file's frontmatter. Omit frontmatter if no epic.
