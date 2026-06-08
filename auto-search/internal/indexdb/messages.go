package indexdb

import (
	"database/sql"
	"fmt"
)

// InsertMessage inserts a message row into the database within a transaction.
// toolUseID, durationMs, and interrupted carry the per-tool-call linkage
// and timing data captured from auto-etl's `tool_use_id`, `duration_ms`,
// and `interrupted` parquet columns.
func InsertMessage(tx *sql.Tx, partitionSourcePath string,
	messageID, sessionID, hostID string,
	messageIndex int,
	role, content, contentTruncated string,
	timestamp int64,
	toolName, toolInput, toolFilePath string,
	toolFileStartLine, toolFileNumLines, toolFileTotalLines int,
	bashCommand string,
	bashExitCode int,
	skillName string,
	toolUseID string,
	durationMs int64,
	interrupted bool,
	inputTokens, cacheInputTokens, outputTokens int,
	workspace, gitRemote, gitBranch, model string,
	parentSessionID string,
	isSubagent bool,
	sourceLineIndex, schemaVersion int,
	toolUseResultJSON string,
	thinkingSignature, stopReason string,
	isError bool,
	cacheCreationInputTokens, cacheReadInputTokens int64,
) error {
	isSubagentInt := 0
	if isSubagent {
		isSubagentInt = 1
	}
	interruptedInt := 0
	if interrupted {
		interruptedInt = 1
	}
	isErrorInt := 0
	if isError {
		isErrorInt = 1
	}
	// 38 columns, 38 placeholders, 38 args.
	_, err := tx.Exec(`
		INSERT INTO messages (
			partition_source_path, message_id, session_id, host_id,
			message_index, role, content, content_truncated, timestamp,
			tool_name, tool_input, tool_file_path,
			tool_file_start_line, tool_file_num_lines, tool_file_total_lines,
			bash_command, bash_exit_code, skill_name,
			tool_use_result_json,
			tool_use_id, duration_ms, interrupted,
			input_tokens, cache_input_tokens, output_tokens,
			workspace, git_remote, git_branch, model,
			parent_session_id, is_subagent, source_line_index,
			thinking_signature, stop_reason, is_error,
			cache_creation_input_tokens, cache_read_input_tokens,
			schema_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		partitionSourcePath, messageID, sessionID, hostID,
		messageIndex, role, content, contentTruncated, timestamp,
		toolName, toolInput, toolFilePath,
		toolFileStartLine, toolFileNumLines, toolFileTotalLines,
		bashCommand, bashExitCode, skillName,
		toolUseResultJSON,
		toolUseID, durationMs, interruptedInt,
		inputTokens, cacheInputTokens, outputTokens,
		workspace, gitRemote, gitBranch, model,
		parentSessionID, isSubagentInt, sourceLineIndex,
		thinkingSignature, stopReason, isErrorInt,
		cacheCreationInputTokens, cacheReadInputTokens,
		schemaVersion,
	)
	if err != nil {
		return fmt.Errorf("insert message %s: %w", messageID, err)
	}
	return nil
}
