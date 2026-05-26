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
	pathMap := buildPathMap(g)
	kindMap := buildKindMap(g)
	if _, err := fmt.Fprintln(w, "graph LR"); err != nil {
		return err
	}
	for _, e := range g.Edges {
		sourcePath := pathMap[e.Source]
		targetPath := pathMap[e.Target]
		sourceID := sanitizeMermaidID(sourcePath)
		targetID := sanitizeMermaidID(targetPath)
		sourceLabel := mermaidNodeLabel(sourceID, sourcePath, kindMap[e.Source])
		targetLabel := mermaidNodeLabel(targetID, targetPath, kindMap[e.Target])
		arrow := "-->"
		if e.Kind == graph.EdgeDocLink {
			arrow = "-.->"
		}
		if _, err := fmt.Fprintf(w, "    %s %s %s\n", sourceLabel, arrow, targetLabel); err != nil {
			return err
		}
	}
	return nil
}

// mermaidNodeLabel returns the Mermaid node declaration with appropriate shape.
// Doc nodes use hexagon syntax ({{path}}), file nodes use rectangle syntax ([path]).
func mermaidNodeLabel(id, path string, kind graph.NodeKind) string {
	if kind == graph.NodeDoc {
		return fmt.Sprintf("%s{{%s}}", id, path)
	}
	return fmt.Sprintf("%s[%s]", id, path)
}

// sanitizeMermaidID replaces special characters (dots, slashes, hyphens, etc.)
// with underscores to produce a valid Mermaid node identifier.
func sanitizeMermaidID(path string) string {
	return reSpecialChars.ReplaceAllString(path, "_")
}
