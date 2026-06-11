# Task 023: reflect-miner-queue

## Problem

The mining skill (epic sub-task 2.1, `docs/epics/001-reflect-playbook-loop.md`) needs to know
*which session to mine next* and *which sessions it has already mined*. Today that bookkeeping
would have to live in a markdown file the agent re-reads and trusts — fragile, and it can't tell
a mined-but-empty session apart from a never-mined one. Coverage tracking belongs in the CLI as
deterministic state, not in the agent's head.

## Goals

- Add a `miner` command group to `auto reflect` that acts as a deterministic work-queue +
  priority scorer over coding-agent sessions.
- Keep the miner's contract narrow: it returns **session IDs + the signals that justify their
  ranking**, never transcript content. The mining skill fetches transcripts via
  `auto search session get <id>`.
- Track mining **coverage** as append-only events on the existing reflect event log, so re-mining
  is a first-class operation and the input backlog is queryable.
- Make the mining skill (2.1) thin and reliable by moving windowing, dedup, and prioritization
  out of the prompt and into the tool.

## Acceptance Criteria

**AC-1**: `miner next` returns ranked, content-free work items
- Given: ETL parquet exists at `~/.auto/etl/output` with indexed sessions, and a reflect event log
- When: an agent runs `auto reflect miner next --limit N`
- Then: it returns up to N session IDs as JSON, each with `{ session_id, cwd, last_message_at,
  message_count, priority_score, signals: {…} }` — where `cwd` is sourced verbatim from the
  session's `Workspace` column (the absolute filesystem working dir; the JSON key stays `cwd` as the
  agent-facing name) — ordered by descending `priority_score`, and containing **no transcript
  content** — only IDs, metadata, and the signal breakdown.

<!-- RESOLVED(P2): `cwd` is not a column in the session schema
REVIEW: AC-1 lists `cwd` as a returned field and solution.md's WorkItem has `CWD string json:"cwd"`, but `auto-etl/internal/model/model.go:105-154` (AgentSession, 28 fields) has NO `cwd` field — verified by grep (`cwd|working_dir|workdir` → no hits). The nearest candidates are `Workspace` (the session's filesystem working dir, `transform.go:443` = `raw.Workspace`) and `SourcePath`. The docs should state explicitly which field `cwd` is sourced from (almost certainly `Workspace`), otherwise the implementer guesses. Note this also ties into the scope-key question on AC-1c.
AUTHOR: Confirmed — no `cwd` column exists; `Workspace` is the working-dir path. AC-1 now states `cwd` is sourced verbatim from the session `Workspace` column (JSON key kept as `cwd` for the agent-facing contract). solution.md WorkItem.CWD comment updated to match. The scope-key (separate concern) is resolved in the AC-1c thread → normalized `GitRemote`, not this path field.
-->

**AC-1b**: `miner next` queues top-level sessions only; subagents ride under the parent
- Given: sessions in the parquet where some have `IsSubagent = true` with a `ParentSessionID`
- When: `miner next` runs
- Then: only top-level sessions (`IsSubagent = false`) are returned as queue items — subagent
  sessions are never independent work items. A flag (e.g. `--include-subagents`) makes each returned
  item also list the child subagent session IDs belonging to that parent, so the skill can fetch and
  mine them as part of the parent's unit. `ack` is recorded at the parent level and covers its
  subagents.

**AC-1c**: queue scope defaults to the current repo (worktree-stable)
- Given: the global parquet contains sessions from many workspaces, including multiple worktrees of
  the current repo
- When: `miner next` runs with no scope flag
- Then: sessions are scoped by **normalized git remote** — those whose normalized `GitRemote` matches
  the current repo's normalized remote are queued, so every worktree of the same repo shares one
  queue. **Fallback**: when the current repo has no remote (unborn HEAD / local-only), scope by
  `Workspace` path-prefix of the current repo root instead. An `--all` flag widens the universe to
  every workspace/remote in the parquet.

