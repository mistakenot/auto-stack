package cli

import (
	"fmt"

	"github.com/mistakenot/auto-search/internal/config"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize shared and autosearch settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sharedPath, _, sharedCreated, err := config.EnsureSharedSettings()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			searchPath, _, searchCreated, err := config.EnsureSearchSettings()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Shared settings: %s\n", sharedPath)
			if sharedCreated {
				fmt.Fprintln(cmd.OutOrStdout(), "Created shared settings.json.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Shared settings.json already exists.")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Search settings: %s\n", searchPath)
			if searchCreated {
				fmt.Fprintln(cmd.OutOrStdout(), "Created autosearch settings.json.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Autosearch settings.json already exists.")
			}
			return nil
		},
	}
}
