# Scanning output across all workers (herdr)

herdr has **no fleet-wide grep** — no `agent grep`, no `pane grep`, no bulk-read
endpoint. Enumerate and filter in the caller.

## The loop

```bash
herdr agent list \
  | jq -r '.result.agents[] | "\(.name // .pane_id)\t\(.pane_id)"' \
  | while IFS=$'\t' read -r name pane; do
      text=$(herdr agent read "$pane" --source recent-unwrapped --lines 200)
      if printf '%s' "$text" | grep -q "PATTERN"; then
        printf '=== %s (%s) ===\n' "$name" "$pane"
        printf '%s' "$text" | grep "PATTERN"
      fi
    done
```

Notes:

- `agent read` prints **raw text** on herdr 0.8+ — do not pipe it through
  `jq .result.read.text` (that was the 0.7.x shape and now yields nothing). See
  `references/herdr/read-output.md`.
- Use `agent list` to skip plain shells. Use `pane list` and its `pane_id`s only
  if you deliberately want to scan shell panes too.
- `--source recent-unwrapped` avoids false negatives from terminal line-wrapping
  splitting a pattern across lines.
- Re-fetch the list each time you scan; do not cache it across invocations.

## Prefer structured fields to text matching

Much of what a scan is usually looking for is already a field, and reading
fields is one round trip instead of N:

```bash
herdr agent list | jq -r '.result.agents[]
  | [.name, .pane_id, .agent, .agent_status, .cwd, .terminal_title_stripped]
  | @tsv'
```

- **Busy / finished / stuck-on-a-question** → `agent_status`
  (`working` / `done` / `blocked`). Note `done`, not `idle`, is the resting
  state for a CLI-driven worker — see `references/herdr/wait-for-ready.md`.
- **What it last did** → `terminal_title_stripped`.
- **Which task / branch** → `cwd`, plus `herdr worktree list --cwd <repo>`.

Fall back to text scanning only for things that exist solely in the transcript,
such as a PR URL or an error message.

## Cost

The read loop is O(N) round trips with no batching. For a large fleet, filter on
the structured fields first and read only the panes that remain ambiguous.
