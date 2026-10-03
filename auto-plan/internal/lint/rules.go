package lint

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
	"github.com/mistakenot/auto-plan/internal/tree"
	"github.com/mistakenot/auto-plan/internal/workspace"
)

// Edge type names the rules read.
const (
	edgeProves    = "proves"
	edgeRejects   = "rejects"
	edgeDependsOn = "dependsOn"
	edgeTouches   = "touches"
)

// kindEpic is the plan kind whose plans hold rails, journeys and children.
const kindEpic = "epic"

// Goal-count bounds: lint warns outside them.
const (
	minGoals = 5
	maxGoals = 8
)

// Rules is every lint rule, in reporting order: errors first, then warnings.
//
// Structural codes (dangling-ref, bad-id, duplicate-id, wrong-endpoint,
// missing-field, …) are not rules: they come from graph.Validate, the one
// shared validate(), and Graph merges them in first, always as errors. So a
// problem is reported once: an edge whose target does not exist at all is a
// dangling-ref, while the rules below cover edges whose target exists but is
// retired (missing-dep, untracked-file, retired-ref).
//
// Gating (D-10): a rule applies once the plan reaches MinLifecycle.
// Checks that need nodes a plan only has later (ACs at solution; stages and
// file coverage at plan) wait for that step; checks of what is already there
// (an open question, a cycle, a bad prose reference) apply at every step.
var Rules = []Rule{
	{Code: "plan-id-mismatch", Severity: SeverityError, MinLifecycle: schema.LifecycleRequirements, Check: planIDMismatch},
	{Code: "open-question", Severity: SeverityError, MinLifecycle: schema.LifecycleRequirements, Check: openQuestion},
	{Code: "ac-no-goal", Severity: SeverityError, MinLifecycle: schema.LifecycleRequirements, Check: acNoGoal},
	{Code: "ac-multi-goal", Severity: SeverityError, MinLifecycle: schema.LifecycleRequirements, Check: acMultiGoal},
	{Code: "dangling-prose-ref", Severity: SeverityError, MinLifecycle: schema.LifecycleRequirements, Check: danglingProseRef},
	{Code: "ambiguous-ref", Severity: SeverityError, MinLifecycle: schema.LifecycleRequirements, Check: ambiguousRef},
	{Code: "tree-syntax", Severity: SeverityError, MinLifecycle: schema.LifecycleRequirements, Check: treeSyntax},
	{Code: "dependency-cycle", Severity: SeverityError, MinLifecycle: schema.LifecycleRequirements, Check: dependencyCycle},
	{Code: "goal-no-ac", Severity: SeverityError, MinLifecycle: schema.LifecycleSolution, Check: goalNoAC},
	{Code: "ac-no-verify", Severity: SeverityError, MinLifecycle: schema.LifecycleSolution, Check: acNoVerify},
	{Code: "unplanned-file", Severity: SeverityError, MinLifecycle: schema.LifecyclePlan, Check: unplannedFile},
	{Code: "untracked-file", Severity: SeverityError, MinLifecycle: schema.LifecyclePlan, Check: untrackedFile},
	{Code: "missing-dep", Severity: SeverityError, MinLifecycle: schema.LifecyclePlan, Check: missingDep},
	{Code: "goal-count", Severity: SeverityWarning, MinLifecycle: schema.LifecycleSolution, Check: goalCount},
	{Code: "decision-no-alternative", Severity: SeverityWarning, MinLifecycle: schema.LifecycleRequirements, Check: decisionNoAlternative},
	{Code: "retired-ref", Severity: SeverityWarning, MinLifecycle: schema.LifecycleRequirements, Check: retiredRef},
}

// active returns the active nodes of type typ, in graph order.
func active(c *Context, typ string) []graph.Node {
	var out []graph.Node
	for _, n := range c.Graph.Nodes {
		if n.Type == typ && n.Active() {
			out = append(out, n)
		}
	}
	return out
}

// activeTargets returns the active local nodes of type typ that edges of
// edgeType from id point at, in edge order.
func activeTargets(c *Context, id, edgeType, typ string) []string {
	var out []string
	for _, e := range c.Graph.Edges {
		if e.From != id || e.Type != edgeType {
			continue
		}
		if n, ok := c.Node(e.To); ok && n.Type == typ && n.Active() && !slices.Contains(out, n.ID) {
			out = append(out, n.ID)
		}
	}
	return out
}

