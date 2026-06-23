# Review Comment Format (HTML planning docs)

HTML planning docs (`plan.html`, `epic.html`) are authored with the `pd-*` web
component library used by the `planning-doc` skill. Review
comments are just `<pd-thread>` elements added to that same markup, so the doc
keeps rendering and resolved threads still surface in `<pd-decisions>`. This file
is the review-side summary; the authoritative element contract (attributes,
roles, the comment-merge protocol) ships in the pinned component release:

```bash
curl -fsSL https://cdn.jsdelivr.net/gh/mistakenot/skills@pd-v0.9.0/pd-components/dist/llms.txt
```

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

Place each `<pd-thread>` directly after the element it discusses, inside the
relevant `<pd-tab>`. Set `anchor` to that element's `id` attribute (or a
`<pd-file>`'s `path`).

## Rules

- Threads are **append-only**: to raise an issue, add a new `<pd-thread>`; to
  reply or resolve, append a new `<pd-comment>` and update the thread's `status`
  attribute. Never delete or rewrite an existing `<pd-comment>` -- the thread is
  the decision log. (The author edits document content in place; only the thread
  history is append-only.)
- **One thread per issue** -- don't combine multiple concerns into a single thread
- Only comment on real issues (structure, security, assumptions), not formatting
- Resolved/rejected threads collapse automatically in the UI; the history stays in the source
