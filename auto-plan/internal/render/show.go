// Package render builds the agent-facing views of a plan. Each view is one
// value that is encoded as JSON (the default) or rendered as text (--text),
// so the two forms carry the same facts and cannot diverge.
package render

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mistakenot/auto-plan/internal/graph"
)

// View is a value with a text rendering; its JSON form is its encoding.
type View interface {
	Text() string
}

// Plans is the set of plans a view can reach through qualified references
// (`005-k7q2:r-8hw3`) and epic ↔ child links. workspace.PlanSet implements
// it. A nil Plans reaches nothing: qualified neighbours stay raw references.
type Plans interface {
	// Graph returns the graph of the plan a plan ID (or a bare number one
	// plan has) names; ok is false when it is missing, ambiguous or
	// unreadable.
	Graph(planID string) (*graph.Graph, bool)
	// IDs lists every plan ID, sorted.
	IDs() []string
	// Family lists an epic's child plan IDs, sorted.
	Family(epic string) []string
}

// lookup resolves a qualified reference through plans.
func lookup(plans Plans, ref string) (graph.Node, bool) {
	planID, id, qualified := graph.ParseRef(ref)
	if !qualified || plans == nil {
		return graph.Node{}, false
	}
	g, ok := plans.Graph(planID)
	if !ok {
		return graph.Node{}, false
	}
	return g.NodeByID(id)
}

// crossEdge is an edge held by another plan whose qualified target is a node
// of this plan.
type crossEdge struct {
	plan string
	edge graph.Edge
}

// incoming returns the edges of every other plan that target plan planID's
// node id, in plan order then edge order.
func incoming(plans Plans, planID, id string) []crossEdge {
	if plans == nil {
		return nil
	}
	ref := graph.Qualify(planID, id)
	var out []crossEdge
	for _, p := range plans.IDs() {
		if p == planID {
			continue
		}
		g, ok := plans.Graph(p)
		if !ok {
			continue
		}
		for _, e := range g.Edges {
			if e.To == ref {
				out = append(out, crossEdge{plan: p, edge: e})
			}
		}
	}
	return out
}

// planEdgeTo reports whether plan planID has an edge `plan <typ> <to>`.
func planEdgeTo(plans Plans, planID, typ, to string) bool {
	g, ok := plans.Graph(planID)
	if !ok {
		return false
	}
	return slices.ContainsFunc(g.Edges, func(e graph.Edge) bool {
		return e.From == "plan" && e.Type == typ && e.To == to
	})
}

// Glyphs mark node types in text views (show-me notation).
const (
	GlyphGoal     = "◎"
	GlyphAC       = "✓"
	GlyphDecision = "◆"
	// GlyphRejected starts a rejected-alternative line on the goal ladder.
	GlyphRejected = "-"
)

// glyphs maps every registered node type to its glyph. A type missing here
// (an unregistered one) renders with "·".
var glyphs = map[string]string{
	"plan":        "▤",
	"goal":        GlyphGoal,
	"ac":          GlyphAC,
	"decision":    GlyphDecision,
	"alternative": "⊘",
	"rail":        "‖",
	"defect":      "✗",
	"stage":       "▶",
	"file":        "□",
	"question":    "?",
	"tree":        "≡",
	"journey":     "↝",
	"leg":         "↳",
	"child":       "▷",
}

// Glyph returns the text glyph of a node type.
func Glyph(typ string) string {
	if g, ok := glyphs[typ]; ok {
		return g
	}
	return "·"
}

