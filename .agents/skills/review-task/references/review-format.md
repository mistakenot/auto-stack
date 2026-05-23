# Review Comment Format

## Comment Syntax

Comments use markdown HTML comments with status, priority, and role tags.

### Raising an issue (UNRESOLVED)

```markdown
<!-- UNRESOLVED(P1): Title of issue
REVIEW: Description of the concern with evidence.
-->
```

### Resolving an issue (RESOLVED)

```markdown
<!-- RESOLVED(P1): Title of issue
REVIEW: Original concern.
AUTHOR: What was changed to address it.
-->
```

### Rejecting an issue (REJECTED)

```markdown
<!-- REJECTED(P1): Title of issue
REVIEW: Original concern.
AUTHOR: Why this doesn't apply, with reference.
-->
```

## Priority Levels

- **P1**: Blocking -- must be fixed before proceeding
- **P2**: Important -- should be fixed, but not a hard blocker
- **P3**: Minor suggestion -- nice to have

## Roles

- **REVIEW**: The reviewer's comment (the concern or question)
- **AUTHOR**: The author's response (fix description or rejection rationale)

## Rules

- Comments are **append-only** (track full decision history, never delete or overwrite previous entries)
- **One thread per issue** -- don't combine multiple concerns into a single comment
- Place comments directly below the offending content with blank lines above and below
- Only comment on real issues (structure, security, assumptions), not formatting
