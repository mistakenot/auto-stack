package cli

import (
	"fmt"
	"strings"

	"github.com/mistakenot/auto-plan/internal/app"
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
