package lint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
)

// Edge type names the epic rules read.
const (
	edgeHonors     = "honors"
	edgeDelivers   = "delivers"
	edgeDischarges = "discharges"
	edgeSupersedes = "supersedes"
)

// EpicRules are the cross-plan rules (AC-11). They run only when a plan is
// linted within its PlanSet — `auto plan lint <plan>` and `lint all` — and
// each runs from the perspective of the plan being linted, so an issue lands
// on the plan whose graph holds the node it is about:
//
//	epic side   rail-unhonored, leg-undelivered, child-missing,
//	            child-epic-mismatch (a child node whose plan names another epic)
//	child side  rail-undischarged, child-epic-mismatch (epic set but not listed),
//	            superseded-ref
//
// A child dependsOn cycle is the per-graph dependency-cycle rule: child nodes
// and their dependsOn edges live in the epic's own graph.
//
// Gating (D-10), by the linted plan's own lifecycle:
//
//   - child-missing, child-epic-mismatch and superseded-ref apply at every
//     step: each is a reference that is wrong now, like dangling-ref.
//   - rail-undischarged waits for solution, when the child's ACs are due.
//   - rail-unhonored and leg-undelivered wait for plan, when the epic has been
//     broken down into child plans. A requirements- or solution-step epic with
//     rails and legs but no children yet lints clean.
var EpicRules = []Rule{
	{Code: "child-missing", Severity: SeverityError, MinLifecycle: schema.LifecycleRequirements, Check: childMissing},
	{Code: "child-epic-mismatch", Severity: SeverityError, MinLifecycle: schema.LifecycleRequirements, Check: childEpicMismatch},
	{Code: "superseded-ref", Severity: SeverityError, MinLifecycle: schema.LifecycleRequirements, Check: supersededRef},
	{Code: "rail-undischarged", Severity: SeverityError, MinLifecycle: schema.LifecycleSolution, Check: railUndischarged},
	{Code: "rail-unhonored", Severity: SeverityError, MinLifecycle: schema.LifecyclePlan, Check: railUnhonored},
	{Code: "leg-undelivered", Severity: SeverityError, MinLifecycle: schema.LifecyclePlan, Check: legUndelivered},
}

// isEpic reports whether the linted plan is kind epic.
func isEpic(c *Context) bool {
	plan, _ := c.Graph.Plan()
	return plan.StringField("kind") == kindEpic
}

// childNodes returns the plan's active child nodes, in graph order.
func childNodes(c *Context) []graph.Node { return active(c, "child") }

// planEdgeTo reports whether plan planID has an edge `plan <typ> <to>`.
func planEdgeTo(c *Context, planID, typ, to string) bool {
	g, ok := c.Set.Graph(planID)
	if !ok {
		return false
	}
	return slices.ContainsFunc(g.Edges, func(e graph.Edge) bool {
		return e.From == schema.PlanNodeID && e.Type == typ && e.To == to
	})
}

// childMissing reports an epic's child node whose plan folder does not exist.
func childMissing(c *Context) []Issue {
	var out []Issue
	for _, n := range childNodes(c) {
		p := n.StringField("plan")
		if !qualifiedPlan(p) || c.Set.Has(p) {
			continue // a malformed number is a validation error already
		}
		out = append(out, Issue{
			Path: graph.NodePath(n.ID) + ".fields.plan", Field: "plan",
			Message: fmt.Sprintf("Child %s names plan %s, which has no folder under docs/plans.", n.ID, p),
			Hint: "create it with auto plan new <name> --kind task --epic " + c.Plan +
				" and point the child at it (auto plan update " + c.Plan + " " + n.ID + " --plan <NNN>), or auto plan retire " + c.Plan + " " + n.ID,
		})
	}
	return out
}

func qualifiedPlan(p string) bool {
	return len(p) == 3 && strings.Trim(p, "0123456789") == ""
}

