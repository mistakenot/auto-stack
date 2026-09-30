package cli

import (
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
	"github.com/mistakenot/auto-plan/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	planKinds    = []string{"task", "epic"}
	planNumberRE = regexp.MustCompile(schema.PlanNumberPattern)
)

func newNewCmd(application *app.App) *cobra.Command {
	var kind, epic string
	cmd := &cobra.Command{
		Use:   "new <name> --kind task|epic [--epic NNN]",
		Short: "Create docs/plans/NNN-<name>/graph.json holding only the plan node",
		Long: `Create the next numbered plan folder (highest existing number + 1, or 001) with a
graph.json holding exactly the plan node at lifecycle "requirements".

The name must be kebab-case (` + schema.NamePattern + `). --epic NNN (task plans only) records the
epic this plan belongs to in plan.fields.epic; the epic must exist and be kind epic. The epic
lists its children itself: add a child node there with auto plan add NNN child --plan <new NNN>.`,
		Example: "  auto plan new auto-mail-mvp --kind epic\n  auto plan new walking-skeleton --kind task --epic 001",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNew(cmd, application, args[0], kind, epic)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "plan kind: task or epic (required)")
	cmd.Flags().StringVar(&epic, "epic", "", "number (NNN) of the epic plan this task plan belongs to")
	return cmd
}

type newResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Epic string `json:"epic,omitempty"`
	Path string `json:"path"`
}

func runNew(cmd *cobra.Command, application *app.App, name, kind, epic string) error {
	text := textMode(cmd)
	if err := workspace.ValidateName(name); err != nil {
		return failOne(cmd, text, "invalid-name", "args.name", "name", err.Error(), name,
			"use lowercase letters, digits and single hyphens, e.g. `auto plan new stage-briefs --kind task`")
	}
	if !slices.Contains(planKinds, kind) {
		return failOne(cmd, text, "invalid-kind", "flags.kind", "kind", "--kind must be one of "+strings.Join(planKinds, "|"), kind,
			"pass --kind task or --kind epic")
	}
	epic = strings.TrimSpace(epic)
	if epic != "" && kind != "task" {
		return failOne(cmd, text, "usage", "flags.epic", "epic", "--epic applies to --kind task only", epic,
			"drop --epic, or create a task plan: `auto plan new "+name+" --kind task --epic "+epic+"`")
	}
	if epic != "" && !planNumberRE.MatchString(epic) {
		return failOne(cmd, text, "invalid-field", "flags.epic", "epic", "--epic must be a plan number (NNN)", epic,
			"pass the epic's 3-digit number, e.g. --epic 001")
	}
	date, err := today()
	if err != nil {
		return failOne(cmd, text, "invalid-env", "env."+EnvDate, EnvDate, err.Error(), nil, "unset "+EnvDate+" or set it to YYYY-MM-DD")
	}
	ws, err := openWorkspace(cmd, application, text)
	if err != nil {
		return err
	}
	id, err := ws.NextID()
	if err != nil {
		return failOne(cmd, text, "plan-number", "$", "", err.Error(), nil, "check "+workspace.PlansDir)
	}
	if epic != "" {
		set, err := ws.PlanSet()
		if err == nil {
			err = set.CheckEpic(epic)
		}
		if err != nil {
			return failOne(cmd, text, "epic-not-found", "flags.epic", "epic", err.Error(), epic,
				"name an existing epic plan (list plans with `auto plan list all`; create one with `auto plan new <name> --kind epic`)")
		}
	}
	p := workspace.Plan{ID: id, Name: name, Dir: workspace.PlansDir + "/" + id + "-" + name}

	fields := map[string]any{
		"name":      name,
		"kind":      kind,
		"lifecycle": string(schema.LifecycleRequirements),
		"created":   date,
	}
	if epic != "" {
		fields["epic"] = epic
	}
	g := graph.New(fields)
	if errs := graph.Validate(g); len(errs) > 0 {
		return fail(cmd, text, errs, "report this: a new plan must always be valid")
	}

	if err := os.MkdirAll(ws.PlansPath(), 0o755); err != nil {
		return failOne(cmd, text, "write-failed", "$", "", err.Error(), nil, "check that the repository is writable")
	}
	if err := os.Mkdir(ws.Abs(p.Dir), 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return failOne(cmd, text, "plan-exists", "args.name", "name", p.Dir+" already exists", p.Dir,
				"another plan took this number; rerun `auto plan new "+name+" --kind "+kind+"`")
		}
		return failOne(cmd, text, "write-failed", "$", "", err.Error(), p.Dir, "check that the repository is writable")
	}
	if err := graph.Save(ws.GraphPath(p), g); err != nil {
		_ = os.Remove(ws.Abs(p.Dir))
		return failOne(cmd, text, "write-failed", "$", "", err.Error(), p.Dir, "check that the repository is writable")
	}

	res := newResult{ID: id, Name: name, Kind: kind, Epic: epic, Path: p.Dir}
	return emit(cmd, text, res, func() string {
		if epic != "" {
			return "created " + p.Dir + " (" + kind + " in epic " + epic + ", requirements)\n" +
				"list it in the epic: auto plan add " + epic + " child --plan " + id + " --title \"…\"\n"
		}
		return "created " + p.Dir + " (" + kind + ", requirements)\n"
	})
}
