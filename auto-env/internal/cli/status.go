package cli

import (
	"errors"
	"fmt"

	"github.com/mistakenot/auto-env/internal/app"
	"github.com/mistakenot/auto-env/internal/config"
	"github.com/mistakenot/auto-env/internal/manifest"
	"github.com/mistakenot/auto-env/internal/port"
	"github.com/mistakenot/auto-env/internal/template"
	"github.com/mistakenot/auto-env/internal/worktree"
	"github.com/spf13/cobra"
)

func newStatusCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current environment status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := worktree.Detect(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: errors.New("not a git repository: autoenv requires a git repository")}
			}
			repoRoot := info.RepoRoot

			manifestPath := config.ManifestPath(repoRoot)
			if !manifest.Exists(manifestPath) {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"provisioned": false})
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			filesDir := config.FilesPath(repoRoot)
			paths, err := template.Discover(filesDir)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			portNames, err := template.ScanPortNames(filesDir, paths, cfg.Delimiters)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			ports, err := port.Allocate(portNames, cfg.PortBase, cfg.PortStride, info.Slot)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			files, err := manifest.Read(manifestPath)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("read manifest: %w", err)}
			}

			output := map[string]any{
				"provisioned": true,
				"name":        info.Name,
				"slot":        info.Slot,
				"ports":       ports,
				"files":       files,
			}
			return writeJSON(cmd.OutOrStdout(), output)
		},
	}
}