// childEpicMismatch reports both halves of a broken epic ↔ child link.
//
// Child side: the plan's `epic` names a plan that does not exist, that is not
// kind epic, or that has no active child node naming this plan.
//
// Epic side (the reverse case): an active child node names an existing plan
// whose `epic` is not this epic. Both halves are reported because each is a
// reference that is wrong in its own graph, and each has its own one-command
// fix there.
func childEpicMismatch(c *Context) []Issue {
	var out []Issue
	plan, _ := c.Graph.Plan()
	if epic := plan.StringField("epic"); epic != "" && qualifiedPlan(epic) {
		path := graph.NodePath(schema.PlanNodeID) + ".fields.epic"
		fix := "auto plan update " + c.Plan + " plan --epic <NNN> (or --epic \"\" to leave the epic)"
		issue := func(msg, hint string) {
			out = append(out, Issue{Path: path, Field: "epic", Message: msg, Hint: hint})
		}
		switch g, err := c.Set.Load(epic); {
		case !c.Set.Has(epic):
			issue(fmt.Sprintf("Plan %s names epic %s, which has no folder under docs/plans.", c.Plan, epic), fix)
		case err != nil:
			// The epic's own lint reports its parse error.
		default:
			ep, _ := g.Plan()
			switch {
			case ep.StringField("kind") != kindEpic:
				issue(fmt.Sprintf("Plan %s names epic %s, which is kind %q, not an epic.", c.Plan, epic, ep.StringField("kind")), fix)
			case !slices.ContainsFunc(g.Nodes, func(n graph.Node) bool {
				return n.Type == "child" && n.Active() && n.StringField("plan") == c.Plan
			}):
				issue(fmt.Sprintf("Plan %s names epic %s, but epic %s lists no child node for plan %s.", c.Plan, epic, epic, c.Plan),
					"auto plan add "+epic+" child --plan "+c.Plan+` --title "…"`+", or "+fix)
			}
		}
	}

	if !isEpic(c) {
		return out
	}
	for _, n := range childNodes(c) {
		p := n.StringField("plan")
		g, ok := c.Set.Graph(p)
		if !ok || p == c.Plan {
			continue // missing: child-missing; unreadable: its own lint
		}
		cp, _ := g.Plan()
		if got := cp.StringField("epic"); got != c.Plan {
			names := "no epic"
			if got != "" {
				names = "epic " + got
			}
			out = append(out, Issue{
				Path: graph.NodePath(n.ID) + ".fields.plan", Field: "plan",
				Message: fmt.Sprintf("Child %s names plan %s, but plan %s declares %s.", n.ID, p, p, names),
				Hint:    "auto plan update " + p + " plan --epic " + c.Plan + ", or auto plan retire " + c.Plan + " " + n.ID,
			})
		}
	}
	return out
}

// supersededRef reports a qualified reference — an edge from an active node,
// or a `[[NNN:id]]` in prose — to a decision that an active decision in its
// own plan supersedes. It covers every cross-plan edge type (honors,
// builds-on, discharges, delivers, wouldBreak), though only builds-on and
// prose can name a decision.
func supersededRef(c *Context) []Issue {
	var out []Issue
	superseder := func(ref string) (string, bool) {
		planID, id, qualified := graph.ParseRef(ref)
		if !qualified {
			return "", false
		}
		n, ok := c.Resolve(ref)
		if !ok || n.Type != "decision" {
			return "", false
		}
		g, _ := c.Set.Graph(planID)
		for _, e := range g.Edges {
			if e.Type != edgeSupersedes || e.To != id {
				continue
			}
			if by, ok := g.NodeByID(e.From); ok && by.Active() {
				return graph.Qualify(planID, by.ID), true
			}
		}
		return "", false
	}
	for _, e := range c.Graph.Edges {
		if !isActive(c, e.From) {
			continue
		}
		by, ok := superseder(e.To)
		if !ok {
			continue
		}
		out = append(out, Issue{
			Path:    graph.EdgePath(e),
			Message: fmt.Sprintf("%s %s %s, which %s supersedes.", e.From, e.Type, e.To, by),
			Hint: "auto plan unlink " + c.Plan + " " + e.From + " " + e.Type + " " + e.To +
				", then auto plan link " + c.Plan + " " + e.From + " " + e.Type + " " + by,
		})
	}
	proseRefs(c, func(n graph.Node, path, field, ref string) {
		if by, ok := superseder(ref); ok {
			out = append(out, Issue{
				Path: path, Field: field,
				Message: fmt.Sprintf("%s's %s refers to [[%s]], which %s supersedes.", n.ID, field, ref, by),
				Hint:    "rewrite the reference as [[" + by + "]] with auto plan update " + c.Plan + " " + n.ID + " --" + flagFor(path) + " …",
			})
		}
	})
	return out
}

