package cli

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/mistakenot/auto-config/internal/app"
	"github.com/mistakenot/auto-config/internal/hooks"
	"github.com/spf13/cobra"
)

func newInitCmd(application *app.App) *cobra.Command {
	var projectFlag bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectFlag {
				return initProject(application)
			}
			fmt.Fprintln(application.Stderr, "global init not yet implemented")
			return nil
		},
	}
	cmd.Flags().BoolVar(&projectFlag, "project", false, "Initialize project-local settings and git hooks")
	return cmd
}

func initProject(application *app.App) error {
	out, err := exec.Command("git", "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return fmt.Errorf("not a git repository (git rev-parse --git-common-dir failed): %w", err)
	}
	gitDir := strings.TrimSpace(string(out))

	if err := hooks.SetupGitHooks(gitDir); err != nil {
		return &ExitError{Code: 1, Err: err}
	}

	fmt.Fprintln(application.Stderr, "installed prepare-commit-msg hook")
	return nil
}