// ShowView is the goal ladder: each goal, the ACs that prove it and the
// decisions that constrain it or its ACs (with the alternatives they
// rejected), then blocks for rails, defects and open questions.
//
// An epic reads goals → journeys → child plans: after the ladder come its
// journeys (each leg tagged with the child plans that deliver it, found by
// scanning the children's `delivers` edges) and its child plans with their
// lifecycle and dependsOn. Rails are tagged with the children that honour
// them. The blocks are data-driven, so a task plan that holds journeys or
// child nodes shows them too.
//
// Every row carries a positional label (G1, AC1.2, D3, A3.1, R1, DF1, Q1)
// for reading only: labels follow the current rank order, change when the
// plan is reordered, and are never accepted as a reference.
type ShowView struct {
	Plan      string `json:"plan"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Lifecycle string `json:"lifecycle"`
	// Epic is the epic plan this plan belongs to (plan.fields.epic).
	Epic      string        `json:"epic,omitempty"`
	Goals     []GoalRow     `json:"goals"`
	Journeys  []JourneyRow  `json:"journeys"`
	Children  []ChildRow    `json:"children"`
	Unlinked  UnlinkedRow   `json:"unlinked"`
	Rails     []RailRow     `json:"rails"`
	Defects   []DefectRow   `json:"defects"`
	Questions []QuestionRow `json:"questions"`
}

// GoalRow is one goal on the ladder.
type GoalRow struct {
	Label     string        `json:"label"`
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	ACs       []ACRow       `json:"acs"`
	Decisions []DecisionRow `json:"decisions"`
}

// ACRow is one acceptance criterion with its verify command.
type ACRow struct {
	Label  string `json:"label"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	Verify string `json:"verify,omitempty"`
}

// DecisionRow is one decision, the goal/AC IDs it constrains, and the
// alternatives it rejected.
type DecisionRow struct {
	Label        string   `json:"label"`
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Constrains   []string `json:"constrains"`
	Alternatives []AltRow `json:"alternatives"`
}

// AltRow is one rejected alternative. Why is the first line of the
// alternative's reason, truncated for the ladder; `auto plan get` has it all.
type AltRow struct {
	Label string `json:"label"`
	ID    string `json:"id"`
	Title string `json:"title"`
	Why   string `json:"why,omitempty"`
}

// RailRow is one rail, with the child plans that honour it and those
// excused from it for now.
type RailRow struct {
	Label     string   `json:"label"`
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	HonoredBy []string `json:"honoredBy"`
	Deferred  []string `json:"deferred"`
}

// JourneyRow is one journey and its legs in reading order.
type JourneyRow struct {
	Label string   `json:"label"`
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Legs  []LegRow `json:"legs"`
}

// LegRow is one leg, with the child plans that deliver it.
type LegRow struct {
	Label       string   `json:"label"`
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	DeliveredBy []string `json:"deliveredBy"`
}

// ChildRow is one child plan of an epic: the child node, the plan it names,
// that plan's name and lifecycle (Found is false when its folder is missing
// or unreadable), and the child nodes it depends on.
type ChildRow struct {
	Label     string   `json:"label"`
	ID        string   `json:"id"`
	Plan      string   `json:"plan"`
	Name      string   `json:"name"`
	Title     string   `json:"title"`
	Lifecycle string   `json:"lifecycle"`
	Found     bool     `json:"found"`
	DependsOn []string `json:"dependsOn"`
}

// DefectRow is one defect and the goals that address it.
type DefectRow struct {
	Label       string   `json:"label"`
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	AddressedBy []string `json:"addressedBy"`
}

