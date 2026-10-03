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

// CodeFrozen is the code of a write refused because the plan is frozen.
const CodeFrozen = "frozen"

// Node statuses. Retiring a node flips its status and keeps its ID.
const (
	StatusActive  = "active"
	StatusRetired = "retired"
)

// Graph is the whole of graph.json.
type Graph struct {
	// Version is the semver format version the plan was written with
	// (schema.Version for new plans). It is never migrated.
	Version string
	// ID is the plan's ID, `NNN-xxxx`: its folder number and 4 random
	// Crockford base32 characters. Qualified references name it.
	ID    string
	Nodes []Node
	Edges []Edge
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

// Edge is a typed relationship with its own ID (`e-xxxx`), so a review or
// comment can anchor to it. To may be qualified (`005-k7q2:r-8hw3`). The
// (From, Type, To) triple is still unique within a plan.
type Edge struct {
	ID    string
	From  string
	Type  string
	To    string
	Extra map[string]json.RawMessage
}

// New returns a graph of the current format version with plan ID id,
// holding only the plan node.
func New(id string, plan map[string]any) *Graph {
	return &Graph{
		Version: schema.Version,
		ID:      id,
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

// EdgeByID returns the edge with the given ID.
func (g *Graph) EdgeByID(id string) (Edge, bool) {
	for _, e := range g.Edges {
		if e.ID == id {
			return e, true
		}
	}
	return Edge{}, false
}

// Number is the plan number the graph's ID starts with ("" when the ID is
// not a plan ID).
func (g *Graph) Number() string {
	if !PlanIDPattern.MatchString(g.ID) {
		return ""
	}
	return g.ID[:3]
}

// OtherVersion reports whether g was written by another major version of
// the format, or by a newer version of this one. Such a plan is read best
// effort and never written. An unparseable version is not "other": Validate
// reports it as bad-version.
func (g *Graph) OtherVersion() bool {
	v, ok := schema.ParseVersion(g.Version)
	if !ok {
		return false
	}
	cur := schema.Current()
	return v.Major != cur.Major || v.Compare(cur) > 0
}

// OtherMajor reports whether g was written by another major version of the
// format: its nodes and edges follow another registry, so Validate does not
// check them against this one.
func (g *Graph) OtherMajor() bool {
	v, ok := schema.ParseVersion(g.Version)
	return ok && v.Major != schema.Current().Major
}

// Frozen reports why g may not be written, or "" when it may. A plan is
// frozen when it was written by another major version or a newer version of
// the format (plans are never migrated), or when its lifecycle is done (a
// finished plan is history). Setting lifecycle done is itself allowed: it is
// a plan's last write.
func (g *Graph) Frozen() string {
	if g.OtherVersion() {
		return "it is format version " + g.Version + " and this tool writes " + schema.Version +
			" (a plan is written only by tools of its own major version, never newer)"
	}
	if plan, ok := g.Plan(); ok && plan.StringField("lifecycle") == string(schema.LifecycleDone) {
		return "its lifecycle is done"
	}
	return ""
}

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
