package render

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
)

var update = flag.Bool("update", false, "rewrite golden files")

// ladderFixture has two goals (one with an AC and two decisions, one
// constraining the AC), a retired AC, an unlinked AC and an unlinked decision,
// a decision with two rejected alternatives (one retired, one with a long
// reason), an unlinked alternative, a rail, a defect, and an open and an
// answered question.
const ladderFixture = `{
  "version": 1,
  "nodes": [
    {"id": "plan", "type": "plan", "status": "active", "fields": {"name": "stage-briefs", "kind": "task", "lifecycle": "solution", "created": "2026-01-01"}},
    {"id": "g-m4t8", "type": "goal", "status": "active", "rank": "a0", "fields": {"title": "Plans can be reordered without breaking references"}},
    {"id": "g-k7q2", "type": "goal", "status": "active", "rank": "a1", "fields": {"title": "An agent gets one stage's context in one call"}},
    {"id": "ac-3fxm", "type": "ac", "status": "active", "rank": "a0", "fields": {"title": "brief is self-contained", "gwt": "Given a, when b, then c", "verify": {"cmd": "go test ./e2e -run Brief"}}},
    {"id": "ac-7w1e", "type": "ac", "status": "retired", "rank": "a1", "fields": {"title": "retired criterion", "gwt": "Given a, when b, then c"}},
    {"id": "ac-8pqr", "type": "ac", "status": "active", "rank": "a0", "fields": {"title": "a manual check", "gwt": "Given a, when b, then c", "verify": {"kind": "manual"}}},
    {"id": "d-9t2w", "type": "decision", "status": "active", "rank": "a0", "fields": {"title": "brief is Markdown + JSON", "chosen": "c", "why": "w", "by": "charlie"}},
    {"id": "d-2hcv", "type": "decision", "status": "active", "rank": "a1", "fields": {"title": "ranks are strings", "chosen": "c", "why": "w", "by": "charlie"}},
    {"id": "d-5n0b", "type": "decision", "status": "active", "rank": "a2", "fields": {"title": "undecided scope", "chosen": "c", "why": "w", "by": "charlie"}},
    {"id": "a-5hcv", "type": "alternative", "status": "active", "rank": "a0", "fields": {"title": "reuse Context Pack", "why": "collides with auto-graph's term"}},
    {"id": "a-7kq1", "type": "alternative", "status": "active", "rank": "a1", "fields": {"title": "HTML only", "why": "agents cannot read it cheaply, and it would need a renderer that does not exist yet\nsecond line"}},
    {"id": "a-0zzz", "type": "alternative", "status": "retired", "rank": "a2", "fields": {"title": "retired option", "why": "w"}},
    {"id": "a-3mmm", "type": "alternative", "status": "active", "rank": "a3", "fields": {"title": "orphan option", "why": "nobody rejected it"}},
    {"id": "r-8hw3", "type": "rail", "status": "active", "rank": "a0", "fields": {"title": "Never copy rail text into a child", "deferred": ["002", "003"]}},
    {"id": "df-1ycb", "type": "defect", "status": "active", "rank": "a0", "fields": {"title": "Stage context is scattered"}},
    {"id": "q-jcx5", "type": "question", "status": "active", "rank": "a0", "fields": {"title": "Include alternatives in briefs?", "status": "open"}},
    {"id": "q-tjhs", "type": "question", "status": "active", "rank": "a1", "fields": {"title": "Answered already", "status": "answered", "answer": "yes"}}
  ],
  "edges": [
    {"from": "ac-3fxm", "type": "proves", "to": "g-k7q2"},
    {"from": "ac-7w1e", "type": "proves", "to": "g-k7q2"},
    {"from": "d-9t2w", "type": "constrains", "to": "ac-3fxm"},
    {"from": "d-2hcv", "type": "constrains", "to": "g-m4t8"},
    {"from": "d-9t2w", "type": "rejects", "to": "a-5hcv"},
    {"from": "d-9t2w", "type": "rejects", "to": "a-7kq1"},
    {"from": "d-9t2w", "type": "rejects", "to": "a-0zzz"},
    {"from": "g-k7q2", "type": "addresses", "to": "df-1ycb"}
  ]
}`

func fixtureView(t *testing.T) ShowView {
	t.Helper()
	g, err := graph.Parse([]byte(ladderFixture))
	if err != nil {
		t.Fatal(err)
	}
	if errs := graph.Validate(g); len(errs) > 0 {
		t.Fatalf("fixture invalid: %+v", errs)
	}
	return Show("004", g)
}

func TestShowTextGolden(t *testing.T) {
	golden(t, "show.txt", fixtureView(t).Text())
}

