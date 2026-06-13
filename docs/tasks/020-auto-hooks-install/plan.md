---
hash: "6bebadeb"
id: "6821afa4"
read_when: "implementing or reviewing the auto hooks install command plan and hook-merging logic"
summary: "Implementation plan for the auto hooks install subcommand: merging fire hook commands into .claude/settings.json and .codex/hooks.json idempotently for all documented hook events."
title: "Plan: Task 020 — Auto Hooks Install"
---

# Plan: Task 020

## Summary

Add an `auto hooks install` subcommand that merges `auto hooks fire --agent <agent>`
command hooks into project-local `.claude/settings.json` and `.codex/hooks.json`
for every documented hook event, preserving existing keys and staying idempotent.

## Changes

| Symbol | File | Description |
|--------|------|-------------|
| + | `auto-cli/cmd/auto/hooksinstallcmd.go` | `newHooksInstallCmd`, `installAgentHooks` merge helper, `hookHandler`/`hookGroup` types, `claudeHookEvents`/`codexHookEvents` constants |
| + | `auto-cli/cmd/auto/hooksinstallcmd_test.go` | Unit + cobra-e2e tests (merge, idempotency, both agents, preserve env) |
| ~ | `auto-cli/cmd/auto/hookscmd.go` | Register `newHooksInstallCmd()` in `newHooksCmd()` (line 52) |
| ~ | `.claude/settings.json` | Dogfood: gains the fire hook by running install in-repo (AC-5) |

## Links
- [Requirements](./requirements.md)
- [Solution](./solution.md)
- [Context](./context.md)

## How to Test
- [x] `auto-cli/cmd/auto/hooksinstallcmd_test.go` — unit: `installAgentHooks` adds/merges/dedupes; cobra-e2e: `auto hooks install` writes both files in a temp git repo
- [x] Manual (AC-5): run `auto hooks install` in this repo; `cat .claude/settings.json`; pipe a sample payload to `auto hooks fire --agent claude`, assert exit 0

## Execution Sequence
```
Phase 1 (Implement) --> Phase 2 (Tests) --> Phase 3 (Dogfood verify)
```
Linear DAG: each phase depends on the previous. Single small command, no parallelism needed.

## Plan

### Phase 1: Implement the install command
- [x] Step 1.1: Create `auto-cli/cmd/auto/hooksinstallcmd.go` (package `main`) with:
  - `claudeHookEvents = []string{"PreToolUse","PostToolUse","UserPromptSubmit","Notification","Stop","SubagentStop","SessionStart","SessionEnd","PreCompact"}`.
  - `codexHookEvents = []string{"SessionStart","SubagentStart","PreToolUse","PermissionRequest","PostToolUse","PreCompact","PostCompact","UserPromptSubmit","SubagentStop","Stop"}`.
  - `installAgentHooks(path, command string, events []string) (added, existing int, created bool, err error)` — operates on a **generic `map[string]any` tree** (NO typed `hookHandler`/`hookGroup` parse structs — those would drop existing fields like `timeout`/`statusMessage`/`args`/`if`/`commandWindows` and violate AC-2):
    - Read file into `map[string]any` (missing → empty map, `created=true`; existing → `created=false`). `os.ReadFile` + `json.Unmarshal`; treat `os.IsNotExist` as empty, return other read/parse errors.
    - Get/create `doc["hooks"]` as `map[string]any`. For each event, get `hooks[event]` as `[]any` of groups.
    - Idempotency: scan each group's `["hooks"]` (`[]any`) for a handler (`map[string]any`) with `["type"]=="command" && ["command"]==command`; if found `existing++`, else append group `map[string]any{"hooks": []any{map[string]any{"type":"command","command":command}}}` and `added++`.
    - Write the whole `map[string]any` via `sharedconfig.WriteJSONFileAtomic(path, doc)` (encoding/json sorts keys → deterministic; all untouched nodes round-trip losslessly).
  - `newHooksInstallCmd()`: `Args: cobra.NoArgs`; resolve `cwd, _ := os.Getwd()` then `sharedgit.RepoRoot(cwd)` — on error return `fmt.Errorf("auto hooks install requires a git repository: %w (run 'git init' or cd into a repo)", err)`. Call `installAgentHooks` for claude (`<root>/.claude/settings.json`, `auto hooks fire --agent claude`) and codex (`<root>/.codex/hooks.json`, `auto hooks fire --agent codex`). Print a per-file text summary to `cmd.OutOrStdout()` (created vs merged, added/already-present counts) **and a Codex trust remediation line** (AC-6): note that `.codex/hooks.json` hooks must be trusted via `/hooks` in Codex before they fire. Return any error.
  - Verify: `cd auto-cli && go build ./...` compiles clean.
- [x] Step 1.2: Edit `auto-cli/cmd/auto/hookscmd.go:52` — add `cmd.AddCommand(newHooksInstallCmd())` in `newHooksCmd()`. Update the `newHooksCmd` doc comment to mention `install`.
  - Verify: `cd auto-cli && go build ./... && go run ./cmd/auto hooks install --help` shows the install subcommand.
- [x] Step 1.3: `cd auto-cli && go vet ./... && gofmt -l cmd/auto` (no output = formatted).
- [x] Step 1.4: Commit: `feat(020): auto hooks install — wire fire hook into claude+codex config`

