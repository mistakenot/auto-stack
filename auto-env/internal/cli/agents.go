package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const autoenvSnippet = `**auto env** — Standalone dev environments for worktree branches. Run ` + "`auto env quickstart`" + ` to learn how to stand up an isolated environment.`

var agentFiles = []string{"CLAUDE.md", "AGENTS.md", "GEMINI.md"}

func newAgentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agents",
		Short: "Register auto env in local agent memory files",
		Long:  "Appends a one-line description of auto env to CLAUDE.md, AGENTS.md, and GEMINI.md if not already present. Idempotent and symlink-safe.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("getting working directory: %w", err)}
			}

			seen := make(map[string]bool)

			for _, name := range agentFiles {
				p := filepath.Join(cwd, name)

				resolved, err := filepath.EvalSymlinks(p)
				if err == nil {
					if seen[resolved] {
						fmt.Fprintf(cmd.OutOrStdout(), "%s: skipped (symlink to already-processed file)\n", name)
						continue
					}
					seen[resolved] = true
				}

				updated, err := ensureEnvSnippet(p)
				if err != nil {
					return &ExitError{Code: 1, Err: fmt.Errorf("updating %s: %w", name, err)}
				}
				if updated {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: updated\n", name)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: already present\n", name)
				}
			}
			return nil
		},
	}
}

func ensureEnvSnippet(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, os.WriteFile(path, []byte(autoenvSnippet+"\n"), 0o644)
		}
		return false, err
	}

	content := string(data)
	if strings.Contains(content, autoenvSnippet) {
		return false, nil
	}

	if !strings.HasSuffix(content, "\n") && len(content) > 0 {
		content += "\n"
	}
	content += "\n" + autoenvSnippet + "\n"
	return true, os.WriteFile(path, []byte(content), 0o644)
}
