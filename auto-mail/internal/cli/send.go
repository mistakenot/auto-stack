package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mistakenot/auto-mail/internal/app"
	"github.com/mistakenot/auto-mail/mail"
	sharedconfig "github.com/mistakenot/auto-shared/config"
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
	cmd.Flags().StringVar(&to, "to", "",
		"destination address, or the relative handle #parent (required)")
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
	// `--to` is the one position a handle is legal in; `--from` is not, because
	// a handle there would have the sender claim its supervisor's identity as
	// author — exactly the physical/logical confusion G5 exists to prevent.
	if err := rejectHandle(from, "`auto mail send --from`",
		"--from is the absolute address a reply comes back to, and a relative one "+
			"would sign this mail as somebody else"); err != nil {
		return err
	}

	client, err := mail.NewDirect("")
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	defer func() { _ = client.Close() }()

	// Who this process is, is established here and passed in — never derived
	// inside the client (D-063-8). The markers it reads are on *this* host, and
	// from T3 the client may be answering from another one.
	binding := mail.BindingFor(application.CWD)
	sender := mail.CallerSender(callerHome(), binding)

	result, err := client.Send(cmd.Context(), mail.SendInput{
		To:      to,
		From:    from,
		Body:    body,
		Binding: binding,
		Cwd:     application.CWD,
		Sender:  sender,
	})
	if err != nil {
		// Every send failure — a refused handle included — lands here: exit 1,
		// the remediation on stderr, and stdout left completely empty so a
		// caller parsing it is never handed half a payload.
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

// callerHome resolves the home directory the Subagent markers live under, and
// answers "" when it cannot.
//
// An empty home is the same answer as an empty marker directory — an ordinary
// agent, and a refused `#parent` with a hint — which is the right failure for a
// question about identity: a caller that cannot tell whether it is a Subagent
// must not be allowed to act as one.
func callerHome() string {
	home, err := sharedconfig.HomeDir()
	if err != nil {
		return ""
	}
	return home
}

// rejectHandle is the position rule (D-063-2), shared by the three surfaces
// that take an address and only an address: `subscribe <address>`,
// `list --address` and `send --from`. It returns nil for anything that is not a
// handle, so an ordinary address never pays for it.
//
// The rule stated once: **a handle names a recipient at a moment; the three
// positions above name something durable, and "durable" cannot be relative.**
//
// The two refusals are ordered so the more actionable one wins. `#nope`
// anywhere is a typo — that the position would also have rejected it is beside
// the point, and being told "not allowed here" would send the caller off to fix
// the wrong thing. So an unrecognised handle is answered first, in every
// position, with the mail package's own text: it is the same failure the
// resolver reports, and one sentence for one mistake is what keeps the four
// refusals distinguishable.
//
// The position message, by contrast, is built here rather than in the mail
// package, because this is the only layer that knows which of the three
// positions the value arrived in — and a refusal that could not name it would
// be the least useful of the four.
func rejectHandle(value, position, why string) error {
	if !mail.IsHandle(value) {
		return nil
	}
	if err := mail.ValidateHandle(value); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return &ExitError{Code: 1, Err: fmt.Errorf(
		"%q is a relative handle, not an address: %w, and %s takes one — %s. "+
			"Use an absolute address (for example `auto-stack/supervisor`); a handle "+
			"is legal only in `auto mail send --to`. See `auto mail docs` under "+
			"\"relative handles\"",
		value, mail.ErrHandleNotAllowed, position, why)}
}
