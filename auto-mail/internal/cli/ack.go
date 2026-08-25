package cli

import (
	"errors"
	"fmt"

	"github.com/mistakenot/auto-mail/internal/app"
	"github.com/mistakenot/auto-mail/mail"
	"github.com/spf13/cobra"
)

func newAckCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "ack <mail-id>",
		Short: "Retire one mail — always a separate explicit call",
		Long: "Ack one mail. Reading never retires mail, so this is always its own call. " +
			"The payload reports two different things: acked says the delivery is acked " +
			"now, wonTransition says whether it was this call that transitioned it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAck(cmd, application, args[0])
		},
	}
}

func runAck(cmd *cobra.Command, application *app.App, mailID string) error {
	client, err := mail.NewDirect("")
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	defer func() { _ = client.Close() }()

	result, err := client.Ack(cmd.Context(), mail.AckInput{
		MailID:  mailID,
		Binding: mail.BindingFor(application.CWD),
	})
	switch {
	case errors.Is(err, mail.ErrUnknownMail):
		return &ExitError{Code: 1, Err: fmt.Errorf("%w — run `auto mail list` to see the ids you hold", err)}
	case errors.Is(err, mail.ErrNoDelivery):
		return &ExitError{Code: 1, Err: fmt.Errorf(
			"%w — that mail exists but is addressed to a subscription you do not hold; "+
				"run `auto mail subscribe <address>` first, or `auto mail list` to see what you do hold", err)}
	case err != nil:
		return &ExitError{Code: 1, Err: err}
	}

	if err := writeJSON(cmd.OutOrStdout(), result); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	if !result.WonTransition {
		// Losing a race is a correct, expected outcome, not invalid usage: the
		// exit code stays 0 and the caller is told who won it (D-062-7). Exit
		// non-zero is reserved for an unknown id or a genuine store failure.
		fmt.Fprintf(cmd.ErrOrStderr(), "already acked at %s by %s\n",
			result.AckedAt.Format("2006-01-02T15:04:05Z07:00"), result.AckedBy)
	}
	return nil
}
