package cli

import (
	"github.com/mistakenot/auto-mail/internal/app"
	"github.com/mistakenot/auto-mail/mail"
	"github.com/spf13/cobra"
)

func newListCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List unacked mail for every subscription bound to this caller",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd, application)
		},
	}
}

func runList(cmd *cobra.Command, _ *app.App) error {
	client, err := mail.NewDirect("")
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	defer func() { _ = client.Close() }()

	deliveries, err := client.List(cmd.Context(), mail.ListInput{})
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	if err := writeJSON(cmd.OutOrStdout(), deliveries); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return nil
}
