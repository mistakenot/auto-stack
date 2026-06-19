---
name: borrow-from-oss
description: "Tracks open-source repos and mines their updates into a ranked YAML backlog of ideas to borrow (docs/research/opensource-ideas.yaml). Use when 'borrow from OSS', 'scan upstream repos for ideas', 'what can we borrow from <repo>', 'track <repo> for inspiration', 'investigate upstream changes', or after adding a repo to watch. Not for mining your own repo's history (use learning-diary)."
---

# Borrow From OSS

Track open-source repositories you admire and turn their progress into a ranked backlog of ideas worth borrowing. The skill maintains a living file — `docs/research/opensource-ideas.yaml`, **one per project** (in a monorepo, one per sub-project — see *Determine state*) — that records *what your project is*, *which upstream repos you watch*, and *every candidate idea* with enough structure to rank it, dedup it across runs, and act on it later.

The core loop: clone an upstream repo, look at what changed since you last investigated it, and ask one question of every change — **"does this solve a problem *we* actually have?"** Most upstream activity is noise for you (their CI tweaks, their domain quirks). The value is in the few changes that map onto your project's real needs. The profile is what lets you tell the difference.

This is the external-facing mirror of learning-diary: that skill mines *your own* history for what you learned; this one mines *other people's* repos for what you could steal.

## The file has three parts

- **`profile`** — what your project is, its architecture, and its current goals. This is the relevance filter. Established once via interview; it's what turns "a neat thing they did" into "this fixes the exact gap in our X."
- **`sources`** — the repos you watch, each with a `last_investigated_commit` watermark so re-runs only look at what's new.
- **`ideas`** — the backlog. Each idea carries labels, a score, and a lifecycle `status`, so the file is a working backlog, not a throwaway report.

## Determine state

First, **resolve which research doc you're working with** — there may be more than one.

A research doc is scoped to a *single project*: its `profile` describes one project, and its `sources`/`ideas` belong to that project. A plain repo has one doc at the root; a **monorepo has one doc per sub-project**, each co-located with the sub-project it describes:

- Single project → `docs/research/opensource-ideas.yaml` at the repo root.
- Monorepo → `<sub-project>/docs/research/opensource-ideas.yaml` (e.g. `packages/api/docs/research/opensource-ideas.yaml`), one per sub-project.

Resolve the target doc:

1. **Find existing docs.** Glob `**/docs/research/opensource-ideas.yaml` (skip `node_modules`, `.tmp`, and vendored trees).
2. **Pick the one the request is about.** Match on the sub-project the user named or implied (a path, a package name), or on the source repo they mention (find the doc whose `sources` already lists it). If exactly one doc exists and nothing points elsewhere, use it.
3. **If it's genuinely ambiguous** (several docs, no signal), ask which sub-project this is for — don't guess.
4. **If none match and this is a new project/sub-project**, it's a **first run** for that scope: pick its root (the sub-project dir the work concerns, else the repo root) and create the doc at `<scope-root>/docs/research/opensource-ideas.yaml`. Co-locate it with the sub-project — don't centralize.

Then read the resolved doc:

- **No file, or no `profile` section** → **first run**. Do the profile interview for *this* scope, then add the first source(s).
- **File exists with a profile** → skip to **investigation**. The user is either adding a new source or asking to scan existing ones for updates.

For the rest of this skill, "the doc" means the resolved file and "your project" means the scope it describes — in a monorepo, the sub-project, not the whole repo.

## First run — establish the profile

The profile is the single most important thing in this file. Without it, every upstream diff looks equally (un)interesting and the backlog fills with noise. With it, you can reject 90% of changes in one pass and spend your attention on the few that matter.

You can usually draft most of the profile yourself by reading the project — `README.md`, `CLAUDE.md`/`AGENTS.md`, the structure, and any architecture docs. In a monorepo, read the **sub-project's own** root (its `package.json`/manifest, its README, its source), scoped to its directory — the profile must describe *this* sub-project specifically, not the whole repo. Do that first, then confirm and fill gaps with the user. Cover:

