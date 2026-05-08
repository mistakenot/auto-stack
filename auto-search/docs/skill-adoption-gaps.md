---
hash: "03b2e92e"
id: "skill-adoption-gaps"
read_when: "analyzing skill usage patterns, improving skill adoption tracking, or extending ETL pipeline for skill metadata"
summary: "Investigation into autosearch's ability to answer skill adoption questions, with identified gaps in ETL pipeline for trigger detection and available-skills baseline."
title: "Skill Adoption Analysis: Gaps and Improvements"
---

# Skill Adoption Analysis: Gaps and Improvements

Investigation into whether autosearch can answer: **"Are skills being appropriately picked up and used by coding agents?"**

## What works today

### Data model
- `skill_name` is a structured field on every message row (not just text in content)
- Extracted during ETL from `tool_use` blocks where `block.Name == "Skill"`, reading `input.skill`
- Indexed in SQLite with `idx_messages_skill_name` for fast filtering
- Both `tool_use` (role=assistant) and `tool_result` (role=tool) messages carry the skill name

### Queries that work
```bash
# Flat skill usage counts (now with filters)
autosearch skills --since 7d
autosearch skills --cwd /home/vscode/src/my-project

# Skill usage ranked by count (NEW: group-by skill_name)
autosearch stats --scope messages --group-by skill_name --since 30d

# Time trend for a specific skill
autosearch stats --scope messages --group-by day --skill contextual-commit --since 14d

# Which workspaces use a skill
autosearch stats --scope messages --group-by workspace --skill release

# Filter search results to a specific skill
autosearch search "error" --skill contextual-commit
```

## Gaps identified

### Gap 1: No user-triggered vs agent-triggered distinction (ETL-level)

**Impact: High** — directly blocks answering "appropriately picked up"

All Skill invocations are stored as `role=assistant`. There's no field distinguishing:
- User typed `/contextual-commit` explicitly
- Agent saw the SKILL.md trigger condition and invoked it proactively
- Agent was instructed by a CLAUDE.md rule to invoke it

This is the most important gap. Without it, we can't measure *proactive adoption* — we can only count total invocations.

**Possible fix:** In ETL, examine the preceding user message for `/skill-name` patterns. If the user message contains a slash-command matching the skill name, mark it as `user_triggered`. Otherwise mark as `agent_triggered`. This heuristic isn't perfect but covers the common case.

**Data model change:** Add `skill_trigger` field to `AgentMessage` with values: `user`, `agent`, or empty.

### Gap 2: No "available skills" baseline (beyond autosearch scope)

**Impact: Medium** — blocks computing adoption rates

To answer "are skills being picked up," you need to know what skills were *available* in each session. Currently there's no record of which SKILL.md files were loaded into the agent's system prompt.

Without this, we can compute absolute counts but not adoption rates (used / available).

**Possible fix:** During ETL, extract skill names from system-reminder blocks that list available skills. These blocks follow a consistent format:
```
The following skills are available for use with the Skill tool:
- skill-name: description...
```

**Data model change:** Add `available_skills` field to `AgentSession` (comma-separated list of skill names available in that session).

### Gap 3: No session-level skill aggregation

**Impact: Low** — queries work via subquery joins, just slower

Sessions don't track which skills were used. Filtering sessions by skill requires a subquery join through the messages table. For the current data scale (~50K messages) this is fast enough, but it means `group-by skill` isn't available in session-scope stats.

**Possible fix:** Add `skills_used` column to the sessions table during indexing, populated by aggregating distinct `skill_name` values from the session's messages.

### Gap 4: Skill invocation context is lost

**Impact: Medium** — blocks understanding *why* skills are invoked

When a skill is invoked, we store the skill name but not the args or the context that triggered it. For example, `contextual-commit` might be invoked after a bug fix vs. after a feature — knowing which matters for understanding adoption patterns.

The `tool_input` field contains `{"skill":"contextual-commit","args":"..."}` but `args` isn't extracted into a dedicated field. This makes it harder to analyze skill usage patterns by argument.

**Possible fix:** Extract `skill_args` as a separate field during ETL.

## Priority order for fixes

1. **User vs agent trigger detection** (ETL) — highest signal for adoption analysis
2. **Available skills extraction** (ETL) — enables adoption rate computation
3. **Skill args extraction** (ETL) — enriches pattern analysis
4. **Session-level skill aggregation** (autosearch indexer) — performance optimization, low priority
