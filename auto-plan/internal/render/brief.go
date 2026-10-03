package render

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/tree"
)

// BriefView is a Stage Brief: everything one stage needs, and nothing about
// any other stage beyond its ID, title and status. It holds the stage's steps
// and commit message, the stages it depends on, the files it touches (as a
// derived file tree), the ACs it covers with their verify commands, the
// decisions constraining those ACs or their goals, the rails those ACs
// discharge, and the plan's open questions.
type BriefView struct {
	Plan      string          `json:"plan"`
	Name      string          `json:"name"`
	Stage     BriefStage      `json:"stage"`
	DependsOn []StageRef      `json:"dependsOn"`
	Files     []FileRow       `json:"files"`
	FileTree  tree.Tree       `json:"fileTree"`
	ACs       []BriefAC       `json:"acs"`
	Decisions []BriefDecision `json:"decisions"`
	Rails     []BriefRail     `json:"rails"`
	Questions []BriefQuestion `json:"questions"`
}

// BriefStage is the briefed stage in full.
type BriefStage struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Status string   `json:"status"`
	Steps  []string `json:"steps"`
	Commit string   `json:"commit"`
}

// StageRef is another stage: ID, title and status only.
type StageRef struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// Ref names a node by ID and title.
type Ref struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// BriefAC is a covered acceptance criterion with its goal and verification.
type BriefAC struct {
	ID     string      `json:"id"`
	Title  string      `json:"title"`
	Goal   *Ref        `json:"goal"`
	GWT    string      `json:"gwt"`
	Verify BriefVerify `json:"verify"`
}

// BriefVerify is how an AC is verified.
type BriefVerify struct {
	Cmd   string   `json:"cmd"`
	Tests []string `json:"tests"`
	Kind  string   `json:"kind"`
}

// BriefDecision is a decision constraining a covered AC or its goal.
type BriefDecision struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Chosen     string   `json:"chosen"`
	Why        string   `json:"why"`
	Constrains []string `json:"constrains"`
}

// BriefRail is a rail a covered AC discharges. A qualified rail (`005:r-8hw3`)
// lives in another plan (usually the epic); its title is resolved through the
// plan set, and stays empty when it does not resolve.
type BriefRail struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Qualified    bool     `json:"qualified,omitempty"`
	DischargedBy []string `json:"dischargedBy"`
}

// BriefQuestion is an open question of the plan.
type BriefQuestion struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Recommended string `json:"recommended,omitempty"`
}

// Brief builds the Stage Brief of stageID. ok is false when stageID is not a
// stage of the plan. plans (may be nil) resolves qualified rails.
func Brief(planID string, g *graph.Graph, stageID string, plans Plans) (BriefView, bool) {
	s, ok := g.NodeByID(stageID)
	if !ok || s.Type != "stage" {
		return BriefView{}, false
	}
	plan, _ := g.Plan()
	ix := newIndex(g)
	v := BriefView{
		Plan: planID, Name: plan.StringField("name"),
		Stage: BriefStage{
			ID: s.ID, Title: s.StringField("title"), Status: stageStatus(s),
			Steps: stringList(s.Fields["steps"]), Commit: s.StringField("commit"),
		},
		DependsOn: []StageRef{}, ACs: []BriefAC{}, Decisions: []BriefDecision{},
		Rails: []BriefRail{}, Questions: []BriefQuestion{},
	}

	var deps, covered []string
	for _, e := range ix.activeEdges("dependsOn") {
		if e.From == s.ID {
			deps = append(deps, e.To)
		}
	}
	for _, e := range ix.activeEdges("covers") {
		if e.From == s.ID {
			covered = append(covered, e.To)
		}
	}
	for _, d := range ix.ordered(deps) {
		v.DependsOn = append(v.DependsOn, StageRef{ID: d.ID, Title: d.StringField("title"), Status: stageStatus(d)})
	}

	files := StageFiles(g, s.ID)
	v.Files, v.FileTree = fileRows(files), tree.FileTree(files)

	goalOf := map[string]graph.Node{}
	for _, e := range ix.activeEdges("proves") {
		if _, seen := goalOf[e.From]; !seen {
			goalOf[e.From] = ix.active[e.To]
		}
	}
	acs := ix.ordered(covered)
	// ACs read in goal order, then their own rank.
	slices.SortStableFunc(acs, func(a, b graph.Node) int {
		return cmp.Or(cmp.Compare(goalOf[a.ID].Rank, goalOf[b.ID].Rank), cmp.Compare(goalOf[a.ID].ID, goalOf[b.ID].ID))
	})
	under := map[string]bool{}
	for _, ac := range acs {
		row := BriefAC{ID: ac.ID, Title: ac.StringField("title"), GWT: ac.StringField("gwt"), Verify: BriefVerify{Tests: []string{}}}
		if goal, ok := goalOf[ac.ID]; ok {
			row.Goal = &Ref{ID: goal.ID, Title: goal.StringField("title")}
			under[goal.ID] = true
		}
		if verify := ac.ObjectField("verify"); verify != nil {
			row.Verify.Cmd, _ = verify["cmd"].(string)
			row.Verify.Kind, _ = verify["kind"].(string)
			row.Verify.Tests = stringList(verify["tests"])
		}
		v.ACs = append(v.ACs, row)
		under[ac.ID] = true
	}

	constrains := map[string][]string{}
	for _, e := range ix.activeEdges("constrains") {
		if under[e.To] {
			constrains[e.From] = append(constrains[e.From], e.To)
		}
	}
	for _, d := range ix.byType("decision") {
		if targets, ok := constrains[d.ID]; ok {
			v.Decisions = append(v.Decisions, BriefDecision{
				ID: d.ID, Title: d.StringField("title"), Chosen: d.StringField("chosen"), Why: d.StringField("why"),
				Constrains: sorted(targets),
			})
		}
	}

	rails := map[string]*BriefRail{}
	var railOrder []string
	for _, ac := range acs {
		for _, e := range g.Edges {
			if e.From != ac.ID || e.Type != "discharges" {
				continue
			}
			r, seen := rails[e.To]
			if !seen {
				r = &BriefRail{ID: e.To, DischargedBy: []string{}}
				if _, _, q := graph.ParseRef(e.To); q {
					r.Qualified = true
					if n, ok := lookup(plans, e.To); ok {
						r.Title = n.StringField("title")
					}
				} else if n, ok := ix.active[e.To]; ok {
					r.Title = n.StringField("title")
				} else {
					continue // a retired or dangling rail
				}
				rails[e.To] = r
				railOrder = append(railOrder, e.To)
			}
			r.DischargedBy = append(r.DischargedBy, ac.ID)
		}
	}
	for _, id := range railOrder {
		v.Rails = append(v.Rails, *rails[id])
	}

	for _, q := range ix.byType("question") {
		if q.StringField("status") == "open" {
			v.Questions = append(v.Questions, BriefQuestion{ID: q.ID, Title: q.StringField("title"), Recommended: q.StringField("recommended")})
		}
	}
	return v, true
}

