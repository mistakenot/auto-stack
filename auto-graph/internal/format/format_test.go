package format

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mistakenot/auto-graph/internal/graph"
)

func testGraph() *graph.Graph {
	return &graph.Graph{
		Root: "/project",
		Nodes: []graph.Node{
			{ID: "src/index.ts", Kind: graph.NodeFile, Path: "src/index.ts", Language: "typescript"},
			{ID: "src/utils.ts", Kind: graph.NodeFile, Path: "src/utils.ts", Language: "typescript"},
			{ID: "src/helpers.ts", Kind: graph.NodeFile, Path: "src/helpers.ts", Language: "typescript"},
		},
		Edges: []graph.Edge{
			{Source: "src/index.ts", Target: "src/utils.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "raw": "./utils"}},
			{Source: "src/utils.ts", Target: "src/helpers.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "raw": "./helpers"}},
		},
	}
}

func TestJSONFormat(t *testing.T) {
	g := testGraph()

	var buf bytes.Buffer
	if err := WriteJSON(&buf, g); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	output := buf.String()

	// Verify it is valid JSON.
	var parsed graph.Graph
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\nOutput:\n%s", err, output)
	}

	// Verify structure.
	if parsed.Root != "/project" {
		t.Errorf("expected root %q, got %q", "/project", parsed.Root)
	}
	if len(parsed.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(parsed.Nodes))
	}
	if len(parsed.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(parsed.Edges))
	}

	// Check node fields.
	if parsed.Nodes[0].ID != "src/index.ts" {
		t.Errorf("first node ID: expected %q, got %q", "src/index.ts", parsed.Nodes[0].ID)
	}
	if parsed.Nodes[0].Kind != graph.NodeFile {
		t.Errorf("first node kind: expected %q, got %q", graph.NodeFile, parsed.Nodes[0].Kind)
	}

	// Check edge fields.
	if parsed.Edges[0].Source != "src/index.ts" {
		t.Errorf("first edge source: expected %q, got %q", "src/index.ts", parsed.Edges[0].Source)
	}
	if parsed.Edges[0].Target != "src/utils.ts" {
		t.Errorf("first edge target: expected %q, got %q", "src/utils.ts", parsed.Edges[0].Target)
	}
}

func TestDOTFormat(t *testing.T) {
	g := testGraph()

	var buf bytes.Buffer
	if err := WriteDOT(&buf, g); err != nil {
		t.Fatalf("WriteDOT failed: %v", err)
	}

	output := buf.String()

	// Verify DOT structure.
	if !strings.Contains(output, "digraph imports {") {
		t.Error("DOT output missing 'digraph imports {'")
	}
	if !strings.Contains(output, "rankdir=LR") {
		t.Error("DOT output missing 'rankdir=LR'")
	}
	if !strings.Contains(output, "->") {
		t.Error("DOT output missing '->' arrow notation")
	}
	if !strings.Contains(output, `"src/index.ts" -> "src/utils.ts"`) {
		t.Errorf("DOT output missing expected edge.\nOutput:\n%s", output)
	}
	if !strings.Contains(output, `"src/utils.ts" -> "src/helpers.ts"`) {
		t.Errorf("DOT output missing expected edge.\nOutput:\n%s", output)
	}
	if !strings.HasSuffix(strings.TrimSpace(output), "}") {
		t.Error("DOT output should end with '}'")
	}
}

func TestMermaidFormat(t *testing.T) {
	g := testGraph()

	var buf bytes.Buffer
	if err := WriteMermaid(&buf, g); err != nil {
		t.Fatalf("WriteMermaid failed: %v", err)
	}

	output := buf.String()

	// Verify Mermaid structure.
	if !strings.HasPrefix(output, "graph LR") {
		t.Error("Mermaid output should start with 'graph LR'")
	}
	if !strings.Contains(output, "-->") {
		t.Error("Mermaid output missing '-->' arrows")
	}
	// Check that node paths appear in the output.
	if !strings.Contains(output, "src/index.ts") {
		t.Error("Mermaid output missing src/index.ts")
	}
	if !strings.Contains(output, "src/utils.ts") {
		t.Error("Mermaid output missing src/utils.ts")
	}
	if !strings.Contains(output, "src/helpers.ts") {
		t.Error("Mermaid output missing src/helpers.ts")
	}
}

func TestJSONEmptyGraph(t *testing.T) {
	g := &graph.Graph{Root: "/empty"}

	var buf bytes.Buffer
	if err := WriteJSON(&buf, g); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	var parsed graph.Graph
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed.Root != "/empty" {
		t.Errorf("expected root %q, got %q", "/empty", parsed.Root)
	}
}

