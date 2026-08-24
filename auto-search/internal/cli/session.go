package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mistakenot/auto-search/internal/config"
	"github.com/mistakenot/auto-search/internal/indexdb"
	"github.com/mistakenot/auto-search/internal/search"
	"github.com/mistakenot/auto-search/internal/sessionhtml"
	"github.com/spf13/cobra"
)

const (
	maxMessageRenderLen = 2048
	maxToolArgPreview   = 80

	// exportSizeWarnBytes is the practical ceiling for a localhost-loaded HTML
	// file; past it the command suggests --light / --exclude-thinking. ~5 MB.
	exportSizeWarnBytes = 5 * 1024 * 1024
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
		newSessionExportCmd(),
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
	var subagent bool
	var noSubagent bool
	var minDuration string
	var minToolDuration string
	var interrupted bool
	var minTokens int64
	var minMessages int
	var minErrors int
	var parentSession string
	var sortBy string
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
				return &ExitError{Code: 1, Err: errors.New("--cwd and --remote are mutually exclusive")}
			}
			if subagent && noSubagent {
				return &ExitError{Code: 1, Err: errors.New("--subagent and --no-subagent are mutually exclusive")}
			}

			sortBy = strings.ToLower(strings.TrimSpace(sortBy))
			switch sortBy {
			case "", "recency":
				sortBy = ""
			case "duration", "tool_duration", "tokens", "messages", "errors":
			default:
				return &ExitError{Code: 1, Err: fmt.Errorf("invalid --sort-by value %q (use recency, duration, tool_duration, tokens, messages, or errors)", sortBy)}
			}

			dbPath, err := config.IndexPath(index)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			db, err := indexdb.Open(dbPath)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("open index: %w; run: auto search index", err)}
			}
			defer func() { _ = db.Close() }()

			tf, err := search.ParseTimeFilter(time.Now(), since, after, before)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if limit == 0 {
				limit = 50
			}

			opts := indexdb.ListSessionsOpts{
				Workspace: cwd,
				Remote:    remote,
				StartMs:   tf.StartMs,
				EndMs:     tf.EndMs,
				SortBy:    sortBy,
				Limit:     limit,
				Offset:    offset,
			}

			if subagent {
				v := true
				opts.IsSubagent = &v
			} else if noSubagent {
				v := false
				opts.IsSubagent = &v
			}

			if minDuration != "" {
				ms, err := search.ParseDurationMs(minDuration)
				if err != nil {
					return &ExitError{Code: 1, Err: fmt.Errorf("invalid --min-duration: %w", err)}
				}
				opts.MinDurationMs = &ms
			}
			if minToolDuration != "" {
				ms, err := search.ParseToolDurationMs(minToolDuration)
				if err != nil {
					return &ExitError{Code: 1, Err: fmt.Errorf("invalid --min-tool-duration: %w", err)}
				}
				opts.MinToolDurationMs = &ms
			}
			if interrupted {
				opts.OnlyInterrupted = true
			}
			if minTokens > 0 {
				opts.MinTokens = &minTokens
			}
			if minMessages > 0 {
				opts.MinMessages = &minMessages
			}
			if minErrors > 0 {
				opts.MinErrors = &minErrors
			}
			if parentSession != "" {
				opts.ParentSessionID = parentSession
			}

			sessions, total, err := indexdb.ListSessions(db, &opts)
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
	cmd.Flags().BoolVar(&subagent, "subagent", false, "show only sub-agent sessions")
	cmd.Flags().BoolVar(&noSubagent, "no-subagent", false, "show only parent (non-sub-agent) sessions")
	cmd.Flags().StringVar(&minDuration, "min-duration", "", "minimum session calendar span (e.g. 10m, 1h, 5d)")
	cmd.Flags().StringVar(&minToolDuration, "min-tool-duration", "", "include only sessions with a tool call >= this duration (e.g. 60s, 5m, 1500ms)")
	cmd.Flags().BoolVar(&interrupted, "interrupted", false, "include only sessions with at least one interrupted tool call")
	cmd.Flags().Int64Var(&minTokens, "min-tokens", 0, "minimum total tokens (e.g. 1000000)")
	cmd.Flags().IntVar(&minMessages, "min-messages", 0, "minimum message count")
	cmd.Flags().IntVar(&minErrors, "min-errors", 0, "minimum bash error count (non-zero exit codes)")
	cmd.Flags().StringVar(&parentSession, "parent-session", "", "filter by parent session ID")
	cmd.Flags().StringVar(&sortBy, "sort-by", "", "sort order: recency (default), duration (calendar span), tool_duration (real work time), tokens, messages, errors")
	cmd.Flags().IntVar(&limit, "limit", 0, "max sessions to return (default 50)")
	cmd.Flags().IntVar(&offset, "offset", 0, "pagination offset (0-based)")
	cmd.Flags().StringVar(&requestID, "request-id", "", "request identifier to echo in responses")
	return cmd
}

