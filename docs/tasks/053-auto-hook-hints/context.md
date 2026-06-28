# Context: Task 053

Design context for auto-hook-hints — adding hint emission to `auto hooks fire`. See [plan.html](plan.html).

## Key Files

- `auto-cli/cmd/auto/hookscmd.go:53-122` — `newHooksFireCmd()` is the main RunE. Reads stdin payload, appends to durable JSONL log, builds bus event, POSTs to autowatch. Currently prints nothing to stdout. This is where hint emission logic wires in.
- `auto-cli/cmd/auto/hookscmd.go:151-233` — `buildBusEvent()` parses the raw payload, resolves git provenance (worktree, branch, commit, remote), and resolves project ID. Returns a `bus.Event` with all the fields we need for template interpolation.
- `auto-cli/cmd/auto/hookscmd.go:290-298` — `stringField()` helper for safe payload field extraction.
- `auto-cli/cmd/auto/hookscmd_test.go` — existing test suite: `TestBuildBusEvent*`, `TestFireExitsZeroWhenDaemonDown`, `TestFireWritesDurableLog`. Pattern: construct cmd via `newHooksFireCmd()`, set stdin/args, call `cmd.Execute()`, assert stdout/stderr/side effects.
- `auto-cli/cmd/auto/hooksinstallcmd.go:17-42` — `claudeHookEvents` and `codexHookEvents` lists. Already wires both agents.
- `auto-shared/hooks/log.go` — `Envelope` struct and `Append()` function. Used for durable JSONL logging.
- `auto-shared/config/jsonfile.go` — `DecodeJSONFile()`, `WriteJSONFile()` patterns. All existing config is JSON, but `yaml.v3` is available as indirect dep in auto-cli.
- `auto-shared/config/paths.go` — `AutoDir()` returns `~/.auto`, `HomeDir()` returns `$HOME`.

## Patterns

- **Config discovery:** Config files are found relative to a known root: `~/.auto/` for global, project root (via `sharedgit.RepoRoot(cwd)`) for project-local. The `.auto/` directory within a project stores per-tool settings.
- **Never-fail hooks:** `auto hooks fire` MUST exit 0 for any runtime condition. All errors are swallowed or sent to stderr. A hook must never break the agent.
- **Config format:** All existing `.auto/` configs are JSON. YAML is a new format for this project. `gopkg.in/yaml.v3` is available as indirect dep in auto-cli/go.mod.
- **Test pattern:** Tests use `newHooksFireCmd()` directly, set stdin with `cmd.SetIn(strings.NewReader(...))`, capture stdout with a buffer, and assert on output. `t.TempDir()` + `t.Setenv("HOME", ...)` isolates file I/O.
- **Git provenance:** `sharedgit.Provenance(cwd)` returns `(root, branch, commit, err)`. `sharedgit.OriginRemote(cwd)` returns the raw remote URL. Both already called in `buildBusEvent`.

## Hook Response JSON Format

Claude and Codex both accept this JSON on stdout from a hook command (exit 0):

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PostToolUse",
    "additionalContext": "Your hint text here"
  }
}
```

The `additionalContext` field is injected into the agent's context window as developer-visible guidance.

## Related Tasks

- No prior tasks on hook hints. The hooks infrastructure was built as part of the initial auto-cli work.
- Grilling session on 2026-06-28 documented design decisions in `docs/grilling/grilling-log.md`.
