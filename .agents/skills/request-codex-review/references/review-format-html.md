# Review Comment Format (HTML planning docs)

## Comment Syntax

Comments use `<pd-thread>` and `<pd-comment>` elements with status, priority, and role attributes.

### Raising an issue (unresolved)

```html
<pd-thread anchor="section-id" status="unresolved" priority="p1" title="Title of issue">
  <pd-comment by="review">Description of the concern with evidence.</pd-comment>
</pd-thread>
```

### Resolving an issue (resolved)

```html
<pd-thread anchor="section-id" status="resolved" priority="p1" title="Title of issue">
  <pd-comment by="review">Original concern.</pd-comment>
  <pd-comment by="author">What was changed to address it.</pd-comment>
</pd-thread>
```

### Rejecting an issue (rejected)

```html
<pd-thread anchor="section-id" status="rejected" priority="p1" title="Title of issue">
  <pd-comment by="review">Original concern.</pd-comment>
  <pd-comment by="author">Why this doesn't apply, with reference.</pd-comment>
</pd-thread>
```

## Priority Levels

- **p1**: Blocking -- must be fixed before proceeding
- **p2**: Important -- should be fixed, but not a hard blocker
- **p3**: Minor suggestion -- nice to have

## Roles

- **review**: The reviewer's comment (the concern or question)
- **author**: The author's response (fix description or rejection rationale)

## Placement

Place each `<pd-thread>` directly after the element it discusses. Set `anchor` to that element's `id` attribute (or a `<pd-file>`'s `path`).

## Rules

- Comments are **append-only** (never delete or rewrite an existing `<pd-comment>` -- append a new one and update the thread's `status` attribute)
- **One thread per issue** -- don't combine multiple concerns into a single thread
- Only comment on real issues (structure, security, assumptions), not formatting
- Resolved/rejected threads collapse automatically in the UI; the history stays in the source