func newSessionGetCmd() *cobra.Command {
	var index string
	var includeThinking bool

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
				return &ExitError{Code: 1, Err: fmt.Errorf("open index: %w; run: auto search index", err)}
			}
			defer func() { _ = db.Close() }()

			msgs, err := indexdb.SessionMessages(db, args[0], includeThinking)
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
	cmd.Flags().BoolVar(&includeThinking, "include-thinking", false, "include thinking messages in the transcript (excluded by default)")
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
				return &ExitError{Code: 1, Err: fmt.Errorf("open index: %w; run: auto search index", err)}
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
					"id":                  sess.SessionID,
					"firstUserIntent":     sess.FirstUserIntent,
					"parentSessionId":     sess.ParentSessionID,
					"subagentName":        sess.SubagentName,
					"isSubagent":          sess.IsSubagent,
					"firstMessageAt":      sess.FirstMessageAt,
					"lastMessageAt":       sess.LastMessageAt,
					"durationMs":          sess.LastMessageAt - sess.FirstMessageAt,
					"totalTurnDurationMs": sess.TotalTurnDurationMs,
					"totalTokens":         sess.TotalTokens,
					"totalBytes":          sess.TotalBytes,
					"workspace":           sess.Workspace,
					"gitRemote":           sess.GitRemote,
					"model":               sess.Model,
					"totalMessages":       counts.Total,
					"userMessages":        counts.User,
					"toolMessages":        counts.Tool,
					"bashMessages":        counts.Bash,
					"readFileMessages":    counts.ReadFile,
					"writeFileMessages":   counts.WriteFile,
					"skillMessages":       counts.Skill,
					"skillsUsed":          counts.SkillsUsed,
					"transcriptSummary":   summary,
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

// newSessionExportCmd writes a rich, self-contained HTML map of a session and
// its sub-agents. Unlike the JSON-default convention this command writes a
// file; the path and size go to stderr while stdout stays empty (reserved for
// a future `--out -` streaming mode).
func newSessionExportCmd() *cobra.Command {
	var (
		index           string
		format          string
		out             string
		excludeThinking bool
		light           bool
	)

	cmd := &cobra.Command{
		Use:   "export <session_id>",
		Short: "Export a session as a self-contained HTML work-graph map",
		Long: "Export a session (and its sub-agents) to a single self-contained " +
			"HTML file that reads top-to-bottom with a collapsible work-graph. " +
			"Embeds full content including thinking by default; use --exclude-thinking " +
			"or --light to shrink an oversized export.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]

			// Validate --format up front (fail-fast on invalid CLI usage).
			switch format {
			case "html":
			case "json":
				return &ExitError{Code: 1, Err: errors.New("format json is reserved — not yet implemented; use --format html")}
			default:
				return &ExitError{Code: 1, Err: fmt.Errorf("invalid --format %q (use html)", format)}
			}

			dbPath, err := config.IndexPath(index)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			db, err := indexdb.Open(dbPath)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("open index: %w; run: auto search index", err)}
			}
			defer func() { _ = db.Close() }()

			// Detect an unknown session before building so we never write an
			// empty file (matches `session describe`).
			if _, err := indexdb.GetSessionByID(db, sessionID); err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("session not found: %s; run: auto search index", sessionID)}
			}

			opts := sessionhtml.Options{IncludeThinking: !excludeThinking && !light, Light: light}
			model, err := sessionhtml.BuildModel(db, sessionID, opts)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("build session model: %w", err)}
			}
			doc, err := sessionhtml.Render(model)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("render session: %w", err)}
			}

			outPath := out
			if outPath == "" {
				outPath = filepath.Join("docs", "sessions", sessionID+".html")
			}
			if dir := filepath.Dir(outPath); dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return &ExitError{Code: 1, Err: fmt.Errorf("create output directory %s: %w", dir, err)}
				}
			}
			if err := os.WriteFile(outPath, doc, 0o644); err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("write %s: %w", outPath, err)}
			}

			// Diagnostics to stderr; stdout stays clean.
			stderr := cmd.ErrOrStderr()
			size := len(doc)
			fmt.Fprintf(stderr, "wrote %s (%s)\n", outPath, humanBytes(size))
			if size > exportSizeWarnBytes {
				fmt.Fprintf(stderr, "warning: %s exceeds ~%s — re-run with --light or --exclude-thinking to shrink it\n",
					humanBytes(size), humanBytes(exportSizeWarnBytes))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&index, "index", config.DefaultIndexName, "named index to query")
	cmd.Flags().StringVar(&format, "format", "html", "output format: html (default); json reserved for later")
	cmd.Flags().StringVar(&out, "out", "", "output path (default ./docs/sessions/<session_id>.html)")
	cmd.Flags().BoolVar(&excludeThinking, "exclude-thinking", false, "drop thinking blocks from the export (smaller file)")
	cmd.Flags().BoolVar(&light, "light", false, "shrink a large export: exclude thinking and use truncated content")
	return cmd
}

