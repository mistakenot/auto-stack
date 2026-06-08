package parser

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ParsedSession holds all parsed data from a single JSONL session file.
type ParsedSession struct {
	ID              string
	ParentSessionID string // set for subagent files, empty for parents
	AgentID         string // hex agent ID from JSONL lines
	SubagentName    string // from .meta.json agentType
	IsSubagent      bool   // true if any line has isSidechain: true
	Workspace       string
	Model           string
	SourcePath      string
	Lines           []ParsedLine
}

// ParsedLine represents a single parsed line from a JSONL file.
type ParsedLine struct {
	Type            string
	Subtype         string // for `system` lines: e.g. "turn_duration", "init"
	Timestamp       time.Time
	SessionID       string
	Cwd             string
	GitBranch       string // from gitBranch field in JSONL
	IsSubagent      bool   // from line's isSidechain field
	AgentID         string // from line's agentId field
	SourceLineIndex int    // 0-based position in the JSONL file
	DurationMs      int64  // populated on `system / turn_duration` lines: per-turn agent work time in ms
	Message         ParsedMessage
	// ToolUseResultRaw is the raw `toolUseResult` envelope (sibling of
	// message), preserved verbatim for structured tool-output rendering.
	ToolUseResultRaw json.RawMessage
	// ToolUseResult mirrors the sibling `toolUseResult` envelope that Claude
	// Code attaches to lines carrying a `tool_result` content block. It
	// carries Claude's own per-call wall-clock measurement and the
	// `interrupted` flag (stuck / cancelled). Present only on tool_result
	// lines; absent on all others.
	ToolUseResult ParsedToolUseResult
	// Version is the Claude Code CLI version string (e.g. "2.1.168").
	Version string
	// PermissionMode is the permission mode for the session (e.g. "bypassPermissions").
	PermissionMode string
	// AttributionSkill is the skill name attributed to this assistant line
	// (e.g. "review-task").
	AttributionSkill string
}

// ParsedToolUseResult holds the fields we extract from the raw
// `toolUseResult` envelope. The envelope shape varies per tool (Bash has
// stdout/stderr/interrupted; WebFetch has bytes/durationMs; subagent Task
// has totalDurationMs; Read tools may emit a bare string). We deliberately
// extract only the cross-tool signal fields here.
type ParsedToolUseResult struct {
	// Present indicates the envelope was a JSON object (vs absent or a bare
	// string). Distinguishes "no envelope" from "envelope present but
	// durationMs=0".
	Present bool
	// DurationMs is Claude's own measured wall-clock duration in ms.
	// Populated by some tools (e.g. WebFetch, Glob with `truncated`); not
	// emitted by every tool. Prefer this over a timestamp delta when
	// available.
	DurationMs int64
	// Interrupted is true when Claude flags the call as cancelled or stuck
	// (e.g. user-interrupted Bash). The literal "stuck vs expected-slow"
	// signal.
	Interrupted bool
}

// ParsedMessage holds the message payload from a JSONL line.
type ParsedMessage struct {
	Role       string
	Content    json.RawMessage
	Model      string
	Usage      ParsedUsage
	StopReason string
}

// ParsedUsage holds token usage information.
type ParsedUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// ContentBlock represents a single block within a content array.
type ContentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"` // tool_use block ID
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	ToolUseID string          `json:"tool_use_id"` // tool_result reference
	IsError   bool            `json:"is_error"`    // tool_result error flag
	Thinking  string          `json:"thinking"`    // reasoning text from thinking blocks
	Signature string          `json:"signature"`   // opaque signature on thinking blocks
	Data      string          `json:"data"`        // encrypted payload on redacted_thinking blocks
}

