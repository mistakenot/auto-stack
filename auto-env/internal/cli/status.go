package cli

import (
	"fmt"

	"github.com/mistakenot/auto-env/internal/app"
	"github.com/mistakenot/auto-env/internal/config"
	"github.com/mistakenot/auto-env/internal/manifest"
	"github.com/mistakenot/auto-env/internal/port"
	"github.com/mistakenot/auto-env/internal/registry"
	"github.com/mistakenot/auto-env/internal/template"
	"github.com/mistakenot/auto-env/internal/worktree"
	"github.com/spf13/cobra"
)

func newStatusCmd(application *app.App) *cobra.Command {
	var global bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current environment status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if global {
				return showGlobalStatus(cmd)
			}

			info, err := worktree.Detect(application.CWD)
			if err != nil {
				return showGlobalStatus(cmd)
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

	cmd.Flags().BoolVar(&global, "global", false, "show all registered environments across the machine")
	return cmd
}

func showGlobalStatus(cmd *cobra.Command) error {
	reg, err := registry.Default()
	if err != nil {
		return &ExitError{Code: 1, Err: fmt.Errorf("access environment registry: %w", err)}
	}
	entries, err := reg.List()
	if err != nil {
		return &ExitError{Code: 1, Err: fmt.Errorf("list environments: %w", err)}
	}
	return writeJSON(cmd.OutOrStdout(), map[string]any{
		"environments": entries,
	})
}
