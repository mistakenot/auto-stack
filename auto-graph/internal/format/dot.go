package format

import (
	"fmt"
	"io"

	"github.com/mistakenot/auto-graph/internal/graph"
)

// WriteDOT writes the graph in Graphviz DOT format to w.
func WriteDOT(w io.Writer, g *graph.Graph) error {
	pathMap := buildPathMap(g)
	if _, err := fmt.Fprintln(w, "digraph imports {"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "    rankdir=LR;"); err != nil {
		return err
	}
	// Emit node declarations for doc nodes.
	for _, n := range g.Nodes {
		if n.Kind == graph.NodeDoc {
			if _, err := fmt.Fprintf(w, "    %q [shape=note];\n", n.Path); err != nil {
				return err
			}
		}
	}
	for _, e := range g.Edges {
		sourcePath := pathMap[e.Source]
		targetPath := pathMap[e.Target]
		if e.Kind == graph.EdgeDocLink {
			if _, err := fmt.Fprintf(w, "    %q -> %q [style=dashed];\n", sourcePath, targetPath); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(w, "    %q -> %q;\n", sourcePath, targetPath); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintln(w, "}"); err != nil {
		return err
	}
	return nil
}

// buildKindMap creates a map from node ID to NodeKind for O(1) lookups.
func buildKindMap(g *graph.Graph) map[string]graph.NodeKind {
	m := make(map[string]graph.NodeKind, len(g.Nodes))
	for _, n := range g.Nodes {
		m[n.ID] = n.Kind
	}
	return m
}

// buildPathMap creates a map from node ID to path for O(1) lookups.
func buildPathMap(g *graph.Graph) map[string]string {
	m := make(map[string]string, len(g.Nodes))
	for _, n := range g.Nodes {
		m[n.ID] = n.Path
	}
	return m
}
