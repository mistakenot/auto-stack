package transform

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mistakenot/auto-etl/internal/model"
	"github.com/mistakenot/auto-etl/internal/parser"
)

func testConfig() Config {
	cfg := DefaultConfig()
	cfg.HostID = "test-host"
	cfg.GitRemoteForSession = func(ws string) string {
		if ws == "/home/user/project" {
			return "git@github.com:user/project.git"
		}
		return ""
	}
	return cfg
}

func makeSubagentSession() parser.ParsedSession {
	return parser.ParsedSession{
		ID:              "abc123",
		ParentSessionID: "parent-uuid-1234",
		AgentID:         "abc123",
		SubagentName:    "Explore",
		IsSubagent:      true,
		Workspace:       "/home/user/project",
		Model:           "claude-opus-4-6",
		SourcePath:      "/tmp/test/subagents/agent-abc123.jsonl",
		Lines: []parser.ParsedLine{
			{
				Type:            "user",
				Timestamp:       time.Date(2026, 3, 10, 11, 0, 0, 0, time.UTC),
				SessionID:       "parent-uuid-1234",
				Cwd:             "/home/user/project",
				GitBranch:       "main",
				IsSubagent:      true,
				AgentID:         "abc123",
				SourceLineIndex: 0,
				Message: parser.ParsedMessage{
					Role:    "user",
					Content: json.RawMessage(`"explore repo"`),
				},
			},
			{
				Type:            "assistant",
				Timestamp:       time.Date(2026, 3, 10, 11, 0, 5, 0, time.UTC),
				SessionID:       "parent-uuid-1234",
				Cwd:             "/home/user/project",
				GitBranch:       "main",
				IsSubagent:      true,
				AgentID:         "abc123",
				SourceLineIndex: 1,
				Message: parser.ParsedMessage{
					Role:    "assistant",
					Content: json.RawMessage(`"Found 3 files."`),
					Model:   "claude-opus-4-6",
					Usage:   parser.ParsedUsage{InputTokens: 15, OutputTokens: 8},
				},
			},
		},
	}
}

func makeParentSession() parser.ParsedSession {
	return parser.ParsedSession{
		ID:         "parent-uuid-1234",
		Workspace:  "/home/user/project",
		Model:      "claude-opus-4-6",
		SourcePath: "/tmp/test/session.jsonl",
		Lines: []parser.ParsedLine{
			{
				Type:            "user",
				Timestamp:       time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC),
				SessionID:       "parent-uuid-1234",
				Cwd:             "/home/user/project",
				GitBranch:       "feature-branch",
				SourceLineIndex: 0,
				Message: parser.ParsedMessage{
					Role:    "user",
					Content: json.RawMessage(`"hello"`),
				},
			},
		},
	}
}

func TestTransformSession_SubagentFieldPropagation(t *testing.T) {
	raw := makeSubagentSession()
	msgs, session := transformSession(&raw, testConfig())

	if session.ParentSessionID != "parent-uuid-1234" {
		t.Errorf("session.ParentSessionID = %q, want parent-uuid-1234", session.ParentSessionID)
	}
	if !session.IsSubagent {
		t.Error("session.IsSubagent = false, want true")
	}
	if session.SubagentName != "Explore" {
		t.Errorf("session.SubagentName = %q, want Explore", session.SubagentName)
	}

	for i, msg := range msgs {
		if msg.ParentSessionID != "parent-uuid-1234" {
			t.Errorf("msg[%d].ParentSessionID = %q, want parent-uuid-1234", i, msg.ParentSessionID)
		}
		if !msg.IsSubagent {
			t.Errorf("msg[%d].IsSubagent = false, want true", i)
		}
	}
}

func TestTransformSession_MessageIDsUseSubagentID(t *testing.T) {
	raw := makeSubagentSession()
	msgs, session := transformSession(&raw, testConfig())

	if session.ID != "abc123" {
		t.Errorf("session.ID = %q, want abc123", session.ID)
	}

	for i, msg := range msgs {
		wantPrefix := "abc123-"
		if msg.SessionID != "abc123" {
			t.Errorf("msg[%d].SessionID = %q, want abc123", i, msg.SessionID)
		}
		if len(msg.ID) < len(wantPrefix) || msg.ID[:len(wantPrefix)] != wantPrefix {
			t.Errorf("msg[%d].ID = %q, want prefix %q", i, msg.ID, wantPrefix)
		}
	}
}

func TestTransformSession_ParentHasNoSubagentFields(t *testing.T) {
	raw := makeParentSession()
	msgs, session := transformSession(&raw, testConfig())

	if session.ParentSessionID != "" {
		t.Errorf("session.ParentSessionID = %q, want empty", session.ParentSessionID)
	}
	if session.IsSubagent {
		t.Error("session.IsSubagent = true, want false")
	}
	if session.SubagentName != "" {
		t.Errorf("session.SubagentName = %q, want empty", session.SubagentName)
	}

	for i, msg := range msgs {
		if msg.ParentSessionID != "" {
			t.Errorf("msg[%d].ParentSessionID = %q, want empty", i, msg.ParentSessionID)
		}
		if msg.IsSubagent {
			t.Errorf("msg[%d].IsSubagent = true, want false", i)
		}
	}
}

