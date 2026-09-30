// Package graph holds a plan's graph.json: a generic nodes[] + edges[]
// document (D-5) validated against the type registry in internal/schema.
//
// There are two gates. Decode is tolerant — only malformed JSON fails it — so
// a broken hand edit still loads and lint sees the whole graph. Validate
// returns every structural problem as data. Writes are Decode → mutate →
// Validate → Save, and refuse when the result holds any error.
package graph

import (
	"encoding/json"
	"math/rand/v2"

	"github.com/mistakenot/auto-plan/internal/schema"
	"github.com/mistakenot/auto-shared/config"
)

// ValidationError is the repo-wide structured error shape
// ({code, path, field, message, value}).
type ValidationError = config.ValidationError

// Version is the graph.json format version this package reads and writes.
const Version = 1

// Node statuses. Retiring a node flips its status and keeps its ID.
const (
	StatusActive  = "active"
	StatusRetired = "retired"
)

// Graph is the whole of graph.json.
type Graph struct {
	Version int
	Nodes   []Node
	Edges   []Edge
	// Extra holds top-level keys this version does not know, kept so
	// Validate can report them and a rewrite does not drop them.
	Extra map[string]json.RawMessage

	// decodeIssues are structural problems found while decoding (a known key
	// holding the wrong JSON type, a node that is not an object). Validate
	// reports them alongside everything else.
	decodeIssues []ValidationError
	rnd          *rand.Rand
}

// Node is a generic node envelope; Fields are validated against its NodeType.
type Node struct {
	ID     string
	Type   string
	Status string
	// Rank orders a node among its siblings for reading (D-12). It never
	// appears in an ID and the file is not sorted by it.
	Rank   string
	Fields map[string]any
	Extra  map[string]json.RawMessage
}

// Edge is a typed relationship. To may be qualified (`005:r-8hw3`).
type Edge struct {
	From  string
	Type  string
	To    string
	Extra map[string]json.RawMessage
}

// New returns a graph holding only the plan node.
func New(plan map[string]any) *Graph {
	return &Graph{
		Version: Version,
		Nodes:   []Node{{ID: schema.PlanNodeID, Type: schema.PlanNodeID, Status: StatusActive, Fields: plan}},
		Edges:   []Edge{},
	}
}

// NodeByID returns the node with the given ID.
func (g *Graph) NodeByID(id string) (Node, bool) {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

// Plan returns the plan node.
func (g *Graph) Plan() (Node, bool) { return g.NodeByID(schema.PlanNodeID) }

// StringField returns a string field of n, or "" when absent or not a string.
func (n Node) StringField(name string) string {
	s, _ := n.Fields[name].(string)
	return s
}

// ObjectField returns an object field of n, or nil.
func (n Node) ObjectField(name string) map[string]any {
	m, _ := n.Fields[name].(map[string]any)
	return m
}

// Active reports whether the node has not been retired.
func (n Node) Active() bool { return n.Status != StatusRetired }

// SetRand injects the random source used to generate IDs. Tests and the
// AUTO_PLAN_SEED environment variable use it for deterministic IDs.
func (g *Graph) SetRand(r *rand.Rand) { g.rnd = r }

func (g *Graph) random() *rand.Rand {
	if g.rnd == nil {
		g.rnd = rand.New(globalSource{}) //nolint:gosec // G404: node IDs are opaque labels, not secrets
	}
	return g.rnd
}

// globalSource draws from math/rand/v2's randomly seeded global generator.
type globalSource struct{}

func (globalSource) Uint64() uint64 { return rand.Uint64() } //nolint:gosec // G404: see random()

// clone returns a copy of g whose slices can be appended to or edited without
// touching g. Field maps are shared: mutations replace them, never edit them.
func (g *Graph) clone() *Graph {
	c := *g
	c.Nodes = append([]Node(nil), g.Nodes...)
	c.Edges = append([]Edge(nil), g.Edges...)
	return &c
}
