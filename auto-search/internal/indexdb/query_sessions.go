package indexdb

import (
	"database/sql"
	"fmt"
	"strings"
)

// SessionRow holds the full data for a single indexed session.
type SessionRow struct {
	DocID               int64
	PartitionSourcePath string
	SessionID           string
	ParentSessionID     string
	HostID              string
	Agent               string
	SubagentName        string
	IsSubagent          bool
	Workspace           string
	GitRemote           string
	Model               string
	SourcePath          string
	FirstMessageAt      int64
	LastMessageAt       int64
	TotalTurnDurationMs int64
	TotalInputTokens    int64
	TotalOutputTokens   int64
	TotalTokens         int64
	TotalBytes          int64
	TotalOutputBytes    int64
	TotalInputBytes     int64
	TranscriptTruncated string
	SchemaVersion       int
}

// ListSessionsOpts holds optional filters for ListSessions.
type ListSessionsOpts struct {
	Workspace       string // filter by workspace path (case-insensitive substring)
	Remote          string // filter by git_remote (case-insensitive substring)
	StartMs         *int64 // inclusive lower bound on first_message_at
	EndMs           *int64 // exclusive upper bound on first_message_at
	IsSubagent      *bool  // nil = no filter, true = only subagents, false = only parents
	MinDurationMs   *int64 // filter: (last_message_at - first_message_at) >= this value
	MinTokens       *int64 // filter: total_tokens >= this value
	MinMessages     *int   // filter: message_count >= this value
	MinErrors       *int   // filter: error_count >= this value
	ParentSessionID string // filter by parent_session_id (exact match)
	SortBy          string // "recency" (default), "duration", "tokens", "messages", "errors"
	Limit           int    // max rows; 0 means default (50)
	Offset          int    // pagination offset
}

// SessionListRow is a compact session summary for list output.
type SessionListRow struct {
	SessionID       string `json:"session_id"`
	ParentSessionID string `json:"parent_session_id,omitempty"`
	SubagentName    string `json:"subagent_name,omitempty"`
	IsSubagent      bool   `json:"is_subagent"`
	Workspace       string `json:"workspace"`
	GitRemote       string `json:"git_remote"`
	Model           string `json:"model"`
	Agent           string `json:"agent"`
	FirstMessageAt  int64  `json:"first_message_at"`
	LastMessageAt   int64  `json:"last_message_at"`
	DurationMs      int64  `json:"duration_ms"`
	TotalTokens     int64  `json:"total_tokens"`
	MessageCount    int    `json:"message_count"`
	ErrorCount      int    `json:"error_count"`
}

