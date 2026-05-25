package format

import (
	"encoding/json"
	"io"

	"github.com/mistakenot/auto-graph/internal/graph"
)

// WriteJSON writes the graph as pretty-printed JSON to w.
func WriteJSON(w io.Writer, g *graph.Graph) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(g)
}
