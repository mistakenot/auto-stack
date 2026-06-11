package model

import "time"

const SchemaVersion = 6

// Default truncation threshold for content_truncated (chars).
const DefaultTruncateMaxChars = 4096

// IntentTruncateMaxChars caps the single-line first-user-intent preview (runes).
const IntentTruncateMaxChars = 200

// Default transcript truncation cap (chars).
const DefaultTranscriptMaxChars = 512 * 1024 // 512k

// MessageRole represents the role of a message sender.
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
	RoleSystem    MessageRole = "system"
	RoleThinking  MessageRole = "thinking"
)

// AgentMessage represents a single normalized message in a session.
type AgentMessage struct {
	ID        string `parquet:"id"`
	SessionID string `parquet:"session_id,dict"`
	HostID    string `parquet:"host_id,dict"`
	Index     int32  `parquet:"index"`

	Role             string `parquet:"role,dict"`
	Content          string `parquet:"content"`
	ContentTruncated string `parquet:"content_truncated"`
	// Unix milliseconds
	Timestamp int64 `parquet:"timestamp"`

	ToolName           string `parquet:"tool_name,dict"`
	ToolInput          string `parquet:"tool_input"`
	ToolFilePath       string `parquet:"tool_file_path,dict"`
	ToolFileStartLine  int32  `parquet:"tool_file_start_line"`
	ToolFileNumLines   int32  `parquet:"tool_file_num_lines"`
	ToolFileTotalLines int32  `parquet:"tool_file_total_lines"`
	BashCommand        string `parquet:"bash_command"`
	BashExitCode       int32  `parquet:"bash_exit_code"`
	SkillName          string `parquet:"skill_name,dict"`
	ToolUseResultJSON  string `parquet:"tool_use_result_json"`

	// ToolUseID is the canonical pairing key linking a `tool_use` block
	// (originator on an assistant message) to its matching `tool_result`
	// block (on the subsequent user/tool message). Set on both rows of the
	// pair. Empty for non-tool messages. Lets downstream queries do an exact
	// JOIN that works even when the agent dispatches multiple tool calls in
	// parallel — adjacency-based pairing breaks for parallel calls.
	ToolUseID string `parquet:"tool_use_id"`

	// DurationMs is the per-tool-call wall-clock duration in milliseconds.
	// Populated on `tool_result` rows. Source preference:
	//   1. `toolUseResult.durationMs` from the raw JSONL envelope (Claude's
	//      own measurement; accounts for interruption).
	//   2. `tool_result.timestamp - tool_use.timestamp` fallback when (1) is
	//      absent.
	// Zero on rows where neither is available (e.g. tool_use without a
	// paired result, non-tool messages).
	DurationMs int64 `parquet:"duration_ms"`

	// Interrupted is true when the raw `toolUseResult.interrupted` envelope
	// flag is set — Claude's literal signal that a tool call was cancelled
	// or stuck (e.g. user-interrupted Bash). Only meaningful on
	// `tool_result` rows.
	Interrupted bool `parquet:"interrupted"`

	InputTokens      int32 `parquet:"input_tokens"`
	CacheInputTokens int32 `parquet:"cache_input_tokens"`
	OutputTokens     int32 `parquet:"output_tokens"`

	// ThinkingSignature is the opaque per-block token from thinking blocks.
	// For normal thinking blocks: the signature string.
	// For redacted_thinking blocks: the encrypted data payload.
	ThinkingSignature string `parquet:"thinking_signature"`
	StopReason        string `parquet:"stop_reason,dict"`
	IsError           bool   `parquet:"is_error"`

	CacheCreationInputTokens int64 `parquet:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `parquet:"cache_read_input_tokens"`

	// Denormalized from session
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

// AgentSession represents a coding agent session.
type AgentSession struct {
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

	PermissionMode string `parquet:"permission_mode,dict"`
	Version        string `parquet:"version,dict"`

	// Unix milliseconds
	FirstMessageAt int64 `parquet:"first_message_at"`
	LastMessageAt  int64 `parquet:"last_message_at"`

	// TotalTurnDurationMs is the sum of per-turn work-time durations emitted by
	// Claude Code as `system / subtype=turn_duration` events. This measures the
	// agent's actual wall-clock work time and is distinct from the calendar
	// span `LastMessageAt - FirstMessageAt`, which is inflated by idle gaps
	// (e.g. an overnight pause between turns).
	TotalTurnDurationMs int64 `parquet:"total_turn_duration_ms"`

	TotalInputTokens  int64 `parquet:"total_input_tokens"`
	TotalOutputTokens int64 `parquet:"total_output_tokens"`
	TotalTokens       int64 `parquet:"total_tokens"`
	TotalBytes        int64 `parquet:"total_bytes"`
	TotalOutputBytes  int64 `parquet:"total_output_bytes"`
	TotalInputBytes   int64 `parquet:"total_input_bytes"`

	TranscriptFull      string `parquet:"transcript_full"`
	TranscriptTruncated string `parquet:"transcript_truncated"`

	// FirstUserIntent is the first "clean" user message in the session (the
	// real intent), with harness-injected junk (slash-command caveats,
	// <command-name>/<system-reminder> blocks, interrupt markers) skipped.
	// Falls back to the slash-command invocation (e.g. "/execute-task 014")
	// when no prose exists; empty when no user message qualifies.
	FirstUserIntent string `parquet:"first_user_intent"`
	// FirstUserIntentTruncated is FirstUserIntent collapsed to a single line
	// and head-truncated to IntentTruncateMaxChars runes for previews.
	FirstUserIntentTruncated string `parquet:"first_user_intent_truncated"`

	Year          int32 `parquet:"year"`
	Month         int32 `parquet:"month"`
	SchemaVersion int32 `parquet:"schema_version"`
}

// PartitionKey holds time-based partition coordinates.
type PartitionKey struct {
	Year  int
	Week  int
	Month int
}

// WeekPartition returns a PartitionKey with year and ISO week number.
func WeekPartition(t time.Time) PartitionKey {
	year, week := t.ISOWeek()
	return PartitionKey{Year: year, Week: week}
}

// MonthPartition returns a PartitionKey with year and month.
func MonthPartition(t time.Time) PartitionKey {
	return PartitionKey{Year: t.Year(), Month: int(t.Month())}
}