func TestTransformSession_SourceLineIndex(t *testing.T) {
	raw := makeSubagentSession()
	msgs, _ := transformSession(&raw, testConfig())

	for i, msg := range msgs {
		if msg.SourceLineIndex != int32(i) {
			t.Errorf("msg[%d].SourceLineIndex = %d, want %d", i, msg.SourceLineIndex, i)
		}
	}
}

// --- Phase 1: Truncation tests ---

func TestMidTruncate_BelowThreshold(t *testing.T) {
	input := "short string"
	got := MidTruncate(input, 100)
	if got != input {
		t.Errorf("MidTruncate(%q, 100) = %q, want %q", input, got, input)
	}
}

func TestMidTruncate_ExactlyAtThreshold(t *testing.T) {
	input := strings.Repeat("x", 100)
	got := MidTruncate(input, 100)
	if got != input {
		t.Errorf("len %d, want len %d (should be unchanged)", len(got), len(input))
	}
}

func TestMidTruncate_AboveThreshold(t *testing.T) {
	input := strings.Repeat("A", 500) + strings.Repeat("B", 500)
	got := MidTruncate(input, 100)

	if !strings.Contains(got, "…[truncated") {
		t.Error("expected truncation marker")
	}
	if !strings.HasPrefix(got, "AAAA") {
		t.Error("expected start of original content")
	}
	if !strings.HasSuffix(got, "BBBB") {
		t.Error("expected end of original content")
	}
	// Result should be approximately maxChars (marker overhead is accounted for)
	if len(got) > 120 {
		t.Errorf("truncated length %d exceeds expected range", len(got))
	}
}

func TestMidTruncate_Empty(t *testing.T) {
	got := MidTruncate("", 100)
	if got != "" {
		t.Errorf("MidTruncate(\"\", 100) = %q, want empty", got)
	}
}

func TestContentTruncated_SmallContent(t *testing.T) {
	raw := makeParentSession()
	msgs, _ := transformSession(&raw, testConfig())

	for _, msg := range msgs {
		if msg.Content != msg.ContentTruncated {
			t.Errorf("small content: ContentTruncated %q != Content %q", msg.ContentTruncated, msg.Content)
		}
	}
}

// --- Phase 2: Git metadata tests ---

func TestTransformSession_GitBranch(t *testing.T) {
	raw := makeParentSession()
	msgs, _ := transformSession(&raw, testConfig())

	if len(msgs) == 0 {
		t.Fatal("expected at least one message")
	}
	if msgs[0].GitBranch != "feature-branch" {
		t.Errorf("msg.GitBranch = %q, want feature-branch", msgs[0].GitBranch)
	}
}

func TestTransformSession_GitBranchEmpty(t *testing.T) {
	raw := makeParentSession()
	raw.Lines[0].GitBranch = "" // simulate null/missing
	msgs, _ := transformSession(&raw, testConfig())

	if len(msgs) == 0 {
		t.Fatal("expected at least one message")
	}
	if msgs[0].GitBranch != "" {
		t.Errorf("msg.GitBranch = %q, want empty", msgs[0].GitBranch)
	}
}

func TestTransformSession_GitRemote(t *testing.T) {
	raw := makeParentSession()
	msgs, session := transformSession(&raw, testConfig())

	want := "git@github.com:user/project.git"
	if session.GitRemote != want {
		t.Errorf("session.GitRemote = %q, want %q", session.GitRemote, want)
	}
	for i, msg := range msgs {
		if msg.GitRemote != want {
			t.Errorf("msg[%d].GitRemote = %q, want %q", i, msg.GitRemote, want)
		}
	}
}

// --- Phase 3: Transcript tests ---

func TestTransformSession_TranscriptsPopulated(t *testing.T) {
	raw := makeSubagentSession()
	_, session := transformSession(&raw, testConfig())

	if session.TranscriptFull == "" {
		t.Error("TranscriptFull is empty")
	}
	if session.TranscriptTruncated == "" {
		t.Error("TranscriptTruncated is empty")
	}
}

func TestTransformSession_TranscriptFormat(t *testing.T) {
	raw := makeSubagentSession()
	_, session := transformSession(&raw, testConfig())

	if !strings.Contains(session.TranscriptFull, "[user]:") {
		t.Error("TranscriptFull missing [user]: prefix")
	}
	if !strings.Contains(session.TranscriptFull, "[assistant]:") {
		t.Error("TranscriptFull missing [assistant]: prefix")
	}
}

