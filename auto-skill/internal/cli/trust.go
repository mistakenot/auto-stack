package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/mistakenot/auto-skill/internal/transport"
	"github.com/mistakenot/auto-skill/internal/trust"
	"github.com/spf13/cobra"
)

// trustHostRE is the accepted host[:port] shape for a trusted endpoint. It keeps
// junk like wildcards, empty hosts, and double-scheme URLs out of the store.
var trustHostRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]+)?$`)

// validateTrustEndpoint restricts `trust add` to the two transports auto-skill
// actually fetches skills over: https:// (remote sources) and file:// (local
// sources — used by local `add` and the test/CI fixtures). Everything else is
// rejected before it can pollute the store: http://, ssh://, git:// and other
// schemes; wildcards (`*`, `?`); an empty host (`https://`); and structurally
// malformed or double-scheme URLs (M5, M6). A bare host (github.com/owner/repo)
// is accepted and canonicalizes to https.
//
// Note: the fuzz report's fix #7 asked for "https only", but file:// is a
// first-class local-source transport (transport.allowedSchemes, the `add` local
// path, and the sync trust gate all support it), so blocking it would break local
// workflows. We reject the concrete junk M5/M6 flagged while keeping file://.
func validateTrustEndpoint(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return errors.New("empty endpoint; pass an https:// or file:// URL")
	}
	if strings.ContainsAny(trimmed, "*?") {
		return fmt.Errorf("endpoint %q must not contain wildcards; pass an exact https:// or file:// URL", raw)
	}

	// Determine the explicit scheme, if any. A bare form (no "://") is treated as
	// https and validated as a host below.
	scheme := "https"
	if before, _, ok := strings.Cut(trimmed, "://"); ok {
		scheme = strings.ToLower(before)
	}

	ep, err := transport.Endpoint(trimmed)
	if err != nil {
		return err
	}

	switch scheme {
	case "https":
		host, ok := strings.CutPrefix(ep, "https://")
		if !ok {
			// A bare form that resolved to a non-https transport (e.g. an ssh
			// shorthand git@host:repo) lands here.
			return fmt.Errorf("only https:// or file:// endpoints may be trusted; %q resolved to %q", raw, ep)
		}
		if !trustHostRE.MatchString(host) {
			return fmt.Errorf("endpoint %q has no valid host; pass a URL like https://github.com/owner/repo", raw)
		}
		return nil
	case "file":
		if !strings.HasPrefix(ep, "file://") {
			return fmt.Errorf("malformed file:// endpoint %q", raw)
		}
		return nil
	default:
		return fmt.Errorf("only https:// or file:// endpoints may be trusted; got scheme %q", scheme)
	}
}

func newTrustCmd(resolveEnv envResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Manage trusted endpoints for skill fetching",
	}

	cmd.AddCommand(
		newTrustListCmd(resolveEnv),
		newTrustAddCmd(resolveEnv),
		newTrustRemoveCmd(resolveEnv),
	)

	return cmd
}

func newTrustListCmd(resolveEnv envResolver) *cobra.Command {
	var textOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List approved endpoints",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			store := trust.NewStore(env.TrustPath())
			tf, err := store.Load()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if textOutput {
				if len(tf.Endpoints) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "no trusted endpoints")
					return nil
				}
				for _, ep := range tf.Endpoints {
					fmt.Fprintln(cmd.OutOrStdout(), ep)
				}
				return nil
			}

			data, err := json.Marshal(tf)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
	cmd.Flags().BoolVar(&textOutput, "text", false, "human-readable output")
	return cmd
}

func newTrustAddCmd(resolveEnv envResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <endpoint>",
		Short: "Approve an endpoint for skill fetching",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if err := validateTrustEndpoint(args[0]); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			store := trust.NewStore(env.TrustPath())
			if err := store.Add(args[0]); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "approved: %s\n", args[0])
			return nil
		},
	}
	return cmd
}

func newTrustRemoveCmd(resolveEnv envResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <endpoint>",
		Short: "Remove an endpoint from the approved list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			store := trust.NewStore(env.TrustPath())
			if err := store.Remove(args[0]); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "removed: %s\n", args[0])
			return nil
		},
	}
	return cmd
}
