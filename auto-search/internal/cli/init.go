package cli

import (
	"fmt"

	"github.com/mistakenot/auto-search/internal/config"
	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize shared and auto search settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			hostPath, _, hostCreated, err := sharedconfig.EnsureHost()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			sharedPath, _, sharedCreated, err := config.EnsureSharedSettings()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			searchPath, _, searchCreated, err := config.EnsureSearchSettings()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "Host config: %s\n", hostPath)
			if hostCreated {
				fmt.Fprintln(w, "Created host.json.")
			} else {
				fmt.Fprintln(w, "host.json already exists.")
			}
			fmt.Fprintf(w, "Shared settings: %s\n", sharedPath)
			if sharedCreated {
				fmt.Fprintln(w, "Created shared settings.json.")
			} else {
				fmt.Fprintln(w, "Shared settings.json already exists.")
			}
			fmt.Fprintf(w, "Search settings: %s\n", searchPath)
			if searchCreated {
				fmt.Fprintln(w, "Created auto search settings.json.")
			} else {
				fmt.Fprintln(w, "Autosearch settings.json already exists.")
			}
			return nil
		},
	}
}
