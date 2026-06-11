package hooks

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-etl/internal/model"
	"github.com/mistakenot/auto-etl/internal/writer"
	"github.com/parquet-go/parquet-go"
)

// TestE2EIngestWriteRoundTrip exercises the full hooks pipeline:
// Ingest JSONL -> produce HookEventRows -> WriteHooks parquet -> read back.
func TestE2EIngestWriteRoundTrip(t *testing.T) {
	rawDir := t.TempDir()
	outputDir := t.TempDir()

	// --- Write 2 envelope lines ------------------------------------------------
	normalEnvelope := `{"agent":"claude","capturedAt":"2026-06-11T10:00:00Z","hostId":"test-host","cwd":"/tmp/proj","project":"myproj","payload":{"hook_event_name":"PostToolUse","session_id":"sess-1","tool_name":"Edit","tool_input":{"file_path":"/tmp/proj/main.go"}}}`
	garbageEnvelope := `{"agent":"codex","capturedAt":"2026-06-11T10:05:00Z","hostId":"test-host","payload":"not a json object"}`

	content := normalEnvelope + "\n" + garbageEnvelope + "\n"
	if err := os.WriteFile(filepath.Join(rawDir, "events-2026-06-11.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// --- Ingest -----------------------------------------------------------------
	state := newHooksSyncState()
	rows, err := Ingest(rawDir, state, "fallback-host")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("Ingest returned %d rows, want 2", len(rows))
	}

	// --- Write to parquet -------------------------------------------------------
	if err := writer.WriteHooks(outputDir, rows); err != nil {
		t.Fatal(err)
	}

	// --- Read back from parquet -------------------------------------------------
	parquetPath := filepath.Join(outputDir, "hooks", "year=2026", "month=06", "hooks.parquet")
	got := readParquetRows(t, parquetPath)

	if len(got) != 2 {
		t.Fatalf("parquet contains %d rows, want 2", len(got))
	}

	// --- Assert row 1 (normal PostToolUse envelope) -----------------------------
	r1 := got[0]
	if r1.Agent != "claude" {
		t.Errorf("row1 Agent = %q, want claude", r1.Agent)
	}
	if r1.Event != "PostToolUse" {
		t.Errorf("row1 Event = %q, want PostToolUse", r1.Event)
	}
	if r1.SessionID != "sess-1" {
		t.Errorf("row1 SessionID = %q, want sess-1", r1.SessionID)
	}
	if r1.Tool != "Edit" {
		t.Errorf("row1 Tool = %q, want Edit", r1.Tool)
	}
	if r1.Project != "myproj" {
		t.Errorf("row1 Project = %q, want myproj", r1.Project)
	}
	if r1.Cwd != "/tmp/proj" {
		t.Errorf("row1 Cwd = %q, want /tmp/proj", r1.Cwd)
	}
	if !strings.Contains(r1.RawJSON, "PostToolUse") {
		t.Errorf("row1 RawJSON should contain PostToolUse, got %q", r1.RawJSON)
	}
	if !strings.Contains(r1.PathsJSON, "/tmp/proj/main.go") {
		t.Errorf("row1 PathsJSON should contain /tmp/proj/main.go, got %q", r1.PathsJSON)
	}

	// --- Assert row 2 (garbage payload preserved) --------------------------------
	r2 := got[1]
	if r2.Agent != "codex" {
		t.Errorf("row2 Agent = %q, want codex", r2.Agent)
	}
	if r2.RawJSON == "" {
		t.Error("row2 RawJSON should not be empty (garbage payload preserved)")
	}
	if r2.Event != "" {
		t.Errorf("row2 Event = %q, want empty (couldn't extract from garbage)", r2.Event)
	}

	// --- Incremental: re-ingest with same state => 0 new rows --------------------
	rows2, err := Ingest(rawDir, state, "fallback-host")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows2) != 0 {
		t.Fatalf("second Ingest returned %d rows, want 0 (incremental)", len(rows2))
	}

	// --- Dedup: re-write same rows => still 2 rows on disk -----------------------
	if err := writer.WriteHooks(outputDir, rows); err != nil {
		t.Fatal(err)
	}
	got2 := readParquetRows(t, parquetPath)
	if len(got2) != 2 {
		t.Fatalf("after dedup write: %d rows, want 2", len(got2))
	}
}

// TestE2EGarbagePayloadRawJSON verifies that a garbage payload string (not a
// JSON object) is stored verbatim in RawJSON so no data is lost.
func TestE2EGarbagePayloadRawJSON(t *testing.T) {
	rawDir := t.TempDir()
	outputDir := t.TempDir()

	envelope := `{"agent":"codex","capturedAt":"2026-06-11T10:05:00Z","hostId":"test-host","payload":"not a json object"}`
	content := envelope + "\n"
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

	if err := writer.WriteHooks(outputDir, rows); err != nil {
		t.Fatal(err)
	}

	parquetPath := filepath.Join(outputDir, "hooks", "year=2026", "month=06", "hooks.parquet")
	got := readParquetRows(t, parquetPath)
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}

	// The garbage payload should be preserved. Since it's a JSON string value,
	// json.RawMessage will store it as `"not a json object"` (with quotes).
	// Verify it's non-empty and parseable.
	if got[0].RawJSON == "" {
		t.Error("RawJSON should not be empty")
	}

	// Normalized fields should be empty since we couldn't extract from a string.
	if got[0].Event != "" {
		t.Errorf("Event = %q, want empty", got[0].Event)
	}
	if got[0].SessionID != "" {
		t.Errorf("SessionID = %q, want empty", got[0].SessionID)
	}
	if got[0].Tool != "" {
		t.Errorf("Tool = %q, want empty", got[0].Tool)
	}
}

// readParquetRows opens a parquet file and returns all HookEventRow rows.
func readParquetRows(t *testing.T, path string) []model.HookEventRow {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open parquet %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	reader := parquet.NewGenericReader[model.HookEventRow](f)
	defer func() { _ = reader.Close() }()

	rows := make([]model.HookEventRow, reader.NumRows())
	n, err := reader.Read(rows)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("read parquet rows: %v", err)
	}
	return rows[:n]
}
