package graph

import (
	"cmp"
	"maps"
	"reflect"
	"slices"
	"strconv"

	"github.com/mistakenot/auto-plan/internal/schema"
)

// Mutation-only codes.
const (
	CodeNotAddable    = "not-addable"
	CodeIDSpace       = "id-space-exhausted"
	CodeNodeNotFound  = "node-not-found"
	CodeEdgeNotFound  = "edge-not-found"
	CodeFixedField    = "fixed-field"
	CodeNotRetirable  = "not-retirable"
	CodeNotMovable    = "not-movable"
	CodeNotSibling    = "not-sibling"
	CodeRankCollision = "rank-collision"
)

// Add creates a node of type typ with a generated, collision-free ID and a
// rank after its last sibling, together with its outgoing edges (an edge whose
// From is empty starts at the new node), each given a generated edge ID. The
// node's ID is drawn before any edge ID, so a seeded sequence of commands
// yields the same node IDs whatever edges they carry. The resulting graph is
// validated as a whole; on any error g is left unchanged and the errors are
// returned. The returned edges carry their IDs.
func (g *Graph) Add(typ string, fields map[string]any, edges []Edge) (Node, []Edge, []ValidationError) {
	nt, ok := schema.Registry.Node(typ)
	if !ok {
		return Node{}, nil, []ValidationError{{
			Code: CodeUnregisteredType, Path: "$.nodes", Field: "type",
			Message: "node type " + strconv.Quote(typ) + " is not registered", Value: typ,
		}}
	}
	if nt.Singleton() {
		return Node{}, nil, []ValidationError{{
			Code: CodeNotAddable, Path: "$.nodes", Field: "type",
			Message: "the " + typ + " node is created with the plan and cannot be added", Value: typ,
		}}
	}

	taken := g.takenIDs()
	id, err := NewID(nt.Prefix, g.random(), taken.has)
	if err != nil {
		return Node{}, nil, []ValidationError{{Code: CodeIDSpace, Path: "$.nodes", Field: "id", Message: err.Error()}}
	}
	taken[id] = true

	own := make([]Edge, len(edges))
	for i, e := range edges {
		if e.From == "" {
			e.From = id
		}
		eid, err := NewID(EdgePrefix, g.random(), taken.has)
		if err != nil {
			return Node{}, nil, []ValidationError{{Code: CodeIDSpace, Path: "$.edges", Field: "id", Message: err.Error()}}
		}
		taken[eid] = true
		e.ID = eid
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
	if errs := g.commit(c); len(errs) > 0 {
		return Node{}, nil, errs
	}
	return node, own, nil
}

// idSet is the IDs already used in a plan: nodes (retired included) and
// edges share one space.
type idSet map[string]bool

func (s idSet) has(id string) bool { return s[id] }

func (g *Graph) takenIDs() idSet {
	s := idSet{}
	for _, n := range g.Nodes {
		s[n.ID] = true
	}
	for _, e := range g.Edges {
		s[e.ID] = true
	}
	return s
}

// lastSiblingRank returns the highest rank among the new node's siblings.
func (g *Graph) lastSiblingRank(nt schema.NodeType, edges []Edge) string {
	last := ""
	for _, n := range g.siblings(nt, scopeTarget(nt, edges)) {
		last = max(last, n.Rank)
	}
	return last
}

// scopeTarget returns the target of the first RankScope edge among edges
// (sorted), or "" when the type has no RankScope or no such edge.
func scopeTarget(nt schema.NodeType, edges []Edge) string {
	if nt.RankScope == "" {
		return ""
	}
	targets := []string{}
	for _, e := range edges {
		if e.Type == nt.RankScope {
			targets = append(targets, e.To)
		}
	}
	if len(targets) == 0 {
		return ""
	}
	return slices.Min(targets)
}

// siblings returns the nodes of type nt that share reading order: every node
// of the type or — when scope is set — those whose RankScope edge targets
// scope. Retired nodes are included so ranks stay distinct.
func (g *Graph) siblings(nt schema.NodeType, scope string) []Node {
	inScope := map[string]bool{}
	if scope != "" {
		for _, e := range g.Edges {
			if e.Type == nt.RankScope && e.To == scope {
				inScope[e.From] = true
			}
		}
	}
	var out []Node
	for _, n := range g.Nodes {
		if n.Type == nt.Name && (scope == "" || inScope[n.ID]) {
			out = append(out, n)
		}
	}
	return out
}

// outgoing returns the edges that start at id.
func (g *Graph) outgoing(id string) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		if e.From == id {
			out = append(out, e)
		}
	}
	return out
}

