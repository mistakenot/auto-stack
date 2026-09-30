package cli

import (
	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/graph"
	"github.com/mistakenot/auto-plan/internal/render"
	"github.com/spf13/cobra"
)

func newShowCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "show <plan>",
		Short: "Show the goal ladder: goals, the ACs proving them, the decisions constraining them",
		Long: `Show a plan as a goal ladder in show-me notation: each ◎ goal, its ✓ ACs with their verify
commands, and the ◆ decisions that constrain the goal or its ACs. The JSON form (default)
carries the same facts as --text.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(cmd, application, args[0])
		},
	}
}

func runShow(cmd *cobra.Command, application *app.App, arg string) error {
	text := textMode(cmd)
	ws, err := openWorkspace(cmd, application, text)
	if err != nil {
		return err
	}
	p, err := resolveOne(cmd, ws, text, arg)
	if err != nil {
		return err
	}
	g, err := graph.Decode(ws.GraphPath(p))
	if err != nil {
		return failOne(cmd, text, "parse-error", "$", "", err.Error(), nil,
			"fix graph.json by hand, then run `auto plan lint "+p.ID+"`")
	}
	return emitView(cmd, text, render.Show(p.ID, g))
}

func emitView(cmd *cobra.Command, text bool, v render.View) error {
	return emit(cmd, text, v, v.Text)
}
