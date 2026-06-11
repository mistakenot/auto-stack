package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	sharedconfig "github.com/mistakenot/auto-shared/config"
	sharedgit "github.com/mistakenot/auto-shared/git"
	"github.com/spf13/cobra"
)

// claudeHookEvents is Claude Code's documented stable hook event set. "Install
// all hooks" means wiring the fire command onto every event in this list.
// Source: code.claude.com/docs/en/hooks.
var claudeHookEvents = []string{
	"PreToolUse",
	"PostToolUse",
	"UserPromptSubmit",
	"Notification",
	"Stop",
	"SubagentStop",
	"SessionStart",
	"SessionEnd",
	"PreCompact",
}

// codexHookEvents is Codex's documented hook event set.
// Source: developers.openai.com/codex/hooks.
var codexHookEvents = []string{
	"SessionStart",
	"SubagentStart",
	"PreToolUse",
	"PermissionRequest",
	"PostToolUse",
	"PreCompact",
	"PostCompact",
	"UserPromptSubmit",
	"SubagentStop",
	"Stop",
}

// installAgentHooks merges a `{"type":"command","command":command}` handler onto
// every event in `events` within the JSON file at `path`, then writes the file
// back. It operates on a fully generic map[string]any tree so EVERY existing
// field survives a round-trip untouched — both unknown top-level keys (e.g.
// `env`) and unknown fields on existing handlers/groups (`timeout`,
// `statusMessage`, `args`, `if`, `commandWindows`, …). Narrowing into typed
// structs would silently drop those fields and violate AC-2.
//
// It is idempotent: an event that already carries a handler with the same
// command is left alone and counted in `existing`. Returns how many handlers
// were newly added vs already present, and whether the file was created fresh.
func installAgentHooks(path, command string, events []string) (added, existing int, created bool, err error) {
	// Read the existing file. A missing file is the normal "first install"
	// case → start from an empty tree and report created=true. Any other read
	// or parse error is real and surfaces to the caller.
	doc := map[string]any{}
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		if err := json.Unmarshal(data, &doc); err != nil {
			return 0, 0, false, fmt.Errorf("parse %s: %w", path, err)
		}
		created = false
	case os.IsNotExist(readErr):
		created = true
	default:
		return 0, 0, false, fmt.Errorf("read %s: %w", path, readErr)
	}

	// Get or create the top-level "hooks" map. If an existing value is present
	// but the wrong type, treat it leniently: replace it with a fresh map so we
	// don't panic (matching the codebase's tolerant parse style).
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		hooks = map[string]any{}
		doc["hooks"] = hooks
	}

	for _, event := range events {
		// Each event maps to a slice of groups. Tolerate an unexpected type by
		// treating it as an empty slice (no matching handler), then proceeding.
		groups, _ := hooks[event].([]any)

		if handlerExists(groups, command) {
			existing++
			continue
		}

		// Append our own group rather than reconciling into an existing one:
		// matcher omitted = match all. Simpler and equally correct.
		groups = append(groups, map[string]any{
			"hooks": []any{
				map[string]any{"type": "command", "command": command},
			},
		})
		hooks[event] = groups
		added++
	}

	if err := sharedconfig.WriteJSONFileAtomic(path, doc); err != nil {
		return 0, 0, false, err
	}
	return added, existing, created, nil
}

// handlerExists reports whether any group in groups already contains a handler
// with type=="command" and command==target. It is defensive about malformed
// nodes: anything that isn't the expected shape simply doesn't match.
func handlerExists(groups []any, target string) bool {
	for _, g := range groups {
		group, ok := g.(map[string]any)
		if !ok {
			continue
		}
		handlers, ok := group["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range handlers {
			handler, ok := h.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := handler["type"].(string)
			cmd, _ := handler["command"].(string)
			if typ == "command" && cmd == target {
				return true
			}
		}
	}
	return false
}

// newHooksInstallCmd implements `auto hooks install`. It wires the
// `auto hooks fire --agent <agent>` command into the project-local config for
// both Claude (.claude/settings.json) and Codex (.codex/hooks.json), on every
// documented hook event, preserving any existing config. The command is bare
// `auto` and relies on PATH so the install stays portable across binary moves.
func newHooksInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Wire `auto hooks fire` into project-local Claude and Codex hook config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			root, err := sharedgit.RepoRoot(cwd)
			if err != nil {
				return fmt.Errorf("auto hooks install requires a git repository: %w (run 'git init' or cd into a repo)", err)
			}

			out := cmd.OutOrStdout()

			claudePath := filepath.Join(root, ".claude", "settings.json")
			added, existing, createdClaude, err := installAgentHooks(claudePath, "auto hooks fire --agent claude", claudeHookEvents)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "%s %s: %d hook(s) added, %d already present\n",
				verbFor(createdClaude), claudePath, added, existing)

			codexPath := filepath.Join(root, ".codex", "hooks.json")
			added, existing, createdCodex, err := installAgentHooks(codexPath, "auto hooks fire --agent codex", codexHookEvents)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "%s %s: %d hook(s) added, %d already present\n",
				verbFor(createdCodex), codexPath, added, existing)

			// AC-6: Codex won't run hooks from .codex/hooks.json until the user
			// trusts them; surface the remediation so the install isn't silently
			// inert.
			fmt.Fprintf(out, "Note: Codex hooks in %s must be trusted via `/hooks` in Codex before they fire.\n", codexPath)
			return nil
		},
	}
}

// verbFor returns a human-readable verb describing whether a file was created
// fresh or merged into an existing one.
func verbFor(created bool) string {
	if created {
		return "Created"
	}
	return "Merged"
}