// QuestionRow is one open question.
type QuestionRow struct {
	Label string `json:"label"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

// UnlinkedRow holds active ACs that prove no goal, decisions that constrain
// nothing, alternatives no decision rejects and legs in no journey, so
// nothing is invisible.
type UnlinkedRow struct {
	ACs          []ACRow       `json:"acs"`
	Decisions    []DecisionRow `json:"decisions"`
	Alternatives []AltRow      `json:"alternatives"`
	Legs         []LegRow      `json:"legs"`
}

// ladderWhyWidth is how many runes of an alternative's reason the ladder shows.
const ladderWhyWidth = 60

// Show builds the goal ladder for a plan. Retired nodes are left out. plans
// (may be nil) supplies the child plans an epic's journeys, children and
// rails are tagged with.
func Show(planID string, g *graph.Graph, plans Plans) ShowView {
	plan, _ := g.Plan()
	v := ShowView{
		Plan:      planID,
		Name:      plan.StringField("name"),
		Kind:      plan.StringField("kind"),
		Lifecycle: plan.StringField("lifecycle"),
		Epic:      plan.StringField("epic"),
		Goals:     []GoalRow{},
		Journeys:  []JourneyRow{},
		Children:  []ChildRow{},
		Unlinked:  UnlinkedRow{ACs: []ACRow{}, Decisions: []DecisionRow{}, Alternatives: []AltRow{}, Legs: []LegRow{}},
		Rails:     []RailRow{},
		Defects:   []DefectRow{},
		Questions: []QuestionRow{},
	}

	ix := newIndex(g)
	goalOfAC := map[string]string{}
	for _, e := range ix.activeEdges("proves") {
		if _, seen := goalOfAC[e.From]; !seen {
			goalOfAC[e.From] = e.To
		}
	}
	constrains := map[string][]string{}
	for _, e := range ix.activeEdges("constrains") {
		constrains[e.From] = append(constrains[e.From], e.To)
	}
	rejects := map[string][]string{}
	rejected := map[string]bool{}
	for _, e := range ix.activeEdges("rejects") {
		rejects[e.From] = append(rejects[e.From], e.To)
		rejected[e.To] = true
	}
	addressedBy := map[string][]string{}
	for _, e := range ix.activeEdges("addresses") {
		addressedBy[e.To] = append(addressedBy[e.To], e.From)
	}

	acs, decisions := ix.byType("ac"), ix.byType("decision")
	// Decisions are numbered by first appearance in reading order, so a
	// decision under two goals keeps one label.
	decisionLabel := map[string]string{}
	decisionRowFor := func(d graph.Node) DecisionRow {
		label, ok := decisionLabel[d.ID]
		if !ok {
			label = "D" + strconv.Itoa(len(decisionLabel)+1)
			decisionLabel[d.ID] = label
		}
		row := DecisionRow{Label: label, ID: d.ID, Title: d.StringField("title"), Constrains: sorted(constrains[d.ID]), Alternatives: []AltRow{}}
		for i, a := range ix.ordered(rejects[d.ID]) {
			row.Alternatives = append(row.Alternatives, altRow(a, "A"+strings.TrimPrefix(label, "D")+"."+strconv.Itoa(i+1)))
		}
		return row
	}

	for gi, goal := range ix.byType("goal") {
		gl := strconv.Itoa(gi + 1)
		row := GoalRow{Label: "G" + gl, ID: goal.ID, Title: goal.StringField("title"), ACs: []ACRow{}, Decisions: []DecisionRow{}}
		under := map[string]bool{goal.ID: true}
		for _, ac := range acs {
			if goalOfAC[ac.ID] == goal.ID {
				row.ACs = append(row.ACs, acRow(ac, "AC"+gl+"."+strconv.Itoa(len(row.ACs)+1)))
				under[ac.ID] = true
			}
		}
		for _, d := range decisions {
			if slices.ContainsFunc(constrains[d.ID], func(id string) bool { return under[id] }) {
				row.Decisions = append(row.Decisions, decisionRowFor(d))
			}
		}
		v.Goals = append(v.Goals, row)
	}
	for _, ac := range acs {
		if _, ok := goalOfAC[ac.ID]; !ok {
			v.Unlinked.ACs = append(v.Unlinked.ACs, acRow(ac, "AC?."+strconv.Itoa(len(v.Unlinked.ACs)+1)))
		}
	}
	for _, d := range decisions {
		if len(constrains[d.ID]) == 0 {
			v.Unlinked.Decisions = append(v.Unlinked.Decisions, decisionRowFor(d))
		}
	}
	for _, a := range ix.byType("alternative") {
		if !rejected[a.ID] {
			v.Unlinked.Alternatives = append(v.Unlinked.Alternatives, altRow(a, "A?."+strconv.Itoa(len(v.Unlinked.Alternatives)+1)))
		}
	}

	var family []string
	if plans != nil {
		family = plans.Family(planID)
	}
	by := func(typ, id string) []string {
		out := []string{}
		for _, p := range family {
			if planEdgeTo(plans, p, typ, graph.Qualify(planID, id)) {
				out = append(out, p)
			}
		}
		return out
	}
	v.Journeys, v.Unlinked.Legs = journeys(ix, by)
	v.Children = children(ix, plans)

	for i, r := range ix.byType("rail") {
		v.Rails = append(v.Rails, RailRow{
			Label: "R" + strconv.Itoa(i+1), ID: r.ID, Title: r.StringField("title"),
			HonoredBy: by("honors", r.ID), Deferred: stringList(r.Fields["deferred"]),
		})
	}
	for i, d := range ix.byType("defect") {
		v.Defects = append(v.Defects, DefectRow{Label: "DF" + strconv.Itoa(i+1), ID: d.ID, Title: d.StringField("title"), AddressedBy: sorted(addressedBy[d.ID])})
	}
	for _, q := range ix.byType("question") {
		if q.StringField("status") == "open" {
			v.Questions = append(v.Questions, QuestionRow{Label: "Q" + strconv.Itoa(len(v.Questions)+1), ID: q.ID, Title: q.StringField("title")})
		}
	}
	return v
}

// journeys groups the active legs under their journey (the first active
// journey a leg is `in`), in reading order; legs in no journey are returned
// separately. deliveredBy lists the plans with a `delivers` edge to a leg.
func journeys(ix *index, deliveredBy func(typ, id string) []string) ([]JourneyRow, []LegRow) {
	journeyOf := map[string]string{}
	for _, e := range ix.activeEdges("in") {
		if _, seen := journeyOf[e.From]; !seen {
			journeyOf[e.From] = e.To
		}
	}
	legRow := func(l graph.Node, label string) LegRow {
		return LegRow{Label: label, ID: l.ID, Title: NodeTitle(l), DeliveredBy: deliveredBy("delivers", l.ID)}
	}
	legs := ix.byType("leg")
	rows := []JourneyRow{}
	for ji, j := range ix.byType("journey") {
		jl := strconv.Itoa(ji + 1)
		row := JourneyRow{Label: "J" + jl, ID: j.ID, Title: j.StringField("title"), Legs: []LegRow{}}
		for _, l := range legs {
			if journeyOf[l.ID] == j.ID {
				row.Legs = append(row.Legs, legRow(l, "L"+jl+"."+strconv.Itoa(len(row.Legs)+1)))
			}
		}
		rows = append(rows, row)
	}
	unlinked := []LegRow{}
	for _, l := range legs {
		if _, ok := journeyOf[l.ID]; !ok {
			unlinked = append(unlinked, legRow(l, "L?."+strconv.Itoa(len(unlinked)+1)))
		}
	}
	return rows, unlinked
}

// children lists the active child nodes in reading order with the name and
// lifecycle of the plan each names.
func children(ix *index, plans Plans) []ChildRow {
	deps := map[string][]string{}
	for _, e := range ix.activeEdges("dependsOn") {
		deps[e.From] = append(deps[e.From], e.To)
	}
	rows := []ChildRow{}
	for i, c := range ix.byType("child") {
		row := ChildRow{
			Label: "C" + strconv.Itoa(i+1), ID: c.ID, Plan: c.StringField("plan"), Title: c.StringField("title"),
			DependsOn: []string{},
		}
		for _, d := range ix.ordered(deps[c.ID]) {
			row.DependsOn = append(row.DependsOn, d.ID)
		}
		if plans != nil {
			if g, ok := plans.Graph(row.Plan); ok {
				p, _ := g.Plan()
				row.Name, row.Lifecycle, row.Found = p.StringField("name"), p.StringField("lifecycle"), true
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func acRow(n graph.Node, label string) ACRow {
	return ACRow{Label: label, ID: n.ID, Title: n.StringField("title"), Verify: verifyCommand(n)}
}

func altRow(n graph.Node, label string) AltRow {
	return AltRow{Label: label, ID: n.ID, Title: n.StringField("title"), Why: firstLine(n.StringField("why"), ladderWhyWidth)}
}

// verifyCommand is an AC's verify command, "(manual)" for a manual check, or
// "" when the AC has none.
func verifyCommand(n graph.Node) string {
	verify := n.ObjectField("verify")
	if verify == nil {
		return ""
	}
	if c, _ := verify["cmd"].(string); c != "" {
		return c
	}
	if kind, _ := verify["kind"].(string); kind == "manual" {
		return "(manual)"
	}
	return ""
}

// line is one row of an indented ladder before column alignment.
type line struct {
	indent  int
	glyph   string
	label   string
	id      string
	title   string
	comment string
}

// block is a titled group of ladder lines.
type block struct {
	title string
	lines []line
}

// Text renders the ladder in show-me notation: glyph, positional label, ID
// and title, with verify commands and other notes in one aligned right-hand
// column.
func (v ShowView) Text() string {
	var ladder []line
	decisionLines := func(indent int, d DecisionRow) []line {
		out := []line{{indent: indent, glyph: GlyphDecision, label: d.Label, id: d.ID, title: d.Title}}
		for _, a := range d.Alternatives {
			out = append(out, altLine(indent, a))
		}
		return out
	}
	for _, g := range v.Goals {
		ladder = append(ladder, line{glyph: GlyphGoal, label: g.Label, id: g.ID, title: g.Title})
		for _, ac := range g.ACs {
			ladder = append(ladder, line{indent: 1, glyph: GlyphAC, label: ac.Label, id: ac.ID, title: ac.Title, comment: ac.Verify})
		}
		for _, d := range g.Decisions {
			ladder = append(ladder, decisionLines(1, d)...)
		}
	}

	blocks := []block{{lines: ladder}}
	var journeyLines, childLines []line
	for _, j := range v.Journeys {
		journeyLines = append(journeyLines, line{indent: 1, glyph: Glyph("journey"), label: j.Label, id: j.ID, title: j.Title})
		for _, l := range j.Legs {
			journeyLines = append(journeyLines, legLine(2, l))
		}
	}
	for i := range v.Children {
		c := &v.Children[i]
		title := c.Plan
		if c.Name != "" {
			title += " " + c.Name
		}
		if c.Title != "" {
			title += " — " + c.Title
		}
		comment := c.Lifecycle
		if !c.Found {
			comment = "(no plan " + c.Plan + ")"
		}
		if len(c.DependsOn) > 0 {
			comment += " · after " + strings.Join(c.DependsOn, ", ")
		}
		childLines = append(childLines, line{indent: 1, glyph: Glyph("child"), label: c.Label, id: c.ID, title: title, comment: comment})
	}
	blocks = append(blocks, block{"journeys", journeyLines}, block{"child plans", childLines})
	var unlinked []line
	for _, ac := range v.Unlinked.ACs {
		unlinked = append(unlinked, line{indent: 1, glyph: GlyphAC, label: ac.Label, id: ac.ID, title: ac.Title, comment: ac.Verify})
	}
	for _, d := range v.Unlinked.Decisions {
		unlinked = append(unlinked, decisionLines(1, d)...)
	}
	for _, a := range v.Unlinked.Alternatives {
		unlinked = append(unlinked, altLine(1, a))
	}
	for _, l := range v.Unlinked.Legs {
		unlinked = append(unlinked, legLine(1, l))
	}
	blocks = append(blocks, block{title: "unlinked", lines: unlinked})

	var rails, defects, questions []line
	for _, r := range v.Rails {
		var notes []string
		if len(r.HonoredBy) > 0 {
			notes = append(notes, "honoured by "+strings.Join(r.HonoredBy, ", "))
		}
		if len(r.Deferred) > 0 {
			notes = append(notes, "deferred: "+strings.Join(r.Deferred, ", "))
		}
		comment := strings.Join(notes, "; ")
		rails = append(rails, line{indent: 1, glyph: Glyph("rail"), label: r.Label, id: r.ID, title: r.Title, comment: comment})
	}
	for _, d := range v.Defects {
		comment := ""
		if len(d.AddressedBy) > 0 {
			comment = "addressed by " + strings.Join(d.AddressedBy, ", ")
		}
		defects = append(defects, line{indent: 1, glyph: Glyph("defect"), label: d.Label, id: d.ID, title: d.Title, comment: comment})
	}
	for _, q := range v.Questions {
		questions = append(questions, line{indent: 1, glyph: Glyph("question"), label: q.Label, id: q.ID, title: q.Title})
	}
	blocks = append(blocks, block{"rails", rails}, block{"defects", defects}, block{"open questions", questions})

	var b strings.Builder
	b.WriteString(v.Plan + "  " + v.Name + "  " + v.Kind + " · " + v.Lifecycle)
	if v.Epic != "" {
		b.WriteString(" · epic " + v.Epic)
	}
	b.WriteString("\n")
	var all []line
	for _, bl := range blocks {
		all = append(all, bl.lines...)
	}
	if len(all) == 0 {
		b.WriteString("\n(no goals yet — add one with `auto plan add " + v.Plan + " goal --title \"…\"`)\n")
		return b.String()
	}
	cols := measure(all)
	for _, bl := range blocks {
		if len(bl.lines) == 0 {
			continue
		}
		b.WriteString("\n")
		if bl.title != "" {
			b.WriteString(bl.title + "\n")
		}
		for _, l := range bl.lines {
			b.WriteString(cols.render(l) + "\n")
		}
	}
	return b.String()
}

