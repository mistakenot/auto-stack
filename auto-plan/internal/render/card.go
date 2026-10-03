package render

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
)

// DescribeWidth is how many runes of each text field `describe` keeps.
const DescribeWidth = 200

// NodeTitle is the one-line name of a node: its title, or for types without
// one, the field that identifies it (a file's path, a leg's actor and action,
// a plan's name, a child plan's number).
func NodeTitle(n graph.Node) string {
	if t := n.StringField("title"); t != "" {
		return t
	}
	switch n.Type {
	case "file":
		return n.StringField("path")
	case "leg":
		return strings.TrimSpace(n.StringField("actor") + ": " + n.StringField("action"))
	case "plan":
		return n.StringField("name")
	case "child":
		if p := n.StringField("plan"); p != "" {
			return "plan " + p
		}
	}
	return ""
}

// GetCommand is the exact command that prints a node in full.
func GetCommand(planID, id string) string { return "auto plan get " + planID + " " + id }

// DescribeView is the cheap summary rung of one node: identity, title, edge
// counts by type, and every field with text cut to DescribeWidth runes. Get
// is the command that recovers the full node.
type DescribeView struct {
	Plan   string         `json:"plan"`
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Term   string         `json:"term"`
	Status string         `json:"status"`
	Rank   string         `json:"rank"`
	Title  string         `json:"title"`
	Fields map[string]any `json:"fields"`
	// Truncated lists the fields (object members as field.member) that were cut.
	Truncated []string `json:"truncated"`
	// Edges counts edges by direction and type: {"out":{"proves":1},"in":{…}}.
	Edges EdgeCounts `json:"edges"`
	Get   string     `json:"get"`

	order []string
}

// EdgeCounts counts a node's edges by type, per direction.
type EdgeCounts struct {
	Out map[string]int `json:"out"`
	In  map[string]int `json:"in"`
}

// Describe builds the summary rung for node id. ok is false when the plan
// has no such node. Edge counts include edges other plans hold into the node
// (plans may be nil).
func Describe(planID string, g *graph.Graph, id string, plans Plans) (DescribeView, bool) {
	n, ok := g.NodeByID(id)
	if !ok {
		return DescribeView{}, false
	}
	v := DescribeView{
		Plan: planID, ID: n.ID, Type: n.Type, Term: term(n.Type), Status: n.Status, Rank: n.Rank,
		Title: NodeTitle(n), Fields: map[string]any{}, Truncated: []string{},
		Edges: EdgeCounts{Out: map[string]int{}, In: map[string]int{}},
		Get:   GetCommand(planID, n.ID),
		order: fieldOrder(n),
	}
	for _, name := range v.order {
		v.Fields[name] = truncateValue(name, n.Fields[name], &v.Truncated)
	}
	for _, e := range g.Edges {
		if e.From == n.ID {
			v.Edges.Out[e.Type]++
		}
		if e.To == n.ID {
			v.Edges.In[e.Type]++
		}
	}
	for _, x := range incoming(plans, planID, n.ID) {
		v.Edges.In[x.edge.Type]++
	}
	return v, true
}

