package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	sharedconfig "github.com/mistakenot/auto-shared/config"
	sharedgit "github.com/mistakenot/auto-shared/git"
	"github.com/spf13/cobra"
)

// knownToolDirs are the per-tool subdirectories that may exist under a
// project's .auto/ folder. Used to record which tools a project uses.
var knownToolDirs = []string{"doc", "env", "etl", "graph", "reflect", "search", "skill", "ui", "watch"}

// newInitCmd is the stack-level initializer. `auto init` ensures the host-level
// config (~/.auto, host.json, the project registry). `auto init --project`
// additionally registers the current git repository in the registry so every
// auto tool — and the UI — knows the project exists.
func newInitCmd(stdout, stderr io.Writer) *cobra.Command {
	var project bool
	var projectID string
	var projectName string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize host-level auto config (and optionally register the current project)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, _, _, err := sharedconfig.EnsureHost(); err != nil {
				return err
			}
			registryPath, registry, _, err := sharedconfig.EnsureProjects()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Project registry: %s\n", registryPath)

			if !project {
				return nil
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			repoRoot, err := sharedgit.RepoRoot(cwd)
			if err != nil {
				return fmt.Errorf("--project requires a git repository: %w", err)
			}

			// Explicit --id is normalized only (bad input is caught by validation);
			// an auto-derived id is slugified so directory names always yield a valid id.
			id := sharedconfig.NormalizeID(projectID)
			if id == "" {
				id = sharedconfig.SlugifyID(filepath.Base(repoRoot))
			}
			name := projectName
			if name == "" {
				name = filepath.Base(repoRoot)
			}

			rawRemote, _ := sharedgit.OriginRemote(repoRoot)
			remote := sharedgit.NormalizeRemoteURL(rawRemote) // strips any embedded credentials

			// Guard: the same id must not be registered for a different path.
			for _, p := range registry.Projects {
				if p.ID == id && filepath.Clean(p.Path) != filepath.Clean(repoRoot) {
					return fmt.Errorf("project id %q is already registered for %s; rerun with --id <different-id>", id, p.Path)
				}
			}

			// Preserve the original registration time on re-registration. Match on
			// the exact path (not longest-prefix) so a nested repo never inherits a
			// parent project's timestamp; a blank value lets UpsertProject default it.
			var registeredAt string
			if existing := registry.FindProjectByExactPath(repoRoot); existing != nil {
				registeredAt = existing.RegisteredAt
			}

			sharedconfig.UpsertProject(&registry, sharedconfig.ProjectRef{
				ID:           id,
				Path:         repoRoot,
				Remote:       remote,
				Name:         name,
				Tools:        detectTools(repoRoot),
				RegisteredAt: registeredAt,
			})
			if errs := sharedconfig.ValidateProjects(registry); len(errs) > 0 {
				e := errs[0]
				return fmt.Errorf("project registry invalid (%s at %s, field %q): %s; fix the value or rerun with --id <valid-id>",
					e.Code, e.Path, e.Field, e.Message)
			}
			if err := sharedconfig.SaveProjects(registryPath, registry); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Registered project %s (%s)\n", id, repoRoot)
			return nil
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "register the current git repository in the project registry")
	cmd.Flags().StringVar(&projectID, "id", "", "project id to register (default: repo directory name)")
	cmd.Flags().StringVar(&projectName, "name", "", "human-readable project name (default: repo directory name)")
	return cmd
}

// detectTools returns the auto tools that have project-local config under
// <repoRoot>/.auto/<tool>/, recording which tools a project already uses.
func detectTools(repoRoot string) []string {
	tools := []string{}
	for _, t := range knownToolDirs {
		if info, err := os.Stat(filepath.Join(repoRoot, ".auto", t)); err == nil && info.IsDir() {
			tools = append(tools, t)
		}
	}
	if len(tools) == 0 {
		return nil
	}
	return tools
}
