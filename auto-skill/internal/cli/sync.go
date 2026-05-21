package cli

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
)

func newSyncCmd(resolveEnv envResolver) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Sync local skills into agent configurations",
		Long:  "Runs npx skills add to register local skills with supported agents (codex, claude-code), then updates agent memory files with fenced skill sections.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := exec.CommandContext(cmd.Context(), "npx", "skills", "add", "./skills", "--agent", "codex", "claude-code", "--full-depth", "-y")
			c.Stdout = cmd.OutOrStdout()
			c.Stderr = cmd.ErrOrStderr()

			if err := c.Run(); err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("npx skills add: %w", err)}
			}

			// Also update agent memory files with fenced sections
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintln(cmd.OutOrStdout(), "\nUpdating agent files...")
			return runAgentsUpdate(cmd, env, false)
		},
	}
}
