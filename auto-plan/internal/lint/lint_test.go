package lint

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mistakenot/auto-plan/internal/graph"
)

// fixture builds a graph.json with a plan node at the given lifecycle plus
// the given node and edge JSON fragments.
func fixture(lifecycle string, nodes, edges []string) string {
	plan := `{"id": "plan", "type": "plan", "status": "active", "fields": {"name": "demo", "kind": "task", "lifecycle": "` +
		lifecycle + `", "created": "2026-01-01"}}`
	return `{"version": 1, "nodes": [` + strings.Join(append([]string{plan}, nodes...), ",") +
		`], "edges": [` + strings.Join(edges, ",") + `]}`
}

const (
	goalA = `{"id": "g-k7q2", "type": "goal", "status": "active", "rank": "a0", "fields": {"title": "a goal"}}`
	acA   = `{"id": "ac-3fxm", "type": "ac", "status": "active", "rank": "a0", "fields": {"title": "an ac", "verify": {"cmd": "go test ./..."}}}`
	decA  = `{"id": "d-9t2w", "type": "decision", "status": "active", "rank": "a0", "fields": {"title": "a decision"}}`
	proof = `{"from": "ac-3fxm", "type": "proves", "to": "g-k7q2"}`
)

func TestRules(t *testing.T) {
	cases := []struct {
		name  string
		graph string
		want  []string // exact codes, in order
	}{
		{"clean requirements", fixture("requirements", []string{goalA, decA}, nil), nil},
		{"clean solution", fixture("solution", []string{goalA, acA, decA}, []string{
			proof, `{"from": "d-9t2w", "type": "constrains", "to": "g-k7q2"}`,
		}), nil},
		{"dangling-ref", fixture("requirements", []string{goalA, decA}, []string{
			`{"from": "d-9t2w", "type": "constrains", "to": "g-zzzz"}`,
		}), []string{"dangling-ref"}},
		{"bad-id", fixture("requirements", []string{
			`{"id": "G1", "type": "goal", "status": "active", "rank": "a0", "fields": {"title": "numbered"}}`,
		}, nil), []string{"bad-id"}},
		{"goal-no-ac", fixture("solution", []string{goalA}, nil), []string{"goal-no-ac"}},
		{"goal-no-ac ignores retired ACs", fixture("solution", []string{goalA,
			strings.Replace(acA, `"active"`, `"retired"`, 1)}, []string{proof}), []string{"goal-no-ac"}},
		{"goal-no-ac gated at requirements", fixture("requirements", []string{goalA}, nil), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, err := graph.Parse([]byte(tc.graph))
			if err != nil {
				t.Fatal(err)
			}
			r := Graph("001", g)
			var got []string
			for _, is := range r.Issues {
				got = append(got, is.Code)
				if is.Hint == "" || is.Message == "" || is.Path == "" {
					t.Errorf("issue lacks message/path/hint: %+v", is)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("codes = %v, want %v\n%+v", got, tc.want, r.Issues)
			}
			if r.OK != (len(tc.want) == 0) {
				t.Errorf("OK = %v with issues %v", r.OK, got)
			}
			if r.Issues == nil {
				t.Error("Issues must be [] not null")
			}
		})
	}
}

func TestGoalNoACHint(t *testing.T) {
	g, _ := graph.Parse([]byte(fixture("solution", []string{goalA}, nil)))
	is := Graph("004", g).Issues[0]
	if is.Hint != `auto plan add 004 ac --proves g-k7q2 --title "…"` || is.Path != "$.nodes[g-k7q2]" || is.Severity != SeverityError {
		t.Fatalf("issue = %+v", is)
	}
}

func TestFileParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.json")
	if err := os.WriteFile(path, []byte("{\"version\": 1,\n  nodes: []}"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := File("001", path)
	if r.OK || len(r.Issues) != 1 || r.Issues[0].Code != CodeParseError {
		t.Fatalf("report = %+v", r)
	}
	if !strings.Contains(r.Issues[0].Message, ":2:") {
		t.Errorf("parse error should carry line 2: %s", r.Issues[0].Message)
	}

	missing := File("001", filepath.Join(t.TempDir(), "graph.json"))
	if missing.OK || missing.Issues[0].Code != "unreadable" {
		t.Fatalf("missing file report = %+v", missing)
	}
}
