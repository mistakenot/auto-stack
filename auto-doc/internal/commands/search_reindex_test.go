package commands

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/search"
	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestSearchReindexIndexesAllDocs(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("getting-started.md", "Getting Started", "Setup instructions", "# Getting Started\n\nHow to get started.")
	ws.WriteDoc("architecture.md", "Architecture", "System design overview", "# Architecture\n\nThe system design.")
	ws.WriteDoc("api/auth.md", "Authentication", "Auth API reference", "# Authentication\n\nHow to authenticate.")

	if err := os.MkdirAll(ws.Path(".auto/doc"), 0o755); err != nil {
		t.Fatalf("mkdir .auto/doc: %v", err)
	}

	var buf bytes.Buffer
	err := SearchReindex(&buf, ws.Path(".auto/doc/index"), ws.Dir, "docs", nil)
	if err != nil {
		t.Fatalf("SearchReindex failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Indexed 3 files") {
		t.Errorf("expected 'Indexed 3 files', got: %q", output)
	}
}

func TestSearchReindexEmptyDocsDir(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	if err := os.MkdirAll(ws.Path("docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.MkdirAll(ws.Path(".auto/doc"), 0o755); err != nil {
		t.Fatalf("mkdir .auto/doc: %v", err)
	}

	var buf bytes.Buffer
	err := SearchReindex(&buf, ws.Path(".auto/doc/index"), ws.Dir, "docs", nil)
	if err != nil {
		t.Fatalf("SearchReindex failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Indexed 0 files") {
		t.Errorf("expected 'Indexed 0 files', got: %q", output)
	}
}

func TestSearchReindexCanSearch(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("getting-started.md", "Getting Started", "Setup instructions", "# Getting Started\n\nHow to get started with the project.")
	ws.WriteDoc("architecture.md", "Architecture", "System design overview", "# Architecture\n\nThe architecture of the system.")

	if err := os.MkdirAll(ws.Path(".auto/doc"), 0o755); err != nil {
		t.Fatalf("mkdir .auto/doc: %v", err)
	}

	indexPath := ws.Path(".auto/doc/index")

	var buf bytes.Buffer
	if err := SearchReindex(&buf, indexPath, ws.Dir, "docs", nil); err != nil {
		t.Fatalf("SearchReindex failed: %v", err)
	}

	idx, err := search.OpenIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenIndex failed: %v", err)
	}
	defer idx.Close()

	results, err := idx.Search("architecture system", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected search results, got none")
	}

	found := false
	for _, r := range results {
		if strings.Contains(r.Path, "architecture.md") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("architecture.md not found in search results: %+v", results)
	}
}

func TestSearchReindexNoDuplicates(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("getting-started.md", "Getting Started", "Setup instructions", "# Getting Started\n\nHow to get started.")
	ws.WriteDoc("architecture.md", "Architecture", "System design overview", "# Architecture\n\nSystem architecture.")

	if err := os.MkdirAll(ws.Path(".auto/doc"), 0o755); err != nil {
		t.Fatalf("mkdir .auto/doc: %v", err)
	}

	indexPath := ws.Path(".auto/doc/index")

	// Run reindex twice
	for i := range 2 {
		var buf bytes.Buffer
		if err := SearchReindex(&buf, indexPath, ws.Dir, "docs", nil); err != nil {
			t.Fatalf("SearchReindex run %d failed: %v", i+1, err)
		}
	}

	idx, err := search.OpenIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenIndex failed: %v", err)
	}
	defer idx.Close()

	results, err := idx.Search("getting started", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Count occurrences of getting-started.md in results
	count := 0
	for _, r := range results {
		if strings.Contains(r.Path, "getting-started.md") {
			count++
		}
	}

	if count > 1 {
		t.Errorf("expected getting-started.md to appear at most once, got %d occurrences", count)
	}
	if count == 0 {
		t.Error("expected getting-started.md in results, got none")
	}
}

func TestSearchReindexIndexesMultipleDocsRoots(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("getting-started.md", "Getting Started", "Setup instructions", "# Getting Started\n\nHow to get started.")
	ws.WriteFile("auto-etl/docs/reference.md", `---
title: "Reference"
summary: "ETL reference"
hash: ""
---

# Reference

multiroottoken
`)

	if err := os.MkdirAll(ws.Path(".auto/doc"), 0o755); err != nil {
		t.Fatalf("mkdir .auto/doc: %v", err)
	}
	indexPath := ws.Path(".auto/doc/index")

	var buf bytes.Buffer
	if err := SearchReindex(&buf, indexPath, ws.Dir, "docs", nil); err != nil {
		t.Fatalf("SearchReindex failed: %v", err)
	}

	idx, err := search.OpenIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenIndex failed: %v", err)
	}
	defer idx.Close()

	results, err := idx.Search("multiroottoken", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	found := false
	for _, r := range results {
		if r.Path == "auto-etl/docs/reference.md" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected auto-etl/docs/reference.md in results, got: %+v", results)
	}
}

func TestSearchReindexRemovesStaleDocuments(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("alpha.md", "Alpha", "Alpha docs", "# Alpha\n\nalphatoken")
	staleDocPath := ws.WriteDoc("beta.md", "Beta", "Beta docs", "# Beta\n\nbetatoken")

	if err := os.MkdirAll(ws.Path(".auto/doc"), 0o755); err != nil {
		t.Fatalf("mkdir .auto/doc: %v", err)
	}
	indexPath := ws.Path(".auto/doc/index")

	var buf bytes.Buffer
	if err := SearchReindex(&buf, indexPath, ws.Dir, "docs", nil); err != nil {
		t.Fatalf("SearchReindex initial failed: %v", err)
	}

	if err := os.Remove(staleDocPath); err != nil {
		t.Fatalf("remove stale doc: %v", err)
	}
	buf.Reset()
	if err := SearchReindex(&buf, indexPath, ws.Dir, "docs", nil); err != nil {
		t.Fatalf("SearchReindex second failed: %v", err)
	}

	idx, err := search.OpenIndex(indexPath)
	if err != nil {
		t.Fatalf("OpenIndex failed: %v", err)
	}
	defer idx.Close()

	results, err := idx.Search("betatoken", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	for _, r := range results {
		if r.Path == "docs/beta.md" {
			t.Fatalf("stale doc still present in index results: %+v", results)
		}
	}
}
