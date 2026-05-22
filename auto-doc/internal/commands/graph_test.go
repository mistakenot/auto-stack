package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/datadyne-io/autodoc/internal/frontmatter"
	"github.com/datadyne-io/autodoc/internal/testutil"
)

func TestBuildGraphEmpty(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.InitGitRepo()

	graph, err := BuildGraph(ws.Dir, "docs", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(graph.Nodes))
	}
	if len(graph.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(graph.Edges))
	}
	if graph.Stats.TotalDocs != 0 {
		t.Errorf("expected 0 total docs, got %d", graph.Stats.TotalDocs)
	}
}

func TestBuildGraphDocsOnly(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("getting-started.md", "Getting Started", "Setup instructions", "# Getting Started")
	ws.WriteDoc("architecture.md", "Architecture", "Overview", "# Architecture")
	ws.InitGitRepo()

	graph, err := BuildGraph(ws.Dir, "docs", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(graph.Nodes))
	}
	if len(graph.Edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(graph.Edges))
	}
	if graph.Stats.TotalDocs != 2 {
		t.Errorf("expected 2 total docs, got %d", graph.Stats.TotalDocs)
	}
	if graph.Stats.IsolatedDocs != 2 {
		t.Errorf("expected 2 isolated docs, got %d", graph.Stats.IsolatedDocs)
	}
	if graph.Stats.ConnectedDocs != 0 {
		t.Errorf("expected 0 connected docs, got %d", graph.Stats.ConnectedDocs)
	}
	for _, n := range graph.Nodes {
		if n.Type != "doc" {
			t.Errorf("expected all nodes to be doc type, got %q", n.Type)
		}
	}
}

func TestBuildGraphWithCodeLinks(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	docHash := graphCreateDoc(t, ws, "docs/cache.md", "deadbeef")

	ws.WriteSourceFile("pkg/cache/lru.go", fmt.Sprintf(`package cache

// [autodoc(deadbeef@%s, 00000000)]
func Get() {
    return
}
`, docHash))
	ws.InitGitRepo()

	graph, err := BuildGraph(ws.Dir, "docs", nil)
	if err != nil {
		t.Fatal(err)
	}

	var docNodes, codeNodes int
	for _, n := range graph.Nodes {
		switch n.Type {
		case "doc":
			docNodes++
		case "code":
			codeNodes++
		}
	}
	if docNodes != 1 {
		t.Errorf("expected 1 doc node, got %d", docNodes)
	}
	if codeNodes != 1 {
		t.Errorf("expected 1 code node, got %d", codeNodes)
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(graph.Edges))
	}

	edge := graph.Edges[0]
	if edge.Source != "pkg/cache/lru.go" {
		t.Errorf("expected source pkg/cache/lru.go, got %q", edge.Source)
	}
	if edge.Target != "docs/cache.md" {
		t.Errorf("expected target docs/cache.md, got %q", edge.Target)
	}
	if edge.LinkType != "code_to_doc" {
		t.Errorf("expected link_type code_to_doc, got %q", edge.LinkType)
	}
	if graph.Stats.ConnectedDocs != 1 {
		t.Errorf("expected 1 connected doc, got %d", graph.Stats.ConnectedDocs)
	}
}

func TestBuildGraphStaleLink(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	graphCreateDoc(t, ws, "docs/cache.md", "deadbeef")

	ws.WriteSourceFile("pkg/cache/lru.go", `package cache

// [autodoc(deadbeef@00000000, 00000000)]
func Get() {
    return
}
`)
	ws.InitGitRepo()

	graph, err := BuildGraph(ws.Dir, "docs", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(graph.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(graph.Edges))
	}
	if graph.Edges[0].Status == "ok" {
		t.Error("expected non-ok status for stale link")
	}
	if graph.Stats.StaleEdges != 1 {
		t.Errorf("expected 1 stale edge, got %d", graph.Stats.StaleEdges)
	}
}

func TestBuildGraphOrphaned(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	ws.WriteDoc("other.md", "Other", "Unrelated doc", "# Other")

	ws.WriteSourceFile("pkg/cache/lru.go", `package cache

// [autodoc(aaaaaaaa@bbbbbbbb, cccccccc)]
func Get() {
    return
}
`)
	ws.InitGitRepo()

	graph, err := BuildGraph(ws.Dir, "docs", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(graph.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(graph.Edges))
	}
	if graph.Edges[0].Status != "orphaned_tag" {
		t.Errorf("expected orphaned_tag status, got %q", graph.Edges[0].Status)
	}
	if graph.Edges[0].Target != "" {
		t.Errorf("expected empty target for orphaned edge, got %q", graph.Edges[0].Target)
	}
	if graph.Stats.OrphanedEdges != 1 {
		t.Errorf("expected 1 orphaned edge, got %d", graph.Stats.OrphanedEdges)
	}
}

