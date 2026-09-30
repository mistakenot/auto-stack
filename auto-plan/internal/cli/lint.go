package cli

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/lint"
	"github.com/mistakenot/auto-plan/internal/workspace"
	"github.com/spf13/cobra"
)

func newLintCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "lint <plan|all>",
		Short: "Validate a plan (or every plan) and report structured issues",
		Long: `Decode graph.json (only malformed JSON fails to load), validate it against the registry,
and run the lint rules that apply at the plan's lifecycle step.

One plan prints {plan, ok, issues:[{code,severity,path,field,message,hint}]}; "all" prints
{ok, plans:[…]}. Exit 1 on any error; warnings alone exit 0.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLint(cmd, application, args[0])
		},
	}
}

type lintAllResult struct {
	OK    bool          `json:"ok"`
	Plans []lint.Report `json:"plans"`
}

func runLint(cmd *cobra.Command, application *app.App, arg string) error {
	text := textMode(cmd)
	ws, err := openWorkspace(cmd, application, text)
	if err != nil {
		return err
	}
	plans, err := ws.Resolve(arg)
	if err != nil {
		return failOne(cmd, text, "plan-not-found", "args.plan", "plan", err.Error(), arg,
			"name a plan as NNN, NNN-name, a path, or all")
	}

	all := lintAllResult{OK: true, Plans: []lint.Report{}}
	for _, p := range plans {
		r := lint.File(p.ID, ws.GraphPath(p))
		all.OK = all.OK && r.OK
		all.Plans = append(all.Plans, r)
	}

	var v any = all
	if arg != workspace.All {
		v = all.Plans[0]
	}
	if err := emit(cmd, text, v, func() string { return lintText(all.Plans) }); err != nil {
		return err
	}
	if !all.OK {
		return &ExitError{Code: 1}
	}
	return nil
}

// lintText prints each plan's verdict first, then its issues with their
// remediation hints.
func lintText(reports []lint.Report) string {
	var b strings.Builder
	if len(reports) == 0 {
		b.WriteString("no plans under " + workspace.PlansDir + "\n")
	}
	for _, r := range reports {
		verdict := "ok"
		if !r.OK {
			verdict = "FAIL"
		}
		fmt.Fprintf(&b, "%s  %s  (%d issue%s)\n", r.Plan, verdict, len(r.Issues), plural(len(r.Issues)))
	}
	for _, r := range reports {
		for _, is := range r.Issues {
			fmt.Fprintf(&b, "\n%s %s %s %s\n  %s\n  hint: %s\n", r.Plan, is.Severity, is.Code, is.Path, is.Message, is.Hint)
		}
	}
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func newFmtCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fmt <plan|all>",
		Short: "Rewrite graph.json in canonical form (or --check that it already is)",
		Long: `Rewrite a plan's graph.json (or every plan's) in canonical form: nodes sorted by (type, id),
edges by (from, type, to), a fixed key order, a 2-space indent, no HTML escaping and a trailing
newline. Unknown keys are kept. fmt does not validate; run lint for that.

One plan prints {plan, path, canonical, changed}; "all" prints {ok, plans:[…]}. canonical says
whether the file was already canonical; changed says whether fmt rewrote it. With --check
nothing is written and the exit code is 1 when any file is not canonical. A file fmt cannot
rewrite without losing data (malformed JSON, or values of the wrong JSON type) is reported with
its errors and exits 1.`,
		Example: "  auto plan fmt 004\n  auto plan fmt all --check",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			check, _ := cmd.Flags().GetBool("check")
			return runFmt(cmd, application, args[0], check)
		},
	}
	cmd.Flags().Bool("check", false, "write nothing; exit 1 when a file is not canonical")
	return cmd
}

// fmtResult is one plan's fmt outcome.
type fmtResult struct {
	Plan      string                  `json:"plan"`
	Path      string                  `json:"path"`
	Canonical bool                    `json:"canonical"`
	Changed   bool                    `json:"changed"`
	Errors    []graph.ValidationError `json:"errors,omitempty"`
}

type fmtAllResult struct {
	OK    bool        `json:"ok"`
	Plans []fmtResult `json:"plans"`
}

func runFmt(cmd *cobra.Command, application *app.App, arg string, check bool) error {
	text := textMode(cmd)
	ws, err := openWorkspace(cmd, application, text)
	if err != nil {
		return err
	}
	plans, err := ws.Resolve(arg)
	if err != nil {
		return failOne(cmd, text, "plan-not-found", "args.plan", "plan", err.Error(), arg,
			"name a plan as NNN, NNN-name, a path, or all")
	}

	all := fmtAllResult{OK: true, Plans: []fmtResult{}}
	for _, p := range plans {
		r := fmtPlan(ws.GraphPath(p), check)
		r.Plan, r.Path = p.ID, p.Dir+"/"+workspace.GraphFile
		all.OK = all.OK && len(r.Errors) == 0 && (r.Canonical || !check)
		all.Plans = append(all.Plans, r)
	}

	var v any = all
	if arg != workspace.All {
		v = all.Plans[0]
	}
	if err := emit(cmd, text, v, func() string { return fmtText(all.Plans, check) }); err != nil {
		return err
	}
	if !all.OK {
		return &ExitError{Code: 1}
	}
	return nil
}

// fmtPlan canonicalises one graph.json, writing it unless check is set.
func fmtPlan(path string, check bool) fmtResult {
	var r fmtResult
	data, err := os.ReadFile(path)
	if err != nil {
		r.Errors = []graph.ValidationError{{Code: "read-failed", Path: "$", Message: err.Error()}}
		return r
	}
	g, err := graph.Parse(data)
	if err != nil {
		r.Errors = []graph.ValidationError{{Code: "parse-error", Path: "$", Message: err.Error()}}
		return r
	}
	if issues := g.DecodeIssues(); len(issues) > 0 {
		r.Errors = issues
		return r
	}
	canonical, err := graph.Encode(g)
	if err != nil {
		r.Errors = []graph.ValidationError{{Code: "encode-failed", Path: "$", Message: err.Error()}}
		return r
	}
	r.Canonical = bytes.Equal(data, canonical)
	if r.Canonical || check {
		return r
	}
	if err := graph.Save(path, g); err != nil {
		r.Errors = []graph.ValidationError{{Code: "write-failed", Path: "$", Message: err.Error()}}
		return r
	}
	r.Changed = true
	return r
}

// fmtText prints each plan's outcome first, then errors and remediation.
func fmtText(results []fmtResult, check bool) string {
	var b strings.Builder
	if len(results) == 0 {
		b.WriteString("no plans under " + workspace.PlansDir + "\n")
	}
	for _, r := range results {
		verdict := "canonical"
		switch {
		case len(r.Errors) > 0:
			verdict = "ERROR"
		case r.Changed:
			verdict = "rewritten"
		case !r.Canonical && check:
			verdict = "NOT canonical"
		}
		fmt.Fprintf(&b, "%s  %s  %s\n", r.Plan, verdict, r.Path)
	}
	for _, r := range results {
		for _, e := range r.Errors {
			fmt.Fprintf(&b, "\n%s error %s %s\n  %s\n", r.Plan, e.Code, e.Path, e.Message)
		}
		if len(r.Errors) > 0 {
			fmt.Fprintf(&b, "  hint: fix graph.json by hand (see `auto plan lint %s`), then rerun fmt\n", r.Plan)
		} else if !r.Canonical && check {
			fmt.Fprintf(&b, "\n%s hint: run `auto plan fmt %s` to rewrite it\n", r.Plan, r.Plan)
		}
	}
	return b.String()
}
