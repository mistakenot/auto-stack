package writer

import (
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-etl/internal/model"
)

func TestWriteHooksRoundTrip(t *testing.T) {
	dir := t.TempDir()

	rows := []model.HookEventRow{
		{
			ID:            "aaa",
			HostID:        "host1",
			Agent:         "claude",
			Event:         "PostToolUse",
			SessionID:     "sess1",
			Cwd:           "/tmp",
			Project:       "proj1",
			Tool:          "Edit",
			PathsJSON:     `["/a.go"]`,
			CapturedAt:    1749636000000,
			RawJSON:       `{"hook_event_name":"PostToolUse"}`,
			SourceFile:    "events-2026-06-11.jsonl",
			Year:          2026,
			Month:         6,
			SchemaVersion: model.HookSchemaVersion,
		},
	}

	if err := WriteHooks(dir, rows); err != nil {
		t.Fatal(err)
	}

	// Read back.
	path := filepath.Join(dir, "hooks", "year=2026", "month=06", "hooks.parquet")
	got, err := readExistingParquet[model.HookEventRow](path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}

	r := got[0]
	if r.ID != "aaa" {
		t.Errorf("ID = %q, want aaa", r.ID)
	}
	if r.Agent != "claude" {
		t.Errorf("Agent = %q, want claude", r.Agent)
	}
	if r.Event != "PostToolUse" {
		t.Errorf("Event = %q, want PostToolUse", r.Event)
	}
	if r.RawJSON != `{"hook_event_name":"PostToolUse"}` {
		t.Errorf("RawJSON = %q", r.RawJSON)
	}
	if r.PathsJSON != `["/a.go"]` {
		t.Errorf("PathsJSON = %q", r.PathsJSON)
	}
}

func TestWriteHooksDedup(t *testing.T) {
	dir := t.TempDir()

	rows1 := []model.HookEventRow{
		{ID: "aaa", Event: "PostToolUse", Year: 2026, Month: 6, SchemaVersion: model.HookSchemaVersion},
		{ID: "bbb", Event: "PreToolUse", Year: 2026, Month: 6, SchemaVersion: model.HookSchemaVersion},
	}

	if err := WriteHooks(dir, rows1); err != nil {
		t.Fatal(err)
	}

	// Write again with overlapping ID "aaa" and a new "ccc".
	rows2 := []model.HookEventRow{
		{ID: "aaa", Event: "PostToolUse-Updated", Year: 2026, Month: 6, SchemaVersion: model.HookSchemaVersion},
		{ID: "ccc", Event: "Stop", Year: 2026, Month: 6, SchemaVersion: model.HookSchemaVersion},
	}

	if err := WriteHooks(dir, rows2); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "hooks", "year=2026", "month=06", "hooks.parquet")
	got, err := readExistingParquet[model.HookEventRow](path)
	if err != nil {
		t.Fatal(err)
	}

	// Should have 3 unique rows: bbb (existing), aaa (updated), ccc (new).
	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3 (deduped)", len(got))
	}

	byID := map[string]model.HookEventRow{}
	for _, r := range got {
		byID[r.ID] = r
	}

	// "aaa" should have the updated event (incoming wins).
	if byID["aaa"].Event != "PostToolUse-Updated" {
		t.Errorf("aaa Event = %q, want PostToolUse-Updated", byID["aaa"].Event)
	}
	if _, ok := byID["bbb"]; !ok {
		t.Error("expected bbb to be preserved")
	}
	if _, ok := byID["ccc"]; !ok {
		t.Error("expected ccc to be present")
	}
}