func TestBuildGraphPathsAreProjectRelative(t *testing.T) {
	ws := testutil.NewWorkspace(t)
	docHash := graphCreateDoc(t, ws, "docs/cache.md", "deadbeef")

	ws.WriteSourceFile("pkg/cache/lru.go", fmt.Sprintf(`package cache

// [autodoc(deadbeef@%s, 00000000)]
func Get() {
    return
}
`, docHash))
	ws.InitGitRepo()

	graph, err := BuildGraph(ws.Dir, "docs", nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range graph.Nodes {
		if strings.HasPrefix(n.Path, "/") {
			t.Errorf("node path should be relative, got %q", n.Path)
		}
		if strings.Contains(n.Path, "\\") {
			t.Errorf("node path should use forward slashes, got %q", n.Path)
		}
	}
	for _, e := range graph.Edges {
		if strings.HasPrefix(e.Source, "/") {
			t.Errorf("edge source should be relative, got %q", e.Source)
		}
		if strings.Contains(e.Source, "\\") {
			t.Errorf("edge source should use forward slashes, got %q", e.Source)
		}
	}
}

func TestGraphOutputJSON(t *testing.T) {
	graph := &GraphJSON{
		Nodes: []GraphNodeJSON{
			{Path: "docs/auth.md", Type: "doc", ID: "a1b2c3d4", Title: "Auth", Summary: "Auth docs"},
			{Path: "pkg/auth.go", Type: "code"},
		},
		Edges: []GraphEdgeJSON{
			{Source: "pkg/auth.go", Target: "docs/auth.md", Line: 5, Status: "ok", LinkType: "code_to_doc"},
		},
		Stats: GraphStatsJSON{TotalDocs: 1, ConnectedDocs: 1, TotalCode: 1, TotalEdges: 1, OKEdges: 1},
	}

	var buf bytes.Buffer
	if err := GraphOutputJSON(&buf, graph); err != nil {
		t.Fatal(err)
	}

	var parsed GraphJSON
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if len(parsed.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(parsed.Nodes))
	}
	if len(parsed.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(parsed.Edges))
	}
}

func TestGraphOutputMarkdown(t *testing.T) {
	graph := &GraphJSON{
		Nodes: []GraphNodeJSON{
			{Path: "docs/auth.md", Type: "doc", ID: "a1b2c3d4", Title: "Auth"},
			{Path: "docs/readme.md", Type: "doc", ID: "e5f6a7b8", Title: "Readme"},
			{Path: "pkg/auth.go", Type: "code"},
		},
		Edges: []GraphEdgeJSON{
			{Source: "pkg/auth.go", Target: "docs/auth.md", Line: 5, Status: "ok", LinkType: "code_to_doc"},
		},
		Stats: GraphStatsJSON{
			TotalDocs: 2, ConnectedDocs: 1, IsolatedDocs: 1,
			TotalCode: 1, TotalEdges: 1, OKEdges: 1,
		},
	}

	var buf bytes.Buffer
	GraphOutput(&buf, graph)
	output := buf.String()

	if !strings.Contains(output, "# Document Graph") {
		t.Error("missing header")
	}
	if !strings.Contains(output, "## Connected Documents") {
		t.Error("missing connected section")
	}
	if !strings.Contains(output, "## Isolated Documents") {
		t.Error("missing isolated section")
	}
	if !strings.Contains(output, "docs/auth.md") {
		t.Error("missing connected doc path")
	}
	if !strings.Contains(output, "docs/readme.md") {
		t.Error("missing isolated doc path")
	}
	if !strings.Contains(output, "pkg/auth.go:5") {
		t.Error("missing edge reference")
	}
	if !strings.Contains(output, "(ok)") {
		t.Error("missing status")
	}
}

func TestGraphOutputNoDocsMessage(t *testing.T) {
	graph := &GraphJSON{
		Stats: GraphStatsJSON{},
	}

	var buf bytes.Buffer
	GraphOutput(&buf, graph)
	if !strings.Contains(buf.String(), "No documents found") {
		t.Error("expected 'No documents found' message")
	}
}

func graphCreateDoc(t *testing.T, ws *testutil.Workspace, relPath, id string) string {
	t.Helper()
	title := "Cache"
	summary := "Cache docs"
	body := "\n# Cache\n"
	hash := frontmatter.ComputeHash(&frontmatter.Doc{Title: title, Summary: summary, Body: body})
	ws.WriteDocWithId(strings.TrimPrefix(relPath, "docs/"), id, title, summary, hash, "# Cache")
	return hash
}
