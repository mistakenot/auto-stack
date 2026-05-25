package cli

import (
	"fmt"

	"github.com/mistakenot/auto-graph/internal/config"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize shared and autograph settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sharedPath, _, sharedCreated, err := config.EnsureSharedSettings()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			graphPath, _, graphCreated, err := config.EnsureGraphSettings()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Shared settings: %s\n", sharedPath)
			if sharedCreated {
				fmt.Fprintln(cmd.OutOrStdout(), "Created shared settings.json.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Shared settings.json already exists.")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Graph settings: %s\n", graphPath)
			if graphCreated {
				fmt.Fprintln(cmd.OutOrStdout(), "Created autograph settings.json.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Autograph settings.json already exists.")
			}
			return nil
		},
	}
}
