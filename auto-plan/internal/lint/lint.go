// Package lint checks a plan graph: it runs structural validation and the lint
// rules that apply at the plan's lifecycle step, and merges both into one
// issues array.
package lint

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
	"github.com/mistakenot/auto-plan/internal/workspace"
)

// Severity of an issue. Errors fail lint; warnings alone do not.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// CodeParseError reports malformed JSON: the only failure that stops a graph
// from loading.
const CodeParseError = "parse-error"

// Issue is one lint finding, with a remediation hint.
type Issue struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	Field    string   `json:"field"`
	Message  string   `json:"message"`
	Hint     string   `json:"hint"`
}

// Report is the lint result for one plan.
type Report struct {
	Plan   string  `json:"plan"`
	OK     bool    `json:"ok"`
	Issues []Issue `json:"issues"`
}

// Rule is one lint check. It applies once the plan's lifecycle reaches
// MinLifecycle (D-10), so partial plans stay valid.
type Rule struct {
	Code         string
	Severity     Severity
	MinLifecycle schema.Lifecycle
	Check        func(c *Context) []Issue
}

// Context is what a rule sees.
type Context struct {
	// Plan is the plan's number, used in hints.
	Plan  string
	Graph *graph.Graph
	// Lifecycle is the plan's lifecycle step (see Lifecycle).
	Lifecycle schema.Lifecycle
	// Set is every plan of the workspace, for resolving qualified
	// references and the cross-plan epic rules. It is nil when a graph is
	// linted alone: qualified references are then shape-checked only and
	// EpicRules do not run.
	Set *workspace.PlanSet

	byID map[string]graph.Node
}

// Resolve returns the node a reference names: a plan-local ID, or (with a
// Set) a qualified `NNN:ID` in another plan. ok is false when it does not
// resolve.
func (c *Context) Resolve(ref string) (graph.Node, bool) {
	if _, _, qualified := graph.ParseRef(ref); !qualified {
		return c.Node(ref)
	}
	if c.Set == nil {
		return graph.Node{}, false
	}
	n, err := c.Set.Lookup(ref)
	return n, err == nil
}

// Node returns the node with the given plan-local ID. When a (broken) graph
// repeats an ID, the first node wins, as in graph.Validate.
func (c *Context) Node(id string) (graph.Node, bool) {
	if c.byID == nil {
		c.byID = make(map[string]graph.Node, len(c.Graph.Nodes))
		for _, n := range c.Graph.Nodes {
			if _, dup := c.byID[n.ID]; !dup {
				c.byID[n.ID] = n
			}
		}
	}
	n, ok := c.byID[id]
	return n, ok
}

// File lints the graph.json at path on its own (no PlanSet). A file that
// cannot be read or parsed is reported as an issue, never as a Go error, so
// `lint all` covers every plan.
func File(planID, path string) Report {
	g, err := graph.Decode(path)
	if err != nil {
		return loadFailure(planID, path, err)
	}
	return Graph(planID, g)
}

// Plan lints plan planID of set: validation, the lifecycle rules, qualified
// references resolved against the set, and the cross-plan EpicRules.
func Plan(set *workspace.PlanSet, planID string) Report {
	g, err := set.Load(planID)
	if err != nil {
		return loadFailure(planID, set.Path(planID), err)
	}
	return InSet(set, planID, g)
}

func loadFailure(planID, path string, err error) Report {
	var pe *graph.ParseError
	issue := Issue{
		Code: CodeParseError, Severity: SeverityError, Path: "$", Message: err.Error(),
		Hint: "fix the JSON syntax in " + path + ", then run `auto plan lint " + planID + "`",
	}
	if !errors.As(err, &pe) {
		issue.Code = "unreadable"
		if errors.Is(err, os.ErrNotExist) {
			issue.Hint = "every plan folder needs a graph.json; recreate it with `auto plan new`"
		}
	}
	return finish(planID, []Issue{issue})
}

// Graph lints a decoded graph on its own: every validation error, then every
// rule that applies at the plan's lifecycle. Qualified references are
// shape-checked only.
func Graph(planID string, g *graph.Graph) Report { return InSet(nil, planID, g) }

