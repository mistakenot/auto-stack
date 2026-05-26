# Task 008: Commit-to-Session Link

## Problem

Git commits and coding sessions are stored in separate ETL datasets with no authoritative join key. The `commits` table has SHAs, messages, and trailers; the `messages` table has session IDs, bash commands, and tool output — but nothing explicitly links a commit to the session that produced it. Today the only join is a fuzzy time-window + repo match, which produces duplicates because multiple sessions run concurrently on the same repo.

## Why This Matters

Linking commits to sessions unlocks a class of analysis that neither git history nor session history can provide alone:

- **"What was the agent doing when it wrote this commit?"** — join a commit SHA to the full session transcript to see the reasoning, tool calls, and user instructions that led to the change.
- **Agent effectiveness measurement** — how many sessions does it take to land a feature? How much token spend per commit? Which models produce commits that stick vs commits that get reverted?
- **Rework detection** — if the same session produces multiple commits to the same file, that's a signal the agent struggled. Cross-referencing commit churn with session transcripts reveals why.
- **Session-aware code review** — a reviewer can see not just the diff, but the session context: what the agent read, what it tried and reverted, what the user asked for.

Without this link, git data and session data are two isolated silos. With it, they become a single queryable timeline of intent → action → outcome.

## Goals

- Add a `session_id` field to the `Commit` parquet row, populated during git ETL extraction
- Use a two-tier fallback: prefer the `Session-Id` git trailer (authoritative), fall back to regex extraction from session message content (retroactive)
- Enable joins between `commits` and `sessions`/`messages` tables via `session_id`
- `autoconfig init --project` installs a `prepare-commit-msg` hook that appends the `Session-Id` trailer, ensuring new commits carry the authoritative join key from the start

## Acceptance Criteria

**AC-1**: Session ID extracted from trailer
- Given: a commit message contains `Session-Id: <uuid>` as a git trailer
- When: the git ETL processes this commit
- Then: the `session_id` field on the `Commit` row is set to `<uuid>`

**AC-2**: Session ID extracted from message content (fallback)
- Given: a commit has no `Session-Id` trailer, but the `messages` table contains a row where `bash_command LIKE '%git commit%'` and `content` contains `[branch SHORT_SHA]` matching this commit's short SHA
- When: the git ETL processes this commit
- Then: the `session_id` field is set to the `session_id` from the matched message row

**AC-3**: Fallback avoids false positives
- Given: the regex `\[[\w/.-]+ ([0-9a-f]{7,})\]` matches content from a non-commit command (e.g. `cat`, `autosearch`, `git log`)
- When: the fallback extraction runs
- Then: only matches from rows where `bash_command` matches a commit-creating command (`git commit`, `git merge`, `git cherry-pick`) are considered

**AC-4**: Trailer takes precedence
- Given: a commit has both a `Session-Id` trailer AND a matching message row from fallback
- When: the ETL processes this commit
- Then: the trailer value is used (fallback is not attempted)

**AC-5**: No session ID leaves field empty
- Given: a commit has no `Session-Id` trailer and no matching message row
- When: the ETL processes this commit
- Then: the `session_id` field is empty string (not populated with a guess)

**AC-6**: Field available in parquet output
- Given: the git ETL has run
- When: querying `commits/**/*.parquet` with duckdb
- Then: `session_id` is a queryable column and can be joined to `sessions.id`

**AC-7**: Hook installed by `autoconfig init --project`
- Given: the project has no existing `prepare-commit-msg` hook
- When: `autoconfig init --project` runs its git-hooks setup
- Then: a `prepare-commit-msg` hook is installed that appends a `Session-Id: <uuid>` trailer to the commit message

**AC-8**: Existing hook is not overwritten
- Given: a `prepare-commit-msg` hook already exists in `.git/hooks/`
- When: `autoconfig init --project` runs its git-hooks setup
- Then: the command exits with a non-zero status and a clear error message explaining that a hook already exists and was not modified

## Out of Scope

- Adding `session_id` to `commit_files` or `commit_hunks` (can be joined through `commits`)
- Building a dedicated `commit_sessions` bridge table
- Hashing or obfuscating session IDs for privacy

## Open Questions

None — all resolved during investigation.
