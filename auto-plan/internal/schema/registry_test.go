package schema

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestPrefixesUnique(t *testing.T) {
	seen := map[string]string{}
	for _, n := range Registry.Nodes {
		if n.Singleton() {
			continue
		}
		if !regexp.MustCompile(`^[a-z]{1,4}$`).MatchString(n.Prefix) {
			t.Errorf("node type %q: prefix %q must be 1-4 lowercase letters", n.Name, n.Prefix)
		}
		if other, ok := seen[n.Prefix]; ok {
			t.Errorf("prefix %q used by both %q and %q", n.Prefix, other, n.Name)
		}
		seen[n.Prefix] = n.Name
	}
}

func TestSingletonIsOnlyPlan(t *testing.T) {
	for _, n := range Registry.Nodes {
		if n.Singleton() != (n.Name == PlanNodeID) {
			t.Errorf("node type %q: Singleton()=%v; only %q may be a singleton", n.Name, n.Singleton(), PlanNodeID)
		}
	}
}

func TestTypeNamesUnique(t *testing.T) {
	var names []string
	for _, n := range Registry.Nodes {
		names = append(names, "node:"+n.Name)
	}
	for _, e := range Registry.Edges {
		names = append(names, "edge:"+e.Name)
	}
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	if len(slices.Compact(sorted)) != len(names) {
		t.Errorf("duplicate type names in registry: %v", names)
	}
}

func TestEdgeEndpointsNameRegisteredTypes(t *testing.T) {
	for _, e := range Registry.Edges {
		if len(e.From) == 0 || len(e.To) == 0 {
			t.Errorf("edge %q: needs at least one From and one To type", e.Name)
		}
		for _, end := range e.From {
			if _, ok := Registry.Node(end); !ok {
				t.Errorf("edge %q: From names unregistered node type %q", e.Name, end)
			}
		}
		for _, end := range e.To {
			if _, ok := Registry.Node(end); !ok && end != AnyType {
				t.Errorf("edge %q: To names unregistered node type %q", e.Name, end)
			}
		}
		if e.SameType {
			for _, from := range e.From {
				if !e.AllowsTo(from) {
					t.Errorf("edge %q: SameType, but %q may start it and not end it", e.Name, from)
				}
			}
		}
	}
}

// TestDesignNodeTypes pins the node types AC-4 requires and their required
// fields.
func TestDesignNodeTypes(t *testing.T) {
	want := map[string][]string{
		"plan":        {"name", "kind", "lifecycle", "created"},
		"goal":        {"title"},
		"ac":          {"title", "gwt"},
		"decision":    {"title", "chosen", "why", "by"},
		"alternative": {"title", "why"},
		"rail":        {"title"},
		"defect":      {"title"},
		"stage":       {"title", "steps", "commit"},
		"file":        {"path", "change", "why"},
		"question":    {"title", "status"},
		"tree":        {"title", "kind", "body"},
		"journey":     {"title"},
		"leg":         {"actor", "action"},
		"child":       {"plan"},
	}
	for name, required := range want {
		nt, ok := Registry.Node(name)
		if !ok {
			t.Errorf("node type %q is not registered", name)
			continue
		}
		var got []string
		for _, f := range nt.Fields {
			if f.Required {
				got = append(got, f.Name)
			}
		}
		if !slices.Equal(got, required) {
			t.Errorf("node type %q: required fields %v, want %v", name, got, required)
		}
	}
	if len(Registry.Nodes) != len(want) {
		t.Errorf("registry has %d node types, want %d", len(Registry.Nodes), len(want))
	}
}

