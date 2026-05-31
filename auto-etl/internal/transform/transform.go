package transform

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mistakenot/auto-etl/internal/model"
	"github.com/mistakenot/auto-etl/internal/parser"
)

var exitCodeRe = regexp.MustCompile(`(?m)^Exit code (\d+)`)

// Config holds transform-time settings.
type Config struct {
	HostID              string
	TruncateMaxChars    int
	TranscriptMaxChars  int
	GitRemoteForSession func(workspace string) string // resolver callback
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		TruncateMaxChars:    model.DefaultTruncateMaxChars,
		TranscriptMaxChars:  model.DefaultTranscriptMaxChars,
		GitRemoteForSession: func(string) string { return "" },
	}
}

// ProgressFunc is called during processing with (current, total) counts.
type ProgressFunc func(current, total int)

// sessionResult holds the output of transforming a single session.
type sessionResult struct {
	messages []model.AgentMessage
	session  model.AgentSession
}

// Transform converts parsed sessions into structured rows for parquet output.
// An optional progress callback is invoked as sessions are transformed.
// Sessions are processed in parallel across available CPUs.
func Transform(sessions []parser.ParsedSession, cfg Config, onProgress ...ProgressFunc) (*model.TransformedRows, error) {
	var progress ProgressFunc
	if len(onProgress) > 0 {
		progress = onProgress[0]
	}

	total := len(sessions)
	results := make([]sessionResult, total)

	// Worker pool
	workers := min(runtime.NumCPU(), total)

	var completed atomic.Int64
	work := make(chan int, total)
	var wg sync.WaitGroup

	for range workers {
		wg.Go(func() {
			for i := range work {
				msgs, session := transformSession(&sessions[i], cfg)
				results[i] = sessionResult{messages: msgs, session: session}
				n := int(completed.Add(1))
				if progress != nil {
					progress(n, total)
				}
			}
		})
	}

	for i := range sessions {
		work <- i
	}
	close(work)
	wg.Wait()

	// Collect results in original order
	result := &model.TransformedRows{}
	var skipped int
	for i := range results {
		if results[i].session.FirstMessageAt == 0 {
			skipped++
		} else {
			result.Messages = append(result.Messages, results[i].messages...)
			result.Sessions = append(result.Sessions, results[i].session)
		}
	}

	log.Printf("transform: %d sessions (%d skipped, no timestamps) -> %d messages (workers=%d)",
		len(sessions), skipped, len(result.Messages), workers)

	return result, nil
}

// buildToolUseIndex pre-scans all lines to build a map from tool_use ID to its metadata.
// This replaces the O(n) per-result scan with a single O(n) pass up front.
// Also records the tool_use line's timestamp so the tool_result branch can
// compute a duration as `result_ts - use_ts` when Claude's raw
// `toolUseResult.durationMs` is absent.
func buildToolUseIndex(lines []parser.ParsedLine) map[string]toolUseMeta {
	idx := make(map[string]toolUseMeta)
	for i := range lines {
		_, blocks := parser.ParseContentBlocks(lines[i].Message.Content)
		for j := range blocks {
			b := &blocks[j]
			if b.Type != "tool_use" || b.ID == "" {
				continue
			}
			m := toolUseMeta{Name: b.Name, StartedAtMs: toUnixMillis(lines[i].Timestamp)}
			var inputMap map[string]any
			if err := json.Unmarshal(b.Input, &inputMap); err == nil {
				if fp, ok := inputMap["file_path"].(string); ok {
					m.FilePath = fp
				}
				if b.Name == "Skill" {
					if skill, ok := inputMap["skill"].(string); ok {
						m.SkillName = skill
					}
				}
				if b.Name == "Bash" {
					if cmd, ok := inputMap["command"].(string); ok {
						m.BashCommand = cmd
					}
				}
				if b.Name == "Read" {
					if offset, ok := inputMap["offset"].(float64); ok {
						m.FileStartLine = int32(offset)
					}
					if limit, ok := inputMap["limit"].(float64); ok {
						m.FileNumLines = int32(limit)
					}
				}
			}
			idx[b.ID] = m
		}
	}
	return idx
}

