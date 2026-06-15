---
name: learning-diary
description: "Mine git history, PRs, and session transcripts for novel techniques, ideas, and breakthroughs, then maintain a structured learning diary in docs/learnings.yaml. Use when 'learning diary', 'mine learnings', 'what did I learn', 'update learnings', 'track learnings', 'learning log', or when the user wants to capture what they've learned from recent work. Not for content creation or blog post writing (separate pipeline)."
---

# Learning Diary

Mine a repository's history for novel techniques, ideas, and breakthroughs. Maintain a structured YAML diary at `docs/learnings.yaml` that captures each learning as a self-contained entry a reader can understand without external context.

The diary has three sections:
- **Baseline** — the user's existing expertise, established on first run via interview. This is the filter: only record learnings that push beyond this frontier.
- **Mining metadata** — which commit ranges have been mined, preventing duplicate work.
- **Learnings** — the entries themselves.

## Determine state

Read `docs/learnings.yaml`. If it doesn't exist or has no `baseline` section, this is a **first run**. If it exists with a baseline and mining metadata, this is an **incremental run**.

## First run — establish baseline

The baseline interview matters because it determines what counts as "novel" for this specific user. A technique obvious to a senior distributed-systems engineer might be a revelation for someone coming from frontend. Without a baseline, the diary fills with noise.

Interview the user with these questions (adapt phrasing to be conversational — these are the topics to cover, not a script):

1. **Role and experience level** — What do you do? How long have you been doing it?
2. **Core domains** — What areas are you deeply comfortable in? (e.g., distributed systems, frontend, ML, DevOps)
3. **Languages and tools** — What's your daily stack?
4. **AI/agent experience** — How long have you been using AI coding tools? What's your comfort level?
5. **Current learning edge** — What are you actively trying to get better at? What feels new or unfamiliar?
6. **Known techniques** — Are there specific techniques or patterns you'd consider "already in your toolkit"? (This helps avoid recording things like "use git rebase" for someone who's been using git for a decade.)

Record the baseline as structured YAML (see format below). This only happens once — subsequent runs skip straight to mining.

After the baseline interview, ask the user for the **commit range or date range** to mine for this first run.

## Incremental run

Read `mining.last_commit` and `mining.ranges_mined` from the existing file. Mine from the last commit to HEAD. If there are no new commits, say so and stop.

Before starting, show the user: "Mining from `<last_commit_short>` to HEAD (`N` commits). Proceed?"

## Mining process

Mine these sources in parallel where possible. Git history is always available; the others are optional — use them when the tools exist, skip gracefully when they don't.

### 1. Git history (required)

```bash
git log <from>..<to> --pretty=format:'%H|%ai|%s' --no-merges
```

Read commit messages and diffs for commits that look like they involve something non-trivial. Skip routine changes (version bumps, typo fixes, dependency updates, formatting). Look for:
- Commits with detailed messages explaining *why* something was done a certain way
- Large refactors that changed approach
- Commits that introduce new tools, patterns, or techniques
- Bug fixes where the root cause was surprising
- **Story arcs** — sequences of commits where an approach was tried, hit a wall, and was replaced with something better. These are the richest learnings because they capture the *journey*, not just the destination. Look for revert-then-redo patterns, commits that undo recent work, or messages like "actually", "better approach", "turns out".

For promising commits, read the full diff to understand the technique.

### 2. Changed files — read the content, not just the diff (required)

Git diffs show *what changed* but not the full picture. For commits that add or significantly modify substantive files, **read the full file** — not just the diff hunk. Novel patterns hide everywhere: in research diaries, in skill instructions, in harness scripts, in component libraries, in config files. A diff showing "+200 lines" is a signal to read the whole file.

```bash
git diff --name-only <from>..<to>
```

Look at everything — docs, skill files (SKILL.md), test harnesses, scripts, code, config, component libraries. Don't filter by path or extension. For each substantive new or heavily-modified file, read it in full and look for:
- Original frameworks, models, or taxonomies the user created
- Design decisions with explicit tradeoff analysis
- Spike reports with empirical findings
- Technique catalogs or pattern libraries
- Novel architectural patterns in skill instructions or tool code
- Reusable patterns embedded in implementation (e.g., an agent-friendly output format, a reference-file convention, an orchestration pattern)

### 3. Pull requests (if `gh` is available)

Check if `gh` is available: `gh --version`. If not, skip this step.

```bash
gh pr list --state merged --search "merged:>=<from_date>" --json number,title,body,mergedAt --limit 50
```

PR descriptions often contain the richest context — the problem statement, alternatives considered, and the reasoning behind the chosen approach. These are gold for learning entries.

### 4. Session transcripts (if `auto search` is available)

Check if `auto search` is available: `auto search --help`. If not, skip this step. If available, refresh the index first: `auto search index`.

```bash
auto search search "<relevant_query>" --cwd <repo_path> --since <timeframe> --role assistant --limit 20
```

Search for sessions in this workspace that overlap with the mined time period. Session transcripts capture the *process* — the dead ends, the "aha" moments, the corrections. These add depth that git history alone can't provide.

