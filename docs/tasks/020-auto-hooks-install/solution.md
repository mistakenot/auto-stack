# Solution: Task 020

## Approach

A single new subcommand, `auto hooks install`, that writes the project-local
config for both agents. Both target files use the **same JSON hook shape**
(`{ "hooks": { "<Event>": [ { "matcher"?, "hooks": [ { "type":"command",
"command":"…" } ] } ] } }`), so one merge routine handles both, parameterized by
(file path, agent name, event list).

1. **Resolve repo root.** Get cwd, call `sharedgit.RepoRoot(cwd)`. If not a git
   repo, fail fast with a remediation hint (`run 'git init' or cd into a repo`).
   Project-local is the only scope (global is out of scope), so no flag is
   required to select it.

2. **For each agent**, compute:
   - target file: `<root>/.claude/settings.json` (claude),
     `<root>/.codex/hooks.json` (codex)
   - command string: `auto hooks fire --agent claude` / `… --agent codex`
     (bare `auto`, relies on PATH — per resolved open question)
   - event list: a curated constant of that agent's documented hook events
     (see "Event lists" below). "ALL hooks" = every event in this constant.

3. **Merge, preserving everything else.** Parse the *entire* file as generic
   JSON (`map[string]any`) so every existing field survives untouched — both
   unknown top-level keys (`env`, `GOMEMLIMIT` in this repo's
   `.claude/settings.json`) **and** unknown fields on existing hook
   handlers/groups (`timeout`, `statusMessage`, `args`, `if`, `async`, `shell`,
   `commandWindows`, …). Navigate generically: `doc["hooks"]` (map) →
   `[event]` (slice of groups) → each group's `["hooks"]` (slice of handlers).
   For each event in the list, scan handlers for one with `type=="command"` and
   `command==<target>`; if present, skip (idempotent); else append a new group
   `{"hooks": [{"type":"command","command":<target>}]}` (matcher omitted = match
   all). Because existing nodes are never narrowed into typed structs, no fields
   are ever dropped. Re-marshal the whole `map[string]any` and write — Go's
   `encoding/json` sorts map keys, so output is deterministic.

<!-- RESOLVED(P1): Typed hook structs will clobber valid existing hook fields
REVIEW: The top-level raw map preserves keys like `env`, but this design then unmarshals `"hooks"` into `map[string][]hookGroup` with handlers containing only `type` and `command`. Both current hook schemas allow additional fields on existing handlers/groups: Claude command hooks can use `args`, `timeout`, `statusMessage`, `if`, `async`, and `shell`; Codex examples include `statusMessage`, `timeout`, and `commandWindows`. Re-marshaling the typed `hooks` map would drop those fields and violate AC-2's requirement that existing hook entries are preserved. Use raw-preserving structs/maps for groups and handlers, or manipulate the hook tree as `map[string]json.RawMessage`/generic JSON so unknown fields survive; add a test seeded with at least `statusMessage`, `timeout`, and `args`/`if`.
AUTHOR: Correct and important. Redesigned the merge to operate on a fully generic `map[string]any` tree — existing handlers/groups are never narrowed into lossy typed structs, so all extra fields (`timeout`, `statusMessage`, `args`, `if`, `async`, `shell`, `commandWindows`) are preserved byte-for-byte through round-trip. Updated the code outline (dropped the typed `hookHandler`/`hookGroup` parse structs), the Rejected Alternatives, and added an AC-2 test seeded with `statusMessage`, `timeout`, `args`, and `if` on a pre-existing handler asserting they survive. Determinism holds because `encoding/json` marshals map keys in sorted order.
-->

4. **Write deterministically.** Use `sharedconfig.WriteJSONFileAtomic` (2-space
   indent, trailing newline, creates parent dirs like `.claude/`, `.codex/`).
   Iterate events in a fixed order so output is stable.

5. **Report each file + Codex trust hint.** Text summary to `cmd.OutOrStdout()`
   (matching the `init`/`hooks fire` siblings): per file, whether it was created
   vs merged, how many events were newly wired vs already present. Then print a
   remediation line (AC-6): Codex hooks in `.codex/hooks.json` must be trusted
   (e.g. via `/hooks` in Codex) before they fire. Exit 0.

### Event lists (the meaning of "ALL")

