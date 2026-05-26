package graph

// NodeKind classifies graph nodes. Currently only "file" is supported;
// future kinds (commit, doc, script) can be added without schema changes.
type NodeKind string

// EdgeKind classifies graph edges. Currently only "import" is supported;
// future kinds (modifies, references) can be added without schema changes.
type EdgeKind string

const (
	NodeFile NodeKind = "file"
	NodeDoc  NodeKind = "doc"

	EdgeImport  EdgeKind = "import"
	EdgeDocLink EdgeKind = "doc_link"
)

// Node represents a vertex in the code graph.
type Node struct {
	ID       string            `json:"id"`
	Kind     NodeKind          `json:"kind"`
	Path     string            `json:"path"`
	Language string            `json:"language,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"`
}

// Edge represents a directed relationship between two nodes.
type Edge struct {
	Source string            `json:"source"`
	Target string            `json:"target"`
	Kind   EdgeKind          `json:"kind"`
	Attrs  map[string]string `json:"attrs,omitempty"`
}

// Graph is the top-level container for a code context graph.
type Graph struct {
	Root  string `json:"root"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}