// ListSessions queries the sessions table directly (no FTS) with optional filters.
func ListSessions(db *sql.DB, opts *ListSessionsOpts) ([]SessionListRow, int, error) {
	if opts.Limit < 0 {
		return nil, 0, fmt.Errorf("invalid limit: %d (must be >= 0)", opts.Limit)
	}
	if opts.Offset < 0 {
		return nil, 0, fmt.Errorf("invalid offset: %d (must be >= 0)", opts.Offset)
	}
	if opts.Limit == 0 {
		opts.Limit = 50
	}

	var where []string
	var args []any

	if opts.Workspace != "" {
		where = append(where, "s.workspace LIKE ?")
		args = append(args, "%"+opts.Workspace+"%")
	}
	if opts.Remote != "" {
		where = append(where, "s.git_remote LIKE ?")
		args = append(args, "%"+opts.Remote+"%")
	}
	if opts.StartMs != nil {
		where = append(where, "s.first_message_at >= ?")
		args = append(args, *opts.StartMs)
	}
	if opts.EndMs != nil {
		where = append(where, "s.first_message_at < ?")
		args = append(args, *opts.EndMs)
	}
	if opts.IsSubagent != nil {
		if *opts.IsSubagent {
			where = append(where, "s.is_subagent = 1")
		} else {
			where = append(where, "s.is_subagent = 0")
		}
	}
	if opts.MinDurationMs != nil {
		where = append(where, "(s.last_message_at - s.first_message_at) >= ?")
		args = append(args, *opts.MinDurationMs)
	}
	if opts.MinTokens != nil {
		where = append(where, "s.total_tokens >= ?")
		args = append(args, *opts.MinTokens)
	}
	if opts.ParentSessionID != "" {
		where = append(where, "s.parent_session_id = ?")
		args = append(args, opts.ParentSessionID)
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// MinMessages and MinErrors filter on computed JOIN columns, so they need
	// the LEFT JOIN in both the count and main queries.
	needsJoin := opts.MinMessages != nil || opts.MinErrors != nil ||
		opts.SortBy == "messages" || opts.SortBy == "errors"

	// Build join-dependent WHERE conditions using raw expressions.
	var joinWhere []string
	var joinWhereArgs []any
	if opts.MinMessages != nil {
		joinWhere = append(joinWhere, "COALESCE(mc.cnt, 0) >= ?")
		joinWhereArgs = append(joinWhereArgs, *opts.MinMessages)
	}
	if opts.MinErrors != nil {
		joinWhere = append(joinWhere, "COALESCE(mc.err_cnt, 0) >= ?")
		joinWhereArgs = append(joinWhereArgs, *opts.MinErrors)
	}

	joinWhereClause := ""
	if len(joinWhere) > 0 {
		if whereClause == "" {
			joinWhereClause = "WHERE " + strings.Join(joinWhere, " AND ")
		} else {
			joinWhereClause = " AND " + strings.Join(joinWhere, " AND ")
		}
	}

	joinSQL := `
		LEFT JOIN (
			SELECT session_id,
				COUNT(*) AS cnt,
				SUM(CASE WHEN bash_exit_code > 0 THEN 1 ELSE 0 END) AS err_cnt
			FROM messages GROUP BY session_id
		) mc ON mc.session_id = s.session_id`

	// Count total matching rows for pagination metadata.
	var total int
	countArgs := append([]any{}, args...)
	if needsJoin {
		countSQL := fmt.Sprintf(`
			SELECT COUNT(*) FROM sessions s %s %s%s`,
			joinSQL, whereClause, joinWhereClause)
		countArgs = append(countArgs, joinWhereArgs...)
		if err := db.QueryRow(countSQL, countArgs...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count sessions: %w", err)
		}
	} else {
		countSQL := "SELECT COUNT(*) FROM sessions s " + whereClause
		if err := db.QueryRow(countSQL, countArgs...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count sessions: %w", err)
		}
	}

	orderBy := "s.first_message_at DESC"
	switch opts.SortBy {
	case "duration":
		orderBy = "(s.last_message_at - s.first_message_at) DESC"
	case "tokens":
		orderBy = "s.total_tokens DESC"
	case "messages":
		orderBy = "message_count DESC"
	case "errors":
		orderBy = "error_count DESC"
	}

	// Fetch rows with a LEFT JOIN to get message and error counts.
	querySQL := fmt.Sprintf(`
		SELECT s.session_id, s.parent_session_id, s.subagent_name, s.is_subagent,
			s.workspace, s.git_remote, s.model, s.agent,
			s.first_message_at, s.last_message_at,
			(s.last_message_at - s.first_message_at) AS duration_ms,
			s.total_tokens,
			COALESCE(mc.cnt, 0) AS message_count,
			COALESCE(mc.err_cnt, 0) AS error_count
		FROM sessions s
		%s
		%s%s
		ORDER BY %s
		LIMIT ? OFFSET ?
	`, joinSQL, whereClause, joinWhereClause, orderBy)
	args = append(args, joinWhereArgs...)
	args = append(args, opts.Limit, opts.Offset)

	rows, err := db.Query(querySQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := []SessionListRow{}
	for rows.Next() {
		var r SessionListRow
		var isSubagentInt int
		if err := rows.Scan(
			&r.SessionID, &r.ParentSessionID, &r.SubagentName, &isSubagentInt,
			&r.Workspace, &r.GitRemote, &r.Model, &r.Agent,
			&r.FirstMessageAt, &r.LastMessageAt, &r.DurationMs,
			&r.TotalTokens,
			&r.MessageCount, &r.ErrorCount,
		); err != nil {
			return nil, 0, fmt.Errorf("scan session list row: %w", err)
		}
		r.IsSubagent = isSubagentInt != 0
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate session list: %w", err)
	}

	return result, total, nil
}

// GetSessionByID loads one session row by session_id.
func GetSessionByID(db *sql.DB, sessionID string) (*SessionRow, error) {
	row := db.QueryRow(`
		SELECT doc_id, partition_source_path, session_id, parent_session_id, host_id,
			agent, subagent_name, is_subagent, workspace, git_remote, model, source_path,
			first_message_at, last_message_at, total_turn_duration_ms,
			total_input_tokens, total_output_tokens, total_tokens,
			total_bytes, total_output_bytes, total_input_bytes,
			transcript_truncated, schema_version
		FROM sessions
		WHERE session_id = ?
	`, sessionID)

	s := &SessionRow{}
	var isSubagentInt int
	err := row.Scan(
		&s.DocID, &s.PartitionSourcePath, &s.SessionID, &s.ParentSessionID, &s.HostID,
		&s.Agent, &s.SubagentName, &isSubagentInt, &s.Workspace, &s.GitRemote, &s.Model, &s.SourcePath,
		&s.FirstMessageAt, &s.LastMessageAt, &s.TotalTurnDurationMs,
		&s.TotalInputTokens, &s.TotalOutputTokens, &s.TotalTokens,
		&s.TotalBytes, &s.TotalOutputBytes, &s.TotalInputBytes,
		&s.TranscriptTruncated, &s.SchemaVersion,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("query session %s: %w", sessionID, err)
	}
	s.IsSubagent = isSubagentInt != 0
	return s, nil
}

// SessionMessages loads all messages for a session ordered by message_index.
func SessionMessages(db *sql.DB, sessionID string) ([]MessageRow, error) {
	rows, err := db.Query(`
		SELECT doc_id, partition_source_path, message_id, session_id, host_id,
			message_index, role, content, content_truncated, timestamp,
			tool_name, tool_input, tool_file_path,
			tool_file_start_line, tool_file_num_lines, tool_file_total_lines,
			bash_command, bash_exit_code, skill_name,
			tool_use_id, duration_ms, interrupted,
			input_tokens, cache_input_tokens, output_tokens,
			workspace, git_remote, git_branch, model,
			parent_session_id, is_subagent, source_line_index, schema_version
		FROM messages
		WHERE session_id = ?
		ORDER BY message_index ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query session messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var msgs []MessageRow
	for rows.Next() {
		var m MessageRow
		var isSubagentInt, interruptedInt int
		if err := rows.Scan(
			&m.DocID, &m.PartitionSourcePath, &m.MessageID, &m.SessionID, &m.HostID,
			&m.MessageIndex, &m.Role, &m.Content, &m.ContentTruncated, &m.Timestamp,
			&m.ToolName, &m.ToolInput, &m.ToolFilePath,
			&m.ToolFileStartLine, &m.ToolFileNumLines, &m.ToolFileTotalLines,
			&m.BashCommand, &m.BashExitCode, &m.SkillName,
			&m.ToolUseID, &m.DurationMs, &interruptedInt,
			&m.InputTokens, &m.CacheInputTokens, &m.OutputTokens,
			&m.Workspace, &m.GitRemote, &m.GitBranch, &m.Model,
			&m.ParentSessionID, &isSubagentInt, &m.SourceLineIndex, &m.SchemaVersion,
		); err != nil {
			return nil, fmt.Errorf("scan message row: %w", err)
		}
		m.IsSubagent = isSubagentInt != 0
		m.Interrupted = interruptedInt != 0
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// SessionMessageCounts returns counts of messages by category for a session.
type SessionMessageCounts struct {
	Total      int
	User       int
	Tool       int
	Bash       int
	ReadFile   int
	WriteFile  int
	Skill      int
	SkillsUsed []string
}

// CountSessionMessages returns categorized message counts for a session.
func CountSessionMessages(db *sql.DB, sessionID string) (SessionMessageCounts, error) {
	var c SessionMessageCounts
	err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ?", sessionID).Scan(&c.Total)
	if err != nil {
		return c, fmt.Errorf("count messages: %w", err)
	}
	err = db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ? AND role = 'user'", sessionID).Scan(&c.User)
	if err != nil {
		return c, fmt.Errorf("count user messages: %w", err)
	}
	err = db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ? AND role = 'tool'", sessionID).Scan(&c.Tool)
	if err != nil {
		return c, fmt.Errorf("count tool messages: %w", err)
	}
	err = db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ? AND tool_name = 'Bash'", sessionID).Scan(&c.Bash)
	if err != nil {
		return c, fmt.Errorf("count bash messages: %w", err)
	}
	err = db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ? AND tool_name = 'Read'", sessionID).Scan(&c.ReadFile)
	if err != nil {
		return c, fmt.Errorf("count read messages: %w", err)
	}
	err = db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ? AND (tool_name = 'Write' OR tool_name = 'Edit')", sessionID).Scan(&c.WriteFile)
	if err != nil {
		return c, fmt.Errorf("count write messages: %w", err)
	}
	err = db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ? AND skill_name != ''", sessionID).Scan(&c.Skill)
	if err != nil {
		return c, fmt.Errorf("count skill messages: %w", err)
	}
	rows, err := db.Query("SELECT DISTINCT skill_name FROM messages WHERE session_id = ? AND skill_name != '' ORDER BY skill_name", sessionID)
	if err != nil {
		return c, fmt.Errorf("query skills used: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return c, fmt.Errorf("scan skill name: %w", err)
		}
		c.SkillsUsed = append(c.SkillsUsed, name)
	}
	if err := rows.Err(); err != nil {
		return c, fmt.Errorf("iterate skills: %w", err)
	}
	return c, nil
}
