package commands

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/frontmatter"
	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestCheckStaleCorrectHash(t *testing.T) {
	body := "\n# Test\n"
	doc := frontmatter.Doc{Title: "Test", Summary: "A test", Body: body}
	hash := frontmatter.ComputeHash(&doc)

	entries := []doctree.Entry{
		{RelPath: "test.md", Title: "Test", Summary: "A test", ReadWhen: "testing", Hash: hash, Body: body},
	}

	result := CheckStale(entries)
	if result.HasStale {
		t.Error("expected no stale files")
	}
}

func TestCheckStaleWrongHash(t *testing.T) {
	entries := []doctree.Entry{
		{RelPath: "test.md", Title: "Test", Summary: "A test", Hash: "wronghsh"},
	}

	result := CheckStale(entries)
	if !result.HasStale {
		t.Error("expected stale files")
	}
	if len(result.StaleFiles) != 1 {
		t.Errorf("got %d stale files, want 1", len(result.StaleFiles))
	}
}

func TestCheckStaleMissingFrontmatter(t *testing.T) {
	entries := []doctree.Entry{
		{RelPath: "test.md", Title: "", Summary: "", Hash: ""},
	}

	result := CheckStale(entries)
	if !result.HasStale {
		t.Error("expected stale for missing frontmatter")
	}
}

func TestStaleThenFixedRemovesFromStale(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	// Write a raw file with a wrong hash — should show as stale.
	ws.WriteFile("docs/guide.md", `---
title: "User Guide"
summary: "Old summary"
read_when: "updating the getting started guide"
hash: "wronghsh"
---

# Getting Started

Welcome to the guide.
`)

	docPath := ws.Path("docs/guide.md")
	docsPath := ws.Path("docs")

	// Step 1: Verify it's stale.
	entries, err := doctree.Walk(docsPath)
	if err != nil {
		t.Fatal(err)
	}
	result := CheckStale(entries)
	if !result.HasStale {
		t.Fatal("expected stale files before fix")
	}

	// Step 2: Edit the summary (simulating agent work), then run Fixed.
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "Old summary", "Step-by-step setup instructions", 1)
	if err := os.WriteFile(docPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Fixed(docPath, "", ""); err != nil {
		t.Fatalf("Fixed: %v", err)
	}

	// Step 3: Re-walk and check — should no longer be stale.
	entries, err = doctree.Walk(docsPath)
	if err != nil {
		t.Fatal(err)
	}
	result = CheckStale(entries)
	if result.HasStale {
		e := entries[0]
		expected := frontmatter.ComputeHash(&frontmatter.Doc{
			Title: e.Title, Summary: e.Summary, Body: e.Body,
		})
		t.Errorf("file should not be stale after Fixed.\nhash=%q expected=%q\ntitle=%q summary=%q\nbody=%q",
			e.Hash, expected, e.Title, e.Summary, e.Body)
	}
}

func TestStaleOutputShowsStale(t *testing.T) {
	ws := testutil.NewWorkspace(t)

	// Write one file with correct hash and one with wrong hash
	doc := frontmatter.Doc{Title: "Good", Summary: "A good doc", Body: "\n# Good\n"}
	goodHash := frontmatter.ComputeHash(&doc)
	ws.WriteFile("docs/good.md", fmt.Sprintf("---\ntitle: \"Good\"\nsummary: \"A good doc\"\nread_when: \"testing\"\nhash: %q\n---\n\n# Good\n", goodHash))
	ws.WriteDocWithHash("bad.md", "Bad", "A bad doc", "wronghsh", "# Bad")

	entries, err := doctree.Walk(ws.Path("docs"))
	if err != nil {
		t.Fatal(err)
	}

	result := CheckStale(entries)
	var buf bytes.Buffer
	StaleOutput(&buf, entries, result.StaleFiles, "docs")
	output := buf.String()

	if !strings.Contains(output, "Stale") {
		t.Error("stale output missing 'Stale' marker")
	}
	if !strings.Contains(output, "A good doc") {
		t.Error("stale output missing good doc summary")
	}
}

func TestStaleFlagsMissingReadWhen(t *testing.T) {
	body := "\n# Test\n"
	doc := frontmatter.Doc{Title: "Test", Summary: "A test", Body: body}
	hash := frontmatter.ComputeHash(&doc)

	entries := []doctree.Entry{
		{RelPath: "test.md", Title: "Test", Summary: "A test", Hash: hash, Body: body},
	}

	result := CheckStale(entries)
	if !result.HasStale {
		t.Error("expected stale for missing read_when")
	}
}

func TestStaleOutputUsesRepoRelativePathsForMultiRoot(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteFile("auto-etl/docs/bad.md", `---
title: "Bad"
summary: "Needs refresh"
hash: "wronghsh"
---

# Bad
`)

	entries, err := doctree.WalkRepo(ws.Dir, "docs")
	if err != nil {
		t.Fatal(err)
	}

	result := CheckStale(entries)
	var buf bytes.Buffer
	StaleOutput(&buf, entries, result.StaleFiles, ".")
	output := buf.String()

	if !strings.Contains(output, "auto-etl/") || !strings.Contains(output, "docs/") {
		t.Fatalf("expected repo-relative directory segments in output:\n%s", output)
	}
	if !strings.Contains(output, "bad.md") {
		t.Fatalf("expected bad.md in output:\n%s", output)
	}
	if !strings.Contains(output, "Stale") {
		t.Fatalf("expected stale marker in output:\n%s", output)
	}
}
