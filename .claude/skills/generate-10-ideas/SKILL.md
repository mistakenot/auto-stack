---
name: generate-10-ideas
description: "Generate 10 brilliant ideas for powerful new functionality. Use when brainstorming features, improvements, or innovations for a system. Internally generates 100 candidates and filters to the top 10 most impactful, pragmatic, and innovative ideas."
---

# Generate 10 Ideas

Brainstorm radically innovative yet pragmatic ideas for making a system more compelling, useful, intuitive, versatile, powerful, robust, and reliable.

## Process

1. **Understand the system** — Read available documentation, code structure, and existing capabilities to deeply understand what the system does today, who uses it, and what its constraints are.

2. **Generate 100 candidates** — In your extended thinking, brainstorm ONE HUNDRED distinct ideas across these dimensions:
   - User experience and workflow improvements
   - New capabilities and integrations
   - Performance, reliability, and robustness
   - Developer experience and extensibility
   - Automation and intelligence
   - Composability and interoperability
   - Error handling and recovery
   - Observability and debugging
   - Security and safety
   - Novel interaction patterns

   For each idea, briefly assess: impact (high/medium/low), implementation difficulty (easy/medium/hard), and complexity burden introduced.

3. **Filter ruthlessly** — From the 100 candidates, select exactly 10 that score highest on this combined criteria:
   - **Brilliance**: genuinely clever or non-obvious insight
   - **Impact**: meaningfully improves the system for users
   - **Pragmatism**: achievable without extreme effort or fragile complexity
   - **Innovation**: not just an incremental tweak — a real leap in capability or usability

4. **Present the top 10** — For each idea, provide:
   - **Title**: short, memorable name
   - **One-liner**: what it does in one sentence
   - **Why it's brilliant**: the non-obvious insight that makes this powerful
   - **Implementation sketch**: 2-3 sentences on how you'd build it
   - **Complexity cost**: honest assessment of what this adds to the system

## Output Format

Present ideas ranked #1 (best) through #10, using this structure for each:

```
### #N — [Title]

**What:** [one-liner description]

**Why it's brilliant:** [the insight]

**How to build it:** [implementation sketch]

**Complexity cost:** [honest assessment]
```

## Constraints

- Every idea must be implementable by a small team in days-to-weeks, not months.
- No ideas that require fundamental architectural rewrites.
- No ideas that add complexity without proportional value.
- Prefer ideas that compose well with existing functionality.
- Prefer ideas that unlock further possibilities (force multipliers over dead ends).
- Be specific to the actual system you're looking at — generic advice like "add caching" or "improve error messages" is not acceptable unless the specific application is genuinely novel.