func TestTransformSession_TranscriptToolName(t *testing.T) {
	raw := makeParentSession()
	raw.Lines = append(raw.Lines, parser.ParsedLine{
		Type:            "assistant",
		Timestamp:       time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC),
		SessionID:       "parent-uuid-1234",
		Cwd:             "/home/user/project",
		SourceLineIndex: 1,
		Message: parser.ParsedMessage{
			Role: "assistant",
			Content: json.RawMessage(`[
				{"type":"tool_use","id":"tu1","name":"Read","input":{"file_path":"/tmp/f.txt"}},
				{"type":"tool_result","tool_use_id":"tu1","content":"\"file content here\""}
			]`),
			Model: "claude-opus-4-6",
		},
	})

	_, session := transformSession(&raw, testConfig())

	if !strings.Contains(session.TranscriptFull, "[tool:Read]:") {
		t.Errorf("TranscriptFull missing [tool:Read]: prefix. Got:\n%s", session.TranscriptFull)
	}
}

func TestTransformSession_TranscriptTruncatedCap(t *testing.T) {
	// Build a session with lots of content
	raw := makeParentSession()
	bigContent := strings.Repeat("x", 100000)
	for i := range 10 {
		raw.Lines = append(raw.Lines, parser.ParsedLine{
			Type:            "assistant",
			Timestamp:       time.Date(2026, 3, 10, 10, 0, i+1, 0, time.UTC),
			SessionID:       "parent-uuid-1234",
			Cwd:             "/home/user/project",
			SourceLineIndex: i + 1,
			Message: parser.ParsedMessage{
				Role:    "assistant",
				Content: json.RawMessage(`"` + bigContent + `"`),
				Model:   "claude-opus-4-6",
			},
		})
	}

	cfg := testConfig()
	cfg.TranscriptMaxChars = 1000 // small cap for testing
	_, session := transformSession(&raw, cfg)

	if len(session.TranscriptTruncated) > 1100 {
		t.Errorf("TranscriptTruncated len %d exceeds cap", len(session.TranscriptTruncated))
	}
	if !strings.Contains(session.TranscriptTruncated, "…[truncated") {
		t.Error("TranscriptTruncated should contain truncation marker")
	}
}

// --- Phase 4: Host ID + Read tool metadata ---

func TestTransformSession_HostID(t *testing.T) {
	raw := makeParentSession()
	msgs, session := transformSession(&raw, testConfig())

	if session.HostID != "test-host" {
		t.Errorf("session.HostID = %q, want test-host", session.HostID)
	}
	for i, msg := range msgs {
		if msg.HostID != "test-host" {
			t.Errorf("msg[%d].HostID = %q, want test-host", i, msg.HostID)
		}
	}
}

func TestTransformSession_ReadToolFileMetadata(t *testing.T) {
	raw := makeParentSession()
	raw.Lines = append(raw.Lines, parser.ParsedLine{
		Type:            "assistant",
		Timestamp:       time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC),
		SessionID:       "parent-uuid-1234",
		Cwd:             "/home/user/project",
		SourceLineIndex: 1,
		Message: parser.ParsedMessage{
			Role:    "assistant",
			Content: json.RawMessage(`[{"type":"tool_use","id":"tu1","name":"Read","input":{"file_path":"/tmp/test.txt","offset":10,"limit":50}}]`),
			Model:   "claude-opus-4-6",
		},
	})

	msgs, _ := transformSession(&raw, testConfig())

	// Find the Read tool message
	var found bool
	for _, msg := range msgs {
		if msg.ToolName == "Read" {
			found = true
			if msg.ToolFileStartLine != 10 {
				t.Errorf("ToolFileStartLine = %d, want 10", msg.ToolFileStartLine)
			}
			if msg.ToolFileNumLines != 50 {
				t.Errorf("ToolFileNumLines = %d, want 50", msg.ToolFileNumLines)
			}
		}
	}
	if !found {
		t.Error("no Read tool message found")
	}
}

// --- Phase 5: tool_use content_truncated + AskUserQuestion rendering ---

func TestRenderAskUserQuestion_Full(t *testing.T) {
	input := json.RawMessage(`{
		"question": "top level",
		"questions": [{
			"question": "Which option?",
			"header": "Configuration",
			"multiSelect": false,
			"options": [
				{"label": "Option A", "description": "First option"},
				{"label": "Option B", "description": "Second option"}
			]
		}]
	}`)
	got := renderAskUserQuestion(input)
	if !strings.Contains(got, "## Question") {
		t.Error("missing ## Question header")
	}
	if !strings.Contains(got, "**Configuration**") {
		t.Error("missing header")
	}
	if !strings.Contains(got, "Which option?") {
		t.Error("missing question text")
	}
	if !strings.Contains(got, "- **Option A**: First option") {
		t.Error("missing Option A")
	}
	if !strings.Contains(got, "- **Option B**: Second option") {
		t.Error("missing Option B")
	}
}

func TestRenderAskUserQuestion_Minimal(t *testing.T) {
	input := json.RawMessage(`{"question": "What should I do?"}`)
	got := renderAskUserQuestion(input)
	if got != "## Question\n\nWhat should I do?" {
		t.Errorf("got %q", got)
	}
}