// legLine is a leg, with the child plans that deliver it in the comment
// column.
func legLine(indent int, l LegRow) line {
	comment := ""
	if len(l.DeliveredBy) > 0 {
		comment = "delivered by " + strings.Join(l.DeliveredBy, ", ")
	}
	return line{indent: indent, glyph: Glyph("leg"), label: l.Label, id: l.ID, title: l.Title, comment: comment}
}

// altLine is a rejected alternative: a "-" in its decision's glyph column,
// nested one step (show-me notation's removed marker).
func altLine(indent int, a AltRow) line {
	return line{indent: indent, glyph: GlyphRejected + "  ", label: a.Label, id: a.ID, title: a.Title, comment: a.Why}
}

// columns holds the aligned widths of a set of ladder lines.
type columns struct {
	labelW, idW, commentAt int
}

func measure(lines []line) columns {
	var c columns
	for _, l := range lines {
		c.labelW = max(c.labelW, utf8.RuneCountInString(l.label))
		c.idW = max(c.idW, len(l.id))
	}
	for _, l := range lines {
		if l.comment != "" {
			c.commentAt = max(c.commentAt, utf8.RuneCountInString(c.head(l)))
		}
	}
	return c
}

func (c columns) head(l line) string {
	return strings.Repeat("    ", l.indent) + l.glyph + " " + pad(l.label, c.labelW) + "  " + pad(l.id, c.idW) + "  " + l.title
}

