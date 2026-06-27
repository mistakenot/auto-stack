---
name: task-feedback-analyser
description: "Extracts recurring patterns from completed task feedback into generalizable workflow rules. Scans feedback.md files and review threads, clusters by theme, and drafts rules with a strict 3-example minimum. Use when 'analyse feedback', 'extract rules', 'find patterns in feedback', or when reviewing historical task outcomes."
---

# Task Feedback Analyser

Extract recurring patterns from completed task feedback into generalizable workflow rules.

> Part of the task planning workflow. See [references/workflow-overview.md](references/workflow-overview.md) for the full pipeline.

## Process

### Step 1: Scan feedback sources

- Read all `docs/tasks/*/feedback.md` files
- Read review comment threads from completed PRs (merged)
- Track which files have been processed to avoid re-scanning (check against existing rules file for "Evidence" task references)

### Step 2: Cluster by theme

Group observations by theme. Examples of themes:

- Missing tenant isolation
- Stale context references
- Tests that don't verify actual behavior
- Incomplete error handling in server functions

### Step 3: Filter to 3+ independent examples

A theme qualifies as a rule only when it appears in **3 or more independent examples** (different task folders). This minimum is strict -- never fabricate or stretch examples to meet the threshold.

Drop themes that don't meet the bar. Report dropped themes so the user knows they exist but aren't mature enough.

### Step 4: Draft rules

Write each rule in imperative form with a `use_when` trigger and verbatim evidence:

See [references/template-rule.md](references/template-rule.md) for the rule template format.

### Step 5: Update rules file

- Read existing `docs/rules.md` (or create if it doesn't exist)
- Merge new rules with existing ones -- do not duplicate
- Assign sequential IDs continuing from the last rule
- Commit: `docs: add rules RULE-$NNN through RULE-$MMM from feedback analysis`

## Rules

- 3-example minimum is strict. Never fabricate examples.
- Use verbatim excerpts, not paraphrases.
- Track processed files to avoid re-scanning on subsequent runs.
- Report themes that fell below the 3-example threshold so the user can watch for them.
