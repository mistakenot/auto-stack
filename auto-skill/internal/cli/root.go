package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mistakenot/auto-shared/version"
	"github.com/mistakenot/auto-skill/internal/app"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/spf13/cobra"
)

const skillAuthoringGuide = `## Writing good skills

- **Description is the trigger surface.** Agents select skills based on the description alone — the body is not seen until after selection.
- **Use trigger phrases** in the description: "Use when", "Prefer for", "Do not use", "Trigger when", "Run when", "Run at", "Do not trigger".
- **Be specific.** "Use when deploying to GKE with Helm" beats "helps with deployment".
- **Keep the body procedural.** Lead with constraints, workflow steps, and required tools — not prose preamble.
- **Put bulk content in side files.** Use references/, scripts/, and assets/ for examples, templates, and heavy docs. The body should point to them.
- **Stay under token budgets.** Body under 4000 tokens, aggregate listing under 2000 tokens.
- **One skill, one job.** If two skills compete for the same prompt, merge them or make descriptions mutually exclusive.`

type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func Execute(ctx context.Context, stdout, stderr io.Writer) int {
	application := app.New(stdout, stderr)
	rootCmd := NewRootCmd(application)
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			if exitErr.Err != nil && exitErr.Err.Error() != "" {
				fmt.Fprintln(stderr, exitErr.Err)
			}
			return exitErr.Code
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

type envResolver func() (skill.Env, error)

func NewRootCmd(application *app.App) *cobra.Command {
	var rootFlag string

	resolveEnv := func() (skill.Env, error) {
		root, overridden, err := skill.ResolveRoot(application.CWD, rootFlag)
		if err != nil {
			return skill.Env{}, err
		}
		return skill.Env{Root: root, RootOverride: overridden}, nil
	}

	cmd := &cobra.Command{
		Use:           "skill",
		Short:         "Manage agent skills",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	cmd.Version = version.Version
	cmd.SetOut(application.Stdout)
	cmd.SetErr(application.Stderr)
	cmd.PersistentFlags().StringVar(&rootFlag, "root", "", "project root override for skills/ and .auto/")

	cmd.AddCommand(
		newAddCmd(resolveEnv),
		newInitCmd(resolveEnv),
		newCreateCmd(resolveEnv),
		newLintCmd(resolveEnv),
		newLsCmd(resolveEnv),
		newDoctorCmd(resolveEnv),
		newQuickstartCmd(),
		newDocsCmd(),
		newUpdateCmd(),
		newSyncCmd(resolveEnv),
		newCacheCmd(resolveEnv),
		newTrustCmd(resolveEnv),
	)

	return cmd
}

func newCreateCmd(resolveEnv envResolver) *cobra.Command {
	var description string
	var withDirs bool
	var textOutput bool

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new skill scaffold",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			result, err := skill.Create(env, skill.CreateOptions{
				Name:        args[0],
				Description: description,
				WithDirs:    withDirs,
			})
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if textOutput {
				fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", displayPath(result.SkillFile))
				for _, createdDir := range result.CreatedDirs {
					fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", displayPath(createdDir))
				}
			} else {
				createdDirs := make([]string, 0, len(result.CreatedDirs))
				for _, createdDir := range result.CreatedDirs {
					createdDirs = append(createdDirs, filepath.ToSlash(createdDir))
				}
				payload := map[string]any{
					"name":        args[0],
					"skillDir":    filepath.ToSlash(result.SkillDir),
					"skillFile":   filepath.ToSlash(result.SkillFile),
					"createdDirs": createdDirs,
				}
				data, err := skill.EncodeJSON(payload)
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				if _, err := cmd.OutOrStdout().Write(data); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			}

			for _, d := range result.Diagnostics {
				if d.Severity != skill.SeverityWarning && d.Severity != skill.SeverityError {
					continue
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "%s %s %s: %s\n", d.Severity, d.Code, d.Path, d.Message)
			}
			if skill.HasErrors(result.Diagnostics) {
				return &ExitError{Code: 1}
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "\n%s\n", skillAuthoringGuide)
			return nil
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "when to use this skill")
	cmd.Flags().BoolVar(&withDirs, "with-dirs", false, "create references/, scripts/, and assets/ subdirectories")
	cmd.Flags().BoolVar(&textOutput, "text", false, "emit human-readable text output")
	_ = cmd.MarkFlagRequired("description")
	return cmd
}

