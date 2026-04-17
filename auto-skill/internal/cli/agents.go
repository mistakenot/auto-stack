package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const autoskillSnippet = `**autoskill** — Author and lint reusable agent skills. Run ` + "`autoskill quickstart`" + ` to learn more.`

// agentFiles are the files to update, checked in order.
var agentFiles = []string{"CLAUDE.md", "AGENTS.md", "GEMINI.md"}

func newAgentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agents",
		Short: "Register autoskill in local agent memory files",
		Long:  "Appends a one-line description of autoskill to CLAUDE.md, AGENTS.md, and GEMINI.md if not already present. Idempotent and symlink-safe.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("getting working directory: %w", err)}
			}

			// Track files we've already processed by resolved path to handle symlinks.
			seen := make(map[string]bool)

			for _, name := range agentFiles {
				p := filepath.Join(cwd, name)

				// Resolve symlinks so we don't write the same file twice.
				resolved, err := filepath.EvalSymlinks(p)
				if err == nil {
					if seen[resolved] {
						fmt.Fprintf(cmd.OutOrStdout(), "%s: skipped (symlink to already-processed file)\n", name)
						continue
					}
					seen[resolved] = true
				}
				// If EvalSymlinks failed the file doesn't exist yet — that's fine, we'll create it.

				updated, err := ensureSkillSnippet(p)
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

// ensureSkillSnippet checks if the file contains the autoskill snippet.
// If not, it appends it to the end. Returns true if the file was modified.
func ensureSkillSnippet(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, os.WriteFile(path, []byte(autoskillSnippet+"\n"), 0o644)
		}
		return false, err
	}

	content := string(data)
	if strings.Contains(content, autoskillSnippet) {
		return false, nil
	}

	// Append to end with a blank line separator.
	if !strings.HasSuffix(content, "\n") && len(content) > 0 {
		content += "\n"
	}
	content += "\n" + autoskillSnippet + "\n"
	return true, os.WriteFile(path, []byte(content), 0o644)
}
