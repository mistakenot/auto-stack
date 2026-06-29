# Delegating to ntm agents

How to find agents running under `ntm` (Named Tmux Manager) and drive each of
the coding-agent CLIs — **Claude Code**, **Codex**, **OpenCode**, and **Grok**
— without knowing in advance which is in which pane.

For **headless review delegation** (no tmux pane — run from bash and exit), use
the `request-claude-review`, `request-codex-review`, or `request-grok-review`
skills. Each invokes its CLI in print/headless mode with `/review-task`, then
hands off to `resolve-comments`.

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

## 4. Spin up a new worker when none are available

When `ntm status` shows **no eligible pane** — every agent is busy, on a feature
branch with a task in flight, or the session only has a `user` shell — add a
fresh Claude Code pane instead of giving up:

```bash
ntm add <session> --cc=1 --json
```

> **Respect the ceiling.** Before adding, count the Claude panes. A session is
> capped at **6** Claude panes (see [§9](#9-reclaiming-panes-garbage-collection)).
> If it is already at the ceiling and nothing is eligible, every worker is
> genuinely busy — report and wait/re-check, do **not** add a seventh pane.

Then bring it online before sending (verified flow):

1. **Re-discover the pane index from `ntm status` — do not trust `ntm add`'s
   output.** The `new_panes[].index` field in `ntm add --json` is unreliable: it
   reported `0` while the real new pane was index `1`. Re-query and pick the
   newly-added Claude pane (the `.command == "claude"` pane that wasn't there
   before — in practice the highest-index `claude` pane):

   ```bash
   ntm status <session> --json
   ```

2. **Wait until it is ready for input.** `.command` flips to `claude` within
   ~2s, but the TUI is not ready yet. Block on idle:

   ```bash
   ntm --robot-wait=<session> --wait-until=idle --timeout=60s
   ```

   It returns when the agent reports `state: WAITING` (ready). If `robot-wait`
   is unavailable, read the pane and confirm the `❯` input prompt is showing:

   ```bash
   ntm copy <session>:<index> --last 20 --quiet --output /dev/stdout
   ```

3. Proceed with `/clear` + send as normal, targeting the new pane explicitly
   with `--pane=<index>` (not `--smart`).

Add **one** worker per dispatch — never spawn repeatedly in a loop. If `ntm add`
fails, report the error and stop.

## 5. Send text to an agent

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

## 6. Send `/clear` and other slash commands / skills

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

> **Do NOT reset a pane with in-place process restart.** It is tempting to
> replace the cooperative `/clear` with an "imperative" restart of the agent
> process. In this environment (verified on **ntm 1.18.2**, 2026-06-27) all three
> restart paths are broken and **must not be used to reset a pane for reuse**:
>
> - `ntm --robot-smart-restart=<s> --panes=<n>` → `FAILED` with
>   `can't find window: <n>` — it addresses the flat pane index as a tmux *window*
>   index. `--hard-kill` fails the same way (`tmux list-panes failed`).
> - `ntm respawn --panes=<n> --force` → kills the agent, but the relaunched
>   `claude` **crashes back to a bash shell** (the re-run launch line reuses a
>   `systemd-run --scope` whose name the exiting process still holds).
> - `ntm --robot-restart-pane=<s> --panes=<n>` → reports
>   `success / prompt_sent / process_alive:true` **even though** the agent crashed
>   to a shell, and sends the `--restart-prompt` **into that shell** where it runs
>   as a shell command. A silent failure that misroutes the dispatch — strictly
>   worse than `/clear`.
>
> The only reliable way to get a clean-context agent is to **spawn a fresh pane**
> ([§4](#4-spin-up-a-new-worker-when-none-are-available)) — a new `claude` with
> empty context and its own scope (verified: a fresh pane answers `NO_CONTEXT` to
> a prior pane's codeword). Re-verify with that codeword check if a future ntm
> release claims to fix in-place restart.

## 7. Readiness and gotchas

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

## 8. Headless CLI delegation (Claude, Codex, Grok)

When you need a second agent to review task docs without an interactive tmux
pane, invoke the matching CLI headlessly from bash. All three follow the same
shape: set cwd, send `/review-task <folder>`, capture output, count comments,
then run `/resolve-comments` in the coordinator.

| Agent | Headless command | Stdin gotcha | Auto-approve flags |
| ----- | ---------------- | ------------ | ------------------ |
| **Claude Code** | `claude -p --add-dir "$CWD" --dangerously-skip-permissions "/review-task …"` | **Requires** `< /dev/null` — inherited open stdin stalls ~3s or blocks in background | `--dangerously-skip-permissions` |
| **Codex** | `codex exec --cd "$CWD" --sandbox workspace-write "…"` | **Requires** `< /dev/null` — blocks on open stdin with "Reading additional input from stdin…" | `--sandbox workspace-write` (writes task docs) |
| **Grok** | `grok --cwd "$CWD" --permission-mode bypassPermissions --always-approve --single "/review-task …"` | **No redirect needed** — headless mode ignores piped stdin | `--permission-mode bypassPermissions --always-approve` |

Grok discovers skills from `.agents/skills/` (same tree `npx skills install`
writes for Codex). Ensure `review-task` is installed before delegating.
`--single` (short form `-p`) takes the prompt as its immediate value; never put
other flags between it and the prompt.

Auth: Claude uses `~/.claude/.credentials.json`; Codex uses `codex login`;
Grok uses `~/.grok/auth.json` or `XAI_API_KEY`.

## 9. Reclaiming panes (garbage collection)

On-demand workers (§4) accumulate: after a burst of parallel tasks finishes, the
session is left holding idle Claude panes forever (each is a full `claude`
process with a ~7.8 GB `MemoryMax`). Reclaim the surplus so the pool shrinks back
toward a warm floor.

**Policy — shared across delegate, delegate-task, and
status-report:**

- **Warm floor = 4** — never reap below 4 Claude panes, so the next dispatch
  reuses one instantly instead of paying spawn + `robot-wait` latency. (The
  floor is a *don't-shrink-below* line, not a target — panes aren't spawned
  eagerly to reach it.)
- **Ceiling = 6** — never let a session exceed 6 Claude panes. At the ceiling
  with everything busy, dispatch **waits / reports** instead of adding (§4).
- **GC runs in status-report**, which already inspects every pane.

**A pane is reapable only when it is idle on `main` with no open PR** — exactly
the dispatch-eligibility check. Never reap a pane that is in progress, stuck,
completed-with-open-PR, or on a feature branch.

**Mechanism — LIFO-safe scale-down.** `ntm scale <session> --cc=N` reaps the
**highest-index (newest) panes first** and is **not busy-aware**, so point it
only at panes already confirmed idle:

1. From `ntm status <session> --json`, count the Claude panes (`cc_total`) and
   classify each (idle-on-main vs busy / feature-branch / open-PR).
2. Walk the Claude panes from the **highest index downward** and count the
   **contiguous run** that are idle-on-main — call it `reapable_top`. Stop at the
   first busy pane: scale would kill it before reaching idle panes beneath it.
3. `to_reap = max(0, min(reapable_top, cc_total - 4))`.
4. If `to_reap > 0`, re-read those top panes to confirm they are *still* idle
   (guard against one picking up work since step 1), then scale down:
   ```bash
   ntm scale <session> --cc=$((cc_total - to_reap)) --force --json
   ```
5. Report which panes were reaped and which idle panes were **kept** and why
   (floor reached, or a busy pane sitting above them).

If a busy pane occupies the highest index, `reapable_top` is 0 and nothing is
reaped that cycle — idle panes lower in the stack are left alone rather than risk
killing the busy one. They get reclaimed on a later cycle once the top frees up.
