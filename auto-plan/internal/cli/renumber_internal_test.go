package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommitRewritesReportsRestore: when a later rename fails, the files
// already replaced are restored and the error says so.
func TestCommitRewritesReportsRestore(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.json")
	if err := os.WriteFile(first, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The second target is a non-empty directory, so renaming over it fails.
	blocked := filepath.Join(dir, "b.json")
	if err := os.MkdirAll(filepath.Join(blocked, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := commitRewrites([]*rewrite{
		{path: first, orig: []byte("old"), data: []byte("new")},
		{path: blocked, orig: nil, data: []byte("new")},
	}, dir, dir)
	if err == nil || !strings.Contains(err.Error(), "the 1 file(s) already replaced were restored") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(first); string(got) != "old" {
		t.Fatalf("first file = %q, want restored", got)
	}
}
