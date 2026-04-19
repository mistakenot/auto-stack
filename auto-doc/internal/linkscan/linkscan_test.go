package linkscan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestScanFilesFindsTagsAndMalformed(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteSourceFile("pkg/cache/lru.go", strings.TrimLeft(`
package cache

func get() {
    // [autodoc(deadbeef@cafebabe, 01234567)]
    value := 1
    _ = value
}

func bad() {
    // [autodoc(deadbeef@bad, short)]
}
`, "\n"))
	ws.InitGitRepo()

	result, err := ScanFiles(ws.Dir)
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}

	if len(result.Tags) != 1 {
		t.Fatalf("len(result.Tags) = %d, want 1", len(result.Tags))
	}
	tag := result.Tags[0]
	if tag.DocId != "deadbeef" {
		t.Fatalf("tag.DocId = %q, want deadbeef", tag.DocId)
	}
	if tag.DocHash != "cafebabe" {
		t.Fatalf("tag.DocHash = %q, want cafebabe", tag.DocHash)
	}
	if tag.ScopeHash != "01234567" {
		t.Fatalf("tag.ScopeHash = %q, want 01234567", tag.ScopeHash)
	}
	if tag.Line != 4 {
		t.Fatalf("tag.Line = %d, want 4", tag.Line)
	}
	if !filepath.IsAbs(tag.FilePath) {
		t.Fatalf("tag.FilePath should be absolute, got %q", tag.FilePath)
	}

	if len(result.Malformed) != 1 {
		t.Fatalf("len(result.Malformed) = %d, want 1", len(result.Malformed))
	}
	if result.Malformed[0].Line != 10 {
		t.Fatalf("malformed line = %d, want 10", result.Malformed[0].Line)
	}
}

func TestScanFilesSkipsDataFileExtensions(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	// Content that would otherwise match the malformed detector.
	junk := `{"description":"see [autodoc(abcdefgh@deadbeef, 12345678)] for context"}` + "\n"
	ws.WriteSourceFile(".beads/issues.jsonl", junk)
	ws.WriteSourceFile("data/records.ndjson", junk)
	ws.WriteSourceFile("config.json", junk)
	ws.InitGitRepo()

	result, err := ScanFiles(ws.Dir)
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	if len(result.Tags) != 0 {
		t.Fatalf("len(result.Tags) = %d, want 0 (data files should be skipped)", len(result.Tags))
	}
	if len(result.Malformed) != 0 {
		t.Fatalf("len(result.Malformed) = %d, want 0 (data files should be skipped)", len(result.Malformed))
	}
}

func TestScanFilesOnlyTrackedFiles(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteSourceFile("tracked.go", "// [autodoc(deadbeef@cafebabe, 01234567)]\n")
	ws.InitGitRepo()
	ws.WriteSourceFile("untracked.go", "// [autodoc(deadbeef@cafebabe, 89abcdef)]\n")

	result, err := ScanFiles(ws.Dir)
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}
	if len(result.Tags) != 1 {
		t.Fatalf("len(result.Tags) = %d, want 1", len(result.Tags))
	}
	if !strings.HasSuffix(filepath.ToSlash(result.Tags[0].FilePath), "/tracked.go") {
		t.Fatalf("unexpected tracked file path: %q", result.Tags[0].FilePath)
	}
}

func TestScanFilesSkipsDeletedTrackedFiles(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	path := ws.WriteSourceFile("docs/old.md", "# old\n")
	ws.InitGitRepo()

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove tracked file: %v", err)
	}

	if _, err := ScanFiles(ws.Dir); err != nil {
		t.Fatalf("ScanFiles should skip deleted tracked files: %v", err)
	}
}

func TestComputeScopeHashFromContentStopsOnShallowerIndent(t *testing.T) {
	content := strings.TrimLeft(`
func demo() {
    // [autodoc(deadbeef@cafebabe, 00000000)]
    a := 1

    b := 2
}

func outside() {}
`, "\n")

	hash1, err := ComputeScopeHashFromContent(content, 2)
	if err != nil {
		t.Fatalf("ComputeScopeHashFromContent: %v", err)
	}

	changedTag := strings.ReplaceAll(content, "00000000", "ffffffff")
	hash2, err := ComputeScopeHashFromContent(changedTag, 2)
	if err != nil {
		t.Fatalf("ComputeScopeHashFromContent: %v", err)
	}

	if hash1 != hash2 {
		t.Fatalf("tag hash text should not affect scope hash: %q != %q", hash1, hash2)
	}
}

func TestComputeScopeHashFromContentColumnZeroExtendsToEOF(t *testing.T) {
	content := strings.TrimLeft(`
// [autodoc(deadbeef@cafebabe, 00000000)]
line one
line two
`, "\n")
	h1, err := ComputeScopeHashFromContent(content, 1)
	if err != nil {
		t.Fatalf("ComputeScopeHashFromContent: %v", err)
	}

	changed := strings.Replace(content, "line two", "line two changed", 1)
	h2, err := ComputeScopeHashFromContent(changed, 1)
	if err != nil {
		t.Fatalf("ComputeScopeHashFromContent: %v", err)
	}

	if h1 == h2 {
		t.Fatal("expected different hash when tail content changes")
	}
}

func TestComputeScopeHashFromContentInvalidTagLine(t *testing.T) {
	if _, err := ComputeScopeHashFromContent("one\ntwo\n", 4); err == nil {
		t.Fatal("expected out-of-range error")
	}
}
