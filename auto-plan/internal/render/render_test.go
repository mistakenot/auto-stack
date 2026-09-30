package render

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mistakenot/auto-plan/internal/graph"
)

var update = flag.Bool("update", false, "rewrite golden files")

// ladderFixture has two goals (one with an AC and two decisions, one
// constraining the AC), a retired AC, an unlinked AC and an unlinked decision.
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
    {"id": "d-5n0b", "type": "decision", "status": "active", "rank": "a2", "fields": {"title": "undecided scope", "chosen": "c", "why": "w", "by": "charlie"}}
  ],
  "edges": [
    {"from": "ac-3fxm", "type": "proves", "to": "g-k7q2"},
    {"from": "ac-7w1e", "type": "proves", "to": "g-k7q2"},
    {"from": "d-9t2w", "type": "constrains", "to": "ac-3fxm"},
    {"from": "d-2hcv", "type": "constrains", "to": "g-m4t8"}
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
	got := fixtureView(t).Text()
	path := filepath.Join("testdata", "golden", "show.txt")
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
		t.Fatalf("show text mismatch (run with -update):\n--- got\n%s\n--- want\n%s", got, want)
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
	if len(v.Unlinked.ACs) != 1 || len(v.Unlinked.Decisions) != 1 {
		t.Fatalf("unlinked = %+v", v.Unlinked)
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
