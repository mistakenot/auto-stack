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
	ToolUseResult   json.RawMessage // raw toolUseResult envelope (sibling of message)
}

// ParsedMessage holds the message payload from a JSONL line.
type ParsedMessage struct {
	Role    string
	Content json.RawMessage
	Model   string
	Usage   ParsedUsage
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
}

// rawLine is the JSON structure of a single JSONL line from Claude session files.
type rawLine struct {
	Type          string          `json:"type"`
	Subtype       string          `json:"subtype"` // for `system` lines: e.g. "turn_duration", "init"
	SessionID     string          `json:"sessionId"`
	Cwd           string          `json:"cwd"`
	GitBranch     *string         `json:"gitBranch"` // pointer to distinguish null from missing
	Timestamp     string          `json:"timestamp"`
	IsSidechain   bool            `json:"isSidechain"`
	AgentID       string          `json:"agentId"`
	DurationMs    int64           `json:"durationMs"` // populated on `system / turn_duration` lines
	Message       rawMessage      `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Model   string          `json:"model"`
	Usage   ParsedUsage     `json:"usage"`
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
			Type:            line.Type,
			Subtype:         line.Subtype,
			Timestamp:       ts,
			SessionID:       line.SessionID,
			Cwd:             line.Cwd,
			GitBranch:       gitBranch,
			IsSubagent:      line.IsSidechain,
			AgentID:         line.AgentID,
			SourceLineIndex: lineIndex,
			DurationMs:      line.DurationMs,
			Message: ParsedMessage{
				Role:    line.Message.Role,
				Content: line.Message.Content,
				Model:   line.Message.Model,
				Usage:   line.Message.Usage,
			},
			ToolUseResult: line.ToolUseResult,
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
