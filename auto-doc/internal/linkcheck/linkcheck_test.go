package linkcheck

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/frontmatter"
	"github.com/datadyne-io/autodoc/internal/linkscan"
	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestCheckDocHashMismatch(t *testing.T) {
	tc := setupCheckCase(t)
	tag := tc.tag
	tag.DocHash = "ffffffff"

	issues, err := Check([]linkscan.Tag{tag}, []doctree.Entry{tc.doc})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}
	if issues[0].Status != DocHashMismatch {
		t.Fatalf("status = %v, want %v", issues[0].Status, DocHashMismatch)
	}
	if issues[0].CurrentDocHash != tc.doc.Hash {
		t.Fatalf("CurrentDocHash = %q, want %q", issues[0].CurrentDocHash, tc.doc.Hash)
	}
	if issues[0].DocFile != "docs/caching.md" {
		t.Fatalf("DocFile = %q, want %q", issues[0].DocFile, "docs/caching.md")
	}
}

func TestCheckScopeHashMismatch(t *testing.T) {
	tc := setupCheckCase(t)
	tag := tc.tag
	tag.ScopeHash = "ffffffff"

	issues, err := Check([]linkscan.Tag{tag}, []doctree.Entry{tc.doc})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}
	if issues[0].Status != ScopeHashMismatch {
		t.Fatalf("status = %v, want %v", issues[0].Status, ScopeHashMismatch)
	}
	if issues[0].CurrentScopeHash == tag.ScopeHash {
		t.Fatal("CurrentScopeHash should differ from stale tag scope hash")
	}
}

func TestCheckBothMismatch(t *testing.T) {
	tc := setupCheckCase(t)
	tag := tc.tag
	tag.DocHash = "ffffffff"
	tag.ScopeHash = "00000000"

	issues, err := Check([]linkscan.Tag{tag}, []doctree.Entry{tc.doc})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}
	if issues[0].Status != BothMismatch {
		t.Fatalf("status = %v, want %v", issues[0].Status, BothMismatch)
	}
}

func TestCheckOrphanedTag(t *testing.T) {
	tc := setupCheckCase(t)
	tag := tc.tag
	tag.DocId = "cafecafe"

	issues, err := Check([]linkscan.Tag{tag}, []doctree.Entry{tc.doc})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}
	if issues[0].Status != OrphanedTag {
		t.Fatalf("status = %v, want %v", issues[0].Status, OrphanedTag)
	}
}

func TestCheckSkipsLinkOK(t *testing.T) {
	tc := setupCheckCase(t)

	issues, err := Check([]linkscan.Tag{tc.tag}, []doctree.Entry{tc.doc})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("len(issues) = %d, want 0", len(issues))
	}
}

func TestCheckSelfReferencingTag(t *testing.T) {
	tc := setupCheckCase(t)
	tc.doc.AbsPath = tc.tag.FilePath

	issues, err := Check([]linkscan.Tag{tc.tag}, []doctree.Entry{tc.doc})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}
	if issues[0].Status != SelfReferencingTag {
		t.Fatalf("status = %v, want %v", issues[0].Status, SelfReferencingTag)
	}
}

func TestIssuesFromMalformed(t *testing.T) {
	issues := IssuesFromMalformed([]linkscan.MalformedTag{{
		FilePath: "/tmp/example.go",
		Line:     12,
		RawText:  "// [autodoc(bad)]",
	}})
	if len(issues) != 1 {
		t.Fatalf("len(issues) = %d, want 1", len(issues))
	}
	if issues[0].Status != MalformedTag {
		t.Fatalf("status = %v, want %v", issues[0].Status, MalformedTag)
	}
	if issues[0].Tag.RawTag != "// [autodoc(bad)]" {
		t.Fatalf("raw tag = %q", issues[0].Tag.RawTag)
	}
}

type checkCase struct {
	doc doctree.Entry
	tag linkscan.Tag
}

func setupCheckCase(t *testing.T) checkCase {
	t.Helper()
	ws := testutil.NewWorkspace(t)
	file := ws.WriteSourceFile("pkg/cache/lru.go", strings.TrimLeft(`
package cache

func read() {
    // [autodoc(deadbeef@00000000, 00000000)]
    value := 1
    _ = value
}
`, "\n"))

	doc := frontmatter.Doc{
		Id:      "deadbeef",
		Title:   "Cache",
		Summary: "Cache behavior",
		Body:    "\n# Cache\n\nDetails.\n",
	}
	doc.Hash = frontmatter.ComputeHash(&doc)
	scopeHash, err := linkscan.ComputeScopeHash(file, 4)
	if err != nil {
		t.Fatalf("ComputeScopeHash: %v", err)
	}

	entry := doctree.Entry{
		RelPath:     "caching.md",
		RepoRelPath: "docs/caching.md",
		Id:          doc.Id,
		Title:       doc.Title,
		Summary:     doc.Summary,
		Hash:        doc.Hash,
		Body:        doc.Body,
	}
	tag := linkscan.Tag{
		FilePath:  filepath.Clean(file),
		Line:      4,
		DocId:     doc.Id,
		DocHash:   doc.Hash,
		ScopeHash: scopeHash,
		RawTag:    "[autodoc(deadbeef@00000000, 00000000)]",
	}
	return checkCase{doc: entry, tag: tag}
}
