---
hash: "a0c3d023"
id: "15aefb47"
read_when: "understanding the structured compiler evaluation dataset or adding cases to the corpus"
summary: "Summary of the 40-case structured compiler evaluation corpus: task type breakdown (17 go_cli_feature, 6 docs_skill, etc.), corrections per case distribution, and sources (task folders, git commits, autosearch sessions)."
title: "Structured Compiler Eval Corpus — Phase 0 Summary"
---

# Structured-Compiler Eval Corpus — Phase 0 Summary

## Counts

- Total cases: **40**
- Sources:
  - From `docs/tasks/NNN-*/` planning folders: 8
  - From notable git commits (fix/refactor): 6
  - From autosearch correction-heavy sessions: 26

## Breakdown by `task_type`

| task_type | count |
|---|---|
| go_cli_feature | 17 |
| docs_skill | 6 |
| architecture | 5 |
| etl_schema | 5 |
| bug_fix | 4 |
| refactor | 3 |

## Corrections per case

- Cases with 0 corrections: 4
- Cases with 1 correction: 14
- Cases with 2 corrections: 13
- Cases with 3+ corrections: 9
- min / p25 / median / p75 / max: 0 / 1 / 2 / 2 / 4

## Dropped sessions

| reason | count |
|---|---|
| not_in_autostack | 16 |
| no_initial_prompt | 0 |
| no_describe | 0 |
| too_few_corrections | 0 |

## Caveats

- Three different source signatures live in the `notes` field. Downstream
  experiments can route on these:
  - `source=task_folder` — anchored to a `docs/tasks/NNN-*/` planning folder.
    `initial_prompt` is a synthesized `# heading + first paragraph` from
    `requirements.md` (not the literal first user message). `corrections`
    come from the `Problems faced` section of `feedback.md` — these are
    *post-hoc retrospectives*, not mid-stream user corrections.
  - `source=git_commit` — initial_prompt is the conventional commit subject;
    `corrections` are body bullet points. Useful for `bug_fix` and `refactor`
    coverage where chat transcripts under-represent the category.
  - `source=autosearch_corrections` — initial_prompt is the literal first
    user message; `corrections` are real mid-stream user redirections
    extracted from search snippets.
- The `classify_task_type` heuristic is keyword-scored (best-match wins).
  Re-classifying with an LLM would likely move some `architecture` items
  into `etl_schema` or vice versa. The labels are good enough for
  diversification, not for authoritative analysis.
- `task_type` for `source=git_commit` cases is derived from the conventional
  commit prefix (`fix(` → bug_fix, `refactor(` → refactor) and is therefore
  reliable.
- `final_artifacts` includes commit SHAs found by `git log --grep '(NNN):'`
  for task-folder cases; this captures the conventional commit prefix used
  in this repo. For session-source cases we only have the session_id.
- Some session-source cases come from worktree workspaces
  (`/home/vscode/src/auto-stack/.claude/worktrees/...`) — these are still
  legitimately auto-stack work but the agent ran inside an isolated clone.
