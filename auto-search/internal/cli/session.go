package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mistakenot/auto-search/internal/config"
	"github.com/mistakenot/auto-search/internal/indexdb"
	"github.com/mistakenot/auto-search/internal/search"
	"github.com/spf13/cobra"
)

const (
	maxMessageRenderLen = 2048
	maxToolArgPreview   = 80
)

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Inspect indexed sessions",
	}
	cmd.AddCommand(
		newSessionListCmd(),
		newSessionGetCmd(),
		newSessionDescribeCmd(),
	)
	return cmd
}

func newSessionListCmd() *cobra.Command {
	var index string
	var since string
	var after string
	var before string
	var cwd string
	var remote string
	var limit int
	var offset int
	var requestID string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List indexed sessions ordered by most recent first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()

			if cwd != "" && remote != "" {
				return &ExitError{Code: 1, Err: fmt.Errorf("--cwd and --remote are mutually exclusive")}
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

			tf, err := search.ParseTimeFilter(time.Now(), since, after, before)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			opts := indexdb.ListSessionsOpts{
				Workspace: cwd,
				Remote:    remote,
				StartMs:   tf.StartMs,
				EndMs:     tf.EndMs,
				Limit:     limit,
				Offset:    offset,
			}

			sessions, total, err := indexdb.ListSessions(db, opts)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			elapsed := time.Since(start).Milliseconds()

			out := map[string]any{
				"_meta": map[string]any{
					"request_id": requestID,
					"elapsed_ms": elapsed,
					"total":      total,
					"offset":     offset,
					"limit":      opts.Limit,
					"returned":   len(sessions),
				},
				"sessions": sessions,
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		},
	}
	cmd.Flags().StringVar(&index, "index", config.DefaultIndexName, "named index to query")
	cmd.Flags().StringVar(&since, "since", "", "relative time filter (e.g. 1w, 7d, 12h)")
	cmd.Flags().StringVar(&after, "after", "", "inclusive lower date bound (YYYY-MM-DD or RFC3339)")
	cmd.Flags().StringVar(&before, "before", "", "exclusive upper date bound (YYYY-MM-DD or RFC3339)")
	cmd.Flags().StringVar(&cwd, "cwd", "", "filter by workspace path (substring match)")
	cmd.Flags().StringVar(&remote, "remote", "", "filter by git remote (substring match)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max sessions to return (default 50)")
	cmd.Flags().IntVar(&offset, "offset", 0, "pagination offset (0-based)")
	cmd.Flags().StringVar(&requestID, "request-id", "", "request identifier to echo in responses")
	return cmd
}

func newSessionGetCmd() *cobra.Command {
	var index string

	cmd := &cobra.Command{
		Use:   "get <session_id>",
		Short: "Render a session transcript for agent consumption",
		Args:  cobra.ExactArgs(1),
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

			msgs, err := indexdb.SessionMessages(db, args[0])
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if len(msgs) == 0 {
				return &ExitError{Code: 1, Err: fmt.Errorf("session not found or has no messages: %s", args[0])}
			}

			out := cmd.OutOrStdout()
			for i := range msgs {
				m := &msgs[i]
				content := messageContent(m)
				content = midTruncate(content, maxMessageRenderLen, m.MessageID)
				open, close := roleTag(m)
				fmt.Fprintf(out, "%s\n%s\n%s\n\n", open, content, close)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&index, "index", config.DefaultIndexName, "named index to query")
	return cmd
}

func newSessionDescribeCmd() *cobra.Command {
	var requestID string
	var index string

	cmd := &cobra.Command{
		Use:   "describe <session_id>",
		Short: "Return a compact JSON summary for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()

			dbPath, err := config.IndexPath(index)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			db, err := indexdb.Open(dbPath)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("open index: %w; run: autosearch index", err)}
			}
			defer func() { _ = db.Close() }()

			sess, err := indexdb.GetSessionByID(db, args[0])
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			counts, err := indexdb.CountSessionMessages(db, args[0])
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			summary := transcriptSummary(sess.TranscriptTruncated)
			elapsed := time.Since(start).Milliseconds()

			out := map[string]any{
				"_meta": map[string]any{
					"request_id": requestID,
					"elapsed_ms": elapsed,
				},
				"session": map[string]any{
					"id":                sess.SessionID,
					"firstMessageAt":    sess.FirstMessageAt,
					"lastMessageAt":     sess.LastMessageAt,
					"totalTokens":       sess.TotalTokens,
					"totalBytes":        sess.TotalBytes,
					"workspace":         sess.Workspace,
					"gitRemote":         sess.GitRemote,
					"model":             sess.Model,
					"totalMessages":     counts.Total,
					"toolMessages":      counts.Tool,
					"bashMessages":      counts.Bash,
					"readFileMessages":  counts.ReadFile,
					"writeFileMessages": counts.WriteFile,
					"skillMessages":     counts.Skill,
					"skillsUsed":        counts.SkillsUsed,
					"transcriptSummary": summary,
				},
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

// messageContent returns the display content for a message row.
// For tool_use rows (assistant + tool_name set + empty content), it returns
// the tool_input JSON so the agent's intent is visible in the transcript.
func messageContent(m *indexdb.MessageRow) string {
	if m.Content != "" {
		return m.Content
	}
	if m.Role == "assistant" && m.ToolName != "" && m.ToolInput != "" {
		return m.ToolInput
	}
	return ""
}

// roleTag returns an XML-like opening and closing tag pair for session get rendering.
func roleTag(m *indexdb.MessageRow) (string, string) {
	var tagName string
	switch m.Role {
	case "assistant":
		tagName = "agent"
	case "user", "tool", "system":
		tagName = m.Role
	default:
		tagName = m.Role
	}

	// Build attributes.
	attrs := fmt.Sprintf(" index=%d", m.MessageIndex)

	// Tool-specific attributes: cmd= for Bash, path= for file tools.
	if m.Role == "tool" && m.ToolName != "" {
		attrs += fmt.Sprintf(" name=%q", m.ToolName)
		switch m.ToolName {
		case "Bash":
			if m.BashCommand != "" {
				attrs += fmt.Sprintf(" cmd=%q", truncateStr(m.BashCommand, maxToolArgPreview))
			}
		case "Read", "Write", "Edit", "Glob":
			if m.ToolFilePath != "" {
				attrs += fmt.Sprintf(" path=%q", truncateStr(m.ToolFilePath, maxToolArgPreview))
			}
		case "Skill":
			if m.SkillName != "" {
				attrs += fmt.Sprintf(" skill=%q", m.SkillName)
			}
		}
	}

	return fmt.Sprintf("<%s%s>", tagName, attrs), fmt.Sprintf("</%s>", tagName)
}

// truncateStr trims s to maxLen, appending "…" if it was shortened.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// midTruncate truncates long text by cutting from the middle.
// When truncated, it includes a hint to retrieve the full message.
func midTruncate(s string, maxLen int, messageID string) string {
	if len(s) <= maxLen {
		return s
	}
	marker := fmt.Sprintf("\n…[truncated — run: autosearch message get %s]…\n", messageID)
	half := (maxLen - len(marker)) / 2
	half = max(half, 0)
	return s[:half] + marker + s[len(s)-half:]
}

// transcriptSummary builds a bounded summary from the first and last N chars.
func transcriptSummary(transcript string) string {
	const n = 300
	transcript = strings.TrimSpace(transcript)
	if len(transcript) <= n*2+10 {
		return transcript
	}
	return transcript[:n] + "\n...\n" + transcript[len(transcript)-n:]
}
