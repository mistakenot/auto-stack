package lint

import (
	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
)

// Rules is every lint rule, in reporting order. Structural codes
// (dangling-ref, bad-id, duplicate-id, wrong-endpoint, …) come from
// graph.Validate, the one shared validate(), and are merged in by Graph.
var Rules = []Rule{
	{Code: "goal-no-ac", Severity: SeverityError, MinLifecycle: schema.LifecycleSolution, Check: goalNoAC},
}

// goalNoAC reports every active goal that no active AC proves.
func goalNoAC(c *Context) []Issue {
	proven := map[string]bool{}
	for _, e := range c.Graph.Edges {
		if e.Type != "proves" {
			continue
		}
		if ac, ok := c.Graph.NodeByID(e.From); ok && ac.Active() {
			proven[e.To] = true
		}
	}
	var out []Issue
	for _, n := range c.Graph.Nodes {
		if n.Type != "goal" || !n.Active() || proven[n.ID] {
			continue
		}
		out = append(out, Issue{
			Path:    graph.NodePath(n.ID),
			Field:   "",
			Message: n.ID + " has no active AC",
			Hint:    "auto plan add " + c.Plan + " ac --proves " + n.ID + ` --title "…"`,
		})
	}
	return out
}