// golden compares got with testdata/golden/<name>, rewriting it with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if got != string(want) {
		t.Fatalf("%s mismatch (run with -update):\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func TestShowStructure(t *testing.T) {
	v := fixtureView(t)
	if len(v.Goals) != 2 || v.Goals[0].ID != "g-m4t8" || v.Goals[1].ID != "g-k7q2" {
		t.Fatalf("goals not in rank order: %+v", v.Goals)
	}
	second := v.Goals[1]
	if len(second.ACs) != 1 || second.ACs[0].ID != "ac-3fxm" || second.ACs[0].Verify != "go test ./e2e -run Brief" {
		t.Fatalf("retired AC must be hidden and verify shown: %+v", second.ACs)
	}
	if len(second.Decisions) != 1 || second.Decisions[0].ID != "d-9t2w" {
		t.Fatalf("decision constraining an AC belongs under its goal: %+v", second.Decisions)
	}
	if len(v.Unlinked.ACs) != 1 || len(v.Unlinked.Decisions) != 1 || len(v.Unlinked.Alternatives) != 1 {
		t.Fatalf("unlinked = %+v", v.Unlinked)
	}
	alts := second.Decisions[0].Alternatives
	if len(alts) != 2 || alts[0].ID != "a-5hcv" || alts[1].ID != "a-7kq1" || !strings.HasSuffix(alts[1].Why, "…") {
		t.Fatalf("rejected alternatives (retired hidden, long why cut) = %+v", alts)
	}
	if len(v.Rails) != 1 || len(v.Rails[0].Deferred) != 2 || len(v.Defects) != 1 || v.Defects[0].AddressedBy[0] != "g-k7q2" {
		t.Fatalf("rails/defects = %+v %+v", v.Rails, v.Defects)
	}
	if len(v.Questions) != 1 || v.Questions[0].ID != "q-jcx5" {
		t.Fatalf("only open questions are shown: %+v", v.Questions)
	}
	// Positional labels follow reading order and are never IDs.
	labels := []string{v.Goals[0].Label, v.Goals[1].Label, second.ACs[0].Label, v.Goals[0].Decisions[0].Label,
		second.Decisions[0].Label, alts[1].Label, v.Unlinked.ACs[0].Label, v.Rails[0].Label, v.Defects[0].Label, v.Questions[0].Label}
	want := []string{"G1", "G2", "AC2.1", "D1", "D2", "A2.2", "AC?.1", "R1", "DF1", "Q1"}
	if !slices.Equal(labels, want) {
		t.Fatalf("labels = %v, want %v", labels, want)
	}
}

// TestShowJSONTextParity checks that every fact in the JSON form appears in
// the text form and every ID in the text form is in the JSON form.
func TestShowJSONTextParity(t *testing.T) {
	v := fixtureView(t)
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatal(err)
	}
	text := v.Text()
	var walk func(key string, x any)
	walk = func(key string, x any) {
		switch val := x.(type) {
		case map[string]any:
			for k, child := range val {
				walk(k, child)
			}
		case []any:
			// constrains lists are relationships the ladder shows by nesting.
			if key == "constrains" {
				return
			}
			for _, child := range val {
				walk(key, child)
			}
		case string:
			if !strings.Contains(text, val) {
				t.Errorf("JSON %s %q missing from text:\n%s", key, val, text)
			}
		}
	}
	walk("", generic)

	for _, id := range regexp.MustCompile(`\b[a-z]{1,4}-[0-9a-z]{4}\b`).FindAllString(text, -1) {
		if !strings.Contains(string(data), `"`+id+`"`) {
			t.Errorf("text ID %s missing from JSON", id)
		}
	}
}

func TestShowEmptyPlan(t *testing.T) {
	g := graph.New(map[string]any{"name": "demo", "kind": "task", "lifecycle": "requirements", "created": "2026-01-01"})
	text := Show("001", g).Text()
	if !strings.Contains(text, "001-demo  task · requirements") || !strings.Contains(text, "auto plan add 001 goal") {
		t.Fatalf("empty plan text:\n%s", text)
	}
}

