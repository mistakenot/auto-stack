---
hash: "4e488b4b"
id: "31b073f9"
read_when: "implementing auto hooks install or extending the hook event allowlist"
summary: "Requirements for auto hooks install: automatically wiring auto hooks fire as a command hook for a curated telemetry-safe allowlist of Claude Code and Codex events, merging idempotently into existing project-local hook config."
title: "Requirements: Task 020 — Auto Hooks Install"
---

# Task 020: auto-hooks-install

## Problem

`auto hooks fire --agent <claude|codex>` exists and works — it reads a hook
payload on stdin, normalizes it, and best-effort POSTs the event to auto-ui. But
there is no way to *wire* an agent's hook configuration up to call it. Today a
user would have to hand-edit `.claude/settings.json` and Codex config by hand for
every hook event. We need `auto hooks install` to do that wiring automatically,
for both agents, merging into any existing hook config.

## Goals

- Add `auto hooks install` that registers `auto hooks fire` as a command hook on
  every event in a **telemetry-safe allowlist** (see "Event allowlist" below),
  for **both** Claude Code and Codex.

<!-- RESOLVED(P1): "Every supported hook event" is unsafe and inconsistent with the plan
REVIEW: I checked the current Claude hook docs at https://code.claude.com/docs/en/hooks. The event surface is much larger than the 9-event Claude list in solution.md/plan.md: it includes PermissionRequest, PostToolUseFailure, PostToolBatch, PermissionDenied, SubagentStart, TaskCreated, TaskCompleted, StopFailure, ConfigChange, CwdChanged, FileChanged, WorktreeCreate, WorktreeRemove, PostCompact, Elicitation, ElicitationResult, and others. Some are not passive telemetry events: WorktreeCreate replaces Claude's default worktree creation and command hooks must print the created worktree path. Installing fire-and-forget `auto hooks fire --agent claude` there could break worktree sessions. Please either define a telemetry-safe supported-event allowlist in the requirements and change AC-1/AC-4 wording, or extend `auto hooks fire` with event-specific output semantics before installing active-control events.
AUTHOR: Agreed — "every supported event" was the wrong framing. Reworded to a curated telemetry-safe allowlist and added an "Event allowlist" section with the explicit safety criterion: we only install on events where a no-stdout, exit-0 hook is a pure observer (it neither blocks nor substitutes default behavior). `auto hooks fire` always exits 0 and writes nothing to stdout (errors go to stderr), so it is a no-op for these events. Active-control events — Claude's WorktreeCreate/WorktreeRemove, and anything whose hook output replaces default behavior — are deliberately EXCLUDED. AC-1/AC-4 wording updated to "every event in the allowlist". The 9-event Claude list and 10-event Codex list in solution.md already match this criterion; WorktreeCreate was never in them.
-->

- Default scope is **project-local** (write into the current repo's agent config
  dirs). No global (`~/.claude`, `~/.codex`) support in this task.
- **Merge** into existing hook configuration rather than overwriting: preserve
  unrelated settings and any pre-existing hook entries.
- **Idempotent**: running the command twice does not create duplicate entries.
- When an agent in this repo fires any hook, it ends up invoking the `auto`
  binary (`auto hooks fire --agent …`). Verifiable by running an agent in this
  repo after install.

## Background (researched)

