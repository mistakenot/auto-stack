package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-env/internal/app"
	"github.com/mistakenot/auto-env/internal/config"
	"github.com/mistakenot/auto-env/internal/worktree"
	"github.com/spf13/cobra"
)

func newInitCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Scaffold the .auto/env/ directory structure",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := worktree.Detect(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: errors.New("not a git repository: autoenv requires a git repository")}
			}
			repoRoot := info.RepoRoot

			envDir := filepath.Join(repoRoot, config.EnvDir)
			configPath := config.ConfigPath(repoRoot)
			filesDir := config.FilesPath(repoRoot)

			if err := os.MkdirAll(envDir, 0755); err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("create env directory: %w", err)}
			}

			if err := os.MkdirAll(filesDir, 0755); err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("create files directory: %w", err)}
			}

			gitignorePath := filepath.Join(envDir, ".gitignore")
			if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
				if err := os.WriteFile(gitignorePath, []byte(".generated\n"), 0644); err != nil {
					return &ExitError{Code: 1, Err: fmt.Errorf("write .gitignore: %w", err)}
				}
			}

			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				cfg := map[string]string{
					"up_command":   "",
					"down_command": "",
				}
				data, err := json.MarshalIndent(cfg, "", "  ")
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				if err := os.WriteFile(configPath, append(data, '\n'), 0644); err != nil {
					return &ExitError{Code: 1, Err: fmt.Errorf("write config: %w", err)}
				}
			}

			fmt.Fprintln(cmd.ErrOrStderr(), "Initialized .auto/env/ directory.")
			fmt.Fprintln(cmd.ErrOrStderr(), "")
			fmt.Fprintln(cmd.ErrOrStderr(), "Next steps:")
			fmt.Fprintln(cmd.ErrOrStderr(), "  1. Edit .auto/env/config.json — set up_command and down_command")
			fmt.Fprintln(cmd.ErrOrStderr(), "  2. Add template files to .auto/env/files/")
			fmt.Fprintln(cmd.ErrOrStderr(), "  3. Run: autoenv up")
			return nil
		},
	}
}
