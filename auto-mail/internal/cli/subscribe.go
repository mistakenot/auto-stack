package cli

import (
	"github.com/mistakenot/auto-mail/internal/app"
	"github.com/mistakenot/auto-mail/mail"
	"github.com/spf13/cobra"
)

func newSubscribeCmd(application *app.App) *cobra.Command {
	var (
		fromNow bool
		name    string
	)
	cmd := &cobra.Command{
		Use:   "subscribe <address>",
		Short: "Become a durable reader of an address",
		Long: "Create (or return) this caller's subscription to an address and record " +
			"its binding. Mail already sent to the address is backfilled unless " +
			"--from-now is given, so an agent that subscribes late still sees what it missed.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSubscribe(cmd, application, args[0], fromNow, name)
		},
	}
	cmd.Flags().BoolVar(&fromNow, "from-now", false,
		"start the cursor at the current high-water mark instead of backfilling existing mail")
	cmd.Flags().StringVar(&name, "name", "", "optional human label for this subscription")
	return cmd
}

func runSubscribe(cmd *cobra.Command, application *app.App, address string, fromNow bool, name string) error {
	// A subscription is durable, and a handle is not: it names whoever this
	// process's supervisor happens to be right now, with no point at which it
	// would ever be re-resolved (D-063-2). Rejected before the store is even
	// opened, so a refused subscribe cannot leave a `#` anywhere.
	if err := rejectHandle(address, "`auto mail subscribe <address>`",
		"a subscription names a reader forever, and a relative name stops meaning "+
			"what it said the moment this process is re-parented"); err != nil {
		return err
	}

	client, err := mail.NewDirect("")
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	defer func() { _ = client.Close() }()

	result, err := client.Subscribe(cmd.Context(), mail.SubscribeInput{
		Address: address,
		Name:    name,
		FromNow: fromNow,
		Binding: mail.BindingFor(application.CWD),
	})
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	if err := writeJSON(cmd.OutOrStdout(), result); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return nil
}