func (c columns) render(l line) string {
	head := c.head(l)
	if l.comment == "" {
		return head
	}
	return head + strings.Repeat(" ", c.commentAt-utf8.RuneCountInString(head)+2) + l.comment
}

func pad(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-utf8.RuneCountInString(s)))
}

// index is a read-side view of a graph: active nodes by ID and edges between
// active nodes.
type index struct {
	g      *graph.Graph
	active map[string]graph.Node
	all    map[string]graph.Node
}

func newIndex(g *graph.Graph) *index {
	ix := &index{g: g, active: map[string]graph.Node{}, all: map[string]graph.Node{}}
	for _, n := range g.Nodes {
		if _, dup := ix.all[n.ID]; dup {
			continue // a broken graph repeating an ID: the first wins, as in Validate
		}
		ix.all[n.ID] = n
		if n.Active() {
			ix.active[n.ID] = n
		}
	}
	return ix
}

// activeEdges returns the edges of type typ whose endpoints are both active
// local nodes.
func (ix *index) activeEdges(typ string) []graph.Edge {
	var out []graph.Edge
	for _, e := range ix.g.Edges {
		if e.Type != typ {
			continue
		}
		if _, ok := ix.active[e.From]; !ok {
			continue
		}
		if _, ok := ix.active[e.To]; !ok {
			continue
		}
		out = append(out, e)
	}
	return out
}

