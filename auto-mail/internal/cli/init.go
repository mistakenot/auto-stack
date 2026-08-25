package cli

import (
	"os"

	"github.com/mistakenot/auto-mail/internal/app"
	"github.com/mistakenot/auto-mail/internal/config"
	"github.com/mistakenot/auto-mail/mail"
	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/spf13/cobra"
)

func newInitCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create ~/.auto/mail/ and the alpha mail store",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, application)
		},
	}
}

func runInit(cmd *cobra.Command, _ *app.App) error {
	// Host identity is shared machine state every tool's init establishes.
	if _, _, _, err := sharedconfig.EnsureHost(); err != nil {
		return &ExitError{Code: 1, Err: err}
	}

	path, err := config.StorePath()
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	_, statErr := os.Stat(path)
	created := os.IsNotExist(statErr)

	// Opening the direct client is what creates ~/.auto/mail/ and migrates the
	// store — the same path every other verb takes, so init cannot drift from
	// what the tool actually needs.
	client, err := mail.NewDirect("")
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	defer func() { _ = client.Close() }()

	if err := writeJSON(cmd.OutOrStdout(), map[string]any{
		"store":   path,
		"created": created,
		"alpha":   true,
	}); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return nil
}