// planFixture loads testdata/stage-briefs.json: a plan at lifecycle plan with
// two goals, three ACs, two decisions (one rejecting two alternatives), a
// rail discharged locally and in another plan (005:r-8hw3), a defect, an open
// and an answered question, two stages (s-p6v5 depends on s-3v6v), five file
// changes and a tree about ac-jttt. It was built with the CLI.
func planFixture(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.Decode(filepath.Join("testdata", "stage-briefs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if errs := graph.Validate(g); len(errs) > 0 {
		t.Fatalf("fixture invalid: %+v", errs)
	}
	return g
}

func TestShowPlanGolden(t *testing.T) {
	golden(t, "show-plan.txt", Show("001", planFixture(t)).Text())
}

func TestDescribe(t *testing.T) {
	g := planFixture(t)
	v, ok := Describe("001", g, "a-9xcp")
	if !ok {
		t.Fatal("a-9xcp not found")
	}
	why, _ := v.Fields["why"].(string)
	if v.Title != "HTML only" || v.Get != "auto plan get 001 a-9xcp" || v.Edges.In["rejects"] != 1 || len(v.Edges.Out) != 0 {
		t.Fatalf("describe = %+v", v)
	}
	if len(v.Truncated) != 0 || strings.HasSuffix(why, "…") {
		t.Fatalf("a short why is not cut: %+v", v)
	}
	// A long field is cut to DescribeWidth runes and named in truncated.
	n, _ := g.NodeByID("a-9xcp")
	n.Fields = map[string]any{"title": "HTML only", "why": strings.Repeat("é", DescribeWidth+50)}
	g2 := &graph.Graph{Version: 1, Nodes: []graph.Node{n}, Edges: []graph.Edge{}}
	v, _ = Describe("001", g2, "a-9xcp")
	why, _ = v.Fields["why"].(string)
	if !slices.Equal(v.Truncated, []string{"why"}) || utf8.RuneCountInString(why) != DescribeWidth+1 {
		t.Fatalf("truncated = %v, why has %d runes", v.Truncated, utf8.RuneCountInString(why))
	}
	text := v.Text()
	if !strings.Contains(text, "truncated: why; full node: auto plan get 001 a-9xcp") {
		t.Fatalf("describe text must print the recovery command:\n%s", text)
	}
	golden(t, "describe.txt", mustDescribe(t, planFixture(t), "ac-jttt").Text())
}

func mustDescribe(t *testing.T, g *graph.Graph, id string) DescribeView {
	t.Helper()
	v, ok := Describe("001", g, id)
	if !ok {
		t.Fatalf("%s not found", id)
	}
	return v
}

func TestCard(t *testing.T) {
	g := planFixture(t)
	v, ok := Card("001", g, "ac-jttt")
	if !ok {
		t.Fatal("ac-jttt not found")
	}
	if _, ok := Card("001", g, "ac-zzzz"); ok {
		t.Fatal("unknown ID must not be found")
	}
	out := map[string]EdgeRow{}
	for _, r := range v.Out {
		out[r.Edge+" "+r.ID] = r
	}
	if r := out["proves g-xyf8"]; r.Type != "goal" || r.Title != "An agent gets one stage's context in one call" {
		t.Fatalf("proves neighbour = %+v", r)
	}
	if r := out["discharges 005:r-8hw3"]; !r.Qualified || r.Type != "" {
		t.Fatalf("a qualified neighbour stays a raw reference: %+v", r)
	}
	if len(v.In) != 3 {
		t.Fatalf("in = %+v", v.In)
	}
	golden(t, "get.txt", v.Text())
}

func TestTraceGoldens(t *testing.T) {
	g := planFixture(t)
	for _, tc := range []struct {
		name, id string
		dir      Direction
	}{
		{"trace-ac-up.txt", "ac-jttt", Up},
		{"trace-ac-down.txt", "ac-jttt", Down},
		{"trace-goal-down.txt", "g-xyf8", Down},
		{"trace-file-up.txt", "f-44v0", Up},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, ok := Trace("001", g, tc.id, tc.dir)
			if !ok {
				t.Fatalf("%s not found", tc.id)
			}
			golden(t, tc.name, v.Text())
		})
	}
}