// InSet lints a decoded graph that belongs to set (nil: lint it alone). With
// a set, each qualified edge target must resolve to a node of an allowed type
// (dangling-ref, wrong-endpoint), and EpicRules run after Rules.
func InSet(set *workspace.PlanSet, planID string, g *graph.Graph) Report {
	issues := []Issue{}
	c := &Context{Plan: planID, Graph: g, Lifecycle: Lifecycle(g), Set: set}
	structural := graph.Validate(g)
	if set != nil {
		for _, e := range g.Edges {
			structural = append(structural, set.CheckEdge(e)...)
		}
	}
	for _, ve := range structural {
		issues = append(issues, Issue{
			Code: ve.Code, Severity: SeverityError, Path: ve.Path, Field: ve.Field,
			Message: ve.Message, Hint: validationHint(c, ve),
		})
	}
	rules := Rules
	if set != nil {
		rules = append(slices.Clone(Rules), EpicRules...)
	}
	for _, r := range rules {
		if !c.Lifecycle.AtLeast(r.MinLifecycle) {
			continue
		}
		for _, is := range r.Check(c) {
			is.Code = r.Code
			is.Severity = r.Severity
			issues = append(issues, is)
		}
	}
	return finish(planID, issues)
}

func finish(planID string, issues []Issue) Report {
	ok := true
	for _, is := range issues {
		if is.Severity == SeverityError {
			ok = false
		}
	}
	return Report{Plan: planID, OK: ok, Issues: issues}
}

// Lifecycle returns the plan's lifecycle step. A missing or unknown value
// counts as requirements (validation reports the bad value itself).
func Lifecycle(g *graph.Graph) schema.Lifecycle {
	plan, _ := g.Plan()
	l := schema.Lifecycle(plan.StringField("lifecycle"))
	if l.Index() < 0 {
		return schema.LifecycleRequirements
	}
	return l
}

// validationHint is the remediation for a structural validation error. Where
// a CLI write fixes it (a write refuses only if its result still has errors),
// the hint is that exact command; otherwise it says what to edit by hand.
func validationHint(c *Context, ve graph.ValidationError) string {
	plan := c.Plan
	lint := "`auto plan lint " + plan + "`"
	switch ve.Code {
	case graph.CodeDanglingRef, graph.CodeWrongEndpoint:
		if from, typ, to, ok := edgeFromPath(ve.Path); ok {
			fix := "point it at an existing node of an allowed type"
			if ve.Code == graph.CodeWrongEndpoint {
				fix = "use an edge type whose endpoints match (see `auto plan link --help`)"
			}
			return fmt.Sprintf("remove the edge with `auto plan unlink %s %s %s %s`, or %s", plan, from, typ, to, fix)
		}
		return "fix the edge in graph.json, then run " + lint
	case graph.CodeBadID:
		return "IDs are generated: restore the generated ID, or re-add the node with `auto plan add " + plan + " <type>`"
	case graph.CodeDuplicateID:
		return "give each node its own ID (re-add the duplicate with `auto plan add " + plan + " <type>`), then run " + lint
	case graph.CodeUnregisteredType, graph.CodeUnknownField:
		return "remove it from graph.json or use a registered type/field (see `auto plan add --help`), then run " + lint
	case graph.CodeMissingField, graph.CodeInvalidField:
		if cmd := updateCommand(c, ve.Path); cmd != "" {
			return "set it with `" + cmd + "`"
		}
		return "set the field to a valid value in graph.json, then run " + lint
	default:
		return "fix graph.json by hand, then run " + lint
	}
}

var (
	nodeFieldPath = regexp.MustCompile(`^\$\.nodes\[([^\]]+)\]\.(fields\.[^\[]+|rank)`)
	edgePathRe    = regexp.MustCompile(`^\$\.edges\[(\S+) (\S+) (\S+)\]`)
)

// edgeFromPath recovers an edge's endpoints from its graph.EdgePath.
func edgeFromPath(path string) (from, typ, to string, ok bool) {
	m := edgePathRe.FindStringSubmatch(path)
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

// updateCommand is the `auto plan update|move` command that sets the field at
// path, or "" when no CLI write can (a Fixed field, an unregistered type).
func updateCommand(c *Context, path string) string {
	m := nodeFieldPath.FindStringSubmatch(path)
	if m == nil {
		return ""
	}
	id := m[1]
	if m[2] == "rank" {
		return "auto plan move " + c.Plan + " " + id + " --before|--after <sibling-id>"
	}
	n, ok := c.Node(id)
	if !ok {
		return ""
	}
	nt, ok := schema.Registry.Node(n.Type)
	if !ok {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(m[2], "fields."), ".")
	spec, ok := nt.Field(parts[0])
	if !ok || spec.Fixed {
		return ""
	}
	flag := parts[0]
	if len(parts) > 1 {
		flag += "-" + parts[1]
	}
	return "auto plan update " + c.Plan + " " + id + " --" + flag + " …"
}
