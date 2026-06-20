# Delegating to ntm agents

How to find agents running under `ntm` (Named Tmux Manager) and drive each of
the three coding-agent CLIs — **Claude Code**, **Codex**, and **OpenCode** —
without knowing in advance which is in which pane.

The shape of every interaction is the same: **discover sessions → enumerate the
agents in a session → identify each agent's type → send text or a slash command
to the right pane.** Only the last step differs per agent, and the differences
are small. Prefer the JSON (`--json` / `--robot-*`) forms — they are stable and
parseable; the human tables are not.

To prove an environment end-to-end before relying on it, maintainers can run the
procedure in `src/planning-workflow/tests/test-agent-delegation.md` (in the
skills repo), which spins up one of each agent, sends a Ping/Pong, clears them,
and tears the session down.

## 1. Find sessions you can delegate to

```bash
ntm list --json
```

Each entry under `.sessions[]` has a `.name` and an `.agents` breakdown that
counts every supported type — including `opencode`:

```json
{ "name": "myproj", "pane_count": 4,
  "agents": { "claude": 1, "codex": 1, "opencode": 1, "user": 1, "total": 4 } }
```

Use `.agents` to pick a session that actually has the agent type you want.

## 2. Enumerate the agents in a session

```bash
ntm status <session> --json
```

`.panes[]` is the authoritative list. Each pane carries everything needed to
target it:

| Field       | Meaning                          | Example values                          |
| ----------- | -------------------------------- | --------------------------------------- |
| `.index`    | Pane number — use this to target | `0`, `1`, `2`, `3`                       |
| `.type`     | Agent type (canonical)           | `claude`, `codex`, `oc`, `user`         |
| `.command`  | Process running in the pane      | `claude`, `node` (codex), `opencode`    |
| `.title`    | Tmux pane title                  | `✳ Claude Code`, `myproj__cod_1`, `myproj__oc_1` |

## 3. Identify which coding agent is in a pane

Two fields identify the agent; use them together, because **neither is reliable
alone**:

- `.command` — the live process: `claude` → **Claude Code**, `opencode` →
  **OpenCode**, `node` → **Codex** (Codex runs under Node, so `node` is its tell
  *within an ntm session*).
- `.type` — a label ntm derives from the pane **title**: `claude`, `codex`, `oc`
  (the per-pane value is `oc`, while `ntm list`'s aggregate key is `opencode` —
  same thing), or `user` for a plain shell.

**`.type` is title-derived and both lags and goes stale — do not trust it
alone (this is verified behaviour):**

- On spawn, an **OpenCode** pane can report `type: "user"` for many seconds
  (its title is `OpenCode`, not `<session>__oc_1`) and only flips to `oc` after
  the agent first produces output. Resolving OpenCode by `type=="oc"` right
  after spawn finds nothing.
- After an agent **exits** (e.g. Codex drops back to a shell), the pane keeps
  its old `type` from the stale title while `.command` already reads `bash`.

**Reliable resolution:** key off `.command` for liveness and identity —
`command=="claude"` / `command=="opencode"`, and Codex by `command=="node"` (or
`type=="codex"`, which is stable for Codex because its title is `<session>__cod_1`).
A pane whose `.command` is `bash`/a shell is **not** a running agent regardless
of `.type` — never delegate there.

## 4. Send text to an agent

**By pane index — works for every agent type (the universal path):**

```bash
ntm send <session> --pane=<index> 'your prompt here'
```

**By agent type — convenience filters, but incomplete:**

```bash
ntm send <session> --cc  'prompt'   # all Claude Code agents
ntm send <session> --cod 'prompt'   # all Codex agents
ntm send <session>       'prompt'   # broadcast to ALL agent panes
ntm send <session> --all 'prompt'   # ALL panes, INCLUDING the user shell
```