**Claude Code** ([code.claude.com/docs/en/hooks](https://code.claude.com/docs/en/hooks)) —
JSON config. Settings files: `~/.claude/settings.json` (user/global),
`.claude/settings.json` (project, committable), `.claude/settings.local.json`
(project, gitignored). Structure:

```json
{
  "hooks": {
    "PostToolUse": [
      { "matcher": "*", "hooks": [ { "type": "command", "command": "auto hooks fire --agent claude" } ] }
    ]
  }
}
```

Each event maps to an array of `{ matcher, hooks: [...] }` groups. A command hook
is `{ "type": "command", "command": "<shell string>" }`. Payload is delivered on
**stdin** as JSON. Matcher `"*"` / `""` / omitted = match all. Claude exposes a
large set of events (PreToolUse, PostToolUse, UserPromptSubmit, Stop,
SubagentStart/Stop, SessionStart, SessionEnd, Notification, PreCompact,
PostCompact, …).

**Codex** ([developers.openai.com/codex/hooks](https://developers.openai.com/codex/hooks)) —
hooks can live in `~/.codex/hooks.json` **or** `~/.codex/config.toml` (and the
project equivalents `<repo>/.codex/hooks.json` / `<repo>/.codex/config.toml`).
Only `type = "command"` is supported. Supported events (10): `SessionStart`,
`SubagentStart`, `PreToolUse`, `PermissionRequest`, `PostToolUse`, `PreCompact`,
`PostCompact`, `UserPromptSubmit`, `SubagentStop`, `Stop`. Payload delivered on
**stdin** as JSON. Same nested `[[hooks.<Event>]]` → `[[hooks.<Event>.hooks]]`
shape as Claude, expressed in JSON (`hooks.json`) or TOML (`config.toml`).

<!-- RESOLVED(P2): Codex project hooks require trust before they run
REVIEW: The current Codex hook docs at https://developers.openai.com/codex/hooks say project-local hooks load only when the project `.codex/` layer is trusted, and non-managed command hooks are skipped until reviewed/trusted via `/hooks`. The goals and AC-5 imply install alone makes Codex fire `auto hooks fire`, but writing `.codex/hooks.json` may only create a pending hook. Add this as an explicit post-install summary/remediation and acceptance step, or narrow AC-5 to direct command simulation instead of actual Codex firing.
AUTHOR: Valid. (1) AC-5 is already scoped to *simulation* — piping a sample payload to the configured command — not to proving Codex auto-fires; clarified the AC text to make that explicit and added a new AC-6 requiring the post-install summary to print the Codex trust remediation. (2) Added a "Codex trust" note below and a goal that the install summary tells the user to run `/hooks` (or trust the project) in Codex before the hooks fire. Verifying Codex's actual auto-fire requires a live trusted Codex session and is out of scope for the automated/dogfood tests.
-->

### Codex trust

Per Codex docs, project-local hooks in `.codex/hooks.json` only run once the
project's `.codex/` layer is **trusted**; until then non-managed command hooks
are pending and skipped. So `auto hooks install` writing the file is necessary
but not sufficient for Codex to fire them — the user must trust the hooks (e.g.
via `/hooks` in a Codex session). The install summary must surface this as a
remediation hint (see AC-6). Claude has no equivalent trust gate for committed
`.claude/settings.json` hooks.

### Event allowlist

"ALL hooks" means **every event on a telemetry-safe allowlist**, not literally
every event each agent exposes. Safety criterion: install only on events where a
hook that writes nothing to stdout and exits 0 is a *pure observer* — it neither
blocks the action nor substitutes for default behavior. `auto hooks fire` meets
this (always exits 0; success output goes nowhere, errors to stderr).

Deliberately **excluded**: active-control events whose hook output replaces or
gates default behavior — e.g. Claude's `WorktreeCreate` / `WorktreeRemove`
(a command hook there must print the worktree path or it breaks the session).
The concrete per-agent lists live in [solution.md](solution.md) → "Event lists"
(Claude: 9 events; Codex: the 10 documented events) and are the single source of
truth, enumerated as Go constants.

## Acceptance Criteria

**AC-1**: Install command exists
- Given: a git repo as the working directory
- When: the user runs `auto hooks install`
- Then: the command registers `auto hooks fire --agent claude` on every event in
  the Claude allowlist and `auto hooks fire --agent codex` on every event in the
  Codex allowlist, in the project-local config files, exiting 0 with a summary of
  what was written.

**AC-2**: Merge, don't clobber
- Given: a `.claude/settings.json` that already contains unrelated keys (e.g.
  `env`) and/or a pre-existing hook entry on one event
- When: the user runs `auto hooks install`
- Then: the existing keys and hook entries are preserved, and the `auto hooks
  fire` entry is added alongside them in each event's hook list.

**AC-3**: Idempotent
- Given: `auto hooks install` has already been run once
- When: the user runs it again
- Then: no duplicate `auto hooks fire` entries are created; the config is
  unchanged (or reports "already installed").

**AC-4**: Both agents covered
- Given: a clean repo with no agent config
- When: the user runs `auto hooks install`
- Then: Claude config (`.claude/...`) and Codex config (`.codex/...`) are both
  created/updated with the fire hook on every event in each agent's allowlist.

**AC-5**: Verified end-to-end in this repo (by simulation)
- Given: this repo (auto-stack) after running `auto hooks install`, with the
  `auto` binary resolvable on `PATH`
- When: the exact configured command string (`auto hooks fire --agent claude`)
  is run with a sample hook payload piped to its stdin
- Then: `auto` resolves via PATH to the built binary and the `hooks fire` path
  runs and exits 0. (This simulates a fired hook; it does not require a live
  Claude/Codex session to actually fire the hook.)

**AC-6**: Codex trust remediation surfaced
- Given: any successful `auto hooks install`
- When: the command prints its summary
- Then: the summary includes a remediation hint that Codex hooks in
  `.codex/hooks.json` must be trusted (e.g. via `/hooks`) before Codex will fire
  them.

## Out of Scope

- Global / user-level install (`~/.claude`, `~/.codex`). Project-local only.
- An uninstall / remove command (can be a follow-up).
- Any change to `auto hooks fire` behavior or the event payload normalization.
- Per-event matcher filtering or selecting a subset of events (we install ALL).
- Supporting agents other than Claude Code and Codex.

## Open Questions

- [x] Claude project file: write to `.claude/settings.json` (committable, shared
  with the team) or `.claude/settings.local.json` (gitignored, per-developer)?
  (answered: **`.claude/settings.json`** — committable, team-wide default.)
- [x] Codex project file format: write `.codex/hooks.json` (JSON — zero new Go
  deps, but a separate file from any existing `config.toml` so won't merge with
  TOML-defined hooks) or `.codex/config.toml` (requires a TOML
  parse/encode dependency to merge safely)? (answered: **`.codex/hooks.json`** —
  JSON, zero new dependencies. Per the project's minimal-runtime-deps rule.)
- [x] How should the hook command reference the binary: bare `auto hooks fire …`
  (relies on `auto` being on PATH) or an absolute path to the currently-running
  executable via `os.Executable()` (robust regardless of PATH)? (answered:
  **bare `auto`** — survives reinstalls/moves; assumes `auto` is on PATH.)
