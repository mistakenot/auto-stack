package cli

import (
	"encoding/json"
	"fmt"

	"github.com/mistakenot/auto-search/internal/config"
	"github.com/mistakenot/auto-search/internal/indexdb"
	"github.com/mistakenot/auto-search/internal/stats"
	"github.com/spf13/cobra"
)

func newStatsCmd() *cobra.Command {
	var scope string
	var groupBy string
	var queryText string
	var measure string
	var index string
	var since string
	var after string
	var before string
	var cwd string
	var remote string
	var skill string
	var role string
	var field string
	var minCount int
	var offset int
	var limit int
	var requestID string

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Group and rank patterns across indexed sessions and messages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := config.IndexPath(index)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			db, err := indexdb.Open(dbPath)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("open index: %w; run: autosearch index", err)}
			}
			defer func() { _ = db.Close() }()

			result, err := stats.Run(&stats.Request{
				DB:        db,
				Scope:     scope,
				GroupBy:   groupBy,
				Query:     queryText,
				Measure:   measure,
				Since:     since,
				After:     after,
				Before:    before,
				CWD:       cwd,
				Remote:    remote,
				Skill:     skill,
				Role:      role,
				Field:     field,
				MinCount:  minCount,
				Offset:    offset,
				PageSize:  limit,
				RequestID: requestID,
			})
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "messages", "stats scope: messages or sessions")
	cmd.Flags().StringVar(&groupBy, "group-by", "", "grouping key (required)")
	cmd.Flags().StringVar(&queryText, "query", "", "optional FTS query filter")
	cmd.Flags().StringVar(&measure, "measure", "count", "primary ranking metric: count, distinct_sessions, distinct_messages")
	cmd.Flags().StringVar(&index, "index", config.DefaultIndexName, "named index to query")
	cmd.Flags().StringVar(&since, "since", "", "relative time filter")
	cmd.Flags().StringVar(&after, "after", "", "inclusive lower date bound")
	cmd.Flags().StringVar(&before, "before", "", "exclusive upper date bound")
	cmd.Flags().StringVar(&cwd, "cwd", "", "filter by workspace path (substring, case-insensitive)")
	cmd.Flags().StringVar(&remote, "remote", "", "filter by git remote (substring, case-insensitive)")
	cmd.Flags().StringVar(&skill, "skill", "", "filter by skill name (substring, case-insensitive)")
	cmd.Flags().StringVar(&role, "role", "", "filter by message role")
	cmd.Flags().StringVar(&field, "field", "all", "filter searchable field: all, content, tool_input, tool_output")
	cmd.Flags().IntVar(&minCount, "min-count", 0, "minimum threshold for selected measure")
	cmd.Flags().IntVar(&offset, "offset", 0, "bucket offset for pagination (0-based)")
	cmd.Flags().IntVar(&limit, "limit", 20, "max buckets to return")
	cmd.Flags().StringVar(&requestID, "request-id", "", "request identifier to echo in responses")

	_ = cmd.MarkFlagRequired("group-by")
	return cmd
}
