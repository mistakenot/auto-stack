# evals

Harbor-based eval suite plus the thin `evals` CLI. Read `README.md` for the
vocabulary, naming conventions and task rules before adding anything.

## Build and test

```bash
cd evals
uv sync
uv run pytest -q              # offline CLI tests, no Docker or model calls
uv run evals lint --text      # must be clean before any run
uv run evals oracle --text    # needs Docker; every task must score 1.0
```

## Rules for agents

- Create tasks with `uv run evals new-task`, never by hand-copying a directory and renaming it.
- Never put arm-specific text in a task's `instruction.md`. Use the arm's `extra_instructions`.
- A treatment arm changes one factor and declares it under `changes` in `experiment.toml`.
- Bump `[task].version` whenever a change alters what a task measures.
- Real runs spend subscription quota. Run `evals run ... --dry-run` first, and only launch real runs when asked.
- Mount paths in arm configs use `${EVALS_ROOT}`. The CLI sets it. Relative paths silently fail inside Harbor's generated compose file.
