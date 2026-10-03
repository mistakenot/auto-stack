package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mistakenot/auto-plan/internal/app"
	"github.com/mistakenot/auto-plan/internal/workspace"
	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/spf13/cobra"
)

func newInitCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create .auto/plan/plans/ with its AGENTS.md and CLAUDE.md (idempotent)",
		Long: `Create .auto/plan/plans/ in this repository, plus two agent memory files when they are missing:
.auto/plan/plans/AGENTS.md (the folders are plans managed by auto plan; use the CLI, not hand edits)
and .auto/plan/plans/CLAUDE.md, a relative symlink to AGENTS.md (a copy where symlinks fail).
Existing files are never overwritten; a CLAUDE.md that is not a symlink to AGENTS.md is left
as is, with a note on stderr. ` + "`auto plan new`" + ` ensures the same files.

Prints {root, created, scaffolded}: created is true when .auto/plan/plans/ was new, scaffolded lists
the files this run created (absent when none).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, application)
		},
	}
}

type initResult struct {
	Root    string `json:"root"`
	Created bool   `json:"created"`
	// Scaffolded lists the .auto/plan/plans files this run created.
	Scaffolded []string `json:"scaffolded,omitempty"`
}

func runInit(cmd *cobra.Command, application *app.App) error {
	text := textMode(cmd)
	// Host identity is shared machine state every tool's init establishes.
	if _, _, _, err := sharedconfig.EnsureHost(); err != nil {
		return failOne(cmd, text, "host-config", "$", "", err.Error(), nil, "check that ~/.auto is writable")
	}
	ws, err := openWorkspace(cmd, application, text)
	if err != nil {
		return err
	}
	_, statErr := os.Stat(ws.PlansPath())
	created := errors.Is(statErr, os.ErrNotExist)
	scaffolded, err := ensureScaffold(cmd, ws, text)
	if err != nil {
		return err
	}
	res := initResult{Root: workspace.PlansDir, Created: created, Scaffolded: scaffolded}
	return emit(cmd, text, res, func() string {
		out := workspace.PlansDir + " already exists\n"
		if created {
			out = "created " + workspace.PlansDir + "\n"
		}
		return out + scaffoldedText(scaffolded)
	})
}

// ensureScaffold creates .auto/plan/plans/ and its missing AGENTS.md / CLAUDE.md
// (workspace.EnsureScaffold) for init and new, printing its notes on stderr.
func ensureScaffold(cmd *cobra.Command, ws *workspace.Workspace, text bool) ([]string, error) {
	created, notes, err := ws.EnsureScaffold()
	if err != nil {
		return nil, failOne(cmd, text, "write-failed", "$", "", err.Error(), workspace.PlansDir, "check that the repository is writable")
	}
	for _, n := range notes {
		fmt.Fprintln(cmd.ErrOrStderr(), "auto plan: "+n)
	}
	return created, nil
}

// scaffoldedText is the --text line per scaffolded file.
func scaffoldedText(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		b.WriteString("created " + p + "\n")
	}
	return b.String()
}