func transformSession(raw *parser.ParsedSession, cfg Config) ([]model.AgentMessage, model.AgentSession) {
	var messages []model.AgentMessage

	// Resolve git remote for this session's workspace
	gitRemote := cfg.GitRemoteForSession(raw.Workspace)

	// Build tool_use index once for O(1) lookups from tool_result blocks
	toolUseIdx := buildToolUseIndex(raw.Lines)

	var (
		totalInput, totalOutput, totalTokens          int64
		totalBytes, totalInputBytes, totalOutputBytes int64
		totalTurnDurationMs                           int64
		msgIndex                                      int32
	)

	// Accumulate per-turn agent work time from `system / turn_duration` events.
	// Claude Code emits one of these at the end of every turn. The sum is real
	// wall-clock work time and complements the calendar span
	// `LastMessageAt - FirstMessageAt`, which is inflated by idle gaps.
	// Done as a separate pass because the main loop below only handles lines
	// that have `message` content blocks; turn_duration lines do not.
	for i := range raw.Lines {
		line := &raw.Lines[i]
		if line.Type == "system" && line.Subtype == "turn_duration" {
			totalTurnDurationMs += line.DurationMs
		}
	}

	for i := range raw.Lines {
		line := &raw.Lines[i]
		if line.Type != "user" && line.Type != "assistant" && line.Type != "system" {
			continue
		}

		ts := line.Timestamp
		tsMillis := toUnixMillis(ts)

		// Try parsing content blocks
		text, blocks := parser.ParseContentBlocks(line.Message.Content)

		// Accumulate token usage
		u := line.Message.Usage
		totalInput += int64(u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens)
		totalOutput += int64(u.OutputTokens)
		totalTokens += int64(u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens + u.OutputTokens)

		if blocks == nil {
			// Bare string content -> single message
			if text == "" {
				continue
			}
			msg := makeBaseMessage(raw, line, msgIndex, tsMillis, cfg.HostID, gitRemote)
			msg.Role = line.Message.Role
			msg.Content = text
			msg.ContentTruncated = MidTruncate(text, cfg.TruncateMaxChars)
			msg.InputTokens = int32(u.InputTokens)
			msg.CacheInputTokens = int32(u.CacheCreationInputTokens + u.CacheReadInputTokens)
			msg.OutputTokens = int32(u.OutputTokens)
			totalBytes += int64(len(text))
			if line.Type == "user" {
				totalInputBytes += int64(len(text))
			} else {
				totalOutputBytes += int64(len(text))
			}
			messages = append(messages, msg)
			msgIndex++
			continue
		}

		// Array of content blocks: one AgentMessage per block
		for j := range blocks {
			block := &blocks[j]
			msg := makeBaseMessage(raw, line, msgIndex, tsMillis, cfg.HostID, gitRemote)
			msg.InputTokens = int32(u.InputTokens)
			msg.CacheInputTokens = int32(u.CacheCreationInputTokens + u.CacheReadInputTokens)
			msg.OutputTokens = int32(u.OutputTokens)

			switch block.Type {
			case "text":
				msg.Role = line.Message.Role
				msg.Content = block.Text
				msg.ContentTruncated = MidTruncate(block.Text, cfg.TruncateMaxChars)
				totalBytes += int64(len(block.Text))
				if line.Type == "user" {
					totalInputBytes += int64(len(block.Text))
				} else {
					totalOutputBytes += int64(len(block.Text))
				}

			case "tool_use":
				msg.Role = string(model.RoleAssistant)
				msg.ToolName = block.Name
				// Persist the canonical pairing key on the originator row so
				// downstream queries can JOIN tool_use ↔ tool_result without
				// adjacency-based heuristics that break on parallel calls.
				msg.ToolUseID = block.ID
				if len(block.Input) > 0 {
					msg.ToolInput = string(block.Input)
				}

				// Parse tool input JSON for file path, bash command, and file metadata
				var inputMap map[string]any
				if err := json.Unmarshal(block.Input, &inputMap); err == nil {
					if fp, ok := inputMap["file_path"].(string); ok {
						msg.ToolFilePath = fp
					}
					if block.Name == "Bash" {
						if cmd, ok := inputMap["command"].(string); ok {
							msg.BashCommand = cmd
						}
					}
					// Extract Skill tool skill name
					if block.Name == "Skill" {
						if skill, ok := inputMap["skill"].(string); ok {
							msg.SkillName = skill
						}
					}
					// Extract Read tool file metadata
					if block.Name == "Read" {
						if offset, ok := inputMap["offset"].(float64); ok {
							msg.ToolFileStartLine = int32(offset)
						}
						if limit, ok := inputMap["limit"].(float64); ok {
							msg.ToolFileNumLines = int32(limit)
						}
					}
				}

				// Populate content_truncated for search indexing.
				// AskUserQuestion gets markdown rendering; other tools get raw JSON.
				var truncSrc string
				if block.Name == "AskUserQuestion" {
					truncSrc = renderAskUserQuestion(block.Input)
				}
				if truncSrc == "" && len(block.Input) > 0 {
					truncSrc = string(block.Input)
				}
				if truncSrc != "" {
					msg.ContentTruncated = MidTruncate(truncSrc, cfg.TruncateMaxChars)
				}

			case "tool_result":
				msg.Role = string(model.RoleTool)
				meta := toolUseIdx[block.ToolUseID]
				msg.ToolName = meta.Name
				msg.BashCommand = meta.BashCommand
				msg.ToolFilePath = meta.FilePath
				msg.ToolFileStartLine = meta.FileStartLine
				msg.ToolFileNumLines = meta.FileNumLines
				msg.SkillName = meta.SkillName
				// Persist canonical pairing key on the result row too so
				// queries can JOIN by id without needing per-line adjacency.
				msg.ToolUseID = block.ToolUseID
				// Per-call duration: prefer Claude's own measurement
				// (`toolUseResult.durationMs`) because it is wall-clock and
				// correct under interruption. Fall back to the ts-diff when
				// the envelope is absent or carried no durationMs (most
				// tools other than WebFetch / Glob do not emit one).
				switch {
				case line.ToolUseResult.Present && line.ToolUseResult.DurationMs > 0:
					msg.DurationMs = line.ToolUseResult.DurationMs
				case meta.StartedAtMs > 0 && tsMillis > meta.StartedAtMs:
					msg.DurationMs = tsMillis - meta.StartedAtMs
				}
				// Interrupted / cancelled signal. Only meaningful when
				// Claude emits the envelope; absent envelopes leave it false.
				if line.ToolUseResult.Present {
					msg.Interrupted = line.ToolUseResult.Interrupted
				}
				// tool_result content: store full unmodified content.
				// Content can be a plain string or an array of content blocks.
				if len(block.Content) > 0 {
					s := unmarshalToolResultContent(block.Content)
					if s != "" {
						msg.Content = s
						msg.ContentTruncated = MidTruncate(s, cfg.TruncateMaxChars)
					}
					if meta.Name == "Bash" {
						if m := exitCodeRe.FindStringSubmatch(s); m != nil {
							code, err := strconv.ParseInt(m[1], 10, 32)
							if err == nil {
								msg.BashExitCode = int32(code)
							}
						}
					}
				}

			default:
				continue
			}

			messages = append(messages, msg)
			msgIndex++
		}
	}

	// Build session row
	var firstAt, lastAt int64
	for i := range raw.Lines {
		ms := toUnixMillis(raw.Lines[i].Timestamp)
		if ms == 0 {
			continue
		}
		if firstAt == 0 || ms < firstAt {
			firstAt = ms
		}
		if ms > lastAt {
			lastAt = ms
		}
	}

	var year, month int32
	for i := range raw.Lines {
		if !raw.Lines[i].Timestamp.IsZero() {
			year = int32(raw.Lines[i].Timestamp.Year())
			month = int32(raw.Lines[i].Timestamp.Month())
			break
		}
	}

	// Build transcripts from messages
	transcriptFull, transcriptTruncated := buildTranscripts(messages, cfg.TranscriptMaxChars)

	session := model.AgentSession{
		ID:                  raw.ID,
		ParentSessionID:     raw.ParentSessionID,
		HostID:              cfg.HostID,
		Agent:               "claude",
		SubagentName:        raw.SubagentName,
		IsSubagent:          raw.IsSubagent,
		Workspace:           raw.Workspace,
		GitRemote:           gitRemote,
		Model:               raw.Model,
		SourcePath:          raw.SourcePath,
		FirstMessageAt:      firstAt,
		LastMessageAt:       lastAt,
		TotalTurnDurationMs: totalTurnDurationMs,
		TotalInputTokens:    totalInput,
		TotalOutputTokens:   totalOutput,
		TotalTokens:         totalTokens,
		TotalBytes:          totalBytes,
		TotalInputBytes:     totalInputBytes,
		TotalOutputBytes:    totalOutputBytes,
		TranscriptFull:      transcriptFull,
		TranscriptTruncated: transcriptTruncated,
		Year:                year,
		Month:               month,
		SchemaVersion:       int32(model.SchemaVersion),
	}

	return messages, session
}

