package cli

import (
	"errors"
	"os"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/workspace"
	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/spf13/cobra"
)

func newInitCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create docs/plans/ in this repository (idempotent)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, application)
		},
	}
}

type initResult struct {
	Root    string `json:"root"`
	Created bool   `json:"created"`
}

func runInit(cmd *cobra.Command, application *app.App) error {
	text := textMode(cmd)
	// Host identity is shared machine state every tool's init establishes.
	if _, _, _, err := sharedconfig.EnsureHost(); err != nil {
		return failOne(cmd, text, "host-config", "$", "", err.Error(), nil, "check that ~/.auto is writable")
	}
	ws, err := openWorkspace(cmd, application, text)
	if err != nil {
		return err
	}
	_, statErr := os.Stat(ws.PlansPath())
	created := errors.Is(statErr, os.ErrNotExist)
	if err := os.MkdirAll(ws.PlansPath(), 0o755); err != nil {
		return failOne(cmd, text, "write-failed", "$", "", err.Error(), ws.PlansPath(), "check that the repository is writable")
	}
	res := initResult{Root: workspace.PlansDir, Created: created}
	return emit(cmd, text, res, func() string {
		if created {
			return "created " + workspace.PlansDir + "\n"
		}
		return workspace.PlansDir + " already exists\n"
	})
}
