package format

import (
	"fmt"
	"io"

	"github.com/mistakenot/auto-graph/internal/graph"
)

// WriteDOT writes the graph in Graphviz DOT format to w.
func WriteDOT(w io.Writer, g *graph.Graph) error {
	if _, err := fmt.Fprintln(w, "digraph imports {"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "    rankdir=LR;"); err != nil {
		return err
	}
	for _, e := range g.Edges {
		sourcePath := nodePath(g, e.Source)
		targetPath := nodePath(g, e.Target)
		if _, err := fmt.Fprintf(w, "    %q -> %q;\n", sourcePath, targetPath); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "}"); err != nil {
		return err
	}
	return nil
}

// nodePath looks up the Path field for a node by ID. If not found, returns the
// ID itself as a fallback.
func nodePath(g *graph.Graph, id string) string {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n.Path
		}
	}
	return id
}
