package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mistakenot/auto-mail/internal/app"
	"github.com/mistakenot/auto-mail/mail"
	"github.com/spf13/cobra"
)

func newSendCmd(application *app.App) *cobra.Command {
	var (
		to       string
		text     string
		bodyJSON string
		from     string
	)
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Post one mail to an address",
		Long: "Post one mail. It is persisted whether or not anyone is subscribed, and a " +
			"later subscriber still receives it. The payload reports two different " +
			"numbers: subscriptions counts durable readers of the address, bound counts " +
			"those with a binding row.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSend(cmd, application, to, text, bodyJSON, from)
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "destination address (required)")
	cmd.Flags().StringVar(&text, "message", "", "body text; sugar for --body '{\"message\": ...}'")
	cmd.Flags().StringVar(&bodyJSON, "body", "", "body as a JSON object")
	cmd.Flags().StringVar(&from, "from", "",
		"sender address to reply to; resolved from this caller's subscription or project when omitted")
	_ = cmd.MarkFlagRequired("to")
	cmd.MarkFlagsMutuallyExclusive("message", "body")
	return cmd
}

func runSend(cmd *cobra.Command, application *app.App, to, text, bodyJSON, from string) error {
	body, err := resolveBody(text, bodyJSON)
	if err != nil {
		// Invalid usage is fail-fast, per the project's CLI convention.
		return err
	}

	client, err := mail.NewDirect("")
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	defer func() { _ = client.Close() }()

	result, err := client.Send(cmd.Context(), mail.SendInput{
		To:      to,
		From:    from,
		Body:    body,
		Binding: mail.BindingFor(application.CWD),
		Cwd:     application.CWD,
	})
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	if err := writeJSON(cmd.OutOrStdout(), result); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	if result.Subscriptions == 0 {
		// Free-form addresses (D-9) buy flexibility at the cost of typos being
		// silent. This note is the mitigation: the send still succeeded and the
		// mail is durable, so it belongs on stderr, not in the exit code.
		fmt.Fprintf(cmd.ErrOrStderr(),
			"no subscription exists for %q — mail is persisted and a later subscriber "+
				"will receive it, but check the address for a typo.\n", to)
	}
	return nil
}

// resolveBody turns the two body flags into the stored JSON object. `--message`
// is sugar for {"message": text} — that key names the *body's own field*, which
// is the one place the word is allowed; the stored unit is Mail.
func resolveBody(text, bodyJSON string) (map[string]any, error) {
	switch {
	case bodyJSON != "":
		var body map[string]any
		if err := json.Unmarshal([]byte(bodyJSON), &body); err != nil {
			return nil, fmt.Errorf("--body is not a JSON object: %w; "+
				"pass something like --body '{\"kind\":\"bug\",\"detail\":\"...\"}'", err)
		}
		return body, nil
	case text != "":
		return map[string]any{"message": text}, nil
	default:
		return nil, errors.New("nothing to send: pass --message <text> or --body <json>")
	}
}