// isActive reports whether id names an active local node.
func isActive(c *Context, id string) bool {
	n, ok := c.Node(id)
	return ok && n.Active()
}

// openQuestion gates on every open question, like pd-lint's pd-question.
func openQuestion(c *Context) []Issue {
	var out []Issue
	for _, n := range active(c, "question") {
		if n.StringField("status") != "open" {
			continue
		}
		out = append(out, Issue{
			Path: graph.NodePath(n.ID) + ".fields.status", Field: "status",
			Message: fmt.Sprintf("Question %s %q is awaiting a human answer.", n.ID, n.StringField("title")),
			Hint:    "answer it: auto plan update " + c.Plan + " " + n.ID + ` --status answered --answer "…"`,
		})
	}
	return out
}

// acNoGoal reports every active AC that proves no active goal.
func acNoGoal(c *Context) []Issue {
	var out []Issue
	for _, n := range active(c, "ac") {
		if len(activeTargets(c, n.ID, edgeProves, "goal")) > 0 {
			continue
		}
		out = append(out, Issue{
			Path:    graph.NodePath(n.ID),
			Message: n.ID + " proves no active goal; every AC proves exactly one",
			Hint:    "auto plan link " + c.Plan + " " + n.ID + " proves <goal-id>",
		})
	}
	return out
}

// acMultiGoal reports every active AC that proves more than one active goal.
func acMultiGoal(c *Context) []Issue {
	var out []Issue
	for _, n := range active(c, "ac") {
		goals := activeTargets(c, n.ID, edgeProves, "goal")
		if len(goals) < 2 {
			continue
		}
		out = append(out, Issue{
			Path:    graph.NodePath(n.ID),
			Message: fmt.Sprintf("%s proves %d goals (%s); every AC proves exactly one", n.ID, len(goals), strings.Join(goals, ", ")),
			Hint:    "keep one goal: auto plan unlink " + c.Plan + " " + n.ID + " proves " + goals[len(goals)-1],
		})
	}
	return out
}

// goalNoAC reports every active goal that no active AC proves. An epic's
// goals are exempt: its child plans carry the ACs, and an epic's own goals
// are reached from theirs (trace's child-of hop), never proven locally.
func goalNoAC(c *Context) []Issue {
	if plan, _ := c.Graph.Plan(); plan.StringField("kind") == kindEpic {
		return nil
	}
	proven := map[string]bool{}
	for _, e := range c.Graph.Edges {
		if e.Type == edgeProves && isActive(c, e.From) {
			proven[e.To] = true
		}
	}
	var out []Issue
	for _, n := range active(c, "goal") {
		if proven[n.ID] {
			continue
		}
		out = append(out, Issue{
			Path:    graph.NodePath(n.ID),
			Message: n.ID + " has no active AC",
			Hint:    "auto plan add " + c.Plan + " ac --proves " + n.ID + ` --title "…"`,
		})
	}
	return out
}

// acNoVerify reports every active AC with no verify command. A manual AC
// (verify.kind manual) needs none.
func acNoVerify(c *Context) []Issue {
	var out []Issue
	for _, n := range active(c, "ac") {
		v := n.ObjectField("verify")
		if kind, _ := v["kind"].(string); kind == "manual" {
			continue
		}
		if cmd, _ := v["cmd"].(string); strings.TrimSpace(cmd) != "" {
			continue
		}
		out = append(out, Issue{
			Path: graph.NodePath(n.ID) + ".fields.verify", Field: "verify",
			Message: n.ID + " has no verify command",
			Hint: "auto plan update " + c.Plan + " " + n.ID + ` --verify-cmd "…"` +
				" (or --verify-kind manual when no command can check it)",
		})
	}
	return out
}

// goalCount warns when the plan has fewer than 5 or more than 8 active goals.
func goalCount(c *Context) []Issue {
	n := len(active(c, "goal"))
	if n >= minGoals && n <= maxGoals {
		return nil
	}
	fix := "split a broad goal or add the missing outcomes: auto plan add " + c.Plan + ` goal --title "…"`
	if n > maxGoals {
		fix = "merge overlapping goals and retire the rest: auto plan retire " + c.Plan + " <goal-id>"
	}
	return []Issue{{
		Path:    "$.nodes",
		Message: fmt.Sprintf("the plan has %d active goal%s; aim for %d–%d", n, plural(n), minGoals, maxGoals),
		Hint:    fix,
	}}
}