func TestRenderAskUserQuestion_MultipleQuestions(t *testing.T) {
	input := json.RawMessage(`{
		"questions": [
			{"question": "First?"},
			{"question": "Second?"}
		]
	}`)
	got := renderAskUserQuestion(input)
	if !strings.Contains(got, "First?") || !strings.Contains(got, "Second?") {
		t.Errorf("missing questions in output: %q", got)
	}
	if !strings.Contains(got, "---") {
		t.Error("missing separator between questions")
	}
}

func TestRenderAskUserQuestion_PartialData(t *testing.T) {
	// No header, no options — just question text
	input := json.RawMessage(`{"questions": [{"question": "Simple?"}]}`)
	got := renderAskUserQuestion(input)
	if !strings.Contains(got, "Simple?") {
		t.Errorf("missing question text: %q", got)
	}
	if strings.Contains(got, "Options:") {
		t.Error("should not contain Options section")
	}
}

func TestRenderAskUserQuestion_OptionsNoDescription(t *testing.T) {
	input := json.RawMessage(`{"questions": [{"question": "Pick one", "options": [{"label": "Yes"}, {"label": "No"}]}]}`)
	got := renderAskUserQuestion(input)
	if !strings.Contains(got, "- **Yes**") {
		t.Errorf("missing Yes option: %q", got)
	}
}

func TestRenderAskUserQuestion_BadJSON(t *testing.T) {
	got := renderAskUserQuestion(json.RawMessage(`not json`))
	if got != "" {
		t.Errorf("expected empty string for bad JSON, got %q", got)
	}
}

func TestRenderAskUserQuestion_EmptyQuestions(t *testing.T) {
	got := renderAskUserQuestion(json.RawMessage(`{"questions": []}`))
	if got != "" {
		t.Errorf("expected empty string for empty questions, got %q", got)
	}
}

func TestRenderAskUserQuestion_UnexpectedShape(t *testing.T) {
	// Valid JSON but no question/questions fields
	got := renderAskUserQuestion(json.RawMessage(`{"foo": "bar"}`))
	if got != "" {
		t.Errorf("expected empty string for unexpected shape, got %q", got)
	}
}

func TestToolUseContentTruncated_AskUserQuestion(t *testing.T) {
	raw := makeParentSession()
	raw.Lines = append(raw.Lines, parser.ParsedLine{
		Type:            "assistant",
		Timestamp:       time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC),
		SessionID:       "parent-uuid-1234",
		Cwd:             "/home/user/project",
		SourceLineIndex: 1,
		Message: parser.ParsedMessage{
			Role:    "assistant",
			Content: json.RawMessage(`[{"type":"tool_use","id":"tu1","name":"AskUserQuestion","input":{"question":"What should I do next?"}}]`),
			Model:   "claude-opus-4-6",
		},
	})

	msgs, _ := transformSession(&raw, testConfig())
	var found bool
	for _, msg := range msgs {
		if msg.ToolName == "AskUserQuestion" {
			found = true
			if !strings.Contains(msg.ContentTruncated, "## Question") {
				t.Errorf("expected markdown rendering, got %q", msg.ContentTruncated)
			}
			if !strings.Contains(msg.ContentTruncated, "What should I do next?") {
				t.Errorf("expected question text, got %q", msg.ContentTruncated)
			}
			if msg.Content != "" {
				t.Errorf("Content should remain empty for tool_use, got %q", msg.Content)
			}
		}
	}
	if !found {
		t.Error("no AskUserQuestion tool message found")
	}
}

func TestToolUseContentTruncated_GenericTool(t *testing.T) {
	raw := makeParentSession()
	raw.Lines = append(raw.Lines, parser.ParsedLine{
		Type:            "assistant",
		Timestamp:       time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC),
		SessionID:       "parent-uuid-1234",
		Cwd:             "/home/user/project",
		SourceLineIndex: 1,
		Message: parser.ParsedMessage{
			Role:    "assistant",
			Content: json.RawMessage(`[{"type":"tool_use","id":"tu1","name":"Read","input":{"file_path":"/tmp/test.txt"}}]`),
			Model:   "claude-opus-4-6",
		},
	})

	msgs, _ := transformSession(&raw, testConfig())
	var found bool
	for _, msg := range msgs {
		if msg.ToolName == "Read" {
			found = true
			if !strings.Contains(msg.ContentTruncated, "file_path") {
				t.Errorf("expected raw JSON in content_truncated, got %q", msg.ContentTruncated)
			}
			if msg.Content != "" {
				t.Errorf("Content should remain empty for tool_use, got %q", msg.Content)
			}
		}
	}
	if !found {
		t.Error("no Read tool message found")
	}
}

