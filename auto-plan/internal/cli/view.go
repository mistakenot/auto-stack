package cli

import (
	"fmt"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/render"
	"github.com/mistakenot/auto-plan/internal/tree"
	"github.com/mistakenot/auto-plan/internal/workspace"
	"github.com/spf13/cobra"
)

func newShowCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show <plan>",
		Short: "Show the goal ladder: goals, the ACs proving them, the decisions constraining them",
		Long: `Show a plan as a goal ladder in show-me notation: each ◎ goal in rank order, its ✓ ACs with
their verify commands, and the ◆ decisions that constrain the goal or its ACs, each followed by
its rejected alternatives on "-" lines. Blocks for unlinked nodes, rails, defects and open
questions follow. Retired nodes are left out.

Every row carries a positional label (G1, AC1.2, D3, A3.1, R1, DF1, Q1) for reading only:
labels follow the current rank order and are never accepted as a reference — use the ID.
The JSON form (default) carries the same facts as --text.`,
		Example: "  auto plan show 004 --text",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := textMode(cmd)
			p, g, err := loadForRead(cmd, application, text, args[0])
			if err != nil {
				return err
			}
			return emitView(cmd, text, render.Show(p.ID, g))
		},
	}
}

func newTraceCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trace <plan> <id> [--up|--down]",
		Short: "Trace a node's lineage up to the roots and down to the leaves",
		Long: `Walk the edges from a node and print the chain as an indented tree: up toward the roots
(an AC's goal, the decisions over it, the rails it discharges) and down toward the leaves (the
stages that cover an AC, the files those stages touch). Without --up or --down both walks print.

Each edge type walks one way when followed forward (from → to):

  up    proves, covers, discharges, about, in, honors, delivers, builds-on
  down  constrains, rejects, touches, addresses

and the other way when followed backward, so "down" from an AC follows covers backward to its
stages. wouldBreak, supersedes and dependsOn are not lineage and are never traced. Retired
neighbours are left out; a node met twice is printed once and then marked "(see above)"; a
qualified neighbour (NNN:ID) is printed as its raw reference.`,
		Example: "  auto plan trace 004 ac-3fxm --up\n  auto plan trace 004 g-k7q2 --down --text",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			up, _ := cmd.Flags().GetBool("up")
			down, _ := cmd.Flags().GetBool("down")
			return runTrace(cmd, application, args[0], args[1], up, down)
		},
	}
	cmd.Flags().Bool("up", false, "walk up toward the roots only")
	cmd.Flags().Bool("down", false, "walk down toward the leaves only")
	return cmd
}

func runTrace(cmd *cobra.Command, application *app.App, planArg, id string, up, down bool) error {
	text := textMode(cmd)
	dir := render.Both
	switch {
	case up && down:
		return failOne(cmd, text, "usage", "flags", "", "trace takes at most one of --up or --down", nil,
			"auto plan trace <plan> <id> --up (or --down, or neither for both walks)")
	case up:
		dir = render.Up
	case down:
		dir = render.Down
	}
	p, g, err := loadForRead(cmd, application, text, planArg)
	if err != nil {
		return err
	}
	v, ok := render.Trace(p.ID, g, id, dir)
	if !ok {
		return nodeNotFound(cmd, text, p, id)
	}
	return emitView(cmd, text, v)
}

func newTreeCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tree <plan> <id> | tree <plan> --files [--stage <id>]",
		Short: "Render a tree node in show-me notation, or derive the plan's file tree",
		Long: `With an ID, parse a tree node's body and render it in show-me notation: the + - ~ gutter, the
indentation or ├── branches, and the comments in one aligned column. Names that mention a node
ID of the plan get that node's glyph (◎ goal, ✓ ac, ◆ decision, ▶ stage, □ file, …). A body
with syntax errors still renders best effort; the errors go to stderr and the exit code is 1.

With --files, derive a ├── file tree from the plan's file nodes (add +, edit ~, delete -); it is
never authored. --stage narrows it to the files that stage touches.`,
		Example: "  auto plan tree 004 t-2kd9 --text\n  auto plan tree 004 --files --stage s-4nrd --text",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			files, _ := cmd.Flags().GetBool("files")
			stage, _ := cmd.Flags().GetString("stage")
			return runTree(cmd, application, args, files, stage)
		},
	}
	cmd.Flags().Bool("files", false, "derive the file tree from the plan's file nodes")
	cmd.Flags().String("stage", "", "with --files: only the files this stage touches")
	return cmd
}