<!-- RESOLVED(P2): scope key is ambiguous — `Workspace` path vs normalized git remote
REVIEW: AC-1c and solution.md scope by matching the session's `Workspace`, but `Workspace` is the absolute filesystem path of the session's working dir (`transform.go:443` = `raw.Workspace`, e.g. `/home/user/project`), NOT a stable repo identity. This repo's core workflow is git worktrees (see CLAUDE.md "Git Worktree Discipline"), where the main checkout and each worktree branch have DIFFERENT `Workspace` paths for the SAME repo — so path-equality scoping would split one repo's sessions into several disjoint queues and miss most of them. context.md:87-88 hints at the robust alternative ("reuse auto-shared/git for deriving/normalizing the current repo's remote"), i.e. scope by the normalized `GitRemote` column (which exists on the session). solution.md (Workspace match) and context.md (remote normalize) disagree; pick one and make it consistent. Recommend `GitRemote` for worktree-stable scoping, with a documented fallback when the remote is empty (unborn HEAD / no remote).
AUTHOR: Adopted `GitRemote`. AC-1c now scopes by normalized `GitRemote` (worktree-stable — every worktree of the repo shares one queue), with a documented `Workspace` path-prefix fallback when the current repo has no remote. `GitRemote` is a verified session column (`model.go:113`), and `DetectRepoLenient` already returns `RepoInfo.Remote` + `auto-shared/git` normalizes it. solution.md scope description, the WorkItem note, and context.md updated to match (path-equality matching removed). NOTE for execution: coverage acks fold by `session_id` within the current checkout's event log, which is unchanged — only the candidate-selection scope key moves to the remote.
-->

**AC-2**: `miner next` excludes already-mined sessions at the current version
- Given: session `S` has an `ack` recorded at the current miner version
- When: `auto reflect miner next` runs
- Then: `S` is omitted from the results; sessions never acked (version 0) or acked only at a
  lower version than the current miner version are included.

**AC-3**: `miner ack` records append-only coverage
- Given: a session `S` the agent has just mined
- When: `auto reflect miner ack <S> --observations N` runs
- Then: a `session_mined` event is appended to the reflect event log carrying the session ID, the
  current miner version (baked into the CLI), the observation count N (N=0 = mined-but-empty), and
  a timestamp. Running `ack` again for `S` appends a second event (history, not overwrite).

**AC-4**: re-mining is driven by the baked-in miner version
- Given: the CLI's baked-in miner version is bumped from `v1` to `v2` in code
- When: `auto reflect miner next` runs against sessions all previously acked at `v1`
- Then: those sessions reappear in the queue (latest ack version `<` current version), so a version
  bump re-opens the corpus with no migration or flag.

**AC-5**: priority scoring is deterministic and signal-based
- Given: a set of pending sessions
- When: `miner next` ranks them
- Then: `priority_score` is computed deterministically (no LLM) from cheap signals read from the
  parquet `content_truncated` column — e.g. correction-language density, `tool_use_error` count,
  build/test-failure markers, `AskUserQuestion` frequency — normalized by session length, and the
  contributing `signals` are returned alongside the score.

**AC-6**: mining coverage is visible from two surfaces
- Given: a reflect event log with `session_mined` events and an ETL parquet of sessions
- When: the agent runs `miner status`, and separately `reflect stats`
- Then: `miner status` reports the full detail — pending vs mined counts, coverage % at the current
  miner version, observations-per-session, signal distribution — and `reflect stats` carries a
  summary line so the input backlog (sessions to mine) sits alongside the existing output backlog
  (`UnconsolidatedObservations`).

**AC-7**: no new store; events only
- Given: the implementation
- When: reviewed against the epic's out-of-scope list
- Then: coverage is a new event type folded from the existing sharded JSONL event log — not a new
  database, file, or store outside the event log.

## Scope additions (from user-simulation 2026-06-11)

Four contract additions adopted after the 7-persona user simulation
(`simulations/2026-06-11-task-023-miner-queue.md`), which surfaced 4 abandonments clustered on
these gaps. Cheap "friction-log" fixes (dedupe `next` output, per-item `fetch_cmd` hint, `miner` in
quickstart, honor-or-reject `--since`, labelled signal units, `status` honoring `--all`) are folded
into the relevant ACs above/below rather than listed separately.

