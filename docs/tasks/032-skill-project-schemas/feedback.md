# Feedback: Task 032

## Problems faced
1. Pre-commit hook race in shared worktree -- Phase 1 and Phase 2 ran concurrently; goimports auto-staged Phase 2's untracked files into Phase 1's commit. All code landed correctly but commit attribution was wrong.
2. golang.org/x/term dependency -- Added for TTY detection, then realized it violates zero-new-deps rule. Had to revert go.mod/go.sum and switch to stdlib `os.ModeCharDevice`.
3. perfsprint lint failures -- `fmt.Sprintf` for simple string concat and `fmt.Errorf` without format verbs both flagged. Required `errors.New` and string concat instead.

## Reflections
- Shared-worktree concurrent subagents are fundamentally racy. The pre-commit hook's goimports formats ALL Go files (not just staged), so untracked files from parallel phases get swept into the wrong commit. Serial dispatch is the only safe approach.
- Check the dependency policy before adding imports. The x/term revert was avoidable — `os.Stdin.Stat()` with `ModeCharDevice` is a one-liner that does the same thing.
- The doctor command's `project_settings` check had to be updated to `skills_yaml` after the schema change. Integration tests caught this immediately — good coverage.

## Useful context
- `config.HomeDir()` prefers `$HOME` over `os.UserHomeDir()`, and `GlobalSettingsPath()` uses `Root` when `RootOverride` is true — this means `--root` flag controls where global settings go in tests.
- `yaml.Decoder.KnownFields(true)` is the YAML equivalent of `json.Decoder.DisallowUnknownFields()` for strict parsing.
- Codex bot raised 4 P2 suggestions worth tracking as follow-ups: accept explicit flags without -y, interactive wizard prompts, lock version_spec validation, and surfacing agent snippet write errors.
