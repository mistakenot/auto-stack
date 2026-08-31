package cli

import (
	"github.com/mistakenot/auto-mail/internal/app"
	"github.com/mistakenot/auto-mail/mail"
	"github.com/spf13/cobra"
)

func newListCmd(application *app.App) *cobra.Command {
	var (
		address      string
		subscription string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List unacked mail for every subscription bound to this caller",
		Long: "List unacked mail. With no filters it returns everything this caller is " +
			"bound to. Reading never retires mail — `auto mail ack <id>` is always a " +
			"separate call, so listing twice returns the same mail twice.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd, application, address, subscription)
		},
	}
	cmd.Flags().StringVar(&address, "address", "", "scope to this caller's subscriptions on one address")
	cmd.Flags().StringVar(&subscription, "subscription", "", "scope to exactly one subscription id")
	// One filter mode at a time: --address and --subscription would otherwise
	// need combination semantics nobody has defined.
	cmd.MarkFlagsMutuallyExclusive("address", "subscription")
	return cmd
}

func runList(cmd *cobra.Command, application *app.App, address, subscription string) error {
	client, err := mail.NewDirect("")
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	defer func() { _ = client.Close() }()

	deliveries, err := client.List(cmd.Context(), mail.ListInput{
		Address:      address,
		Subscription: subscription,
		Binding:      mail.BindingFor(application.CWD),
	})
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	if err := writeJSON(cmd.OutOrStdout(), deliveries); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return nil
}