func TestToolUseContentTruncated_LargeInput(t *testing.T) {
	bigValue := strings.Repeat("x", 8000)
	inputJSON := `{"command":"` + bigValue + `"}`
	raw := makeParentSession()
	raw.Lines = append(raw.Lines, parser.ParsedLine{
		Type:            "assistant",
		Timestamp:       time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC),
		SessionID:       "parent-uuid-1234",
		Cwd:             "/home/user/project",
		SourceLineIndex: 1,
		Message: parser.ParsedMessage{
			Role:    "assistant",
			Content: json.RawMessage(`[{"type":"tool_use","id":"tu1","name":"Bash","input":` + inputJSON + `}]`),
			Model:   "claude-opus-4-6",
		},
	})

	cfg := testConfig()
	msgs, _ := transformSession(&raw, cfg)
	var found bool
	for _, msg := range msgs {
		if msg.ToolName == "Bash" {
			found = true
			if !strings.Contains(msg.ContentTruncated, "…[truncated") {
				t.Errorf("expected truncation marker, got len %d", len(msg.ContentTruncated))
			}
		}
	}
	if !found {
		t.Error("no Bash tool message found")
	}
}

// --- Phase 6: Skill name extraction ---

func TestTransformSession_SkillName_ToolUse(t *testing.T) {
	raw := makeParentSession()
	raw.Lines = append(raw.Lines, parser.ParsedLine{
		Type:            "assistant",
		Timestamp:       time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC),
		SessionID:       "parent-uuid-1234",
		Cwd:             "/home/user/project",
		SourceLineIndex: 1,
		Message: parser.ParsedMessage{
			Role:    "assistant",
			Content: json.RawMessage(`[{"type":"tool_use","id":"tu1","name":"Skill","input":{"skill":"contextual-commit"}}]`),
			Model:   "claude-opus-4-6",
		},
	})

	msgs, _ := transformSession(&raw, testConfig())
	var found bool
	for _, msg := range msgs {
		if msg.ToolName == "Skill" && msg.Role == "assistant" {
			found = true
			if msg.SkillName != "contextual-commit" {
				t.Errorf("SkillName = %q, want contextual-commit", msg.SkillName)
			}
		}
	}
	if !found {
		t.Error("no Skill tool_use message found")
	}
}

func TestTransformSession_SkillName_ToolResult(t *testing.T) {
	raw := makeParentSession()
	raw.Lines = append(raw.Lines, parser.ParsedLine{
		Type:            "assistant",
		Timestamp:       time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC),
		SessionID:       "parent-uuid-1234",
		Cwd:             "/home/user/project",
		SourceLineIndex: 1,
		Message: parser.ParsedMessage{
			Role: "assistant",
			Content: json.RawMessage(`[
				{"type":"tool_use","id":"tu1","name":"Skill","input":{"skill":"langchain-docs","args":"how does X work?"}},
				{"type":"tool_result","tool_use_id":"tu1","content":"\"Launching skill: langchain-docs\""}
			]`),
			Model: "claude-opus-4-6",
		},
	})

	msgs, _ := transformSession(&raw, testConfig())
	var toolUseFound, toolResultFound bool
	for _, msg := range msgs {
		if msg.ToolName == "Skill" && msg.Role == "assistant" {
			toolUseFound = true
			if msg.SkillName != "langchain-docs" {
				t.Errorf("tool_use SkillName = %q, want langchain-docs", msg.SkillName)
			}
		}
		if msg.ToolName == "Skill" && msg.Role == "tool" {
			toolResultFound = true
			if msg.SkillName != "langchain-docs" {
				t.Errorf("tool_result SkillName = %q, want langchain-docs", msg.SkillName)
			}
		}
	}
	if !toolUseFound {
		t.Error("no Skill tool_use message found")
	}
	if !toolResultFound {
		t.Error("no Skill tool_result message found")
	}
}

func TestTransformSession_SkillName_WithArgs(t *testing.T) {
	raw := makeParentSession()
	raw.Lines = append(raw.Lines, parser.ParsedLine{
		Type:            "assistant",
		Timestamp:       time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC),
		SessionID:       "parent-uuid-1234",
		Cwd:             "/home/user/project",
		SourceLineIndex: 1,
		Message: parser.ParsedMessage{
			Role:    "assistant",
			Content: json.RawMessage(`[{"type":"tool_use","id":"tu1","name":"Skill","input":{"skill":"create-solution","args":"docs/requirements.md"}}]`),
			Model:   "claude-opus-4-6",
		},
	})

	msgs, _ := transformSession(&raw, testConfig())
	for _, msg := range msgs {
		if msg.ToolName == "Skill" {
			if msg.SkillName != "create-solution" {
				t.Errorf("SkillName = %q, want create-solution", msg.SkillName)
			}
		}
	}
}

