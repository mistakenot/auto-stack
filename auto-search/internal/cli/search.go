package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

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
	var toolName string
	var sessionID string
	var minToolDuration string
	var interrupted bool
	var includeThinking bool
	var offset int
	var limit int
	var requestID string
	var highlight bool
	var textOut bool

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search indexed messages or session transcripts",
		Long: `Search indexed messages or session transcripts using BM25 ranking.

Use --min-tool-duration and --interrupted (with --tool-name optional) to
find slow or stuck tool calls:

  # All Bash calls slower than 60 seconds in the last 30 days.
  autosearch search "" --tool-name Bash --min-tool-duration 60s --since 30d

  # Every tool call Claude reported as interrupted.
  autosearch search "" --interrupted

  # Scope a duration query to a single session.
  autosearch search "" --session-id <session-id> --min-tool-duration 60s
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cwd != "" && remote != "" {
				return &ExitError{Code: 1, Err: errors.New("--cwd and --remote are mutually exclusive")}
			}
			mode = strings.ToLower(strings.TrimSpace(mode))
			if mode != "bm25" {
				return &ExitError{Code: 1, Err: fmt.Errorf("invalid --mode value %q (only bm25 is supported)", mode)}
			}

			var minToolDurationMs *int64
			if minToolDuration != "" {
				ms, err := search.ParseToolDurationMs(minToolDuration)
				if err != nil {
					return &ExitError{Code: 1, Err: fmt.Errorf("invalid --min-tool-duration: %w", err)}
				}
				minToolDurationMs = &ms
			}

			// Query is optional iff at least one structured filter is set.
			// This lets users ask "show me all hangs" without inventing a
			// dummy FTS query.
			queryStr := ""
			if len(args) > 0 {
				queryStr = args[0]
			}
			hasStructuredFilter := toolName != "" || sessionID != "" ||
				minToolDurationMs != nil || interrupted
			if queryStr == "" && !hasStructuredFilter {
				return &ExitError{Code: 1, Err: errors.New("search requires either a query argument or at least one of --tool-name / --session-id / --min-tool-duration / --interrupted")}
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

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")

			switch scope {
			case "messages":
				result, err := search.SearchMessages(&search.MessageSearchOpts{
					DB:                db,
					Query:             queryStr,
					Since:             since,
					After:             after,
					Before:            before,
					CWD:               cwd,
					Remote:            remote,
					Skill:             skill,
					Role:              role,
					Field:             field,
					ToolName:          toolName,
					SessionID:         sessionID,
					MinToolDurationMs: minToolDurationMs,
					OnlyInterrupted:   interrupted,
					IncludeThinking:   includeThinking,
					Offset:            offset,
					PageSize:          limit,
					RequestID:         requestID,
					Highlight:         highlight,
				})
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				if textOut {
					renderMessageHitsText(cmd.OutOrStdout(), result)
					return nil
				}
				return enc.Encode(result)

			case "sessions":
				if toolName != "" || sessionID != "" || minToolDurationMs != nil || interrupted || textOut {
					return &ExitError{Code: 1, Err: errors.New("--tool-name, --session-id, --min-tool-duration, --interrupted, and --text only apply to --scope messages")}
				}
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
	cmd.Flags().StringVar(&role, "role", "", "filter by message role (user, assistant, tool, thinking)")
	cmd.Flags().StringVar(&field, "field", "all", "filter searchable field: all, content, tool_input, tool_output")
	cmd.Flags().StringVar(&toolName, "tool-name", "", "filter by tool name (e.g. Read, Write, Edit, Bash) — messages scope only")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "filter to a single session ID — messages scope only")
	cmd.Flags().StringVar(&minToolDuration, "min-tool-duration", "", "include only tool calls with duration_ms >= this value (e.g. 60s, 5m, 1500ms) — messages scope only")
	cmd.Flags().BoolVar(&interrupted, "interrupted", false, "include only tool calls Claude reported as interrupted — messages scope only")
	cmd.Flags().BoolVar(&includeThinking, "include-thinking", false, "include thinking messages in results (excluded by default)")
	cmd.Flags().IntVar(&offset, "offset", 0, "result offset for pagination (0-based)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max results to return (default 20)")
	cmd.Flags().BoolVar(&highlight, "highlight", false, "highlight matched terms in snippets (no-op when query is empty)")
	cmd.Flags().BoolVar(&textOut, "text", false, "render hits as a skim-friendly text table instead of JSON (messages scope only)")
	cmd.Flags().StringVar(&requestID, "request-id", "", "request identifier to echo in responses")
	return cmd
}

// renderMessageHitsText prints a compact text table of message hits suitable
// for terminal skimming. Used by --text on messages scope. Includes
// duration_ms and interrupted columns when any row has them set so users
// can spot slow / stuck calls without parsing JSON.
func renderMessageHitsText(w io.Writer, result *search.MessageSearchResult) {
	showDuration := false
	showInterrupted := false
	for i := range result.Hits {
		if result.Hits[i].DurationMs > 0 {
			showDuration = true
		}
		if result.Hits[i].Interrupted {
			showInterrupted = true
		}
	}
	fmt.Fprintf(w, "total=%d returned=%d offset=%d\n",
		result.Meta.TotalMatches, result.Meta.ReturnedHits, result.Meta.Offset)
	for i := range result.Hits {
		h := &result.Hits[i]
		extras := ""
		if h.ToolName != "" {
			extras += " tool=" + h.ToolName
		}
		if showDuration {
			extras += fmt.Sprintf(" duration_ms=%d", h.DurationMs)
		}
		if showInterrupted && h.Interrupted {
			extras += " interrupted=true"
		}
		snippet := search.TruncateAtRune(strings.ReplaceAll(strings.TrimSpace(h.Snippet), "\n", " "), 160)
		fmt.Fprintf(w, "[%s] %s%s\n  %s\n", h.MessageType, h.MessageID, extras, snippet)
	}
}
