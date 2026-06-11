package etlread

import (
	"os"
	"path/filepath"
	"testing"

	sharedmodel "github.com/mistakenot/auto-shared/model"
	"github.com/parquet-go/parquet-go"
)

func writeParquetFile[T any](t *testing.T, path string, rows []T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	w := parquet.NewGenericWriter[T](f)
	if _, err := w.Write(rows); err != nil {
		t.Fatalf("write rows: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
}

func TestReadSessions(t *testing.T) {
	root := t.TempDir()
	sessions := []sharedmodel.AgentSession{
		{
			ID:        "sess-001",
			Workspace: "/home/user/project",
			GitRemote: "github.com/example/repo",
			Agent:     "claude",
			Model:     "opus",
		},
		{
			ID:        "sess-002",
			Workspace: "/home/user/other",
			GitRemote: "github.com/example/other",
			Agent:     "claude",
			Model:     "sonnet",
		},
	}
	writeParquetFile(t, filepath.Join(root, "sessions", "year=2026", "data.parquet"), sessions)

	got, err := ReadSessions(root)
	if err != nil {
		t.Fatalf("ReadSessions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(got))
	}
	if got[0].ID != "sess-001" {
		t.Errorf("session[0].ID = %q, want %q", got[0].ID, "sess-001")
	}
	if got[1].Workspace != "/home/user/other" {
		t.Errorf("session[1].Workspace = %q, want %q", got[1].Workspace, "/home/user/other")
	}
}

func TestReadMessageSignals_Projection(t *testing.T) {
	root := t.TempDir()
	// Write full AgentMessage rows — the reader should project to MsgSignalRow
	messages := []sharedmodel.AgentMessage{
		{
			ID:               "msg-001",
			SessionID:        "sess-001",
			Role:             "assistant",
			Content:          "This is the full content that should NOT appear in MsgSignalRow",
			ContentTruncated: "This is truncated",
			ToolName:         "Read",
			IsError:          false,
			Workspace:        "/home/user/project",
			GitRemote:        "github.com/example/repo",
			IsSubagent:       false,
			ParentSessionID:  "",
		},
		{
			ID:               "msg-002",
			SessionID:        "sess-001",
			Role:             "tool",
			Content:          "Large tool output blob",
			ContentTruncated: "Large tool...",
			ToolName:         "Bash",
			IsError:          true,
			Workspace:        "/home/user/project",
			GitRemote:        "github.com/example/repo",
			IsSubagent:       true,
			ParentSessionID:  "sess-parent",
		},
	}
	writeParquetFile(t, filepath.Join(root, "messages", "year=2026", "data.parquet"), messages)

	got, err := ReadMessageSignals(root)
	if err != nil {
		t.Fatalf("ReadMessageSignals: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 message signals, got %d", len(got))
	}

	// Verify projection fields are populated
	if got[0].SessionID != "sess-001" {
		t.Errorf("msg[0].SessionID = %q, want %q", got[0].SessionID, "sess-001")
	}
	if got[0].Role != "assistant" {
		t.Errorf("msg[0].Role = %q, want %q", got[0].Role, "assistant")
	}
	if got[0].ContentTruncated != "This is truncated" {
		t.Errorf("msg[0].ContentTruncated = %q, want %q", got[0].ContentTruncated, "This is truncated")
	}
	if got[0].ToolName != "Read" {
		t.Errorf("msg[0].ToolName = %q, want %q", got[0].ToolName, "Read")
	}

	// Verify second row error/subagent fields
	if !got[1].IsError {
		t.Errorf("msg[1].IsError = false, want true")
	}
	if !got[1].IsSubagent {
		t.Errorf("msg[1].IsSubagent = false, want true")
	}
	if got[1].ParentSessionID != "sess-parent" {
		t.Errorf("msg[1].ParentSessionID = %q, want %q", got[1].ParentSessionID, "sess-parent")
	}
}

func TestResolveSource_OK(t *testing.T) {
	root := t.TempDir()
	sessions := []sharedmodel.AgentSession{{ID: "sess-001"}}
	writeParquetFile(t, filepath.Join(root, "sessions", "data.parquet"), sessions)

	state, err := ResolveSource(root)
	if err != nil {
		t.Fatalf("ResolveSource: %v", err)
	}
	if state != SourceOK {
		t.Errorf("state = %d, want SourceOK (%d)", state, SourceOK)
	}
}

func TestResolveSource_Empty(t *testing.T) {
	root := t.TempDir()
	// Create the sessions dir but no parquet files
	if err := os.MkdirAll(filepath.Join(root, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}

	state, err := ResolveSource(root)
	if err != nil {
		t.Fatalf("ResolveSource: %v", err)
	}
	if state != SourceEmpty {
		t.Errorf("state = %d, want SourceEmpty (%d)", state, SourceEmpty)
	}
}

func TestResolveSource_Missing(t *testing.T) {
	state, err := ResolveSource("/nonexistent/path/for/testing")
	if err != nil {
		t.Fatalf("ResolveSource: %v", err)
	}
	if state != SourceMissing {
		t.Errorf("state = %d, want SourceMissing (%d)", state, SourceMissing)
	}
}

func TestReadSessions_MissingDir(t *testing.T) {
	root := t.TempDir()
	// No sessions/ subdir created
	got, err := ReadSessions(root)
	if err != nil {
		t.Fatalf("ReadSessions: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %d rows", len(got))
	}
}