func TestTransformSession_SkillName_EmptyForNonSkillTools(t *testing.T) {
	raw := makeParentSession()
	raw.Lines = append(raw.Lines, parser.ParsedLine{
		Type:            "assistant",
		Timestamp:       time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC),
		SessionID:       "parent-uuid-1234",
		Cwd:             "/home/user/project",
		SourceLineIndex: 1,
		Message: parser.ParsedMessage{
			Role:    "assistant",
			Content: json.RawMessage(`[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls"}}]`),
			Model:   "claude-opus-4-6",
		},
	})

	msgs, _ := transformSession(&raw, testConfig())
	for _, msg := range msgs {
		if msg.ToolName == "Bash" && msg.SkillName != "" {
			t.Errorf("non-Skill tool should have empty SkillName, got %q", msg.SkillName)
		}
	}
}

func TestTransformSession_SkillName_MissingSkillField(t *testing.T) {
	raw := makeParentSession()
	raw.Lines = append(raw.Lines, parser.ParsedLine{
		Type:            "assistant",
		Timestamp:       time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC),
		SessionID:       "parent-uuid-1234",
		Cwd:             "/home/user/project",
		SourceLineIndex: 1,
		Message: parser.ParsedMessage{
			Role:    "assistant",
			Content: json.RawMessage(`[{"type":"tool_use","id":"tu1","name":"Skill","input":{"args":"something"}}]`),
			Model:   "claude-opus-4-6",
		},
	})

	msgs, _ := transformSession(&raw, testConfig())
	for _, msg := range msgs {
		if msg.ToolName == "Skill" && msg.SkillName != "" {
			t.Errorf("Skill with missing skill field should have empty SkillName, got %q", msg.SkillName)
		}
	}
}

func TestToolUseContentTruncated_EmptyInput(t *testing.T) {
	raw := makeParentSession()
	raw.Lines = append(raw.Lines, parser.ParsedLine{
		Type:            "assistant",
		Timestamp:       time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC),
		SessionID:       "parent-uuid-1234",
		Cwd:             "/home/user/project",
		SourceLineIndex: 1,
		Message: parser.ParsedMessage{
			Role:    "assistant",
			Content: json.RawMessage(`[{"type":"tool_use","id":"tu1","name":"Read","input":{}}]`),
			Model:   "claude-opus-4-6",
		},
	})

	msgs, _ := transformSession(&raw, testConfig())
	for _, msg := range msgs {
		if msg.ToolName == "Read" {
			// Empty object "{}" is still valid JSON — should be stored
			if msg.ContentTruncated != "{}" {
				t.Errorf("expected '{}' for empty input, got %q", msg.ContentTruncated)
			}
		}
	}
}

// --- turn_duration accumulation ---

func TestTransformSession_TotalTurnDurationMsAccumulates(t *testing.T) {
	// Claude Code emits one `system / subtype=turn_duration` line at the end
	// of every turn with a `durationMs` field measuring per-turn agent work
	// time. transformSession must sum these into the returned
	// AgentSession.TotalTurnDurationMs.
	raw := makeParentSession()
	raw.Lines = append(raw.Lines,
		parser.ParsedLine{
			Type:       "system",
			Subtype:    "turn_duration",
			Timestamp:  time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC),
			DurationMs: 12000,
		},
		parser.ParsedLine{
			Type:       "system",
			Subtype:    "turn_duration",
			Timestamp:  time.Date(2026, 3, 10, 10, 0, 10, 0, time.UTC),
			DurationMs: 18000,
		},
		parser.ParsedLine{
			Type:       "system",
			Subtype:    "turn_duration",
			Timestamp:  time.Date(2026, 3, 10, 10, 0, 15, 0, time.UTC),
			DurationMs: 20000,
		},
	)

	_, session := transformSession(&raw, testConfig())

	if got, want := session.TotalTurnDurationMs, int64(50000); got != want {
		t.Errorf("TotalTurnDurationMs = %d, want %d", got, want)
	}
}

func TestTransformSession_TotalTurnDurationMsIgnoresOtherSystemSubtypes(t *testing.T) {
	// Non-turn_duration system events (e.g. `system / subtype=init`) must not
	// contribute to TotalTurnDurationMs even if a `DurationMs` field is set.
	raw := makeParentSession()
	raw.Lines = append(raw.Lines,
		parser.ParsedLine{
			Type:       "system",
			Subtype:    "init",
			Timestamp:  time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC),
			DurationMs: 999999,
		},
		parser.ParsedLine{
			Type:       "system",
			Subtype:    "turn_duration",
			Timestamp:  time.Date(2026, 3, 10, 10, 0, 10, 0, time.UTC),
			DurationMs: 7000,
		},
	)

	_, session := transformSession(&raw, testConfig())

	if got, want := session.TotalTurnDurationMs, int64(7000); got != want {
		t.Errorf("TotalTurnDurationMs = %d, want %d (init subtype must be ignored)", got, want)
	}
}

