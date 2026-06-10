package cli

import (
	"fmt"
	"sort"
	"strings"

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
			report, err := svc.Stats()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if outputFormat == "text" {
				fmt.Fprintf(cmd.OutOrStdout(), "unconsolidated_observations=%d\n", report.UnconsolidatedObservations)
				for _, s := range report.Rules {
					fmt.Fprintf(cmd.OutOrStdout(), "%s surfaced=%d selected=%d rate=%.2f feedback=%d ranks=%s outcomes=%s\n",
						s.RuleID, s.Surfaced, s.Selected, s.SelectionRate, s.FeedbackCount,
						formatRankDistribution(s.RankDistribution), formatOutcomeCounts(s.OutcomeCounts))
				}
				return nil
			}
			if err := writeJSON(cmd.OutOrStdout(), report); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}

// formatRankDistribution renders a rank->count map deterministically as
// "rank:count" pairs ordered by ascending rank, e.g. "1:3,2:1". An empty map
// renders as "-".
func formatRankDistribution(dist map[int]int) string {
	if len(dist) == 0 {
		return "-"
	}
	ranks := make([]int, 0, len(dist))
	for rank := range dist {
		ranks = append(ranks, rank)
	}
	sort.Ints(ranks)
	parts := make([]string, 0, len(ranks))
	for _, rank := range ranks {
		parts = append(parts, fmt.Sprintf("%d:%d", rank, dist[rank]))
	}
	return strings.Join(parts, ",")
}

// formatOutcomeCounts renders an outcome->count map deterministically as
// "outcome:count" pairs ordered by outcome name, e.g. "fail:1,success:2". An
// empty map renders as "-".
func formatOutcomeCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "-"
	}
	outcomes := make([]string, 0, len(counts))
	for outcome := range counts {
		outcomes = append(outcomes, outcome)
	}
	sort.Strings(outcomes)
	parts := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		parts = append(parts, fmt.Sprintf("%s:%d", outcome, counts[outcome]))
	}
	return strings.Join(parts, ",")
}
