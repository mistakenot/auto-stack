// Package render builds the agent-facing views of a plan. Each view is one
// value that is encoded as JSON (the default) or rendered as text (--text),
// so the two forms carry the same facts and cannot diverge.
package render

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mistakenot/auto-plan/internal/graph"
)

// View is a value with a text rendering; its JSON form is its encoding.
type View interface {
	Text() string
}

// Glyphs mark node types in text views (show-me notation).
const (
	GlyphGoal     = "◎"
	GlyphAC       = "✓"
	GlyphDecision = "◆"
)

// ShowView is the goal ladder: each goal, the ACs that prove it and the
// decisions that constrain it or its ACs.
type ShowView struct {
	Plan      string      `json:"plan"`
	Name      string      `json:"name"`
	Kind      string      `json:"kind"`
	Lifecycle string      `json:"lifecycle"`
	Goals     []GoalRow   `json:"goals"`
	Unlinked  UnlinkedRow `json:"unlinked"`
}

// GoalRow is one goal on the ladder.
type GoalRow struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	ACs       []ACRow       `json:"acs"`
	Decisions []DecisionRow `json:"decisions"`
}

// ACRow is one acceptance criterion with its verify command.
type ACRow struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Verify string `json:"verify,omitempty"`
}

// DecisionRow is one decision and the goal/AC IDs it constrains.
type DecisionRow struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Constrains []string `json:"constrains"`
}

// UnlinkedRow holds active ACs that prove no goal and decisions that
// constrain nothing, so nothing in the plan is invisible.
type UnlinkedRow struct {
	ACs       []ACRow       `json:"acs"`
	Decisions []DecisionRow `json:"decisions"`
}

// Show builds the goal ladder for a plan. Retired nodes are left out.
func Show(planID string, g *graph.Graph) ShowView {
	plan, _ := g.Plan()
	v := ShowView{
		Plan:      planID,
		Name:      plan.StringField("name"),
		Kind:      plan.StringField("kind"),
		Lifecycle: plan.StringField("lifecycle"),
		Goals:     []GoalRow{},
		Unlinked:  UnlinkedRow{ACs: []ACRow{}, Decisions: []DecisionRow{}},
	}

	active := map[string]graph.Node{}
	for _, n := range g.Nodes {
		if n.Active() {
			active[n.ID] = n
		}
	}
	goalOfAC := map[string]string{}
	constrains := map[string][]string{}
	for _, e := range g.Edges {
		if _, ok := active[e.From]; !ok {
			continue
		}
		if _, ok := active[e.To]; !ok {
			continue
		}
		switch e.Type {
		case "proves":
			if _, seen := goalOfAC[e.From]; !seen {
				goalOfAC[e.From] = e.To
			}
		case "constrains":
			constrains[e.From] = append(constrains[e.From], e.To)
		}
	}

	byType := func(typ string) []graph.Node {
		var out []graph.Node
		for _, n := range g.Nodes {
			if n.Type == typ && n.Active() {
				out = append(out, n)
			}
		}
		slices.SortFunc(out, func(a, b graph.Node) int {
			return cmp.Or(cmp.Compare(a.Rank, b.Rank), cmp.Compare(a.ID, b.ID))
		})
		return out
	}
	acs, decisions := byType("ac"), byType("decision")

	for _, goal := range byType("goal") {
		row := GoalRow{ID: goal.ID, Title: goal.StringField("title"), ACs: []ACRow{}, Decisions: []DecisionRow{}}
		under := map[string]bool{goal.ID: true}
		for _, ac := range acs {
			if goalOfAC[ac.ID] == goal.ID {
				row.ACs = append(row.ACs, acRow(ac))
				under[ac.ID] = true
			}
		}
		for _, d := range decisions {
			if slices.ContainsFunc(constrains[d.ID], func(id string) bool { return under[id] }) {
				row.Decisions = append(row.Decisions, decisionRow(d, constrains[d.ID]))
			}
		}
		v.Goals = append(v.Goals, row)
	}
	for _, ac := range acs {
		if _, ok := goalOfAC[ac.ID]; !ok {
			v.Unlinked.ACs = append(v.Unlinked.ACs, acRow(ac))
		}
	}
	for _, d := range decisions {
		if len(constrains[d.ID]) == 0 {
			v.Unlinked.Decisions = append(v.Unlinked.Decisions, decisionRow(d, nil))
		}
	}
	return v
}

func acRow(n graph.Node) ACRow {
	row := ACRow{ID: n.ID, Title: n.StringField("title")}
	if verify := n.ObjectField("verify"); verify != nil {
		row.Verify, _ = verify["cmd"].(string)
		if row.Verify == "" {
			if kind, _ := verify["kind"].(string); kind == "manual" {
				row.Verify = "(manual)"
			}
		}
	}
	return row
}

func decisionRow(n graph.Node, targets []string) DecisionRow {
	t := slices.Clone(targets)
	slices.Sort(t)
	if t == nil {
		t = []string{}
	}
	return DecisionRow{ID: n.ID, Title: n.StringField("title"), Constrains: t}
}

// line is one row of an indented ladder before column alignment.
type line struct {
	indent  int
	glyph   string
	id      string
	title   string
	comment string
}

// Text renders the ladder in show-me notation: glyph, ID and title, with
// verify commands in an aligned right-hand column.
func (v ShowView) Text() string {
	var lines []line
	for _, g := range v.Goals {
		lines = append(lines, line{0, GlyphGoal, g.ID, g.Title, ""})
		for _, ac := range g.ACs {
			lines = append(lines, line{1, GlyphAC, ac.ID, ac.Title, ac.Verify})
		}
		for _, d := range g.Decisions {
			lines = append(lines, line{1, GlyphDecision, d.ID, d.Title, ""})
		}
	}
	var unlinked []line
	for _, ac := range v.Unlinked.ACs {
		unlinked = append(unlinked, line{1, GlyphAC, ac.ID, ac.Title, ac.Verify})
	}
	for _, d := range v.Unlinked.Decisions {
		unlinked = append(unlinked, line{1, GlyphDecision, d.ID, d.Title, ""})
	}

	var b strings.Builder
	b.WriteString(v.Plan + "-" + v.Name + "  " + v.Kind + " · " + v.Lifecycle + "\n")
	if len(lines) == 0 && len(unlinked) == 0 {
		b.WriteString("\n(no goals yet — add one with `auto plan add " + v.Plan + " goal --title \"…\"`)\n")
		return b.String()
	}
	idW, titleW := widths(append(slices.Clone(lines), unlinked...))
	if len(lines) > 0 {
		b.WriteString("\n")
		writeLines(&b, lines, idW, titleW)
	}
	if len(unlinked) > 0 {
		b.WriteString("\nunlinked\n")
		writeLines(&b, unlinked, idW, titleW)
	}
	return b.String()
}

func widths(lines []line) (idW, titleW int) {
	for _, l := range lines {
		idW = max(idW, len(l.id))
		if l.comment != "" {
			titleW = max(titleW, 4*l.indent+utf8.RuneCountInString(l.title))
		}
	}
	return idW, titleW
}

func writeLines(b *strings.Builder, lines []line, idW, titleW int) {
	for _, l := range lines {
		lead := strings.Repeat("    ", l.indent)
		b.WriteString(lead + l.glyph + " " + pad(l.id, idW) + "  " + l.title)
		if l.comment != "" {
			gap := titleW - 4*l.indent - utf8.RuneCountInString(l.title)
			b.WriteString(strings.Repeat(" ", gap+2) + l.comment)
		}
		b.WriteString("\n")
	}
}

func pad(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-len(s)))
}