func runTree(cmd *cobra.Command, application *app.App, args []string, files bool, stage string) error {
	text := textMode(cmd)
	const usageHint = "auto plan tree <plan> <tree-id>, or auto plan tree <plan> --files [--stage <stage-id>]"
	switch {
	case files && len(args) == 2:
		return failOne(cmd, text, "usage", "args", "", "tree takes a node ID or --files, not both", args[1], usageHint)
	case !files && len(args) == 1:
		return failOne(cmd, text, "usage", "args", "", "tree needs a tree node ID or --files", nil, usageHint)
	case !files && stage != "":
		return failOne(cmd, text, "usage", "flags", "stage", "--stage only applies with --files", stage, usageHint)
	}
	p, g, err := loadForRead(cmd, application, text, args[0])
	if err != nil {
		return err
	}
	if files {
		if stage != "" {
			if err := requireType(cmd, text, p, g, stage, "stage"); err != nil {
				return err
			}
		}
		return emitView(cmd, text, render.Files(p.ID, g, stage))
	}
	if err := requireType(cmd, text, p, g, args[1], "tree"); err != nil {
		return err
	}
	n, _ := g.NodeByID(args[1])
	v := render.TreeNode(p.ID, g, n)
	if err := emitView(cmd, text, v); err != nil {
		return err
	}
	if len(v.Errors) > 0 {
		return fail(cmd, text, treeErrors(n, v.Errors),
			"fix the body with auto plan update "+p.ID+" "+n.ID+" --body @file; `auto plan lint "+p.ID+"` reports the same tree-syntax errors")
	}
	return nil
}

func treeErrors(n graph.Node, errs []tree.Error) []graph.ValidationError {
	out := make([]graph.ValidationError, 0, len(errs))
	for _, e := range errs {
		out = append(out, graph.ValidationError{
			Code: "tree-syntax", Path: graph.NodePath(n.ID) + ".fields.body", Field: "body",
			Message: fmt.Sprintf("line %d: %s", e.Line, e.Message), Value: e.Line,
		})
	}
	return out
}

func newBriefCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "brief <plan> <stage-id>",
		Short: "Print a Stage Brief: everything one stage needs, nothing about the others",
		Long: `Print a Stage Brief for one stage: its steps and commit message, the stages it depends on (ID,
title and status only), the file tree it touches, the ACs it covers with their verify commands,
the decisions constraining those ACs or their goals, the rails those ACs discharge, and the
plan's open questions. Other stages appear only by ID, title and status.

JSON by default; --text prints Markdown.`,
		Example: "  auto plan brief 004 s-4nrd --text",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := textMode(cmd)
			p, g, err := loadForRead(cmd, application, text, args[0])
			if err != nil {
				return err
			}
			if err := requireType(cmd, text, p, g, args[1], "stage"); err != nil {
				return err
			}
			v, _ := render.Brief(p.ID, g, args[1])
			return emitView(cmd, text, v)
		},
	}
}

func emitView(cmd *cobra.Command, text bool, v render.View) error {
	return emit(cmd, text, v, v.Text)
}

// loadForRead resolves one plan and decodes its graph. Read commands work on
// a graph that fails validation (lint reports those problems); only malformed
// JSON stops them.
func loadForRead(cmd *cobra.Command, application *app.App, text bool, arg string) (workspace.Plan, *graph.Graph, error) {
	ws, err := openWorkspace(cmd, application, text)
	if err != nil {
		return workspace.Plan{}, nil, err
	}
	p, err := resolveOne(cmd, ws, text, arg)
	if err != nil {
		return workspace.Plan{}, nil, err
	}
	g, err := graph.Decode(ws.GraphPath(p))
	if err != nil {
		return workspace.Plan{}, nil, failOne(cmd, text, "parse-error", "$", "", err.Error(), nil,
			"fix graph.json by hand, then run `auto plan lint "+p.ID+"`")
	}
	return p, g, nil
}

// nodeNotFound reports an ID the plan does not hold.
func nodeNotFound(cmd *cobra.Command, text bool, p workspace.Plan, id string) error {
	return failOne(cmd, text, graph.CodeNodeNotFound, "args.id", "id", "plan "+p.ID+" has no node "+id, id,
		"IDs are plan-local; list them with `auto plan list "+p.ID+"` or find one with `auto plan search "+p.ID+" <text>`")
}

// requireType checks that id names a node of type typ.
func requireType(cmd *cobra.Command, text bool, p workspace.Plan, g *graph.Graph, id, typ string) error {
	n, ok := g.NodeByID(id)
	if !ok {
		return nodeNotFound(cmd, text, p, id)
	}
	if n.Type != typ {
		return failOne(cmd, text, "wrong-type", "args.id", "id", id+" is a "+n.Type+", not a "+typ, id,
			"list the plan's "+typ+" nodes with `auto plan list "+p.ID+" --type "+typ+"`")
	}
	return nil
}
