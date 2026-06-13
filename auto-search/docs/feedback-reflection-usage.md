---
hash: "9d41bbca"
id: "b4e2d831"
read_when: "prioritizing autosearch improvements based on reflection workflow needs"
summary: "Field feedback from using autosearch for session reflection: what worked, what didn't, and concrete improvements needed."
title: "Feedback: Reflection Usage of Autosearch"
---

# Feedback: Reflection Usage of Autosearch

Field notes from using `autosearch` to analyze coding session history for recurring problems and improvement opportunities. This covers what worked, what didn't, and what would make it much better.

## What Worked

Keyword search was effective for surfacing known problem categories. These queries found real signal:

```bash
# Wasted iteration — agent going in circles, retrying the same thing
autosearch search '"try again" OR "retry" OR "attempt"' --highlight

# Environment gaps — tools or commands missing from the dev container
autosearch search '"command not found" OR "not installed"' --highlight

# Agent guessing wrong — reverts, wrong approaches, undo
autosearch search '"revert" OR "undo" OR "wrong approach"' --highlight

# Flaky tests — tests that pass on retry, race conditions
autosearch search '"race" OR "flaky" OR "passes on retry"' --highlight

# Duplicate/conflict issues — name collisions, merge conflicts
autosearch search '"already exists" OR "duplicate" OR "redeclared"' --highlight

# Stuck processes — timeouts, hangs, infinite loops
autosearch search '"timeout" OR "hang" OR "stuck" OR "infinite loop"' --highlight
```

The strongest findings came from reading actual transcripts via `message get` / `session get`, not from search hit counts.

## What Didn't Work

### Hit counts are useless for ranking

Almost everything hits 50 (the result cap). There's no way to tell if "server functions" comes up 50 times or 500 times. When everything saturates at the cap, you can't compare frequency between topics — "yes this topic exists" is not a meaningful frequency signal.

### Highlights were always null

Both at session and message scope. Not clear if this is a bug or a config issue, but it meant a second round-trip (`message get`) for every hit just to see context.

### No aggregation

What was really needed: "which files appear in the most Read tool calls across sessions" or "which doc paths show up most often." Autosearch does keyword search on text — it can't do structured queries against tool metadata (tool_name, tool_file_path, bash_command).

### Too much noise in correction searches

Searching for user corrections (`"don't" OR "stop" OR "wrong"`) returned mostly "tool use was rejected" system messages, not actual user feedback. No way to filter by role or distinguish human feedback from system boilerplate.

## Net Assessment

A reflection pass using autosearch added 2-3 genuinely new insights (e.g. file-layout problems, e2e testing as biggest pain area, specific skills to drop). But about 70% of the evidence cited was "50 sessions" — which is really "this topic exists in the data" rather than a meaningful frequency signal. The strongest findings came from reading actual transcripts, not from search hit counts.

## Concrete Improvements Needed

> **Status review (2026-06-08):** 4 of 5 items shipped. Verified against the
> `autosearch` CLI on `main`. Only the negative-filter (`--exclude`) item remains open.

### P0: Fix highlights — ✅ DONE

`--highlight` should return snippet text with `**` markers around matched terms. Currently returning null. Investigate whether this is a rendering bug or a search-layer issue.

**Done:** `autosearch search <query> --highlight` returns snippets with `**term**`
markers (e.g. `…starting from Scratch with the **database** migrations…`). No longer null.

### P1: Uncapped count mode — ✅ DONE

A `--count` flag (or separate mode) that returns total match counts without the 50-result cap. This enables frequency comparison between topics — the core use case for reflection.

**Done (different surface than a literal `--count` flag):** true uncapped frequency
comes from the `autosearch stats` command (`--measure count`), and every `search`
response now reports real totals in `_meta.total_hits` / `_meta.total_matches`
regardless of how many hits are returned. `search --limit` is also configurable.

### P1: Role filtering — ✅ DONE

`--role user` / `--role assistant` / `--role tool` to filter out system noise and focus on actual human feedback or agent output.

**Done:** `search` and `stats` both accept `--role user|assistant|tool|thinking`
(thinking added in task 016; excluded by default).

### P2: Structured metadata queries — ✅ DONE

Queries against tool-call fields rather than free text:

```bash
# Most-read files across sessions
autosearch stats --group-by tool_file_path --tool-name Read --measure count

# Most-used bash commands
autosearch stats --group-by bash_command --measure count

# Files edited multiple times in a session (churn indicator)
autosearch stats --group-by tool_file_path --tool-name Edit --min-count 3
```

This is a different capability from FTS — it's structured aggregation over the indexed columns. Likely belongs in a dedicated `autosearch stats` command rather than bolted onto `search`.

**Done:** the `autosearch stats` command exists with `--group-by`, `--tool-name`,
`--measure {count,distinct_sessions,distinct_messages}`, and `--min-count`. (The
original `--sort count` sketch became `--measure count`; ranking is by the measure.)

### P2: Negative filters for noise reduction — ⬜ OPEN

Ability to exclude known-noisy patterns:

```bash
# Find user corrections, excluding system rejection messages
autosearch search '"don't" OR "stop" OR "wrong"' --role user --exclude "tool use was rejected"
```

**Still open:** there is no `--exclude` flag (nor a query `NOT` operator) on `search`
as of 2026-06-08. `--role` covers some of the system-noise case, but term-level
exclusion is not yet implemented.
