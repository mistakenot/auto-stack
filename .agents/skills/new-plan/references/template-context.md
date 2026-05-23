# Context Template

```markdown
# Context: Task $ID

One sentence describing what this file contains + link to solution.md.

## Key Files
- `path/to/file.ts:42` -- description of relevant code
- `path/to/other.ts:15-30` -- code snippet with explanation

## Patterns
- How similar features are implemented in this codebase
- Relevant conventions and constraints

## Related Tasks
- Task $OTHER_ID: what it did that's relevant here
```

## Rules

- Only facts from codebase: paths, snippets, descriptions
- Full paths relative to repo root
- Every code snippet needs a file path + line reference
- Never paste full files -- excerpts only