> **Gotcha — there is no `--oc` type filter.** `ntm send` only exposes
> `--cc`, `--cod`, `--gmi`. The same holds for the global `--type` filter
> (`claude|codex|gemini|cursor|windsurf|aider`). **OpenCode can only be targeted
> by `--pane=<index>` / `--panes=<index>`** — never by type. Always resolve
> OpenCode's pane index from `ntm status --json` first.

> **Gotcha — the default send path runs a CASS duplicate-check that can block.**
> By default `ntm send` checks CASS for similar past sessions and may stop on an
> interactive "duplicate work?" confirmation; it can also emit CASS errors to
> stderr (e.g. `search failed: … table not found: fts_messages`) that are noise,
> not failures. For unattended delegation, disable the check and bypass the gate:
>
> ```bash
> ntm send <session> --pane=<index> --no-cass-check 'prompt'
> ntm send <session> --pane=<index> --force-non-interactive 'prompt'
> ```
>
> `--no-cass-check` skips the lookup entirely; `--force-non-interactive` keeps the
> lookup but auto-declines the confirmation (destructive/ambiguous gates still
> fail closed). `--robot-send` does not run this gate.

**Programmatically, with delivery + acknowledgement (robust path):**

```bash
ntm --robot-send=<session> --panes=<index> --msg='your prompt' --track --timeout=50s
```

`--track` waits for the agent to acknowledge (echo-detected) and returns JSON
with `ack.confirmations[].latency_ms`. This works for **all three** agents,
including OpenCode (targeted by pane). Use it when you need to confirm the
message actually landed rather than fire-and-forget.

## 5. Send `/clear` and other slash commands / skills

A slash command is just text the TUI interprets: send the literal `/command`
string with `ntm send` and the agent handles it. This is how the delegate-task
flow sends `/clear`, `/execute-task <id>`, `/rename`, etc.

```bash
ntm send <session> --pane=<index> '/clear'
```

All three accept `/clear`, but the effect and the surrounding quirks differ:

| Agent           | `/clear` behaviour                                            | Slash commands / skills                                   |
| --------------- | ------------------------------------------------------------- | --------------------------------------------------------- |
| **Claude Code** | Resets context in place; returns to the welcome/empty prompt. | `/clear`, `/<skill>`, `/rename`, any slash command — sent as literal text. |
| **Codex**       | Starts a **new** conversation; prints prior token usage and a `codex resume <id>` line. | `/skills` to list, `/model` to switch; send `/<cmd>` as text. |
| **OpenCode**    | Returns to the "Ask anything…" splash; context dropped.       | Sent as text; an interactive palette also exists (`ctrl+p`), but for automation send the literal command. |

## 6. Readiness and gotchas

- **Confirm an agent is actually live before the first send.** Codex can show a
  blocking launch interstitial — e.g. an *"Update available… 1. Update now / 2.
  Skip"* prompt — and the **Enter** that `ntm send` appends will select the
  default (Update now), which runs `npm install` and drops the pane back to a
  shell, swallowing your message. (A first-run *"Do you trust this directory?"*
  prompt behaves the same way.) Before delegating, verify the pane's `.command`
  is the agent (not `bash`) and read the pane; if an interstitial is showing,
  dismiss it with the *non-default* choice (e.g. `ntm send <session>
  --pane=<index> '2'`) rather than a bare Enter. Keeping the agent CLIs updated
  avoids the update prompt entirely.

- **`ntm --robot-wait` does not track OpenCode.** Waiting for idle
  (`ntm --robot-wait=<session> --wait-until=idle`) reports state for Claude and
  Codex only; OpenCode is absent from the result. To confirm OpenCode is ready
  or done, either inspect the pane (`ntm copy <session>:<index> --last 20
  --quiet --output /dev/stdout`) or use `--robot-send … --track` and rely on the
  ack.

- **Always verify a pane's branch/PR state before delegating** (see the
  delegate-task skill): a pane on a feature branch usually has a task in flight.

- **When in doubt, read the pane.** `ntm copy <session>:<index> --last N
  --quiet --output /dev/stdout` dumps the last N lines so you can see exactly
  what state the agent is in.
