package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/doctree"
	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestDocsIndexBasic(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("getting-started.md", "Getting Started", "Setup instructions for new users", "# Getting Started")
	ws.WriteDoc("architecture.md", "Architecture", "Overview of system design", "# Architecture")

	entries, err := doctree.Walk(ws.Path("docs"))
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	DocsIndex(&buf, entries, "docs")
	output := buf.String()

	// Should have markdown links with relative paths from project root
	if !strings.Contains(output, "[Architecture](docs/architecture.md): Overview of system design") {
		t.Errorf("missing or wrong architecture entry, got:\n%s", output)
	}
	if !strings.Contains(output, "[Getting Started](docs/getting-started.md): Setup instructions for new users") {
		t.Errorf("missing or wrong getting-started entry, got:\n%s", output)
	}
}

func TestDocsIndexGroupedByDir(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("readme.md", "Readme", "Project readme", "# Readme")
	ws.WriteDoc("api/auth.md", "Authentication", "Auth docs", "# Auth")
	ws.WriteDoc("api/endpoints.md", "API Endpoints", "Endpoint list", "# Endpoints")
	ws.WriteDoc("guides/setup.md", "Setup Guide", "How to set up", "# Setup")

	entries, err := doctree.Walk(ws.Path("docs"))
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	DocsIndex(&buf, entries, "docs")
	output := buf.String()

	// Subdirectory groups should have bold headers
	if !strings.Contains(output, "**docs/api**") {
		t.Errorf("missing api group header, got:\n%s", output)
	}
	if !strings.Contains(output, "**docs/guides**") {
		t.Errorf("missing guides group header, got:\n%s", output)
	}

	// api group should come before guides (sorted)
	apiIdx := strings.Index(output, "**docs/api**")
	guidesIdx := strings.Index(output, "**docs/guides**")
	if apiIdx > guidesIdx {
		t.Error("api should come before guides")
	}
}

func TestDocsIndexNestedDirs(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("concepts/frontend/react.md", "React Patterns", "React best practices", "# React")
	ws.WriteDoc("concepts/frontend/css.md", "CSS Guide", "CSS conventions", "# CSS")
	ws.WriteDoc("concepts/backend.md", "Backend", "Backend overview", "# Backend")

	entries, err := doctree.Walk(ws.Path("docs"))
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	DocsIndex(&buf, entries, "docs")
	output := buf.String()

	if !strings.Contains(output, "**docs/concepts**") {
		t.Errorf("missing concepts header, got:\n%s", output)
	}
	if !strings.Contains(output, "**docs/concepts/frontend**") {
		t.Errorf("missing concepts/frontend header, got:\n%s", output)
	}

	// Files sorted by name within group
	cssIdx := strings.Index(output, "CSS Guide")
	reactIdx := strings.Index(output, "React Patterns")
	if cssIdx > reactIdx {
		t.Error("css should come before react (sorted by filename)")
	}
}

func TestDocsIndexEmpty(t *testing.T) {
	var buf bytes.Buffer
	DocsIndex(&buf, nil, "docs")
	if buf.Len() != 0 {
		t.Errorf("expected empty output, got: %q", buf.String())
	}
}

func TestDocsIndexMissingSummary(t *testing.T) {
	entries := []doctree.Entry{
		{RelPath: "bare.md", RepoRelPath: "docs/bare.md", Title: "Bare File", Summary: ""},
	}

	var buf bytes.Buffer
	DocsIndex(&buf, entries, "docs")
	output := buf.String()

	if !strings.Contains(output, "[Bare File](docs/bare.md)") {
		t.Errorf("wrong format for missing summary, got:\n%s", output)
	}
	// Should NOT have a trailing colon with no summary
	if strings.Contains(output, "bare.md):") {
		t.Error("should not have colon when summary is empty")
	}
}

func TestDocsIndexExcludesTags(t *testing.T) {
	entries := []doctree.Entry{
		{RelPath: "getting-started.md", RepoRelPath: "docs/getting-started.md", Title: "Getting Started", Summary: "Setup instructions"},
		{RelPath: "old-notes.md", RepoRelPath: "docs/old-notes.md", Title: "Old Notes", Summary: "Archived notes", Tags: []string{"archive"}},
		{RelPath: "reference.md", RepoRelPath: "docs/reference.md", Title: "Reference", Summary: "API reference", Tags: []string{"reference"}},
	}

	var buf bytes.Buffer
	DocsIndex(&buf, entries, "docs", "archive")
	output := buf.String()

	if !strings.Contains(output, "Getting Started") {
		t.Error("missing non-excluded entry")
	}
	if strings.Contains(output, "Old Notes") {
		t.Error("excluded entry should not appear")
	}
	if !strings.Contains(output, "Reference") {
		t.Error("non-excluded tagged entry should appear")
	}
}

func TestDocsIndexExcludesMultipleTags(t *testing.T) {
	entries := []doctree.Entry{
		{RelPath: "keep.md", RepoRelPath: "docs/keep.md", Title: "Keep", Summary: "Keep this"},
		{RelPath: "archive.md", RepoRelPath: "docs/archive.md", Title: "Archive", Summary: "Archived", Tags: []string{"archive"}},
		{RelPath: "draft.md", RepoRelPath: "docs/draft.md", Title: "Draft", Summary: "Draft doc", Tags: []string{"draft"}},
	}

	var buf bytes.Buffer
	DocsIndex(&buf, entries, "docs", "archive", "draft")
	output := buf.String()

	if !strings.Contains(output, "Keep") {
		t.Error("missing non-excluded entry")
	}
	if strings.Contains(output, "Archive") {
		t.Error("archive entry should be excluded")
	}
	if strings.Contains(output, "Draft") {
		t.Error("draft entry should be excluded")
	}
}

func TestDocsIndexNoExcludedTagsShowsAll(t *testing.T) {
	entries := []doctree.Entry{
		{RelPath: "a.md", RepoRelPath: "docs/a.md", Title: "A", Summary: "Doc A", Tags: []string{"archive"}},
		{RelPath: "b.md", RepoRelPath: "docs/b.md", Title: "B", Summary: "Doc B"},
	}

	var buf bytes.Buffer
	DocsIndex(&buf, entries, "docs")
	output := buf.String()

	if !strings.Contains(output, "A") || !strings.Contains(output, "B") {
		t.Errorf("without exclude tags, all entries should appear, got:\n%s", output)
	}
}

func TestDocsIndexUsesRepoRelativePathsAcrossMultipleRoots(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("root.md", "Root", "Root docs", "# Root")
	ws.WriteFile("auto-etl/docs/reference.md", `---
title: "Reference"
summary: "ETL reference"
hash: ""
---

# Reference
`)

	entries, err := doctree.WalkRepo(ws.Dir, "docs")
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	DocsIndex(&buf, entries, "docs")
	output := buf.String()

	if !strings.Contains(output, "[Root](docs/root.md): Root docs") {
		t.Fatalf("missing root repo-relative link, got:\n%s", output)
	}
	if !strings.Contains(output, "[Reference](auto-etl/docs/reference.md): ETL reference") {
		t.Fatalf("missing nested repo-relative link, got:\n%s", output)
	}
}
