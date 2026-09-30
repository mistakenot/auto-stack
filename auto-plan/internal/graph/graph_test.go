package graph

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func seeded(g *Graph) *Graph {
	g.SetRand(rand.New(rand.NewPCG(1, 2)))
	return g
}

func newPlan() *Graph {
	return seeded(New(map[string]any{
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

func mustAdd(t *testing.T, g *Graph, typ string, fields map[string]any, edges ...Edge) Node {
	t.Helper()
	n, errs := g.Add(typ, fields, edges)
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
	d := mustAdd(t, g, "decision", map[string]any{"title": "brief is Markdown + JSON"})
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
  "version": 1,
  "nodes": [
    {"id": "plan", "type": "plan", "status": "active", "fields": {"name": "demo", "kind": "task", "lifecycle": "requirements", "created": "2026-01-01"}},
    {"id": "g-k7q2", "type": "goal", "status": "active", "rank": "a0", "fields": {"title": "one"}},
    {"id": "g-k7q2", "type": "goal", "status": "active", "rank": "a1", "fields": {"title": "two", "colour": "red"}},
    {"id": "G1", "type": "goal", "status": "active", "rank": "a2", "fields": {"title": "three"}},
    {"id": "w-0000", "type": "widget", "status": "active", "rank": "a0", "fields": {}},
    {"id": "d-9t2w", "type": "decision", "status": "active", "rank": "a0", "fields": {"title": "d"}}
  ],
  "edges": [
    {"from": "ac-zzzz", "type": "proves", "to": "g-k7q2"},
    {"from": "d-9t2w", "type": "proves", "to": "g-k7q2"}
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
	if _, errs := g.Add("goal", map[string]any{"title": "new"}, nil); len(errs) == 0 {
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
		{"bad enum in object", "ac", map[string]any{"title": "t", "verify": map[string]any{"kind": "robot"}}, CodeInvalidField},
		{"list of non-strings", "ac", map[string]any{"title": "t", "verify": map[string]any{"tests": []any{1}}}, CodeInvalidField},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newPlan()
			before, _ := Encode(g)
			_, errs := g.Add(tc.typ, tc.fields, nil)
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
	g := New(map[string]any{"name": "Bad Name", "kind": "story", "lifecycle": "requirements", "created": "yesterday"})
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
	n, errs := g2.Add("goal", map[string]any{"title": "new"}, nil)
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
	a1 := mustAdd(t, g, "ac", map[string]any{"title": "x"}, Edge{Type: "proves", To: g1.ID})
	a2 := mustAdd(t, g, "ac", map[string]any{"title": "y"}, Edge{Type: "proves", To: g1.ID})
	b1 := mustAdd(t, g, "ac", map[string]any{"title": "z"}, Edge{Type: "proves", To: g2.ID})
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
	_, errs := g.Add("ac", map[string]any{"title": "t"}, []Edge{{Type: "proves", To: "g-zzzz"}})
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
		{g.Nodes[0].ID, "proves", "005:g-k7q2", CodeWrongEndpoint}, // proves is not cross-plan
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
	if _, errs := g.Add("plan", nil, nil); !slices.Contains(codes(errs), CodeNotAddable) {
		t.Fatalf("plan: %v", codes(errs))
	}
	if _, errs := g.Add("widget", nil, nil); !slices.Contains(codes(errs), CodeUnregisteredType) {
		t.Fatalf("widget: %v", codes(errs))
	}
}

func TestMissingPlanNodeAndBadVersion(t *testing.T) {
	g := mustParse(t, []byte(`{"version": 2, "nodes": [], "edges": [], "extra": true}`))
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
