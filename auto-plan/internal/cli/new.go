package cli

import (
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/schema"
	"github.com/mistakenot/auto-plan/internal/workspace"
	"github.com/spf13/cobra"
)

var planKinds = []string{"task", "epic"}

func newNewCmd(application *app.App) *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "new <name> --kind task|epic",
		Short: "Create docs/plans/NNN-<name>/graph.json holding only the plan node",
		Long: `Create the next numbered plan folder (highest existing number + 1, or 001) with a
graph.json holding exactly the plan node at lifecycle "requirements".

The name must be kebab-case (` + schema.NamePattern + `).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNew(cmd, application, args[0], kind)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "plan kind: task or epic (required)")
	return cmd
}

type newResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"path"`
}

func runNew(cmd *cobra.Command, application *app.App, name, kind string) error {
	text := textMode(cmd)
	if err := workspace.ValidateName(name); err != nil {
		return failOne(cmd, text, "invalid-name", "args.name", "name", err.Error(), name,
			"use lowercase letters, digits and single hyphens, e.g. `auto plan new stage-briefs --kind task`")
	}
	if !slices.Contains(planKinds, kind) {
		return failOne(cmd, text, "invalid-kind", "flags.kind", "kind", "--kind must be one of "+strings.Join(planKinds, "|"), kind,
			"pass --kind task or --kind epic")
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
	p := workspace.Plan{ID: id, Name: name, Dir: workspace.PlansDir + "/" + id + "-" + name}

	g := graph.New(map[string]any{
		"name":      name,
		"kind":      kind,
		"lifecycle": string(schema.LifecycleRequirements),
		"created":   date,
	})
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

	res := newResult{ID: id, Name: name, Kind: kind, Path: p.Dir}
	return emit(cmd, text, res, func() string {
		return "created " + p.Dir + " (" + kind + ", requirements)\n"
	})
}
