// Command auto is the unified entry point for the autonomous coding stack.
// Each tool is mounted as a subcommand (auto doc, auto search, auto etl, …).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	doccmd "github.com/datadyne-io/autodoc/rootcmd"
	configcmd "github.com/mistakenot/auto-config/rootcmd"
	envcmd "github.com/mistakenot/auto-env/rootcmd"
	etlcmd "github.com/mistakenot/auto-etl/rootcmd"
	graphcmd "github.com/mistakenot/auto-graph/rootcmd"
	reflectcmd "github.com/mistakenot/auto-reflect/rootcmd"
	searchcmd "github.com/mistakenot/auto-search/rootcmd"
	"github.com/mistakenot/auto-shared/update"
	"github.com/mistakenot/auto-shared/version"
	skillcmd "github.com/mistakenot/auto-skill/rootcmd"
	uicmd "github.com/mistakenot/auto-ui/rootcmd"
	watchcmd "github.com/mistakenot/auto-watch/rootcmd"
	"github.com/spf13/cobra"
)

func newRootCmd(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "auto",
		Short:         "Autonomous coding stack",
		Version:       version.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	root.AddCommand(
		configcmd.New(stdout, stderr),
		doccmd.New(stdout, stderr),
		envcmd.New(stdout, stderr),
		etlcmd.New(stdout, stderr),
		graphcmd.New(stdout, stderr),
		reflectcmd.New(stdout, stderr),
		searchcmd.New(stdout, stderr),
		skillcmd.New(stdout, stderr),
		uicmd.New(stdout, stderr),
		watchcmd.New(stdout, stderr),
	)
	root.AddCommand(newInitCmd())
	root.AddCommand(newHooksCmd())
	root.AddCommand(newUpdateCmd())

	return root
}

// newUpdateCmd is the sole binary self-update path for the merged binary. Some
// per-tool `auto <tool> update` subcommands are retained equivalents, but
// `auto skill update` is NOT — it is the skills update verb (it floats vendored
// skills), so binary self-update lives only here at the root.
func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Check for and install the latest auto-stack release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := update.Run(cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			data, err := json.Marshal(result)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
}

// exitCoder is implemented by subcommand errors that carry a specific process
// exit code (e.g. auto-reflect's *cli.ExitError). The dispatcher honors it so a
// subcommand's real exit code survives instead of collapsing to 1.
type exitCoder interface{ ExitCode() int }

// exitCodeFor returns the exit code an error should map to: a subcommand's
// declared non-zero code when available, otherwise 1.
func exitCodeFor(err error) int {
	var ec exitCoder
	if errors.As(err, &ec) {
		if code := ec.ExitCode(); code != 0 {
			return code
		}
	}
	return 1
}

func main() {
	root := newRootCmd(os.Stdout, os.Stderr)
	if err := root.ExecuteContext(context.Background()); err != nil {
		if msg := err.Error(); msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
		os.Exit(exitCodeFor(err))
	}
}