// byType returns the active nodes of a type in reading order.
func (ix *index) byType(typ string) []graph.Node {
	var out []graph.Node
	for _, n := range ix.active {
		if n.Type == typ {
			out = append(out, n)
		}
	}
	sortReading(out)
	return out
}

// ordered returns the active nodes among ids, in reading order.
func (ix *index) ordered(ids []string) []graph.Node {
	var out []graph.Node
	for _, id := range ids {
		if n, ok := ix.active[id]; ok {
			out = append(out, n)
		}
	}
	sortReading(out)
	return slices.CompactFunc(out, func(a, b graph.Node) bool { return a.ID == b.ID })
}

// sortReading sorts nodes by rank, then ID (D-12).
func sortReading(nodes []graph.Node) {
	slices.SortFunc(nodes, func(a, b graph.Node) int {
		return cmp.Or(cmp.Compare(a.Rank, b.Rank), cmp.Compare(a.ID, b.ID))
	})
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	out = slices.Compact(out)
	if out == nil {
		out = []string{}
	}
	return out
}

// stringList reads a list field as strings.
func stringList(v any) []string {
	out := []string{}
	switch l := v.(type) {
	case []string:
		out = append(out, l...)
	case []any:
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// firstLine returns the first non-empty line of s, cut to width runes with
// "…" when it is longer or when more lines follow.
func firstLine(s string, width int) string {
	s = strings.TrimSpace(s)
	first, rest, more := strings.Cut(s, "\n")
	first = strings.TrimSpace(first)
	if utf8.RuneCountInString(first) > width {
		return string([]rune(first)[:width-1]) + "…"
	}
	if more && strings.TrimSpace(rest) != "" {
		return first + " …"
	}
	return first
}
