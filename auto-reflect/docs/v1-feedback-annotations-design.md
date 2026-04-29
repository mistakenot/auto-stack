---
hash: "e97b2983"
id: "fb-annot-v1"
read_when: "designing or building the autoreflect feedback annotation system"
summary: "Design analysis for a v1 autoreflect feedback annotation CLI — agents annotate files as good/bad/missing after tasks, stored as JSONL with git provenance, queryable by type/file/time."
title: "V1 Feedback Annotations Design"
---

# V1 Feedback Annotations Design

## What This Is

A design analysis for a minimal `autoreflect` prototype that captures agent feedback on specific file content during or after a coding session. The goal is to build the cheapest useful data capture layer so we can start experimenting with what feedback agents actually produce, before investing in the full reflection pipeline.

## Proposed CLI Surface

```bash
# Mark specific lines in a file as beneficial
autoreflect good ./path/to/file.md --start 10 --end 13 --comment "clear examples helped avoid wrong approach"

# Mark specific lines as harmful
autoreflect bad ./path/to/file.md --start 4 --end 6 --comment "outdated instructions caused 20min debugging detour"

# Note missing context that had to be figured out manually
autoreflect missing --comment "no docs on how the auth middleware chains — had to read 4 files to piece it together"

# Query recent feedback
autoreflect list                              # all recent
autoreflect list --type good                  # only good
autoreflect list --type bad                   # only bad
autoreflect list --type missing               # only missing
autoreflect list --file ./path/to/file.md     # by file
autoreflect list --since 7d                   # by time
```

## How This Relates to the Existing Research

The auto-reflect docs describe a layered architecture that progresses from raw session data through to distilled rules:

1. **Episodic** (raw session logs via auto-etl)
2. **Working** (structured reflections, diary entries)
3. **Procedural** (durable playbook rules)

The existing V1 requirements doc (`requirements.md`) targets layer 3 — a rule playbook with `rule create` and `lookup`. That design assumes someone (human or agent) has already done the thinking to distill a lesson into a reusable rule.

This proposal targets a new layer between 1 and 2 — **explicit annotations on content that helped or hurt during actual work**. It's the raw signal that later reflection can aggregate into rules.

The V2 feedback doc (`v2-feedback-and-learning-loop.md`) describes `autoreflect feedback <rule-id> --helpful|--harmful` as feedback on rules. This proposal is feedback on files and content, not rules. They're complementary:

- **File annotations** (this proposal): "this content in CLAUDE.md line 10-13 was helpful" / "this section was actively harmful"
- **Rule feedback** (V2 doc): "rule r-1a2b3c4d was helpful when I encountered auth issues"

The decision mining doc describes automated extraction of decisions from session transcripts using keyword filtering and LLM classification. This proposal is the explicit counterpart — the agent tells us directly what helped, instead of us mining it from transcripts later.

## Does This Lock Us Into Bad Decisions?

Short answer: no, with a few adjustments.

### What's safe about this design

**JSONL is maximally flexible.** Append-only, schema-on-read, trivially extensible. Adding new fields later doesn't break existing records. This is the right format for a data capture layer where we don't know the final schema yet.

**`good/bad/missing` is a clean starting taxonomy.** It maps directly to the three questions an agent can answer after a task:
- What helped? (good)
- What hurt? (bad)
- What was missing? (missing)

If we need more types later (`unclear`, `outdated`, `conflicting`), JSONL handles that without migration.

**Git hash anchoring is good.** It pins annotations to a specific repo state so we can detect drift later. The CASS analysis doc and trauma candidate doc both emphasize provenance — knowing *when* and *in what context* a judgment was made.

### What needs adjustment to avoid regret

**1. Store the actual content snippet, not just line ranges.**

Line numbers drift immediately as files change. If we only store `start: 10, end: 13`, we need the exact git hash + file path to reconstruct what was annotated. That works, but it's fragile and expensive to query.

Better: also store a `content_snippet` field with the actual text of the annotated lines. This makes annotations self-contained and human-readable without git checkout gymnastics. The git hash still serves as the provenance anchor, but the snippet is the portable record.

```json
{
  "type": "bad",
  "file": "docs/auth.md",
  "start": 4,
  "end": 6,
  "content_snippet": "Use JWT tokens for all API auth.\nTokens never expire.\nStore tokens in localStorage.",
  "comment": "outdated — tokens now expire, and we moved to httpOnly cookies",
  "git_hash": "a1b2c3d4",
  "timestamp": "2026-04-29T14:22:00Z"
}
```

**2. Use `.auto/reflect/annotations.jsonl`, not `comments.jsonl`.**

"Comments" is ambiguous (code comments? PR comments? inline feedback comments from the V2 doc?). "Annotations" is precise — these are structured labels on specific content. This also avoids collision with the V2 inline feedback markers concept (`// [autoreflect: helpful r-1a2b3c4d]`) which the docs call "inline feedback comments."

**3. Make `--start`/`--end` optional, not required.**

Sometimes the feedback is about an entire file ("this whole doc was useless") or about a concept that spans a file without clean line boundaries. Requiring line ranges would force agents to pick arbitrary boundaries when the real annotation is file-level.

```bash
# File-level annotation (no line range)
autoreflect bad ./docs/setup.md --comment "entirely outdated, every step was wrong"

# Line-level annotation
autoreflect good ./CLAUDE.md --start 10 --end 13 --comment "saved time on test strategy"
```

**4. Capture workspace and relative paths, not absolute paths.**

Absolute paths break across machines. Store the file path relative to the workspace root, and store the workspace root separately. This aligns with how auto-etl stores `workspace` and how autosearch filters by `--cwd`/`--remote`.

## Proposed Record Schema