1. **What the project is** — its purpose and what it produces.
2. **Architecture & stack** — how it's structured, the languages/frameworks, key conventions. Be concrete; this is what "additive vs. rebuild" judgements are measured against.
3. **Current goals** — what the user is actively trying to improve or build. Ideas that align with a stated goal rank higher.
4. **Non-goals** — things explicitly out of scope. An idea that only helps a non-goal gets filtered out, no matter how clever.

Record it as structured YAML (see format below). This happens once; later runs read it and move straight to investigation. If the project's direction shifts later, the user can ask to refresh the profile.

## Adding a source

When the user names a repo to watch (or you're seeding the first one), add an entry to `sources` with `repo`, `url`, the date, and a one-line `why` — what you hope to learn from it. Leave `last_investigated_commit` empty; that signals the next investigation is a **baseline snapshot** rather than a diff.

## Investigation

Work the repo into `.tmp/` (gitignored) so you can read files directly, not just diffs.

```bash
mkdir -p .tmp/borrow-from-oss
DIR=.tmp/borrow-from-oss/<owner>-<repo>
# First time: full clone (you need history for later diffs).
git clone https://github.com/<owner>/<repo>.git "$DIR"
# Subsequent runs: update.
git -C "$DIR" fetch --all --prune
DEFAULT=$(git -C "$DIR" rev-parse --abbrev-ref origin/HEAD)   # e.g. origin/main
HEAD_SHA=$(git -C "$DIR" rev-parse "$DEFAULT")
```

### Case A — baseline snapshot (`last_investigated_commit` is empty)

There's no "since last time" yet, and replaying a large repo's whole history is mostly noise. Instead, investigate the repo *as it stands now*: read the README and architecture docs, walk the structure, and read the substantive files that define how it works. You're building a mental model and harvesting the standout ideas that already exist — the patterns, abstractions, and capabilities that map onto your profile.

After the snapshot, set the watermark: `last_investigated_commit: <HEAD_SHA>`.

### Case B — incremental diff (`last_investigated_commit` is set)

Look only at what landed since the watermark.

```bash
LAST=<last_investigated_commit>
git -C "$DIR" log "$LAST".."$HEAD_SHA" --no-merges --pretty=format:'%H|%ai|%s'
git -C "$DIR" diff --stat "$LAST".."$HEAD_SHA"
```

Read commit messages and the `--stat` churn to find changes worth a deep look — new files, large additions, commits whose messages describe a new capability or a deliberate redesign. Skip version bumps, formatting, dependency lockfile churn, and changes confined to their domain-specific quirks.

For each promising change, **read the full file at the new version**, not just the diff hunk — `git -C "$DIR" show "$HEAD_SHA":<path>`. Diffs show *what moved*; the full file shows the *pattern*, which is what you're actually borrowing.

When many files changed, consider spawning parallel sub-agents to investigate different files/areas in depth and report back candidate ideas — this keeps a large diff tractable and is faster than reading serially.

### The relevance test (both cases)

For every candidate, run it through the profile before it earns a spot in the backlog:

- **Do we have this problem?** If it solves something the profile says we don't care about (a non-goal, or a subsystem we don't have), drop it.
- **Could we actually apply it?** An idea you can't map onto a real file or subsystem in *our* project is a daydream, not a backlog item. If you can't name where it would go, it's probably not ready.
- **Is it already covered?** Check existing `ideas` (and your own project — the scope the doc describes) — don't resurface something already implemented, rejected, or sitting in the backlog. If a new commit *extends* an existing idea, update that idea instead of adding a duplicate.

When in doubt, keep it but mark `confidence: low` — a slightly generous backlog the user can prune beats silently dropping something they'd have wanted.

## Labelling and scoring

Every idea gets labelled so the user can scan and rank the backlog at a glance. The labels also force you to think about *cost of adoption*, which is the whole point of "what would it take to borrow this."

- **`change_type`** — the most important label. How disruptive is adopting it?
  - `additive` — a new capability that slots in without touching existing code. Low risk.
  - `structural-rebuild` — requires reworking something we already have. Note what gets rebuilt.
  - `replacement` — swaps out an existing approach wholesale. Highest blast radius.