func (g *Graph) nodeIndex(id string) int {
	return slices.IndexFunc(g.Nodes, func(n Node) bool { return n.ID == id })
}

func notFound(id string) []ValidationError {
	return []ValidationError{{
		Code: CodeNodeNotFound, Path: NodePath(id), Field: "id",
		Message: "no node " + strconv.Quote(id) + " in this plan", Value: id,
	}}
}

// commit validates c as a whole and, when it is valid, makes it g's content.
// On any error g is left unchanged.
func (g *Graph) commit(c *Graph) []ValidationError {
	if errs := Validate(c); len(errs) > 0 {
		return errs
	}
	g.Nodes, g.Edges = c.Nodes, c.Edges
	g.Canonicalize()
	return nil
}

// Update merges fields into node id and returns the fields whose value
// changed (a removed field maps to nil). A nil value removes a field; a map
// value for an object field merges member by member (a nil member removes
// it, and an object left empty is removed). Fixed fields cannot change. The
// resulting graph is validated as a whole; on any error g is left unchanged.
func (g *Graph) Update(id string, fields map[string]any) (map[string]any, []ValidationError) {
	i := g.nodeIndex(id)
	if i < 0 {
		return nil, notFound(id)
	}
	n := g.Nodes[i]
	nt, registered := schema.Registry.Node(n.Type)

	next := maps.Clone(n.Fields)
	if next == nil {
		next = map[string]any{}
	}
	var errs []ValidationError
	for _, k := range slices.Sorted(maps.Keys(fields)) {
		val := fields[k]
		spec, known := nt.Field(k)
		if registered && known && spec.Fixed {
			errs = append(errs, ValidationError{
				Code: CodeFixedField, Path: NodePath(id) + ".fields." + k, Field: k,
				Message: k + " is set when the node is created and cannot be updated", Value: val,
			})
			continue
		}
		members, isMap := val.(map[string]any)
		if known && spec.Kind == schema.KindObject && isMap {
			obj, _ := next[k].(map[string]any)
			obj = maps.Clone(obj)
			if obj == nil {
				obj = map[string]any{}
			}
			for mk, mv := range members {
				if mv == nil {
					delete(obj, mk)
				} else {
					obj[mk] = mv
				}
			}
			if len(obj) == 0 {
				delete(next, k)
			} else {
				next[k] = obj
			}
			continue
		}
		if val == nil {
			delete(next, k)
		} else {
			next[k] = val
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}

	changed := map[string]any{}
	for _, k := range slices.Sorted(maps.Keys(fields)) {
		if !reflect.DeepEqual(n.Fields[k], next[k]) {
			changed[k] = next[k]
		}
	}

	c := g.clone()
	c.Nodes[i].Fields = next
	if errs := g.commit(c); len(errs) > 0 {
		return nil, errs
	}
	return changed, nil
}

// Unlink removes the edge from → to of type typ. The resulting graph is
// validated as a whole; on any error g is left unchanged.
func (g *Graph) Unlink(from, typ, to string) (Edge, []ValidationError) {
	e := Edge{From: from, Type: typ, To: to}
	j := slices.IndexFunc(g.Edges, func(x Edge) bool { return x.From == from && x.Type == typ && x.To == to })
	if j < 0 {
		return Edge{}, []ValidationError{{
			Code: CodeEdgeNotFound, Path: EdgePath(e), Field: "",
			Message: "no edge " + from + " " + typ + " " + to + " in this plan",
		}}
	}
	return g.unlinkAt(j)
}

// UnlinkID removes the edge with ID id; see Unlink.
func (g *Graph) UnlinkID(id string) (Edge, []ValidationError) {
	j := slices.IndexFunc(g.Edges, func(x Edge) bool { return x.ID == id })
	if j < 0 {
		return Edge{}, []ValidationError{{
			Code: CodeEdgeNotFound, Path: "$.edges", Field: "id", Value: id,
			Message: "no edge " + strconv.Quote(id) + " in this plan",
		}}
	}
	return g.unlinkAt(j)
}

func (g *Graph) unlinkAt(j int) (Edge, []ValidationError) {
	e := g.Edges[j]
	c := g.clone()
	c.Edges = slices.Delete(c.Edges, j, j+1)
	if errs := g.commit(c); len(errs) > 0 {
		return Edge{}, errs
	}
	return e, nil
}

// Retire sets node id's status to retired. The ID, fields, rank and edges are
// kept, so references and history survive; retiring twice is a no-op. The
// plan node cannot be retired.
func (g *Graph) Retire(id string) (Node, []ValidationError) {
	i := g.nodeIndex(id)
	if i < 0 {
		return Node{}, notFound(id)
	}
	if nt, ok := schema.Registry.Node(g.Nodes[i].Type); ok && nt.Singleton() {
		return Node{}, []ValidationError{{
			Code: CodeNotRetirable, Path: NodePath(id), Field: "status",
			Message: "the " + nt.Name + " node cannot be retired", Value: id,
		}}
	}
	c := g.clone()
	c.Nodes[i].Status = StatusRetired
	node := c.Nodes[i]
	if errs := g.commit(c); len(errs) > 0 {
		return Node{}, errs
	}
	return node, nil
}

// Move gives node id one new rank so it sorts directly before (or, with
// after set, directly after) its sibling anchor. No other node changes. The
// anchor must be a sibling: the same type and, for a type with a RankScope,
// the same scope target.
func (g *Graph) Move(id, anchor string, after bool) (Node, []ValidationError) {
	i := g.nodeIndex(id)
	if i < 0 {
		return Node{}, notFound(id)
	}
	n := g.Nodes[i]
	nt, ok := schema.Registry.Node(n.Type)
	if !ok || nt.Singleton() {
		return Node{}, []ValidationError{{
			Code: CodeNotMovable, Path: NodePath(id), Field: "rank",
			Message: "a " + n.Type + " node has no reading order to change", Value: id,
		}}
	}
	if _, exists := g.NodeByID(anchor); !exists {
		return Node{}, notFound(anchor)
	}
	sibs := g.siblings(nt, scopeTarget(nt, g.outgoing(id)))
	sibs = slices.DeleteFunc(sibs, func(s Node) bool { return s.ID == id })
	slices.SortFunc(sibs, func(a, b Node) int { return cmp.Or(cmp.Compare(a.Rank, b.Rank), cmp.Compare(a.ID, b.ID)) })
	k := slices.IndexFunc(sibs, func(s Node) bool { return s.ID == anchor })
	if k < 0 {
		return Node{}, []ValidationError{{
			Code: CodeNotSibling, Path: NodePath(id), Field: "rank",
			Message: anchor + " is not a sibling of " + id + " (siblings share a type" + scopeNote(nt) + ")", Value: anchor,
		}}
	}
	lo, hi := "", sibs[k].Rank
	if k > 0 {
		lo = sibs[k-1].Rank
	}
	if after {
		lo, hi = sibs[k].Rank, ""
		if k+1 < len(sibs) {
			hi = sibs[k+1].Rank
		}
	}
	rank, err := RankBetween(lo, hi)
	if err != nil {
		return Node{}, []ValidationError{{
			Code: CodeRankCollision, Path: NodePath(id) + ".rank", Field: "rank",
			Message: "no rank fits between " + strconv.Quote(lo) + " and " + strconv.Quote(hi) + ": " + err.Error(), Value: anchor,
		}}
	}
	c := g.clone()
	c.Nodes[i].Rank = rank
	node := c.Nodes[i]
	if errs := g.commit(c); len(errs) > 0 {
		return Node{}, errs
	}
	return node, nil
}

func scopeNote(nt schema.NodeType) string {
	if nt.RankScope == "" {
		return ""
	}
	return " and the same " + nt.RankScope + " target"
}

// Link adds one typed edge with a generated edge ID. The resulting graph is
// validated as a whole; on any error g is left unchanged.
func (g *Graph) Link(from, typ, to string) (Edge, []ValidationError) {
	id, err := NewID(EdgePrefix, g.random(), g.takenIDs().has)
	if err != nil {
		return Edge{}, []ValidationError{{Code: CodeIDSpace, Path: "$.edges", Field: "id", Message: err.Error()}}
	}
	e := Edge{ID: id, From: from, Type: typ, To: to}
	c := g.clone()
	c.Edges = append(c.Edges, e)
	if errs := g.commit(c); len(errs) > 0 {
		return Edge{}, errs
	}
	return e, nil
}
