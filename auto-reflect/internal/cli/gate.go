package cli

import (
	"fmt"
	"strings"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/loop"
	"github.com/spf13/cobra"
)

func newGateCmd(application *app.App) *cobra.Command {
	gateCmd := &cobra.Command{
		Use:   "gate",
		Short: "Loop gate checks (feedback completeness)",
	}
	gateCmd.AddCommand(newGateCheckCmd(application))
	return gateCmd
}

func newGateCheckCmd(application *app.App) *cobra.Command {
	var (
		session string
		since   string
		format  string
	)

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Exit 0 only when no outstanding feedback ids remain in scope",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			svc := loop.NewService(application.CWD)
			res, err := svc.GateCheck(strings.TrimSpace(session), strings.TrimSpace(since))
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if res.Clean() {
				if outputFormat == "text" {
					fmt.Fprintln(cmd.OutOrStdout(), "Gate clean: no outstanding feedback.")
					return nil
				}
				if err := writeJSON(cmd.OutOrStdout(), map[string]any{"clean": true, "outstanding": []string{}}); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				return nil
			}

			// Outstanding feedback: results to stdout (clean=false), remediation to stderr.
			if outputFormat == "text" {
				fmt.Fprintf(cmd.OutOrStdout(), "Gate open: %d outstanding feedback id(s)\n", len(res.Outstanding))
				for _, id := range res.Outstanding {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", id)
				}
			} else {
				if err := writeJSON(cmd.OutOrStdout(), map[string]any{"clean": false, "outstanding": res.Outstanding}); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "remediation: submit feedback ranking all outstanding ids with `auto reflect feedback '{...}'` (run with --session %s to scope explicitly)\n", remediationSession(res.SessionID))
			return &ExitError{Code: 2}
		},
	}

	cmd.Flags().StringVar(&session, "session", "", "session id to scope the gate to (overrides env detection)")
	cmd.Flags().StringVar(&since, "since", "", "lookback window when no session is detected (e.g. 24h, 7d; default 24h)")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}

func remediationSession(id string) string {
	if id == "" {
		return "<id>"
	}
	return id
}
