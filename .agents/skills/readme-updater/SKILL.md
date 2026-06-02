---
name: readme-updater
description: >-
  Bring a project's README up to date with the current state of the codebase.
  Audits the existing README for stale commands, dead links, renamed tools, and
  drifted status claims; surveys what's changed since the README was last
  touched; rewrites it with verified facts; then commits and pushes. Use this
  skill whenever the user says "update the README", "the readme is stale",
  "refresh the README", "rewrite README.md", "add new features to the readme",
  or otherwise asks for documentation that reflects the current state of the
  project. Also trigger when a README clearly hasn't kept pace with the
  codebase (e.g. recent feature commits not reflected, broken examples).
license: MIT
---

# README Updater

Bring a stale README back into sync with reality, end to end, in one pass.
The skill runs four phases sequentially without pausing for human input,
then commits and pushes the result.

## When this skill triggers

- "update the README", "the README is out of date", "refresh README.md"
- "the README doesn't mention <feature>"
- "rewrite the README to cover what we've shipped"
- Any request to bring docs in line with the current state of the code

It does **not** trigger for:
- Small spot-edits to a README (just edit directly)
- Drafting a brand-new README from scratch in an empty repo (different problem)
- Generating per-package docs (use a docgen tool)

## The most important lesson

**Never trust your memory or the existing README for CLI claims.**
On every prior run, the model has shipped at least one snippet referencing
a flag that no longer exists or a subcommand that was renamed. The
verification phase below is non-negotiable — it is the one step that
distinguishes a useful README update from a confidence-damaging one.

## Phase 1 — Scope

Find when the README was last meaningfully updated and what's changed
since. This bounds the work.

```bash
# Last commits that touched README.md (not just chore renames)
git log --oneline -20 README.md

# Pick the most recent substantive update commit — call it $LAST_README_SHA
git log --oneline $LAST_README_SHA..HEAD | wc -l   # how many commits since
git log --oneline $LAST_README_SHA..HEAD           # the actual list
```

If the README has never been written (empty repo or new file), there is no
prior state to audit — skip Phase 2 and treat Phase 3 as a fresh inventory.

## Phase 2 — Audit (inconsistency check)

Before drafting anything new, scan the **existing** README for claims that
no longer match reality. This is faster than rewriting from scratch and
catches drift the user may not know about.

Run these checks. Don't pause for user input — log findings inline and
carry them into Phase 4.

### 2.1 CLI commands and flags

For every code block in the README that looks like a CLI invocation
(`<binary> <subcommand> --flag value`), check that the binary, subcommand,
and flag still exist.

```bash
# Get the real command tree
<binary> --help
<binary> <subcommand> --help
```

Flag the snippet as **stale** if any of:
- The binary doesn't exist (renamed, removed)
- The subcommand is renamed (e.g. `cochange` → `co-change`)
- The flag is renamed, removed, or moved to a different subcommand
- The default value drifted

### 2.2 File paths and directory layouts

For every `path/to/file` or `dir/` mentioned, verify it exists:

```bash
ls path/to/file 2>/dev/null || echo "STALE: path/to/file"
```

### 2.3 Tool / package inventory

If the README lists tools, packages, or sub-projects in a table, compare
that list to what the repo actually contains. New tools added since the
last README touch are the most common source of staleness.

```bash
# For monorepos with discoverable sub-projects
ls -d <prefix>-*/  # e.g. auto-*/, packages/*, apps/*
```

