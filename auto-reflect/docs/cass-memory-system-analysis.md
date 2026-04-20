---
hash: "831a32ff"
id: "afd6ca72"
read_when: "designing autoreflect memory architecture and reflection patterns"
summary: "Analysis of the Cass memory system architecture and what auto-reflect should borrow, adapt, or avoid from it."
title: "Cass Memory System Analysis for Auto-Reflect"
---

# Cass Memory System Notes For Auto-Reflect

## Purpose

This note examines `.tmp/cass_memory_system` and extracts the parts that are useful for the future `auto-reflect` tool.

The key question is not "should we copy CMS?" It is:

- how CMS actually works,
- what user access patterns it exposes,
- which of those patterns fit this stack,
- and which parts `auto-reflect` should avoid inheriting.

## Short Take

CMS is a memory layer around `cass`, not a full autonomous-improvement harness.

Its central idea is strong:

1. raw session history is the ground truth,
2. sessions are summarized into structured diary entries,
3. durable rules are distilled into a scored playbook,
4. later sessions retrieve from that playbook and feed back into it.

That shape is useful for `auto-reflect`.

What should not be copied directly is the storage and substrate choice. CMS is built around `cass`, while this stack already wants `auto-etl` and `auto-search` to provide the shared session corpus and search/index layer.

## How CMS Works

### 1. Core memory model

CMS uses three layers:

- episodic memory: raw agent session history from `cass`
- working memory: structured diary entries per session
- procedural memory: a playbook of reusable rules and anti-patterns

The main schemas are:

- playbook bullets in `.tmp/cass_memory_system/src/types.ts`
- diary entries in `.tmp/cass_memory_system/src/types.ts`
- context results in `.tmp/cass_memory_system/src/types.ts`

### 2. Reflection pipeline

The main orchestration lives in `.tmp/cass_memory_system/src/orchestrator.ts`.

The flow is:

1. discover unprocessed sessions from `cass` using a processed-session log
2. generate a diary entry for each session
3. reflect on that diary and propose deltas
4. validate deltas
5. curate deltas into the durable playbook
6. record implicit feedback from outcomes and inline comments

The important design detail is that CMS does not directly rewrite the playbook from an LLM response. It uses an intermediate delta model:

- `add`
- `helpful`
- `harmful`
- `replace`
- `deprecate`
- `merge`

That is a good pattern for `auto-reflect` because it gives you:

- a reviewable intermediate representation
- cleaner validation
- safer persistence
- better auditability

### 3. Diary generation

The diary layer is not just a summary blob. A diary entry records:

- session path
- agent
- workspace
- status
- accomplishments
- decisions
- challenges
- preferences
- key learnings
- related sessions
- tags and search anchors

CMS has both:

- an LLM-based diary path
- a heuristic fast path for offline or cheap operation

That split is useful. `auto-reflect` should also support:

- a deterministic extraction path from structured session data
- an optional richer LLM pass on top

### 4. Curation and scoring

The most valuable CMS idea is not retrieval, but curation discipline.

Playbook rules carry:

- provenance
- feedback history
- maturity
- deprecation state
- tags
- optional embeddings
- effective score

Rules are not static. CMS applies:

- time decay
- a harmful multiplier
- promotion and demotion
- automatic deprecation
- anti-pattern inversion for repeatedly harmful rules

This means CMS treats the playbook as a living knowledge base, not as a flat notes file.

### 5. Storage model

CMS stores memory in several layers:

- global playbook
- repo playbook
- diary JSON files
- processed-session logs
- context usage logs
- outcomes logs
- privacy audit logs
- embedding cache

Global and repo playbooks are merged at read time.

That merge model is useful. The exact file format probably is not. For this stack, the likely mapping is:

- shared session truth from `auto-etl`
- retrieval/index from `auto-search`
- durable reflect state under `~/.auto/reflect` and `.auto/reflect`

## What Access Patterns CMS Offers

CMS exposes several distinct user access patterns. This is probably the most useful part of the project to study.

### 1. Pre-task retrieval

This is the main path.

The user or agent runs `context` with a task description and gets back:

- relevant rules
- anti-patterns
- related history snippets
- warnings
- suggested follow-up searches

This is the best CMS API shape. It is the practical answer to:

"I am about to do a task. What should I know first?"

For `auto-reflect`, this likely maps to a future command like:

```bash
autoreflect context "implement auth rate limiting"
```

or a cross-tool surface exposed through MCP.

### 2. Post-task feedback

CMS supports explicit and implicit feedback.

Explicit feedback:

- mark a rule helpful or harmful
- leave inline feedback comments in session output

Implicit feedback:

- record an outcome for a session
- infer helpfulness or harmfulness from success, failure, retries, duration, and sentiment

This is strong. A memory system only improves if it closes the loop between retrieval and later outcomes.

For `auto-reflect`, this suggests we should treat retrieval as an event that can later be evaluated, not just as a read-only lookup.

### 3. Provenance inspection

CMS has a `why` command that answers:

- why was this rule learned?
- which sessions led to it?
- what reasoning or evidence supported it?
- how has later feedback changed confidence in it?

This is extremely important for trust.

If `auto-reflect` learns rules but cannot explain them, users will not trust them.

### 4. Similarity and maintenance views

CMS exposes maintenance-oriented views such as:

- similar rules
- top-performing rules
- stale rules
- audit scans
- validation checks for proposed rules

These are operator access patterns rather than day-to-day agent patterns.

They matter because they turn the memory base into something maintainable rather than magical.

### 5. Guided historical onboarding

CMS has an `onboard` workflow that helps a user or agent backfill memory from old sessions.

It supports:

- checking onboarding status
- sampling candidate sessions
- prioritizing sessions that fill coverage gaps
- reading/exporting sessions for review
- tracking progress across onboarding runs

This is one of the most transferable ideas for `auto-reflect`.

We will almost certainly need a way to bootstrap rules from historical data without pretending the system can fully automate that from day one.

### 6. Export and distribution

CMS can export the playbook into agent-facing docs like `AGENTS.md` or `CLAUDE.md`, and it also exposes an MCP server.

That means it supports three consumer modes:

- human CLI use
- agent CLI use
- programmatic tool integration

This is the right direction for `auto-reflect`, but probably later. The first implementation should not try to ship every distribution surface at once.

## What CMS Does Well

### Clear retrieval loop

CMS has a simple story for the agent:

1. ask for context before work
2. do the work
3. leave feedback
4. let reflection update the memory

That is much better than a vague "search your history if useful" pattern.

### Strong intermediate representation

The delta model between reflection and persistence is a good architectural choice.

It would be a mistake for `auto-reflect` to jump straight from "analysis text" to "persisted rule."

### Memory with provenance

CMS keeps enough metadata for rules to be inspectable and reversible.

That matters more than fancy retrieval.

### Graceful degradation

CMS handles missing search, missing embeddings, and missing LLM access without collapsing completely.

This is especially important in agent tooling.

## What CMS Gets Wrong For This Stack

### Wrong substrate

CMS assumes `cass` is the main session system.

This stack already has a different architecture:

- `auto-etl` for canonical transcript normalization
- `auto-search` for indexing and retrieval
- `auto-reflect` as a consumer of those shared datasets

So the CMS ideas are useful, but the implementation substrate should not be copied.

### Too broad a surface too early

CMS has a large command surface:

- context
- similar
- mark
- playbook
- top
- stale
- why
- usage
- validate
- doctor
- reflect
- forget
- audit
- project
- starters
- quickstart
- privacy
- serve
- outcome
- onboard
- guard
- trauma

That is product-rich, but it is too much for the first `auto-reflect` versions.

### Too much product identity around one local tool

CMS is designed as a self-contained memory product.

`auto-reflect` should probably be more modular and more native to the auto-stack shared data model.

## What Auto-Reflect Should Borrow

### Borrow directly

- the episodic -> working -> procedural memory shape
- a structured rule schema with provenance and feedback
- a delta-based reflection pipeline
- a `context` access path
- a `why` provenance path
- an onboarding/backfill workflow
- explicit and implicit feedback loops
- graceful degradation

### Borrow carefully

- rule scoring and decay
- semantic similarity for deduplication
- repo-local plus global memory layering
- export to agent-facing docs
- MCP integration

### Avoid copying directly

- `cass`-specific assumptions
- the oversized initial command surface
- YAML playbook as the only canonical store
- trauma/scar mechanics in the first versions

## Proposed Mapping Into Auto-Reflect

The stack-wide guidance says:

- `auto-etl` creates canonical session data
- `auto-search` indexes it
- `auto-reflect` should use that shared data to find patterns and suggest improvements

So the likely mapping is:

### Episodic memory

Use `auto-etl` canonical session data as the ground truth.

### Working memory

Create a structured reflection record per analyzed session or per analyzed cluster of sessions.

This is CMS's diary concept, but backed by our canonical model rather than raw `cass export`.

### Procedural memory

Create a durable rule store for learned guidance, probably under:

- `~/.auto/reflect/...`
- `.auto/reflect/...`

### Retrieval

Use `auto-search` as the retrieval/index layer, not a direct dependency on `cass`.

## Recommended First Auto-Reflect Surface

If we take the CMS lesson seriously, `auto-reflect` should start narrower than CMS.

I would start with:

### 1. `autoreflect reflect`

Analyze sessions and propose or persist rule deltas.

### 2. `autoreflect context`

Given a task, return:

- relevant rules
- anti-patterns
- related session snippets
- confidence and provenance data

### 3. `autoreflect why <rule-id>`

Explain:

- why the rule exists
- which sessions support it
- what feedback changed its status

### 4. `autoreflect onboard`

Backfill rules from historical sessions with progress tracking.

Later commands can follow if justified:

- `similar`
- `stale`
- `validate`
- `doctor`
- `serve`

## Recommendation

CMS should inform `auto-reflect` mainly as a user model and curation model.

The strongest reusable idea is:

"memory is not just search; memory is retrieval plus feedback plus rule curation."

The right path for `auto-reflect` is probably:

1. keep the CMS memory shape,
2. replace `cass` with the stack's ETL/search pipeline,
3. ship a much smaller initial command surface,
4. make provenance and feedback first-class from the beginning.

## References

Primary CMS files examined:

- `.tmp/cass_memory_system/src/orchestrator.ts`
- `.tmp/cass_memory_system/src/commands/context.ts`
- `.tmp/cass_memory_system/src/diary.ts`
- `.tmp/cass_memory_system/src/playbook.ts`
- `.tmp/cass_memory_system/src/curate.ts`
- `.tmp/cass_memory_system/src/scoring.ts`
- `.tmp/cass_memory_system/src/outcome.ts`
- `.tmp/cass_memory_system/src/commands/onboard.ts`
- `.tmp/cass_memory_system/src/commands/why.ts`
- `.tmp/cass_memory_system/src/commands/serve.ts`
- `.tmp/cass_memory_system/src/types.ts`
- `.tmp/cass_memory_system/src/config.ts`
- `.tmp/cass_memory_system/src/cass.ts`

Relevant stack references:

- `AGENTS.md`
- `auto-reflect/CLAUDE.md`