**AC-8**: source-state is never misreported as coverage
- Given: the ETL output dir is empty, missing, or unreadable
- When: `miner next` / `miner status` run
- Then: `coverage_pct` is `null` (not `100`/`0`) whenever `total_sessions == 0`; a missing/unreadable
  source is surfaced on the **default** invocation as a stderr error with a remediation hint
  (`run auto etl run`) and a non-zero exit — never hidden behind a verbose flag. An empty `next`
  result (exit 0, `[]`) carries a stderr hint distinguishing *drained* ("0 for this workspace, try
  `--all`") from *source empty*. The exit-code contract is documented: `0`+items = work,
  `0`+empty = drained, non-zero = source error.

**AC-9**: `ack` distinguishes mined / empty / failed / skipped
- Given: a session the agent attempted to mine
- When: `miner ack <S> --status <mined|empty|failed|skipped>` runs (default `mined`)
- Then: the status is recorded on the `session_mined` event. `mined`/`empty`/`skipped` are
  terminal-at-the-current-version (excluded from `next`); `failed` is **retryable** — the session
  reappears in `next` at the same version. Quality means (observations-per-session) count `mined`
  only; `empty`/`skipped`/`failed` are excluded from that mean. This prevents the `--limit 1`
  infinite re-serve loop a refused-but-unrecordable failure would cause.

**AC-10**: `priority_score` + `signals` snapshot persist on the ack event
- Given: a session being acked
- When: the `session_mined` event is written
- Then: it carries the `priority_score` and the `signals` breakdown computed at mining time (the data
  already exists at ack time). Each `next` item also carries a `prior_ack` marker
  (`{version, status, observations, ts}` or `null`) and a `remined` boolean, so a re-mine after a
  version bump is distinguishable from a first mine and a v(n-1)→v(n) score diff is reconstructable.

**AC-11**: read-only per-session signal access, decoupled from the queue filter
- Given: any session in scope, regardless of ack-state or subagent-status
- When: `miner describe <id>` (one session: signals + full ack history) or
  `miner signals <id...>` (signal rows for many sessions) runs
- Then: computed signals + ack history are returned **without** mutating state and **without** the
  ack/subagent exclusion that `next` applies — aligning with the repo's resource-oriented
  `describe`/`get` convention that the bare `next`/`ack`/`status` triad skips.

## Out of Scope

- The mining skill itself (epic 2.1) — this task only builds the CLI surface it consumes.
- The consolidation step (2.2) and any change to `observation`/`consolidate`/`rule` commands.
- LLM-based or semantic prioritization — v1 scoring is deterministic keyword/heuristic signals only.
- Acting on signals beyond ranking (e.g. auto-skipping low-signal sessions) — `next` ranks; the
  skill decides.
- A concurrent multi-miner claim/lease protocol — single-consumer assumed for v1.
- **Outcome/ROI scoring** — joining mining counts to rules-shipped, or any score that predicts
  "will produce a promoted rule" (sim wildcard). v1 scores *friction*, not payoff.
- **Cross-repo coverage rollup beyond `--all`** — `--group-by workspace` / per-workspace breakdown
  arrays / org totals (sim #5). `status --all` widens the universe but reports a single aggregate;
  the grouped dashboard is a follow-on.
- **CSV/tabular output and atomic streaming** of large dumps (sim wildcard) — JSON only in v1.

## Open Questions

- [x] **Session universe scope** — does the queue cover only sessions whose `Workspace` matches the
  current repo, or all sessions in the global parquet?
  (answered: current repo by default; `--all` flag widens to every workspace in the parquet.)
- [x] **Subagent sessions** — does the queue include subagent sessions (`IsSubagent = true`), or only
  top-level sessions?
  (answered: `next` returns top-level sessions only; subagents are exposed under a parent flag
  (e.g. `--include-subagents`) so the skill mines them as part of the parent unit, and `ack` covers
  the parent + its subagents.)
- [x] **Status surface** — standalone `miner status`, fold into existing `reflect stats`, or both?
  (answered: both — `miner status` for full detail, plus a summary line in `reflect stats`.)
- [x] **Parquet reader strategy** (solution-stage): auto-reflect does not currently read parquet.
  Options: (A) duplicate the `AgentSession`/`AgentMessage` structs, (B) extract a shared model
  module, (C) a generic schema-less parquet reader.
  (answered: **option B** — hoist the canonical `AgentSession`/`AgentMessage`/`SchemaVersion` into a
  new `auto-shared` package as the single source of truth, and re-point auto-etl and auto-search at
  it via type-alias shims (near-zero churn). See `solution.md` for the migration design.)
