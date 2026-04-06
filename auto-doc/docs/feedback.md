---
hash: "2e8d1de6"
id: "8fad0ddc"
summary: "Agent feedback loops for tracking which docs were useful during tasks"
title: "Feedback"
---

# Feedback feature

We want to build feedback loops into our coding agents so that our coding agents can provide information on what parts of our documentation were useful and what parts were not useful. This will follow the agents context engineering type workflow. How it will work in general is that after a coding agent has completed a task, it will be asked to mark the feedback docs to mark the lines in the feedback docs that were useful to its implementation work as well as the ones that were not useful. This meta information will then be stored in the repository, not in the docs file, but in a separate JSON log file where we will keep all these bits of feedback so that a process later on down the line can go through them, tidy up our documentation and make it more useful.

Inspired by https://arxiv.org/html/2510.04618v2

## Signals

To build a feedback system, we need to gather signals from our system. We can presume that our system has:
- Plan files, describing work to be done.
- Context links included, either as part of the plan files or in a seperate context file, linking to docs in our codebase.
- Transcripts. Message histories from AI coding sessions. These sessions usually start by a. reading the plan + context links b. reading docs / exploring the code base c. executing work d. verifying / correcting work as we go.

**List of all possible signals**

### Doc quality signals (from transcript analysis)

- **Useful guidance**: Agent read doc, followed it, task succeeded. Doc is working as intended.
- **Misleading guidance**: Agent followed doc guidance but it led to errors, backtracking, or incorrect implementation.
- **Contradictory docs**: Agent read doc A, then doc B, and had to reconcile conflicting guidance. Transcript shows hesitation, backtracking, or explicit "this conflicts with..." reasoning.
- **Ignored guidance**: Agent read doc, then did something different and succeeded. Doc may be outdated, wrong, or irrelevant.
- **Incomplete guidance**: Doc was on the right track but lacked enough detail — agent had to supplement by reading code or other sources.

### Missing doc signals (from transcript patterns)

- **Excessive exploration**: High ratio of read/grep tool calls to edit tool calls. Agent is building a mental model that should have been handed to it.
- **Broad code reading**: Agent touching many directories rather than staying focused in one area. Especially reads that aren't in context.md.
- **Heavy search tool usage**: Lots of Grep/Glob calls before the first edit, particularly searching for concepts or patterns that should be documented.
- **Context.md gap**: Files the agent actually read during the task that weren't listed in context.md. The diff between "files in context" and "files actually needed" reveals doc gaps.
- **Search terms as questions**: What the agent grepped for are effectively the questions it was trying to answer. These suggest what a missing doc should cover.
- **File cluster patterns**: If the agent read many files in the same directory (e.g. 8 files in `src/auth/`), that area likely needs documentation.

### Structural signals (from plan/context comparison)

- **Plan deviation**: Agent deviated from the plan, possibly because docs led it astray or didn't cover an edge case.
- **Context underuse**: Doc appears in context.md across many tasks but is rarely referenced in transcripts — may be low value or poorly written.
- **Context overuse**: Doc is frequently discovered by agents during exploration but never appears in context.md — should be recommended for future context files.


## CLI

```bash
# Mark that lines 0 - 10 in a docs file provided clear guidance on how to structure relationship between modules
autodoc feedback good ./docs/path/to/file.md --describe "Clear guidance on how to structure repository/service relationship" --start 0 --end 10 

# Mark that lines 10 - 12 conflicted with what was in another file
autodoc feedback bad ./docs/path/to/file.md --describe "Conflicted with file ./docs/other/file.md" --start 10 --end 12

# This saves a event in the log so that we know that a file has had its issues fixed. anything before this point can be considered fixed / not an issue.
autodoc feedback fixed ./docs/path/to/file.md
# This will output a full, markdown pretty print string instructing the agent exactly what to do. This should include:
# - Looking at the work you just completed (assume its in the agent context window)
# - Compare that to the docs you read at the start of the session
# - Pick out what lines were good/bad for your work. Ignore the ones that didn't have much affect.
# Then use good/bad instructions to make notes of why.
autodoc feedback instructions
```

## Storage

In the `.autodoc/feedback.jsonl` file, each good/bad item is saved as a json object with these keys:
- id (random generate, keep it short but random)
- ts: timestamp, iso
- type: good/bad
- description: the description text offered
- gitHash: git hash that this command was run at (so we can get the right version of the doc)