package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mistakenot/auto-env/internal/app"
	"github.com/mistakenot/auto-env/internal/config"
	"github.com/mistakenot/auto-env/internal/manifest"
	"github.com/mistakenot/auto-env/internal/worktree"
	"github.com/spf13/cobra"
)

func newDownCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop services and remove generated files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := worktree.Detect(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: errors.New("not a git repository: autoenv requires a git repository")}
			}
			repoRoot := info.RepoRoot

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			shCmd := exec.Command("sh", "-c", cfg.DownCommand)
			shCmd.Dir = repoRoot
			shCmd.Stdout = os.Stdout
			shCmd.Stderr = os.Stderr
			if err := shCmd.Run(); err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("down_command failed: %w (generated files preserved, fix the issue and retry)", err)}
			}

			manifestPath := config.ManifestPath(repoRoot)
			files, err := manifest.Read(manifestPath)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("read manifest: %w", err)}
			}
			if files == nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: no .generated manifest found, down_command was still executed")
				return nil
			}

			for _, f := range files {
				_ = os.Remove(filepath.Join(repoRoot, f))
			}
			_ = os.Remove(manifestPath)

			fmt.Fprintln(cmd.OutOrStdout(), "Environment stopped and generated files removed.")
			return nil
		},
	}
}
