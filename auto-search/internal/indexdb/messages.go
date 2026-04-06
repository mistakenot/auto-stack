package indexdb

import (
	"database/sql"
	"fmt"
)

// InsertMessage inserts a message row into the database within a transaction.
func InsertMessage(tx *sql.Tx, partitionSourcePath string,
	messageID, sessionID, hostID string,
	messageIndex int,
	role, content, contentTruncated string,
	timestamp int64,
	toolName, toolInput, toolFilePath string,
	toolFileStartLine, toolFileNumLines, toolFileTotalLines int,
	bashCommand string,
	inputTokens, cacheInputTokens, outputTokens int,
	workspace, gitRemote, gitBranch, model string,
	parentSessionID string,
	isSubagent bool,
	sourceLineIndex, schemaVersion int,
) error {
	isSubagentInt := 0
	if isSubagent {
		isSubagentInt = 1
	}
	_, err := tx.Exec(`
		INSERT INTO messages (
			partition_source_path, message_id, session_id, host_id,
			message_index, role, content, content_truncated, timestamp,
			tool_name, tool_input, tool_file_path,
			tool_file_start_line, tool_file_num_lines, tool_file_total_lines,
			bash_command, input_tokens, cache_input_tokens, output_tokens,
			workspace, git_remote, git_branch, model,
			parent_session_id, is_subagent, source_line_index, schema_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		partitionSourcePath, messageID, sessionID, hostID,
		messageIndex, role, content, contentTruncated, timestamp,
		toolName, toolInput, toolFilePath,
		toolFileStartLine, toolFileNumLines, toolFileTotalLines,
		bashCommand, inputTokens, cacheInputTokens, outputTokens,
		workspace, gitRemote, gitBranch, model,
		parentSessionID, isSubagentInt, sourceLineIndex, schemaVersion,
	)
	if err != nil {
		return fmt.Errorf("insert message %s: %w", messageID, err)
	}
	return nil
}
