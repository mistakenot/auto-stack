# TODO.md

## auto-etl — align with user-journey spec

- [ ] autoetl: add `quickstart` command
- [ ] fix: warning: `~/.auto/host.json` not found, using hostname:
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

## auto-img

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
