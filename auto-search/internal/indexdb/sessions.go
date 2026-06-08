package indexdb

import (
	"database/sql"
	"fmt"
)

// InsertSession inserts a session row into the database within a transaction.
func InsertSession(tx *sql.Tx, partitionSourcePath string,
	sessionID, parentSessionID, hostID, agent, subagentName string,
	isSubagent bool,
	workspace, gitRemote, model, sourcePath string,
	firstMessageAt, lastMessageAt, totalTurnDurationMs int64,
	totalInputTokens, totalOutputTokens, totalTokens int64,
	totalBytes, totalOutputBytes, totalInputBytes int64,
	transcriptTruncated string,
	firstUserIntent, firstUserIntentTruncated string,
	schemaVersion int,
) error {
	isSubagentInt := 0
	if isSubagent {
		isSubagentInt = 1
	}
	_, err := tx.Exec(`
		INSERT INTO sessions (
			partition_source_path, session_id, parent_session_id, host_id,
			agent, subagent_name, is_subagent, workspace, git_remote, model,
			source_path, first_message_at, last_message_at, total_turn_duration_ms,
			total_input_tokens, total_output_tokens, total_tokens,
			total_bytes, total_output_bytes, total_input_bytes,
			transcript_truncated, first_user_intent, first_user_intent_truncated,
			schema_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		partitionSourcePath, sessionID, parentSessionID, hostID,
		agent, subagentName, isSubagentInt, workspace, gitRemote, model,
		sourcePath, firstMessageAt, lastMessageAt, totalTurnDurationMs,
		totalInputTokens, totalOutputTokens, totalTokens,
		totalBytes, totalOutputBytes, totalInputBytes,
		transcriptTruncated, firstUserIntent, firstUserIntentTruncated,
		schemaVersion,
	)
	if err != nil {
		return fmt.Errorf("insert session %s: %w", sessionID, err)
	}
	return nil
}
