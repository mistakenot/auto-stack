package lint

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
	"github.com/mistakenot/auto-plan/internal/workspace"
)

// fixture builds a graph.json with a plan node at the given lifecycle plus
// the given node and edge JSON fragments.
func fixture(lifecycle string, nodes, edges []string) string {
	plan := `{"id": "plan", "type": "plan", "status": "active", "fields": {"name": "demo", "kind": "task", "lifecycle": "` +
		lifecycle + `", "created": "2026-01-01"}}`
	return `{"version": "1.0.0", "id": "001-k7q2", "nodes": [` + strings.Join(append([]string{plan}, nodes...), ",") +
		`], "edges": [` + strings.Join(edges, ",") + `]}`
}

const (
	goalA = `{"id": "g-k7q2", "type": "goal", "status": "active", "rank": "a0", "fields": {"title": "a goal"}}`
	acA   = `{"id": "ac-3fxm", "type": "ac", "status": "active", "rank": "a0", "fields": {"title": "an ac", "gwt": "Given a, when b, then c", "verify": {"cmd": "go test ./..."}}}`
	decA  = `{"id": "d-9t2w", "type": "decision", "status": "active", "rank": "a0", "fields": {"title": "a decision", "chosen": "c", "why": "w", "by": "charlie"}}`
	altA  = `{"id": "a-5hcv", "type": "alternative", "status": "active", "rank": "a0", "fields": {"title": "an option", "why": "worse"}}`
	proof = `{"id": "e-0001", "from": "ac-3fxm", "type": "proves", "to": "g-k7q2"}`
	rej   = `{"id": "e-0002", "from": "d-9t2w", "type": "rejects", "to": "a-5hcv"}`

	questionOpen     = `{"id": "q-7mzk", "type": "question", "status": "active", "rank": "a0", "fields": {"title": "Which store?", "status": "open"}}`
	questionAnswered = `{"id": "q-7mzk", "type": "question", "status": "active", "rank": "a0", "fields": {"title": "Which store?", "status": "answered", "answer": "JSON"}}`
)

func retired(node string) string { return strings.Replace(node, `"active"`, `"retired"`, 1) }

func node(id, typ, fields string) string {
	return `{"id": "` + id + `", "type": "` + typ + `", "status": "active", "rank": "a0", "fields": {` + fields + `}}`
}

// edge returns an edge with an ID derived from its endpoints, so fixtures
// stay readable and IDs stay unique per plan.
func edge(from, typ, to string) string {
	return `{"id": "` + edgeID(from, typ, to) + `", "from": "` + from + `", "type": "` + typ + `", "to": "` + to + `"}`
}

func edgeID(from, typ, to string) string {
	const crockford = "0123456789abcdefghjkmnpqrstvwxyz"
	h := fnv.New32a()
	_, _ = h.Write([]byte(from + " " + typ + " " + to))
	n := h.Sum32()
	id := []byte("e-")
	for range 4 {
		id = append(id, crockford[n%32])
		n /= 32
	}
	return string(id)
}

// planIDs are the fixture plans' IDs, by folder number.
var planIDs = map[string]string{"001": "001-aaaa", "002": "002-bbbb", "003": "003-cccc", "009": "009-zzzz"}

// pid is the plan ID of fixture plan number n.
func pid(n string) string { return planIDs[n] }

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
			rej, `{"id": "e-0009", "from": "d-9t2w", "type": "constrains", "to": "g-zzzz"}`,
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
		{"an AC may prove several goals", fixture("requirements", []string{goalA, strings.Replace(goalA, "g-k7q2", "g-k7q3", 1), acA},
			[]string{proof, edge("ac-3fxm", "proves", "g-k7q3")}), nil},
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
			node("c-aaa1", "child", `"plan": "002-bbbb"`), node("c-aaa2", "child", `"plan": "003-cccc"`),
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
		{"dangling-prose-ref: quoted examples are not references", fixture("requirements", []string{
			node("g-k7q2", "goal", `"title": "g", "description": "a field containing `+"`[[ac-zz9q]]`"+` fails:\n\n`+"```"+`\n[[ac-zz9q]]\n`+"```"+`"`),
		}, nil), nil},
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
		"plan-id-mismatch": SeverityError, "ambiguous-ref": SeverityError,
		"open-question": SeverityError, "ac-no-goal": SeverityError,
		"dangling-prose-ref": SeverityError, "tree-syntax": SeverityError, "dependency-cycle": SeverityError,
		"goal-no-ac": SeverityError, "ac-no-verify": SeverityError, "unplanned-file": SeverityError,
		"untracked-file": SeverityError, "missing-dep": SeverityError,
		"annex-missing": SeverityError, "unlinked-file": SeverityError, "annex-ref": SeverityError,
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

