// Package lint checks a plan graph: it runs structural validation and the lint
// rules that apply at the plan's lifecycle step, and merges both into one
// issues array.
package lint

import (
	"errors"
	"os"

	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
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
}

// File lints the graph.json at path. A file that cannot be read or parsed is
// reported as an issue, never as a Go error, so `lint all` covers every plan.
func File(planID, path string) Report {
	g, err := graph.Decode(path)
	if err != nil {
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
	return Graph(planID, g)
}

// Graph lints a decoded graph: every validation error, then every rule that
// applies at the plan's lifecycle.
func Graph(planID string, g *graph.Graph) Report {
	issues := []Issue{}
	for _, ve := range graph.Validate(g) {
		issues = append(issues, Issue{
			Code: ve.Code, Severity: SeverityError, Path: ve.Path, Field: ve.Field,
			Message: ve.Message, Hint: validationHint(planID, ve.Code),
		})
	}
	c := &Context{Plan: planID, Graph: g}
	lifecycle := Lifecycle(g)
	for _, r := range Rules {
		if !lifecycle.AtLeast(r.MinLifecycle) {
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

// validationHint is the remediation for a structural validation code.
func validationHint(plan, code string) string {
	lint := "`auto plan lint " + plan + "`"
	switch code {
	case graph.CodeDanglingRef:
		return "add the missing node, or point the edge at an existing ID in graph.json, then run " + lint
	case graph.CodeBadID:
		return "IDs are generated: restore the generated ID, or re-add the node with `auto plan add " + plan + " <type>`"
	case graph.CodeDuplicateID:
		return "give each node its own ID (re-add the duplicate with `auto plan add " + plan + " <type>`), then run " + lint
	case graph.CodeWrongEndpoint:
		return "use an edge type whose endpoints match (see `auto plan link --help`), then run " + lint
	case graph.CodeUnregisteredType, graph.CodeUnknownField:
		return "remove it from graph.json or use a registered type/field (see `auto plan add --help`), then run " + lint
	case graph.CodeMissingField, graph.CodeInvalidField:
		return "set the field to a valid value in graph.json, then run " + lint
	default:
		return "fix graph.json by hand, then run " + lint
	}
}
