package indexdb

import (
	"database/sql"
	"fmt"
)

// MessageRow holds the full data for a single indexed message.
type MessageRow struct {
	DocID               int64
	PartitionSourcePath string
	MessageID           string
	SessionID           string
	HostID              string
	MessageIndex        int
	Role                string
	Content             string
	ContentTruncated    string
	Timestamp           int64
	ToolName            string
	ToolInput           string
	ToolFilePath        string
	ToolFileStartLine   int
	ToolFileNumLines    int
	ToolFileTotalLines  int
	BashCommand         string
	BashExitCode        int
	SkillName           string
	ToolUseResultJSON   string
	InputTokens         int
	CacheInputTokens    int
	OutputTokens        int
	Workspace           string
	GitRemote           string
	GitBranch           string
	Model               string
	ParentSessionID     string
	IsSubagent          bool
	SourceLineIndex     int
	SchemaVersion       int
}

// GetMessageByID loads one message row by message_id.
func GetMessageByID(db *sql.DB, messageID string) (*MessageRow, error) {
	row := db.QueryRow(`
		SELECT doc_id, partition_source_path, message_id, session_id, host_id,
			message_index, role, content, content_truncated, timestamp,
			tool_name, tool_input, tool_file_path,
			tool_file_start_line, tool_file_num_lines, tool_file_total_lines,
			bash_command, bash_exit_code, skill_name, tool_use_result_json, input_tokens, cache_input_tokens, output_tokens,
			workspace, git_remote, git_branch, model,
			parent_session_id, is_subagent, source_line_index, schema_version
		FROM messages
		WHERE message_id = ?
	`, messageID)

	m := &MessageRow{}
	var isSubagentInt int
	err := row.Scan(
		&m.DocID, &m.PartitionSourcePath, &m.MessageID, &m.SessionID, &m.HostID,
		&m.MessageIndex, &m.Role, &m.Content, &m.ContentTruncated, &m.Timestamp,
		&m.ToolName, &m.ToolInput, &m.ToolFilePath,
		&m.ToolFileStartLine, &m.ToolFileNumLines, &m.ToolFileTotalLines,
		&m.BashCommand, &m.BashExitCode, &m.SkillName, &m.ToolUseResultJSON, &m.InputTokens, &m.CacheInputTokens, &m.OutputTokens,
		&m.Workspace, &m.GitRemote, &m.GitBranch, &m.Model,
		&m.ParentSessionID, &isSubagentInt, &m.SourceLineIndex, &m.SchemaVersion,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("message not found: %s", messageID)
	}
	if err != nil {
		return nil, fmt.Errorf("query message %s: %w", messageID, err)
	}
	m.IsSubagent = isSubagentInt != 0
	return m, nil
}

// NeighborMessageIDs returns the previous and next message_id for a message
// within its session, based on message_index ordering.
func NeighborMessageIDs(db *sql.DB, sessionID string, messageIndex int) (prev, next string, err error) {
	// Previous message.
	err = db.QueryRow(`
		SELECT message_id FROM messages
		WHERE session_id = ? AND message_index < ?
		ORDER BY message_index DESC LIMIT 1
	`, sessionID, messageIndex).Scan(&prev)
	if err == sql.ErrNoRows {
		prev = ""
	} else if err != nil {
		return "", "", fmt.Errorf("query prev message: %w", err)
	}

	// Next message.
	err = db.QueryRow(`
		SELECT message_id FROM messages
		WHERE session_id = ? AND message_index > ?
		ORDER BY message_index ASC LIMIT 1
	`, sessionID, messageIndex).Scan(&next)
	if err == sql.ErrNoRows {
		next = ""
	} else if err != nil {
		return "", "", fmt.Errorf("query next message: %w", err)
	}

	return prev, next, nil
}

// SessionTimeRange returns first_message_at and last_message_at for a session.
func SessionTimeRange(db *sql.DB, sessionID string) (firstAt, lastAt int64, err error) {
	err = db.QueryRow(`
		SELECT first_message_at, last_message_at FROM sessions
		WHERE session_id = ?
	`, sessionID).Scan(&firstAt, &lastAt)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("query session time range: %w", err)
	}
	return firstAt, lastAt, nil
}
