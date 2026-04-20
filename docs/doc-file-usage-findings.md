---
hash: "29cf1abf"
id: "b3e7f290"
read_when: "analyzing doc discovery patterns or improving doc tooling"
summary: "Analysis of how agents interact with documentation files across 420 coding sessions, revealing that docs are seen constantly but read rarely, discovery bypasses the tooling, and user direction is the primary driver of doc consumption."
title: "Doc File Usage in Agent Sessions: Findings and Structural Insights"
---

# Doc File Usage in Agent Sessions

Analysis from self-improve runs focused on doc file usage, covering 420 sessions indexed via autosearch. April 2026.

## Executive Summary

**a. Do agents actually read and use our doc files?**

Rarely, and almost never on their own initiative. 75% of sessions reference doc paths somewhere, but the vast majority of these references are incidental -- file listings, git status output, autodoc maintenance commands. Of 50 sampled hits on requirements.md files, only 2 were actual Read operations; 28 were tool output noise. Agents see doc paths constantly but almost never open them to read the content.

The exceptions are revealing: user-journey.md appears in 62 sessions and is genuinely consulted. Requirements docs for active sub-projects (auto-search: 50, auto-etl: 26) get real reads. But these top 5 docs account for the overwhelming majority of purposeful reads. The remaining 50+ docs in the repo are effectively invisible to agents.

**b. If agents aren't reading docs, why not?**

Three reinforcing structural causes:

1. **No trigger to read.** Agents are reactive. They read source code because they need it to complete a task. Nothing in the current architecture tells an agent "before implementing X, read doc Y." The CLAUDE.md index is a flat passive list loaded at session start, not an active instruction system. The `autodoc search keyword` command exists but was used in only 1-2 of 420 sessions (and those were during autodoc development/testing, not production use).

2. **Discovery bypasses the tooling.** When agents do find docs, they do it through directory traversal (`ls docs/`), not through the doc index or autodoc search. The carefully designed discovery infrastructure is a dead path. Agents default to filesystem exploration because it's what they already do for code.

3. **Maintenance dominates interaction.** `autodoc fix` ran in 30% of sessions, `autodoc fixed` in 23%. These mechanically update hashes without reading content. The most common "doc interaction" is maintenance noise that creates the illusion of doc engagement without any content consumption.

**c. When docs are read, are they useful to implementation?**

Yes, clearly. When users explicitly direct agents to read requirements docs, the content shapes implementation. 48 sessions had agents say "Let me read the requirements" -- but only 3 of those were user-initiated. The remaining 45 were agent-initiated, typically during complex feature work where the agent recognized it needed context. The cass_inspiration.md doc (31 sessions) was created alongside implementation and saw sustained use. The two-way-freshness doc cluster (5 docs, 67 sessions) drove concentrated usage during complex feature implementation.

The pattern is consistent: docs that are written as part of active implementation work get read. Docs that are written as standalone artifacts do not.

---

## Detailed Insights

### 1. The doc visibility paradox: omnipresent but unread

315 of 420 sessions (75%) contain some reference to "docs/" in their content. This number is misleading. It measures doc *visibility* -- agents encounter doc paths in file listings, git diffs, autodoc output, CLAUDE.md index entries. It does not measure doc *consumption*.

The gap between visibility and consumption is enormous. Agents see doc paths hundreds of times per session through routine operations. Each `git status`, each `ls`, each `autodoc fix` run surfaces doc paths. But seeing a path and reading the file behind it are fundamentally different operations. The doc infrastructure produces a constant background hum of doc references that inflates apparent engagement without driving actual use.

Evidence:
- 50 sampled "requirements.md" hits: 2 were Read operations, 28 were tool output, remainder were incidental references
- `autodoc fix` (126 sessions) and `autodoc fixed` (98 sessions) produce doc path references without reading content
- 0 sessions used `find` targeting doc files; only 3 used Glob patterns targeting docs

### 2. The discovery mechanism exists but agents don't use it

The CLAUDE.md file contains explicit instructions: "Use `autodoc search keyword <query>` to find relevant docs by keyword." Each sub-project CLAUDE.md repeats this instruction. Across 420 sessions, only 1-2 sessions ever ran that command -- and those were during autodoc development/testing, not genuine discovery use.

