package render

import (
	"cmp"
	"slices"
	"strings"

	"github.com/mistakenot/auto-plan/internal/graph"
)

// Direction is which way a trace walks: up toward the roots (goals, epic
// rails and legs) or down toward the leaves (stages, files).
type Direction string

const (
	Up   Direction = "up"
	Down Direction = "down"
	// Both walks up and then down from the same node.
	Both Direction = "both"
)

// TraceEdges is the one table of trace semantics: for each edge type, the
// direction a walk goes when it follows the edge forward (from → to). The
// reverse hop goes the other way. Walking up from an AC follows `proves`
// forward to its goal and `constrains` backward to the decisions over it;
// walking down follows `covers` backward to the stages that make it pass,
// then `touches` forward to their files.
//
//	edge        from → to                   forward is
//	proves      ac → goal                   up
//	covers      stage → ac                  up
//	discharges  ac → rail (NNN:ID)          up
//	about       tree → any                  up
//	in          leg → journey               up
//	honors      plan → rail (NNN:ID)        up
//	delivers    plan → leg (NNN:ID)         up
//	builds-on   plan → decision (NNN:ID)    up
//	child-of    goal → epic goal (derived)  up
//	constrains  decision → goal|ac          down
//	rejects     decision → alternative      down
//	touches     stage → file                down
//	addresses   goal → defect               down
//
// Walks cross plan boundaries. A qualified edge target (`005:r-8hw3`) is
// followed into plan 005, and a walk down from a node also follows the
// qualified edges other plans hold into it (down from an epic rail: the
// child plans that honour it and the ACs that discharge it). Nodes in other
// plans print qualified; one that does not resolve is a raw-reference leaf.
//
// child-of is the one derived hop, not a registry edge: it is how a task's
// lineage reaches its epic (AC-14: `ac → goal (proves) → epic goal via the
// child plan`). No edge links a task goal to a particular epic goal, so the
// honest model is the child link itself: a goal of plan P steps up to every
// active goal of P's epic E when the link is consistent both ways — P's
// `epic` is E and E has an active child node naming P (the hop's Via). Down
// from an epic goal, the same link steps to every active goal of each such
// child plan. A broken link (child-epic-mismatch) is not followed.
//
// Three edge types are not lineage and are never traced: wouldBreak (a
// counterfactual), supersedes (decision history) and dependsOn (ordering
// between stages or child plans).
var TraceEdges = map[string]Direction{
	"proves":     Up,
	"covers":     Up,
	"discharges": Up,
	"about":      Up,
	"in":         Up,
	"honors":     Up,
	"delivers":   Up,
	"builds-on":  Up,
	EdgeChildOf:  Up,
	"constrains": Down,
	"rejects":    Down,
	"touches":    Down,
	"addresses":  Down,
}

// EdgeChildOf names trace's derived hop from a child plan's goal to its
// epic's goals (see TraceEdges).
const EdgeChildOf = "child-of"

// TraceView is the lineage of one node: indented walks over the edges up to
// the roots and down to the leaves. Retired neighbours are left out. Nodes in
// other plans carry qualified IDs (`005:g-k7q2`); a qualified neighbour that
// does not resolve is a leaf printed as its raw reference.
type TraceView struct {
	Plan      string      `json:"plan"`
	Direction Direction   `json:"direction"`
	Root      TraceStep   `json:"root"`
	Up        []TraceStep `json:"up"`
	Down      []TraceStep `json:"down"`
}

// TraceStep is one hop of a walk. Dir is "out" when the walk followed the
// edge from → to and "in" when it followed it backward. Qualified marks a
// node in another plan (ID is then NNN:ID; Type and Title are empty when it
// does not resolve). Via is the child node a child-of hop went through. Seen
// marks a node already printed earlier in the same walk; it is not expanded
// again.
type TraceStep struct {
	Edge      string      `json:"edge,omitempty"`
	Dir       string      `json:"dir,omitempty"`
	ID        string      `json:"id"`
	Type      string      `json:"type"`
	Title     string      `json:"title"`
	Qualified bool        `json:"qualified,omitempty"`
	Via       string      `json:"via,omitempty"`
	Seen      bool        `json:"seen,omitempty"`
	Children  []TraceStep `json:"children,omitempty"`

	plan, local, rank string
}

