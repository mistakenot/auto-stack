# TODO.md

> Organized as one `##` section per tool (canonical binary name), with `###`
> sub-groups inside larger ones. Cross-cutting notes live at the top; planned
> (not-yet-built) tools are grouped at the bottom.

## Bottlenecks — current state

What are the current bottlenecks?

- Validation / verification. Need to be able to view screenshots, terminal videos, verification agent outputs, etc.
- General sense of chaos. What do I have in flight? What is stuck in worktrees? What work has been orphaned from a worker? What is actually happening??
- Multi-stage work gets chocked up, too much gating, waiting, etc.
- Agents probably spend a lot of time down, waiting for things to do
- Do we have enough tmux worker instances? how to create/kill them automatically?

## Session quality scoring (cross-tool)

- [ ] Calculate a quality score per coding session based on ETL + search data
- [ ] Top score = agent one-shotted the implementation with minimal corrections
- [ ] Negative signals: multi-line edits to same file, repeated tool failures, user corrections, PR feedback requiring changes, long back-and-forth loops, rollbacks
- [ ] Positive signals: clean first-pass implementation, tests passing on first run, no user corrections, small diff-to-task ratio
- [ ] Should be fast to compute from existing parquet/indexed data (no LLM calls)
- [ ] Optionally include static analysis signals: cyclomatic complexity, lint warnings introduced, code churn
- [ ] Use scores to identify which workflows/prompts/patterns produce the best outcomes
- [ ] Feed into auto-reflect for pattern extraction (what do high-scoring sessions have in common?)

## auto-doc

- [ ] exclude .claude folder by default
- [ ] support filtering by `tags: []` front matter value (e.g. `tags: [needs-review]`), so autowatch cron triggers can pick up on tagged docs
- [ ] fix source tag scanning to skip directories (crashes on `.claude/skills/open-prose` directory). The `ignores` config only applies to doc scanning, not source tag scanning.

## auto-etl

Align with user-journey spec.

- [ ] add `quickstart` command
- [ ] fix: warning: `~/.auto/host.json` not found, using hostname:
- [ ] ensure sessions/messages include `model_id`
- [ ] **[PARKED — needs more design]** Live tier for monitoring: batch ETL (parquet) is too stale for live queries ("what is job X doing now?"); building a separate monitoring store would recreate data auto-etl already owns. Direction: one schema / one transform / many materializations — a hot tier (pure-Go SQLite, fed by a continuous tailer of the JSONL log) in front of parquet, behind one query surface; ETL becomes a compaction flush, not a daily job. DRAFT/RFC: `docs/live-tier-architecture.md`. Open questions to resolve before building: hot-store retention window, compaction trigger, multi-host query scope, hot/cold schema unification, replay-on-restart.

## auto-reflect

- [ ] Capture requirements-scoping decisions as minable signal. Today the tool models `observation` (kind: `correction|pattern|gap|incident`) and `rule` — there is **no `decision` concept**, and nothing captures the answers a user gives to scoping questions (e.g. AskUserQuestion during `/new-task`). Those answers currently live only in the planning doc (`<pd-decision>`/`<pd-question>`/`<pd-answer>`), contextual commits, and the raw transcript — and the structured AskUserQuestion payload is dropped during ETL (see `docs/research/askuserquestion-analytics.md`). Proposal: add a `decision` observation kind (or a dedicated decision event) so scoping Q&A becomes first-class, queryable signal that can feed back into asking better questions. Related design: `docs/better-questions.md`, `docs/claude-decision-intelligence-deep-dive.md`.

## auto-search

- [ ] `truncateStr` in `auto-search/internal/cli/session.go` slices by bytes, not runes — same UTF-8 bug as the `search.TruncateAtRune` fix in PR #47 (`messages.go` / `cli/search.go` snippets). Fires at the 80-char tool-arg preview when args contain emoji, accented chars, or unicode in commands/paths. Route through `search.TruncateAtRune` (or duplicate the `utf8.RuneStart` advance loop) so tool tags in `session get` output stay valid UTF-8.

### Git-related APIs — low-level primitives

- [ ] list files ranked by edit frequency across sessions (most-touched hotspots), filterable by time range and remote.
- [ ] list files edited multiple times within a single session (rework signal — repeated edits suggest complexity or uncertainty).
- [ ] list file edit sequences for a target file — what files are typically edited before/after it (workflow adjacency graph).
- [ ] list tool failure hotspots by file — files associated with the most bash failures or tool errors (problematic areas).
- [ ] list branches ranked by session count and token spend — surface where effort concentrates and long-lived branches.
- [ ] list files edited within a short time window of each other (temporal co-edit pairs) — reveals implicit coupling not visible in imports.

### Query gaps — found during requirements-extraction research

- [ ] `search` command: add `--min-index` / `--max-index` filter on message index. Needed to isolate session-opening messages (e.g. "user messages where index < 5") without post-filtering JSON.
- [ ] Skill metadata indexing: `--skill` filter doesn't match skills invoked via `<command-name>` tags in user messages (e.g. `/new-task`, `/process-requirements`). Only ETL-tracked skills appear in `autosearch skills`. Need to extract skill name from `<command-name>` tags during indexing.

## auto-skill

- [ ] Test/eval mode: simulate skill picking across coding agents
  - load skill frontmatter (descriptions, trigger conditions) into different agents
  - run probabilistic tests with synthetic scenarios to verify correct skill selection
  - measure precision/recall: does the right skill get picked at the right time?
  - regression suite to catch skill description changes that degrade selection accuracy
- [ ] Skill health check: audit total skill load per agent
  - count total skills loaded, warn if too many or too few
  - measure total token cost of all skill frontmatter in system prompt
  - flag when combined skill descriptions consume too much context budget
  - identify redundant/overlapping skills that could be consolidated
- [ ] Skill sync checks. Skills can highlite what binaries are required to execute. then on sync, we check if these are installed and warn if not. `depends_on` or similar.

## auto-artifact image follow-ups

- [ ] Image artifact browsing and progressive disclosure
  - auto-generated descriptions per image (from context or vision model)
  - index file for progressive disclosure (like skill frontmatter — agents see summaries, fetch full images on demand)
  - list/show commands for existing artifacts
  - namespacing to separate different projects (e.g. S3 prefix per project)
  - auto-generates downsized preview thumbnails so agents can browse before fetching full-size originals
  - metadata includes token cost estimate and file size for budget-aware fetching

## auto-mail (planned)

- [ ] Using Using our existing RPC stuff that allows cross-host communication I'm interested in creating a very simple mail inbox system where agents can push messages to inboxes They can then also read from inboxes, pull messages from inboxes etc so that different projects can cross-communicate to each other We maybe keep this really flexible to begin with We have threads, which are a bit like threads and slack or whatever which have topic names or IDs Yeah, I'm not sure when you think about this more

## auto-hook

- [ ] capture codex / opencode hooks the same way we capture claude hooks.
- [ ] higher level abstraction for hooks. e.g: whenever an agent opens a pr or pushes to a pr, we want to hint it with some text to say "If you just pushed to a PR and have finished other work, check back in a few minutes and run /address-feedback", but we define this once and it works across all agents.
