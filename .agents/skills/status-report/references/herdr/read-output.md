# Reading output from one worker (herdr)

```bash
herdr agent read <target>  [--source visible|recent|recent-unwrapped|detection] [--lines N] [--format text|ansi]
herdr pane  read <pane_id> [--source visible|recent|recent-unwrapped] [--lines N] [--format text|ansi]
```

Both print the **captured text directly to stdout** — no JSON envelope, and no
`--json` flag exists for either. (On herdr 0.7.x `agent read` wrapped its output
in JSON at `.result.read.text`; that changed in 0.8, so any pipeline doing
`| jq -r .result.read.text` will silently produce nothing.)

`agent read` takes an agent name or a pane id hosting an agent; `pane read`
takes a pane id and works on plain shells too.

## Sources

| `--source` | Use for |
| ---------- | ------- |
| `visible` | the currently rendered viewport — what a human would see, including the status line |
| `recent` | recent rendered output, soft wraps included |
| `recent-unwrapped` | recent output with soft wraps joined — **prefer this for parsing**, matching, and logs |
| `detection` | the plain-text bottom-buffer snapshot herdr itself uses for agent detection |

`--format ansi` keeps escape codes when colour is the evidence; `text` strips
them.

## `--lines` and the alternate-screen limit

`--lines N` asks for more rows from the pane's screen and host scrollback. If
raising it does not reveal more of a completed response, the agent is painting
on the terminal's **alternate screen**: rows that scroll off there never enter
herdr's scrollback and no line count can recover them.

**A small `--lines` can return less than the viewport.** Verified live: reading
a pane at `--source recent-unwrapped --lines 40` missed a reply that
`--source visible` showed, while `--lines 120` found it. Never conclude "the
agent did not answer" from a narrow window — re-read with `--lines 120` or more,
or with `visible`, before deciding.

The documented fallback, only after a read has failed this way: ask the agent to
write its full response as markdown to a temp file and reply with just the path,
then read the file. Do not build that into the initial prompt.

## Reading the permission mode

`--source visible` includes the agent's status line, where Claude Code shows
`⏵⏵ bypass permissions on` / `⏸ manual mode on` and Codex shows
`permissions: YOLO mode`. Useful for a human-readable report — but it truncates
in narrow panes and changes between agent versions, so **never gate a dispatch
on it**. Use `references/herdr/verify-worker.md` for that.

## Cheap status without a read

`agent get`'s `terminal_title_stripped` carries the title the agent set, which
for Claude Code is a short summary of its last turn. It is one field instead of
a screenful — good for a fleet table (`references/herdr/scan-output.md`), though
it is a summary and not a substitute for reading when something looks wrong.
