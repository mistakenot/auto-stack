package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestJSONLAppendAndReadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "feedback.jsonl")

	if err := AppendJSONLine(path, map[string]any{"id": "a", "n": 1}); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := AppendJSONLine(path, map[string]any{"id": "b", "n": 2}); err != nil {
		t.Fatalf("append 2: %v", err)
	}

	var ids []string
	err := ReadJSONLines(path, func(_ int, line []byte) error {
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			return err
		}
		ids = append(ids, row["id"].(string))
		return nil
	})
	if err != nil {
		t.Fatalf("read lines: %v", err)
	}

	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("unexpected ids: %#v", ids)
	}
}

func TestReadJSONLinesEmptyFileAndMissingFile(t *testing.T) {
	tmp := t.TempDir()
	empty := filepath.Join(tmp, "empty.jsonl")
	if err := os.WriteFile(empty, []byte(""), 0o644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	seen := 0
	if err := ReadJSONLines(empty, func(_ int, _ []byte) error {
		seen++
		return nil
	}); err != nil {
		t.Fatalf("read empty file: %v", err)
	}
	if seen != 0 {
		t.Fatalf("expected no lines from empty file, got %d", seen)
	}

	missing := filepath.Join(tmp, "missing.jsonl")
	err := ReadJSONLines(missing, func(_ int, _ []byte) error { return nil })
	if err == nil {
		t.Fatal("expected os.ErrNotExist")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}