This is not an awareness problem -- agents load CLAUDE.md at session start and demonstrably follow other instructions in it (running `autodoc fix`, following coding conventions, using correct CLI patterns). The search instruction is ignored because agents have no *reason* to search for docs. Their workflow is task-driven: receive instruction, find relevant code, modify code, test. Docs sit outside this loop.

When agents do discover docs, they use the same filesystem exploration they use for everything else: `ls docs/`, directory traversal, encountering paths in tool output. The autodoc search infrastructure and the doc index are bypassed entirely. This reveals a design assumption that didn't hold: the assumption that agents would proactively seek documentation the way a human developer might browse a wiki.

### 3. Doc read frequency follows a steep power law with structural explanation

The read distribution is not just skewed -- it's structurally predictable:

| Doc | Sessions | Why it's read |
|-----|----------|---------------|
| user-journey.md | 62 | Architectural north star, referenced in CLAUDE.md narrative |
| auto-search/docs/requirements.md | 50 | Active sub-project, agents directed to read before implementing |
| auto-etl/docs/requirements.md | 26 | Active sub-project |
| random.md | 25 | Product overview, useful for orientation |
| signals.md | 24 | Conceptual framing doc |
| cass_inspiration.md | 31 | Created alongside implementation, co-evolved with code |
| auto-env/docs/requirements.md | 1 | Effectively dead |
| auto-config/docs/requirements.md | 3 | Barely started sub-project |

The top-read docs share two properties: they describe active work areas, and they are reachable from CLAUDE.md or sub-project CLAUDE.md files that agents load at session start. The bottom docs describe dormant or very early sub-projects where no implementation sessions occur.

This is not a doc quality problem. auto-env/docs/requirements.md is a well-written, detailed spec. It has 1 session read because auto-env has had almost no implementation sessions. Doc reads are a trailing indicator of implementation activity, not a leading indicator of doc quality.

### 4. User direction is the primary driver of doc consumption

48 sessions contain agents saying "Let me read the requirements." Only 3 of these were responses to explicit user direction. The remaining 45 were agent-initiated, but the pattern is instructive: agents read requirements docs when they recognize they're about to implement something substantial and need the spec. This happens naturally during feature work but almost never during maintenance, refactoring, or bug fixing.

The implication: docs are consumed when the agent's task requires understanding *what to build*, not *how to build it*. For "how" questions, agents read source code. For "what" questions, agents may read docs -- but only if they know the doc exists and can find it quickly.

The 3 user-directed reads had the highest doc-to-implementation correlation. When a user says "read the requirements doc and implement section 3," the agent reads the doc thoroughly and the implementation tracks the spec closely. Undirected reads tend to be shallow -- the agent skims for immediately relevant details and moves on.

### 5. Dated analysis docs are write-once artifacts

7 dated analysis docs exist in docs/ (session-problems-2026-03-20-22.md, improvement-opportunities-2026-03-22.md, etc.). These were produced by reflection and self-improve workflows. Most appear in 8-19 sessions -- but almost entirely as incidental references (git status, file listings, autodoc index entries), not as purposeful reads.

These docs serve a legitimate archival purpose: they capture findings at a point in time. But their presence in the CLAUDE.md doc index (prior to the ignores fix) meant they consumed context window space in every subsequent session without providing value. They are the clearest example of the lifecycle gap: the tooling makes it easy to create docs and index them, but provides no mechanism to distinguish active references from historical artifacts.

6 of the 7 dated docs are not in the current CLAUDE.md index (they were excluded by the ignores config added in a previous improvement cycle). This is correct behavior.

### 6. Sub-project CLAUDE.md files are the most effective doc delivery mechanism

The data shows a clear pattern: docs listed in sub-project CLAUDE.md files get higher read counts than equivalent docs that are only in the root CLAUDE.md index. auto-search has 8 docs in its CLAUDE.md; its requirements.md has 50 session reads. auto-doc has 12 docs across its various CLAUDE.md-indexed entries and its two-way-freshness cluster accounts for 67 sessions.

This works because sub-project CLAUDE.md files are loaded when an agent is *already working in that sub-project*. The context is pre-filtered by task. An agent working in auto-search/ sees auto-search docs, not auto-env docs. The sub-project CLAUDE.md acts as a task-scoped doc index, which is closer to what agents need than the flat root-level index.