func TestTransformSession_TotalTurnDurationMsZeroWhenAbsent(t *testing.T) {
	// Sessions with no turn_duration events (e.g. older Claude Code versions)
	// should yield TotalTurnDurationMs=0, preserving FirstMessageAt/LastMessageAt
	// as the only available time-span signal.
	raw := makeParentSession()
	_, session := transformSession(&raw, testConfig())

	if session.TotalTurnDurationMs != 0 {
		t.Errorf("TotalTurnDurationMs = %d, want 0", session.TotalTurnDurationMs)
	}
	if session.FirstMessageAt == 0 {
		t.Error("FirstMessageAt should still be set when turn_duration is absent")
	}
}

// --- per-tool-call duration / tool_use_id / interrupted ---

// makeToolPairLines returns a (tool_use, tool_result) pair of ParsedLines
// for the given tool_use_id, tool name, and timestamps. The tool_result
// line carries the given toolUseResult envelope. Used by the per-call
// duration tests below.
func makeToolPairLines(
	toolUseID, toolName string,
	useTS, resultTS time.Time,
	envelope parser.ParsedToolUseResult,
	bashCmd string,
) []parser.ParsedLine {
	useContent := `[{"type":"tool_use","id":"` + toolUseID +
		`","name":"` + toolName + `","input":{"command":"` + bashCmd + `"}}]`
	resultContent := `[{"type":"tool_result","tool_use_id":"` + toolUseID +
		`","content":"ok"}]`
	return []parser.ParsedLine{
		{
			Type:      "assistant",
			Timestamp: useTS,
			SessionID: "parent-uuid-1234",
			Cwd:       "/home/user/project",
			Message: parser.ParsedMessage{
				Role:    "assistant",
				Content: json.RawMessage(useContent),
				Model:   "claude-opus-4-6",
			},
		},
		{
			Type:          "user",
			Timestamp:     resultTS,
			SessionID:     "parent-uuid-1234",
			Cwd:           "/home/user/project",
			ToolUseResult: envelope,
			Message: parser.ParsedMessage{
				Role:    "user",
				Content: json.RawMessage(resultContent),
			},
		},
	}
}

func TestTransformSession_ToolUseIDPropagatedOnBothRows(t *testing.T) {
	// The transform must persist tool_use.id on the assistant tool_use row
	// AND propagate it to the matching tool_result row. This is the load-
	// bearing JOIN key for downstream queries that need to pair calls
	// without falling back to lossy adjacency-based heuristics.
	raw := makeParentSession()
	raw.Lines = append(raw.Lines,
		makeToolPairLines(
			"toolu_abc",
			"Bash",
			time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC),
			time.Date(2026, 3, 10, 10, 0, 7, 0, time.UTC),
			parser.ParsedToolUseResult{},
			"ls",
		)...,
	)

	msgs, _ := transformSession(&raw, testConfig())

	var use, result *model.AgentMessage
	for i := range msgs {
		switch msgs[i].Role {
		case "assistant":
			if msgs[i].ToolName == "Bash" {
				use = &msgs[i]
			}
		case "tool":
			result = &msgs[i]
		}
	}
	if use == nil {
		t.Fatal("no assistant tool_use row found")
	}
	if result == nil {
		t.Fatal("no tool_result row found")
	}
	if use.ToolUseID != "toolu_abc" {
		t.Errorf("tool_use.ToolUseID = %q, want toolu_abc", use.ToolUseID)
	}
	if result.ToolUseID != "toolu_abc" {
		t.Errorf("tool_result.ToolUseID = %q, want toolu_abc", result.ToolUseID)
	}
}

func TestTransformSession_DurationMsFromExplicitEnvelope(t *testing.T) {
	// When Claude emits toolUseResult.durationMs we MUST prefer it over
	// the ts-diff fallback because it is wall-clock and correct under
	// interruption. The ts-diff here would be 3200ms; the envelope says
	// 2500ms — assert we surface 2500.
	raw := makeParentSession()
	raw.Lines = append(raw.Lines,
		makeToolPairLines(
			"toolu_explicit",
			"WebFetch",
			time.Date(2026, 3, 10, 10, 0, 1, 0, time.UTC),
			time.Date(2026, 3, 10, 10, 0, 4, 200_000_000, time.UTC),
			parser.ParsedToolUseResult{Present: true, DurationMs: 2500},
			"",
		)...,
	)

	msgs, _ := transformSession(&raw, testConfig())

	var result *model.AgentMessage
	for i := range msgs {
		if msgs[i].Role == "tool" {
			result = &msgs[i]
		}
	}
	if result == nil {
		t.Fatal("no tool_result row found")
	}
	if got, want := result.DurationMs, int64(2500); got != want {
		t.Errorf("DurationMs = %d, want %d (explicit envelope must win over ts-diff)", got, want)
	}
}