// decisionNoAlternative warns about every active decision that rejects no
// active alternative.
func decisionNoAlternative(c *Context) []Issue {
	var out []Issue
	for _, n := range active(c, "decision") {
		if len(activeTargets(c, n.ID, edgeRejects, "alternative")) > 0 {
			continue
		}
		out = append(out, Issue{
			Path:    graph.NodePath(n.ID),
			Message: n.ID + " rejects no alternative",
			Hint: "auto plan add " + c.Plan + ` alternative --title "…" --why "…"` +
				", then auto plan link " + c.Plan + " " + n.ID + " rejects <alternative-id>",
		})
	}
	return out
}

// proseRef matches a `[[…]]` reference in Markdown prose.
var proseRef = graph.ProseRefRE()

// qualifiedRef reports the shape of a cross-plan prose reference: the full
// `[[005-k7q2:r-8hw3]]`, or the hand-written shorthand `[[005:r-8hw3]]`,
// which resolves when exactly one plan has the number.
func qualifiedRef(ref string) bool {
	return graph.QualifiedRefPattern.MatchString(ref) || graph.ShortRefPattern.MatchString(ref)
}

// planIDMismatch reports a plan ID whose number is not its folder's: the
// folder or the ID was renamed by hand. renumber puts both on one number.
func planIDMismatch(c *Context) []Issue {
	n := c.Graph.Number()
	if c.Number == "" || n == "" || n == c.Number {
		return nil
	}
	return []Issue{{
		Path: "$.id", Field: "id",
		Message: fmt.Sprintf("Plan ID %s starts with %s, but its folder %s is number %s.", c.Graph.ID, n, c.Folder, c.Number),
		Hint:    "auto plan renumber " + c.Folder + " --to " + c.Number + " (rewrites the ID and every reference to it), or rename the folder back",
	}}
}

// ambiguousRef reports a shorthand prose reference `[[NNN:id]]` whose number
// names more than one plan, so it cannot be resolved.
func ambiguousRef(c *Context) []Issue {
	if c.Set == nil {
		return nil
	}
	var out []Issue
	proseRefs(c, func(n graph.Node, path, field, ref string) {
		if !graph.ShortRefPattern.MatchString(ref) {
			return
		}
		var amb *workspace.AmbiguousError
		if _, err := c.Set.Lookup(ref); errors.As(err, &amb) {
			_, id, _ := graph.ParseRef(ref)
			out = append(out, Issue{Path: path, Field: field,
				Message: fmt.Sprintf("%s's %s refers to [[%s]], but plan number %s names %d plans: %s", n.ID, field, ref, amb.Arg, len(amb.Candidates), planList(amb.Candidates)),
				Hint:    "write the plan ID, e.g. [[" + graph.Qualify(amb.Candidates[0].Ref(), id) + "]], with auto plan update " + c.Plan + " " + n.ID + " --" + flagFor(path) + " …",
			})
		}
	})
	return out
}

// proseRefs calls fn for each distinct `[[…]]` reference in every text field
// (registry kind text, nested objects included) of every active node.
func proseRefs(c *Context, fn func(n graph.Node, path, field, ref string)) {
	for _, n := range c.Graph.Nodes {
		if !n.Active() {
			continue
		}
		nt, ok := schema.Registry.Node(n.Type)
		if !ok {
			continue
		}
		walkText(graph.NodePath(n.ID)+".fields", nt.Fields, n.Fields, func(path, field, text string) {
			var seen []string
			for _, m := range proseRef.FindAllStringSubmatch(text, -1) {
				if !slices.Contains(seen, m[1]) {
					seen = append(seen, m[1])
					fn(n, path, field, m[1])
				}
			}
		})
	}
}

func walkText(path string, specs []schema.FieldSpec, values map[string]any, fn func(path, field, text string)) {
	for i := range specs {
		f := &specs[i]
		switch f.Kind {
		case schema.KindText:
			if s, ok := values[f.Name].(string); ok {
				fn(path+"."+f.Name, f.Name, s)
			}
		case schema.KindObject:
			if m, ok := values[f.Name].(map[string]any); ok {
				walkText(path+"."+f.Name, f.Fields, m, fn)
			}
		case schema.KindString, schema.KindEnum, schema.KindList:
		}
	}
}