### 7. Autodoc maintenance creates an illusion of doc engagement

`autodoc fix` appeared in 126/420 sessions (30%). `autodoc fixed` appeared in 98/420 sessions (23%). These are the most common doc-related operations by a wide margin -- and they involve zero content reading.

These commands mechanically validate frontmatter, update hashes, and regenerate index entries. An agent running `autodoc fix` + `autodoc fixed` produces git diffs that touch doc files, creates commit messages mentioning docs, and generates CLAUDE.md index updates. From the outside, this looks like active doc engagement. From the inside, it's mechanical maintenance that could run as a pre-commit hook with no agent involvement.

The irony: the tool designed to keep docs fresh and connected is the tool most responsible for inflating doc interaction metrics without driving doc consumption.

---

## Connecting Tactical Observations to Structural Patterns

### The lifecycle gap (S1 + S5 + S6 + T5)

Docs are easy to create and automatically indexed, but there is no signal about whether they are consumed. Dated analysis docs (S6) accumulate in the index. Dead requirements docs like auto-env (T5) persist with equal visibility to active ones. Maintenance operations (S5) dominate interactions. The root cause is that doc lifecycle has only two states: exists and doesn't-exist. There is no concept of active/archive/draft, no read tracking, no staleness-by-disuse.

### The discovery bypass (S2 + S3 + T4)

The autodoc search mechanism (S2) and the root doc index are bypassed because agents discover docs through filesystem traversal (S3). Sub-project CLAUDE.md files work better (T4) because they're loaded automatically in the right context. The root cause is an assumption mismatch: the tooling assumes agents will search for docs; agents actually encounter docs as a side effect of code exploration.

### The user-direction dependency (S1 + S4 + T3 + T6)

Docs drive implementation when users direct agents to read them (S4). Docs created alongside implementation get sustained use (T3). user-journey.md is most-read because it's referenced in the CLAUDE.md narrative, not just listed (T6). The root cause is that agents are fundamentally reactive -- they follow instructions and explore code, but they don't proactively browse documentation. Doc consumption requires either explicit direction or tight integration with the task at hand.

### The maintenance-consumption inversion (S5 + S7 + T1)

The most common doc interactions are maintenance operations that don't read content (S5). The steep power law in reads (S7) means most docs get zero purposeful reads. Even docs created for specific workflows like auto-package-patterns.md see minimal use (T1, 8 sessions). The root cause is that the doc tooling optimizes for doc *health* (frontmatter validity, hash freshness, index completeness) rather than doc *utility* (does this doc help agents complete tasks).

### The doc-as-context pattern (T3 + T6 + T7)

The docs that actually get used share a common trait: they provide context for active work, not standalone reference material. cass_inspiration.md (T3) was created during implementation and co-evolved with code. user-journey.md (T6) describes the north-star architecture that agents need to understand. The two-way-freshness cluster (T7) was concentrated during a specific complex feature. Docs work when they're part of the workflow, not when they're a separate resource to consult.

---

## Review Notes

Independent verification of the above findings (reviewer ran separate autosearch queries):

- **Incidental references (75%)**: Confirmed directionally, though "almost all incidental" slightly overstates it -- there is a meaningful subset (~26 sessions) of explicit user-directed doc reads.
- **autodoc search keyword usage**: The "exactly 1 session" figure is imprecise. 2 sessions contained actual invocations, but both were during development/testing. Zero production use is the accurate claim.
- **Discovery via traversal**: Strongly confirmed. Agents reach docs via path traversal or user direction, never via autodoc search.
- **User-direction dependency**: Confirmed. No counter-evidence of agents proactively seeking requirements docs without prompting.
- **Maintenance dominance**: Confirmed. Hash/link maintenance operations outnumber content reads by an order of magnitude.
- **Additional nuance**: Agents do occasionally proactively read docs -- "check the documentation" appeared in 20+ assistant-initiated messages, typically when encountering errors. The zero-proactivity claim should be "near-zero" rather than absolute.
- **Some docs have genuine readership**: signals.md (31 sessions), random.md (25 sessions), auto-img-research.md (17 sessions) are not zero-read artifacts. The "write-only" characterization applies to dated analysis docs but not to all reference docs.
