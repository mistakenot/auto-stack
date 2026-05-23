---
hash: "75e99696"
id: "598cb565"
read_when: "when leaving or responding to review comments on task planning documents"
summary: "How to use markdown comments for threaded conversations when reviewing task planning docs."
title: "Communicate During Plan Reviews"
---

# How to communicate during plan reviews

When we create tasks, we create a set of markdown files describing the work to be done. When a reviewer wants to leave comments and start a conversation with the author, we do this through markdown comments.

Each "thread" should start below the offending content, and have a new line above and below it 

## Roles
- `AUTHOR` the session that originally wrote the docs.
- `REVIEW` the session that didn't write the doc and is reviewing it.

## Leaving an initial review comment

The reviewer will start a thread using markdown comments, like so:

```markdown
- we don't need auth middleware for this because it's private network only
<!-- UNRESOLVED(P1): Auth middleware missing
REVIEW: Insecure assumption as this service could accidentally be exposed on the public internet. See [security.md](../../docs/security.md). Recommend to add auth middleware as per normal.
-->
```

## Responding to a comment

The author can choose to either: 

1. Make a change, leave a response and mark it as done.
2. Reject the feedback with a comment explaining why.
3. Continue the conversation by posting a response without resolving or changing anything.

**Resolving**

```markdown
- apply standard auth middleware
<!-- RESOLVED(P1): Auth middleware missing
REVIEW: Insecure assumption as this service could accidentally be exposed on the public internet. See [security.md](../../docs/security.md). Recommend to add auth middleware as per normal.
AUTHOR: Updated to include standard middleware.
-->

**Rejecting**

```markdown
- we don't need auth middleware for this because it's private network only
<!-- REJECTED(P1): Auth middleware missing
REVIEW: Insecure assumption as this service could accidentally be exposed on the public internet. See [security.md](../../docs/security.md). Recommend to add auth middleware as per normal.
AUTHOR: Requirements explicitly call for no auth on line 23 [requirements.md](./requirements.md)
-->
```

**Continuing the conversation**

```markdown
- we don't need auth middleware for this because it's private network only
<!-- UNRESOLVED(P1): Auth middleware missing
REVIEW: Insecure assumption as this service could accidentally be exposed on the public internet. See [security.md](../../docs/security.md). Recommend to add auth middleware as per normal.
AUTHOR: This will block requirement S2, input required from human.
-->
```

## Important

- Comments are append only. We want to track full history of decision making and why.
- Comments should focus on issues that wont be caught by linting or code formatting, like structure, security, invalid assumptions, or unclear expectations, etc.
- If a line in the docs has multiple issues associated, create one thread for each issue, keep each thread focussed on one problem.