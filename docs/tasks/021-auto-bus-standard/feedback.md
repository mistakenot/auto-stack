---
hash: "203ff4ce"
id: "142aa87c"
read_when: "reviewing auto bus standard implementation lessons or understanding CloudEvents envelope and JSON-RPC security considerations"
summary: "Post-implementation feedback for the auto bus standard task: worktree rebase conflict with untracked planning docs, registry hermeticity in tests, and security fixes for XSS, DNS-rebinding, and timestamp parsing."
title: "Feedback: Task 021 — Auto Bus Standard"
---

# Feedback: Task 021

## Problems faced
1. Untracked planning docs in worktree conflicted with rebase onto latest main -- had to stash them to /tmp before rebasing. The branch forked before task 021/022 planning docs were committed to main, causing add/add conflicts on plan.md.
2. Registry hermeticity in tests -- loading real `~/.auto/projects.json` in unit tests would break CI. Solved with a functional-option `WithRegistryProvider` injected per request, keeping existing tests hermetic while giving the server live registry refresh.
3. Security review caught XSS via unsanitized `marked.parse` output, DNS-rebinding/CSRF on the `/api/rpc` endpoint, and RFC 3339 nano timestamp rejection -- all valid and fixed post-review.

## Reflections
- The 5-phase serial execution in a shared worktree (lesson from task 017/018) worked well. Phases 2 and 3 touch disjoint modules but sharing a worktree meant no merge conflicts between them.
- The `params.type` vs `method` authority decision should have been pinned earlier -- it caused rework during plan review. When two fields can carry the same semantic, decide which is authoritative before writing code.
- The `worktree` param on `doc.get`/`doc.list` was almost left as an unvalidated arbitrary path. The review caught it -- always validate client-supplied paths against a known registry, even on a loopback-only server.

## Useful context
- CloudEvents spec shaped the envelope well -- `specversion`, `type`, `source`, `id`, `time` are the right minimum. Adding workspace provenance (`remote`, `branch`, `worktree`, `commit`) as extension attributes kept the envelope self-describing without custom framing.
- The JSON-RPC 2.0 notification-gets-no-reply rule doesn't fit an HTTP one-shot binding where the producer needs error feedback. Documenting the deviation explicitly in the spec was the right call over silently breaking the contract.