// TestDesignEdgeEndpoints pins the edge endpoint sets AC-4 names.
func TestDesignEdgeEndpoints(t *testing.T) {
	cases := []struct {
		edge, from, to string
		ok             bool
	}{
		{"proves", "ac", "goal", true},
		{"proves", "decision", "goal", false},
		{"constrains", "decision", "goal", true},
		{"constrains", "decision", "ac", true},
		{"constrains", "decision", "stage", false},
		{"rejects", "decision", "alternative", true},
		{"rejects", "ac", "alternative", false},
		{"wouldBreak", "alternative", "ac", true},
		{"wouldBreak", "alternative", "goal", true},
		{"wouldBreak", "alternative", "rail", true},
		{"wouldBreak", "alternative", "decision", false},
		{"supersedes", "decision", "decision", true},
		{"dependsOn", "stage", "stage", true},
		{"dependsOn", "child", "child", true},
		{"touches", "stage", "file", true},
		{"covers", "stage", "ac", true},
		{"discharges", "ac", "rail", true},
		{"addresses", "goal", "defect", true},
		{"about", "tree", "goal", true},
		{"about", "tree", "file", true},
		{"about", "goal", "tree", false},
		{"in", "leg", "journey", true},
		{"honors", "plan", "rail", true},
		{"delivers", "plan", "leg", true},
		{"builds-on", "plan", "decision", true},
	}
	for _, tc := range cases {
		e, ok := Registry.Edge(tc.edge)
		if !ok {
			t.Errorf("edge %q is not registered", tc.edge)
			continue
		}
		if got := e.AllowsFrom(tc.from) && e.AllowsTo(tc.to); got != tc.ok {
			t.Errorf("%s %s → %s allowed=%v, want %v", tc.edge, tc.from, tc.to, got, tc.ok)
		}
	}
	for _, name := range []string{"honors", "delivers", "builds-on", "discharges"} {
		if e, _ := Registry.Edge(name); !e.CrossPlan {
			t.Errorf("edge %q must accept qualified cross-plan targets", name)
		}
	}
	for _, name := range []string{"proves", "constrains", "touches", "covers"} {
		if e, _ := Registry.Edge(name); e.CrossPlan {
			t.Errorf("edge %q must stay plan-local", name)
		}
	}
	if e, _ := Registry.Edge("dependsOn"); !e.SameType {
		t.Error("dependsOn must join same-type endpoints (stage → stage, child → child)")
	}
}

func TestRankScopeNamesOutgoingEdge(t *testing.T) {
	for _, n := range Registry.Nodes {
		if n.RankScope == "" {
			continue
		}
		e, ok := Registry.Edge(n.RankScope)
		if !ok || !slices.Contains(e.From, n.Name) {
			t.Errorf("node type %q: RankScope %q must be an edge type starting at %q", n.Name, n.RankScope, n.Name)
		}
	}
}

func TestFieldSpecsWellFormed(t *testing.T) {
	var check func(owner string, fields []FieldSpec)
	check = func(owner string, fields []FieldSpec) {
		seen := map[string]bool{}
		for _, f := range fields {
			where := owner + "." + f.Name
			if !regexp.MustCompile(`^[a-z][a-z0-9]*$`).MatchString(f.Name) {
				t.Errorf("%s: field name must be lowercase alphanumeric (it becomes a flag)", where)
			}
			if seen[f.Name] {
				t.Errorf("%s: duplicate field", where)
			}
			seen[f.Name] = true
			if f.Pattern != "" {
				if _, err := regexp.Compile(f.Pattern); err != nil {
					t.Errorf("%s: pattern does not compile: %v", where, err)
				}
			}
			switch f.Kind {
			case KindEnum:
				if len(f.Enum) == 0 {
					t.Errorf("%s: enum field has no values", where)
				}
			case KindObject:
				if len(f.Fields) == 0 {
					t.Errorf("%s: object field has no members", where)
				}
				check(where, f.Fields)
			case KindString, KindText, KindList:
			default:
				t.Errorf("%s: unknown kind %q", where, f.Kind)
			}
			if f.Fixed && (f.Kind == KindObject || !f.Required) {
				t.Errorf("%s: a Fixed field must be a required scalar (it is set at creation)", where)
			}
			if f.Help == "" {
				t.Errorf("%s: missing Help (it is the flag's --help text)", where)
			}
		}
	}
	for _, n := range Registry.Nodes {
		if n.MinLifecycle.Index() < 0 {
			t.Errorf("node type %q: MinLifecycle %q is not a lifecycle step", n.Name, n.MinLifecycle)
		}
		check(n.Name, n.Fields)
	}
}

// TestEveryTypeHasGlossaryTerm proves each registered node type names a
// canonical term that exists in the ubiquitous-language glossary, with no
// exemptions (AC-1).
func TestEveryTypeHasGlossaryTerm(t *testing.T) {
	path := glossaryPath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("glossary not readable (%v); skipping", err)
	}
	text := string(data)
	for _, n := range Registry.Nodes {
		if n.Term == "" {
			t.Errorf("node type %q has no Term", n.Name)
			continue
		}
		if !strings.Contains(text, "\n**"+n.Term+"**:\n") {
			t.Errorf("node type %q: glossary has no **%s**: entry", n.Name, n.Term)
		}
	}
}

// glossaryPath walks up from this file to the go.work root.
func glossaryPath(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return filepath.Join(dir, "docs", "concepts", "UBIQUITOUS_LANGUAGE.md")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skip("no go.work root found; skipping glossary check")
		}
		dir = parent
	}
}

func TestLifecycleOrder(t *testing.T) {
	if !LifecycleSolution.AtLeast(LifecycleRequirements) || LifecycleRequirements.AtLeast(LifecycleSolution) {
		t.Fatal("lifecycle ordering wrong")
	}
	if Lifecycle("bogus").AtLeast(LifecycleRequirements) {
		t.Fatal("unknown lifecycle must reach nothing")
	}
}