### Phase 2: Tests
- [x] Step 2.1: Create `auto-cli/cmd/auto/hooksinstallcmd_test.go` (package `main`). Helper: temp dir + `git init` (reuse the `gitInTest`/init-test pattern) + `t.Chdir(repo)`.
  - `TestInstallWritesBothAgents` (AC-1, AC-4): run `newHooksInstallCmd()` via `cmd.Execute()`; assert `.claude/settings.json` and `.codex/hooks.json` exist and each contains a `command` handler `auto hooks fire --agent <agent>` on every event in the respective constant list.
  - `TestInstallPreservesExistingKeysAndHooks` (AC-2): pre-seed `.claude/settings.json` with `{"env":{"GOMEMLIMIT":"1GiB"},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo existing","statusMessage":"hi","timeout":30,"args":["x"],"if":"Bash(git *)"}]}]}}`; run install; assert `env` survives, the `echo existing` handler is retained **with all four extra fields (`statusMessage`, `timeout`, `args`, `if`) intact** (proves lossless generic-tree merge), and the fire handler is added alongside it on `Stop`.
  - `TestInstallIdempotent` (AC-3): run install twice; assert exactly one fire handler per event and second run reports 0 added (e.g. assert via `installAgentHooks` return counts on a unit call).
  - `TestInstallRejectsNonRepo`: run in a non-git temp dir; assert error mentions git repository.
  - `TestInstallSummaryHasCodexTrustHint` (AC-6): capture `cmd.OutOrStdout()`; assert it mentions trusting `.codex/hooks.json` / `/hooks`.
  - Unit `TestInstallAgentHooksCounts`: call `installAgentHooks` directly on a temp path; first call `added==len(events), created==true`; second call `existing==len(events), added==0, created==false`.
- [x] Step 2.2: Run `cd auto-cli && go test ./cmd/auto/...` — all pass.
- [x] Step 2.3: Commit: `test(020): cover merge, idempotency, both agents, repo guard`

### Phase 3: Dogfood verification (AC-5)
- [x] Step 3.1: Build: `make build` (produces `./bin/auto`).
- [x] Step 3.2: Run `./bin/auto hooks install` in this repo. Verify: command exits 0 and prints a summary naming `.claude/settings.json` and `.codex/hooks.json`.
- [x] Step 3.3: Inspect `.claude/settings.json` — verify the original `env`/`GOMEMLIMIT` key is intact AND a `hooks` block now contains `auto hooks fire --agent claude` on each Claude event. Verify `.codex/hooks.json` was created with the codex events.
- [x] Step 3.4 (AC-5): Simulate a fired hook **through PATH resolution of the exact configured command** (not by calling `./bin/auto` directly). Put the built binary on PATH and run the bare command from the config: `export PATH="$(pwd)/bin:$PATH"; command -v auto` (verify it points at `$(pwd)/bin/auto`), then `echo '{"hook_event_name":"PostToolUse","cwd":"'$(pwd)'","tool_name":"Edit"}' | sh -c 'auto hooks fire --agent claude'` — verify exit 0. This proves the installed bare `auto hooks fire …` string resolves, which `./bin/auto …` would not.
- [x] Step 3.5 (AC-6): Verify the `auto hooks install` summary from Step 3.2 included the Codex trust remediation hint (mentions `.codex/hooks.json` must be trusted, e.g. via `/hooks`).

<!-- RESOLVED(P2): AC-5 test bypasses the installed command string
REVIEW: AC-5 requires proving the configured bare command `auto hooks fire --agent claude` resolves to the `auto` binary. Step 3.4 runs `./bin/auto hooks fire...` directly, so it would pass even when `auto` is not on PATH and the installed hook command would fail. Verify the command from the config itself, for example build `./bin/auto`, set `PATH="$(pwd)/bin:$PATH"` or otherwise ensure `command -v auto` is the expected binary, then pipe the sample payload through `sh -c 'auto hooks fire --agent claude'` or through the exact JSON `command` value.
AUTHOR: Fixed. Step 3.4 now puts `$(pwd)/bin` on PATH, asserts `command -v auto` resolves to the built binary, and runs the sample payload through `sh -c 'auto hooks fire --agent claude'` — the exact bare command string the installer writes — so PATH resolution is actually exercised. Updated AC-5 in requirements.md to state the PATH precondition explicitly.
-->

- [x] Step 3.6: Commit: `chore(020): install fire hooks in-repo (dogfood)` (includes updated `.claude/settings.json` and new `.codex/hooks.json`).

## Success Criteria
- [x] `cd auto-cli && go build ./...` and `go vet ./...` pass; `gofmt -l cmd/auto` is empty.
- [x] `cd auto-cli && go test ./cmd/auto/...` passes, covering AC-1 through AC-4 and AC-6.
- [x] `auto hooks install` is a registered subcommand under `auto hooks` (visible in `--help`).
- [x] AC-2 verified: `.claude/settings.json`'s `env` key and any pre-existing hook (incl. extra handler fields `statusMessage`/`timeout`/`args`/`if`) survive a merge.
- [x] AC-3 verified: running install twice produces no duplicate fire handlers.
- [x] AC-5 verified: after in-repo install, with `$(pwd)/bin` on PATH, piping a sample payload through `sh -c 'auto hooks fire --agent claude'` (the exact configured command) exits 0.
- [x] AC-6 verified: install summary prints the Codex trust remediation hint.

## Open Questions
- (none — all resolved in requirements.md)