- **`area`** — which of *our* subsystems it touches (name it from the profile's architecture).
- **`risk`** — maturity/maintenance risk: is it battle-tested in their repo or experimental? Does it add a heavy dependency?
- **`confidence`** — how sure you are it applies to us.
- **`dependencies`** — new deps/infra it would pull in, or `none`.

Two of the labels feed the score, so anchor them to concrete definitions — vague reads are why the *same* idea can score differently across runs:

- **`effort`** — size of the change *for us*. `S` = a few edits to one file or a doc/CLAUDE.md rule (≈ under an hour). `M` = a new file or a change spanning a few files (≈ an afternoon). `L` = a new subsystem, or a change touching many files or the build (≈ days).
- **`impact`** — `high` only when you can name the **stated profile goal it serves** or the recurring pain it removes. `medium` = clearly improves one workflow we care about, but no stated goal rides on it. `low` = nice-to-have with narrow benefit. If you can't name the goal or pain, it isn't `high`.

**Score (impact-to-effort, 1–10).** The user wants quick wins surfaced first, so reward value relative to cost:

```
base   = {low:1, medium:2, high:3}[impact]  ÷  {S:1, M:2, L:3}[effort]   → 0.33 … 3.0
score  = round(base × 3)                                                  → ~1 … 9
adjust  +1 if it directly serves a stated profile goal
        −1 if risk is high OR confidence is low
clamp to 1 … 10
```

Worked examples (use these to calibrate):
- `additive`, `effort: S`, `impact: high`, serves a stated goal → 3 ÷ 1 = 3.0 → 9, **+1 → 10**.
- `additive`, `effort: M`, `impact: high`, no specific goal → 3 ÷ 2 = 1.5 → **5** (not 9 — `M` not `S`, no goal bonus).
- `structural-rebuild`, `effort: L`, `impact: low` → 1 ÷ 3 = 0.33 → 1 → **1**.

The number is just for sorting — don't over-engineer it. But do apply the anchors honestly: an idea you scored `high`/`S` should genuinely be a stated-goal win you could ship in an hour.

## Recommend the top 5

Rank all `candidate`-status ideas by `score` (break ties by lower effort, then higher confidence). Take the top 5 and **set their `status: shortlisted` in the YAML now, before you present anything.** This is your own ranking output, not a decision that waits on the user — `shortlisted` just means "this made the top cut," and it commits you to nothing. Don't leave the top picks as `candidate` "for the user to decide"; that conflates `shortlisted` (which you set) with `accepted` (which the user sets). If you wrote a top-5 in the summary, those exact five must read `status: shortlisted` in the file.

Then present them — each as: title, the one-line application to *our* project, change_type, effort, impact, and the source permalink — and briefly note why each beat the rest. Finally, ask the user which (if any) to act on: promote chosen ones to `status: accepted`, and leave the rest `shortlisted` for later.

## Idea lifecycle

Status flows: `candidate` → `shortlisted` → `accepted` → `implemented`. Or sideways to `rejected` / `deferred`. Two of these transitions are **yours to make automatically**: a fresh idea starts `candidate`, and the top-5 of each run become `shortlisted`. Only `accepted` is a human gate — never promote past `shortlisted` on your own. The payoff: re-runs **never resurface** ideas that are `implemented` or `rejected`, and the user can see at a glance what made the cut versus what they actually chose. When an idea is rejected, record *why* in `notes` so the reasoning survives.

## File format

The full YAML schema — `profile`, `sources` (with `investigations`), and `ideas`
(with every label field) — is in [references/file-format.md](references/file-format.md).
Read it before writing the doc the first time. Each doc holds one project's profile,
its sources, and its own I-series of ideas.

## Writing the file

- Write to the **resolved doc path** (the repo-root doc, or the sub-project's co-located one), creating `docs/research/` under that scope if needed. Ideas live in the same doc as the profile they were filtered against — never write a sub-project's ideas into another scope's doc.
- Use the `Write` tool so the user can review the diff — don't shell out to edit YAML.
- **Preserve everything.** Append new ideas (newest first); update the watermark and `investigations`; never rewrite or drop existing ideas or their statuses unless the user asks.
- Assign idea IDs sequentially from the highest existing one *in this doc* (each doc has its own I-series).

## After investigating

Print a short summary:
- Which source(s) you investigated, and the commit range (or "baseline snapshot").
- How many changes you examined, how many candidates survived the relevance test.
- The top-5 shortlist, ranked.

If nothing survived, say so plainly — a quiet upstream week is a valid result, not a failure.
