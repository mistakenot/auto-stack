package sessionhtml

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-search/internal/indexdb"
)

// testSession / testMessage are thin builders so the table data below reads as
// a scenario, not 26-/38-arg call sites.
type testSession struct {
	id, parent, subagentName, intent, model string
	isSubagent                              bool
	firstMs, lastMs, totalTokens            int64
}

type testMessage struct {
	id, session, role, content, contentTrunc string
	idx                                      int
	toolName, toolInput, bashCommand         string
	bashExit                                 int
	skillName, toolUseID                     string
	durationMs                               int64
	interrupted, isError                     bool
	outputTokens                             int
}

func newTestDB(t *testing.T, sessions []testSession, messages []testMessage) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.sqlite")
	db, err := indexdb.Create(path)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	for _, s := range sessions {
		if err := indexdb.InsertSession(tx, "p.parquet",
			s.id, s.parent, "test-host", "claude", s.subagentName,
			s.isSubagent, "/ws", "git@github.com:test/repo", s.model, "/src.jsonl",
			s.firstMs, s.lastMs, 0,
			0, 0, s.totalTokens,
			0, 0, 0,
			"", s.intent, s.intent,
			"", "", 1,
		); err != nil {
			t.Fatalf("insert session %s: %v", s.id, err)
		}
	}
	for i := range messages {
		m := messages[i]
		if err := indexdb.InsertMessage(tx, "p.parquet",
			m.id, m.session, "test-host",
			m.idx, m.role, m.content, m.contentTrunc, 0,
			m.toolName, m.toolInput, "",
			0, 0, 0,
			m.bashCommand, m.bashExit, m.skillName,
			m.toolUseID, m.durationMs, m.interrupted,
			0, 0, m.outputTokens,
			"/ws", "git@github.com:test/repo", "main", "opus",
			"", false, 0, 1,
			"", "", "", m.isError,
			0, 0,
		); err != nil {
			t.Fatalf("insert message %s: %v", m.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return db
}

// scenario builds a coordinator (p1) that dispatches one sub-agent (c1) plus a
// failing Bash call, a thinking block, and assistant prose. c1's intent matches
// the dispatch prompt so correlation can be exercised.
func scenario() ([]testSession, []testMessage) {
	sessions := []testSession{
		{id: "p1", intent: "/execute-task 044", model: "opus", firstMs: 1000, lastMs: 61000, totalTokens: 8000},
		{id: "c1", parent: "p1", subagentName: "Explore", isSubagent: true,
			intent: "Explore the auth module structure and report back", model: "sonnet",
			firstMs: 2000, lastMs: 12000, totalTokens: 3500},
	}
	messages := []testMessage{
		{id: "p1-0", session: "p1", idx: 0, role: "user",
			content:      "<command-name>/execute-task</command-name> <command-args>044</command-args>",
			contentTrunc: "<command-name>/execute-task</command-name> <command-args>044</command-args>"},
		{id: "p1-1", session: "p1", idx: 1, role: "thinking",
			content:      "Let me plan the approach in detail before coding.",
			contentTrunc: "Let me plan the approach"},
		{id: "p1-2", session: "p1", idx: 2, role: "assistant",
			content:      "I'll start by exploring the structure of the codebase carefully.",
			contentTrunc: "I'll start by exploring"},
		// Agent dispatch — prompt prefix-matches c1's intent.
		{id: "p1-3", session: "p1", idx: 3, role: "assistant", toolName: "Agent",
			toolInput: `{"description":"explore auth","subagent_type":"Explore","prompt":"Explore the auth module structure and report back with the file layout"}`,
			toolUseID: "tu-agent", durationMs: 10000},
		{id: "p1-3r", session: "p1", idx: 4, role: "tool", toolName: "Agent",
			content: "Found 2 files in the auth module.", toolUseID: "tu-agent", durationMs: 10000},
		// Failing Bash call paired by tool_use_id.
		{id: "p1-4", session: "p1", idx: 5, role: "assistant", toolName: "Bash",
			toolInput: `{"command":"go test ./..."}`, bashCommand: "go test ./...", toolUseID: "tu-bash"},
		{id: "p1-4r", session: "p1", idx: 6, role: "tool", toolName: "Bash",
			content: "FAIL\nexit status 2", contentTrunc: "FAIL", toolUseID: "tu-bash",
			bashExit: 2, durationMs: 15000, isError: true},
		// Child messages.
		{id: "c1-0", session: "c1", idx: 0, role: "user",
			content:      "Explore the auth module structure and report back with the file layout",
			contentTrunc: "Explore the auth module"},
		{id: "c1-1", session: "c1", idx: 1, role: "assistant",
			content:      "The auth module has middleware.go and token.go.",
			contentTrunc: "The auth module"},
	}
	return sessions, messages
}

func TestBuildModelCorrelatesAndPairs(t *testing.T) {
	sessions, messages := scenario()
	db := newTestDB(t, sessions, messages)

	root, err := BuildModel(db, "p1", Options{IncludeThinking: true})
	if err != nil {
		t.Fatalf("BuildModel: %v", err)
	}
	if root == nil {
		t.Fatal("root node is nil")
	}

	if root.Title != "/execute-task 044" {
		t.Errorf("title = %q, want /execute-task 044", root.Title)
	}
	if root.Counts.Bash != 1 {
		t.Errorf("bash count = %d, want 1", root.Counts.Bash)
	}
	if root.Counts.Error != 1 {
		t.Errorf("error count = %d, want 1", root.Counts.Error)
	}
	if root.Counts.Agent != 1 {
		t.Errorf("agent count = %d, want 1", root.Counts.Agent)
	}

	// Find the agent event and assert correlation.
	var agentEv *Event
	var bashEv *Event
	for i := range root.Events {
		switch root.Events[i].Kind {
		case "agent":
			agentEv = &root.Events[i]
		case "tool":
			if root.Events[i].Tool == "Bash" {
				bashEv = &root.Events[i]
			}
		}
	}
	if agentEv == nil {
		t.Fatal("no agent event found")
	}
	if agentEv.Child == nil {
		t.Fatal("agent event has no correlated child")
	}
	if agentEv.Child.ID != "c1" {
		t.Errorf("child id = %q, want c1", agentEv.Child.ID)
	}
	if agentEv.Child.SubagentName != "Explore" {
		t.Errorf("child subagent = %q, want Explore", agentEv.Child.SubagentName)
	}
	if agentEv.Child.Depth != 1 {
		t.Errorf("child depth = %d, want 1", agentEv.Child.Depth)
	}
	if agentEv.SubagentType != "Explore" {
		t.Errorf("dispatch subagent_type = %q, want Explore", agentEv.SubagentType)
	}

	// Tool pairing: duration / exit / error live on the result row.
	if bashEv == nil {
		t.Fatal("no bash event found")
	}
	if bashEv.Duration != 15000 {
		t.Errorf("bash duration = %d, want 15000", bashEv.Duration)
	}
	if bashEv.Exit != 2 {
		t.Errorf("bash exit = %d, want 2", bashEv.Exit)
	}
	if !bashEv.IsError {
		t.Error("bash event should be flagged is_error")
	}
	if bashEv.Output != "FAIL\nexit status 2" {
		t.Errorf("bash output = %q, want full content", bashEv.Output)
	}
}

func TestBuildModelThinkingToggle(t *testing.T) {
	sessions, messages := scenario()
	db := newTestDB(t, sessions, messages)

	with, err := BuildModel(db, "p1", Options{IncludeThinking: true})
	if err != nil {
		t.Fatalf("BuildModel(include): %v", err)
	}
	if countKind(with, "thinking") != 1 {
		t.Errorf("thinking events with IncludeThinking = %d, want 1", countKind(with, "thinking"))
	}

	without, err := BuildModel(db, "p1", Options{IncludeThinking: false})
	if err != nil {
		t.Fatalf("BuildModel(exclude): %v", err)
	}
	if countKind(without, "thinking") != 0 {
		t.Errorf("thinking events without IncludeThinking = %d, want 0", countKind(without, "thinking"))
	}
}

func TestBuildModelLightUsesTruncatedContent(t *testing.T) {
	sessions, messages := scenario()
	db := newTestDB(t, sessions, messages)

	light, err := BuildModel(db, "p1", Options{Light: true})
	if err != nil {
		t.Fatalf("BuildModel(light): %v", err)
	}
	// The assistant prose event should carry the truncated body, flagged.
	var found bool
	for i := range light.Events {
		e := &light.Events[i]
		if e.Kind == "assistant" {
			found = true
			if e.Body != "I'll start by exploring" {
				t.Errorf("light assistant body = %q, want truncated", e.Body)
			}
			if !e.Truncated {
				t.Error("light assistant event should be flagged truncated")
			}
		}
	}
	if !found {
		t.Fatal("no assistant event found")
	}

	// Default (full content) must NOT truncate.
	full, err := BuildModel(db, "p1", Options{IncludeThinking: true})
	if err != nil {
		t.Fatalf("BuildModel(full): %v", err)
	}
	for i := range full.Events {
		e := &full.Events[i]
		if e.Kind == "assistant" && e.Truncated {
			t.Error("default export should embed full content, not truncate")
		}
	}
}

func TestBuildModelUnknownRoot(t *testing.T) {
	sessions, messages := scenario()
	db := newTestDB(t, sessions, messages)
	if _, err := BuildModel(db, "does-not-exist", Options{}); err == nil {
		t.Fatal("expected error for unknown root session")
	}
}

func TestBuildModelUnmatchedDispatchNoCrash(t *testing.T) {
	// A coordinator that dispatches an Agent but has no child session: the
	// dispatch should render with no child rather than erroring.
	sessions := []testSession{{id: "solo", intent: "/run", firstMs: 1, lastMs: 2}}
	messages := []testMessage{
		{id: "s-0", session: "solo", idx: 0, role: "user", content: "go"},
		{id: "s-1", session: "solo", idx: 1, role: "assistant", toolName: "Agent",
			toolInput: `{"description":"do work","prompt":"do some work"}`, toolUseID: "tu-x"},
	}
	db := newTestDB(t, sessions, messages)
	root, err := BuildModel(db, "solo", Options{})
	if err != nil {
		t.Fatalf("BuildModel: %v", err)
	}
	var agentEv *Event
	for i := range root.Events {
		if root.Events[i].Kind == "agent" {
			agentEv = &root.Events[i]
		}
	}
	if agentEv == nil {
		t.Fatal("no agent event")
	}
	if agentEv.Child != nil {
		t.Errorf("unmatched dispatch should have nil child, got %v", agentEv.Child)
	}
}

func countKind(n *Node, kind string) int {
	c := 0
	var walk func(*Node)
	walk = func(x *Node) {
		for i := range x.Events {
			if x.Events[i].Kind == kind {
				c++
			}
			if x.Events[i].Child != nil {
				walk(x.Events[i].Child)
			}
		}
	}
	walk(n)
	return c
}
