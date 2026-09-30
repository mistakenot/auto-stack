package lint

import (
	"fmt"
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
	acA   = `{"id": "ac-3fxm", "type": "ac", "status": "active", "rank": "a0", "fields": {"title": "an ac", "gwt": "Given a, when b, then c", "verify": {"cmd": "go test ./..."}}}`
	decA  = `{"id": "d-9t2w", "type": "decision", "status": "active", "rank": "a0", "fields": {"title": "a decision", "chosen": "c", "why": "w", "by": "charlie"}}`
	altA  = `{"id": "a-5hcv", "type": "alternative", "status": "active", "rank": "a0", "fields": {"title": "an option", "why": "worse"}}`
	proof = `{"from": "ac-3fxm", "type": "proves", "to": "g-k7q2"}`
	rej   = `{"from": "d-9t2w", "type": "rejects", "to": "a-5hcv"}`

	questionOpen     = `{"id": "q-7mzk", "type": "question", "status": "active", "rank": "a0", "fields": {"title": "Which store?", "status": "open"}}`
	questionAnswered = `{"id": "q-7mzk", "type": "question", "status": "active", "rank": "a0", "fields": {"title": "Which store?", "status": "answered", "answer": "JSON"}}`
)

func retired(node string) string { return strings.Replace(node, `"active"`, `"retired"`, 1) }

func node(id, typ, fields string) string {
	return `{"id": "` + id + `", "type": "` + typ + `", "status": "active", "rank": "a0", "fields": {` + fields + `}}`
}

func edge(from, typ, to string) string {
	return `{"from": "` + from + `", "type": "` + typ + `", "to": "` + to + `"}`
}

func stage(id string) string {
	return node(id, "stage", `"title": "stage `+id+`", "steps": ["do it"], "commit": "feat: `+id+`"`)
}

func file(id, path string) string {
	return node(id, "file", `"path": "`+path+`", "change": "add", "why": "needed"`)
}

func treeNode(body string) string {
	return node("t-4nrd", "tree", `"title": "call tree", "kind": "call", "body": `+fmt.Sprintf("%q", body))
}

// goals returns n goals, each proven by its own verified AC, plus the proves
// edges: the goal ladder of a plan that passes every goal/AC rule.
func goals(n int) (nodes, edges []string) {
	for i := 1; i <= n; i++ {
		g, ac := fmt.Sprintf("g-aaa%d", i), fmt.Sprintf("ac-aaa%d", i)
		nodes = append(nodes,
			node(g, "goal", `"title": "goal `+g+`"`),
			node(ac, "ac", `"title": "ac `+ac+`", "gwt": "Given, when, then", "verify": {"cmd": "go test ./..."}`))
		edges = append(edges, edge(ac, "proves", g))
	}
	return nodes, edges
}

// cleanPlan is a plan that lints clean at lifecycle `plan`: five proven
// goals, a decision with a rejected alternative, an answered question, and two
// ordered stages that touch every file change. extra nodes and edges are added.
func cleanPlan(lifecycle string, extraNodes, extraEdges []string) string {
	nodes, edges := goals(5)
	nodes = append(nodes, decA, altA, questionAnswered, stage("s-aaa1"), stage("s-aaa2"), file("f-aaa1", "a.go"))
	edges = append(edges, rej, edge("d-9t2w", "constrains", "g-aaa1"),
		edge("s-aaa2", "dependsOn", "s-aaa1"), edge("s-aaa1", "touches", "f-aaa1"))
	return fixture(lifecycle, append(nodes, extraNodes...), append(edges, extraEdges...))
}

