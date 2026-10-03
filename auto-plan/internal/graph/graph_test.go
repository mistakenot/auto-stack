package graph

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mistakenot/auto-plan/internal/schema"
)

func seeded(g *Graph) *Graph {
	g.SetRand(rand.New(rand.NewPCG(1, 2)))
	return g
}

func newPlan() *Graph {
	return seeded(New("001-k7q2", map[string]any{
		"name": "demo", "kind": "task", "lifecycle": "requirements", "created": "2026-01-01",
	}))
}

func codes(errs []ValidationError) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Code)
	}
	return out
}

// decisionFields returns a decision's required fields.
func decisionFields(title string) map[string]any {
	return map[string]any{"title": title, "chosen": "c", "why": "w", "by": "charlie"}
}

// acFields returns an AC's required fields.
func acFields(title string) map[string]any {
	return map[string]any{"title": title, "gwt": "Given a\nWhen b\nThen c"}
}

func mustAdd(t *testing.T, g *Graph, typ string, fields map[string]any, edges ...Edge) Node {
	t.Helper()
	n, _, errs := g.Add(typ, fields, edges)
	if len(errs) > 0 {
		t.Fatalf("Add(%s): %+v", typ, errs)
	}
	return n
}

// populated returns a plan with two goals, an AC, and a constraining decision
// with HTML-significant characters that must not be escaped.
func populated(t *testing.T) *Graph {
	t.Helper()
	g := newPlan()
	g1 := mustAdd(t, g, "goal", map[string]any{"title": "Plans <reorder> & keep refs"})
	mustAdd(t, g, "goal", map[string]any{"title": "Second goal"})
	ac := mustAdd(t, g, "ac", map[string]any{
		"title":  "brief is self-contained",
		"gwt":    "Given x\nWhen y\nThen z",
		"verify": map[string]any{"cmd": "go test ./e2e -run Brief", "tests": []any{"TestBrief"}},
	}, Edge{Type: "proves", To: g1.ID})
	d := mustAdd(t, g, "decision", decisionFields("brief is Markdown + JSON"))
	if _, errs := g.Link(d.ID, "constrains", ac.ID); len(errs) > 0 {
		t.Fatalf("Link: %+v", errs)
	}
	return g
}

