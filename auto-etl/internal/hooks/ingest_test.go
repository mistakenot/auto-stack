package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIngestBasic(t *testing.T) {
	rawDir := t.TempDir()

	line1 := `{"agent":"claude","capturedAt":"2026-06-11T10:00:00Z","hostId":"host1","cwd":"/tmp","project":"proj1","payload":{"hook_event_name":"PostToolUse","session_id":"sess1","tool_name":"Edit","tool_input":{"file_path":"/a.go"}}}`
	line2 := `{"agent":"codex","capturedAt":"2026-06-11T10:01:00Z","hostId":"host1","cwd":"/tmp","project":"proj1","payload":{"hook_event_name":"PreToolUse","session_id":"sess2","tool_name":"Bash","tool_input":{"command":"ls"}}}`

	content := line1 + "\n" + line2 + "\n"
	if err := os.WriteFile(filepath.Join(rawDir, "events-2026-06-11.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	state := newHooksSyncState()
	rows, err := Ingest(rawDir, state, "fallback-host")
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	// Check first row normalized fields.
	r := rows[0]
	if r.Agent != "claude" {
		t.Errorf("Agent = %q, want claude", r.Agent)
	}
	if r.Event != "PostToolUse" {
		t.Errorf("Event = %q, want PostToolUse", r.Event)
	}
	if r.SessionID != "sess1" {
		t.Errorf("SessionID = %q, want sess1", r.SessionID)
	}
	if r.Tool != "Edit" {
		t.Errorf("Tool = %q, want Edit", r.Tool)
	}
	if r.PathsJSON != `["/a.go"]` {
		t.Errorf("PathsJSON = %q, want [\"/a.go\"]", r.PathsJSON)
	}
	if r.HostID != "host1" {
		t.Errorf("HostID = %q, want host1", r.HostID)
	}
	if r.Cwd != "/tmp" {
		t.Errorf("Cwd = %q, want /tmp", r.Cwd)
	}
	if r.Project != "proj1" {
		t.Errorf("Project = %q, want proj1", r.Project)
	}
	if r.CapturedAt == 0 {
		t.Error("CapturedAt should be non-zero")
	}
	if r.Year != 2026 {
		t.Errorf("Year = %d, want 2026", r.Year)
	}
	if r.Month != 6 {
		t.Errorf("Month = %d, want 6", r.Month)
	}
	if r.ID == "" {
		t.Error("ID should be non-empty")
	}

	// Check second row.
	if rows[1].Agent != "codex" {
		t.Errorf("row[1].Agent = %q, want codex", rows[1].Agent)
	}

	// State should have advanced.
	fs := state.Files["events-2026-06-11.jsonl"]
	if fs == nil {
		t.Fatal("expected file state entry")
	}
	if fs.Offset != int64(len(content)) {
		t.Errorf("offset = %d, want %d", fs.Offset, len(content))
	}
}

func TestIngestIncremental(t *testing.T) {
	rawDir := t.TempDir()
	filePath := filepath.Join(rawDir, "events-2026-06-11.jsonl")

	line1 := `{"agent":"claude","capturedAt":"2026-06-11T10:00:00Z","hostId":"host1","cwd":"/tmp","project":"proj1","payload":{"hook_event_name":"PostToolUse","session_id":"sess1","tool_name":"Edit","tool_input":{"file_path":"/a.go"}}}`
	line2 := `{"agent":"codex","capturedAt":"2026-06-11T10:01:00Z","hostId":"host1","cwd":"/tmp","project":"proj1","payload":{"hook_event_name":"PreToolUse","session_id":"sess2","tool_name":"Bash","tool_input":{"command":"ls"}}}`

	content := line1 + "\n" + line2 + "\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	state := newHooksSyncState()

	// First ingest — should get 2 rows.
	rows, err := Ingest(rawDir, state, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("first ingest: got %d rows, want 2", len(rows))
	}

	// Second ingest with same state — should get 0.
	rows, err = Ingest(rawDir, state, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("second ingest: got %d rows, want 0", len(rows))
	}

	// Append a third line.
	line3 := `{"agent":"claude","capturedAt":"2026-06-11T10:02:00Z","hostId":"host1","cwd":"/tmp","project":"proj1","payload":{"hook_event_name":"Stop","session_id":"sess3"}}`
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(line3 + "\n")
	f.Close()

	// Third ingest — should get 1 new row.
	rows, err = Ingest(rawDir, state, "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("third ingest: got %d rows, want 1", len(rows))
	}
	if rows[0].Event != "Stop" {
		t.Errorf("Event = %q, want Stop", rows[0].Event)
	}
}

func TestIngestMalformed(t *testing.T) {
	rawDir := t.TempDir()

	content := "not json at all\n"
	if err := os.WriteFile(filepath.Join(rawDir, "events-2026-06-11.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	state := newHooksSyncState()
	rows, err := Ingest(rawDir, state, "fallback-host")
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}

	// RawJSON should contain the malformed line since payload was empty.
	if rows[0].RawJSON != "not json at all" {
		t.Errorf("RawJSON = %q, want %q", rows[0].RawJSON, "not json at all")
	}

	// Fallback host should be used.
	if rows[0].HostID != "fallback-host" {
		t.Errorf("HostID = %q, want fallback-host", rows[0].HostID)
	}
}

func TestIngestLargeLine(t *testing.T) {
	rawDir := t.TempDir()

	// Build a payload with >64KB of data (bufio.Scanner default limit).
	bigValue := strings.Repeat("x", 200*1024)
	line := `{"agent":"claude","capturedAt":"2026-06-11T10:00:00Z","hostId":"host1","cwd":"/tmp","project":"proj1","payload":{"hook_event_name":"PostToolUse","session_id":"sess1","tool_name":"Write","tool_input":{"file_path":"/big.go","content":"` + bigValue + `"}}}`

	content := line + "\n"
	if err := os.WriteFile(filepath.Join(rawDir, "events-2026-06-11.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	state := newHooksSyncState()
	rows, err := Ingest(rawDir, state, "fallback")
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Tool != "Write" {
		t.Errorf("Tool = %q, want Write", rows[0].Tool)
	}
}

func TestIngestPartialLine(t *testing.T) {
	rawDir := t.TempDir()
	filePath := filepath.Join(rawDir, "events-2026-06-11.jsonl")

	// Write a complete line followed by a partial line (no trailing \n).
	completeLine := `{"agent":"claude","capturedAt":"2026-06-11T10:00:00Z","hostId":"host1","cwd":"/tmp","project":"proj1","payload":{"hook_event_name":"PostToolUse","session_id":"sess1"}}`
	partialLine := `{"agent":"codex","capturedAt":"2026-06-11T10:01:00Z"`

	content := completeLine + "\n" + partialLine
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	state := newHooksSyncState()
	rows, err := Ingest(rawDir, state, "fallback")
	if err != nil {
		t.Fatal(err)
	}

	// Only the complete line should be ingested.
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 (partial line should be skipped)", len(rows))
	}

	// Offset should stop before the partial line.
	fs := state.Files["events-2026-06-11.jsonl"]
	expectedOffset := int64(len(completeLine) + 1) // +1 for \n
	if fs.Offset != expectedOffset {
		t.Errorf("offset = %d, want %d (should not include partial line)", fs.Offset, expectedOffset)
	}
}
