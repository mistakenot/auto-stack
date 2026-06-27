# Enforcing the glossary automatically

A ubiquitous language only works if it's enforced. Two complementary layers:

1. **Agent-time** — the `@`-link in `CLAUDE.md` plus the enforcement directive `init` writes
   means every agent session loads the glossary and flags drift. This is the everyday layer.
2. **Commit/CI-time** — run `glossary.py check` so a malformed entry or a `_Has_:` line
   pointing at an undefined term fails fast, before it spreads. This is the safety net.

The bundled script is stdlib-only (`scripts/glossary.py`) — no install step. `check` exits
non-zero on any error, so it drops straight into a hook or a CI job.

## Pre-commit hook (husky)

If the repo already uses husky (this one does), add to `.husky/pre-commit`:

```sh
python3 scripts/glossary.py check docs/concepts/UBIQUITOUS_LANGUAGE.md
```

For a repo without husky, the same line works in a plain `.git/hooks/pre-commit` (make it
executable). Keep the path to the script wherever the skill installed it.

## GitHub Actions

```yaml
# .github/workflows/glossary.yml
name: glossary
on: [pull_request]
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: python3 scripts/glossary.py check docs/concepts/UBIQUITOUS_LANGUAGE.md
```

## Keeping the diagram fresh

If you embed the ER diagram in the glossary (`glossary.py diagram <file> --write`), you can
regenerate it in the same hook so the picture never drifts from the `_Has_:` lines:

```sh
python3 scripts/glossary.py diagram docs/concepts/UBIQUITOUS_LANGUAGE.md --write
git add docs/concepts/UBIQUITOUS_LANGUAGE.md
```

Only wire this in if the team wants the diagram version-controlled; otherwise generate it
on demand. Offer the hook — don't silently add it to someone's repo.
