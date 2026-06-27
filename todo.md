# TODO.md

## What are the current bottlenecks?

- Validation / verification. Need to be able to view screenshots, terminal videos, verification agent outputs, etc.
- General sense of chaos. What do I have in flight? What is stuck in worktrees? What work has been orphaned from a worker? What is actually happening??
- Multi-stage work gets chocked up, too much gating, waiting, etc.
- Agents probably spend a lot of time down, waiting for things to do
- Do we have enough tmux worker instances? how to create/kill them automatically?

## auto-etl — align with user-journey spec

- [ ] autoetl: add `quickstart` command
- [ ] fix: warning: `~/.auto/host.json` not found, using hostname:
- [ ] ensure sessions/messages include `model_id`
- [ ] etl should import GitHub PR feedback (comments, reviews) as an additional data source — contains valuable signal (code review insights, design decisions, bug context). Spec: `auto-etl/docs/github-pr-etl.md`
- [ ] auto-doc: support filtering by `tags: []` front matter value (e.g. `tags: [needs-review]`), so autowatch cron triggers can pick up on tagged docs
- [ ] auto-doc: fix source tag scanning to skip directories (crashes on `.claude/skills/open-prose` directory). The `ignores` config only applies to doc scanning, not source tag scanning.

## session quality scoring

- [ ] Calculate a quality score per coding session based on ETL + search data
- [ ] Top score = agent one-shotted the implementation with minimal corrections
- [ ] Negative signals: multi-line edits to same file, repeated tool failures, user corrections, PR feedback requiring changes, long back-and-forth loops, rollbacks
- [ ] Positive signals: clean first-pass implementation, tests passing on first run, no user corrections, small diff-to-task ratio
- [ ] Should be fast to compute from existing parquet/indexed data (no LLM calls)
- [ ] Optionally include static analysis signals: cyclomatic complexity, lint warnings introduced, code churn
- [ ] Use scores to identify which workflows/prompts/patterns produce the best outcomes
- [ ] Feed into auto-reflect for pattern extraction (what do high-scoring sessions have in common?)

## auto-skill

- [ ] Skill linting: validate skill files against quality rules
  - description under max character limit
  - description explicitly mentions when to use the skill (trigger conditions)
  - total skill file size under max character limit
  - has explicit prerequisite checks (e.g. required tools, files, env)
  - frontmatter has required fields
  - no dead links or references to nonexistent files
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

## auto-img (auto artifact)

- [ ] Agent-friendly tool for storing image artifacts long term
  - uploads to S3, keeps images out of the repository
  - optional S3 lifecycle rules (e.g. expire after N days, transition to glacier)
  - auto-generated descriptions per image (from context or vision model)
  - index file for progressive disclosure (like skill frontmatter — agents see summaries, fetch full images on demand)
  - `init` command uses a CloudFormation template to create S3 bucket + IAM + lifecycle rules
  - CLI: `autoimg upload <file>`, `autoimg list`, `autoimg show <id>`, `autoimg init`
  - namespacing to separate different projects (e.g. S3 prefix per project)
  - auto-generates downsized preview thumbnails so agents can browse before fetching full-size originals
  - metadata includes token cost estimate and file size for budget-aware fetching

## autodoc

- [ ] exclude .claude folder by default

## auto-search

- [ ] `truncateStr` in `auto-search/internal/cli/session.go` slices by bytes, not runes — same UTF-8 bug as the `search.TruncateAtRune` fix in PR #47 (`messages.go` / `cli/search.go` snippets). Fires at the 80-char tool-arg preview when args contain emoji, accented chars, or unicode in commands/paths. Route through `search.TruncateAtRune` (or duplicate the `utf8.RuneStart` advance loop) so tool tags in `session get` output stay valid UTF-8.

**Git related apis - low level primitives**

- [ ] list files that are scored as being frequently edited at the same time as a target file.
- [ ] list files ranked by edit frequency across sessions (most-touched hotspots), filterable by time range and remote.
- [ ] list files edited multiple times within a single session (rework signal — repeated edits suggest complexity or uncertainty).
- [ ] list file edit sequences for a target file — what files are typically edited before/after it (workflow adjacency graph).
- [ ] list tool failure hotspots by file — files associated with the most bash failures or tool errors (problematic areas).
- [ ] list branches ranked by session count and token spend — surface where effort concentrates and long-lived branches.
- [ ] list files edited within a short time window of each other (temporal co-edit pairs) — reveals implicit coupling not visible in imports.

**Query gaps — found during requirements-extraction research**

- [ ] `search` command: add `--session` filter to scope a search to a single session ID. Currently must use `session get` and parse locally.
- [ ] `search` command: add `--min-index` / `--max-index` filter on message index. Needed to isolate session-opening messages (e.g. "user messages where index < 5") without post-filtering JSON.
- [ ] `search` / `stats`: add `--tool-name AskUserQuestion` support — currently tool-name filter exists but AskUserQuestion isn't surfaced as a tool name in the index. Would directly surface Q&A decision pairs.
- [ ] Skill metadata indexing: `--skill` filter doesn't match skills invoked via `<command-name>` tags in user messages (e.g. `/new-task`, `/process-requirements`). Only ETL-tracked skills appear in `autosearch skills`. Need to extract skill name from `<command-name>` tags during indexing.
