package manifest

import (
	"path/filepath"
	"testing"
)

func TestWriteRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".generated")
	files := []string{"ecosystem.config.js", "Caddyfile", "web/config.json"}

	if err := Write(path, files); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != len(files) {
		t.Fatalf("got %d files, want %d", len(got), len(files))
	}
	for i, f := range files {
		if got[i] != f {
			t.Errorf("file[%d] = %q, want %q", i, got[i], f)
		}
	}
}

func TestReadMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent")

	files, err := Read(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if files != nil {
		t.Errorf("expected nil, got %v", files)
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".generated")

	if Exists(path) {
		t.Error("expected false for nonexistent file")
	}

	if err := Write(path, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if !Exists(path) {
		t.Error("expected true after writing")
	}
}
