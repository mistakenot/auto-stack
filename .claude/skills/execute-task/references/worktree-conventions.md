# Worktree Conventions

## Branch Naming

Use `task/$ID-$NAME` format with dashes.

## Worktree Isolation

- Planning happens on `main`. Implementation happens on feature branches in isolated git worktrees.
- Always create an isolated worktree for execution with branch name `task/$ID-$NAME`.
- After squash merge, worktree commits are already in main -- discarding is safe.

## Push Before Spawning Agents

Always `git push` to origin before spawning agents with worktree isolation. Agent worktrees branch from `origin/main`, not local.

## Subagent CWD

Subagents spawned inside worktrees do NOT inherit the coordinator's cwd. Always pass absolute paths to subagents, including:

- Absolute worktree path
- Task folder path
- Which phase to execute