func TestTraceWalks(t *testing.T) {
	g := planFixture(t)
	v, _ := Trace("001", g, "ac-jttt", Both)
	if len(v.Up) == 0 || v.Up[0].Edge != "proves" || v.Up[0].ID != "g-xyf8" || v.Up[0].Dir != "out" {
		t.Fatalf("up from an AC starts at its goal: %+v", v.Up)
	}
	if len(v.Down) == 0 || v.Down[0].Edge != "covers" || v.Down[0].Dir != "in" || len(v.Down[0].Children) != 2 ||
		v.Down[0].Children[0].Edge != "touches" {
		t.Fatalf("down from an AC: stages that cover it, then their files: %+v", v.Down)
	}
	// Every edge type is either traced one way or deliberately not traced.
	for _, e := range schema.Registry.Edges {
		_, traced := TraceEdges[e.Name]
		untraced := e.Name == "wouldBreak" || e.Name == "supersedes" || e.Name == "dependsOn"
		if traced == untraced {
			t.Errorf("edge %s: traced=%v, but untraced list says %v", e.Name, traced, untraced)
		}
	}
	if v.Up == nil || v.Down == nil {
		t.Fatalf("walks are lists, never null: %+v", v)
	}
	v, _ = Trace("001", g, "ac-jttt", Up)
	if len(v.Down) != 0 {
		t.Fatalf("an unrequested walk is empty: %+v", v.Down)
	}

	// A node met twice is expanded once, then marked seen.
	shared, err := graph.Parse([]byte(`{"version":1,"nodes":[
	  {"id":"plan","type":"plan","status":"active","fields":{"name":"x","kind":"task","lifecycle":"plan","created":"2026-01-01"}},
	  {"id":"g-aaaa","type":"goal","status":"active","rank":"a0","fields":{"title":"goal"}},
	  {"id":"ac-aaaa","type":"ac","status":"active","rank":"a0","fields":{"title":"one","gwt":"g"}},
	  {"id":"ac-bbbb","type":"ac","status":"active","rank":"a1","fields":{"title":"two","gwt":"g"}},
	  {"id":"ac-cccc","type":"ac","status":"retired","rank":"a2","fields":{"title":"gone","gwt":"g"}},
	  {"id":"s-aaaa","type":"stage","status":"active","rank":"a0","fields":{"title":"both","steps":["x"],"commit":"c"}}],
	 "edges":[{"from":"ac-aaaa","type":"proves","to":"g-aaaa"},{"from":"ac-bbbb","type":"proves","to":"g-aaaa"},
	  {"from":"ac-cccc","type":"proves","to":"g-aaaa"},
	  {"from":"s-aaaa","type":"covers","to":"ac-aaaa"},{"from":"s-aaaa","type":"covers","to":"ac-bbbb"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	v, _ = Trace("001", shared, "g-aaaa", Down)
	if len(v.Down) != 2 || v.Down[0].Children[0].Seen || !v.Down[1].Children[0].Seen || v.Down[1].Children[0].Children != nil {
		t.Fatalf("shared stage: expanded once, then seen; retired AC left out: %+v", v.Down)
	}
	if !strings.Contains(v.Text(), "both  (see above)") {
		t.Fatalf("seen marker missing:\n%s", v.Text())
	}
}

func TestBrief(t *testing.T) {
	g := planFixture(t)
	v, ok := Brief("001", g, "s-3v6v")
	if !ok {
		t.Fatal("s-3v6v not found")
	}
	if _, ok := Brief("001", g, "ac-jttt"); ok {
		t.Fatal("a non-stage has no brief")
	}
	if len(v.ACs) != 1 || v.ACs[0].ID != "ac-jttt" || v.ACs[0].Verify.Cmd != "go test ./e2e -run Brief" ||
		len(v.Decisions) != 1 || v.Decisions[0].ID != "d-ytbz" || len(v.Rails) != 2 || len(v.Questions) != 1 {
		t.Fatalf("brief = %+v", v)
	}
	text := v.Text()
	golden(t, "brief.txt", text)

	other, _ := Brief("001", g, "s-p6v5")
	otherText := other.Text()
	golden(t, "brief-dependent.txt", otherText)
	// A brief carries nothing about another stage beyond its ID, title and status.
	for _, step := range stringList(mustNode(t, g, "s-3v6v").Fields["steps"]) {
		if strings.Contains(otherText, step) {
			t.Errorf("brief for s-p6v5 leaks s-3v6v's step %q:\n%s", step, otherText)
		}
	}
	for _, step := range stringList(mustNode(t, g, "s-p6v5").Fields["steps"]) {
		if strings.Contains(text, step) {
			t.Errorf("brief for s-3v6v leaks s-p6v5's step %q:\n%s", step, text)
		}
	}
	if strings.Contains(otherText, "feat: render layer") || !strings.Contains(otherText, "- s-3v6v: Render layer (done)") {
		t.Errorf("a dependency appears by ID, title and status only:\n%s", otherText)
	}
}

func mustNode(t *testing.T, g *graph.Graph, id string) graph.Node {
	t.Helper()
	n, ok := g.NodeByID(id)
	if !ok {
		t.Fatalf("%s not found", id)
	}
	return n
}

func TestFilesAndTreeViews(t *testing.T) {
	g := planFixture(t)
	golden(t, "files.txt", Files("001", g, "").Text())
	staged := Files("001", g, "s-p6v5")
	golden(t, "files-stage.txt", staged.Text())
	if len(staged.Files) != 3 || staged.Stage != "s-p6v5" {
		t.Fatalf("stage filter = %+v", staged.Files)
	}
	if got := Files("001", g, "s-zzzz").Text(); !strings.Contains(got, "touches no file changes") {
		t.Fatalf("empty stage filter: %q", got)
	}
	v := TreeNode("001", g, mustNode(t, g, "t-nfba"))
	if len(v.Errors) != 0 || !slices.Equal(v.About, []string{"ac-jttt"}) {
		t.Fatalf("tree view = %+v", v)
	}
	golden(t, "tree-node.txt", v.Text())
}