func TestEncodeRoundTripIsByteIdentical(t *testing.T) {
	g := populated(t)
	first, err := Encode(g)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	if errs := Validate(parsed); len(errs) > 0 {
		t.Fatalf("canonical graph invalid: %+v", errs)
	}
	second, err := Encode(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("round trip changed bytes:\n%s\n---\n%s", first, second)
	}

	// Save → Decode → Save through the filesystem as well.
	path := filepath.Join(t.TempDir(), "graph.json")
	if err := Save(path, parsed); err != nil {
		t.Fatal(err)
	}
	again, err := Decode(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(path, again); err != nil {
		t.Fatal(err)
	}
	onDisk, _ := os.ReadFile(path)
	if !bytes.Equal(first, onDisk) {
		t.Fatalf("Save→Decode→Save changed bytes")
	}
}

func TestEncodeCanonicalForm(t *testing.T) {
	g := populated(t)
	data, err := Encode(g)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.HasSuffix(s, "}\n") || strings.HasSuffix(s, "\n\n") {
		t.Errorf("want exactly one trailing newline")
	}
	if !strings.Contains(s, "\n  \"nodes\": [\n    {\n      \"id\"") {
		t.Errorf("want 2-space indent with id first:\n%s", s)
	}
	if !strings.Contains(s, "Plans <reorder> & keep refs") {
		t.Errorf("HTML characters must not be escaped:\n%s", s)
	}
	// Nodes sorted by (type, id): ac < decision < goal < plan.
	var order []string
	for _, n := range mustParse(t, data).Nodes {
		order = append(order, n.Type)
	}
	if !slices.IsSorted(order) || order[len(order)-1] != "plan" {
		t.Errorf("nodes not sorted by type: %v", order)
	}
	// Key order within a node is fixed: id, type, status, rank, fields.
	i := strings.Index(s, `"id": "ac-`)
	if i < 0 {
		t.Fatal("no ac node")
	}
	block := s[i:]
	for _, k := range []string{`"type"`, `"status"`, `"rank"`, `"fields"`} {
		j := strings.Index(block, k)
		if j < 0 {
			t.Fatalf("missing %s in node", k)
		}
		block = block[j:]
	}
}

func mustParse(t *testing.T, data []byte) *Graph {
	t.Helper()
	g, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestParseErrorHasPosition(t *testing.T) {
	_, err := Parse([]byte("{\n  \"version\": 1,\n  \"nodes\": [,]\n}\n"))
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("want *ParseError, got %v", err)
	}
	if pe.Line != 3 {
		t.Errorf("line = %d, want 3 (%v)", pe.Line, pe)
	}
}

// brokenFixture is a hand-broken graph: a duplicate ID, a dangling edge, an
// unregistered node type, an unknown field, a bad ID and a wrong endpoint.
const brokenFixture = `{
  "version": "1.0.0",
  "id": "001-k7q2",
  "nodes": [
    {"id": "plan", "type": "plan", "status": "active", "fields": {"name": "demo", "kind": "task", "lifecycle": "requirements", "created": "2026-01-01"}},
    {"id": "g-k7q2", "type": "goal", "status": "active", "rank": "a0", "fields": {"title": "one"}},
    {"id": "g-k7q2", "type": "goal", "status": "active", "rank": "a1", "fields": {"title": "two", "colour": "red"}},
    {"id": "G1", "type": "goal", "status": "active", "rank": "a2", "fields": {"title": "three"}},
    {"id": "w-0000", "type": "widget", "status": "active", "rank": "a0", "fields": {}},
    {"id": "d-9t2w", "type": "decision", "status": "active", "rank": "a0", "fields": {"title": "d"}}
  ],
  "edges": [
    {"id": "e-0001", "from": "ac-zzzz", "type": "proves", "to": "g-k7q2"},
    {"id": "e-0002", "from": "d-9t2w", "type": "proves", "to": "g-k7q2"}
  ]
}
`

func TestBrokenFixtureDecodesAndValidateReportsEachProblem(t *testing.T) {
	g, err := Parse([]byte(brokenFixture))
	if err != nil {
		t.Fatalf("a structurally broken graph must still decode: %v", err)
	}
	got := codes(Validate(g))
	for _, want := range []string{
		CodeDuplicateID, CodeUnknownField, CodeBadID, CodeUnregisteredType, CodeDanglingRef, CodeWrongEndpoint,
	} {
		if !slices.Contains(got, want) {
			t.Errorf("Validate codes %v missing %q", got, want)
		}
	}

	// Writes against it are refused, and nothing changes.
	before, _ := Encode(g)
	seeded(g)
	if _, _, errs := g.Add("goal", map[string]any{"title": "new"}, nil); len(errs) == 0 {
		t.Fatal("Add on an invalid graph must be refused")
	}
	if _, errs := g.Link("d-9t2w", "constrains", "g-k7q2"); len(errs) == 0 {
		t.Fatal("Link on an invalid graph must be refused")
	}
	after, _ := Encode(g)
	if !bytes.Equal(before, after) {
		t.Fatal("refused writes changed the graph")
	}
}

func TestUnknownFieldsSurviveRewrite(t *testing.T) {
	g := mustParse(t, []byte(brokenFixture))
	data, err := Encode(g)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"colour": "red"`) || !strings.Contains(string(data), `"widget"`) {
		t.Fatalf("unknown content dropped on encode:\n%s", data)
	}
}

func TestValidateFieldRules(t *testing.T) {
	cases := []struct {
		name   string
		typ    string
		fields map[string]any
		want   string
	}{
		{"missing required", "goal", map[string]any{}, CodeMissingField},
		{"blank required", "goal", map[string]any{"title": "  "}, CodeMissingField},
		{"multi-line string", "goal", map[string]any{"title": "a\nb"}, CodeInvalidField},
		{"wrong type", "goal", map[string]any{"title": 3}, CodeInvalidField},
		{"unknown field", "goal", map[string]any{"title": "t", "nope": "x"}, CodeUnknownField},
		{"bad enum in object", "ac", map[string]any{"title": "t", "gwt": "g", "verify": map[string]any{"kind": "robot"}}, CodeInvalidField},
		{"list of non-strings", "ac", map[string]any{"title": "t", "gwt": "g", "verify": map[string]any{"tests": []any{1}}}, CodeInvalidField},
		{"ac without gwt", "ac", map[string]any{"title": "t"}, CodeMissingField},
		{"decision without why", "decision", map[string]any{"title": "t", "chosen": "c", "by": "b"}, CodeMissingField},
		{"bad reversibility", "decision", map[string]any{"title": "t", "chosen": "c", "why": "w", "by": "b", "reversibility": "sideways"}, CodeInvalidField},
		{"stage with no steps", "stage", map[string]any{"title": "t", "steps": []any{}, "commit": "c"}, CodeMissingField},
		{"file with bad change", "file", map[string]any{"path": "a.go", "change": "rename", "why": "w"}, CodeInvalidField},
		{"file with absolute path", "file", map[string]any{"path": "/etc/passwd", "change": "edit", "why": "w"}, CodeInvalidField},
		{"tree with bad kind", "tree", map[string]any{"title": "t", "kind": "graph", "body": "x"}, CodeInvalidField},
		{"question with bad status", "question", map[string]any{"title": "t", "status": "maybe"}, CodeInvalidField},
		{"child with bad plan number", "child", map[string]any{"plan": "12"}, CodeInvalidField},
		{"rail with bad deferral", "rail", map[string]any{"title": "t", "deferred": []any{"x"}}, CodeInvalidField},
		{"leg without action", "leg", map[string]any{"actor": "agent"}, CodeMissingField},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newPlan()
			before, _ := Encode(g)
			_, _, errs := g.Add(tc.typ, tc.fields, nil)
			if !slices.Contains(codes(errs), tc.want) {
				t.Fatalf("codes %v, want %q", codes(errs), tc.want)
			}
			after, _ := Encode(g)
			if !bytes.Equal(before, after) {
				t.Fatal("rejected Add changed the graph")
			}
		})
	}
}

func TestPlanFieldPatterns(t *testing.T) {
	g := New("001-k7q2", map[string]any{"name": "Bad Name", "kind": "story", "lifecycle": "requirements", "created": "yesterday"})
	got := codes(Validate(g))
	if n := strings.Count(strings.Join(got, ","), CodeInvalidField); n != 3 {
		t.Fatalf("want 3 invalid-field (name, kind, created), got %v", got)
	}
}

func TestIDsMatchPatternAndRetryCollisions(t *testing.T) {
	g := populated(t)
	for _, n := range g.Nodes {
		if n.Type == "plan" {
			continue
		}
		if !IDPattern.MatchString(n.ID) {
			t.Errorf("ID %q does not match %s", n.ID, IDPattern)
		}
	}

	// A seeded source replays the same sequence, so pre-taking the first ID it
	// would produce forces a retry.
	first, err := NewID("g", rand.New(rand.NewPCG(7, 7)), func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	var tried []string
	second, err := NewID("g", rand.New(rand.NewPCG(7, 7)), func(id string) bool {
		tried = append(tried, id)
		return id == first
	})
	if err != nil {
		t.Fatal(err)
	}
	if second == first || len(tried) != 2 || tried[0] != first {
		t.Fatalf("collision not retried: first=%s second=%s tried=%v", first, second, tried)
	}

	// Retired IDs count as taken: every existing ID is checked.
	g2 := newPlan()
	g2.Nodes = append(g2.Nodes, Node{ID: first, Type: "goal", Status: StatusRetired, Rank: "a0", Fields: map[string]any{"title": "old"}})
	g2.SetRand(rand.New(rand.NewPCG(7, 7)))
	n, _, errs := g2.Add("goal", map[string]any{"title": "new"}, nil)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if n.ID == first {
		t.Fatalf("new node reused retired ID %s", first)
	}
}

func TestIDSpaceExhausted(t *testing.T) {
	if _, err := NewID("g", rand.New(rand.NewPCG(1, 1)), func(string) bool { return true }); !errors.Is(err, ErrIDSpaceExhausted) {
		t.Fatalf("want ErrIDSpaceExhausted, got %v", err)
	}
}

func TestRankAfterSiblings(t *testing.T) {
	g := newPlan()
	g1 := mustAdd(t, g, "goal", map[string]any{"title": "a"})
	g2 := mustAdd(t, g, "goal", map[string]any{"title": "b"})
	if g1.Rank != RankFirst || g2.Rank != "a1" {
		t.Fatalf("goal ranks = %s, %s; want a0, a1", g1.Rank, g2.Rank)
	}
	// ACs rank among the ACs proving the same goal.
	a1 := mustAdd(t, g, "ac", acFields("x"), Edge{Type: "proves", To: g1.ID})
	a2 := mustAdd(t, g, "ac", acFields("y"), Edge{Type: "proves", To: g1.ID})
	b1 := mustAdd(t, g, "ac", acFields("z"), Edge{Type: "proves", To: g2.ID})
	if a1.Rank != "a0" || a2.Rank != "a1" || b1.Rank != "a0" {
		t.Fatalf("ac ranks = %s %s %s; want a0 a1 a0", a1.Rank, a2.Rank, b1.Rank)
	}

	for in, want := range map[string]string{"": "a0", "a0": "a1", "a9": "aa", "az": "az1", "b": "c"} {
		if got := RankAfter(in); got != want || (in != "" && got <= in) {
			t.Errorf("RankAfter(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRejectedAddLeavesGraphDeepEqual(t *testing.T) {
	g := populated(t)
	snapshot := g.clone()
	_, _, errs := g.Add("ac", acFields("t"), []Edge{{Type: "proves", To: "g-zzzz"}})
	if !slices.Contains(codes(errs), CodeDanglingRef) {
		t.Fatalf("want dangling-ref, got %v", codes(errs))
	}
	if !reflect.DeepEqual(g.Nodes, snapshot.Nodes) || !reflect.DeepEqual(g.Edges, snapshot.Edges) {
		t.Fatal("rejected Add changed the graph")
	}

	for _, tc := range []struct{ from, typ, to, want string }{
		{"d-zzzz", "constrains", "g-zzzz", CodeDanglingRef},
		{g.Nodes[0].ID, "constrains", g.Nodes[0].ID, CodeWrongEndpoint}, // ac constrains ac
		{g.Nodes[0].ID, "nope", g.Nodes[0].ID, CodeUnregisteredType},
		{g.Nodes[0].ID, "proves", "005-m3x9:g-k7q2", CodeWrongEndpoint}, // proves is not cross-plan
	} {
		_, errs := g.Link(tc.from, tc.typ, tc.to)
		if !slices.Contains(codes(errs), tc.want) {
			t.Errorf("Link(%s %s %s) codes %v, want %s", tc.from, tc.typ, tc.to, codes(errs), tc.want)
		}
	}
	if !reflect.DeepEqual(g.Edges, snapshot.Edges) {
		t.Fatal("rejected Link changed the graph")
	}

	// A duplicate edge is refused.
	e := g.Edges[0]
	if _, errs := g.Link(e.From, e.Type, e.To); !slices.Contains(codes(errs), CodeDuplicateEdge) {
		t.Fatalf("duplicate edge: codes %v", codes(errs))
	}
}

func TestAddRefusesPlanAndUnregistered(t *testing.T) {
	g := newPlan()
	if _, _, errs := g.Add("plan", nil, nil); !slices.Contains(codes(errs), CodeNotAddable) {
		t.Fatalf("plan: %v", codes(errs))
	}
	if _, _, errs := g.Add("widget", nil, nil); !slices.Contains(codes(errs), CodeUnregisteredType) {
		t.Fatalf("widget: %v", codes(errs))
	}
}

func TestMissingPlanNodeAndBadVersion(t *testing.T) {
	g := mustParse(t, []byte(`{"version": "one", "nodes": [], "edges": [], "extra": true}`))
	got := codes(Validate(g))
	for _, want := range []string{CodeBadVersion, CodeMissingPlanNode, CodeUnknownField} {
		if !slices.Contains(got, want) {
			t.Errorf("codes %v missing %s", got, want)
		}
	}
	g = mustParse(t, []byte(`[1, 2]`))
	if got := codes(Validate(g)); !slices.Contains(got, CodeInvalidGraph) {
		t.Errorf("non-object graph: %v", got)
	}
}

func TestParseRef(t *testing.T) {
	if p, id, q := ParseRef("005:r-8hw3"); p != "005" || id != "r-8hw3" || !q {
		t.Fatalf("qualified: %s %s %v", p, id, q)
	}
	if p, id, q := ParseRef("r-8hw3"); p != "" || id != "r-8hw3" || q {
		t.Fatalf("local: %s %s %v", p, id, q)
	}
}

// everyType builds a plan holding one active node of every addable type,
// linked by every edge type, and returns the IDs by type.
func everyType(t *testing.T) (*Graph, map[string]string) {
	t.Helper()
	g := newPlan()
	ids := map[string]string{}
	add := func(typ string, fields map[string]any, edges ...Edge) {
		ids[typ] = mustAdd(t, g, typ, fields, edges...).ID
	}
	add("goal", map[string]any{"title": "g"})
	add("ac", acFields("a"), Edge{Type: "proves", To: ids["goal"]})
	add("decision", decisionFields("d"))
	add("alternative", map[string]any{"title": "alt", "why": "slower"})
	add("rail", map[string]any{"title": "r", "deferred": []any{"002-a1b2"}})
	add("defect", map[string]any{"title": "df"})
	add("file", map[string]any{"path": "auto-plan/internal/x.go", "change": "add", "why": "w"})
	add("stage", map[string]any{"title": "s", "steps": []any{"one", "two"}, "commit": "feat: s"},
		Edge{Type: "touches", To: ids["file"]}, Edge{Type: "covers", To: ids["ac"]})
	add("question", map[string]any{"title": "q?", "status": "open", "recommended": "yes"})
	add("tree", map[string]any{"title": "t", "kind": "call", "body": "main\n  run"}, Edge{Type: "about", To: ids["stage"]})
	add("journey", map[string]any{"title": "j"})
	add("leg", map[string]any{"actor": "agent", "action": "plans"}, Edge{Type: "in", To: ids["journey"]})
	add("child", map[string]any{"plan": "002-a1b2"})
	for _, l := range []struct{ from, typ, to string }{
		{ids["decision"], "constrains", ids["ac"]},
		{ids["decision"], "rejects", ids["alternative"]},
		{ids["alternative"], "wouldBreak", ids["rail"]},
		{ids["ac"], "discharges", ids["rail"]},
		{ids["goal"], "addresses", ids["defect"]},
		{"plan", "honors", "005-m3x9:r-8hw3"},
		{"plan", "delivers", "005-m3x9:l-2qdn"},
		{"plan", "builds-on", "005-m3x9:d-6yb4"},
	} {
		if _, errs := g.Link(l.from, l.typ, l.to); len(errs) > 0 {
			t.Fatalf("Link(%s %s %s): %+v", l.from, l.typ, l.to, errs)
		}
	}
	return g, ids
}

func TestEveryNodeTypeRoundTrips(t *testing.T) {
	g, _ := everyType(t)
	first, err := Encode(g)
	if err != nil {
		t.Fatal(err)
	}
	parsed := mustParse(t, first)
	if errs := Validate(parsed); len(errs) > 0 {
		t.Fatalf("every-type graph invalid: %+v", errs)
	}
	second, _ := Encode(parsed)
	if !bytes.Equal(first, second) {
		t.Fatal("every-type graph is not byte-stable")
	}
}

func TestSameTypeAndAnyEndpoints(t *testing.T) {
	g, ids := everyType(t)
	s2 := mustAdd(t, g, "stage", map[string]any{"title": "s2", "steps": []any{"x"}, "commit": "c"})
	c2 := mustAdd(t, g, "child", map[string]any{"plan": "003-c3d4"})
	if _, errs := g.Link(s2.ID, "dependsOn", ids["stage"]); len(errs) > 0 {
		t.Fatalf("stage dependsOn stage: %+v", errs)
	}
	if _, errs := g.Link(c2.ID, "dependsOn", ids["child"]); len(errs) > 0 {
		t.Fatalf("child dependsOn child: %+v", errs)
	}
	if _, errs := g.Link(s2.ID, "dependsOn", ids["child"]); !slices.Contains(codes(errs), CodeWrongEndpoint) {
		t.Fatalf("stage dependsOn child: codes %v, want wrong-endpoint", codes(errs))
	}
	// about accepts any node type, the plan node included.
	for _, to := range []string{ids["goal"], ids["leg"], "plan"} {
		if _, errs := g.Link(ids["tree"], "about", to); len(errs) > 0 {
			t.Fatalf("tree about %s: %+v", to, errs)
		}
	}
	if _, errs := g.Link(ids["goal"], "about", ids["tree"]); !slices.Contains(codes(errs), CodeWrongEndpoint) {
		t.Fatalf("goal about tree: codes %v", codes(errs))
	}
}

// snapshotOf deep-copies g's content for all-or-nothing checks.
func snapshotOf(t *testing.T, g *Graph) []byte {
	t.Helper()
	data, err := Encode(g)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestUpdateMergesAndReportsChanges(t *testing.T) {
	g, ids := everyType(t)
	changed, errs := g.Update(ids["ac"], map[string]any{
		"title":  "renamed",
		"gwt":    acFields("")["gwt"], // unchanged value: not reported
		"verify": map[string]any{"cmd": "go test ./...", "kind": "command"},
	})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if _, reported := changed["gwt"]; reported || changed["title"] != "renamed" || changed["verify"] == nil {
		t.Fatalf("changed = %v", changed)
	}
	// Object members merge; a nil member removes one, and an emptied object goes.
	if _, errs := g.Update(ids["ac"], map[string]any{"verify": map[string]any{"kind": nil}}); len(errs) > 0 {
		t.Fatal(errs)
	}
	n, _ := g.NodeByID(ids["ac"])
	if v := n.ObjectField("verify"); v["cmd"] != "go test ./..." || v["kind"] != nil {
		t.Fatalf("verify = %v", v)
	}
	if _, errs := g.Update(ids["ac"], map[string]any{"verify": map[string]any{"cmd": nil}}); len(errs) > 0 {
		t.Fatal(errs)
	}
	if n, _ := g.NodeByID(ids["ac"]); n.Fields["verify"] != nil {
		t.Fatalf("emptied verify kept: %v", n.Fields)
	}
	// A nil value removes an optional field.
	if _, errs := g.Update(ids["question"], map[string]any{"recommended": nil}); len(errs) > 0 {
		t.Fatal(errs)
	}
	if n, _ := g.NodeByID(ids["question"]); n.Fields["recommended"] != nil {
		t.Fatal("recommended not removed")
	}
	// The plan node advances its lifecycle.
	if ch, errs := g.Update("plan", map[string]any{"lifecycle": "solution"}); len(errs) > 0 || ch["lifecycle"] != "solution" {
		t.Fatalf("lifecycle: %v %v", ch, errs)
	}
}

func TestMutationsAreAllOrNothing(t *testing.T) {
	g, ids := everyType(t)
	before := snapshotOf(t, g)
	cases := []struct {
		name string
		run  func() []ValidationError
		want string
	}{
		{"update missing node", func() []ValidationError { _, e := g.Update("g-zzzz", map[string]any{"title": "x"}); return e }, CodeNodeNotFound},
		{"update removes required", func() []ValidationError { _, e := g.Update(ids["goal"], map[string]any{"title": nil}); return e }, CodeMissingField},
		{"update bad enum", func() []ValidationError {
			_, e := g.Update(ids["goal"], map[string]any{"title": "ok", "description": 3})
			return e
		}, CodeInvalidField},
		{"update unknown field", func() []ValidationError { _, e := g.Update(ids["goal"], map[string]any{"colour": "red"}); return e }, CodeUnknownField},
		{"update fixed field", func() []ValidationError { _, e := g.Update("plan", map[string]any{"name": "other"}); return e }, CodeFixedField},
		{"update bad lifecycle", func() []ValidationError { _, e := g.Update("plan", map[string]any{"lifecycle": "shipped"}); return e }, CodeInvalidField},
		{"unlink missing edge", func() []ValidationError { _, e := g.Unlink(ids["decision"], "constrains", ids["goal"]); return e }, CodeEdgeNotFound},
		{"retire missing", func() []ValidationError { _, e := g.Retire("g-zzzz"); return e }, CodeNodeNotFound},
		{"retire plan", func() []ValidationError { _, e := g.Retire("plan"); return e }, CodeNotRetirable},
		{"move plan", func() []ValidationError { _, e := g.Move("plan", ids["goal"], false); return e }, CodeNotMovable},
		{"move to other type", func() []ValidationError { _, e := g.Move(ids["goal"], ids["ac"], false); return e }, CodeNotSibling},
		{"move to missing", func() []ValidationError { _, e := g.Move(ids["goal"], "g-zzzz", false); return e }, CodeNodeNotFound},
		{"move before itself", func() []ValidationError { _, e := g.Move(ids["goal"], ids["goal"], false); return e }, CodeNotSibling},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if errs := tc.run(); !slices.Contains(codes(errs), tc.want) {
				t.Fatalf("codes %v, want %s", codes(errs), tc.want)
			}
			if after := snapshotOf(t, g); !bytes.Equal(before, after) {
				t.Fatal("rejected mutation changed the graph")
			}
		})
	}
}

func TestUnlinkRemovesOneEdge(t *testing.T) {
	g, ids := everyType(t)
	n := len(g.Edges)
	e, errs := g.Unlink(ids["decision"], "rejects", ids["alternative"])
	if len(errs) > 0 || e.Type != "rejects" {
		t.Fatalf("Unlink: %v %v", e, errs)
	}
	if len(g.Edges) != n-1 {
		t.Fatalf("edges %d → %d", n, len(g.Edges))
	}
	for _, x := range g.Edges {
		if x.From == e.From && x.Type == e.Type && x.To == e.To {
			t.Fatal("edge still present")
		}
	}
}

func TestRetireKeepsIDAndIsNeverReused(t *testing.T) {
	g := newPlan()
	goal := mustAdd(t, g, "goal", map[string]any{"title": "g"})
	ac := mustAdd(t, g, "ac", acFields("a"), Edge{Type: "proves", To: goal.ID})
	r, errs := g.Retire(ac.ID)
	if len(errs) > 0 || r.ID != ac.ID || r.Status != StatusRetired {
		t.Fatalf("Retire = %+v %v", r, errs)
	}
	n, ok := g.NodeByID(ac.ID)
	if !ok || n.Status != StatusRetired || n.Rank != ac.Rank || n.StringField("title") != "a" || len(g.outgoing(ac.ID)) != 1 {
		t.Fatalf("retired node lost data: %+v", n)
	}
	if _, errs := g.Retire(ac.ID); len(errs) > 0 {
		t.Fatalf("retiring twice must be a no-op: %v", errs)
	}
	// Replaying the seed that produced ac's ID must not hand it out again.
	g.SetRand(rand.New(rand.NewPCG(1, 2)))
	for range 3 {
		if n := mustAdd(t, g, "ac", acFields("b"), Edge{Type: "proves", To: goal.ID}); n.ID == ac.ID {
			t.Fatalf("retired ID %s reused", ac.ID)
		}
	}
}

func TestMoveChangesExactlyOneRank(t *testing.T) {
	g := newPlan()
	var goals []Node
	for _, title := range []string{"a", "b", "c", "d"} {
		goals = append(goals, mustAdd(t, g, "goal", map[string]any{"title": title}))
	}
	order := func() []string {
		sibs := slices.Clone(g.siblings(mustNodeType(t, "goal"), ""))
		slices.SortFunc(sibs, func(a, b Node) int { return strings.Compare(a.Rank, b.Rank) })
		var out []string
		for _, n := range sibs {
			out = append(out, n.StringField("title"))
		}
		return out
	}
	moves := []struct {
		id, anchor string
		after      bool
		want       []string
	}{
		{goals[3].ID, goals[0].ID, false, []string{"d", "a", "b", "c"}},
		{goals[0].ID, goals[2].ID, true, []string{"d", "b", "c", "a"}},
		{goals[1].ID, goals[3].ID, false, []string{"b", "d", "c", "a"}},
		{goals[2].ID, goals[0].ID, true, []string{"b", "d", "a", "c"}},
		{goals[2].ID, goals[3].ID, false, []string{"b", "c", "d", "a"}},
		{goals[2].ID, goals[1].ID, false, []string{"c", "b", "d", "a"}},
		{goals[1].ID, goals[0].ID, true, []string{"c", "d", "a", "b"}},
	}
	for i, m := range moves {
		before := map[string]Node{}
		for _, n := range g.Nodes {
			before[n.ID] = n
		}
		moved, errs := g.Move(m.id, m.anchor, m.after)
		if len(errs) > 0 {
			t.Fatalf("move %d: %v", i, errs)
		}
		for _, n := range g.Nodes {
			changed := !reflect.DeepEqual(before[n.ID], n)
			if changed != (n.ID == m.id) {
				t.Fatalf("move %d: node %s changed=%v", i, n.ID, changed)
			}
		}
		if moved.Rank == before[m.id].Rank {
			t.Fatalf("move %d: rank unchanged", i)
		}
		if got := order(); !slices.Equal(got, m.want) {
			t.Fatalf("move %d: order %v, want %v", i, got, m.want)
		}
	}
}

func TestMoveStaysWithinRankScope(t *testing.T) {
	g := newPlan()
	g1 := mustAdd(t, g, "goal", map[string]any{"title": "g1"})
	g2 := mustAdd(t, g, "goal", map[string]any{"title": "g2"})
	a1 := mustAdd(t, g, "ac", acFields("a1"), Edge{Type: "proves", To: g1.ID})
	a2 := mustAdd(t, g, "ac", acFields("a2"), Edge{Type: "proves", To: g1.ID})
	b1 := mustAdd(t, g, "ac", acFields("b1"), Edge{Type: "proves", To: g2.ID})
	moved, errs := g.Move(a2.ID, a1.ID, false)
	if len(errs) > 0 || moved.Rank >= a1.Rank {
		t.Fatalf("move a2 before a1: %+v %v", moved, errs)
	}
	if _, errs := g.Move(a2.ID, b1.ID, false); !slices.Contains(codes(errs), CodeNotSibling) {
		t.Fatalf("ACs under different goals are not siblings: %v", codes(errs))
	}
}

func mustNodeType(t *testing.T, name string) schema.NodeType {
	t.Helper()
	nt, ok := schema.Registry.Node(name)
	if !ok {
		t.Fatalf("no node type %s", name)
	}
	return nt
}

func TestRankBetween(t *testing.T) {
	for _, tc := range []struct{ lo, hi string }{
		{"", ""}, {"", "a0"}, {"a0", ""}, {"a0", "a1"}, {"a0", "a2"}, {"9", "a0"}, {"", "1"},
		{"", "0i"}, {"az", "az1"}, {"a0i", "a1"}, {"az1", ""}, {"a", "b"}, {"zz", ""},
	} {
		got, err := RankBetween(tc.lo, tc.hi)
		if err != nil {
			t.Errorf("RankBetween(%q, %q): %v", tc.lo, tc.hi, err)
			continue
		}
		if got <= tc.lo || (tc.hi != "" && got >= tc.hi) || strings.HasSuffix(got, "0") || !RankPattern.MatchString(got) {
			t.Errorf("RankBetween(%q, %q) = %q", tc.lo, tc.hi, got)
		}
	}
	for _, tc := range []struct{ lo, hi string }{{"a1", "a1"}, {"a2", "a1"}, {"a", "a0"}, {"a", "a00"}, {"A", ""}} {
		if got, err := RankBetween(tc.lo, tc.hi); err == nil {
			t.Errorf("RankBetween(%q, %q) = %q, want an error", tc.lo, tc.hi, got)
		}
	}
	// Repeated insertion at the front, the back and one gap stays ordered.
	r := rand.New(rand.NewPCG(3, 4))
	keys := []string{RankFirst}
	for range 500 {
		i := r.IntN(len(keys) + 1)
		lo, hi := "", ""
		if i > 0 {
			lo = keys[i-1]
		}
		if i < len(keys) {
			hi = keys[i]
		}
		k, err := RankBetween(lo, hi)
		if err != nil {
			t.Fatalf("RankBetween(%q, %q): %v", lo, hi, err)
		}
		keys = slices.Insert(keys, i, k)
	}
	if !slices.IsSorted(keys) || len(slices.Compact(slices.Clone(keys))) != len(keys) {
		t.Fatal("inserted keys are not strictly ordered")
	}
}

func TestParseRefFullAndShorthand(t *testing.T) {
	if p, id, q := ParseRef("005-k7q2:r-8hw3"); p != "005-k7q2" || id != "r-8hw3" || !q {
		t.Fatalf("full: %s %s %v", p, id, q)
	}
	if !QualifiedRefPattern.MatchString("005-k7q2:plan") || QualifiedRefPattern.MatchString("005:plan") {
		t.Error("QualifiedRefPattern must take the plan ID, not the bare number")
	}
	if !ShortRefPattern.MatchString("005:r-8hw3") || ShortRefPattern.MatchString("005-k7q2:r-8hw3") {
		t.Error("ShortRefPattern must take the bare number only")
	}
}

// TestEnvelopeKeyOrder: version, then id, then nodes and edges; each edge
// is id, from, type, to.
func TestEnvelopeKeyOrder(t *testing.T) {
	g := newPlan()
	goal := mustAdd(t, g, "goal", map[string]any{"title": "g"})
	mustAdd(t, g, "ac", acFields("a"), Edge{Type: "proves", To: goal.ID})
	data, err := Encode(g)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.HasPrefix(s, "{\n  \"version\": \"1.0.0\",\n  \"id\": \"001-k7q2\",\n  \"nodes\": [") {
		t.Fatalf("envelope head:\n%s", s[:min(len(s), 120)])
	}
	if !regexp.MustCompile(`\{\n      "id": "e-[0-9a-z]{4}",\n      "from": "ac-`).MatchString(s) {
		t.Fatalf("edge key order:\n%s", s)
	}
}

// TestEdgesGetIDsDrawnAfterTheNode: Add draws the node ID first, so the same
// seed gives the same node ID whether or not edges come with it; edge IDs are
// unique across nodes and edges.
func TestEdgesGetIDsDrawnAfterTheNode(t *testing.T) {
	base := newPlan()
	goal := mustAdd(t, base, "goal", map[string]any{"title": "g"})
	enc, _ := Encode(base)

	withEdge := mustParse(t, enc)
	withEdge.SetRand(rand.New(rand.NewPCG(9, 9)))
	n1, edges, errs := withEdge.Add("ac", acFields("a"), []Edge{{Type: "proves", To: goal.ID}})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	without := mustParse(t, enc)
	without.SetRand(rand.New(rand.NewPCG(9, 9)))
	n2, _, errs := without.Add("ac", acFields("a"), nil)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if n1.ID != n2.ID {
		t.Fatalf("node ID depends on its edges: %s vs %s", n1.ID, n2.ID)
	}
	if len(edges) != 1 || !EdgeIDPattern.MatchString(edges[0].ID) || edges[0].From != n1.ID {
		t.Fatalf("edges: %+v", edges)
	}
	if e, ok := withEdge.EdgeByID(edges[0].ID); !ok || e.To != goal.ID {
		t.Fatalf("EdgeByID: %+v %v", e, ok)
	}
}

func TestLinkCollisionRetryCoversEdges(t *testing.T) {
	g := newPlan()
	goal := mustAdd(t, g, "goal", map[string]any{"title": "g"})
	d1 := mustAdd(t, g, "decision", decisionFields("d1"))
	d2 := mustAdd(t, g, "decision", decisionFields("d2"))
	enc, _ := Encode(g)
	// Two links drawn from the same seed: the second must retry past the first.
	g = mustParse(t, enc)
	g.SetRand(rand.New(rand.NewPCG(5, 5)))
	e1, errs := g.Link(d1.ID, "constrains", goal.ID)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	g.SetRand(rand.New(rand.NewPCG(5, 5)))
	e2, errs := g.Link(d2.ID, "constrains", goal.ID)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if e1.ID == e2.ID || !EdgeIDPattern.MatchString(e2.ID) {
		t.Fatalf("edge IDs %s %s", e1.ID, e2.ID)
	}
	// A duplicate (from, type, to) is still refused despite its fresh ID.
	if _, errs := g.Link(d1.ID, "constrains", goal.ID); !slices.Contains(codes(errs), CodeDuplicateEdge) {
		t.Fatalf("duplicate triple: %v", codes(errs))
	}
	// Unlink by ID.
	got, errs := g.UnlinkID(e1.ID)
	if len(errs) > 0 || got.From != d1.ID {
		t.Fatalf("UnlinkID: %+v %v", got, errs)
	}
	if _, ok := g.EdgeByID(e1.ID); ok {
		t.Fatal("edge still present")
	}
	if _, errs := g.UnlinkID("e-zzzz"); !slices.Contains(codes(errs), CodeEdgeNotFound) {
		t.Fatalf("unknown edge ID: %v", codes(errs))
	}
}

func TestEdgeAndPlanIDValidation(t *testing.T) {
	doc := func(id, edges string) string {
		return `{"version": "1.0.0", ` + id + `"nodes": [
  {"id": "plan", "type": "plan", "status": "active", "fields": {"name": "demo", "kind": "task", "lifecycle": "requirements", "created": "2026-01-01"}},
  {"id": "g-k7q2", "type": "goal", "status": "active", "rank": "a0", "fields": {"title": "g"}},
  {"id": "d-9t2w", "type": "decision", "status": "active", "rank": "a0", "fields": {"title": "d", "chosen": "c", "why": "w", "by": "b"}},
  {"id": "d-9t2x", "type": "decision", "status": "active", "rank": "a1", "fields": {"title": "d", "chosen": "c", "why": "w", "by": "b"}}
], "edges": [` + edges + `]}`
	}
	cases := []struct {
		name, id, edges, want string
	}{
		{"missing plan ID", ``, ``, CodeMissingField},
		{"bad plan ID", `"id": "001-K7Q2", `, ``, CodeBadID},
		{"missing edge ID", `"id": "001-k7q2", `, `{"from": "d-9t2w", "type": "constrains", "to": "g-k7q2"}`, CodeMissingField},
		{"bad edge ID", `"id": "001-k7q2", `, `{"id": "x-0000", "from": "d-9t2w", "type": "constrains", "to": "g-k7q2"}`, CodeBadID},
		{"edge ID reused", `"id": "001-k7q2", `, `{"id": "e-0000", "from": "d-9t2w", "type": "constrains", "to": "g-k7q2"}, {"id": "e-0000", "from": "d-9t2x", "type": "constrains", "to": "g-k7q2"}`, CodeDuplicateID},
		{"shorthand target stored", `"id": "001-k7q2", `, `{"id": "e-0000", "from": "d-9t2w", "type": "builds-on", "to": "005:d-0000"}`, CodeDanglingRef},
	}
	for _, tc := range cases {
		got := codes(Validate(mustParse(t, []byte(doc(tc.id, tc.edges)))))
		if !slices.Contains(got, tc.want) {
			t.Errorf("%s: codes %v, want %s", tc.name, got, tc.want)
		}
	}
	if got := Validate(mustParse(t, []byte(doc(`"id": "001-k7q2", `, `{"id": "e-0000", "from": "d-9t2w", "type": "constrains", "to": "g-k7q2"}`)))); len(got) > 0 {
		t.Fatalf("valid doc: %+v", got)
	}
}

// TestVersionsAndFreezing: reads tolerate any 1.x.y; another major decodes
// and is not checked against this registry; writes are refused for another
// major, a newer version, or a done plan.
func TestVersionsAndFreezing(t *testing.T) {
	doc := func(version, lifecycle string) *Graph {
		return mustParse(t, []byte(`{"version": "`+version+`", "id": "001-k7q2", "nodes": [
  {"id": "plan", "type": "plan", "status": "active", "fields": {"name": "demo", "kind": "task", "lifecycle": "`+lifecycle+`", "created": "2026-01-01"}},
  {"id": "w-0000", "type": "widget", "status": "active", "rank": "a0", "fields": {}}
], "edges": []}`))
	}
	if g := doc("1.0.0", "requirements"); g.Frozen() != "" || g.OtherVersion() {
		t.Errorf("1.0.0 requirements: frozen %q", g.Frozen())
	}
	if g := doc("1.0.0", "done"); !strings.Contains(g.Frozen(), "done") {
		t.Errorf("done plan not frozen: %q", g.Frozen())
	}
	newer := doc("1.4.0", "requirements")
	if !newer.OtherVersion() || newer.OtherMajor() || newer.Frozen() == "" {
		t.Errorf("1.4.0: other %v major %v frozen %q", newer.OtherVersion(), newer.OtherMajor(), newer.Frozen())
	}
	if got := codes(Validate(newer)); !slices.Contains(got, CodeUnregisteredType) {
		t.Errorf("a 1.x plan is still validated: %v", got)
	}
	other := doc("2.0.0", "requirements")
	if !other.OtherMajor() || other.Frozen() == "" {
		t.Errorf("2.0.0: major %v frozen %q", other.OtherMajor(), other.Frozen())
	}
	if got := Validate(other); len(got) != 0 {
		t.Errorf("another major is not checked against this registry: %v", codes(got))
	}
	if got := codes(Validate(mustParse(t, []byte(`{"version": 1, "id": "001-k7q2", "nodes": [], "edges": []}`)))); !slices.Contains(got, CodeInvalidField) || slices.Contains(got, CodeBadVersion) {
		t.Errorf("integer version: %v (want one invalid-field, no duplicate bad-version)", got)
	}
}

func TestRewritePlanRefs(t *testing.T) {
	g := mustParse(t, []byte(`{"version": "1.0.0", "id": "001-k7q2", "nodes": [
  {"id": "plan", "type": "plan", "status": "active", "fields": {"name": "e", "kind": "epic", "lifecycle": "requirements", "created": "2026-01-01"}},
  {"id": "c-0000", "type": "child", "status": "active", "rank": "a0", "fields": {"plan": "002-m3x9"}},
  {"id": "r-0000", "type": "rail", "status": "active", "rank": "a0", "fields": {"title": "r", "deferred": ["002-m3x9", "003-aaaa"], "description": "see [[002-m3x9:g-0000]], [[002:g-0001]] and [[003-aaaa:g-0000]]"}}
], "edges": [{"id": "e-0000", "from": "c-0000", "type": "dependsOn", "to": "c-0000"}]}`))
	orig, _ := Encode(g)
	c := mustParse(t, orig)
	if !c.RewritePlanRefs("002-m3x9", "004-m3x9", "002", "004") {
		t.Fatal("nothing rewritten")
	}
	child, _ := c.NodeByID("c-0000")
	rail, _ := c.NodeByID("r-0000")
	if child.StringField("plan") != "004-m3x9" {
		t.Errorf("child.plan = %v", child.Fields["plan"])
	}
	if got := rail.Fields["deferred"].([]any); got[0] != "004-m3x9" || got[1] != "003-aaaa" {
		t.Errorf("deferred = %v", got)
	}
	if got := rail.StringField("description"); got != "see [[004-m3x9:g-0000]], [[004:g-0001]] and [[003-aaaa:g-0000]]" {
		t.Errorf("prose = %q", got)
	}
	if again, _ := Encode(g); !bytes.Equal(again, orig) {
		t.Error("the source graph changed")
	}
	if c.RewritePlanRefs("009-aaaa", "010-aaaa", "", "") {
		t.Error("no reference to 009-aaaa, yet something changed")
	}
}