Useful queries to try:
- Search for key terms from interesting commits
- `auto search stats --group-by skill --cwd <repo_path> --since <timeframe>` to see which skills were used (novel skill usage = potential learning)
- `auto search search "" --cwd <repo_path> --since <timeframe> --role user` to see what the user was asking about (questions reveal learning edges)

### Synthesis and filtering

For each candidate learning, apply these filters — but note that the threshold shifts depending on how close the topic is to the user's stated frontier:

- **Is this beyond the baseline?** If the user listed "TypeScript" as a core language and the technique is basic TS generics, skip it. But if the topic touches the user's `frontier` (their stated learning edge), set a **low bar** — these are the entries the user cares about most, even if the technique seems incremental.
- **Is this original work?** If the user created a new framework, model, taxonomy, or architectural pattern — something that didn't exist before they made it — it is *always* worth recording. Original intellectual work is frontier-pushing by definition. Look for these in research diaries, design docs, and planning artifacts.
- **Is this self-contained?** Could someone read this entry and understand the technique without reading the full codebase?
- **Is there a real insight here?** "Used library X" is not a learning. "Used library X in an unconventional way to solve Y because Z" is.
- **Is this a duplicate?** Check existing entries in the diary for overlap.

Only filter out things that are clearly routine for the user's stated experience level (standard git usage for a git expert, basic CRUD patterns for a senior backend engineer, etc.). When in doubt about whether something is novel enough, **include it** — a slightly generous diary is more useful than one that filters out insights the user would have wanted to remember.

## Entry format

Each learning entry has these fields:

```yaml
- id: L001  # sequential, zero-padded to 3 digits
  title: "Short, descriptive title of the technique or insight"
  date_discovered: 2026-06-10  # when the technique was first used (from commit date)
  date_recorded: 2026-06-13    # when this entry was mined
  category: workflow  # see categories below
  commits:
    - abc123def
  prs:
    - 42
  related_learnings:
    - L003  # cross-reference to related entries
  problem: |
    What situation or challenge prompted this technique.
    Include enough domain context that a reader who has never
    seen this codebase understands the problem space and why
    it matters. Don't assume the reader was there.
  solution: |
    What was actually done — the technique, pattern, or insight.
    Be concrete: name the tools, show the command, describe the
    pattern. Specific enough that someone could apply this in
    their own project without reading the original code.
  evidence: |
    Where this appears in the codebase. Quote the key lines of
    code, commit messages, or session excerpts that demonstrate
    the technique. This anchors the entry in reality and lets
    the user reconstruct the full story later if needed.
  takeaway: |
    The generalizable lesson — the part that transfers beyond
    this project. What would you tell a colleague who faces a
    similar situation? Frame it as portable advice, not project
    trivia.
```

### Categories

Use these categories (add new ones if none fit, but prefer existing):
- `architecture` — system design, patterns, structural decisions
- `ai-prompting` — prompt engineering, skill design, agent workflows
- `debugging` — diagnosis techniques, surprising root causes
- `testing` — test strategies, eval approaches, quality assurance
- `tooling` — tool usage, CLI tricks, dev environment
- `workflow` — process improvements, automation, collaboration patterns
- `performance` — optimization techniques, profiling insights
- `infrastructure` — deployment, CI/CD, environment management

### Cross-referencing

When a new entry builds on, refines, or contradicts an earlier one, add its ID to `related_learnings`. This creates learning trajectories — showing how understanding evolved over time.

## Full file format

```yaml
baseline:
  recorded: 2026-06-13
  summary: |
    Free-text summary of the user's background and expertise.
  domains:
    - distributed systems
    - frontend
  languages:
    - TypeScript
    - Python
  tools:
    - Claude Code
    - VS Code
  ai_experience: |
    Free-text description of AI/agent tool experience.
  frontier: |
    What the user is currently learning or finds challenging.
    This is the most important filter — things near this
    edge are the most valuable to record.

mining:
  last_run: 2026-06-13T10:30:00Z
  last_commit: abc123def456
  ranges_mined:
    - from_commit: abc123
      to_commit: def456
      from_date: 2026-05-01
      to_date: 2026-06-13
      entries_added: 5
      mined_at: 2026-06-13T10:30:00Z

learnings:
  # entries here, newest first
```

## Writing the file

- On first run, create the full file with baseline, mining metadata, and initial entries.
- On incremental runs, read the existing file, append new entries to the `learnings` list (newest first), update `mining.last_commit`, `mining.last_run`, and append to `mining.ranges_mined`.
- Assign IDs sequentially from the highest existing ID.
- Preserve all existing content — never remove or modify existing entries unless the user explicitly asks.
- Write with `Write` tool, not by shelling out, so the user can review the diff.

## After mining

Print a summary:
- How many commits/PRs/sessions were examined
- How many candidate learnings were found
- How many passed the baseline filter
- List the titles of entries added

If zero learnings were found, that's fine — say so. Not every commit range produces novel insights.
