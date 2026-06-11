package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-watch/internal/app"
	"github.com/mistakenot/auto-watch/internal/config"
	"github.com/mistakenot/auto-watch/internal/gitx"
	"github.com/mistakenot/auto-watch/internal/model"
	"github.com/spf13/cobra"
)

func newInitCmd(application *app.App) *cobra.Command {
	var projectID string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize global and project-local autowatch config",
		RunE: func(cmd *cobra.Command, args []string) error {
			settingsPath, globalCfg, globalCreated, err := config.EnsureGlobalConfig()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			hostPath, _, hostCreated, err := config.EnsureHostFile()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Project registry: %s\n", settingsPath)
			if globalCreated {
				fmt.Fprintln(cmd.OutOrStdout(), "Created ~/.auto/projects.json.")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Host file: %s\n", hostPath)
			if hostCreated {
				fmt.Fprintln(cmd.OutOrStdout(), "Created host.json.")
			}

			repoRoot, err := gitx.FindRepoRoot(application.CWD)
			if err != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "Project setup skipped: current directory is not inside a git repo.")
				return nil //nolint:nilerr // intentionally skip project setup when not in a git repo
			}

			resolvedProjectID := config.NormalizeID(projectID)
			if resolvedProjectID == "" {
				resolvedProjectID = config.NormalizeID(filepath.Base(repoRoot))
			}

			projectCfgPath := config.ProjectConfigPath(repoRoot)
			if _, err := os.Stat(projectCfgPath); err == nil {
				projectCfg, err := config.LoadProjectConfig(repoRoot)
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				resolvedProjectID = projectCfg.ID
			}

			for _, project := range globalCfg.Projects {
				if project.ID == resolvedProjectID && filepath.Clean(project.Path) != filepath.Clean(repoRoot) {
					return &ExitError{
						Code: 1,
						Err:  fmt.Errorf("duplicate_project_id: project id %q is already registered for %s; rerun with --project-id <different-id>", resolvedProjectID, project.Path),
					}
				}
			}

			projectCfg, created, err := config.EnsureProjectConfig(repoRoot, resolvedProjectID)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if _, _, err := config.EnsureWorktreeIgnore(repoRoot); err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			remote, err := gitx.OriginRemote(repoRoot)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			config.UpsertProjectRef(&globalCfg, &model.ProjectRef{
				ID:     projectCfg.ID,
				Path:   repoRoot,
				Remote: remote,
			})
			if errs := config.ValidateGlobalConfig(globalCfg); len(errs) > 0 {
				return &ExitError{Code: 1, Err: fmt.Errorf("global settings are invalid: %s", errs[0].Message)}
			}
			if err := config.SaveGlobalConfig(settingsPath, globalCfg); err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Project config: %s\n", projectCfgPath)
			if created {
				fmt.Fprintln(cmd.OutOrStdout(), "Created project.json.")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Registered project %s (%s)\n", projectCfg.ID, repoRoot)
			return nil
		},
	}
	cmd.Flags().StringVar(&projectID, "project-id", "", "project id to register")
	return cmd
}