// danglingProseRef reports `[[id]]` references that name no node in the
// plan, and references that are not node IDs at all. A qualified
// `[[NNN-xxxx:id]]` (or `[[NNN:id]]`) must resolve against that plan when the plan
// is linted within its PlanSet; linted alone, it is checked for shape only. A
// reference to a retired node is a retired-ref warning instead.
func danglingProseRef(c *Context) []Issue {
	var out []Issue
	proseRefs(c, func(n graph.Node, path, field, ref string) {
		hint := "fix the reference with auto plan update " + c.Plan + " " + n.ID + " --" + flagFor(path) +
			" …; see the plan's node IDs with auto plan show " + c.Plan
		switch {
		case qualifiedRef(ref):
			if c.Set == nil {
				return
			}
			if _, err := c.Set.Lookup(ref); err != nil && !errors.Is(err, workspace.ErrAmbiguous) {
				planID, id, _ := graph.ParseRef(ref)
				why := "plan " + planID + " has no node " + id
				switch {
				case errors.Is(err, workspace.ErrNotFound):
					why = "there is no plan " + planID
				case !errors.Is(err, workspace.ErrNodeNotFound):
					why = "plan " + planID + " cannot be read"
				}
				out = append(out, Issue{Path: path, Field: field, Hint: hint,
					Message: fmt.Sprintf("%s's %s refers to [[%s]], but %s", n.ID, field, ref, why)})
			}
		case ref == schema.PlanNodeID || graph.IDPattern.MatchString(ref):
			if _, ok := c.Node(ref); ok {
				return
			}
			out = append(out, Issue{Path: path, Field: field, Hint: hint,
				Message: fmt.Sprintf("%s's %s refers to [[%s]], which is not a node in this plan", n.ID, field, ref)})
		default:
			out = append(out, Issue{Path: path, Field: field, Hint: hint,
				Message: fmt.Sprintf("%s's %s holds [[%s]], which is not a node ID (write [[ac-3fxm]], or [[005-k7q2:r-8hw3]] for another plan)", n.ID, field, ref)})
		}
	})
	return out
}

// flagFor is the update flag for a text field at path (`…fields.why` → why).
func flagFor(path string) string {
	_, rest, _ := strings.Cut(path, ".fields.")
	return strings.ReplaceAll(rest, ".", "-")
}

// treeSyntax parses every active tree's body as show-me notation.
func treeSyntax(c *Context) []Issue {
	var out []Issue
	for _, n := range active(c, "tree") {
		body, ok := n.Fields["body"].(string)
		if !ok {
			continue // missing or mistyped: a validation error already
		}
		_, errs := tree.Parse(body)
		for _, e := range errs {
			out = append(out, Issue{
				Path: graph.NodePath(n.ID) + ".fields.body", Field: "body",
				Message: n.ID + " body " + e.Error(),
				Hint:    "fix the show-me notation and rewrite it with auto plan update " + c.Plan + " " + n.ID + " --body @tree.txt",
			})
		}
	}
	return out
}

