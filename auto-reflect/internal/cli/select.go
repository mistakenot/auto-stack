package cli

import (
	"fmt"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/loop"
	"github.com/spf13/cobra"
)

func newSelectCmd(application *app.App) *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "select <retrieval_id...>",
		Short: "Commit to an ordering of retrieved rules; reveals content and mints feedback ids",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			svc := loop.NewService(application.CWD)
			results, err := svc.Select(args)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if outputFormat == "text" {
				for _, r := range results {
					fmt.Fprintf(cmd.OutOrStdout(), "%s [%s]\n%s\n(%s)\n\n", r.FeedbackID, r.RuleType, r.Content, r.CausalNote)
				}
				return nil
			}
			if err := writeJSON(cmd.OutOrStdout(), results); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}