// railUndischarged reports each rail the plan honours (a `plan honors` edge,
// usually to an epic's rail) that no active AC of the plan discharges. An
// honors edge whose target does not resolve to a rail is left to dangling-ref
// and wrong-endpoint.
func railUndischarged(c *Context) []Issue {
	var out []Issue
	for _, e := range c.Graph.Edges {
		if e.From != schema.PlanNodeID || e.Type != edgeHonors {
			continue
		}
		if r, ok := c.Resolve(e.To); !ok || r.Type != "rail" {
			continue // dangling-ref or wrong-endpoint already
		}
		discharged := slices.ContainsFunc(c.Graph.Edges, func(d graph.Edge) bool {
			if d.Type != edgeDischarges || d.To != e.To {
				return false
			}
			n, ok := c.Node(d.From)
			return ok && n.Type == "ac" && n.Active()
		})
		if discharged {
			continue
		}
		out = append(out, Issue{
			Path:    graph.EdgePath(e),
			Message: fmt.Sprintf("Plan %s honours rail %s, but no AC discharges it.", c.Plan, e.To),
			Hint:    "auto plan link " + c.Plan + " <ac-id> discharges " + e.To + " (or add an AC for it with --discharges " + e.To + ")",
		})
	}
	return out
}

// railUnhonored reports each active rail of an epic that some child plan
// neither honours nor is excused from. A rail constrains every child, so each
// child plan in the family must honour it or be named in the rail's `deferred`
// list. Child plans are the epic's family: the plans its child nodes name and
// the plans that declare it as their epic. A rail of an epic with no child plans
// is reported unless something is deferred.
func railUnhonored(c *Context) []Issue {
	if !isEpic(c) {
		return nil
	}
	family := c.Set.Family(c.Plan)
	var out []Issue
	for _, r := range active(c, "rail") {
		deferred := stringItems(r.Fields["deferred"])
		ref := graph.Qualify(c.Plan, r.ID)
		var missing []string
		for _, p := range family {
			if !slices.Contains(deferred, p) && !planEdgeTo(c, p, edgeHonors, ref) {
				missing = append(missing, p)
			}
		}
		var msg string
		switch {
		case len(missing) > 0:
			msg = fmt.Sprintf("Rail %s %q is not honoured by child plan %s, which is not deferred.", r.ID, r.StringField("title"), strings.Join(missing, ", "))
		case len(family) == 0 && len(deferred) == 0:
			msg = fmt.Sprintf("Rail %s %q is honoured by no child plan (%s) and is not deferred.", r.ID, r.StringField("title"), familyLabel(family))
		default:
			continue
		}
		child := "<child>"
		if len(missing) > 0 {
			child = missing[0]
		}
		out = append(out, Issue{
			Path:    graph.NodePath(r.ID),
			Message: msg,
			Hint: "auto plan link " + child + " plan honors " + ref + ", or excuse the child for now with auto plan update " +
				c.Plan + " " + r.ID + " --deferred <NNN>",
		})
	}
	return out
}

// legUndelivered reports each active leg of an epic that no child plan
// delivers.
func legUndelivered(c *Context) []Issue {
	if !isEpic(c) {
		return nil
	}
	family := c.Set.Family(c.Plan)
	var out []Issue
	for _, l := range active(c, "leg") {
		ref := graph.Qualify(c.Plan, l.ID)
		if slices.ContainsFunc(family, func(p string) bool { return planEdgeTo(c, p, edgeDelivers, ref) }) {
			continue
		}
		out = append(out, Issue{
			Path: graph.NodePath(l.ID),
			Message: fmt.Sprintf("Leg %s %q is delivered by no child plan (%s).", l.ID,
				strings.TrimSpace(l.StringField("actor")+": "+l.StringField("action")), familyLabel(family)),
			Hint: "auto plan link <child> plan delivers " + ref + ", or auto plan retire " + c.Plan + " " + l.ID,
		})
	}
	return out
}

func familyLabel(family []string) string {
	if len(family) == 0 {
		return "the epic has no child plans"
	}
	return "children: " + strings.Join(family, ", ")
}

// stringItems reads a list field's string items.
func stringItems(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
	}
	return out
}