```json
{
  "id": "a-1a2b3c4d",
  "type": "good|bad|missing",
  "file": "docs/auth.md",
  "start_line": 4,
  "end_line": 6,
  "content_snippet": "the actual text of annotated lines",
  "comment": "agent's explanation of why this was good/bad/missing",
  "git_hash": "a1b2c3d4e5f6...",
  "workspace": "/home/charlie/src/my-project",
  "timestamp": "2026-04-29T14:22:00Z"
}
```

Field notes:
- `id`: generated, regex `^a-[0-9a-f]{8}$` (prefix `a-` for annotation, vs `r-` for rules)
- `type`: required, one of `good`, `bad`, `missing`
- `file`: relative to workspace, required for `good`/`bad`, absent for `missing`
- `start_line`/`end_line`: optional, only meaningful when `file` is present
- `content_snippet`: auto-captured from file when line range given, absent otherwise
- `comment`: required, trimmed non-empty string
- `git_hash`: auto-captured from HEAD at annotation time
- `workspace`: auto-detected from cwd
- `timestamp`: auto-generated ISO 8601

For `missing` type, `file` could optionally point to the file where the missing info *should* live, if the agent knows. But it shouldn't be required — sometimes the whole point is "I don't know where this should be documented."

## Query API

Keep it simple. The `list` command filters the JSONL and returns matches.

```bash
autoreflect list [--type good|bad|missing] [--file <path>] [--since <duration>] [--format json|text] [--limit <n>]
```

Default: JSON, all types, last 30 days, limit 50.

Filters are AND-combined. `--file` matches as a substring so you can filter by directory (`--file docs/`) or exact file.

JSON output shape:

```json
{
  "annotations": [...],
  "total": 12,
  "filters": {
    "type": "bad",
    "file": null,
    "since": "2026-04-22T00:00:00Z"
  }
}
```

Text output: one annotation per block, sorted newest-first, with type/file/comment on separate lines.

## What the Long-Term Vision Looks Like

Based on the full research doc set, the end state is a pipeline:

```
[raw annotations]  →  [pattern detection]  →  [rule proposals]  →  [playbook rules]
     (this v1)          (future reflect)       (human review)       (existing v1 req)
```

**Stage 1 (this proposal):** Agents annotate files after tasks. We accumulate raw signal in JSONL.

**Stage 2 (reflection):** A periodic reflection agent runs over annotations + session data:
- "docs/auth.md lines 4-6 have been marked `bad` 5 times in the last month"
- "3 agents noted missing docs on middleware chaining"
- "CLAUDE.md lines 10-20 are consistently marked `good`"
- Proposes rule candidates: "Update auth.md section on token expiry" or "Add middleware chaining docs"

**Stage 3 (promotion):** Following the trauma candidate promotion pattern from the research — proposed rules go through human review before becoming durable playbook entries. This prevents noisy heuristics from silently writing policy.

**Stage 4 (feedback loop):** Following the V2 feedback doc — once rules exist, agents can mark them helpful/harmful. Combined with the annotation data, we get a full loop:
- Annotations tell us what content helps/hurts
- Rules tell us what to do about it
- Rule feedback tells us if the rules work

The CASS analysis doc's "asymmetric scoring" (4x weight for harmful vs helpful) and "confidence decay" (90-day half-life) are relevant here too — annotations that say "bad" should weigh more heavily than "good" in aggregate analysis, and old annotations should decay unless reinforced.

## What We Learn from the Decision Mining Doc

The decision mining doc warns about two failure modes:

1. **Over-specific rules** from single instances ("don't use ast-grep" when the real lesson was "don't add dependencies")
2. **Over-general rules** from poor abstraction ("prefer server-side" when it was really about security-sensitive operations)

Raw annotations help with both problems because they preserve the original context. Instead of jumping straight from "agent got confused" to "new rule: don't do X", we have:
- The exact file content that caused the problem
- The agent's explanation of why
- A git hash for full reconstruction

This means the reflection step (stage 2) can look at multiple annotations before proposing a rule, which is exactly what the decision mining doc recommends: "One instance is an anecdote. Three instances with shared context is a pattern worth codifying."

## Implementation Notes

**Minimal v1 scope:**
- Three commands: `good`, `bad`, `missing`
- One query command: `list`
- One file: `.auto/reflect/annotations.jsonl`
- Standard auto-stack patterns: JSON default, text mode via flag, strict validation, remediation hints on error

**What to skip for v1:**
- No `rule create`/`lookup` yet — that's the existing requirements doc and can be added alongside or after
- No LLM calls
- No auto-reflection/aggregation
- No MCP
- No integration with autosearch (future: cross-reference annotations with session data)
- No deduplication (if an agent annotates the same lines twice, store both — dedup is a reflection concern)

**Go implementation:**
- Standard Cobra CLI
- JSONL append for writes, line-scan with filtering for reads
- git hash from `git rev-parse HEAD`
- Content snippet from reading the file at annotation time
- Workspace from git root detection

## Open Questions

1. **Should `missing` accept an optional `--file` for "this info should live here"?** Probably yes, since it's zero-cost and useful for the reflection step to know where gaps cluster.

2. **Should we capture `session_id` or `agent` metadata?** The V2 feedback doc recommends not requiring it but storing it when available. For v1, auto-detect if possible (e.g., check if `CLAUDE_SESSION_ID` or similar env vars exist), otherwise leave null.

3. **Should annotations be repo-local only?** For v1, yes — stored under `.auto/reflect/` in the project. Global annotations can come later if needed.

4. **How does this relate to `autoreflect rule create`?** They're different layers. Annotations are raw observations ("this content was bad"). Rules are distilled guidance ("always check token expiry before debugging auth"). Both should exist eventually. This proposal ships the observation layer first because it's simpler and generates the data that makes good rules possible.
