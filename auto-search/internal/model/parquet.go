package model

// ParquetSessionRow mirrors the autoetl AgentSession parquet schema.
// Fields match the parquet column names used by autoetl.
type ParquetSessionRow struct {
	ID              string `parquet:"id"`
	ParentSessionID string `parquet:"parent_session_id,dict"`
	HostID          string `parquet:"host_id,dict"`
	Agent           string `parquet:"agent,dict"`
	SubagentName    string `parquet:"subagent_name,dict"`
	IsSubagent      bool   `parquet:"is_subagent"`
	Workspace       string `parquet:"workspace,dict"`
	GitRemote       string `parquet:"git_remote,dict"`
	Model           string `parquet:"model,dict"`
	SourcePath      string `parquet:"source_path"`

	FirstMessageAt int64 `parquet:"first_message_at"`
	LastMessageAt  int64 `parquet:"last_message_at"`

	// TotalTurnDurationMs mirrors auto-etl AgentSession.TotalTurnDurationMs.
	// Sum of per-turn `system / turn_duration` durations; real wall-clock
	// work time, distinct from the calendar span.
	TotalTurnDurationMs int64 `parquet:"total_turn_duration_ms"`

	TotalInputTokens  int64 `parquet:"total_input_tokens"`
	TotalOutputTokens int64 `parquet:"total_output_tokens"`
	TotalTokens       int64 `parquet:"total_tokens"`
	TotalBytes        int64 `parquet:"total_bytes"`
	TotalOutputBytes  int64 `parquet:"total_output_bytes"`
	TotalInputBytes   int64 `parquet:"total_input_bytes"`

	TranscriptFull      string `parquet:"transcript_full"`
	TranscriptTruncated string `parquet:"transcript_truncated"`

	Year          int32 `parquet:"year"`
	Month         int32 `parquet:"month"`
	SchemaVersion int32 `parquet:"schema_version"`
}

// ParquetMessageRow mirrors the autoetl AgentMessage parquet schema.
type ParquetMessageRow struct {
	ID        string `parquet:"id"`
	SessionID string `parquet:"session_id,dict"`
	HostID    string `parquet:"host_id,dict"`
	Index     int32  `parquet:"index"`

	Role             string `parquet:"role,dict"`
	Content          string `parquet:"content"`
	ContentTruncated string `parquet:"content_truncated"`
	Timestamp        int64  `parquet:"timestamp"`

	ToolName           string `parquet:"tool_name,dict"`
	ToolInput          string `parquet:"tool_input"`
	ToolFilePath       string `parquet:"tool_file_path,dict"`
	ToolFileStartLine  int32  `parquet:"tool_file_start_line"`
	ToolFileNumLines   int32  `parquet:"tool_file_num_lines"`
	ToolFileTotalLines int32  `parquet:"tool_file_total_lines"`
	BashCommand        string `parquet:"bash_command"`
	BashExitCode       int32  `parquet:"bash_exit_code"`
	SkillName          string `parquet:"skill_name,dict"`

	InputTokens      int32 `parquet:"input_tokens"`
	CacheInputTokens int32 `parquet:"cache_input_tokens"`
	OutputTokens     int32 `parquet:"output_tokens"`

	Workspace       string `parquet:"workspace,dict"`
	GitRemote       string `parquet:"git_remote,dict"`
	GitBranch       string `parquet:"git_branch,dict"`
	Model           string `parquet:"model,dict"`
	ParentSessionID string `parquet:"parent_session_id,dict"`
	IsSubagent      bool   `parquet:"is_subagent"`
	SourceLineIndex int32  `parquet:"source_line_index"`

	Year          int32 `parquet:"year"`
	Week          int32 `parquet:"week"`
	Month         int32 `parquet:"month"`
	SchemaVersion int32 `parquet:"schema_version"`
}