// rawLine is the JSON structure of a single JSONL line from Claude session files.
type rawLine struct {
	Type        string     `json:"type"`
	Subtype     string     `json:"subtype"` // for `system` lines: e.g. "turn_duration", "init"
	SessionID   string     `json:"sessionId"`
	Cwd         string     `json:"cwd"`
	GitBranch   *string    `json:"gitBranch"` // pointer to distinguish null from missing
	Timestamp   string     `json:"timestamp"`
	IsSidechain bool       `json:"isSidechain"`
	AgentID     string     `json:"agentId"`
	DurationMs  int64      `json:"durationMs"` // populated on `system / turn_duration` lines
	Message     rawMessage `json:"message"`
	// ToolUseResult is the sibling envelope on tool_result-bearing lines.
	// It can be either an object (most tools) or a bare string (Read-style
	// tools), hence RawMessage + post-decode handling.
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	// Version is the Claude Code CLI version string (e.g. "2.1.168").
	// Top-level field on message lines.
	Version string `json:"version"`
	// PermissionMode is the permission mode for the session (e.g. "bypassPermissions").
	// Top-level field on message lines and standalone permission-mode lines.
	PermissionMode string `json:"permissionMode"`
	// AttributionSkill is the skill name attributed to this assistant line
	// (e.g. "review-task"). Top-level field on assistant lines.
	AttributionSkill string `json:"attributionSkill"`
}

// rawToolUseResult is the object-shaped subset of `toolUseResult` we
// decode. Tools whose envelope is a bare string (e.g. Read) simply do not
// populate these fields. Tools whose envelope is an object but lacks
// these keys (e.g. Edit's structuredPatch payload) leave them at zero.
type rawToolUseResult struct {
	DurationMs  int64 `json:"durationMs"`
	Interrupted bool  `json:"interrupted"`
}

type rawMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Model      string          `json:"model"`
	Usage      ParsedUsage     `json:"usage"`
	StopReason string          `json:"stop_reason"`
}

// ProgressFunc is called during processing with (current, total) counts.
type ProgressFunc func(current, total int)

// ScanAndParse walks inputDir finding all .jsonl files and parses each one.
// An optional progress callback is invoked after each file is processed.
func ScanAndParse(inputDir string, onProgress ...ProgressFunc) ([]ParsedSession, error) {
	files, err := findJSONLFiles(inputDir)
	if err != nil {
		return nil, fmt.Errorf("scanning %s: %w", inputDir, err)
	}

	var progress ProgressFunc
	if len(onProgress) > 0 {
		progress = onProgress[0]
	}

	total := len(files)
	var sessions []ParsedSession
	for i, f := range files {
		s, err := ParseSession(f)
		if err != nil {
			if progress != nil {
				progress(i+1, total)
			}
			continue // skip unparseable files
		}
		if len(s.Lines) == 0 {
			if progress != nil {
				progress(i+1, total)
			}
			continue
		}
		sessions = append(sessions, *s)
		if progress != nil {
			progress(i+1, total)
		}
	}
	return sessions, nil
}