func makeBaseMessage(session *parser.ParsedSession, line *parser.ParsedLine, index int32, tsMillis int64, hostID, gitRemote string) model.AgentMessage {
	var year, month int32
	var week int32
	if !line.Timestamp.IsZero() {
		year = int32(line.Timestamp.Year())
		month = int32(line.Timestamp.Month())
		_, w := line.Timestamp.ISOWeek()
		week = int32(w)
	}
	return model.AgentMessage{
		ID:              fmt.Sprintf("%s-%d", session.ID, index),
		SessionID:       session.ID,
		HostID:          hostID,
		Index:           index,
		Timestamp:       tsMillis,
		Workspace:       session.Workspace,
		GitRemote:       gitRemote,
		GitBranch:       line.GitBranch,
		Model:           session.Model,
		ParentSessionID: session.ParentSessionID,
		IsSubagent:      session.IsSubagent,
		SourceLineIndex: int32(line.SourceLineIndex),
		Year:            year,
		Week:            week,
		Month:           month,
		SchemaVersion:   int32(model.SchemaVersion),
	}
}

// toolUseMeta holds metadata extracted from a matching tool_use block.
// StartedAtMs is the tool_use line's Unix-ms timestamp, used as the
// fallback for per-tool-call duration computation when Claude does not
// emit `toolUseResult.durationMs`.
type toolUseMeta struct {
	Name          string
	BashCommand   string
	FilePath      string
	FileStartLine int32
	FileNumLines  int32
	SkillName     string
	StartedAtMs   int64
}

