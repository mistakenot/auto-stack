# Clearing a `blocked` worker (herdr)

A worker whose `agent_status` is `blocked` is sitting at a dialog — a Claude
Code `AskUserQuestion` picker, an approval prompt, a startup interstitial. Only
**keystrokes** clear it: `agent prompt` is refused by design (`agent_blocked`),
and nothing the worker does on its own will move it.

## Decide first: answer it, or hand it to a human

Read the pane (`references/herdr/read-output.md`) and work out what it is asking.

- **The answer is settled** — by the task docs, the user's prompt, or the
  workflow (e.g. a safe interstitial choice) — answer it as below.
- **The decision is load-bearing** and not yours to make — a schema change, a
  destructive step, a scope call — escalate to the user with the question
  verbatim. A worker that stopped to ask about one of these was right to stop.
- **Never send `esc`.** The picker footer offers it as *cancel*: it discards the
  question rather than answering it, and the worker has usually done real work
  to reach that point.

## The recipe

```bash
herdr agent read     <name> | grep '❯'        # which option is selected
herdr agent send-keys <name> down             # or up — one press
sleep 1; herdr agent read <name> | grep '❯'   # confirm the ❯ marker moved
herdr agent send-keys <name> enter            # confirm the selection
sleep 2; herdr agent get <name>               # blocked -> working (or done)
```

**Re-read after every press, and wait before you do.** A read fired in the
same instant as the press can still show the previous frame: verified live, a
read at t+0 showed the marker unmoved and a read at t+0.5s showed it moved.
`{"type":"ok"}` means herdr validated the key names, **not** that the dialog
received them; the only evidence of delivery is a re-read showing the marker
moved or the dialog gone.

The picker does **not** take option numbers (`1`, `2`) as a shortcut — navigate
with `up`/`down`.

## If a press appears not to land

Two spellings exist for every part of this. Verified 2026-09-11 on herdr 0.8.2
with Claude Code 2.1.268, **all of these cleared both an `AskUserQuestion`
picker and the folder-trust interstitial**: `agent send-keys <name>` and
`pane send-keys <pane_id>`; `enter` and `return`; single presses and a batched
`down down return`.

A report from the same day on Claude Code **2.1.252** saw the opposite:
`agent send-keys <name>` with `down`, `enter`, or `1` returned `ok` and moved
nothing, `pane send-keys <pane_id> enter` did nothing, and only
`pane send-keys <pane_id> return` confirmed the selection. That may have been a
version difference or reads taken before the press rendered (the same report
noted presses "arriving late"). Either way the fallback is cheap, so when a
re-read shows the marker unmoved:

```bash
herdr pane send-keys <pane_id> down      # pane id from `agent get` / `agent list`
sleep 1; herdr agent read <name> | grep '❯'
herdr pane send-keys <pane_id> return
```

Diagnose with a **navigation** key first — it is non-destructive, and whether
the marker moves separates "keys are not arriving on this path" from "the
confirm key is wrong". If nothing moves on either path, do not keep sending
keys into a dialog you cannot see responding: hand it to a human with
`herdr agent attach <name>`, answer, detach.

## Avoiding it in the first place

A blocked background worker is a hard stop until a keypress or a human
arrives. When dispatching unattended work whose prompt you control, tell the
worker to proceed on its recommended answer and record the question rather than
ask — the pattern `/new-task-quick` uses for gating questions.
Reserve blocking for decisions that are genuinely load-bearing.
