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
		for _, end := range append(slices.Clone(e.From), e.To...) {
			if _, ok := Registry.Node(end); !ok {
				t.Errorf("edge %q names unregistered node type %q", e.Name, end)
			}
		}
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