func newLintCmd(resolveEnv envResolver) *cobra.Command {
	var textOutput bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "lint [path]",
		Short: "Lint skills for schema and portability issues",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if textOutput && jsonOutput {
				return &ExitError{Code: 1, Err: errors.New("invalid flags: --text and --json cannot be combined")}
			}

			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			target := ""
			if len(args) == 1 {
				target = args[0]
			}

			diags, err := skill.Lint(env, target)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if textOutput {
				text := skill.FormatDiagnosticsText(diags)
				if _, err := cmd.OutOrStdout().Write([]byte(text)); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			} else {
				data, err := skill.EncodeJSON(diags)
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				if _, err := cmd.OutOrStdout().Write(data); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			}

			if skill.HasErrors(diags) {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&textOutput, "text", false, "emit human-readable diagnostic output")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON array output (default)")
	return cmd
}

func newLsCmd(resolveEnv envResolver) *cobra.Command {
	var textOutput bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List available skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			summaries, parseErrors, err := skill.List(env)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if textOutput && jsonOutput {
				return &ExitError{Code: 1, Err: errors.New("invalid flags: --text and --json cannot be combined")}
			}

			if textOutput {
				for _, summary := range summaries {
					fmt.Fprintf(cmd.OutOrStdout(), "- %s: %s\n", summary.Name, summary.Description)
				}
			} else {
				data, err := skill.EncodeJSON(summaries)
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				if _, err := cmd.OutOrStdout().Write(data); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			}

			for _, parseErr := range parseErrors {
				fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", parseErr)
			}
			if len(parseErrors) > 0 {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&textOutput, "text", false, "emit human-readable listing output")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON array output (default)")
	return cmd
}

func newDoctorCmd(resolveEnv envResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check auto skill configuration and project setup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			report, err := doctorReport(env)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			data, err := skill.EncodeJSON(report)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if ok, _ := report["ok"].(bool); !ok {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
	return cmd
}

func newQuickstartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quickstart",
		Short: "Show a quickstart workflow",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			quickstart := strings.Join([]string{
				"# auto skill quickstart",
				"",
				"```bash",
				"auto skill init",
				`auto skill create my-skill --description "Use when the user needs X. Prefer for Y."`,
				"auto skill lint",
				"auto skill ls --text",
				"```",
				"",
				skillAuthoringGuide,
			}, "\n")
			_, err := fmt.Fprintln(cmd.OutOrStdout(), quickstart)
			return err
		},
	}
}

func newDocsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docs",
		Short: "Show command reference",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			docs := strings.Join([]string{
				"# auto skill commands",
				"",
				"- `init`: initialize global (`auto skill init`) or project (`auto skill init --project`) settings.",
				"- `create <name> --description ...`: create a skill scaffold and lint it.",
				"- `lint [path]`: validate skills and emit structured JSON diagnostics.",
				"- `ls`: list skills in JSON (default) or text (`--text`).",
				"- `doctor`: verify setup and report issues in JSON.",
				"- `quickstart`: show a minimal happy-path workflow.",
			}, "\n")
			_, err := fmt.Fprintln(cmd.OutOrStdout(), docs)
			return err
		},
	}
}

func doctorReport(env skill.Env) (map[string]any, error) {
	checks := []map[string]any{}

	globalPath, err := env.GlobalSettingsPath()
	if err != nil {
		return nil, err
	}
	globalOK := fileExists(globalPath)
	checks = append(checks, map[string]any{
		"code":    "global_settings",
		"ok":      globalOK,
		"path":    filepath.ToSlash(globalPath),
		"message": boolMessage(globalOK, "global settings found", "global settings missing"),
		"hint":    "run auto skill init",
	})

	skillsYAMLPath := env.SkillsYAMLPath()
	skillsYAMLOK := fileExists(skillsYAMLPath)
	checks = append(checks, map[string]any{
		"code":    "skills_yaml",
		"ok":      skillsYAMLOK,
		"path":    filepath.ToSlash(skillsYAMLPath),
		"message": boolMessage(skillsYAMLOK, "skills.yaml found", "skills.yaml missing"),
		"hint":    "run auto skill init --project -y",
	})

	skillsPath := env.SkillsDir()
	skillsOK := dirExists(skillsPath)
	checks = append(checks, map[string]any{
		"code":    "skills_directory",
		"ok":      skillsOK,
		"path":    filepath.ToSlash(skillsPath),
		"message": boolMessage(skillsOK, "skills directory found", "skills directory missing"),
		"hint":    "run auto skill init --project",
	})

	ok := true
	for _, check := range checks {
		if v, _ := check["ok"].(bool); !v {
			ok = false
		}
	}
	slices.SortStableFunc(checks, func(a, b map[string]any) int {
		ac, _ := a["code"].(string)
		bc, _ := b["code"].(string)
		return strings.Compare(ac, bc)
	})

	return map[string]any{
		"ok":     ok,
		"checks": checks,
	}, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func boolMessage(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func displayPath(abs string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return skill.DisplayPath("", abs)
	}
	return skill.DisplayPath(cwd, abs)
}