func stageStatus(n graph.Node) string { return cmp.Or(n.StringField("status"), "todo") }

// Text renders the brief as Markdown.
func (v BriefView) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Stage %s: %s\n\nPlan %s (%s) · status %s\n", v.Stage.ID, v.Stage.Title, v.Plan, v.Name, v.Stage.Status)

	section := func(title string, empty bool) bool {
		b.WriteString("\n## " + title + "\n\n")
		if empty {
			b.WriteString("(none)\n")
		}
		return !empty
	}

	if section("Depends on", len(v.DependsOn) == 0) {
		for _, d := range v.DependsOn {
			fmt.Fprintf(&b, "- %s: %s (%s)\n", d.ID, d.Title, d.Status)
		}
	}
	if section("Steps", len(v.Stage.Steps) == 0) {
		for i, s := range v.Stage.Steps {
			fmt.Fprintf(&b, "%d. %s\n", i+1, s)
		}
	}
	if section("Commit", v.Stage.Commit == "") {
		b.WriteString("`" + v.Stage.Commit + "`\n")
	}
	if section("Files", len(v.Files) == 0) {
		b.WriteString("```\n" + tree.Render(v.FileTree, nil) + "```\n")
	}
	if section("Acceptance criteria", len(v.ACs) == 0) {
		for i, ac := range v.ACs {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "### %s: %s\n\n", ac.ID, ac.Title)
			if ac.Goal != nil {
				fmt.Fprintf(&b, "Proves %s: %s\n\n", ac.Goal.ID, ac.Goal.Title)
			}
			if ac.GWT != "" {
				b.WriteString(ac.GWT + "\n\n")
			}
			switch {
			case ac.Verify.Cmd != "":
				b.WriteString("- Verify: `" + ac.Verify.Cmd + "`\n")
			case ac.Verify.Kind == "manual":
				b.WriteString("- Verify: manual\n")
			default:
				b.WriteString("- Verify: (none)\n")
			}
			if len(ac.Verify.Tests) > 0 {
				b.WriteString("- Tests: " + strings.Join(ac.Verify.Tests, ", ") + "\n")
			}
		}
	}
	if section("Decisions", len(v.Decisions) == 0) {
		for i, d := range v.Decisions {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "### %s: %s\n\nConstrains %s\n\n**Chosen:** %s\n\n**Why:** %s\n",
				d.ID, d.Title, strings.Join(d.Constrains, ", "), d.Chosen, d.Why)
		}
	}
	if section("Rails", len(v.Rails) == 0) {
		for _, r := range v.Rails {
			title := r.Title
			if r.Qualified && title == "" {
				title = "a rail in plan " + strings.SplitN(r.ID, ":", 2)[0]
			}
			fmt.Fprintf(&b, "- %s: %s (discharged by %s)\n", r.ID, title, strings.Join(r.DischargedBy, ", "))
		}
	}
	if section("Open questions", len(v.Questions) == 0) {
		for _, q := range v.Questions {
			fmt.Fprintf(&b, "- %s: %s\n", q.ID, q.Title)
			if q.Recommended != "" {
				b.WriteString("  Recommended: " + strings.ReplaceAll(q.Recommended, "\n", "\n  ") + "\n")
			}
		}
	}
	return b.String()
}
