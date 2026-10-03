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
		"annex":       {"kind", "path", "title"},
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
		{"about", "annex", "goal", true},
		{"about", "annex", "ac", true},
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
			if f.Computed && (f.Required || f.Fixed || f.Kind == KindObject) {
				t.Errorf("%s: a Computed field must be an optional scalar the tool sets (not required, not Fixed)", where)
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

// TestACLayerIsOptionalEnum pins the optional test-layer field AC-2 adds to
// the ac node: an enum of the five layers, not required, so ACs without a
// layer (the frozen dogfood fixture) stay valid.
func TestACLayerIsOptionalEnum(t *testing.T) {
	ac, ok := Registry.Node("ac")
	if !ok {
		t.Fatal("ac node type is not registered")
	}
	layer, ok := ac.Field("layer")
	if !ok {
		t.Fatal("ac has no layer field")
	}
	if layer.Kind != KindEnum || layer.Required {
		t.Errorf("ac.layer must be an optional enum, got kind=%q required=%v", layer.Kind, layer.Required)
	}
	if !slices.Equal(layer.Enum, []string{"e2e", "integration", "golden", "unit", "manual"}) {
		t.Errorf("ac.layer enum = %v", layer.Enum)
	}
}

// TestAnnexHashIsComputed pins annex.hash as a Computed field (D-6): the tool
// sets it at freeze, so it is never required and never a CLI flag.
func TestAnnexHashIsComputed(t *testing.T) {
	annex, ok := Registry.Node("annex")
	if !ok {
		t.Fatal("annex node type is not registered")
	}
	if annex.Prefix != "ax" || annex.Term != "Annex" || annex.UniqueBy != "kind" {
		t.Errorf("annex = prefix %q, term %q, uniqueBy %q", annex.Prefix, annex.Term, annex.UniqueBy)
	}
	hash, ok := annex.Field("hash")
	if !ok {
		t.Fatal("annex has no hash field")
	}
	if !hash.Computed || hash.Required {
		t.Errorf("annex.hash must be Computed and not required, got computed=%v required=%v", hash.Computed, hash.Required)
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

// TestRepoPathPatternStaysInRepo: a file path is canonical and repo-relative,
// so it can never point outside the repository.
func TestRepoPathPatternStaysInRepo(t *testing.T) {
	re := regexp.MustCompile(RepoPathPattern)
	for _, p := range []string{"a", "a/b.go", "auto-plan/internal/", "docs/.gitignore", ".github/x.yml", "a/...b", "..hidden"} {
		if !re.MatchString(p) {
			t.Errorf("rejects %q", p)
		}
	}
	for _, p := range []string{"", ".", "..", "../outside", "a/../../outside", "a/..", "./a", "a/./b", "a//b", "/abs", `C:\outside`, `a\b`, "a b"} {
		if re.MatchString(p) {
			t.Errorf("accepts %q", p)
		}
	}
}

// TestLifecycleLivesInRegistry: the lifecycle sequence is owned by the
// registry. Every node type's MinLifecycle names one of its steps, the plan
// node's lifecycle enum is exactly the sequence, and the step constants are
// all in it. (lint's TestRuleLifecyclesAreRegistrySteps covers the rules.)
func TestLifecycleLivesInRegistry(t *testing.T) {
	if len(Registry.Lifecycle) == 0 {
		t.Fatal("Registry.Lifecycle is empty")
	}
	seen := map[Lifecycle]bool{}
	for _, l := range Registry.Lifecycle {
		if seen[l] {
			t.Errorf("lifecycle step %q is listed twice", l)
		}
		seen[l] = true
	}
	for _, n := range Registry.Nodes {
		if !slices.Contains(Registry.Lifecycle, n.MinLifecycle) {
			t.Errorf("node type %q: MinLifecycle %q is not a step of Registry.Lifecycle", n.Name, n.MinLifecycle)
		}
	}
	plan, _ := Registry.Node(PlanNodeID)
	f, _ := plan.Field("lifecycle")
	if !slices.Equal(f.Enum, LifecycleNames()) {
		t.Errorf("plan.lifecycle enum %v differs from Registry.Lifecycle %v", f.Enum, LifecycleNames())
	}
	for _, l := range []Lifecycle{LifecycleRequirements, LifecycleSolution, LifecyclePlan, LifecycleExecuting, LifecycleDone} {
		if l.Index() < 0 {
			t.Errorf("step constant %q is not in Registry.Lifecycle", l)
		}
	}
}

// TestPlanRefFieldsUsePlanIDPattern: every field holding plan IDs is a
// string or list checked against PlanIDPattern, so shorthand expansion and
// renumber can treat them uniformly.
func TestPlanRefFieldsUsePlanIDPattern(t *testing.T) {
	n := 0
	for _, nt := range Registry.Nodes {
		for _, f := range nt.Fields {
			if !f.PlanRef {
				continue
			}
			n++
			if f.Pattern != PlanIDPattern || (f.Kind != KindString && f.Kind != KindList) {
				t.Errorf("%s.%s: a PlanRef field must be a string or list with Pattern PlanIDPattern", nt.Name, f.Name)
			}
		}
	}
	if n != 3 {
		t.Errorf("want 3 PlanRef fields (plan.epic, child.plan, rail.deferred), got %d", n)
	}
}

// TestEdgePrefixIsReserved: edge IDs are `e-xxxx` and share one ID space
// with nodes, so no node type may use the prefix "e".
func TestEdgePrefixIsReserved(t *testing.T) {
	for _, nt := range Registry.Nodes {
		if nt.Prefix == "e" {
			t.Errorf("node type %q uses the reserved edge prefix e", nt.Name)
		}
	}
}

func TestParseVersion(t *testing.T) {
	for s, want := range map[string]SemVer{"1.0.0": {1, 0, 0}, "1.12.3": {1, 12, 3}, "2.0.10": {2, 0, 10}} {
		got, ok := ParseVersion(s)
		if !ok || got != want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", s, got, ok, want)
		}
	}
	for _, s := range []string{"", "1", "1.0", "v1.0.0", "01.0.0", "1.0.0-rc1", "1.0.x"} {
		if _, ok := ParseVersion(s); ok {
			t.Errorf("ParseVersion(%q) accepted", s)
		}
	}
	if (SemVer{1, 2, 0}).Compare(SemVer{1, 10, 0}) >= 0 {
		t.Error("1.2.0 must sort before 1.10.0")
	}
	if _, ok := ParseVersion(Version); !ok {
		t.Errorf("schema.Version %q is not semver", Version)
	}
}