// planDoc builds a graph.json whose plan node has the given kind, lifecycle
// and epic ("" for none).
func planDoc(name, kind, lifecycle, epic string, nodes, edges []string) string {
	epicField := ""
	if epic != "" {
		epicField = `, "epic": "` + pid(epic) + `"`
	}
	plan := `{"id": "plan", "type": "plan", "status": "active", "fields": {"name": "` + name + `", "kind": "` + kind +
		`", "lifecycle": "` + lifecycle + `", "created": "2026-01-01"` + epicField + `}}`
	return `{"version": "1.0.0", "id": "@PLAN@", "nodes": [` + strings.Join(append([]string{plan}, nodes...), ",") +
		`], "edges": [` + strings.Join(edges, ",") + `]}`
}

// writeSet writes each folder's graph.json under a temp .auto/plan/plans and
// returns the workspace's PlanSet. A doc's "@PLAN@" ID becomes the plan ID of
// its folder's number.
func writeSet(t *testing.T, folders map[string]string) *workspace.PlanSet {
	t.Helper()
	root := t.TempDir()
	for folder, doc := range folders {
		dir := filepath.Join(root, ".auto", "plan", "plans", folder)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		doc = strings.Replace(doc, `"@PLAN@"`, `"`+pid(folder[:3])+`"`, 1)
		if err := os.WriteFile(filepath.Join(dir, "graph.json"), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	set, err := (&workspace.Workspace{Root: root, CWD: root}).PlanSet()
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// Epic family fixture: epic 001 with a goal, a decision, a rail, a journey
// with one leg, and a child node for 002; child 002 honours the rail,
// delivers the leg, builds on the decision, and has an AC that discharges the
// rail.
var (
	epicGoal  = node("g-e001", "goal", `"title": "agents exchange mail"`)
	epicDec   = node("d-e001", "decision", `"title": "SQLite", "chosen": "SQLite", "why": "durable", "by": "charlie"`)
	epicAlt   = node("a-e001", "alternative", `"title": "files", "why": "no transactions"`)
	epicRail  = node("r-e001", "rail", `"title": "no network calls"`)
	epicJour  = node("j-e001", "journey", `"title": "send and ack"`)
	epicLeg   = node("l-e001", "leg", `"actor": "agent", "action": "sends mail"`)
	epicChild = node("c-e001", "child", `"plan": "002-bbbb", "title": "walking skeleton"`)

	childGoal = node("g-t001", "goal", `"title": "round trip"`)
	childAC   = node("ac-t001", "ac", `"title": "read back", "gwt": "Given, when, then", "verify": {"cmd": "go test ./..."}`)
)

func epicDoc(lifecycle string, extraNodes, extraEdges []string) string {
	return planDoc("mail-mvp", "epic", lifecycle, "",
		append([]string{epicGoal, epicDec, epicAlt, epicRail, epicJour, epicLeg, epicChild}, extraNodes...),
		append([]string{edge("d-e001", "rejects", "a-e001"), edge("d-e001", "constrains", "g-e001"), edge("l-e001", "in", "j-e001")}, extraEdges...))
}

// epicUnlisted is the clean epic without its child node for 002.
func epicUnlisted(lifecycle string) string {
	return strings.Replace(epicDoc(lifecycle, nil, nil), ","+epicChild, "", 1)
}

// withProse gives the child's goal a description.
func withProse(doc, description string) string {
	return strings.Replace(doc, `"title": "round trip"`, `"title": "round trip", "description": "`+description+`"`, 1)
}

// childEdges are the clean child's edges; drop removes the ones it names.
func childEdges(drop ...string) []string {
	all := map[string]string{
		"honors":     edge("plan", "honors", "001-aaaa:r-e001"),
		"delivers":   edge("plan", "delivers", "001-aaaa:l-e001"),
		"builds-on":  edge("plan", "builds-on", "001-aaaa:d-e001"),
		"proves":     edge("ac-t001", "proves", "g-t001"),
		"discharges": edge("ac-t001", "discharges", "001-aaaa:r-e001"),
	}
	var out []string
	for _, k := range []string{"honors", "delivers", "builds-on", "proves", "discharges"} {
		if !slices.Contains(drop, k) {
			out = append(out, all[k])
		}
	}
	return out
}

func childDoc(lifecycle, epic string, extraNodes, edges []string) string {
	return planDoc("walking-skeleton", "task", lifecycle, epic, append([]string{childGoal, childAC}, extraNodes...), edges)
}

// family is a clean epic family at lifecycle plan, with overrides.
func family(epic, child string) map[string]string {
	if epic == "" {
		epic = epicDoc("plan", nil, nil)
	}
	if child == "" {
		child = childDoc("plan", "001", nil, childEdges())
	}
	return map[string]string{"001-mail-mvp": epic, "002-walking-skeleton": child}
}

// twoChildren is a clean epic family at lifecycle plan with a second child,
// 003, whose edges are given; deferred, when set, is the rail's deferred list.
func twoChildren(deferred []string, edges003 []string) map[string]string {
	epic := epicDoc("plan", []string{node("c-e002", "child", `"plan": "003-cccc", "title": "fan-out"`)}, nil)
	if deferred != nil {
		ids := make([]string, len(deferred))
		for i, d := range deferred {
			ids[i] = pid(d)
		}
		epic = strings.Replace(epic, `"no network calls"`, `"no network calls", "deferred": ["`+strings.Join(ids, `", "`)+`"]`, 1)
	}
	folders := family(epic, "")
	folders["003-fan-out"] = planDoc("fan-out", "task", "plan", "001", []string{childGoal, childAC}, edges003)
	return folders
}

// lintNumber lints the (first) plan with folder number n within set.
func lintNumber(set *workspace.PlanSet, n string) Report {
	return Plan(set, set.ByNumber(n)[0])
}

// errorCodes lints planID within set and returns its error codes (and the
// warning codes named in keep), checking every issue carries a message,
// path and hint.
func errorCodes(t *testing.T, set *workspace.PlanSet, planID string, keep ...string) []string {
	t.Helper()
	var out []string
	for _, is := range lintNumber(set, planID).Issues {
		if is.Hint == "" || is.Message == "" || is.Path == "" {
			t.Errorf("issue lacks message/path/hint: %+v", is)
		}
		if is.Severity == SeverityError || slices.Contains(keep, is.Code) {
			out = append(out, is.Code)
		}
	}
	return out
}

// TestEpicRules: a two-plan fixture per cross-plan code, plus a clean epic
// family (AC-11).
func TestEpicRules(t *testing.T) {
	supersede := []string{node("d-e002", "decision", `"title": "SQLite in WAL mode", "chosen": "WAL", "why": "readers", "by": "charlie"`)}
	supersedeEdges := []string{edge("d-e002", "supersedes", "d-e001"), edge("d-e002", "rejects", "a-e001")}
	cases := []struct {
		name    string
		folders map[string]string
		lint    string
		keep    []string // warning codes to report too
		want    []string
	}{
		{"clean family: epic", family("", ""), "001", nil, nil},
		{"clean family: child", family("", ""), "002", nil, nil},
		{"clean requirements-only epic: rails and legs, no children yet", map[string]string{
			"001-mail-mvp": planDoc("mail-mvp", "epic", "requirements", "", []string{epicGoal, epicRail, epicJour, epicLeg},
				[]string{edge("l-e001", "in", "j-e001")}),
		}, "001", nil, nil},
		{"epic goals need no ACs at solution", map[string]string{
			"001-mail-mvp": planDoc("mail-mvp", "epic", "solution", "", []string{epicGoal}, nil),
		}, "001", nil, nil},

		{"rail-unhonored", family("", childDoc("plan", "001", nil, childEdges("honors"))), "001", nil, []string{"rail-unhonored"}},
		{"rail-unhonored gated below plan", family(epicDoc("solution", nil, nil), childDoc("plan", "001", nil, childEdges("honors"))), "001", nil, nil},
		{"rail-unhonored: deferred", family(
			strings.Replace(epicDoc("plan", nil, nil), `"no network calls"`, `"no network calls", "deferred": ["002-bbbb"]`, 1),
			childDoc("plan", "001", nil, childEdges("honors"))), "001", nil, nil},

		{"rail-unhonored: one honouring child does not cover another", twoChildren(nil, childEdges("honors")), "001", nil,
			[]string{"rail-unhonored"}},
		{"rail-unhonored: deferred excuses the named child", twoChildren([]string{"003"}, childEdges("honors")), "001", nil, nil},
		{"rail-unhonored: deferred excuses only the named child", twoChildren([]string{"002"}, childEdges("honors")), "001", nil,
			[]string{"rail-unhonored"}},
		{"rail-unhonored: every child honours", twoChildren(nil, childEdges()), "001", nil, nil},

		{"rail-undischarged", family("", childDoc("solution", "001", nil, childEdges("discharges"))), "002", nil, []string{"rail-undischarged"}},
		{"rail-undischarged: a retired AC does not count", family("", childDoc("solution", "001",
			[]string{retired(strings.Replace(childAC, "ac-t001", "ac-t002", 1))},
			append(childEdges("discharges"), edge("ac-t002", "discharges", "001-aaaa:r-e001")))), "002", nil, []string{"rail-undischarged"}},
		{"rail-undischarged gated at requirements", family("", childDoc("requirements", "001", nil, childEdges("discharges"))), "002", nil, nil},

		{"leg-undelivered", family("", childDoc("plan", "001", nil, childEdges("delivers"))), "001", nil, []string{"leg-undelivered"}},

		{"child-missing", family(epicDoc("requirements", []string{node("c-e002", "child", `"plan": "009-zzzz"`)}, nil), ""), "001", nil,
			[]string{"child-missing"}},

		{"child-epic-mismatch: the epic does not list the child", family(epicUnlisted("requirements"), ""), "002", nil,
			[]string{"child-epic-mismatch"}},
		{"child-epic-mismatch: the named epic is a task", map[string]string{
			"001-mail-mvp":         planDoc("mail-mvp", "task", "requirements", "", []string{epicChild}, nil),
			"002-walking-skeleton": childDoc("requirements", "001", nil, childEdges("honors", "delivers", "builds-on", "discharges")),
		}, "002", nil, []string{"child-epic-mismatch"}},
		{"child-epic-mismatch: the named epic does not exist", map[string]string{
			"002-walking-skeleton": childDoc("requirements", "009", nil, childEdges("honors", "delivers", "builds-on", "discharges")),
		}, "002", nil, []string{"child-epic-mismatch"}},
		{"child-epic-mismatch: reverse, the child names no epic", family("", childDoc("plan", "", nil, childEdges())), "001", nil,
			[]string{"child-epic-mismatch"}},

		{"superseded-ref: builds-on", family(epicDoc("plan", supersede, supersedeEdges), ""), "002", nil, []string{"superseded-ref"}},
		{"superseded-ref: prose", family(epicDoc("plan", supersede, supersedeEdges),
			withProse(childDoc("plan", "001", nil, childEdges("builds-on")), "per [[001:d-e001]]")),
			"002", nil, []string{"superseded-ref"}},
		{"superseded-ref: a retired superseder does not count", family(epicDoc("plan",
			[]string{retired(supersede[0])}, supersedeEdges), ""), "002", nil, nil},

		{"child dependency-cycle", map[string]string{
			"001-mail-mvp": planDoc("mail-mvp", "epic", "requirements", "", []string{epicChild, node("c-e002", "child", `"plan": "003-cccc"`)},
				[]string{edge("c-e001", "dependsOn", "c-e002"), edge("c-e002", "dependsOn", "c-e001")}),
			"002-walking-skeleton": childDoc("requirements", "001", nil, childEdges("honors", "delivers", "builds-on", "discharges")),
			"003-fan-out":          planDoc("fan-out", "task", "requirements", "001", nil, nil),
		}, "001", nil, []string{"dependency-cycle"}},

		// Qualified references resolve against the set.
		{"qualified dangling-ref: no such node", family("", childDoc("plan", "001", nil,
			append(childEdges(), edge("plan", "honors", "001-aaaa:r-zzzz")))), "002", nil, []string{"dangling-ref"}},
		{"qualified dangling-ref: no such plan", family("", childDoc("plan", "001", nil,
			append(childEdges(), edge("plan", "honors", "009-zzzz:r-e001")))), "002", nil, []string{"dangling-ref"}},
		{"qualified dangling-ref: malformed", family("", childDoc("plan", "001", nil,
			append(childEdges(), edge("plan", "honors", "1:r-e001")))), "002", nil, []string{"dangling-ref"}},
		{"qualified wrong-endpoint", family("", childDoc("plan", "001", nil,
			append(childEdges(), edge("plan", "honors", "001-aaaa:g-e001")))), "002", nil, []string{"wrong-endpoint"}},
		{"qualified dangling-prose-ref", family("",
			withProse(childDoc("plan", "001", nil, childEdges()), "per [[001:d-zzzz]], [[009:plan]] and [[001:d-e001]]")),
			"002", nil, []string{"dangling-prose-ref", "dangling-prose-ref"}},
		{"qualified retired-ref", family(strings.Replace(epicDoc("plan", nil, nil), `"id": "l-e001", "type": "leg", "status": "active"`,
			`"id": "l-e001", "type": "leg", "status": "retired"`, 1), ""), "002", []string{"retired-ref"}, []string{"retired-ref"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set := writeSet(t, tc.folders)
			if got := errorCodes(t, set, tc.lint, tc.keep...); !slices.Equal(got, tc.want) {
				t.Fatalf("codes = %v, want %v\n%+v", got, tc.want, lintNumber(set, tc.lint).Issues)
			}
		})
	}
}

// TestEpicRulesNeedTheSet: linted alone, a plan's qualified references are
// shape-checked only and no epic rule runs.
func TestEpicRulesNeedTheSet(t *testing.T) {
	doc := strings.Replace(childDoc("solution", "009", nil, append(childEdges("discharges"), edge("plan", "honors", "001-aaaa:r-zzzz"))),
		`"@PLAN@"`, `"002-bbbb"`, 1)
	for _, is := range issuesOf(t, "002", doc) {
		if is.Severity == SeverityError {
			t.Fatalf("lint alone must not resolve qualified refs or run epic rules: %+v", is)
		}
	}
	if len(EpicRules) != 6 {
		t.Fatalf("%d epic rules", len(EpicRules))
	}
	for _, r := range EpicRules {
		if r.Severity != SeverityError {
			t.Errorf("%s severity = %s", r.Code, r.Severity)
		}
	}
}

// TestEpicMessages pins the wording and hints of the cross-plan codes.
func TestEpicMessages(t *testing.T) {
	set := writeSet(t, family("", childDoc("solution", "001", nil, childEdges("honors", "delivers", "discharges"))))
	got := map[string]Issue{}
	for _, is := range lintNumber(set, "001").Issues {
		got[is.Code] = is
	}
	if is := got["rail-unhonored"]; is.Path != "$.nodes[r-e001]" ||
		is.Message != `Rail r-e001 "no network calls" is not honoured by child plan 002-bbbb, which is not deferred.` ||
		is.Hint != "auto plan link 002-bbbb plan honors 001-aaaa:r-e001, or excuse the child for now with auto plan update 001-aaaa r-e001 --deferred <child-plan>" {
		t.Errorf("rail-unhonored = %+v", is)
	}
	if is := got["leg-undelivered"]; is.Message != `Leg l-e001 "agent: sends mail" is delivered by no child plan (children: 002-bbbb).` ||
		is.Hint != "auto plan link <child> plan delivers 001-aaaa:l-e001, or auto plan retire 001-aaaa l-e001" {
		t.Errorf("leg-undelivered = %+v", is)
	}

	set = writeSet(t, family(epicUnlisted("requirements"),
		childDoc("solution", "001", nil, append(childEdges("discharges"), edge("plan", "honors", "001-aaaa:r-zzzz")))))
	got = map[string]Issue{}
	for _, is := range lintNumber(set, "002").Issues {
		got[is.Code] = is
	}
	if is := got["child-epic-mismatch"]; is.Path != "$.nodes[plan].fields.epic" || is.Field != "epic" ||
		is.Message != "Plan 002-bbbb names epic 001-aaaa, but epic 001-aaaa lists no child node for plan 002-bbbb." ||
		!strings.HasPrefix(is.Hint, `auto plan add 001-aaaa child --plan 002-bbbb --title "…"`) {
		t.Errorf("child-epic-mismatch = %+v", is)
	}
	if is := got["dangling-ref"]; is.Message != `edge targets "001-aaaa:r-zzzz", but plan 001-aaaa has no node r-zzzz` ||
		is.Hint != "remove the edge with `auto plan unlink 002-bbbb plan honors 001-aaaa:r-zzzz`, or point it at an existing node of an allowed type" {
		t.Errorf("dangling-ref = %+v", is)
	}
	if is := got["rail-undischarged"]; is.Message != "Plan 002-bbbb honours rail 001-aaaa:r-e001, but no AC discharges it." ||
		is.Hint != "auto plan link 002-bbbb <ac-id> discharges 001-aaaa:r-e001 (or add an AC for it with --discharges 001-aaaa:r-e001)" {
		t.Errorf("rail-undischarged = %+v", is)
	}
}

// annex builds an annex node with the given kind, path and title.
func annex(id, kind, path, title string) string {
	return node(id, "annex", `"kind": "`+kind+`", "path": "`+path+`", "title": "`+title+`"`)
}

// mdFile is one Markdown file for an fstest.MapFS.
func mdFile(content string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(content)} }

// TestAnnexRules drives the folder-aware rules through an fstest.MapFS (no temp
// dirs): the plan folder's files are the map, annex paths resolve against it.
func TestAnnexRules(t *testing.T) {
	// base is five proven goals at solution: clean but for what the case adds,
	// and past the annex rules' MinLifecycle.
	base := func(extraNodes []string) string {
		n, e := goals(5)
		return fixture("solution", append(n, extraNodes...), e)
	}
	cases := []struct {
		name  string
		doc   string
		files fstest.MapFS
		want  []string
	}{
		{"annex-missing: the file is not in the folder",
			base([]string{annex("ax-0001", "usage", "usage.md", "How to use it")}),
			fstest.MapFS{}, []string{"annex-missing"}},
		{"valid annex: file present, no references",
			base([]string{annex("ax-0001", "usage", "usage.md", "How to use it")}),
			fstest.MapFS{"usage.md": mdFile("# Usage\n\nRun it.\n")}, nil},
		{"unlinked-file: a stray .md no annex names",
			base([]string{annex("ax-0001", "usage", "usage.md", "How to use it")}),
			fstest.MapFS{"usage.md": mdFile("# Usage\n"), "stray.md": mdFile("# Stray\n")},
			[]string{"unlinked-file"}},
		{"unlinked-file: graph.json is never flagged",
			base([]string{annex("ax-0001", "usage", "usage.md", "How to use it")}),
			fstest.MapFS{"usage.md": mdFile("# Usage\n"), "graph.json": mdFile("{}")}, nil},
		{"annex-ref: a dangling [[id]] in the annex body",
			base([]string{annex("ax-0001", "structures", "structures.md", "The shapes")}),
			fstest.MapFS{"structures.md": mdFile("See [[ac-zz9q]] for the shape.\n")},
			[]string{"annex-ref"}},
		{"annex-ref: a reference that resolves passes",
			base([]string{annex("ax-0001", "structures", "structures.md", "The shapes")}),
			fstest.MapFS{"structures.md": mdFile("See [[g-aaa1]] for the goal.\n")}, nil},
		{"annex-ref: a [[id]] in a code span is an example, not a reference",
			base([]string{annex("ax-0001", "structures", "structures.md", "The shapes")}),
			fstest.MapFS{"structures.md": mdFile("Write `[[ac-zz9q]]` to reference a node.\n")}, nil},
		{"all three at once, in reporting order",
			base([]string{
				annex("ax-0001", "usage", "usage.md", "How to use it"),
				annex("ax-0002", "structures", "structures.md", "The shapes"),
			}),
			fstest.MapFS{
				"structures.md": mdFile("See [[ac-zz9q]].\n"),
				"stray.md":      mdFile("# Stray\n"),
			},
			[]string{"annex-missing", "unlinked-file", "annex-ref"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, err := graph.Parse([]byte(tc.doc))
			if err != nil {
				t.Fatal(err)
			}
			r := run(&Context{Plan: "001", Graph: g, Lifecycle: Lifecycle(g), FS: tc.files})
			var got []string
			for _, is := range r.Issues {
				got = append(got, is.Code)
				if is.Message == "" || is.Path == "" || is.Hint == "" {
					t.Errorf("issue lacks message/path/hint: %+v", is)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("codes = %v, want %v\n%+v", got, tc.want, r.Issues)
			}
		})
	}
}

// TestAnnexRulesSkipWithoutFolder: linted without its folder (c.FS nil), a
// plan's annex rules do not run, even with annex nodes present.
func TestAnnexRulesSkipWithoutFolder(t *testing.T) {
	doc := func() string {
		n, e := goals(5)
		return fixture("solution", append(n, annex("ax-0001", "usage", "usage.md", "t")), e)
	}()
	for _, is := range issuesOf(t, "001", doc) {
		if is.Code == "annex-missing" || is.Code == "unlinked-file" || is.Code == "annex-ref" {
			t.Errorf("annex rule ran without a folder: %+v", is)
		}
	}
}

// TestRuleLifecyclesAreRegistrySteps: every rule switches on at a step of
// the registry's lifecycle sequence (the sequence lives in the registry).
func TestRuleLifecyclesAreRegistrySteps(t *testing.T) {
	for _, rules := range [][]Rule{Rules, SetRules, EpicRules} {
		for _, r := range rules {
			if !slices.Contains(schema.Registry.Lifecycle, r.MinLifecycle) {
				t.Errorf("rule %s: MinLifecycle %q is not a step of schema.Registry.Lifecycle", r.Code, r.MinLifecycle)
			}
		}
	}
}

// collision is two task plans that both took number 002 on separate
// branches, an epic 001 whose prose cites [[002:plan]], and a plan whose ID
// disagrees with its folder.
func collision() map[string]string {
	task := func(name string) string { return planDoc(name, "task", "requirements", "", nil, nil) }
	return map[string]string{
		"001-mail-mvp": planDoc("mail-mvp", "epic", "requirements", "", []string{
			node("g-e001", "goal", `"title": "g", "description": "after [[002:plan]] and [[003:plan]]"`),
		}, nil),
		"002-beta":  strings.Replace(task("beta"), `"@PLAN@"`, `"002-b0b0"`, 1),
		"002-gamma": strings.Replace(task("gamma"), `"@PLAN@"`, `"002-g0g0"`, 1),
		"003-delta": strings.Replace(task("delta"), `"@PLAN@"`, `"007-d0d0"`, 1),
	}
}

func TestPlanSetRules(t *testing.T) {
	set := writeSet(t, collision())
	byFolder := func(folder string) Report {
		for _, p := range set.Plans() {
			if p.Folder() == folder {
				return Plan(set, p)
			}
		}
		t.Fatalf("no %s", folder)
		return Report{}
	}
	find := func(r Report, code string) Issue {
		for _, is := range r.Issues {
			if is.Code == code {
				return is
			}
		}
		t.Fatalf("%s: no %s in %+v", r.Plan, code, r.Issues)
		return Issue{}
	}

	beta, gamma := byFolder("002-beta"), byFolder("002-gamma")
	for _, r := range []Report{beta, gamma} {
		is := find(r, "duplicate-plan-number")
		if r.OK || is.Severity != SeverityError ||
			!strings.Contains(is.Message, "002-b0b0 (.auto/plan/plans/002-beta)") || !strings.Contains(is.Message, "002-g0g0 (.auto/plan/plans/002-gamma)") ||
			is.Hint != "auto plan renumber "+r.Plan+" (moves it to the next free number and rewrites every reference to it)" {
			t.Errorf("%s duplicate-plan-number = %+v", r.Plan, is)
		}
	}

	delta := byFolder("003-delta")
	if is := find(delta, "plan-id-mismatch"); is.Message != "Plan ID 007-d0d0 starts with 007, but its folder 003-delta is number 003." ||
		is.Hint != "auto plan renumber 003-delta --to 003 (rewrites the ID and every reference to it), or rename the folder back" {
		t.Errorf("plan-id-mismatch = %+v", is)
	}

	epic := byFolder("001-mail-mvp")
	amb := find(epic, "ambiguous-ref")
	if !strings.Contains(amb.Message, "[[002:plan]], but plan number 002 names 2 plans") || !strings.Contains(amb.Hint, "[[002-b0b0:plan]]") {
		t.Errorf("ambiguous-ref = %+v", amb)
	}
	// [[003:plan]] resolves (one folder has 003), whatever its ID says.
	for _, is := range epic.Issues {
		if is.Code == "dangling-prose-ref" {
			t.Errorf("unexpected %+v", is)
		}
	}

	// A copied folder keeps its ID: duplicate-plan-id.
	folders := collision()
	folders["004-copy"] = folders["002-beta"]
	set = writeSet(t, folders)
	for _, p := range set.Plans() {
		if p.Folder() == "004-copy" {
			if got := errorCodes(t, set, "004"); !slices.Contains(got, "duplicate-plan-id") || !slices.Contains(got, "plan-id-mismatch") {
				t.Errorf("copy: %v", got)
			}
		}
	}
}

// TestOtherVersions: a newer 1.x plan is checked and warned about; another
// major is warned about and not checked against this registry.
func TestOtherVersions(t *testing.T) {
	doc := func(version string) string {
		return strings.Replace(fixture("requirements", []string{node("w-0000", "widget", "")}, nil), `"version": "1.0.0"`, `"version": "`+version+`"`, 1)
	}
	codesOf := func(version string) []string {
		var out []string
		for _, is := range issuesOf(t, "001", doc(version)) {
			out = append(out, is.Code+"/"+string(is.Severity))
		}
		return out
	}
	if got := codesOf("1.0.0"); slices.Contains(got, "other-version/warning") {
		t.Errorf("1.0.0: %v", got)
	}
	if got := codesOf("1.3.0"); !slices.Equal(got, []string{"other-version/warning", "unregistered-type/error"}) {
		t.Errorf("1.3.0: %v", got)
	}
	if got := codesOf("2.0.0"); !slices.Equal(got, []string{"other-version/warning"}) {
		t.Errorf("2.0.0: %v", got)
	}
}
