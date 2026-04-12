package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mistakenot/auto-search/internal/config"
	"github.com/mistakenot/auto-search/internal/indexdb"
	"github.com/mistakenot/auto-search/internal/search"
	"github.com/spf13/cobra"
)

func newSearchCmd() *cobra.Command {
	var scope string
	var mode string
	var index string
	var since string
	var after string
	var before string
	var cwd string
	var remote string
	var skill string
	var role string
	var field string
	var offset int
	var limit int
	var requestID string
	var highlight bool

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search indexed messages or session transcripts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cwd != "" && remote != "" {
				return &ExitError{Code: 1, Err: errors.New("--cwd and --remote are mutually exclusive")}
			}

			dbPath, err := config.IndexPath(index)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			db, err := indexdb.Open(dbPath)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("open index: %w; run: autosearch index", err)}
			}
			defer func() { _ = db.Close() }()

			queryStr := args[0]
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")

			switch scope {
			case "messages":
				result, err := search.SearchMessages(&search.MessageSearchOpts{
					DB:        db,
					Query:     queryStr,
					Since:     since,
					After:     after,
					Before:    before,
					CWD:       cwd,
					Remote:    remote,
					Skill:     skill,
					Role:      role,
					Field:     field,
					Offset:    offset,
					PageSize:  limit,
					RequestID: requestID,
					Highlight: highlight,
				})
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				return enc.Encode(result)

			case "sessions":
				result, err := search.SearchSessions(&search.SessionSearchOpts{
					DB:        db,
					Query:     queryStr,
					Since:     since,
					After:     after,
					Before:    before,
					CWD:       cwd,
					Remote:    remote,
					Skill:     skill,
					Role:      role,
					Field:     field,
					Offset:    offset,
					PageSize:  limit,
					RequestID: requestID,
				})
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				return enc.Encode(result)

			default:
				return &ExitError{Code: 1, Err: fmt.Errorf("unknown scope: %s (use messages or sessions)", scope)}
			}
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "messages", "search scope: messages or sessions")
	cmd.Flags().StringVar(&mode, "mode", "bm25", "search mode")
	cmd.Flags().StringVar(&index, "index", config.DefaultIndexName, "named index to query")
	cmd.Flags().StringVar(&since, "since", "", "relative time filter")
	cmd.Flags().StringVar(&after, "after", "", "inclusive lower date bound")
	cmd.Flags().StringVar(&before, "before", "", "exclusive upper date bound")
	cmd.Flags().StringVar(&cwd, "cwd", "", "workspace path filter")
	cmd.Flags().StringVar(&remote, "remote", "", "git remote filter")
	cmd.Flags().StringVar(&skill, "skill", "", "filter by skill name")
	cmd.Flags().StringVar(&role, "role", "", "filter by message role (user, assistant, tool)")
	cmd.Flags().StringVar(&field, "field", "all", "filter searchable field: all, content, tool_input, tool_output")
	cmd.Flags().IntVar(&offset, "offset", 0, "result offset for pagination (0-based)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max results to return (default 20)")
	cmd.Flags().BoolVar(&highlight, "highlight", false, "highlight matched terms in snippets")
	cmd.Flags().StringVar(&requestID, "request-id", "", "request identifier to echo in responses")
	return cmd
}
