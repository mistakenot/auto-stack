# Sending a prompt to a worker (herdr)

Deliver text to a running agent and confirm it was actually accepted.

## Use `agent prompt` — not raw keystrokes

```bash
herdr agent prompt <target> "<text>" [--wait] [--timeout MS]
```

`agent prompt` sends the text honouring the pane's live bracketed-paste mode,
then sends an encoded Enter **after a short delay**. That delay is not
decoration: sending text and Enter back-to-back is genuinely unreliable, and
`agent prompt` is the only supported way to get it right.

```bash
herdr agent prompt task-042 "/execute-task 042" --wait --timeout 600000
```

`--wait` blocks until the agent reaches its first settled state. Do not pass
`--until` alongside it — the defaults are already correct (see
`references/herdr/wait-for-ready.md`, and note that **`--until idle` will hang**
for a CLI-driven worker).

Multi-line prompts work: embedded newlines land as soft newlines in the input
box and do **not** submit early. Verified live on both Claude Code and Codex.

Slash commands are ordinary text — `herdr agent prompt task-042 "/execute-task
042"` is all there is to it; there is no separate slash-command primitive. Send
the command with its argument in one string: a bare `/name` leaves the agent's
autocomplete menu open, where Enter selects a menu entry instead of submitting.

## Error codes you must handle

**`agent_blocked`** — the agent is sitting at an approval or question dialog.
`agent prompt` refuses to send **before writing any input**, which is exactly
what you want: blind input into a dialog answers the dialog. Read the pane
(`references/herdr/read-output.md`) to see what it is asking, then either
answer it per `references/herdr/unblock-worker.md` — `agent send-keys`, one
press at a time, **re-reading the pane after each** to confirm it landed — or
escalate to the user.

**`agent_prompt_stalled`** — the prompt produced no observed state change within
5s:

```json
{"error":{"code":"agent_prompt_stalled","message":"agent prompt produced no observed state change within 5000 ms; status is done and state_change_seq remained 5"}}
```

Observed live immediately after a preceding prompt settled; the text was **not**
delivered. Read the pane to confirm nothing landed, then retry once — the retry
succeeded in testing. If a second attempt also stalls, treat the worker as
unhealthy and reap it rather than sending a third time.

## Why not `pane send-text` + `send-keys Enter`

That pair is the manual equivalent and it is **fragile**. Verified live on
herdr 0.7.1: sending Codex a prompt with `pane send-text` followed immediately
by `pane send-keys Enter` left the text sitting unsubmitted in the input box —
the Enter was swallowed. Claude Code accepted the same sequence, so the bug is
invisible until you switch agents. A one-second pause fixed it. `agent prompt`
encapsulates exactly this, so use it and skip the class of bug entirely.

Reach for the raw pane surface only when you deliberately need keystrokes rather
than a prompt:

```bash
herdr agent send-keys <target> <key> [key ...]   # e.g. esc, enter, ctrl+c, down, shift+tab
herdr pane  send-keys <pane_id> <key> [key ...]  # same, addressed by pane id
herdr pane run <pane_id> "<command>"             # shell command + Enter, for a shell pane
```

`agent send-keys` validates every key before writing any bytes, but
`{"type":"ok"}` is not evidence of delivery — re-read the pane (after a short
pause) to confirm a dialog actually moved. Both levels and both confirm
spellings (`enter`, `return`) verified live against Claude Code dialogs on
herdr 0.8.2; if a re-read shows nothing landed, `pane send-keys <pane_id>` with
`return` is the fallback path one report needed. Full procedure in
`references/herdr/unblock-worker.md`.

Key names are herdr's own (`ctrl+u`, `shift+tab`, `down`, `enter`, `esc`) —
**tmux syntax like `C-u` is silently ignored**, verified live, so a "clear the
input box" step written in tmux notation does nothing and the next text appends
to whatever was already there.

`pane run` types a command **and** Enter atomically, but it is for shell panes
(for example running a setup command in a fresh worktree pane before the agent
launches). Never use it to prompt a running TUI agent.
