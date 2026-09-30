package graph

import (
	"strconv"

	"github.com/mistakenot/auto-plan/internal/schema"
)

// Mutation-only codes.
const (
	CodeNotAddable = "not-addable"
	CodeIDSpace    = "id-space-exhausted"
)

// Add creates a node of type typ with a generated, collision-free ID and a
// rank after its last sibling, together with its outgoing edges (an edge whose
// From is empty starts at the new node). The resulting graph is validated as a
// whole; on any error g is left unchanged and the errors are returned.
func (g *Graph) Add(typ string, fields map[string]any, edges []Edge) (Node, []ValidationError) {
	nt, ok := schema.Registry.Node(typ)
	if !ok {
		return Node{}, []ValidationError{{
			Code: CodeUnregisteredType, Path: "$.nodes", Field: "type",
			Message: "node type " + strconv.Quote(typ) + " is not registered", Value: typ,
		}}
	}
	if nt.Singleton() {
		return Node{}, []ValidationError{{
			Code: CodeNotAddable, Path: "$.nodes", Field: "type",
			Message: "the " + typ + " node is created with the plan and cannot be added", Value: typ,
		}}
	}

	taken := func(id string) bool {
		_, exists := g.NodeByID(id)
		return exists
	}
	id, err := NewID(nt.Prefix, g.random(), taken)
	if err != nil {
		return Node{}, []ValidationError{{Code: CodeIDSpace, Path: "$.nodes", Field: "id", Message: err.Error()}}
	}

	own := make([]Edge, len(edges))
	for i, e := range edges {
		if e.From == "" {
			e.From = id
		}
		own[i] = e
	}
	if fields == nil {
		fields = map[string]any{}
	}
	node := Node{
		ID:     id,
		Type:   typ,
		Status: StatusActive,
		Rank:   RankAfter(g.lastSiblingRank(nt, own)),
		Fields: fields,
	}

	c := g.clone()
	c.Nodes = append(c.Nodes, node)
	c.Edges = append(c.Edges, own...)
	if errs := Validate(c); len(errs) > 0 {
		return Node{}, errs
	}
	g.Nodes, g.Edges = c.Nodes, c.Edges
	g.Canonicalize()
	return node, nil
}

// lastSiblingRank returns the highest rank among the new node's siblings:
// every node of its type, or — when the type has a RankScope edge — those
// whose RankScope edge targets the same node as the new one's.
func (g *Graph) lastSiblingRank(nt schema.NodeType, edges []Edge) string {
	scope := ""
	if nt.RankScope != "" {
		for _, e := range edges {
			if e.Type == nt.RankScope {
				scope = e.To
				break
			}
		}
	}
	inScope := map[string]bool{}
	if scope != "" {
		for _, e := range g.Edges {
			if e.Type == nt.RankScope && e.To == scope {
				inScope[e.From] = true
			}
		}
	}
	last := ""
	for _, n := range g.Nodes {
		if n.Type != nt.Name || (scope != "" && !inScope[n.ID]) {
			continue
		}
		if n.Rank > last {
			last = n.Rank
		}
	}
	return last
}

// Link adds one typed edge. The resulting graph is validated as a whole; on
// any error g is left unchanged.
func (g *Graph) Link(from, typ, to string) (Edge, []ValidationError) {
	e := Edge{From: from, Type: typ, To: to}
	c := g.clone()
	c.Edges = append(c.Edges, e)
	if errs := Validate(c); len(errs) > 0 {
		return Edge{}, errs
	}
	g.Edges = c.Edges
	g.Canonicalize()
	return e, nil
}
