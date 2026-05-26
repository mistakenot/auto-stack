package doclink

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-graph/internal/graph"
)

// initGitRepo creates a git repo in dir with an initial commit of all files.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test.com"},
		{"config", "user.name", "test"},
		{"add", "-A"},
		{"commit", "-m", "init", "--allow-empty"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// writeFile is a test helper that writes content to a file, creating parent dirs.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScan_ResolvesLinks(t *testing.T) {
	dir := t.TempDir()

	// Create a source file with an autodoc tag.
	writeFile(t, filepath.Join(dir, "main.go"), `package main
// [autodoc(aabbccdd@11223344, 55667788)]
func main() {}
`)

	// Create a doc file with matching ID.
	writeFile(t, filepath.Join(dir, "docs", "guide.md"), `---
id: "aabbccdd"
title: "User Guide"
summary: "A guide"
hash: "00000000"
---
Some doc content.
`)

	initGitRepo(t, dir)

	var buf bytes.Buffer
	links, err := Scan(dir, &buf)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if buf.Len() > 0 {
		t.Errorf("unexpected warnings: %s", buf.String())
	}

	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}

	link := links[0]
	if link.SourceFile != "main.go" {
		t.Errorf("SourceFile = %q, want %q", link.SourceFile, "main.go")
	}
	if link.DocFile != "docs/guide.md" {
		t.Errorf("DocFile = %q, want %q", link.DocFile, "docs/guide.md")
	}
	if link.DocID != "aabbccdd" {
		t.Errorf("DocID = %q, want %q", link.DocID, "aabbccdd")
	}
	if link.DocTitle != "User Guide" {
		t.Errorf("DocTitle = %q, want %q", link.DocTitle, "User Guide")
	}
}

func TestScan_SoftFailure_NonGitDir(t *testing.T) {
	dir := t.TempDir()

	// Not a git repo — ScanFiles should fail, but Scan should soft-fail.
	var buf bytes.Buffer
	links, err := Scan(dir, &buf)
	if err != nil {
		t.Fatalf("Scan should not return error, got: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("expected empty links, got %d", len(links))
	}
	if buf.Len() == 0 {
		t.Error("expected a warning to be logged")
	}
}

func TestScan_SoftFailure_GitDirNoDocs(t *testing.T) {
	dir := t.TempDir()

	// Git repo with a source file but no docs directory.
	writeFile(t, filepath.Join(dir, "main.go"), `package main
func main() {}
`)

	initGitRepo(t, dir)

	var buf bytes.Buffer
	links, err := Scan(dir, &buf)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("expected empty links, got %d", len(links))
	}
}

func TestScan_DeduplicatesLinks(t *testing.T) {
	dir := t.TempDir()

	// Two tags in the same file pointing to the same doc.
	writeFile(t, filepath.Join(dir, "main.go"), `package main
// [autodoc(aabbccdd@11223344, 55667788)]
// [autodoc(aabbccdd@99887766, aabbccdd)]
func main() {}
`)

	writeFile(t, filepath.Join(dir, "docs", "guide.md"), `---
id: "aabbccdd"
title: "Guide"
summary: "A guide"
hash: "00000000"
---
Content.
`)

	initGitRepo(t, dir)

	var buf bytes.Buffer
	links, err := Scan(dir, &buf)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(links) != 1 {
		t.Errorf("expected 1 deduplicated link, got %d", len(links))
	}
}

func TestEnrich_AddsDocNodesAndEdges(t *testing.T) {
	g := &graph.Graph{
		Root: "/project",
		Nodes: []graph.Node{
			{ID: "main.go", Kind: graph.NodeFile, Path: "main.go"},
		},
	}

	links := []Link{
		{SourceFile: "main.go", DocFile: "docs/guide.md", DocID: "aabbccdd", DocTitle: "Guide"},
	}

	Enrich(g, links)

	if len(g.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(g.Nodes))
	}

	docNode := g.Nodes[1]
	if docNode.Kind != graph.NodeDoc {
		t.Errorf("node kind = %q, want %q", docNode.Kind, graph.NodeDoc)
	}
	if docNode.ID != "docs/guide.md" {
		t.Errorf("node ID = %q, want %q", docNode.ID, "docs/guide.md")
	}
	if docNode.Attrs["title"] != "Guide" {
		t.Errorf("node title = %q, want %q", docNode.Attrs["title"], "Guide")
	}

	if len(g.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(g.Edges))
	}

	edge := g.Edges[0]
	if edge.Source != "main.go" {
		t.Errorf("edge source = %q, want %q", edge.Source, "main.go")
	}
	if edge.Target != "docs/guide.md" {
		t.Errorf("edge target = %q, want %q", edge.Target, "docs/guide.md")
	}
	if edge.Kind != graph.EdgeDocLink {
		t.Errorf("edge kind = %q, want %q", edge.Kind, graph.EdgeDocLink)
	}
}

func TestEnrich_DeduplicatesNodesAndEdges(t *testing.T) {
	g := &graph.Graph{
		Root: "/project",
		Nodes: []graph.Node{
			{ID: "main.go", Kind: graph.NodeFile, Path: "main.go"},
			{ID: "lib.go", Kind: graph.NodeFile, Path: "lib.go"},
		},
	}

	links := []Link{
		{SourceFile: "main.go", DocFile: "docs/guide.md", DocID: "aabbccdd", DocTitle: "Guide"},
		{SourceFile: "main.go", DocFile: "docs/guide.md", DocID: "aabbccdd", DocTitle: "Guide"},
		{SourceFile: "lib.go", DocFile: "docs/guide.md", DocID: "aabbccdd", DocTitle: "Guide"},
	}

	Enrich(g, links)

	// Original 2 nodes + 1 doc node = 3.
	if len(g.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(g.Nodes))
	}

	// 2 edges: main.go->guide.md, lib.go->guide.md (deduped from 3 links).
	if len(g.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(g.Edges))
	}
}

func TestEnrich_SkipsUnknownSourceFiles(t *testing.T) {
	g := &graph.Graph{
		Root: "/project",
		Nodes: []graph.Node{
			{ID: "main.go", Kind: graph.NodeFile, Path: "main.go"},
		},
	}

	links := []Link{
		{SourceFile: "unknown.go", DocFile: "docs/guide.md", DocID: "aabbccdd", DocTitle: "Guide"},
	}

	Enrich(g, links)

	if len(g.Nodes) != 1 {
		t.Errorf("expected 1 node (unchanged), got %d", len(g.Nodes))
	}
	if len(g.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(g.Edges))
	}
}

func TestEnrich_NoOpWhenEmpty(t *testing.T) {
	g := &graph.Graph{
		Root: "/project",
		Nodes: []graph.Node{
			{ID: "main.go", Kind: graph.NodeFile, Path: "main.go"},
		},
	}

	Enrich(g, nil)

	if len(g.Nodes) != 1 {
		t.Errorf("expected 1 node (unchanged), got %d", len(g.Nodes))
	}
	if len(g.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(g.Edges))
	}
}
