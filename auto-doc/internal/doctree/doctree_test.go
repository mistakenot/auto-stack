package doctree

import (
	"fmt"
	"slices"
	"testing"

	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestWalkBasic(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("getting-started.md", "Getting Started", "Setup instructions", "# Getting Started")
	ws.WriteDoc("architecture.md", "Architecture", "System overview", "# Architecture")

	entries, err := Walk(ws.Path("docs"))
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	// Sorted alphabetically
	if entries[0].RelPath != "architecture.md" {
		t.Errorf("entries[0].RelPath = %q, want %q", entries[0].RelPath, "architecture.md")
	}
	if entries[0].Title != "Architecture" {
		t.Errorf("entries[0].Title = %q, want %q", entries[0].Title, "Architecture")
	}
	if entries[1].RelPath != "getting-started.md" {
		t.Errorf("entries[1].RelPath = %q, want %q", entries[1].RelPath, "getting-started.md")
	}
}

func TestWalkNestedDirs(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("api/auth.md", "Authentication", "Auth docs", "# Auth")
	ws.WriteDoc("api/endpoints.md", "Endpoints", "API endpoints", "# Endpoints")
	ws.WriteDoc("guides/setup.md", "Setup", "Setup guide", "# Setup")

	entries, err := Walk(ws.Path("docs"))
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	if entries[0].RelPath != "api/auth.md" {
		t.Errorf("entries[0].RelPath = %q, want %q", entries[0].RelPath, "api/auth.md")
	}
}

func TestWalkSkipsNonMarkdown(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("readme.md", "Readme", "The readme", "# Readme")
	ws.WriteFile("docs/image.png", "not a real image")
	ws.WriteFile("docs/data.json", "{}")

	entries, err := Walk(ws.Path("docs"))
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
}

func TestWalkEmptyDir(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	// Create empty docs dir
	ws.WriteFile("docs/.gitkeep", "")

	entries, err := Walk(ws.Path("docs"))
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("got %d entries, want 0", len(entries))
	}
}

func TestWalkIgnoresGlob(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("readme.md", "Readme", "The readme", "# Readme")
	ws.WriteDoc("internal.md", "Internal", "Internal docs", "# Internal")
	ws.WriteDoc("draft-notes.md", "Draft", "Draft notes", "# Draft")

	entries, err := Walk(ws.Path("docs"), "internal.md", "draft-*.md")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].RelPath != "readme.md" {
		t.Errorf("entries[0].RelPath = %q, want %q", entries[0].RelPath, "readme.md")
	}
}

func TestWalkIgnoresNestedGlob(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("api/auth.md", "Auth", "Auth docs", "# Auth")
	ws.WriteDoc("api/draft-api.md", "Draft API", "Draft", "# Draft")
	ws.WriteDoc("guide.md", "Guide", "Guide docs", "# Guide")

	entries, err := Walk(ws.Path("docs"), "draft-*.md")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
}

func TestWalkIgnoresDirectoryWildcard(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("tasks/plan.md", "Plan", "A plan", "# Plan")
	ws.WriteDoc("tasks/sub/task.md", "Task", "A task", "# Task")
	ws.WriteDoc("guide.md", "Guide", "Guide docs", "# Guide")

	// "tasks/*" should exclude everything under tasks/, including nested
	entries, err := Walk(ws.Path("docs"), "tasks/*")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %v", len(entries), entries)
	}
	if entries[0].RelPath != "guide.md" {
		t.Errorf("entries[0].RelPath = %q, want %q", entries[0].RelPath, "guide.md")
	}
}

func TestWalkIgnoresWithDocsPrefix(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("tasks/plan.md", "Plan", "A plan", "# Plan")
	ws.WriteDoc("guide.md", "Guide", "Guide docs", "# Guide")

	// User might write "docs/tasks/*" in config — should still work
	entries, err := Walk(ws.Path("docs"), "docs/tasks/*")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %v", len(entries), entries)
	}
	if entries[0].RelPath != "guide.md" {
		t.Errorf("entries[0].RelPath = %q, want %q", entries[0].RelPath, "guide.md")
	}
}

func TestWalkParsesDocId(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDocWithId("guide.md", "deadbeef", "Guide", "Guide docs", "", "# Guide")

	entries, err := Walk(ws.Path("docs"))
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Id != "deadbeef" {
		t.Fatalf("entries[0].Id = %q, want %q", entries[0].Id, "deadbeef")
	}
}