func TestDOTEmptyGraph(t *testing.T) {
	g := &graph.Graph{Root: "/empty"}

	var buf bytes.Buffer
	if err := WriteDOT(&buf, g); err != nil {
		t.Fatalf("WriteDOT failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "digraph imports {") {
		t.Error("empty DOT output missing header")
	}
	if !strings.HasSuffix(strings.TrimSpace(output), "}") {
		t.Error("empty DOT output should end with '}'")
	}
}

func TestMermaidEmptyGraph(t *testing.T) {
	g := &graph.Graph{Root: "/empty"}

	var buf bytes.Buffer
	if err := WriteMermaid(&buf, g); err != nil {
		t.Fatalf("WriteMermaid failed: %v", err)
	}

	output := buf.String()
	if !strings.HasPrefix(output, "graph LR") {
		t.Error("empty Mermaid output should start with 'graph LR'")
	}
}

func testGraphWithDocs() *graph.Graph {
	return &graph.Graph{
		Root: "/project",
		Nodes: []graph.Node{
			{ID: "src/index.ts", Kind: graph.NodeFile, Path: "src/index.ts", Language: "typescript"},
			{ID: "src/utils.ts", Kind: graph.NodeFile, Path: "src/utils.ts", Language: "typescript"},
			{ID: "docs/guide.md", Kind: graph.NodeDoc, Path: "docs/guide.md"},
		},
		Edges: []graph.Edge{
			{Source: "src/index.ts", Target: "src/utils.ts", Kind: graph.EdgeImport, Attrs: map[string]string{"import_kind": "static", "raw": "./utils"}},
			{Source: "src/index.ts", Target: "docs/guide.md", Kind: graph.EdgeDocLink},
		},
	}
}

func TestDOTFormat_DocNodes(t *testing.T) {
	g := testGraphWithDocs()

	var buf bytes.Buffer
	if err := WriteDOT(&buf, g); err != nil {
		t.Fatalf("WriteDOT failed: %v", err)
	}

	output := buf.String()

	// Doc node should have shape=note.
	if !strings.Contains(output, `"docs/guide.md" [shape=note]`) {
		t.Errorf("DOT output missing doc node declaration with [shape=note].\nOutput:\n%s", output)
	}

	// Doc edge should have style=dashed.
	if !strings.Contains(output, `"src/index.ts" -> "docs/guide.md" [style=dashed]`) {
		t.Errorf("DOT output missing dashed doc edge.\nOutput:\n%s", output)
	}

	// Import edge should NOT have style=dashed.
	if !strings.Contains(output, `"src/index.ts" -> "src/utils.ts";`) {
		t.Errorf("DOT output missing plain import edge.\nOutput:\n%s", output)
	}
}

func TestMermaidFormat_DocNodes(t *testing.T) {
	g := testGraphWithDocs()

	var buf bytes.Buffer
	if err := WriteMermaid(&buf, g); err != nil {
		t.Fatalf("WriteMermaid failed: %v", err)
	}

	output := buf.String()

	// Doc node should use hexagon syntax {{}}.
	if !strings.Contains(output, "{{docs/guide.md}}") {
		t.Errorf("Mermaid output missing doc node with hexagon syntax.\nOutput:\n%s", output)
	}

	// Doc edge should use dotted arrow -.->
	if !strings.Contains(output, "-.->") {
		t.Errorf("Mermaid output missing dotted arrow for doc edge.\nOutput:\n%s", output)
	}

	// Import edge should use solid arrow -->
	if !strings.Contains(output, "-->") {
		t.Errorf("Mermaid output missing solid arrow for import edge.\nOutput:\n%s", output)
	}

	// File nodes should use rectangle syntax [].
	if !strings.Contains(output, "[src/index.ts]") {
		t.Errorf("Mermaid output missing file node with rectangle syntax.\nOutput:\n%s", output)
	}
}

func TestJSONFormat_DocNodes(t *testing.T) {
	g := testGraphWithDocs()

	var buf bytes.Buffer
	if err := WriteJSON(&buf, g); err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	var parsed graph.Graph
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	// Verify doc node kind.
	var foundDocNode bool
	for _, n := range parsed.Nodes {
		if n.ID == "docs/guide.md" {
			foundDocNode = true
			if n.Kind != graph.NodeDoc {
				t.Errorf("expected doc node kind %q, got %q", graph.NodeDoc, n.Kind)
			}
		}
	}
	if !foundDocNode {
		t.Error("expected docs/guide.md node in JSON output")
	}

	// Verify doc_link edge kind.
	var foundDocEdge bool
	for _, e := range parsed.Edges {
		if e.Target == "docs/guide.md" {
			foundDocEdge = true
			if e.Kind != graph.EdgeDocLink {
				t.Errorf("expected edge kind %q, got %q", graph.EdgeDocLink, e.Kind)
			}
		}
	}
	if !foundDocEdge {
		t.Error("expected doc_link edge in JSON output")
	}
}
