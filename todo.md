# TODO.md

## auto-etl — align with user-journey spec

- [ ] autoetl: add `quickstart` command
- [ ] fix: warning: `~/.auto/host.json` not found, using hostname:
- [ ] etl should import GitHub PR feedback (comments, reviews) as an additional data source — contains valuable signal (code review insights, design decisions, bug context). Spec: `auto-etl/docs/github-pr-etl.md`
- [ ] auto-doc: support filtering by `tags: []` front matter value (e.g. `tags: [needs-review]`), so autowatch cron triggers can pick up on tagged docs
- [ ] auto-doc: fix source tag scanning to skip directories (crashes on `.claude/skills/open-prose` directory). The `ignores` config only applies to doc scanning, not source tag scanning.

## autodoc

- [ ] exclude .claude folder by default
