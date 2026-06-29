package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/loop"
	"github.com/spf13/cobra"
)

func newRetrieveCmd(application *app.App) *cobra.Command {
	var (
		domain   []string
		limit    int
		format   string
		noDrafts bool
	)

	cmd := &cobra.Command{
		Use:   "retrieve <intent>",
		Short: "Match rules to an intent, minting retrieval ids (predicates only, no content)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if limit < 0 {
				return &ExitError{Code: 1, Err: errors.New("invalid --limit: use --limit <n> where n >= 0")}
			}

			svc := loop.NewService(application.CWD)
			results, err := svc.Retrieve(args[0], domain, limit, !noDrafts)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if outputFormat == "text" {
				for i := range results {
					printRetrievedText(cmd, &results[i])
				}
				return nil
			}
			items := make([]map[string]any, 0, len(results))
			for i := range results {
				r := &results[i]
				items = append(items, map[string]any{
					"id":           r.RetrievalID,
					"retrieval_id": r.RetrievalID,
					"use_when":     r.UseWhen,
					"domain":       r.Domain,
					"rule_type":    r.RuleType,
					"lifecycle":    r.Lifecycle,
					"draft":        r.Draft,
				})
			}
			if err := writeJSON(cmd.OutOrStdout(), items); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&domain, "domain", nil, "domain tag(s); ranking boost (non-excluding), repeatable or comma-separated")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum rules to surface (0 means all)")
	cmd.Flags().BoolVar(&noDrafts, "no-drafts", false, "exclude draft rules (drafts are surfaced by default; stale rules are never surfaced)")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}

func printRetrievedText(cmd *cobra.Command, r *loop.RetrievedRule) {
	fmt.Fprintf(cmd.OutOrStdout(), "%s [%s] (%s) %s  lifecycle=%s\n", r.RetrievalID, r.RuleType, strings.Join(r.Domain, ","), r.UseWhen, r.Lifecycle)
}