// humanBytes formats a byte count as B / KB / MB with one decimal place.
func humanBytes(n int) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	if n < unit*unit {
		return fmt.Sprintf("%.1f KB", float64(n)/unit)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(unit*unit))
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
//
// For tool rows we surface the per-call timing data captured by auto-etl:
//   - duration_ms=<ms> when populated (lets users eyeball slow calls in
//     the transcript without dropping to duckdb).
//   - interrupted=true only when set (omitted on the common false case
//     to keep transcripts uncluttered).
//
// Both render unquoted to match the existing index=<n> precedent on the
// opening tag — quoted attrs (name="...", cmd="...") are reserved for
// opaque strings that may contain whitespace.
//
// Naming: snake_case attribute names match the underlying parquet column
// names. This makes "what column does this come from" trivially
// discoverable when a user is debugging.
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
				attrs += fmt.Sprintf(" cmd=%q", search.TruncateAtRune(m.BashCommand, maxToolArgPreview))
			}
		case "Read", "Write", "Edit", "Glob":
			if m.ToolFilePath != "" {
				attrs += fmt.Sprintf(" path=%q", search.TruncateAtRune(m.ToolFilePath, maxToolArgPreview))
			}
		case "Skill":
			if m.SkillName != "" {
				attrs += fmt.Sprintf(" skill=%q", m.SkillName)
			}
		}
		// Per-call timing. Only render when populated so empty/non-tool
		// rows stay clean. duration_ms covers expected-slow detection;
		// interrupted=true covers hang/cancel detection — together they
		// answer the "stuck vs expected-slow" classifier question.
		if m.DurationMs > 0 {
			attrs += fmt.Sprintf(" duration_ms=%d", m.DurationMs)
		}
		if m.Interrupted {
			attrs += " interrupted=true"
		}
	}

	return fmt.Sprintf("<%s%s>", tagName, attrs), fmt.Sprintf("</%s>", tagName)
}

// runeSafePrefix returns the prefix of s up to at most n bytes, backing the
// cut off to the nearest rune boundary at or before n so the result is always
// valid UTF-8 (never slicing through a multi-byte sequence).
func runeSafePrefix(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// runeSafeSuffix returns the suffix of s of at most n bytes, advancing the cut
// forward to the nearest rune boundary so the result is always valid UTF-8.
func runeSafeSuffix(s string, n int) string {
	if n >= len(s) {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

// midTruncate truncates long text by cutting from the middle.
// When truncated, it includes a hint to retrieve the full message.
func midTruncate(s string, maxLen int, messageID string) string {
	if len(s) <= maxLen {
		return s
	}
	marker := fmt.Sprintf("\n…[truncated — run: auto search message get %s]…\n", messageID)
	half := (maxLen - len(marker)) / 2
	half = max(half, 0)
	return runeSafePrefix(s, half) + marker + runeSafeSuffix(s, half)
}

// transcriptSummary builds a bounded summary from the first and last N chars.
func transcriptSummary(transcript string) string {
	const n = 300
	transcript = strings.TrimSpace(transcript)
	if len(transcript) <= n*2+10 {
		return transcript
	}
	return runeSafePrefix(transcript, n) + "\n...\n" + runeSafeSuffix(transcript, n)
}