// ParseSession parses a single Claude Code JSONL session file.
func ParseSession(path string) (*ParsedSession, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	session := &ParsedSession{
		SourcePath: path,
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	var lineIndex int
	var rawSessionID string

	for scanner.Scan() {
		var line rawLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			lineIndex++
			continue // skip malformed lines
		}

		// Extract session metadata from any line that has it
		if rawSessionID == "" && line.SessionID != "" {
			rawSessionID = line.SessionID
		}
		if session.Workspace == "" && line.Cwd != "" {
			session.Workspace = line.Cwd
		}
		if line.Message.Model != "" {
			session.Model = line.Message.Model
		}

		// Track subagent fields
		if line.IsSidechain {
			session.IsSubagent = true
		}
		if session.AgentID == "" && line.AgentID != "" {
			session.AgentID = line.AgentID
		}

		ts := parseTimestamp(line.Timestamp)

		var gitBranch string
		if line.GitBranch != nil {
			gitBranch = *line.GitBranch
		}

		parsed := ParsedLine{
			Type:             line.Type,
			Subtype:          line.Subtype,
			Timestamp:        ts,
			SessionID:        line.SessionID,
			Cwd:              line.Cwd,
			GitBranch:        gitBranch,
			IsSubagent:       line.IsSidechain,
			AgentID:          line.AgentID,
			SourceLineIndex:  lineIndex,
			DurationMs:       line.DurationMs,
			ToolUseResult:    parseToolUseResult(line.ToolUseResult),
			ToolUseResultRaw: line.ToolUseResult,
			Version:          line.Version,
			PermissionMode:   line.PermissionMode,
			AttributionSkill: line.AttributionSkill,
			Message: ParsedMessage{
				Role:       line.Message.Role,
				Content:    line.Message.Content,
				Model:      line.Message.Model,
				Usage:      line.Message.Usage,
				StopReason: line.Message.StopReason,
			},
		}

		session.Lines = append(session.Lines, parsed)
		lineIndex++
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}

	// Assign session ID: subagents use agentId, parents use raw sessionId
	if session.IsSubagent && session.AgentID != "" {
		session.ID = session.AgentID
		session.ParentSessionID = rawSessionID
	} else {
		session.ID = rawSessionID
	}

	// Load .meta.json for subagent files
	if session.IsSubagent {
		session.SubagentName = loadSubagentMeta(path)
	}

	return session, nil
}

// loadSubagentMeta reads the .meta.json sibling of a subagent JSONL file.
// Returns the agentType value, or empty string if not found.
func loadSubagentMeta(jsonlPath string) string {
	// agent-{agentId}.jsonl -> agent-{agentId}.meta.json
	base := strings.TrimSuffix(jsonlPath, ".jsonl")
	metaPath := base + ".meta.json"

	data, err := os.ReadFile(metaPath)
	if err != nil {
		return ""
	}

	var meta struct {
		AgentType string `json:"agentType"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	return meta.AgentType
}

// ParseContentBlocks parses raw JSON content which can be either a bare string
// or an array of ContentBlock. Returns:
//   - (text, nil) if content is a bare string
//   - ("", blocks) if content is an array of blocks
//   - ("", nil) if content is empty/invalid
func ParseContentBlocks(raw json.RawMessage) (string, []ContentBlock) {
	if len(raw) == 0 {
		return "", nil
	}

	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) == 0 {
		return "", nil
	}

	// Bare string
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", nil
		}
		return s, nil
	}

	// Array of content blocks
	if trimmed[0] == '[' {
		var blocks []ContentBlock
		if err := json.Unmarshal(raw, &blocks); err != nil {
			return "", nil
		}
		return "", blocks
	}

	return "", nil
}

// parseToolUseResult decodes the `toolUseResult` sibling envelope. The
// envelope is heterogeneous — it can be absent, a bare JSON string, or an
// object whose keys vary by tool (Bash, WebFetch, Glob, Task, Edit, …).
// We return a struct populated only with the cross-tool signal fields
// (`durationMs`, `interrupted`) and a `Present` flag to distinguish "no
// envelope" from "envelope but no durationMs".
func parseToolUseResult(raw json.RawMessage) ParsedToolUseResult {
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) == 0 || trimmed == "null" {
		return ParsedToolUseResult{}
	}
	// Object envelope: decode known fields. Bare strings and arrays fall
	// through with Present=false because they carry no per-tool-call timing.
	if trimmed[0] != '{' {
		return ParsedToolUseResult{}
	}
	var rt rawToolUseResult
	if err := json.Unmarshal(raw, &rt); err != nil {
		return ParsedToolUseResult{}
	}
	return ParsedToolUseResult{
		Present:     true,
		DurationMs:  rt.DurationMs,
		Interrupted: rt.Interrupted,
	}
}

func parseTimestamp(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func findJSONLFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // intentionally skip inaccessible directories
		}
		if !info.IsDir() && filepath.Ext(path) == ".jsonl" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
