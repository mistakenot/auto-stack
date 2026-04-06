---
hash: "602f82e4"
id: "7e454001"
summary: "Curated research notes on agent engineering principles covering progressive disclosure, worktree isolation, spec-first development, architecture enforcement, and integrated feedback loops."
title: "Research: Agent Engineering Principles (Tweets)"
---

## 1. Progressive Disclosure
- Context window is finite — every token competes for attention
- Flooding the agent upfront degrades reasoning on what actually matters
- Short AGENTS.md (~100 lines) acts as a map, not a manual
- Agent reads entry point first, fetches deeper docs only when needed
- Monolithic instruction files rot fast and become counterproductive
- Capped search (SWE-agent's 50-result limit) is the same idea — force specificity, don't dump everything

## 2. Git Worktree Isolation
- Parallel agents sharing a workspace will collide on file changes
- Each agent gets its own directory, branch, and environment
- Changes are validated in isolation before touching main
- Also applies sequentially — clean handoff between sessions requires a known-good state
- Enables rollback: agent breaks something → `git revert` → try again
- Same model as CI/CD for humans; turns out agents need it too

## 3. Spec First, Repo as System of Record
- Agents cannot see Slack threads, Google Docs, or tribal knowledge — it simply doesn't exist to them
- Anything not in the repo at runtime is invisible
- Feature lists, architectural decisions, conventions must be machine-readable files
- Anthropic used JSON (not Markdown) deliberately — rigid structure resists casual agent overwriting
- Documentation is now a runtime dependency, not a human nicety
- Stale or vague specs produce confident wrong output, not errors

## 4. Mechanical Architecture Enforcement
- 3.5 PRs/engineer/day → human code review can't keep up
- Encode constraints as linters and structural tests that run automatically
- Enforce *invariants* (dependency direction, boundary crossing, naming), not implementations
- Linter error messages written specifically for agent consumption — include the rule violated + remediation steps
- Recurring background tasks scan for drift and open targeted cleanup PRs
- Most can be automerged in under a minute

## 5. Integrated Feedback Loops
- The gap between action and consequence is where cascading failures are born
- SWE-agent: linter runs on every edit, rejects the change before it's applied
- Anthropic: Puppeteer gives agents real browser access — bugs invisible in code become obvious in UI
- OpenAI: full local observability (logs, metrics, traces) queryable by the agent via LogQL/PromQL
- Each agent task runs on an isolated app instance with its own observability data
- Rule: if the agent can't observe the consequence of its action in the domain that matters, it will optimise for proxies that don't correlate with correctness