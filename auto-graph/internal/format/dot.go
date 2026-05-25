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
	for _, e := range g.Edges {
		sourcePath := pathMap[e.Source]
		targetPath := pathMap[e.Target]
		if _, err := fmt.Fprintf(w, "    %q -> %q;\n", sourcePath, targetPath); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "}"); err != nil {
		return err
	}
	return nil
}

// buildPathMap creates a map from node ID to path for O(1) lookups.
func buildPathMap(g *graph.Graph) map[string]string {
	m := make(map[string]string, len(g.Nodes))
	for _, n := range g.Nodes {
		m[n.ID] = n.Path
	}
	return m
}