func TestWalkRepoDiscoversMultipleDocsRoots(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("root.md", "Root", "Root docs", "# Root")
	writeRepoDoc(t, ws, "auto-etl/docs/etl.md", "ETL", "ETL docs")
	writeRepoDoc(t, ws, "services/payments/api/docs/endpoints.md", "Endpoints", "API endpoints")

	entries, err := WalkRepo(ws.Dir, "docs")
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	wantPaths := []string{
		"auto-etl/docs/etl.md",
		"docs/root.md",
		"services/payments/api/docs/endpoints.md",
	}
	if got := repoPaths(entries); !slices.Equal(got, wantPaths) {
		t.Fatalf("repo paths = %v, want %v", got, wantPaths)
	}

	var target Entry
	for _, e := range entries {
		if e.RepoRelPath == "services/payments/api/docs/endpoints.md" {
			target = e
			break
		}
	}
	if target.DocsRootRel != "services/payments/api/docs" {
		t.Fatalf("DocsRootRel = %q, want %q", target.DocsRootRel, "services/payments/api/docs")
	}
	if target.RelPath != "endpoints.md" {
		t.Fatalf("RelPath = %q, want %q", target.RelPath, "endpoints.md")
	}
}

func TestDiscoverDocsMarkdownPathsGitIncludesUntrackedAndExcludesIgnored(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("root.md", "Root", "Root docs", "# Root")
	ws.WriteFile(".gitignore", "ignored/\n")
	ws.InitGitRepo()

	writeRepoDoc(t, ws, "auto-doc/docs/untracked.md", "Untracked", "Untracked docs")
	writeRepoDoc(t, ws, "ignored/docs/hidden.md", "Hidden", "Ignored docs")

	paths, err := DiscoverDocsMarkdownPaths(ws.Dir, "docs")
	if err != nil {
		t.Fatalf("DiscoverDocsMarkdownPaths: %v", err)
	}

	if !slices.Contains(paths, "docs/root.md") {
		t.Fatalf("expected tracked docs/root.md in %v", paths)
	}
	if !slices.Contains(paths, "auto-doc/docs/untracked.md") {
		t.Fatalf("expected untracked auto-doc/docs/untracked.md in %v", paths)
	}
	if slices.Contains(paths, "ignored/docs/hidden.md") {
		t.Fatalf("ignored docs file should be excluded: %v", paths)
	}
}

func TestDiscoverDocsMarkdownPathsFilesystemFallbackAndCompatibilityRoot(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	writeRepoDoc(t, ws, "services/docs/guide.md", "Guide", "Service guide")
	writeRepoDoc(t, ws, "manuals/intro.md", "Manual", "Manual intro")
	writeRepoDoc(t, ws, "notes/readme.md", "Notes", "General notes")

	paths, err := DiscoverDocsMarkdownPaths(ws.Dir, "manuals")
	if err != nil {
		t.Fatalf("DiscoverDocsMarkdownPaths: %v", err)
	}

	if !slices.Contains(paths, "services/docs/guide.md") {
		t.Fatalf("expected services/docs/guide.md in %v", paths)
	}
	if !slices.Contains(paths, "manuals/intro.md") {
		t.Fatalf("expected compatibility root file manuals/intro.md in %v", paths)
	}
	if slices.Contains(paths, "notes/readme.md") {
		t.Fatalf("unexpected file from non-doc path included: %v", paths)
	}
}

func TestDiscoverDocsMarkdownPathsDedupesOverlap(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	writeRepoDoc(t, ws, "manuals/docs/guide.md", "Guide", "Guide docs")

	paths, err := DiscoverDocsMarkdownPaths(ws.Dir, "manuals")
	if err != nil {
		t.Fatalf("DiscoverDocsMarkdownPaths: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("got %d paths, want 1: %v", len(paths), paths)
	}
	if paths[0] != "manuals/docs/guide.md" {
		t.Fatalf("path = %q, want %q", paths[0], "manuals/docs/guide.md")
	}
}

func TestWalkRepoIgnoresLegacyTasksPattern(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	writeRepoDoc(t, ws, "auto-etl/docs/tasks/plan.md", "Plan", "Task plan")
	writeRepoDoc(t, ws, "auto-etl/docs/guide.md", "Guide", "Guide docs")

	entries, err := WalkRepo(ws.Dir, "docs", "tasks/*")
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].RepoRelPath != "auto-etl/docs/guide.md" {
		t.Fatalf("RepoRelPath = %q, want %q", entries[0].RepoRelPath, "auto-etl/docs/guide.md")
	}
}

func writeRepoDoc(t *testing.T, ws *testutil.Workspace, relPath, title, summary string) {
	t.Helper()
	ws.WriteFile(relPath, fmt.Sprintf(`---
title: %q
summary: %q
hash: ""
---

# %s
`, title, summary, title))
}

func repoPaths(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for i := range entries {
		out = append(out, entries[i].RepoRelPath)
	}
	return out
}
