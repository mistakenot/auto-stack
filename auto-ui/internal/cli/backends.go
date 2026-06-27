package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-shared/transport"
	"github.com/mistakenot/auto-ui/internal/config"
	"github.com/spf13/cobra"
)

// verifyTimeout bounds the one-shot connectivity probe performed by
// `auto ui backends add` so the command never hangs against an unresponsive
// backend.
const verifyTimeout = 5 * time.Second

// newBackendsCmd builds the `auto ui backends` command group, which manages the
// list of autowatch backends the UI proxies to (~/.auto/ui/backends.json).
func newBackendsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backends",
		Short: "Manage autowatch backends the UI proxies to",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(
		newBackendsListCmd(),
		newBackendsAddCmd(),
		newBackendsRemoveCmd(),
	)
	return cmd
}

func newBackendsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.BackendsPath()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			cfg, err := config.LoadBackends(path)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
}

func newBackendsAddCmd() *cobra.Command {
	var (
		name     string
		noVerify bool
	)
	cmd := &cobra.Command{
		Use:   "add <uri>",
		Short: "Register an autowatch backend (verifies connectivity by default)",
		Long: "Register an autowatch backend by its dial URI (unix:// or tcp://). " +
			"By default the backend is dialed once to confirm it is reachable and to learn " +
			"its authoritative hostId; pass --no-verify to register without connecting.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			uri := args[0]

			path, err := config.BackendsPath()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			cfg, err := config.LoadBackends(path)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			backend := config.Backend{URI: uri, Name: name}

			// Probe connectivity first (unless skipped) so an unreachable or
			// invalid backend is rejected before we mutate backends.json. On
			// success we learn the authoritative hostId from daemon.status.
			if !noVerify {
				hostID, projectCount, verr := verifyBackend(cmd.Context(), uri)
				if verr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "could not verify backend %s: %v\n", uri, verr)
					fmt.Fprintln(cmd.ErrOrStderr(), "ensure the autowatch daemon is running and reachable, or re-run with --no-verify to register without connecting")
					return &ExitError{Code: 1}
				}
				backend.HostID = hostID
				fmt.Fprintf(cmd.ErrOrStderr(), "✓ connected to %s (%d projects)\n", hostID, projectCount)
			}

			cfg.Backends = append(cfg.Backends, backend)

			if err := config.SaveBackends(path, cfg); err != nil {
				var verrs *config.ValidationErrorsError
				if errors.As(err, &verrs) {
					printValidationErrors(cmd, verrs)
					fmt.Fprintln(cmd.ErrOrStderr(), "fix the backend uri (use unix:// or tcp://) and remove any duplicate before retrying")
					return &ExitError{Code: 1}
				}
				return &ExitError{Code: 1, Err: err}
			}

			data, err := json.Marshal(backend)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "optional human-friendly label for the backend")
	cmd.Flags().BoolVar(&noVerify, "no-verify", false, "register without dialing the backend to confirm reachability")
	return cmd
}

func newBackendsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <uri>",
		Short: "Remove a registered backend by URI",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			uri := args[0]

			path, err := config.BackendsPath()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			cfg, err := config.LoadBackends(path)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			kept := make([]config.Backend, 0, len(cfg.Backends))
			found := false
			for _, b := range cfg.Backends {
				if b.URI == uri {
					found = true
					continue
				}
				kept = append(kept, b)
			}
			if !found {
				fmt.Fprintf(cmd.ErrOrStderr(), "no backend registered with uri %q\n", uri)
				fmt.Fprintln(cmd.ErrOrStderr(), "run auto ui backends list to see registered backends")
				return &ExitError{Code: 1}
			}
			cfg.Backends = kept

			if err := config.SaveBackends(path, cfg); err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			out := map[string]any{"uri": uri, "removed": true}
			data, err := json.Marshal(out)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
}

// statusResult is the subset of the autowatch daemon.status payload the add
// command needs to learn the authoritative hostId.
type statusResult struct {
	HostID string `json:"hostId"`
}

// verifyBackend performs a one-shot dial of uri, calling daemon.status to learn
// the backend's hostId and project.list to count its projects. It tears the
// connection down before returning. Any dial/RPC failure is returned as an error.
func verifyBackend(parent context.Context, uri string) (hostID string, projectCount int, err error) {
	ctx, cancel := context.WithTimeout(parent, verifyTimeout)
	defer cancel()

	conn, err := transport.Dial(ctx, uri)
	if err != nil {
		return "", 0, fmt.Errorf("dial: %w", err)
	}

	peer := rpc.NewPeer(conn)
	serveCtx, serveCancel := context.WithCancel(ctx)
	defer serveCancel()
	go func() { _ = peer.Serve(serveCtx) }()

	statusRaw, err := peer.Call(ctx, "daemon.status", nil)
	if err != nil {
		return "", 0, fmt.Errorf("daemon.status: %w", err)
	}
	var status statusResult
	if err := json.Unmarshal(statusRaw, &status); err != nil {
		return "", 0, fmt.Errorf("decode daemon.status: %w", err)
	}

	projectsRaw, err := peer.Call(ctx, "project.list", nil)
	if err != nil {
		return "", 0, fmt.Errorf("project.list: %w", err)
	}
	var projects []json.RawMessage
	if err := json.Unmarshal(projectsRaw, &projects); err != nil {
		return "", 0, fmt.Errorf("decode project.list: %w", err)
	}

	return status.HostID, len(projects), nil
}

// printValidationErrors writes each structured validation error to stderr.
func printValidationErrors(cmd *cobra.Command, verrs *config.ValidationErrorsError) {
	for _, e := range verrs.Errors {
		fmt.Fprintf(cmd.ErrOrStderr(), "validation error [%s] %s: %s\n", e.Code, e.Field, e.Message)
	}
}
