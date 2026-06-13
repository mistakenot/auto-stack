---
hash: "76bb5b44"
id: "11201f9f"
read_when: "implementing auto hooks install or locating the hookscmd registration point and JSON merge patterns"
summary: "Codebase context for the auto hooks install command, with precise references for hookscmd.go, JSON config helpers, git.RepoRoot, the agent hook shapes for Claude and Codex, and the test harness pattern."
title: "Context: Auto Hooks Install (Task 020)"
---

# Context: Task 020

Verified codebase facts grounding the `auto hooks install` design. See
[solution.md](solution.md).

## Key Files

- `auto-cli/cmd/auto/hookscmd.go:47-54` -- `newHooksCmd()` builds the `hooks`
  parent and adds `newHooksFireCmd()`. Add `cmd.AddCommand(newHooksInstallCmd())`
  here at line 52, alongside `fire`.
- `auto-cli/cmd/auto/hookscmd.go:60-89` -- `newHooksFireCmd()` is the existing
  `auto hooks fire --agent <claude|codex>` command this task wires up. It reads a
  hook payload on stdin, normalizes it, and best-effort POSTs to auto-ui. The
  installed hook command string is exactly `auto hooks fire --agent <agent>`.
- `auto-cli/cmd/auto/initcmd.go:44-51` -- pattern for resolving the repo root:
  `cwd, _ := os.Getwd(); repoRoot, err := sharedgit.RepoRoot(cwd)`; on error
  return a remediation-style message. Output uses
  `fmt.Fprintf(cmd.OutOrStdout(), …)` (text, not JSON) at lines 38, 98.
- `auto-shared/config/jsonfile.go` -- JSON helpers:
  - `DecodeJSONFile(path string, target any) error` (line 13) — lenient read.
  - `WriteJSONFile(path string, value any) error` (line 42) — `json.MarshalIndent(v,"","  ")` + `\n`, creates parent dirs.
  - `WriteJSONFileAtomic(path string, value any) error` (line 66) — temp-file + rename, same 2-space format. **Use this for writes.**
- `auto-shared/git/detect.go:13` -- `func RepoRoot(dir string) (string, error)`
  via `git rev-parse --show-toplevel`.
- `.claude/settings.json` -- this repo's existing file is `{"env":{"GOMEMLIMIT":"1GiB"}}`.
  Real merge target for AC-2/AC-5: the `env` key must survive after install.

## Patterns

- **No existing agent-config / hook-merge code** anywhere in the repo (verified
  by grep for `settings.json`, `.claude`, `hooks`, `settings.local`). Greenfield —
  no prior merge pattern to follow; design is in solution.md.
- **JSON convention**: stdlib `encoding/json` only, no comment/ordered-JSON libs.
  All config files use 2-space indent + trailing newline (jsonfile.go:46,71).
- **CLI output**: `docs/auto-package-patterns.md` and CLAUDE.md say JSON-to-stdout
  is the default for data commands, diagnostics to stderr, fail-fast on bad
  usage, deterministic rewrites that "explicitly report each rewritten file", and
  every hard error carries a remediation hint. Action/init-style siblings
  (`init`, `hooks fire`) use plain text via `cmd.OutOrStdout()` — install follows
  that text-summary convention.
- **Build/test**: module `github.com/mistakenot/auto-cli`. Build merged binary:
  `cd auto-cli && go build -o ../bin/auto ./cmd/auto` (or `make build`). Tests:
  `cd auto-cli && go test ./...`. New code lives in `package main` under
  `auto-cli/cmd/auto/` (same package as the test files).
- **Filesystem test harness** (`initcmd_test.go:22-32`, `hookscmd_test.go:85-90`):
  `t.TempDir()` + `git init` in it + `t.Chdir(repo)`; run `cmd.Execute()` with
  `cmd.SetOut/SetErr(io.Discard)`; assert side effects on disk. `t.Setenv("HOME", …)`
  isolates global config reads.

## Agent hook config shapes (researched, external)

Both agents share the JSON shape `{"hooks": {"<Event>": [{"matcher"?, "hooks":
[{"type":"command","command":"…"}]}]}}`; payload arrives on the command's
**stdin** as JSON. Matcher omitted / `""` / `"*"` = match all.
- Claude: `.claude/settings.json` (project, committable) — chosen target.
- Codex: `.codex/hooks.json` (project) — chosen target; `type` must be
  `"command"` (only supported type).

## Related Tasks

- Task 017 (unify-binaries-into-auto): merged all tools under the single `auto`
  binary; `auto hooks` lives in `auto-cli/cmd/auto/`.
- PRs #72/#73 (project registry plumbing): introduced `auto hooks fire` and the
  project registry that `fire` uses to map cwd → project id. This task adds the
  `install` half that points agents at `fire`.
