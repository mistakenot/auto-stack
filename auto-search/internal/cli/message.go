package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mistakenot/auto-search/internal/config"
	"github.com/mistakenot/auto-search/internal/indexdb"
	"github.com/spf13/cobra"
)

func newMessageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "message",
		Short: "Inspect indexed messages",
	}
	cmd.AddCommand(
		newMessageGetCmd(),
		newMessageDescribeCmd(),
	)
	return cmd
}

func newMessageGetCmd() *cobra.Command {
	var index string

	cmd := &cobra.Command{
		Use:   "get <message_id>",
		Short: "Return the full stored content for a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := config.IndexPath(index)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			db, err := indexdb.Open(dbPath)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("open index: %w; run: auto search index", err)}
			}
			defer func() { _ = db.Close() }()

			msg, err := indexdb.GetMessageByID(db, args[0])
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			// message get returns the full raw content, not JSON.
			fmt.Fprint(cmd.OutOrStdout(), msg.Content)
			return nil
		},
	}
	cmd.Flags().StringVar(&index, "index", config.DefaultIndexName, "named index to query")
	return cmd
}

func newMessageDescribeCmd() *cobra.Command {
	var requestID string
	var index string

	cmd := &cobra.Command{
		Use:   "describe <message_id>",
		Short: "Return a compact JSON summary for a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()

			dbPath, err := config.IndexPath(index)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			db, err := indexdb.Open(dbPath)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("open index: %w; run: auto search index", err)}
			}
			defer func() { _ = db.Close() }()

			msg, err := indexdb.GetMessageByID(db, args[0])
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			prev, next, err := indexdb.NeighborMessageIDs(db, msg.SessionID, msg.MessageIndex)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			sessionFirstAt, sessionLastAt, err := indexdb.SessionTimeRange(db, msg.SessionID)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			// Preview from content_truncated.
			preview := msg.ContentTruncated
			const maxPreview = 500
			if len(preview) > maxPreview {
				preview = preview[:maxPreview]
			}

			elapsed := time.Since(start).Milliseconds()

			// Per-tool-call fields (toolUseId/durationMs/interrupted) are
			// populated by auto-etl from the raw `tool_use.id` ↔
			// `tool_result.tool_use_id` pairing and the
			// `toolUseResult.durationMs` / `interrupted` envelope. Empty /
			// zero on non-tool messages. Naming matches this file's existing
			// camelCase convention.
			msgMap := map[string]any{
				"id":                    msg.MessageID,
				"sessionId":             msg.SessionID,
				"messageIndex":          msg.MessageIndex,
				"messageType":           msg.Role,
				"timestamp":             msg.Timestamp,
				"workspace":             msg.Workspace,
				"gitRemote":             msg.GitRemote,
				"model":                 msg.Model,
				"toolName":              msg.ToolName,
				"toolFilePath":          msg.ToolFilePath,
				"bashCommand":           msg.BashCommand,
				"skillName":             msg.SkillName,
				"toolUseId":             msg.ToolUseID,
				"durationMs":            msg.DurationMs,
				"interrupted":           msg.Interrupted,
				"preview":               preview,
				"previousMessageId":     prev,
				"nextMessageId":         next,
				"sessionFirstMessageAt": sessionFirstAt,
				"sessionLastMessageAt":  sessionLastAt,
			}
			if msg.ToolUseResultJSON != "" {
				msgMap["toolUseResult"] = json.RawMessage(msg.ToolUseResultJSON)
			}

			out := map[string]any{
				"_meta": map[string]any{
					"request_id": requestID,
					"elapsed_ms": elapsed,
				},
				"message": msgMap,
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		},
	}
	cmd.Flags().StringVar(&requestID, "request-id", "", "request identifier to echo in responses")
	cmd.Flags().StringVar(&index, "index", config.DefaultIndexName, "named index to query")
	return cmd
}
