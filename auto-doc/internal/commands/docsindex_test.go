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

func TestDocsIndexWithReadWhen(t *testing.T) {
	entries := []doctree.Entry{
		{RelPath: "guide.md", RepoRelPath: "docs/guide.md", Title: "Guide", Summary: "How to use", ReadWhen: "onboarding new users"},
	}

	var buf bytes.Buffer
	DocsIndex(&buf, entries, "docs")
	output := buf.String()

	if !strings.Contains(output, ". Read when: onboarding new users") {
		t.Fatalf("expected read_when inline, got:\n%s", output)
	}
}

func TestDocsIndexReadWhenWithoutSummary(t *testing.T) {
	entries := []doctree.Entry{
		{RelPath: "bare.md", RepoRelPath: "docs/bare.md", Title: "Bare", Summary: "", ReadWhen: "testing"},
	}

	var buf bytes.Buffer
	DocsIndex(&buf, entries, "docs")
	output := buf.String()

	if strings.Contains(output, "Read when") {
		t.Fatalf("should not show read_when without summary, got:\n%s", output)
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
