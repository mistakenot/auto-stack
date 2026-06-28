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
- [ ] We need to do a better job of tidying up the index blobs we insert into agents.md and claw.md files I think the best way to do this is maybe to have a optional set of like globs in the config which is like either an exclude from indexing or an include from indexing which will maybe name it better, exclude from memory files, include from... yeah I think... I think maybe... I'm not sure, maybe inclusive, include in memory files and then the files matching those globs are the ones that actually get put into the memory files because right now we're pulling in lots of junk like research files that shouldn't really be in there, we need to keep those really tight and consistent.

## auto-etl

Align with user-journey spec.

- [ ] add `quickstart` command
- [ ] fix: warning: `~/.auto/host.json` not found, using hostname:
- [ ] ensure sessions/messages include `model_id`
- [ ] import GitHub PR feedback (comments, reviews) as an additional data source — contains valuable signal (code review insights, design decisions, bug context). Spec: `auto-etl/docs/github-pr-etl.md`

## auto-search

- [ ] `truncateStr` in `auto-search/internal/cli/session.go` slices by bytes, not runes — same UTF-8 bug as the `search.TruncateAtRune` fix in PR #47 (`messages.go` / `cli/search.go` snippets). Fires at the 80-char tool-arg preview when args contain emoji, accented chars, or unicode in commands/paths. Route through `search.TruncateAtRune` (or duplicate the `utf8.RuneStart` advance loop) so tool tags in `session get` output stay valid UTF-8.
- [ ] `session render` function, which takes a session id and renders a html file of that session graph to help humans read through it, using planning-doc components.

### Git-related APIs — low-level primitives

- [ ] list files that are scored as being frequently edited at the same time as a target file.
- [ ] list files ranked by edit frequency across sessions (most-touched hotspots), filterable by time range and remote.
- [ ] list files edited multiple times within a single session (rework signal — repeated edits suggest complexity or uncertainty).
- [ ] list file edit sequences for a target file — what files are typically edited before/after it (workflow adjacency graph).
- [ ] list tool failure hotspots by file — files associated with the most bash failures or tool errors (problematic areas).
- [ ] list branches ranked by session count and token spend — surface where effort concentrates and long-lived branches.
- [ ] list files edited within a short time window of each other (temporal co-edit pairs) — reveals implicit coupling not visible in imports.

### Query gaps — found during requirements-extraction research

- [ ] `search` command: add `--session` filter to scope a search to a single session ID. Currently must use `session get` and parse locally.
- [ ] `search` command: add `--min-index` / `--max-index` filter on message index. Needed to isolate session-opening messages (e.g. "user messages where index < 5") without post-filtering JSON.
- [ ] `search` / `stats`: add `--tool-name AskUserQuestion` support — currently tool-name filter exists but AskUserQuestion isn't surfaced as a tool name in the index. Would directly surface Q&A decision pairs.
- [ ] Skill metadata indexing: `--skill` filter doesn't match skills invoked via `<command-name>` tags in user messages (e.g. `/new-task`, `/process-requirements`). Only ETL-tracked skills appear in `autosearch skills`. Need to extract skill name from `<command-name>` tags during indexing.

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
- [ ] Skill sync checks. Skills can highlite what binaries are required to execute. then on sync, we check if these are installed and warn if not. `depends_on` or similar.

## auto-img (planned — auto artifact)

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

## auto-mail (planned)

- [ ] Using Using our existing RPC stuff that allows cross-host communication I'm interested in creating a very simple mail inbox system where agents can push messages to inboxes They can then also read from inboxes, pull messages from inboxes etc so that different projects can cross-communicate to each other We maybe keep this really flexible to begin with We have threads, which are a bit like threads and slack or whatever which have topic names or IDs Yeah, I'm not sure when you think about this more