Enumerated as Go constants — the single knob to update when an agent adds
events. Sources: Claude [code.claude.com/docs/en/hooks](https://code.claude.com/docs/en/hooks),
Codex [developers.openai.com/codex/hooks](https://developers.openai.com/codex/hooks).

- **Claude** (documented stable set): `PreToolUse`, `PostToolUse`,
  `UserPromptSubmit`, `Notification`, `Stop`, `SubagentStop`, `SessionStart`,
  `SessionEnd`, `PreCompact`.
- **Codex** (the 10 documented events): `SessionStart`, `SubagentStart`,
  `PreToolUse`, `PermissionRequest`, `PostToolUse`, `PreCompact`, `PostCompact`,
  `UserPromptSubmit`, `SubagentStop`, `Stop`.

Unknown/extra event keys are harmless (agents ignore them), but we keep the list
to the documented set so the written config stays clean and deterministic.

### Shared types & helper (code outline)

In `auto-cli/cmd/auto/hooksinstallcmd.go` (package `main`). The merge operates on
a **generic JSON tree** (`map[string]any`) — no typed parse structs for existing
content, so unknown handler/group fields survive.

```go
var claudeHookEvents = []string{ /* PreToolUse, PostToolUse, … */ }
var codexHookEvents  = []string{ /* SessionStart, SubagentStart, … */ }

func newHooksInstallCmd() *cobra.Command // RunE: resolve root, install both agents

// installAgentHooks merges a `type:command` handler with `command` onto every
// event in `events` within the JSON file at `path`, operating on a generic
// map[string]any so ALL existing fields (top-level keys, and handler/group
// fields like timeout/statusMessage/args/if/commandWindows) are preserved.
// Returns counts of (added, alreadyPresent) and whether the file was created.
func installAgentHooks(path, command string, events []string) (added, existing int, created bool, err error)

// handlerExists reports whether any group under doc.hooks[event] already has a
// handler with type=="command" && command==target (drives idempotency).
// appendHandlerGroup adds {"hooks":[{"type":"command","command":target}]} to
// doc.hooks[event], creating the hooks map / event slice as needed.
```

Registered via `cmd.AddCommand(newHooksInstallCmd())` in `newHooksCmd()`
(hookscmd.go:52), as a sibling of `fire`.

## Files

```
+ auto-cli/cmd/auto/hooksinstallcmd.go       # newHooksInstallCmd + merge helper + event constants
+ auto-cli/cmd/auto/hooksinstallcmd_test.go  # unit + e2e tests for merge/idempotency/both-agents
~ auto-cli/cmd/auto/hookscmd.go              # register install subcommand in newHooksCmd()
~ .claude/settings.json                      # dogfood: gains the fire hook (created by running install here, AC-5)
```

## Test Coverage

| AC   | Test Type   | File                                          |
|------|-------------|-----------------------------------------------|
| AC-1 | e2e (cobra) | auto-cli/cmd/auto/hooksinstallcmd_test.go     |
| AC-2 | unit        | auto-cli/cmd/auto/hooksinstallcmd_test.go     |
| AC-3 | unit        | auto-cli/cmd/auto/hooksinstallcmd_test.go     |
| AC-4 | e2e (cobra) | auto-cli/cmd/auto/hooksinstallcmd_test.go     |
| AC-5 | manual+test | run install in-repo, put `bin/` on PATH, pipe sample payload through `sh -c 'auto hooks fire …'` |
| AC-6 | unit        | auto-cli/cmd/auto/hooksinstallcmd_test.go (assert summary has trust hint) |

Test patterns (from `initcmd_test.go`): `t.TempDir()` + `git init` + `t.Chdir`,
run via `cmd.Execute()` with `io.Discard`, then assert on-disk JSON.
- AC-2: seed `.claude/settings.json` with `{"env":{...}}` + a pre-existing hook
  on one event whose handler carries **extra fields** (`statusMessage`,
  `timeout`, `args`, `if`); after install assert `env` survives AND the
  pre-existing handler is retained with all four extra fields intact (proves the
  generic-tree merge is lossless).
- AC-3: run install twice; assert no duplicate `auto hooks fire` handlers and the
  second run reports 0 added.

## Out of Scope

- Global / user-level install (`~/.claude`, `~/.codex`).
- Uninstall / remove command.
- Merging into Codex `config.toml` (we write `.codex/hooks.json`; TOML-defined
  hooks are a separate file and are not touched — accepted tradeoff, zero deps).
- Selecting a subset of events or per-event matchers (we wire the full list).
- JSON output mode / `--json` flag (text summary only for now).
- Any change to `auto hooks fire` or event normalization.

## Rejected Alternatives

- **Typed structs for settings.json (top-level *or* hook handlers/groups)**:
  would drop unmodeled fields on re-marshal — top-level keys like `env`, and
  per-handler fields like `timeout`/`statusMessage`/`args`/`if`/`commandWindows`
  on *existing* hooks — violating AC-2. Rejected in favor of a fully generic
  `map[string]any` tree that round-trips every field losslessly.
- **`.codex/config.toml` merge**: requires a TOML parse+encode dependency,
  conflicting with the minimal-runtime-deps rule. Rejected per resolved open
  question (use `.codex/hooks.json`).
- **Absolute `os.Executable()` path in the command**: breaks if the binary is
  moved/reinstalled. Rejected per resolved open question (bare `auto`).
- **Appending our handler into an existing `"*"` group**: more complex matcher
  reconciliation for no benefit; adding our own group is simpler and equally
  correct. Rejected.
