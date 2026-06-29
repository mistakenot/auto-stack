package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/spf13/cobra"
)

func newInitCmd(resolveEnv envResolver) *cobra.Command {
	var (
		project      bool
		yes          bool
		targets      []string
		autoUpdate   bool
		noAutoUpdate bool
		defaultVer   string
		commitTgts   bool
		noCommitTgts bool
		textOutput   bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize auto skill settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if autoUpdate && noAutoUpdate {
				return &ExitError{Code: 1, Err: errors.New("invalid flags: --auto-update and --no-auto-update cannot be combined")}
			}
			if commitTgts && noCommitTgts {
				return &ExitError{Code: 1, Err: errors.New("invalid flags: --commit-targets and --no-commit-targets cannot be combined")}
			}

			hostPath, _, hostCreated, err := sharedconfig.EnsureHost()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if !project {
				return runGlobalInit(cmd, env, hostPath, hostCreated, textOutput)
			}

			if !yes && !isTerminal() {
				return &ExitError{Code: 1, Err: errors.New("not a TTY — pass -y or explicit flags (--target, --auto-update, --default-version, --commit-targets)")}
			}

			opts := skill.DefaultInitProjectOptions()

			if len(targets) > 0 {
				opts.Targets = targets
			}
			if noAutoUpdate {
				opts.AutoUpdate = false
			} else if autoUpdate {
				opts.AutoUpdate = true
			}
			if noCommitTgts {
				opts.CommitTargets = false
			} else if commitTgts {
				opts.CommitTargets = true
			}
			if defaultVer != "" {
				opts.DefaultVersion = defaultVer
			}

			result, initErr := skill.InitProject(env, opts)
			if initErr != nil {
				return &ExitError{Code: 1, Err: initErr}
			}

			if textOutput {
				fmt.Fprintf(cmd.OutOrStdout(), "Host config: %s\n", displayPath(hostPath))
				if hostCreated {
					fmt.Fprintln(cmd.OutOrStdout(), "  Created.")
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "  Already exists.")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "skills.yaml: %s\n", displayPath(result.SkillsYAMLPath))
				if result.SkillsYAMLCreated {
					fmt.Fprintln(cmd.OutOrStdout(), "  Created.")
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "  Already exists.")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "lock.json: %s\n", displayPath(result.LockPath))
				if result.LockCreated {
					fmt.Fprintln(cmd.OutOrStdout(), "  Created.")
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "  Already exists.")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Skills directory: %s\n", displayPath(result.SkillsDir))
				if result.SkillsDirCreated {
					fmt.Fprintln(cmd.OutOrStdout(), "  Created.")
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "  Already exists.")
				}
				if result.ProjectID != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Project ID: %s\n", result.ProjectID)
				}
				for _, f := range result.AgentFiles {
					fmt.Fprintf(cmd.OutOrStdout(), "Agent file updated: %s\n", f)
				}
				return nil
			}

			payload := map[string]any{
				"mode": "project",
				"host": map[string]any{
					"path":    filepath.ToSlash(hostPath),
					"created": hostCreated,
				},
				"skills_yaml": map[string]any{
					"path":    filepath.ToSlash(result.SkillsYAMLPath),
					"created": result.SkillsYAMLCreated,
				},
				"lock": map[string]any{
					"path":    filepath.ToSlash(result.LockPath),
					"created": result.LockCreated,
				},
				"skills_dir": map[string]any{
					"path":    filepath.ToSlash(result.SkillsDir),
					"created": result.SkillsDirCreated,
				},
				"project_id":          result.ProjectID,
				"agent_files_updated": result.AgentFiles,
			}
			return writeJSON(cmd, payload)
		},
	}

	cmd.Flags().BoolVar(&project, "project", false, "initialize project-local .auto/skills/ and skills.yaml")
	cmd.Flags().BoolVarP(&yes, "y", "y", false, "non-interactive mode using defaults")
	cmd.Flags().StringSliceVar(&targets, "target", nil, "output target styles (repeatable; default: claude,agents)")
	cmd.Flags().BoolVar(&autoUpdate, "auto-update", false, "enable auto-update (default)")
	cmd.Flags().BoolVar(&noAutoUpdate, "no-auto-update", false, "disable auto-update")
	cmd.Flags().StringVar(&defaultVer, "default-version", "", "default version spec (default: latest)")
	cmd.Flags().BoolVar(&commitTgts, "commit-targets", false, "commit target files (default)")
	cmd.Flags().BoolVar(&noCommitTgts, "no-commit-targets", false, "gitignore target files")
	cmd.Flags().BoolVar(&textOutput, "text", false, "emit human-readable text output")
	return cmd
}

func runGlobalInit(cmd *cobra.Command, env skill.Env, hostPath string, hostCreated, textOutput bool) error {
	result, err := skill.InitGlobal(env)
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}

	if textOutput {
		fmt.Fprintf(cmd.OutOrStdout(), "Host config: %s\n", displayPath(hostPath))
		if hostCreated {
			fmt.Fprintln(cmd.OutOrStdout(), "  Created.")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "  Already exists.")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Global settings: %s\n", displayPath(result.SettingsPath))
		if result.SettingsCreated {
			fmt.Fprintln(cmd.OutOrStdout(), "  Created.")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "  Already exists.")
		}
		for _, f := range result.AgentFiles {
			fmt.Fprintf(cmd.OutOrStdout(), "Agent file updated: %s\n", f)
		}
		return nil
	}

	payload := map[string]any{
		"mode": "global",
		"host": map[string]any{
			"path":    filepath.ToSlash(hostPath),
			"created": hostCreated,
		},
		"global": map[string]any{
			"path":    filepath.ToSlash(result.SettingsPath),
			"created": result.SettingsCreated,
		},
		"agent_files_updated": result.AgentFiles,
	}
	return writeJSON(cmd, payload)
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func writeJSON(cmd *cobra.Command, payload any) error {
	data, err := skill.EncodeJSON(payload)
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	if _, err := cmd.OutOrStdout().Write(data); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return nil
}