// unmarshalToolResultContent extracts text from tool_result content which can
// be either a plain JSON string or an array of content blocks (e.g. from Agent
// subagent results). For arrays, text blocks are concatenated with newlines.
func unmarshalToolResultContent(raw json.RawMessage) string {
	// Try plain string first (most common case).
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	// Try array of content blocks.
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	return ""
}

// buildTranscripts concatenates message contents into session-level transcripts.
func buildTranscripts(messages []model.AgentMessage, maxChars int) (full, truncated string) {
	var fullParts []string
	var truncParts []string

	for i := range messages {
		prefix := rolePrefix(messages[i].Role, messages[i].ToolName)

		if messages[i].Content != "" {
			fullParts = append(fullParts, prefix+messages[i].Content)
		}
		ct := messages[i].ContentTruncated
		if ct == "" {
			ct = messages[i].Content
		}
		if ct != "" {
			truncParts = append(truncParts, prefix+ct)
		}
	}

	full = strings.Join(fullParts, "\n\n")
	truncated = MidTruncate(strings.Join(truncParts, "\n\n"), maxChars)
	return full, truncated
}

// rolePrefix returns the transcript prefix for a message role.
func rolePrefix(role, toolName string) string {
	if role == string(model.RoleTool) && toolName != "" {
		return "[tool:" + toolName + "]:\n"
	}
	return "[" + role + "]:\n"
}

// MidTruncate truncates content symmetrically from the middle if it exceeds maxChars.
// Returns the original string if it fits within maxChars.
func MidTruncate(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}

	marker := fmt.Sprintf("\n…[truncated %d chars]…\n", len(s)-maxChars)

	// Account for marker length in the available space
	available := maxChars - len(marker)
	if available <= 0 {
		// Edge case: maxChars is very small
		return marker
	}

	half := available / 2
	return s[:half] + marker + s[len(s)-half:]
}

// renderAskUserQuestion attempts to render AskUserQuestion tool input as
// human-readable markdown. Returns empty string if the input doesn't match
// the expected shape (caller falls back to raw JSON).
func renderAskUserQuestion(raw json.RawMessage) string {
	var input struct {
		Question  string `json:"question"`
		Questions []struct {
			Question    string `json:"question"`
			Header      string `json:"header"`
			MultiSelect bool   `json:"multiSelect"`
			Options     []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return ""
	}

	// If there's a top-level question and no questions array, treat it as a single question
	if len(input.Questions) == 0 {
		if input.Question == "" {
			return ""
		}
		return "## Question\n\n" + input.Question
	}

	var sections []string
	for _, q := range input.Questions {
		var sb strings.Builder
		sb.WriteString("## Question\n")
		if q.Header != "" {
			sb.WriteString("\n**" + q.Header + "**\n")
		}
		if q.Question != "" {
			sb.WriteString("\n" + q.Question + "\n")
		}
		if len(q.Options) > 0 {
			sb.WriteString("\nOptions:\n")
			for _, opt := range q.Options {
				if opt.Description != "" {
					sb.WriteString("- **" + opt.Label + "**: " + opt.Description + "\n")
				} else {
					sb.WriteString("- **" + opt.Label + "**\n")
				}
			}
		}
		sections = append(sections, sb.String())
	}

	return strings.Join(sections, "\n---\n\n")
}

func toUnixMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
