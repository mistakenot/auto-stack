package format

import (
	"fmt"
	"io"
	"regexp"

	"github.com/mistakenot/auto-graph/internal/graph"
)

// reSpecialChars matches characters that are not valid in bare Mermaid node IDs.
var reSpecialChars = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// WriteMermaid writes the graph in Mermaid flowchart syntax to w.
func WriteMermaid(w io.Writer, g *graph.Graph) error {
	if _, err := fmt.Fprintln(w, "graph LR"); err != nil {
		return err
	}
	for _, e := range g.Edges {
		sourcePath := nodePath(g, e.Source)
		targetPath := nodePath(g, e.Target)
		sourceID := sanitizeMermaidID(sourcePath)
		targetID := sanitizeMermaidID(targetPath)
		if _, err := fmt.Fprintf(w, "    %s[%s] --> %s[%s]\n", sourceID, sourcePath, targetID, targetPath); err != nil {
			return err
		}
	}
	return nil
}

// sanitizeMermaidID replaces special characters (dots, slashes, hyphens, etc.)
// with underscores to produce a valid Mermaid node identifier.
func sanitizeMermaidID(path string) string {
	return reSpecialChars.ReplaceAllString(path, "_")
}
