package cli

import (
	"fmt"
	"os"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/config"
	"github.com/mistakenot/auto-reflect/internal/gitutil"
	"github.com/mistakenot/auto-reflect/internal/rules"
	"github.com/mistakenot/auto-reflect/internal/store"
	"github.com/spf13/cobra"
)

func newInitCmd(application *app.App) *cobra.Command {
	var projectOnly bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize auto reflect state (global + repository-local; use --project for repo-local only)",
		Long: "Initialize auto reflect state. By default this sets up global settings " +
			"(shared + reflect) and, when run inside a git repo, the repository-local " +
			"state (events dir + playbook). Pass --project to set up only the " +
			"repository-local state, following the cross-tool `init`/`init --project` convention.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !projectOnly {
				if err := initGlobalSettings(cmd); err != nil {
					return err
				}
			}

			repoInfo, err := gitutil.DetectRepo(application.CWD)
			if err != nil {
				if projectOnly {
					return &ExitError{Code: 1, Err: fmt.Errorf("init --project requires being inside a git repo: %w", err)}
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Project setup skipped: current directory is not inside a git repo.")
				return nil //nolint:nilerr // intentionally skip project setup outside git repositories
			}

			stateDir, err := store.EnsureStateDir(repoInfo.Root)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			playbookPath := store.PlaybookPath(repoInfo.Root)
			eventsDir := store.EventsDir(repoInfo.Root)

			if err := os.MkdirAll(eventsDir, 0o755); err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("create events directory: %w", err)}
			}
			playbookCreated, err := ensurePlaybook(playbookPath)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Project state dir: %s\n", store.DisplayPath(application.CWD, stateDir))
			fmt.Fprintf(cmd.OutOrStdout(), "Events dir: %s\n", store.DisplayPath(application.CWD, eventsDir))
			fmt.Fprintf(cmd.OutOrStdout(), "Playbook: %s\n", store.DisplayPath(application.CWD, playbookPath))
			if playbookCreated {
				fmt.Fprintln(cmd.OutOrStdout(), "Created playbook.json.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Playbook already exists.")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&projectOnly, "project", false, "set up only repository-local state (events dir + playbook), skipping global settings")
	return cmd
}

// initGlobalSettings creates the shared and reflect global settings files and
// reports their state. Used by plain `init` (not `init --project`).
func initGlobalSettings(cmd *cobra.Command) error {
	sharedPath, _, sharedCreated, err := config.EnsureSharedSettings()
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	reflectPath, _, reflectCreated, err := config.EnsureReflectSettings()
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Shared settings: %s\n", sharedPath)
	if sharedCreated {
		fmt.Fprintln(cmd.OutOrStdout(), "Created shared settings.json.")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Shared settings.json already exists.")
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Reflect settings: %s\n", reflectPath)
	if reflectCreated {
		fmt.Fprintln(cmd.OutOrStdout(), "Created reflect settings.json.")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Reflect settings.json already exists.")
	}
	return nil
}

func ensurePlaybook(playbookPath string) (bool, error) {
	if _, err := os.Stat(playbookPath); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat playbook: %w", err)
	}

	empty := rules.Playbook{
		SchemaVersion: rules.SchemaVersion,
		FoldedThrough: map[string]int{},
		Rules:         []rules.Rule{},
	}
	if err := store.WriteJSONFile(playbookPath, empty); err != nil {
		return false, fmt.Errorf("create playbook: %w", err)
	}
	return true, nil
}
