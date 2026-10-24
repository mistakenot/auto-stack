# Waiting for a worker (herdr)

Block until an agent settles, or until its output matches a pattern. Status is
**push-based** — reported by the installed integration hook, not scraped from
the screen.

## The one gotcha that will hang you: `idle` vs `done`

`idle` and `done` are the *same underlying ready state*. The difference is
whether a human has seen it:

- **`idle`** — ready for input **and its tab has been seen in the focused herdr
  UI**.
- **`done`** — the same ready state, after work finished that nobody watched.

**CLI reads do not mark a tab as seen.** A worker you drive entirely from the
command line therefore settles at **`done`**, never `idle`. Verified live:
`herdr agent wait <target> --until idle --timeout 60000` timed out against an
agent that was demonstrably sitting at its prompt, while a bare
`herdr agent wait <target>` returned instantly.

```bash
herdr agent wait <target> [--timeout MS]                  # ✅ settled states: idle | done | blocked
herdr agent wait <target> --until blocked [--timeout MS]  # ✅ specific: wait for it to ask a question
herdr agent wait <target> --until idle                    # ❌ hangs for a CLI-driven worker
```

Same rule for `herdr agent prompt --wait`: leave the defaults alone.

## Commands

```bash
herdr agent get <target>                                   # non-blocking snapshot
herdr agent wait <target> [--until STATUS]... [--timeout MS]
herdr pane wait-output <pane_id> (--match TEXT | --regex PATTERN) \
     [--source visible|recent|recent-unwrapped] [--lines N] [--timeout MS]
```

`herdr agent start` already blocks until the agent is ready (see
`references/herdr/spawn-worker.md`), so there is **no registration race to poll
around** — this is the main thing 0.8.2 fixes over 0.7.x.

Omitting `--timeout` on `pane wait-output` waits indefinitely; always pass one.

## State vocabulary

`idle | working | blocked | done | unknown`

| State | Meaning |
| ----- | ------- |
| `working` | A turn is actively running. |
| `idle` | Ready for input, and seen in the focused UI. |
| `done` | Ready for input, after unwatched work. **The normal resting state for a background worker.** |
| `blocked` | herdr recognised an approval or question UI. Something is waiting on a human. |
| `unknown` | An agent is present but herdr cannot classify it. **It does not mean finished** — read the pane. |

`agent get` also returns `interactive_ready` (boolean) and `state_change_seq`,
which increments on each observed transition.

## Confirming a dispatch actually started

`agent prompt --wait` is normally enough. When you want an explicit kickoff
check — for instance dispatching without `--wait` so you can move on — watch for
the transition into `working`:

```bash
herdr agent prompt task-042 "/execute-task 042"
herdr agent wait task-042 --until working --timeout 20000
```

Verified live for both Claude Code and Codex. If it never reaches `working`, the
prompt did not land — see `references/herdr/send-prompt.md`.

## Matching on output

```bash
herdr pane wait-output <pane_id> --match "PR opened" --source recent-unwrapped --timeout 600000
```

`pane wait-output` searches the selected snapshot **immediately**, so text that
is already on screen matches straight away. Two consequences:

- It cannot tell "already there" from "just arrived" — do not use it to detect a
  *new* event unless the pattern is unique to that event.
- Your own prompt echoes into the pane. Matching on a string you just sent will
  match your own input line, not the reply. Verified live: waiting for `PONG`
  after sending "reply with PONG" matched the input box. Match on something only
  the agent's *output* can contain, or use `agent wait` on status instead.

`--regex` takes a Rust regular expression; `--match` is a literal substring.
