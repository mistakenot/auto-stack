package parser

import (
	"path/filepath"
	"testing"
)

func TestParseSession_ParentBaseline(t *testing.T) {
	path := filepath.Join("testdata", "parent-session", "session.jsonl")
	s, err := ParseSession(path)
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}

	if s.ID != "aaaa1111-2222-3333-4444-555566667777" {
		t.Errorf("ID = %q, want parent UUID", s.ID)
	}
	if s.ParentSessionID != "" {
		t.Errorf("ParentSessionID = %q, want empty", s.ParentSessionID)
	}
	if s.IsSubagent {
		t.Error("IsSubagent = true, want false")
	}
	if s.SubagentName != "" {
		t.Errorf("SubagentName = %q, want empty", s.SubagentName)
	}
	if s.Workspace != "/home/user/project" {
		t.Errorf("Workspace = %q", s.Workspace)
	}
	if s.Model != "claude-opus-4-6" {
		t.Errorf("Model = %q", s.Model)
	}
	if len(s.Lines) != 3 {
		t.Fatalf("Lines count = %d, want 3", len(s.Lines))
	}
}

func TestParseSession_SubagentDetection(t *testing.T) {
	path := filepath.Join("testdata", "with-subagent", "subagents", "agent-abc123.jsonl")
	s, err := ParseSession(path)
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}

	if s.ID != "abc123" {
		t.Errorf("ID = %q, want agentId", s.ID)
	}
	if s.ParentSessionID != "bbbb1111-2222-3333-4444-555566667777" {
		t.Errorf("ParentSessionID = %q, want parent UUID", s.ParentSessionID)
	}
	if !s.IsSubagent {
		t.Error("IsSubagent = false, want true")
	}
	if s.AgentID != "abc123" {
		t.Errorf("AgentID = %q, want abc123", s.AgentID)
	}
}

func TestParseSession_SubagentMetaLoading(t *testing.T) {
	path := filepath.Join("testdata", "with-subagent", "subagents", "agent-abc123.jsonl")
	s, err := ParseSession(path)
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}

	if s.SubagentName != "Explore" {
		t.Errorf("SubagentName = %q, want Explore", s.SubagentName)
	}
}

func TestParseSession_SubagentNoMeta(t *testing.T) {
	path := filepath.Join("testdata", "subagent-no-meta", "subagents", "agent-def456.jsonl")
	s, err := ParseSession(path)
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}

	if !s.IsSubagent {
		t.Error("IsSubagent = false, want true")
	}
	if s.SubagentName != "" {
		t.Errorf("SubagentName = %q, want empty (no .meta.json)", s.SubagentName)
	}
	if s.ID != "def456" {
		t.Errorf("ID = %q, want agentId", s.ID)
	}
}

func TestParseSession_SourceLineIndex(t *testing.T) {
	path := filepath.Join("testdata", "parent-session", "session.jsonl")
	s, err := ParseSession(path)
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}

	for i, line := range s.Lines {
		if line.SourceLineIndex != i {
			t.Errorf("line %d: SourceLineIndex = %d, want %d", i, line.SourceLineIndex, i)
		}
	}
}

func TestParseSession_ParentNotSubagent(t *testing.T) {
	// Parent session file in the with-subagent dir should NOT be marked as subagent
	path := filepath.Join("testdata", "with-subagent", "session.jsonl")
	s, err := ParseSession(path)
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}

	if s.IsSubagent {
		t.Error("parent session.jsonl should not be IsSubagent")
	}
	if s.ID != "bbbb1111-2222-3333-4444-555566667777" {
		t.Errorf("ID = %q, want parent UUID", s.ID)
	}
}

func TestParseSession_ToolUseResultCaptured(t *testing.T) {
	// Claude Code attaches a `toolUseResult` sibling envelope to lines
	// carrying a `tool_result` content block. Three shapes show up in real
	// data: an object with `interrupted` (Bash); an object with `durationMs`
	// (WebFetch); a bare string (Read). The parser must surface
	// `interrupted=true` and `durationMs` from the first two shapes and
	// silently skip the bare-string case (Present=false).
	path := filepath.Join("testdata", "tool-use-result", "session.jsonl")
	s, err := ParseSession(path)
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}

	// Collect tool_result-bearing lines in order.
	var toolResults []ParsedLine
	for _, l := range s.Lines {
		if l.Type == "user" && containsToolResultBlock(l.Message.Content) {
			toolResults = append(toolResults, l)
		}
	}
	if len(toolResults) != 3 {
		t.Fatalf("tool_result lines = %d, want 3", len(toolResults))
	}

	// Line 0: Bash with interrupted=true, no durationMs.
	if !toolResults[0].ToolUseResult.Present {
		t.Error("toolResults[0].ToolUseResult.Present = false, want true")
	}
	if !toolResults[0].ToolUseResult.Interrupted {
		t.Error("toolResults[0].ToolUseResult.Interrupted = false, want true")
	}
	if toolResults[0].ToolUseResult.DurationMs != 0 {
		t.Errorf("toolResults[0].ToolUseResult.DurationMs = %d, want 0",
			toolResults[0].ToolUseResult.DurationMs)
	}

	// Line 1: WebFetch with durationMs=850, no interrupted.
	if !toolResults[1].ToolUseResult.Present {
		t.Error("toolResults[1].ToolUseResult.Present = false, want true")
	}
	if toolResults[1].ToolUseResult.DurationMs != 850 {
		t.Errorf("toolResults[1].ToolUseResult.DurationMs = %d, want 850",
			toolResults[1].ToolUseResult.DurationMs)
	}
	if toolResults[1].ToolUseResult.Interrupted {
		t.Error("toolResults[1].ToolUseResult.Interrupted = true, want false")
	}

	// Line 2: Read with bare-string envelope. We don't decode strings,
	// so Present must be false (and the result row falls back to ts-diff
	// in the transform stage).
	if toolResults[2].ToolUseResult.Present {
		t.Error("toolResults[2].ToolUseResult.Present = true, want false (bare-string envelope)")
	}
}

// containsToolResultBlock is a small helper for the tool_use_result test.
// Returns true if the content array decodes to at least one tool_result
// block. Avoids re-implementing ContentBlock parsing in the test.
func containsToolResultBlock(raw []byte) bool {
	_, blocks := ParseContentBlocks(raw)
	for i := range blocks {
		if blocks[i].Type == "tool_result" {
			return true
		}
	}
	return false
}

func TestParseSession_TurnDurationCaptured(t *testing.T) {
	// Claude Code emits one `system / subtype=turn_duration` line at the end of
	// every turn with a `durationMs` field. The parser must surface both
	// `Subtype` and `DurationMs` on the corresponding ParsedLine so the
	// transform stage can accumulate them into AgentSession.TotalTurnDurationMs.
	path := filepath.Join("testdata", "turn-duration", "session.jsonl")
	s, err := ParseSession(path)
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}

	var got []ParsedLine
	for _, l := range s.Lines {
		if l.Type == "system" && l.Subtype == "turn_duration" {
			got = append(got, l)
		}
	}

	if len(got) != 2 {
		t.Fatalf("turn_duration lines = %d, want 2", len(got))
	}
	if got[0].DurationMs != 12345 {
		t.Errorf("got[0].DurationMs = %d, want 12345", got[0].DurationMs)
	}
	if got[1].DurationMs != 58601 {
		t.Errorf("got[1].DurationMs = %d, want 58601", got[1].DurationMs)
	}
}
