package indexdb

import (
	"database/sql"
	"fmt"
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
			bash_command, input_tokens, cache_input_tokens, output_tokens,
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
			&m.BashCommand, &m.InputTokens, &m.CacheInputTokens, &m.OutputTokens,
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
	Total     int
	Tool      int
	Bash      int
	ReadFile  int
	WriteFile int
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
	return c, nil
}
