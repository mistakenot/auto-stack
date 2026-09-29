package linkscan

import (
	"os"
	"reflect"
	"testing"

	"github.com/datadyne-io/autodoc/internal/testutil"
)

func newFolderWorkspace(t *testing.T) *testutil.Workspace {
	t.Helper()
	ws := testutil.NewWorkspace(t)
	ws.WriteSourceFile("pkg/cache/.autodoc", "# cache links\n[autodoc(deadbeef@cafebabe, 01234567)]\n\n[autodoc(bad)]\n")
	ws.WriteSourceFile("pkg/cache/lru.go", "package cache\n\nfunc get() {}\n")
	ws.WriteSourceFile("pkg/cache/ttl.go", "package cache\n\nfunc ttl() {}\n")
	ws.WriteSourceFile("pkg/cache/lru_test.go", "package cache\n")
	ws.WriteSourceFile("pkg/cache/README.md", "# notes\n")
	ws.WriteSourceFile("pkg/cache/inner/shard.go", "package inner\n")
	ws.WriteSourceFile("pkg/cache/inner/.autodoc", "[autodoc(deadbeef@cafebabe, 01234567)]\n")
	ws.WriteSourceFile("pkg/other/other.go", "package other\n")
	ws.InitGitRepo()
	return ws
}

func TestScanFilesParsesFolderLinkFile(t *testing.T) {
	ws := newFolderWorkspace(t)

	result, err := ScanFiles(ws.Dir)
	if err != nil {
		t.Fatalf("ScanFiles: %v", err)
	}

	var folderTags []Tag
	for _, tag := range result.Tags {
		if tag.ScopeKind == ScopeKindFolder {
			folderTags = append(folderTags, tag)
		}
	}
	if len(folderTags) != 2 {
		t.Fatalf("folder tags = %d, want 2 (outer + inner): %+v", len(folderTags), result.Tags)
	}
	outer := folderTags[1]
	if outer.FilePath != ws.Path("pkg/cache/.autodoc") {
		outer = folderTags[0]
	}
	if outer.Line != 2 || outer.DocId != "deadbeef" || outer.ScopeHash != "01234567" {
		t.Fatalf("outer tag = %+v", outer)
	}

	// The comment and blank line are skipped; the junk tag line is malformed.
	if len(result.Malformed) != 1 || result.Malformed[0].Line != 4 {
		t.Fatalf("malformed = %+v, want one at line 4", result.Malformed)
	}
}

func TestFolderScopeFilesIsRecursiveAndFiltered(t *testing.T) {
	ws := newFolderWorkspace(t)
	ws.WriteSourceFile("pkg/cache/untracked.go", "package cache\n") // untracked but not ignored

	files, err := FolderScopeFiles(ws.Path("pkg/cache"))
	if err != nil {
		t.Fatalf("FolderScopeFiles: %v", err)
	}
	want := []string{"inner/shard.go", "lru.go", "ttl.go", "untracked.go"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("files = %v, want %v", files, want)
	}
}

func TestFolderScopeHashChanges(t *testing.T) {
	ws := newFolderWorkspace(t)
	dir := ws.Path("pkg/cache")

	base, err := ComputeFolderScopeHash(dir)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	same := func(label string, wantSame bool) {
		t.Helper()
		got, err := ComputeFolderScopeHash(dir)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if (got == base) != wantSame {
			t.Fatalf("%s: hash %s vs base %s, wantSame=%v", label, got, base, wantSame)
		}
		base = got
	}

	ws.WriteSourceFile("pkg/cache/README.md", "# changed notes\n")
	same("markdown edit is ignored", true)

	ws.WriteSourceFile("pkg/cache/lru_test.go", "package cache\n\n// more\n")
	same("test file edit is ignored", true)

	ws.WriteSourceFile("pkg/cache/.autodoc", "[autodoc(deadbeef@11111111, 22222222)]\n")
	ws.WriteSourceFile("pkg/cache/inner/.autodoc", "[autodoc(deadbeef@33333333, 44444444)]\n")
	same("link file edits are ignored", true)

	ws.WriteSourceFile("pkg/cache/lru.go", "package cache\n\n// [autodoc(deadbeef@cafebabe, 01234567)]\nfunc get() {}\n")
	same("adding a comment line changes hash", false) // only the tag text is stripped
	ws.WriteSourceFile("pkg/cache/lru.go", "package cache\n\n// [autodoc(deadbeef@99999999, 88888888)]\nfunc get() {}\n")
	same("inline tag refresh is ignored", true)

	ws.WriteSourceFile("pkg/other/other.go", "package other\n\nfunc x() {}\n")
	same("sibling folder edit is ignored", true)

	ws.WriteSourceFile("pkg/cache/inner/shard.go", "package inner\n\nfunc shard() {}\n")
	same("nested file edit changes hash", false)

	ws.WriteSourceFile("pkg/cache/new.go", "package cache\n")
	same("new file changes hash", false)

	if err := os.Rename(ws.Path("pkg/cache/new.go"), ws.Path("pkg/cache/renamed.go")); err != nil {
		t.Fatal(err)
	}
	same("rename changes hash", false)

	if err := os.Remove(ws.Path("pkg/cache/renamed.go")); err != nil {
		t.Fatal(err)
	}
	same("delete changes hash", false)
}

func TestFolderScopeHashIgnoresCRLF(t *testing.T) {
	ws := newFolderWorkspace(t)
	dir := ws.Path("pkg/cache")
	base, err := ComputeFolderScopeHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	ws.WriteSourceFile("pkg/cache/lru.go", "package cache\r\n\r\nfunc get() {}\r\n")
	got, err := ComputeFolderScopeHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != base {
		t.Fatalf("CRLF checkout changed hash: %s vs %s", got, base)
	}
}
