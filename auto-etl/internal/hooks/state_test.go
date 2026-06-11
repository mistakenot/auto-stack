package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	state := newHooksSyncState()
	state.Files["events-2026-06-11.jsonl"] = &FileState{Offset: 42}

	if err := state.Save(path); err != nil {
		t.Fatal(err)
	}

	loaded := LoadHooksSyncState(path)
	fs, ok := loaded.Files["events-2026-06-11.jsonl"]
	if !ok {
		t.Fatal("expected file entry after round-trip")
	}
	if fs.Offset != 42 {
		t.Errorf("offset = %d, want 42", fs.Offset)
	}
}

func TestLoadCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := os.WriteFile(path, []byte("not valid json!!!"), 0o644); err != nil {
		t.Fatal(err)
	}

	state := LoadHooksSyncState(path)
	if state == nil {
		t.Fatal("expected non-nil state from corrupt file")
	}
	if state.Files == nil {
		t.Fatal("expected initialized Files map")
	}
	if len(state.Files) != 0 {
		t.Errorf("expected empty Files, got %d entries", len(state.Files))
	}
}

func TestLoadMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")

	state := LoadHooksSyncState(path)
	if state == nil {
		t.Fatal("expected non-nil state from missing file")
	}
	if state.Files == nil {
		t.Fatal("expected initialized Files map")
	}
	if len(state.Files) != 0 {
		t.Errorf("expected empty Files, got %d entries", len(state.Files))
	}
}

func TestNilMapGuard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Write JSON with "files": null explicitly.
	raw := `{"schema_version":1,"files":null}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	state := LoadHooksSyncState(path)
	if state.Files == nil {
		t.Fatal("Files map should be initialized, not nil")
	}
}
