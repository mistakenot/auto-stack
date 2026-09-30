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
//	constrains  decision → goal|ac          down
//	rejects     decision → alternative      down
//	touches     stage → file                down
//	addresses   goal → defect               down
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
	"constrains": Down,
	"rejects":    Down,
	"touches":    Down,
	"addresses":  Down,
}

// TraceView is the lineage of one node: indented walks over the edges up to
// the roots and down to the leaves. Retired neighbours are left out. A
// qualified neighbour (`005:r-8hw3`) is a leaf printed as its raw reference.
type TraceView struct {
	Plan      string      `json:"plan"`
	Direction Direction   `json:"direction"`
	Root      TraceStep   `json:"root"`
	Up        []TraceStep `json:"up"`
	Down      []TraceStep `json:"down"`
}

// TraceStep is one hop of a walk. Dir is "out" when the walk followed the
// edge from → to and "in" when it followed it backward. Seen marks a node
// already printed earlier in the same walk; it is not expanded again.
type TraceStep struct {
	Edge      string      `json:"edge,omitempty"`
	Dir       string      `json:"dir,omitempty"`
	ID        string      `json:"id"`
	Type      string      `json:"type"`
	Title     string      `json:"title"`
	Qualified bool        `json:"qualified,omitempty"`
	Seen      bool        `json:"seen,omitempty"`
	Children  []TraceStep `json:"children,omitempty"`
}

// Trace walks the lineage of node id. ok is false when there is no such node.
func Trace(planID string, g *graph.Graph, id string, dir Direction) (TraceView, bool) {
	n, ok := g.NodeByID(id)
	if !ok {
		return TraceView{}, false
	}
	ix := newIndex(g)
	v := TraceView{
		Plan: planID, Direction: dir,
		Root: TraceStep{ID: n.ID, Type: n.Type, Title: NodeTitle(n)},
		Up:   []TraceStep{}, Down: []TraceStep{},
	}
	if dir == Up || dir == Both {
		v.Up = ix.walk(n.ID, Up, map[string]bool{n.ID: true})
	}
	if dir == Down || dir == Both {
		v.Down = ix.walk(n.ID, Down, map[string]bool{n.ID: true})
	}
	return v, true
}

// walk returns the hops from id in direction want, depth first. seen holds
// the nodes already printed in this walk.
func (ix *index) walk(id string, want Direction, seen map[string]bool) []TraceStep {
	type hop struct {
		edge, dir string
		ref       string
	}
	var hops []hop
	for _, e := range ix.g.Edges {
		d, traced := TraceEdges[e.Type]
		if !traced {
			continue
		}
		switch {
		case e.From == id && d == want:
			hops = append(hops, hop{e.Type, "out", e.To})
		case e.To == id && d != want:
			hops = append(hops, hop{e.Type, "in", e.From})
		}
	}

	steps := []TraceStep{}
	for _, h := range hops {
		if _, _, qualified := graph.ParseRef(h.ref); qualified {
			steps = append(steps, TraceStep{Edge: h.edge, Dir: h.dir, ID: h.ref, Qualified: true})
			continue
		}
		n, ok := ix.active[h.ref]
		if !ok {
			continue // retired or dangling: not lineage
		}
		steps = append(steps, TraceStep{Edge: h.edge, Dir: h.dir, ID: n.ID, Type: n.Type, Title: NodeTitle(n)})
	}
	slices.SortFunc(steps, func(a, b TraceStep) int {
		return cmp.Or(
			cmp.Compare(edgeOrder(a.Edge), edgeOrder(b.Edge)),
			cmp.Compare(b.Dir, a.Dir), // out before in
			cmp.Compare(ix.active[a.ID].Rank, ix.active[b.ID].Rank),
			cmp.Compare(a.ID, b.ID),
		)
	})
	steps = slices.CompactFunc(steps, func(a, b TraceStep) bool {
		return a.Edge == b.Edge && a.Dir == b.Dir && a.ID == b.ID
	})
	for i := range steps {
		s := &steps[i]
		if s.Qualified {
			continue
		}
		if seen[s.ID] {
			s.Seen = true
			continue
		}
		seen[s.ID] = true
		s.Children = ix.walk(s.ID, want, seen)
		if len(s.Children) == 0 {
			s.Children = nil
		}
	}
	return steps
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
		for _, s := range steps {
			edgeW = max(edgeW, len(s.Edge))
			idW = max(idW, len(s.ID))
			measure(s.Children)
		}
	}
	measure(v.Up)
	measure(v.Down)

	var write func(steps []TraceStep, depth int)
	write = func(steps []TraceStep, depth int) {
		for _, s := range steps {
			arrow := "→"
			if s.Dir == "in" {
				arrow = "←"
			}
			glyph, title := Glyph(s.Type), s.Title
			switch {
			case s.Qualified:
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
