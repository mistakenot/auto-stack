package commands

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/linkcheck"
	"github.com/datadyne-io/autodoc/internal/linkscan"
)

func TestTreeOutputJSON(t *testing.T) {
	entries := []doctree.Entry{
		{RelPath: "getting-started.md", RepoRelPath: "docs/getting-started.md", Id: "abc12345", Title: "Getting Started", Summary: "Setup guide", Hash: "deadbeef"},
		{RelPath: "api/auth.md", RepoRelPath: "docs/api/auth.md", Title: "Auth", Summary: "Auth API", Hash: "cafebabe"},
	}

	var buf bytes.Buffer
	if err := TreeOutputJSON(&buf, entries); err != nil {
		t.Fatal(err)
	}

	var docs []DocJSON
	if err := json.Unmarshal(buf.Bytes(), &docs); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("len = %d, want 2", len(docs))
	}
	if docs[0].Path != "docs/getting-started.md" {
		t.Errorf("path = %q, want docs/getting-started.md", docs[0].Path)
	}
	if docs[0].ID != "abc12345" {
		t.Errorf("id = %q, want abc12345", docs[0].ID)
	}
}

func TestTreeOutputJSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := TreeOutputJSON(&buf, nil); err != nil {
		t.Fatal(err)
	}
	var docs []DocJSON
	if err := json.Unmarshal(buf.Bytes(), &docs); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("len = %d, want 0", len(docs))
	}
}

func TestStaleOutputJSON(t *testing.T) {
	stale := []doctree.Entry{
		{RelPath: "stale.md", RepoRelPath: "docs/stale.md", Title: "Stale", Summary: "", Hash: "wrong", Body: "content"},
	}

	var buf bytes.Buffer
	if err := StaleOutputJSON(&buf, stale); err != nil {
		t.Fatal(err)
	}

	var docs []StaleDocJSON
	if err := json.Unmarshal(buf.Bytes(), &docs); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("len = %d, want 1", len(docs))
	}
	if docs[0].Path != "docs/stale.md" {
		t.Errorf("path = %q", docs[0].Path)
	}
	// Should have both missing_frontmatter (empty summary) and stale_hash
	found := map[string]bool{}
	for _, iss := range docs[0].Issues {
		found[iss] = true
	}
	if !found["missing_frontmatter"] {
		t.Error("missing missing_frontmatter issue")
	}
	if !found["stale_hash"] {
		t.Error("missing stale_hash issue")
	}
}

func TestFixOutputJSON(t *testing.T) {
	docIssues := []docIssue{
		{RepoRelPath: "docs/a.md", MissingFM: true},
		{RepoRelPath: "docs/b.md", StaleHash: true, DefaultTitle: true},
	}
	linkIssues := []linkcheck.LinkIssue{
		{
			Status: linkcheck.OrphanedTag,
			Tag:    linkscan.Tag{FilePath: "src/main.go", Line: 10, DocId: "deadbeef", DocHash: "cafebabe", ScopeHash: "12345678"},
		},
	}

	var buf bytes.Buffer
	if err := FixOutputJSON(&buf, docIssues, linkIssues); err != nil {
		t.Fatal(err)
	}

	var issues []FixIssueJSON
	if err := json.Unmarshal(buf.Bytes(), &issues); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// 1 missing_fm + 1 stale_hash + 1 default_title + 1 orphaned_tag = 4
	if len(issues) != 4 {
		t.Fatalf("len = %d, want 4, got %+v", len(issues), issues)
	}

	types := map[string]int{}
	for _, iss := range issues {
		types[iss.Type]++
	}
	if types["missing_frontmatter"] != 1 {
		t.Error("expected 1 missing_frontmatter")
	}
	if types["orphaned_tag"] != 1 {
		t.Error("expected 1 orphaned_tag")
	}
}

func TestTreeOutputJSONIncludesReadWhen(t *testing.T) {
	entries := []doctree.Entry{
		{RelPath: "guide.md", RepoRelPath: "docs/guide.md", Title: "Guide", Summary: "A guide", ReadWhen: "when onboarding", Hash: "12345678"},
	}

	var buf bytes.Buffer
	if err := TreeOutputJSON(&buf, entries); err != nil {
		t.Fatal(err)
	}

	var docs []DocJSON
	if err := json.Unmarshal(buf.Bytes(), &docs); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if docs[0].ReadWhen != "when onboarding" {
		t.Errorf("read_when = %q, want %q", docs[0].ReadWhen, "when onboarding")
	}
}

func TestFixOutputJSONIncludesEmptyReadWhen(t *testing.T) {
	docIssues := []docIssue{
		{RepoRelPath: "docs/a.md", EmptyReadWhen: true},
	}

	var buf bytes.Buffer
	if err := FixOutputJSON(&buf, docIssues, nil); err != nil {
		t.Fatal(err)
	}

	var issues []FixIssueJSON
	if err := json.Unmarshal(buf.Bytes(), &issues); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(issues) != 1 || issues[0].Type != "empty_read_when" {
		t.Fatalf("expected empty_read_when issue, got %+v", issues)
	}
}

func TestFixedResultJSONOutput(t *testing.T) {
	result := FixedResultJSON{Path: "docs/test.md", OldHash: "aabbccdd", NewHash: "11223344"}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, result); err != nil {
		t.Fatal(err)
	}

	var parsed FixedResultJSON
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed.OldHash != "aabbccdd" || parsed.NewHash != "11223344" {
		t.Errorf("hashes = %q/%q, want aabbccdd/11223344", parsed.OldHash, parsed.NewHash)
	}
}
