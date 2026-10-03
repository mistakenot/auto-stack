package lint

import (
	"fmt"
	"strings"

	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
	"github.com/mistakenot/auto-plan/internal/workspace"
)

// SetRules are the rules about a plan's place among the plans of its
// workspace. They run only when a plan is linted within its PlanSet, at every
// lifecycle step. Two branches that each create plan NNN merge cleanly in
// git (the folders differ by name) and collide only here.
var SetRules = []Rule{
	{Code: "duplicate-plan-number", Severity: SeverityError, MinLifecycle: schema.LifecycleRequirements, Check: duplicatePlanNumber},
	{Code: "duplicate-plan-id", Severity: SeverityError, MinLifecycle: schema.LifecycleRequirements, Check: duplicatePlanID},
}

// duplicatePlanNumber reports a plan whose folder number another folder
// shares, naming every plan ID involved.
func duplicatePlanNumber(c *Context) []Issue {
	if c.Number == "" {
		return nil
	}
	same := c.Set.ByNumber(c.Number)
	if len(same) < 2 {
		return nil
	}
	return []Issue{{
		Path: "$.id", Field: "id",
		Message: fmt.Sprintf("Plan number %s is used by %d plans: %s. Each plan needs its own number.",
			c.Number, len(same), planList(same)),
		Hint: "auto plan renumber " + c.Plan + " (moves it to the next free number and rewrites every reference to it)",
	}}
}

// duplicatePlanID reports a plan whose graph.json records the same plan ID
// as another folder's (a copied folder).
func duplicatePlanID(c *Context) []Issue {
	if !graph.PlanIDPattern.MatchString(c.Graph.ID) {
		return nil
	}
	same := c.Set.WithID(c.Graph.ID)
	if len(same) < 2 {
		return nil
	}
	return []Issue{{
		Path: "$.id", Field: "id",
		Message: fmt.Sprintf("Plan ID %s is recorded by %d folders: %s. A plan ID names one plan.",
			c.Graph.ID, len(same), planList(same)),
		Hint: "a copied folder keeps the original's ID: remove the copy, or give it a fresh ID with `auto plan new` and move its content",
	}}
}

func planList(plans []workspace.Plan) string {
	names := make([]string, len(plans))
	for i, p := range plans {
		names[i] = p.Ref() + " (" + p.Dir + ")"
	}
	return strings.Join(names, ", ")
}
