---
hash: "e50cf1d7"
id: "463f31b9"
read_when: "when reviewing user-journey open questions or implementation roadmap"
summary: "Open questions and action items from reviewing the auto-stack user journey document for consistency and end-to-end coherence."
title: "Review: user-journey.md"
---

# Review: user-journey.md

Open questions focused on journey consistency and end-to-end coherence. Implementation details deferred to when we build each tool.

---

## Action items (done)

1. **Code block boundaries**: Updated journey to briefly describe indentation scoping, matching the implementation.
2. **CLAUDE.md schema**: Removed stale Go structs, now points to `auto-etl/internal/model/model.go`.

---

## Open questions

### Reflection flow — incomplete narrative

The reflection example (steps 1-5) stops at "takes the highest usage session id" and never completes. This is the most important gap because reflection is the payoff of the whole stack — etl + search exist to feed it. The journey should show the full loop: find a problem, diagnose it, propose a rule, write it somewhere, verify it helps.

Related: where do rules land? CLAUDE.md? `.auto/reflect/rules.md`? This affects whether the journey's end-to-end story hangs together.

### ~~Remote host identity~~ — addressed

Added `git_remote` field to sessions and messages tables. Git remote origin URL is the cross-host project identifier.

### ~~Narrative voice~~ — addressed

Added preamble: agents are primary users, JSON is default output.
