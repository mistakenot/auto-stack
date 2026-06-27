---
name: skill-reviewer
description: "Review all compiled skills against multi-agent best practices and produce a prioritized report. Use when the user asks to 'review skills', 'audit skills', 'check skill quality', 'lint skills', or wants a quality report on the skills in this repo."
---

# Skill Reviewer

Compile skills, lint them, review against best practices from Claude/Codex/OpenCode, and produce a prioritized report.

## Process

### Step 1: Compile fresh skills

```bash
python src/compile.py
```

Abort if compilation fails -- fix errors first.

### Step 2: Run auto skill lint

```bash
auto skill lint
```

Capture the output for the report.

### Step 3: Fetch best practices

Fetch these URLs and extract the key rules and checklists from each:

- https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices.md
- https://developers.openai.com/codex/learn/best-practices.md
- https://opencode.ai/docs/skills.md

These are the authoritative sources. Always fetch fresh -- do not rely on cached or bundled copies.

### Step 4: Review each skill in parallel

Spawn one subagent per skill directory in `./skills/`. Pass each subagent the best practices content fetched in Step 3.

Each subagent:

1. Read the compiled `SKILL.md` and any files in `references/`
2. Check against every applicable rule from all three best practice sources
3. Return a list of issues, each with:
   - **Priority**: P1 (violates a hard requirement), P2 (violates a strong recommendation), P3 (minor improvement)
   - **Rule source**: which best practice doc the rule comes from
   - **Issue**: what's wrong
   - **Suggestion**: how to fix it

### Step 5: Produce report

Combine all subagent results into a single markdown report. Save to `./docs/reviews/YYYY-MM-DD.md`.

Report structure:

```markdown
# Skill Review — YYYY-MM-DD

## Summary

| Skill | P1 | P2 | P3 |
|-------|----|----|-----|

## Autoskill Lint Output

[paste lint output]

## Issues by Skill

### skill-name

#### P1 — Blocking
- **[rule-source]** issue description → suggestion

#### P2 — Important
- **[rule-source]** issue description → suggestion

#### P3 — Minor
- **[rule-source]** issue description → suggestion
```

## Priority Definitions

- **P1**: Violates a hard requirement (missing frontmatter fields, name format invalid, description empty/vague, skill exceeds 500-line body limit, broken file references)
- **P2**: Violates a strong recommendation (second-person writing, no trigger phrases in description, content that should be in references, inconsistent terminology, no verification steps in workflows)
- **P3**: Minor improvement (could be more concise, naming convention suggestions, missing examples, could benefit from progressive disclosure)
