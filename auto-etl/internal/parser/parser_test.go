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
