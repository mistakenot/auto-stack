package cli

import (
	"fmt"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/loop"
	"github.com/spf13/cobra"
)

func newStatsCmd(application *app.App) *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Per-rule surfaced/selected/selection_rate/feedback_count",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			svc := loop.NewService(application.CWD)
			stats, err := svc.Stats()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if outputFormat == "text" {
				for _, s := range stats {
					fmt.Fprintf(cmd.OutOrStdout(), "%s surfaced=%d selected=%d rate=%.2f feedback=%d\n",
						s.RuleID, s.Surfaced, s.Selected, s.SelectionRate, s.FeedbackCount)
				}
				return nil
			}
			if err := writeJSON(cmd.OutOrStdout(), stats); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}