// Trace walks the lineage of node id. ok is false when there is no such node.
// plans (may be nil) lets the walk cross into other plans.
func Trace(planID string, g *graph.Graph, id string, dir Direction, plans Plans) (TraceView, bool) {
	n, ok := g.NodeByID(id)
	if !ok {
		return TraceView{}, false
	}
	t := &tracer{root: planID, plans: plans, ix: map[string]*index{planID: newIndex(g)}}
	v := TraceView{
		Plan: planID, Direction: dir,
		Root: TraceStep{ID: n.ID, Type: n.Type, Title: NodeTitle(n)},
		Up:   []TraceStep{}, Down: []TraceStep{},
	}
	key := graph.Qualify(planID, n.ID)
	if dir == Up || dir == Both {
		v.Up = t.walk(planID, n.ID, Up, map[string]bool{key: true})
	}
	if dir == Down || dir == Both {
		v.Down = t.walk(planID, n.ID, Down, map[string]bool{key: true})
	}
	return v, true
}

// tracer walks lineage across the plans of a set.
type tracer struct {
	root  string
	plans Plans
	ix    map[string]*index
}

// index returns plan planID's index, or nil when it cannot be reached.
func (t *tracer) index(planID string) *index {
	if ix, ok := t.ix[planID]; ok {
		return ix
	}
	var ix *index
	if t.plans != nil {
		if g, ok := t.plans.Graph(planID); ok {
			ix = newIndex(g)
		}
	}
	t.ix[planID] = ix
	return ix
}

// display is how a node is named in the walk: plan-local in the root plan,
// qualified elsewhere.
func (t *tracer) display(planID, id string) string {
	if planID == t.root {
		return id
	}
	return graph.Qualify(planID, id)
}

// hop is one candidate step before it is resolved.
type hop struct {
	edge, dir string
	plan, id  string
	via       string
}

// walk returns the hops from plan planID's node id in direction want, depth
// first. seen holds the nodes (qualified) already printed in this walk.
func (t *tracer) walk(planID, id string, want Direction, seen map[string]bool) []TraceStep {
	ix := t.index(planID)
	var hops []hop
	for _, e := range ix.g.Edges {
		d, traced := TraceEdges[e.Type]
		if !traced {
			continue
		}
		switch {
		case e.From == id && d == want:
			to, toID, qualified := graph.ParseRef(e.To)
			if !qualified {
				to = planID
			}
			hops = append(hops, hop{edge: e.Type, dir: "out", plan: to, id: toID})
		case e.To == id && d != want:
			hops = append(hops, hop{edge: e.Type, dir: "in", plan: planID, id: e.From})
		}
	}
	for _, x := range incoming(t.plans, planID, id) {
		if d, traced := TraceEdges[x.edge.Type]; traced && d != want {
			hops = append(hops, hop{edge: x.edge.Type, dir: "in", plan: x.plan, id: x.edge.From})
		}
	}
	hops = append(hops, t.childOf(planID, id, want)...)

	steps := []TraceStep{}
	for _, h := range hops {
		tix := t.index(h.plan)
		if tix == nil {
			ref := graph.Qualify(h.plan, h.id)
			steps = append(steps, TraceStep{Edge: h.edge, Dir: h.dir, ID: ref, Qualified: true, plan: h.plan, local: h.id})
			continue
		}
		n, ok := tix.active[h.id]
		if !ok {
			continue // retired or dangling: not lineage
		}
		steps = append(steps, TraceStep{
			Edge: h.edge, Dir: h.dir, ID: t.display(h.plan, n.ID), Type: n.Type, Title: NodeTitle(n),
			Qualified: h.plan != t.root, Via: h.via, plan: h.plan, local: n.ID, rank: n.Rank,
		})
	}
	slices.SortFunc(steps, func(a, b TraceStep) int {
		return cmp.Or(
			cmp.Compare(edgeOrder(a.Edge), edgeOrder(b.Edge)),
			cmp.Compare(b.Dir, a.Dir), // out before in
			cmp.Compare(a.rank, b.rank),
			cmp.Compare(a.ID, b.ID),
		)
	})
	steps = slices.CompactFunc(steps, func(a, b TraceStep) bool {
		return a.Edge == b.Edge && a.Dir == b.Dir && a.ID == b.ID
	})
	for i := range steps {
		s := &steps[i]
		if s.Qualified && s.Type == "" {
			continue // unresolved: a raw-reference leaf
		}
		key := graph.Qualify(s.plan, s.local)
		if seen[key] {
			s.Seen = true
			continue
		}
		seen[key] = true
		s.Children = t.walk(s.plan, s.local, want, seen)
		if len(s.Children) == 0 {
			s.Children = nil
		}
	}
	return steps
}