func TestRules(t *testing.T) {
	cases := []struct {
		name  string
		graph string
		want  []string // exact codes, in order
	}{
		// Lifecycle gating (AC-9).
		{"clean requirements: goals, questions and decisions", fixture("requirements",
			[]string{goalA, decA, altA, questionAnswered}, []string{rej}), nil},
		{"clean solution", cleanPlan("solution", nil, nil), nil},
		{"clean plan", cleanPlan("plan", nil, nil), nil},

		// Structural codes come from graph.Validate.
		{"dangling-ref", fixture("requirements", []string{goalA, decA, altA}, []string{
			rej, `{"from": "d-9t2w", "type": "constrains", "to": "g-zzzz"}`,
		}), []string{"dangling-ref"}},
		{"bad-id", fixture("requirements", []string{
			`{"id": "G1", "type": "goal", "status": "active", "rank": "a0", "fields": {"title": "numbered"}}`,
		}, nil), []string{"bad-id"}},
		{"duplicate-id", fixture("requirements", []string{goalA, goalA}, nil), []string{"duplicate-id"}},
		{"wrong-endpoint", fixture("requirements", []string{goalA, decA, altA}, []string{rej, edge("d-9t2w", "proves", "g-k7q2")}),
			[]string{"wrong-endpoint"}},

		// Core rules (AC-7).
		{"open-question at requirements", fixture("requirements", []string{questionOpen}, nil), []string{"open-question"}},
		{"open-question at plan", cleanPlan("plan", []string{strings.Replace(questionOpen, "q-7mzk", "q-7mzm", 1)}, nil),
			[]string{"open-question"}},
		{"ac-no-goal", fixture("requirements", []string{acA}, nil), []string{"ac-no-goal"}},
		{"ac-no-goal: its goal is retired", fixture("requirements", []string{retired(goalA), acA}, []string{proof}),
			[]string{"ac-no-goal", "retired-ref"}},
		{"ac-multi-goal", fixture("requirements", []string{goalA, strings.Replace(goalA, "g-k7q2", "g-k7q3", 1), acA},
			[]string{proof, edge("ac-3fxm", "proves", "g-k7q3")}), []string{"ac-multi-goal"}},
		{"goal-no-ac", cleanPlan("solution", []string{goalA}, nil), []string{"goal-no-ac"}},
		{"goal-no-ac ignores retired ACs", cleanPlan("solution", []string{goalA, retired(acA)}, []string{proof}),
			[]string{"goal-no-ac"}},
		{"goal-no-ac gated at requirements", fixture("requirements", []string{goalA}, nil), nil},
		{"ac-no-verify", cleanPlan("solution", []string{goalA,
			strings.Replace(acA, `"verify": {"cmd": "go test ./..."}`, `"verify": {"tests": ["TestA"]}`, 1)},
			[]string{proof}), []string{"ac-no-verify"}},
		{"ac-no-verify: manual needs no command", cleanPlan("plan", []string{
			node("ac-aaa6", "ac", `"title": "looks right", "gwt": "Given, when, then", "verify": {"kind": "manual"}`),
		}, []string{edge("ac-aaa6", "proves", "g-aaa1")}), nil},
		{"ac-no-verify gated at requirements", fixture("requirements",
			[]string{goalA, strings.Replace(acA, `, "verify": {"cmd": "go test ./..."}`, "", 1)}, []string{proof}), nil},
		{"goal-count below 5", fixture("solution", []string{goalA, acA}, []string{proof}), []string{"goal-count"}},
		{"goal-count above 8", func() string {
			n, e := goals(9)
			return fixture("solution", n, e)
		}(), []string{"goal-count"}},
		{"goal-count gated at requirements", fixture("requirements", []string{goalA}, nil), nil},
		{"decision-no-alternative", fixture("requirements", []string{goalA, decA}, nil), []string{"decision-no-alternative"}},
		{"decision-no-alternative: its alternative is retired", fixture("requirements", []string{decA, retired(altA)}, []string{rej}),
			[]string{"decision-no-alternative", "retired-ref"}},
		{"retired-ref", fixture("requirements", []string{retired(goalA), decA, altA},
			[]string{rej, edge("d-9t2w", "constrains", "g-k7q2")}), []string{"retired-ref"}},
		{"retired-ref ignores edges from retired nodes", fixture("requirements", []string{retired(goalA), retired(decA)},
			[]string{edge("d-9t2w", "constrains", "g-k7q2")}), nil},

		// Cycles.
		{"dependency-cycle", cleanPlan("plan", []string{stage("s-aaa3")}, []string{
			edge("s-aaa1", "dependsOn", "s-aaa3"), edge("s-aaa3", "dependsOn", "s-aaa2"), edge("s-aaa3", "touches", "f-aaa1"),
		}), []string{"dependency-cycle"}},
		{"dependency-cycle: self-loop", cleanPlan("plan", nil, []string{edge("s-aaa1", "dependsOn", "s-aaa1")}),
			[]string{"dependency-cycle"}},
		{"dependency-cycle applies at requirements", fixture("requirements", []string{stage("s-aaa1"), stage("s-aaa2")},
			[]string{edge("s-aaa1", "dependsOn", "s-aaa2"), edge("s-aaa2", "dependsOn", "s-aaa1")}), []string{"dependency-cycle"}},
		{"dependency-cycle: child plans", fixture("requirements", []string{
			node("c-aaa1", "child", `"plan": "002"`), node("c-aaa2", "child", `"plan": "003"`),
		}, []string{edge("c-aaa1", "dependsOn", "c-aaa2"), edge("c-aaa2", "dependsOn", "c-aaa1")}), []string{"dependency-cycle"}},

		// Prose references (AC-8).
		{"dangling-prose-ref", fixture("requirements", []string{
			node("g-k7q2", "goal", `"title": "g", "description": "see [[ac-zz9q]] and [[g-k7q2]]"`),
		}, nil), []string{"dangling-prose-ref"}},
		{"dangling-prose-ref: not an ID", fixture("requirements", []string{
			node("g-k7q2", "goal", `"title": "g", "description": "see [[AC-3]]"`),
		}, nil), []string{"dangling-prose-ref"}},
		{"dangling-prose-ref: qualified refs are shape-checked only", fixture("requirements", []string{
			node("g-k7q2", "goal", `"title": "g", "description": "honours [[005:r-8hw3]] and [[005:plan]]"`),
		}, nil), nil},
		{"dangling-prose-ref: bad qualified shape", fixture("requirements", []string{
			node("g-k7q2", "goal", `"title": "g", "description": "honours [[5:r-8hw3]]"`),
		}, nil), []string{"dangling-prose-ref"}},
		{"retired-ref: prose", fixture("requirements", []string{retired(goalA),
			strings.Replace(decA, `"why": "w"`, `"why": "because [[g-k7q2]]"`, 1), altA}, []string{rej}),
			[]string{"retired-ref"}},

		// Trees (AC-8).
		{"tree-syntax clean", fixture("requirements", []string{treeNode("+ runLint  new\n~   lint.File\n      graph.Decode\n- oldLint")}, nil), nil},
		{"tree-syntax", fixture("requirements", []string{treeNode("* runLint\n  lint.File")}, nil), []string{"tree-syntax"}},

		// pd-lint parity (AC-10).
		{"unplanned-file", cleanPlan("plan", []string{file("f-aaa2", "b.go")}, nil), []string{"unplanned-file"}},
		{"unplanned-file gated at solution", cleanPlan("solution", []string{file("f-aaa2", "b.go")}, nil), nil},
		{"unplanned-file: touched only by a retired stage", cleanPlan("plan", []string{file("f-aaa2", "b.go"), retired(stage("s-aaa3"))},
			[]string{edge("s-aaa3", "touches", "f-aaa2")}), []string{"unplanned-file"}},
		{"untracked-file", cleanPlan("plan", []string{retired(file("f-aaa2", "b.go"))}, []string{edge("s-aaa1", "touches", "f-aaa2")}),
			[]string{"untracked-file"}},
		{"untracked-file: a missing node is a dangling-ref", cleanPlan("plan", nil, []string{edge("s-aaa1", "touches", "f-zzzz")}),
			[]string{"dangling-ref"}},
		{"untracked-file below plan is a retired-ref", cleanPlan("solution", []string{retired(file("f-aaa2", "b.go"))},
			[]string{edge("s-aaa1", "touches", "f-aaa2")}), []string{"retired-ref"}},
		{"missing-dep", cleanPlan("plan", []string{retired(stage("s-aaa3"))}, []string{edge("s-aaa1", "dependsOn", "s-aaa3")}),
			[]string{"missing-dep"}},
		{"missing-dep: a missing node is a dangling-ref", cleanPlan("plan", nil, []string{edge("s-aaa1", "dependsOn", "s-zzzz")}),
			[]string{"dangling-ref"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, err := graph.Parse([]byte(tc.graph))
			if err != nil {
				t.Fatal(err)
			}
			r := Graph("001", g)
			var got []string
			errs := 0
			for _, is := range r.Issues {
				got = append(got, is.Code)
				if is.Hint == "" || is.Message == "" || is.Path == "" {
					t.Errorf("issue lacks message/path/hint: %+v", is)
				}
				if is.Severity == SeverityError {
					errs++
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("codes = %v, want %v\n%+v", got, tc.want, r.Issues)
			}
			if r.OK != (errs == 0) {
				t.Errorf("OK = %v with issues %v", r.OK, got)
			}
			if r.Issues == nil {
				t.Error("Issues must be [] not null")
			}
		})
	}
}

// TestSeverities pins every rule's code and severity (AC-7).
func TestSeverities(t *testing.T) {
	want := map[string]Severity{
		"open-question": SeverityError, "ac-no-goal": SeverityError, "ac-multi-goal": SeverityError,
		"dangling-prose-ref": SeverityError, "tree-syntax": SeverityError, "dependency-cycle": SeverityError,
		"goal-no-ac": SeverityError, "ac-no-verify": SeverityError, "unplanned-file": SeverityError,
		"untracked-file": SeverityError, "missing-dep": SeverityError,
		"goal-count": SeverityWarning, "decision-no-alternative": SeverityWarning, "retired-ref": SeverityWarning,
	}
	if len(Rules) != len(want) {
		t.Fatalf("%d rules, want %d", len(Rules), len(want))
	}
	for _, r := range Rules {
		if want[r.Code] != r.Severity {
			t.Errorf("%s severity = %s, want %s", r.Code, r.Severity, want[r.Code])
		}
	}
	// Validation codes surface as errors.
	g, _ := graph.Parse([]byte(fixture("requirements", []string{goalA, goalA, decA, altA},
		[]string{rej, edge("d-9t2w", "proves", "g-k7q2")})))
	for _, is := range Graph("001", g).Issues {
		if is.Severity != SeverityError {
			t.Errorf("%s severity = %s", is.Code, is.Severity)
		}
	}
}

// TestLifecycleGating: the same requirements-only graph gains goal-no-ac and
// ac-no-verify once the plan reaches solution (AC-9).
func TestLifecycleGating(t *testing.T) {
	noVerify := strings.Replace(acA, `, "verify": {"cmd": "go test ./..."}`, "", 1)
	body := func(l string) string {
		return fixture(l, []string{goalA, strings.Replace(goalA, "g-k7q2", "g-m4t8", 1), noVerify, decA, altA, questionAnswered},
			[]string{proof, rej})
	}
	codes := func(l string) []string {
		g, err := graph.Parse([]byte(body(l)))
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, is := range Graph("001", g).Issues {
			if is.Severity == SeverityError {
				out = append(out, is.Code)
			}
		}
		return out
	}
	if got := codes("requirements"); len(got) != 0 {
		t.Errorf("requirements errors = %v, want none", got)
	}
	if got, want := codes("solution"), []string{"goal-no-ac", "ac-no-verify"}; !slices.Equal(got, want) {
		t.Errorf("solution errors = %v, want %v", got, want)
	}
}

func issuesOf(t *testing.T, planID, doc string) []Issue {
	t.Helper()
	g, err := graph.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return Graph(planID, g).Issues
}

// TestMessages pins the wording and hints, including pd-lint's messages for
// the parity codes.
func TestMessages(t *testing.T) {
	cases := []struct {
		name, doc string
		want      Issue
	}{
		{"goal-no-ac", fixture("solution", []string{goalA}, nil), Issue{
			Code: "goal-no-ac", Severity: SeverityError, Path: "$.nodes[g-k7q2]",
			Message: "g-k7q2 has no active AC", Hint: `auto plan add 004 ac --proves g-k7q2 --title "…"`,
		}},
		{"open-question", fixture("requirements", []string{questionOpen}, nil), Issue{
			Code: "open-question", Severity: SeverityError, Path: "$.nodes[q-7mzk].fields.status", Field: "status",
			Message: `Question q-7mzk "Which store?" is awaiting a human answer.`,
			Hint:    `answer it: auto plan update 004 q-7mzk --status answered --answer "…"`,
		}},
		{"unplanned-file", cleanPlan("plan", []string{file("f-aaa2", "b.go")}, nil), Issue{
			Code: "unplanned-file", Severity: SeverityError, Path: "$.nodes[f-aaa2]",
			Message: "File b.go (f-aaa2) is in the file tree but no stage touches it.",
			Hint:    "auto plan link 004 <stage-id> touches f-aaa2, or auto plan retire 004 f-aaa2",
		}},
		{"untracked-file", cleanPlan("plan", []string{retired(file("f-aaa2", "b.go"))}, []string{edge("s-aaa1", "touches", "f-aaa2")}), Issue{
			Code: "untracked-file", Severity: SeverityError, Path: "$.edges[s-aaa1 touches f-aaa2]",
			Message: "Stage s-aaa1 touches b.go (f-aaa2), which is missing from the file tree: the file change is retired.",
			Hint:    "auto plan unlink 004 s-aaa1 touches f-aaa2, and add the file again with auto plan add 004 file if the stage still changes it",
		}},
		{"missing-dep", cleanPlan("plan", []string{retired(stage("s-aaa3"))}, []string{edge("s-aaa1", "dependsOn", "s-aaa3")}), Issue{
			Code: "missing-dep", Severity: SeverityError, Path: "$.edges[s-aaa1 dependsOn s-aaa3]",
			Message: "Stage s-aaa1 depends on stage s-aaa3, which doesn't exist: it is retired.",
			Hint:    "auto plan unlink 004 s-aaa1 dependsOn s-aaa3",
		}},
		{"dependency-cycle", cleanPlan("plan", []string{stage("s-aaa3")}, []string{
			edge("s-aaa1", "dependsOn", "s-aaa3"), edge("s-aaa3", "dependsOn", "s-aaa2"), edge("s-aaa3", "touches", "f-aaa1"),
		}), Issue{
			Code: "dependency-cycle", Severity: SeverityError, Path: "$.nodes[s-aaa1]",
			Message: "Dependency cycle among stages: s-aaa1 → s-aaa3 → s-aaa2 → s-aaa1.",
			Hint:    "break it: auto plan unlink 004 s-aaa1 dependsOn s-aaa3",
		}},
		{"dangling-prose-ref", fixture("requirements", []string{altA,
			strings.Replace(decA, `"why": "w"`, `"why": "unlike [[ac-zz9q]]"`, 1)}, []string{rej}), Issue{
			Code: "dangling-prose-ref", Severity: SeverityError, Path: "$.nodes[d-9t2w].fields.why", Field: "why",
			Message: "d-9t2w's why refers to [[ac-zz9q]], which is not a node in this plan",
			Hint:    "fix the reference with auto plan update 004 d-9t2w --why …; see the plan's node IDs with auto plan show 004",
		}},
		{"tree-syntax", fixture("requirements", []string{treeNode("+ a\n  b\n      c")}, nil), Issue{
			Code: "tree-syntax", Severity: SeverityError, Path: "$.nodes[t-4nrd].fields.body", Field: "body",
			Message: `t-4nrd body line 3: "c" is at depth 2 but the line before it is at depth 0, so it has no parent`,
			Hint:    "fix the show-me notation and rewrite it with auto plan update 004 t-4nrd --body @tree.txt",
		}},
		{"missing-field hint names the update flag", fixture("requirements", []string{
			`{"id": "ac-3fxm", "type": "ac", "status": "active", "rank": "a0", "fields": {"title": "an ac", "gwt": "g", "verify": {"kind": "sometimes"}}}`,
			goalA,
		}, []string{proof}), Issue{
			Code: "invalid-field", Severity: SeverityError, Path: "$.nodes[ac-3fxm].fields.verify.kind", Field: "kind",
			Message: "kind must be one of command|manual", Hint: "set it with `auto plan update 004 ac-3fxm --verify-kind …`",
		}},
		{"dangling-ref hint names the unlink", fixture("requirements", []string{goalA, decA, altA},
			[]string{rej, edge("d-9t2w", "constrains", "g-zzzz")}), Issue{
			Code: "dangling-ref", Severity: SeverityError, Path: "$.edges[d-9t2w constrains g-zzzz].to", Field: "to",
			Message: `edge targets "g-zzzz", which is not a node in this plan`,
			Hint:    "remove the edge with `auto plan unlink 004 d-9t2w constrains g-zzzz`, or point it at an existing node of an allowed type",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, is := range issuesOf(t, "004", tc.doc) {
				if is.Code == tc.want.Code {
					if is != tc.want {
						t.Fatalf("issue =\n  %+v\nwant\n  %+v", is, tc.want)
					}
					return
				}
			}
			t.Fatalf("no %s issue", tc.want.Code)
		})
	}
}

// TestCyclesDeterministic: a planted 3-cycle reports the same path on every
// run, whatever the edge order in the file.
func TestCyclesDeterministic(t *testing.T) {
	edges := []string{
		edge("s-ccc3", "dependsOn", "s-ccc1"), edge("s-ccc1", "dependsOn", "s-ccc2"), edge("s-ccc2", "dependsOn", "s-ccc3"),
		edge("s-ccc4", "dependsOn", "s-ccc1"),
	}
	const want = "Dependency cycle among stages: s-ccc1 → s-ccc2 → s-ccc3 → s-ccc1."
	for i := range 20 {
		rot := append(slices.Clone(edges[i%len(edges):]), edges[:i%len(edges)]...)
		if i%2 == 1 {
			slices.Reverse(rot)
		}
		doc := fixture("requirements", []string{stage("s-ccc4"), stage("s-ccc2"), stage("s-ccc3"), stage("s-ccc1")}, rot)
		var got []string
		for _, is := range issuesOf(t, "001", doc) {
			got = append(got, is.Message)
		}
		if !slices.Equal(got, []string{want}) {
			t.Fatalf("run %d: %v", i, got)
		}
	}
}

func TestCycles(t *testing.T) {
	adj := map[string][]string{
		"a": {"b"}, "b": {"a", "c"}, "c": {"d"}, "d": {"c"}, "e": {"e"}, "f": {"a"}, "x": {"zz"},
	}
	got := Cycles([]string{"f", "e", "d", "c", "b", "a", "x"}, adj)
	want := [][]string{{"a", "b", "a"}, {"c", "d", "c"}, {"e", "e"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("Cycles = %v, want %v", got, want)
	}
	if got := Cycles(nil, nil); got != nil {
		t.Fatalf("Cycles(nil) = %v", got)
	}
	// The shortest route back: a → b → a, not a → c → d → a.
	got = Cycles([]string{"a", "b", "c", "d"}, map[string][]string{"a": {"c", "b"}, "b": {"a"}, "c": {"d"}, "d": {"a"}})
	if fmt.Sprint(got) != "[[a b a]]" {
		t.Fatalf("shortest cycle = %v", got)
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