func TestTransformSession_DurationMsFallbackFromTimestampDelta(t *testing.T) {
	// When the envelope is absent (e.g. Read, Edit, most tools that emit
	// a bare-string toolUseResult, or older Claude Code versions), we MUST
	// fall back to (result_ts - use_ts) so the field is still populated
	// for the common case.
	raw := makeParentSession()
	raw.Lines = append(raw.Lines,
		makeToolPairLines(
			"toolu_fallback",
			"Read",
			time.Date(2026, 3, 10, 10, 0, 1, 0, time.UTC),
			time.Date(2026, 3, 10, 10, 0, 4, 200_000_000, time.UTC),
			parser.ParsedToolUseResult{}, // Present=false; no envelope
			"",
		)...,
	)

	msgs, _ := transformSession(&raw, testConfig())

	var result *model.AgentMessage
	for i := range msgs {
		if msgs[i].Role == "tool" {
			result = &msgs[i]
		}
	}
	if result == nil {
		t.Fatal("no tool_result row found")
	}
	if got, want := result.DurationMs, int64(3200); got != want {
		t.Errorf("DurationMs = %d, want %d (ts-diff fallback)", got, want)
	}
}

func TestTransformSession_InterruptedFlagSurvives(t *testing.T) {
	// The `interrupted` envelope flag is Claude's literal "stuck or
	// cancelled" signal and is the only way to distinguish (a) expected-
	// slow from (b) hung tool calls. Verify the boolean survives the
	// transform.
	raw := makeParentSession()
	raw.Lines = append(raw.Lines,
		makeToolPairLines(
			"toolu_interrupt",
			"Bash",
			time.Date(2026, 3, 10, 10, 0, 1, 0, time.UTC),
			time.Date(2026, 3, 10, 10, 0, 9, 0, time.UTC),
			parser.ParsedToolUseResult{Present: true, Interrupted: true},
			"sleep 30",
		)...,
	)

	msgs, _ := transformSession(&raw, testConfig())

	var result *model.AgentMessage
	for i := range msgs {
		if msgs[i].Role == "tool" {
			result = &msgs[i]
		}
	}
	if result == nil {
		t.Fatal("no tool_result row found")
	}
	if !result.Interrupted {
		t.Error("Interrupted = false, want true (envelope flag must survive)")
	}
}

func TestTransformSession_ParallelToolCallsPairedByID(t *testing.T) {
	// When the agent dispatches multiple tools in parallel (common for
	// Read/Glob bursts), the JSONL contains interleaved tool_use blocks
	// followed by interleaved tool_result blocks. Adjacency-based pairing
	// silently mis-pairs these; the id-based JOIN must keep each result
	// matched to its correct originator.
	//
	// Layout: two tool_use blocks on one assistant line, then two
	// tool_result blocks on one user line, in opposite order.
	raw := makeParentSession()
	useTS := time.Date(2026, 3, 10, 10, 0, 1, 0, time.UTC)
	resultTS := time.Date(2026, 3, 10, 10, 0, 5, 0, time.UTC)
	raw.Lines = append(raw.Lines,
		parser.ParsedLine{
			Type:      "assistant",
			Timestamp: useTS,
			SessionID: "parent-uuid-1234",
			Cwd:       "/home/user/project",
			Message: parser.ParsedMessage{
				Role: "assistant",
				Content: json.RawMessage(`[
					{"type":"tool_use","id":"toolu_A","name":"Read","input":{"file_path":"/a.txt"}},
					{"type":"tool_use","id":"toolu_B","name":"Read","input":{"file_path":"/b.txt"}}
				]`),
				Model: "claude-opus-4-6",
			},
		},
		// Results in REVERSE order — B before A. Adjacency would mis-pair.
		parser.ParsedLine{
			Type:      "user",
			Timestamp: resultTS,
			SessionID: "parent-uuid-1234",
			Cwd:       "/home/user/project",
			Message: parser.ParsedMessage{
				Role: "user",
				Content: json.RawMessage(`[
					{"type":"tool_result","tool_use_id":"toolu_B","content":"b contents"},
					{"type":"tool_result","tool_use_id":"toolu_A","content":"a contents"}
				]`),
			},
		},
	)

	msgs, _ := transformSession(&raw, testConfig())

	// Collect tool_result rows in order; check each one keeps the correct
	// originator id and inherited file_path metadata.
	var results []model.AgentMessage
	for _, m := range msgs {
		if m.Role == "tool" {
			results = append(results, m)
		}
	}
	if len(results) != 2 {
		t.Fatalf("tool_result rows = %d, want 2", len(results))
	}
	// First result row corresponds to B (because the result line lists B first).
	if results[0].ToolUseID != "toolu_B" {
		t.Errorf("results[0].ToolUseID = %q, want toolu_B", results[0].ToolUseID)
	}
	if results[0].ToolFilePath != "/b.txt" {
		t.Errorf("results[0].ToolFilePath = %q, want /b.txt (id-based join, not adjacency)", results[0].ToolFilePath)
	}
	if results[1].ToolUseID != "toolu_A" {
		t.Errorf("results[1].ToolUseID = %q, want toolu_A", results[1].ToolUseID)
	}
	if results[1].ToolFilePath != "/a.txt" {
		t.Errorf("results[1].ToolFilePath = %q, want /a.txt (id-based join, not adjacency)", results[1].ToolFilePath)
	}
}