// dependsOnGraph returns the active nodes that start or end a dependsOn edge
// between active nodes, and the adjacency of those edges.
func dependsOnGraph(c *Context) ([]string, map[string][]string) {
	var nodes []string
	adj := map[string][]string{}
	for _, e := range c.Graph.Edges {
		if e.Type != edgeDependsOn || !isActive(c, e.From) || !isActive(c, e.To) {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
		for _, id := range []string{e.From, e.To} {
			if !slices.Contains(nodes, id) {
				nodes = append(nodes, id)
			}
		}
	}
	return nodes, adj
}

// dependencyCycle reports each cycle of dependsOn edges among active stages
// (or child plans), one per strongly connected component, with the path
// joined by " → " as pd-lint does.
func dependencyCycle(c *Context) []Issue {
	var out []Issue
	for _, cycle := range Cycles(dependsOnGraph(c)) {
		n, _ := c.Node(cycle[0])
		out = append(out, Issue{
			Path:    graph.NodePath(cycle[0]),
			Message: fmt.Sprintf("Dependency cycle among %s: %s.", pluralTerm(n.Type), strings.Join(cycle, CycleArrow)),
			Hint:    "break it: auto plan unlink " + c.Plan + " " + cycle[0] + " " + edgeDependsOn + " " + cycle[1],
		})
	}
	return out
}

// unplannedFile reports every active file change no active stage touches.
// Unlike pd-lint, which skips the check when a doc has no phases section, it
// applies whenever the plan is at `plan`: the lifecycle says stages are due.
func unplannedFile(c *Context) []Issue {
	touched := map[string]bool{}
	for _, e := range c.Graph.Edges {
		if e.Type == edgeTouches && isActive(c, e.From) {
			touched[e.To] = true
		}
	}
	var out []Issue
	for _, n := range active(c, "file") {
		if touched[n.ID] {
			continue
		}
		out = append(out, Issue{
			Path:    graph.NodePath(n.ID),
			Message: fmt.Sprintf("File %s (%s) is in the file tree but no stage touches it.", n.StringField("path"), n.ID),
			Hint:    "auto plan link " + c.Plan + " <stage-id> touches " + n.ID + ", or auto plan retire " + c.Plan + " " + n.ID,
		})
	}
	return out
}

// untrackedFile reports an active stage that touches a file change that is
// no longer in the file tree because it is retired. (A touches edge to an ID
// that does not exist is a dangling-ref, and one to a non-file node a
// wrong-endpoint, both from validation.)
func untrackedFile(c *Context) []Issue {
	var out []Issue
	for _, e := range c.Graph.Edges {
		if e.Type != edgeTouches || !isActive(c, e.From) {
			continue
		}
		f, ok := c.Node(e.To)
		if !ok || f.Active() {
			continue
		}
		out = append(out, Issue{
			Path: graph.EdgePath(e),
			Message: fmt.Sprintf("Stage %s touches %s (%s), which is missing from the file tree: the file change is retired.",
				e.From, f.StringField("path"), f.ID),
			Hint: "auto plan unlink " + c.Plan + " " + e.From + " touches " + f.ID +
				", and add the file again with auto plan add " + c.Plan + " file if the stage still changes it",
		})
	}
	return out
}

// missingDep reports an active stage (or child plan) that depends on one that
// no longer exists because it is retired. (A dependsOn edge to an ID that
// does not exist is a dangling-ref, from validation.)
func missingDep(c *Context) []Issue {
	var out []Issue
	for _, e := range c.Graph.Edges {
		if e.Type != edgeDependsOn || !isActive(c, e.From) {
			continue
		}
		dep, ok := c.Node(e.To)
		if !ok || dep.Active() {
			continue
		}
		from, _ := c.Node(e.From)
		out = append(out, Issue{
			Path: graph.EdgePath(e),
			Message: fmt.Sprintf("%s %s depends on %s %s, which doesn't exist: it is retired.",
				upperFirst(termOf(from.Type)), e.From, strings.ToLower(termOf(dep.Type)), dep.ID),
			Hint: "auto plan unlink " + c.Plan + " " + e.From + " " + edgeDependsOn + " " + dep.ID,
		})
	}
	return out
}

// retiredRef warns about every reference from an active node to a retired
// one: edges, and `[[id]]` prose references, qualified ones included when the
// plan is linted within its PlanSet. From `plan` on, touches and
// dependsOn edges to retired nodes are untracked-file and missing-dep errors
// instead, so they are not reported twice.
func retiredRef(c *Context) []Issue {
	var out []Issue
	specific := c.Lifecycle.AtLeast(schema.LifecyclePlan)
	for _, e := range c.Graph.Edges {
		if specific && (e.Type == edgeTouches || e.Type == edgeDependsOn) {
			continue
		}
		if !isActive(c, e.From) {
			continue
		}
		to, ok := c.Resolve(e.To)
		if !ok || to.Active() {
			continue
		}
		out = append(out, Issue{
			Path:    graph.EdgePath(e),
			Message: fmt.Sprintf("%s %s %s, which is retired", e.From, e.Type, e.To),
			Hint:    "auto plan unlink " + c.Plan + " " + e.From + " " + e.Type + " " + e.To + ", or link an active node instead",
		})
	}
	proseRefs(c, func(n graph.Node, path, field, ref string) {
		if to, ok := c.Resolve(ref); ok && !to.Active() {
			out = append(out, Issue{
				Path: path, Field: field,
				Message: fmt.Sprintf("%s's %s refers to [[%s]], which is retired", n.ID, field, ref),
				Hint:    "rewrite the reference with auto plan update " + c.Plan + " " + n.ID + " --" + flagFor(path) + " …",
			})
		}
	})
	return out
}

// termOf is a node type's glossary term, or the type name when unregistered.
func termOf(typ string) string {
	if nt, ok := schema.Registry.Node(typ); ok {
		return nt.Term
	}
	return typ
}

// pluralTerm is the lowercase plural of a type's term ("stages").
func pluralTerm(typ string) string { return strings.ToLower(termOf(typ)) + "s" }

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