// truncateValue cuts strings (and strings inside lists and objects) to
// DescribeWidth runes, recording each cut field's path.
func truncateValue(path string, v any, cut *[]string) any {
	switch x := v.(type) {
	case string:
		if utf8.RuneCountInString(x) > DescribeWidth {
			if !slices.Contains(*cut, path) {
				*cut = append(*cut, path)
			}
			return string([]rune(x)[:DescribeWidth]) + "…"
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i, el := range x {
			out[i] = truncateValue(path, el, cut)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for _, k := range slices.Sorted(maps.Keys(x)) {
			out[k] = truncateValue(path+"."+k, x[k], cut)
		}
		return out
	}
	return v
}

// Text renders the summary: a header, the fields, edge counts, then the get
// command (always printed; flagged when something was cut).
func (v DescribeView) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s  %s · %s · rank %s\n", Glyph(v.Type), v.ID, v.Term, v.Status, orDash(v.Rank))
	writeFields(&b, v.order, v.Fields)
	b.WriteString("\nedges\n")
	if len(v.Edges.Out)+len(v.Edges.In) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, dir := range []struct {
		arrow string
		m     map[string]int
	}{{"out →", v.Edges.Out}, {"in  ←", v.Edges.In}} {
		types := slices.Collect(maps.Keys(dir.m))
		slices.SortFunc(types, func(a, b string) int { return cmp.Or(cmp.Compare(edgeOrder(a), edgeOrder(b)), cmp.Compare(a, b)) })
		for _, typ := range types {
			fmt.Fprintf(&b, "  %s %s ×%d\n", dir.arrow, typ, dir.m[typ])
		}
	}
	b.WriteString("\n")
	if len(v.Truncated) > 0 {
		b.WriteString("truncated: " + strings.Join(v.Truncated, ", ") + "; full node: " + v.Get + "\n")
	} else {
		b.WriteString("full node: " + v.Get + "\n")
	}
	return b.String()
}

// CardView is the full-fidelity node: every field, plus each incoming and
// outgoing edge with the neighbour's type and title (what `get` prints).
type CardView struct {
	Plan   string         `json:"plan"`
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Term   string         `json:"term"`
	Status string         `json:"status"`
	Rank   string         `json:"rank"`
	Title  string         `json:"title"`
	Fields map[string]any `json:"fields"`
	Out    []EdgeRow      `json:"out"`
	In     []EdgeRow      `json:"in"`

	order []string
}

// EdgeRow is one edge seen from a node: the edge's ID and type and the
// neighbour at the other end. A qualified neighbour (`005-k7q2:r-8hw3`)
// lives in another plan: its type, title and status are resolved through the
// plan set, and stay empty when it does not resolve. An edge another plan
// holds into the node carries that plan's edge ID.
type EdgeRow struct {
	EdgeID    string `json:"edge_id"`
	Edge      string `json:"edge"`
	ID        string `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Qualified bool   `json:"qualified,omitempty"`
}

// Card builds the full node card for id. ok is false when there is no such
// node. With plans, qualified neighbours are resolved and the edges other
// plans hold into the node are listed under In as qualified rows.
func Card(planID string, g *graph.Graph, id string, plans Plans) (CardView, bool) {
	n, ok := g.NodeByID(id)
	if !ok {
		return CardView{}, false
	}
	v := CardView{
		Plan: planID, ID: n.ID, Type: n.Type, Term: term(n.Type), Status: n.Status, Rank: n.Rank,
		Title: NodeTitle(n), Fields: map[string]any{}, Out: []EdgeRow{}, In: []EdgeRow{},
		order: fieldOrder(n),
	}
	maps.Copy(v.Fields, n.Fields)
	for _, e := range g.Edges {
		if e.From == n.ID {
			v.Out = append(v.Out, neighbour(g, plans, e.ID, e.Type, e.To))
		}
		if e.To == n.ID {
			v.In = append(v.In, neighbour(g, plans, e.ID, e.Type, e.From))
		}
	}
	for _, x := range incoming(plans, planID, n.ID) {
		v.In = append(v.In, neighbour(g, plans, x.edge.ID, x.edge.Type, graph.Qualify(x.plan, x.edge.From)))
	}
	sortEdgeRows(v.Out)
	sortEdgeRows(v.In)
	return v, true
}

// neighbour describes the node at the other end of an edge.
func neighbour(g *graph.Graph, plans Plans, edgeID, edge, ref string) EdgeRow {
	row := EdgeRow{EdgeID: edgeID, Edge: edge, ID: ref}
	if _, _, qualified := graph.ParseRef(ref); qualified {
		row.Qualified = true
		if n, ok := lookup(plans, ref); ok {
			row.Type, row.Title, row.Status = n.Type, NodeTitle(n), n.Status
		}
		return row
	}
	if n, ok := g.NodeByID(ref); ok {
		row.Type, row.Title, row.Status = n.Type, NodeTitle(n), n.Status
	}
	return row
}

// sortEdgeRows orders edges by registry edge order, then neighbour ID.
func sortEdgeRows(rows []EdgeRow) {
	slices.SortFunc(rows, func(a, b EdgeRow) int {
		return cmp.Or(cmp.Compare(edgeOrder(a.Edge), edgeOrder(b.Edge)), cmp.Compare(a.Edge, b.Edge), cmp.Compare(a.ID, b.ID))
	})
}

func edgeOrder(typ string) int {
	i := slices.IndexFunc(schema.Registry.Edges, func(e schema.EdgeType) bool { return e.Name == typ })
	if i < 0 {
		return len(schema.Registry.Edges)
	}
	return i
}

// Text renders the card: a header, every field in registry order, then the
// outgoing and incoming edges with aligned columns.
func (v CardView) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s  %s · %s · rank %s\n", Glyph(v.Type), v.ID, v.Term, v.Status, orDash(v.Rank))
	writeFields(&b, v.order, v.Fields)
	writeEdgeRows(&b, "out", "→", v.Out)
	writeEdgeRows(&b, "in", "←", v.In)
	return b.String()
}

func writeEdgeRows(b *strings.Builder, title, arrow string, rows []EdgeRow) {
	b.WriteString("\n" + title + "\n")
	if len(rows) == 0 {
		b.WriteString("  (none)\n")
		return
	}
	eidW, edgeW, idW := 0, 0, 0
	for _, r := range rows {
		eidW = max(eidW, len(r.EdgeID))
		edgeW = max(edgeW, len(r.Edge))
		idW = max(idW, len(r.ID))
	}
	for _, r := range rows {
		glyph, rest := Glyph(r.Type), r.Title
		switch {
		case r.Qualified && r.Type == "":
			glyph, rest = "⇢", "(in plan "+strings.SplitN(r.ID, ":", 2)[0]+")"
		case r.Type == "":
			rest = "(missing)"
		case r.Status == graph.StatusRetired:
			rest += "  (retired)"
		}
		fmt.Fprintf(b, "  %s  %s %s %s %s  %s\n", pad(r.EdgeID, eidW), pad(r.Edge, edgeW), arrow, glyph, pad(r.ID, idW), rest)
	}
}

// writeFields prints name/value pairs in order with an aligned value column.
// Multi-line text continues on indented lines; lists print one "- item" per
// line under a single name;
// objects print as name.member.
func writeFields(b *strings.Builder, order []string, fields map[string]any) {
	type kv struct{ k, v string }
	var rows []kv
	var add func(name string, v any)
	add = func(name string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for _, k := range slices.Sorted(maps.Keys(x)) {
				add(name+"."+k, x[k])
			}
		case []any:
			if len(x) == 0 {
				rows = append(rows, kv{name, "(empty)"})
			}
			for i, el := range x {
				key := name
				if i > 0 {
					key = "" // one list, one key
				}
				rows = append(rows, kv{key, "- " + fmt.Sprint(el)})
			}
		case string:
			rows = append(rows, kv{name, x})
		default:
			rows = append(rows, kv{name, fmt.Sprint(x)})
		}
	}
	for _, name := range order {
		add(name, fields[name])
	}
	w := 0
	for _, r := range rows {
		w = max(w, len(r.k))
	}
	for _, r := range rows {
		lines := strings.Split(r.v, "\n")
		b.WriteString("  " + pad(r.k, w) + "  " + lines[0] + "\n")
		for _, l := range lines[1:] {
			b.WriteString("  " + strings.Repeat(" ", w) + "  " + l + "\n")
		}
	}
}

// fieldOrder lists n's fields: registry order first, then unknown fields
// sorted.
func fieldOrder(n graph.Node) []string {
	var out []string
	seen := map[string]bool{}
	if nt, ok := schema.Registry.Node(n.Type); ok {
		for i := range nt.Fields {
			name := nt.Fields[i].Name
			if _, has := n.Fields[name]; has {
				out = append(out, name)
				seen[name] = true
			}
		}
	}
	for _, k := range slices.Sorted(maps.Keys(n.Fields)) {
		if !seen[k] {
			out = append(out, k)
		}
	}
	return out
}

// term is a node type's glossary term, or the type name when unregistered.
func term(typ string) string {
	if nt, ok := schema.Registry.Node(typ); ok {
		return nt.Term
	}
	return typ
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