// childOf returns the derived child-of hops of a goal (see TraceEdges): up
// to every goal of its plan's epic, or down to every goal of each child plan.
// Both require the epic ↔ child link to hold both ways.
func (t *tracer) childOf(planID, id string, want Direction) []hop {
	ix := t.index(planID)
	if n, ok := ix.active[id]; !ok || n.Type != "goal" {
		return nil
	}
	lists := func(epicIx *index, child string) (graph.Node, bool) {
		for _, c := range epicIx.byType("child") {
			if c.StringField("plan") == child {
				return c, true
			}
		}
		return graph.Node{}, false
	}
	var out []hop
	if want == Up {
		plan, _ := ix.g.Plan()
		epic := plan.StringField("epic")
		if epic == "" || epic == planID {
			return nil
		}
		eix := t.index(epic)
		if eix == nil {
			return nil
		}
		c, ok := lists(eix, planID)
		if !ok {
			return nil
		}
		for _, goal := range eix.byType("goal") {
			out = append(out, hop{edge: EdgeChildOf, dir: "out", plan: epic, id: goal.ID, via: t.display(epic, c.ID)})
		}
		return out
	}
	for _, c := range ix.byType("child") {
		child := c.StringField("plan")
		cix := t.index(child)
		if child == planID || cix == nil {
			continue
		}
		if p, _ := cix.g.Plan(); p.StringField("epic") != planID {
			continue
		}
		for _, goal := range cix.byType("goal") {
			out = append(out, hop{edge: EdgeChildOf, dir: "in", plan: child, id: goal.ID, via: t.display(planID, c.ID)})
		}
	}
	return out
}

// Text renders the root, then each requested walk as an indented tree:
// `edge → glyph id  title` when the edge was followed forward and `edge ←`
// when backward. A node met again is marked "(see above)".
func (v TraceView) Text() string {
	var b strings.Builder
	b.WriteString(Glyph(v.Root.Type) + " " + v.Root.ID + "  " + v.Root.Title + "\n")
	edgeW, idW := 0, 0
	var measure func([]TraceStep)
	measure = func(steps []TraceStep) {
		for i := range steps {
			s := &steps[i]
			edgeW = max(edgeW, len(s.Edge))
			idW = max(idW, len(s.ID))
			measure(s.Children)
		}
	}
	measure(v.Up)
	measure(v.Down)

	var write func(steps []TraceStep, depth int)
	write = func(steps []TraceStep, depth int) {
		for i := range steps {
			s := &steps[i]
			arrow := "→"
			if s.Dir == "in" {
				arrow = "←"
			}
			glyph, title := Glyph(s.Type), s.Title
			if s.Via != "" {
				title += "  (via " + s.Via + ")"
			}
			switch {
			case s.Qualified && s.Type == "":
				glyph, title = "⇢", "(in plan "+strings.SplitN(s.ID, ":", 2)[0]+")"
			case s.Seen:
				title += "  (see above)"
			}
			b.WriteString(strings.Repeat("  ", depth+1) + pad(s.Edge, edgeW) + " " + arrow + " " + glyph + " " + pad(s.ID, idW) + "  " + title + "\n")
			write(s.Children, depth+1)
		}
	}
	for _, walk := range []struct {
		dir   Direction
		steps []TraceStep
	}{{Up, v.Up}, {Down, v.Down}} {
		if v.Direction != Both && v.Direction != walk.dir {
			continue
		}
		b.WriteString("\n" + string(walk.dir) + "\n")
		if len(walk.steps) == 0 {
			b.WriteString("  (none)\n")
		}
		write(walk.steps, 0)
	}
	return b.String()
}
