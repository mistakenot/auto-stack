package cli

import (
	"errors"
	"math/rand/v2"
	"os"
	"regexp"
	"slices"
	"strconv"
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

// planIDStream separates the seeded stream that draws plan IDs from the
// node-ID streams (which are keyed by a plan's node count).
const planIDStream = 1 << 40

func newNewCmd(application *app.App) *cobra.Command {
	var kind, epic string
	cmd := &cobra.Command{
		Use:   "new <name> --kind task|epic [--epic <plan>]",
		Short: "Create docs/plans/NNN-<name>/graph.json holding only the plan node",
		Long: `Create the next numbered plan folder (highest existing number + 1, or 001) with a
graph.json holding exactly the plan node at lifecycle "requirements", format version
` + schema.Version + `, and a new plan ID: the number, a hyphen and 4 random characters (004-k7q2).
Qualified references to the plan name that ID, so two branches that both create plan 004 still
get distinct plans; ` + "`auto plan renumber`" + ` moves one of them after the merge.

Prints {id, number, name, kind, epic, path, scaffolded}: id is the plan ID, number its folder
number. Like init, new first ensures docs/plans/AGENTS.md and its CLAUDE.md symlink; scaffolded
lists the files it created (absent when none).

The name must be kebab-case (` + schema.NamePattern + `). --epic (task plans only) records the
epic this plan belongs to in plan.fields.epic, as the epic's plan ID (a bare NNN is expanded);
the epic must exist and be kind epic. The epic lists its children itself: add a child node there
with auto plan add <epic> child --plan <new plan>.`,
		Example: "  auto plan new auto-mail-mvp --kind epic\n  auto plan new walking-skeleton --kind task --epic 001",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNew(cmd, application, args[0], kind, epic)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "plan kind: task or epic (required)")
	cmd.Flags().StringVar(&epic, "epic", "", "the epic plan this task plan belongs to (NNN or its plan ID)")
	return cmd
}

type newResult struct {
	// ID is the new plan's ID (`004-k7q2`).
	ID string `json:"id"`
	// Number is its folder number (`004`).
	Number string `json:"number"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Epic   string `json:"epic,omitempty"`
	Path   string `json:"path"`
	// Scaffolded lists the docs/plans files this run created (see init).
	Scaffolded []string `json:"scaffolded,omitempty"`
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
	if epic != "" && !planNumberRE.MatchString(epic) && !graph.PlanIDPattern.MatchString(epic) {
		return failOne(cmd, text, "invalid-field", "flags.epic", "epic", "--epic must be a plan number (NNN) or plan ID (NNN-xxxx)", epic,
			"pass the epic's number or ID, e.g. --epic 001")
	}
	date, err := today()
	if err != nil {
		return failOne(cmd, text, "invalid-env", "env."+EnvDate, EnvDate, err.Error(), nil, "unset "+EnvDate+" or set it to YYYY-MM-DD")
	}
	ws, err := openWorkspace(cmd, application, text)
	if err != nil {
		return err
	}
	number, err := ws.NextNumber()
	if err != nil {
		return failOne(cmd, text, "plan-number", "$", "", err.Error(), nil, "check "+workspace.PlansDir)
	}
	if epic != "" {
		set, err := ws.PlanSet()
		if err == nil {
			epic, err = set.ExpandPlan(epic)
		}
		if errors.Is(err, workspace.ErrAmbiguous) {
			return planArgFailure(cmd, text, "flags.epic", "epic", epic, err, "")
		}
		if err == nil {
			err = set.CheckEpic(epic)
		}
		if err != nil {
			return failOne(cmd, text, "epic-not-found", "flags.epic", "epic", err.Error(), epic,
				"name an existing epic plan (list plans with `auto plan list`; create one with `auto plan new <name> --kind epic`)")
		}
	}
	n, _ := strconv.Atoi(number)
	// Under a seed, each plan number draws from its own reproducible stream.
	r, err := seededRand(cmd, text, planIDStream+uint64(n)) //nolint:gosec // G115: n is 1..999
	if err != nil {
		return err
	}
	if r == nil {
		r = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) //nolint:gosec // G404: plan IDs are opaque labels, not secrets
	}
	id := graph.NewPlanID(number, r)
	p := workspace.Plan{Number: number, ID: id, Name: name, Dir: workspace.PlansDir + "/" + number + "-" + name}

	fields := map[string]any{
		"name":      name,
		"kind":      kind,
		"lifecycle": string(schema.LifecycleRequirements),
		"created":   date,
	}
	if epic != "" {
		fields["epic"] = epic
	}
	g := graph.New(id, fields)
	if errs := graph.Validate(g); len(errs) > 0 {
		return fail(cmd, text, errs, "report this: a new plan must always be valid")
	}

	scaffolded, err := ensureScaffold(cmd, ws, text)
	if err != nil {
		return err
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

	res := newResult{ID: id, Number: number, Name: name, Kind: kind, Epic: epic, Path: p.Dir, Scaffolded: scaffolded}
	return emit(cmd, text, res, func() string {
		if epic != "" {
			return "created plan " + id + " in " + p.Dir + " (" + kind + " in epic " + epic + ", requirements)\n" +
				"list it in the epic: auto plan add " + epic + " child --plan " + id + " --title \"…\"\n" +
				scaffoldedText(scaffolded)
		}
		return "created plan " + id + " in " + p.Dir + " (" + kind + ", requirements)\n" + scaffoldedText(scaffolded)
	})
}