Flag missing entries (tool exists in repo, not in README) and dead entries
(README lists tool, repo doesn't have it).

### 2.4 Naming consistency

Skim the README for inconsistent naming of the same thing:
- `auto-eval` vs `autoeval` vs `auto_eval`
- `myproject-cli` vs `myproject` vs `mp`

Pick one canonical name (usually the binary name) and standardise.

### 2.5 Status claims

If the README labels things as "Active", "Beta", "Coming Soon", "Deprecated":
- Has a "Coming Soon" tool now shipped? → Promote it.
- Has an "Active" tool been removed? → Demote or delete.
- Does the "Beta" claim still hold given recent stability work?

### 2.6 External references

- Install URLs (`curl … | bash`) — fetch the URL or confirm the repo path.
- Badge URLs — fetch and confirm they render.
- Cross-links to other docs — confirm targets exist.
- Version numbers (`Go 1.22+`, `Node 18+`) — confirm against `go.mod` /
  `package.json` / CI config.

Keep findings in a short scratch list (`audit_findings`) — you'll address
them in Phase 4.

## Phase 3 — Survey

Dispatch parallel research agents to inventory the current state. Run them
concurrently — this is the longest phase and the agents are independent.

### Agent A — Project / tool inventory

For monorepos, the inventory is per-sub-project. For single-project repos,
substitute "the project" for "each sub-project".

Prompt template:

> For each directory below, read its README/CLAUDE.md, list the binary's
> available commands (look at main.go / cmd/ / package.json scripts /
> cobra command tree), and identify whether it is fully implemented or
> scaffolded.
>
> For each, report under 100 words:
> - One-line description
> - Status: Active / Working / Scaffolded
> - Binary name
> - Top-level commands
> - Notable recent features
>
> Total report under 1500 words.

### Agent B — Feature mining from git history

Prompt template:

> The README was last updated at commit $LAST_README_SHA. Run
> `git log --oneline $LAST_README_SHA..HEAD` and for each `feat:` commit
> summarise the user-visible feature added. Group by tool/area. For each
> feature give: tool, feature name, one-sentence description, commit hash.
> Focus on user-facing features, not refactors or CI changes.
> Under 1000 words.

Sending both agents in **one message** (parallel tool calls) is important —
they run concurrently and you get both reports back before Phase 4 starts.

## Phase 4 — Draft

Rewrite the README using the audit findings (Phase 2) and survey reports
(Phase 3). Some structural defaults that have proven to work:

### Diagrams

Replace ASCII diagrams with **Mermaid**. GitHub renders Mermaid natively in
the web UI, in PRs, and in issues. Mermaid is text-based, diff-friendly,
and requires no build step. Use `flowchart LR` for pipelines and
`flowchart TD` for data architecture.

Other options considered (in case the user asks):
- Checked-in SVG / PNG → high quality but binary, hard to diff, needs a
  build step or an external editor.
- D2 → newer than Mermaid, less broadly rendered.
- ASCII → diff-friendly but ugly. Only use if the user specifically asks.

Style classes give a polished look:

```mermaid
flowchart LR
    A[Foo] --> B[Bar]
    classDef active fill:#1f6feb,stroke:#1f6feb,color:#fff;
    class A,B active;
```

### Status badges

For status-of-tool tables, use [shields.io](https://shields.io) badges:

```
![Active](https://img.shields.io/badge/status-active-brightgreen)
![Coming Soon](https://img.shields.io/badge/status-coming%20soon-yellow)
![Beta](https://img.shields.io/badge/status-beta-blue)
![Deprecated](https://img.shields.io/badge/status-deprecated-red)
```

Badges read at a glance and survive markdown rendering everywhere.

### Tool tables

Order rows by **role in the pipeline / system**, not alphabetically.
Foundation / input tools first, derived tools in the middle, infra and
orchestration last, "coming soon" at the bottom. Add a one-line lede above
the table explaining the ordering, so readers know the order is
intentional.

### Quick Start

If the project has a non-trivial workflow, write a numbered "Quick Start:
The Full Loop" section. Each step should be runnable as-is, paired with
one or two lines of explanation. The goal: someone copies the entire
block, runs it, sees the system end-to-end.

### Feature Highlights

A bulleted section grouped by domain (e.g. "Ingest & analytics",
"Search & discovery", "Code context", "Documentation"). Pulls from the
git-history mining agent's report. Each bullet is one sentence, name in
bold.

### Writing rules

These rules apply to every change you make in the README. They keep the
prose feeling consistent and prevent the document from drifting into a
changelog.

- **Present tense, current state only.** Describe the system as it is —
  as if it had always been this way. Never say "we added X", "X is now
  Y", "new in v2", "now supports Z", or anything that implies a
  before/after. The git log carries the change history; the README
  carries the current state. This is the single most common drift
  pattern and the easiest to catch yourself doing.
- **Match the existing tone and style.** If the README uses terse bullet
  points, keep that style. If it uses full paragraphs, match that. Don't
  impose a new voice mid-document.
- **Preserve existing structure where it works.** Add new sections or
  entries in the logical place within the existing document. Don't
  reorganise unless the current structure genuinely can't accommodate
  the new content. Other docs and external sites may link to existing
  heading anchors.
- **Be specific.** Use actual command names, flag names, file paths, and
  concrete examples — not vague descriptions ("supports several
  options"). Specificity is what makes a README useful as a reference,
  not just an introduction.
- **Don't pad.** If a feature is simple, a one-liner is fine. Don't
  inflate descriptions to seem thorough. Short and accurate beats long
  and hedging.

### What to keep

Sections worth preserving by default: Install, Quick Start, Data
Architecture, Configuration, Development, License. Don't rewrite for
the sake of it.

## Phase 5 — Verify (non-negotiable)

For **every CLI snippet** in the new draft, run the actual command's
`--help` output and confirm the binary, subcommand, and every flag is
spelt correctly and accepts the value type shown.

```bash
# For each snippet in the README, verify:
<binary> --help
<binary> <subcommand> --help

# If the snippet uses --flag value, confirm --flag exists and accepts that type.
```

A short, automatable pattern: grep the README for lines starting with
`<binary> ` inside code blocks, then for each one diff against
`<binary> <subcommand> --help`. Doing this catches roughly five wrong
flags per rewrite on real codebases.

If a verification fails:
- The intended feature exists but the flag is named differently → fix the
  snippet.
- The intended feature doesn't exist at all → cut the snippet, pick a
  feature that does work, and document that instead. Don't aspirational-document.

Also verify any URLs, file paths, version numbers, and `make` targets
quoted in the README still resolve.

## Phase 6 — Commit and push

Stage only the README (and any directly related doc updates the audit
turned up — e.g. a renamed file the README pointed at). Don't sweep in
unrelated working-tree changes.

```bash
git add README.md
```

Write a commit message that names what was done and why. The format used
on the auto-stack repo:

```
docs(readme): <one-line subject under 70 chars>

<two or three paragraphs explaining what changed and why, calling out the
biggest categories of update (new tools, restructured sections, verified
CLI snippets, replaced ASCII with Mermaid, etc.).>

Co-Authored-By: …
```

Then push:

```bash
git push origin <current-branch>
```

If the repo has a pre-commit hook that runs format / vet / lint, let it
run. If it fails, fix the underlying issue rather than passing
`--no-verify`.

## Communicating with the user

Brief updates per phase, not a running monologue:

- Start of Phase 2: "Auditing the current README — checking commands,
  paths, status claims."
- Start of Phase 3: "Dispatching survey agents in parallel — project
  inventory and git-history feature mining."
- Start of Phase 4: "Drafting the new README."
- Start of Phase 5: "Verifying every CLI snippet against --help."
- End: "Pushed as <sha>. Summary of what changed: …"

If the verification phase catches the model contradicting itself
(documenting a flag that doesn't exist), say so explicitly in the final
report — it builds trust and helps the user spot the same kind of error
the next time around.

## Common pitfalls

1. **Memorised flags.** "I'm sure `--bash-exit-code` exists." Don't
   trust this. Run `--help`.
2. **Inventing aspirational features.** If a tool is scaffolded, label
   it "Coming Soon" — don't write a Quick Start for it.
3. **Skipping the audit because the rewrite "covers it anyway".**
   The audit catches things you'd otherwise carry forward unchanged
   (renamed file paths, dead install URLs, naming drift).
4. **Sweeping unrelated changes into the commit.** Stage explicitly,
   don't `git add -A`.
5. **Breaking existing anchors.** If other docs or external sites link
   to `#section-name` in the README, preserve the heading or leave a
   redirect-style link.

## Why phases run without pausing

The user typically already trusts the workflow when they invoke this
skill. Pausing for confirmation at the end of each phase wastes a turn.
The verification phase (5) is the safety net — it catches almost every
mistake before the commit lands. If something genuinely needs human
judgement (e.g. a tool's status is ambiguous), surface it in the final
summary so the user can amend, rather than blocking the whole flow.
