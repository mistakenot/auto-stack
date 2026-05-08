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
	Workspace string // filter by workspace path (case-insensitive substring)
	Remote    string // filter by git_remote (case-insensitive substring)
	StartMs   *int64 // inclusive lower bound on first_message_at
	EndMs     *int64 // exclusive upper bound on first_message_at
	Limit     int    // max rows; 0 means default (50)
	Offset    int    // pagination offset
}

// SessionListRow is a compact session summary for list output.
type SessionListRow struct {
	SessionID      string `json:"session_id"`
	Workspace      string `json:"workspace"`
	GitRemote      string `json:"git_remote"`
	Model          string `json:"model"`
	Agent          string `json:"agent"`
	FirstMessageAt int64  `json:"first_message_at"`
	LastMessageAt  int64  `json:"last_message_at"`
	TotalTokens    int64  `json:"total_tokens"`
	MessageCount   int    `json:"message_count"`
}

// ListSessions queries the sessions table directly (no FTS) with optional filters.
func ListSessions(db *sql.DB, opts ListSessionsOpts) ([]SessionListRow, int, error) {
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

	// Workspace/Remote are case-insensitive substring matches. See SubstringFilter.
	if frag, arg := SubstringFilter("s.workspace", opts.Workspace); frag != "" {
		where = append(where, frag)
		args = append(args, arg)
	}
	if frag, arg := SubstringFilter("s.git_remote", opts.Remote); frag != "" {
		where = append(where, frag)
		args = append(args, arg)
	}
	if opts.StartMs != nil {
		where = append(where, "s.first_message_at >= ?")
		args = append(args, *opts.StartMs)
	}
	if opts.EndMs != nil {
		where = append(where, "s.first_message_at < ?")
		args = append(args, *opts.EndMs)
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// Count total matching rows for pagination metadata.
	countSQL := "SELECT COUNT(*) FROM sessions s " + whereClause
	var total int
	if err := db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count sessions: %w", err)
	}

	// Fetch rows with a LEFT JOIN to get message counts.
	querySQL := fmt.Sprintf(`
		SELECT s.session_id, s.workspace, s.git_remote, s.model, s.agent,
			s.first_message_at, s.last_message_at, s.total_tokens,
			COALESCE(mc.cnt, 0) AS message_count
		FROM sessions s
		LEFT JOIN (
			SELECT session_id, COUNT(*) AS cnt FROM messages GROUP BY session_id
		) mc ON mc.session_id = s.session_id
		%s
		ORDER BY s.first_message_at DESC
		LIMIT ? OFFSET ?
	`, whereClause)
	args = append(args, opts.Limit, opts.Offset)

	rows, err := db.Query(querySQL, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := []SessionListRow{}
	for rows.Next() {
		var r SessionListRow
		if err := rows.Scan(
			&r.SessionID, &r.Workspace, &r.GitRemote, &r.Model, &r.Agent,
			&r.FirstMessageAt, &r.LastMessageAt, &r.TotalTokens,
			&r.MessageCount,
		); err != nil {
			return nil, 0, fmt.Errorf("scan session list row: %w", err)
		}
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
			first_message_at, last_message_at,
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
		&s.FirstMessageAt, &s.LastMessageAt,
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
			bash_command, skill_name, input_tokens, cache_input_tokens, output_tokens,
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
		var isSubagentInt int
		if err := rows.Scan(
			&m.DocID, &m.PartitionSourcePath, &m.MessageID, &m.SessionID, &m.HostID,
			&m.MessageIndex, &m.Role, &m.Content, &m.ContentTruncated, &m.Timestamp,
			&m.ToolName, &m.ToolInput, &m.ToolFilePath,
			&m.ToolFileStartLine, &m.ToolFileNumLines, &m.ToolFileTotalLines,
			&m.BashCommand, &m.SkillName, &m.InputTokens, &m.CacheInputTokens, &m.OutputTokens,
			&m.Workspace, &m.GitRemote, &m.GitBranch, &m.Model,
			&m.ParentSessionID, &isSubagentInt, &m.SourceLineIndex, &m.SchemaVersion,
		); err != nil {
			return nil, fmt.Errorf("scan message row: %w", err)
		}
		m.IsSubagent = isSubagentInt != 0
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// SessionMessageCounts returns counts of messages by category for a session.
type SessionMessageCounts struct {
	Total      int
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
