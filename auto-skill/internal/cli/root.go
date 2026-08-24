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
	"github.com/mistakenot/auto-skill/internal/ownership"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/sync"
	"github.com/mistakenot/auto-skill/internal/trace"
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

type traceResolver func(*cobra.Command) *trace.Logger

func NewRootCmd(application *app.App) *cobra.Command {
	var rootFlag string
	var traceFlag bool

	resolveEnv := func() (skill.Env, error) {
		root, overridden, err := skill.ResolveRoot(application.CWD, rootFlag)
		if err != nil {
			return skill.Env{}, err
		}
		return skill.Env{Root: root, RootOverride: overridden}, nil
	}
	resolveTrace := func(cmd *cobra.Command) *trace.Logger {
		if !traceFlag {
			return nil
		}
		return trace.New(cmd.ErrOrStderr())
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
	cmd.PersistentFlags().BoolVar(&traceFlag, "trace", false, "emit detailed trace logs to stderr")

	cmd.AddCommand(
		newAddCmd(resolveEnv, resolveTrace),
		newInitCmd(resolveEnv),
		newCreateCmd(resolveEnv),
		newLintCmd(resolveEnv),
		newListCmd(resolveEnv),
		newDescribeCmd(resolveEnv),
		newGetCmd(resolveEnv),
		newSourceCmd(resolveEnv),
		newTargetCmd(resolveEnv),
		newDoctorCmd(resolveEnv),
		newQuickstartCmd(),
		newDocsCmd(),
		newUpdateCmd(resolveEnv, resolveTrace),
		newSyncCmd(resolveEnv, resolveTrace),
		newMigrateCmd(resolveEnv),
		newAdoptCmd(resolveEnv),
		newRemoveCmd(resolveEnv),
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

func newDoctorCmd(resolveEnv envResolver) *cobra.Command {
	var textOutput bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check auto skill configuration, ownership drift, and project setup",
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

			// Payload first: JSON (default) on stdout, or a human summary with --text.
			if textOutput {
				writeDoctorText(cmd.OutOrStdout(), report)
			} else {
				data, err := skill.EncodeJSON(report)
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				if _, err := cmd.OutOrStdout().Write(data); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			}

			if ok, _ := report["ok"].(bool); !ok {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&textOutput, "text", false, "emit a human-readable summary instead of JSON")
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
				"## Set up a target project",
				"",
				"```bash",
				"auto skill init --project -y",
				"auto skill target list --format text",
				"```",
				"",
				"## Add and render a vendored Skill",
				"",
				"Vendored Skills come from a remote or local source and are tracked in `.auto/skills/lock.json` plus `.auto/skills/skills.yaml`. `add` writes those files and runs a best-effort sync unless `--no-sync` is passed.",
				"",
				"If the Skill declares `customize:` variables, `add` may report `required_value_missing` and exit non-zero — see \"Customize a Skill with variables\" below.",
				"",
				"```bash",
				"auto skill add mistakenot/skills --skill handoff",
				"auto skill sync --check          # verify target copies without writing",
				"auto skill sync                  # render authored + vendored Skills into targets",
				"auto skill list --format text",
				"auto skill describe handoff --format text",
				"auto skill get handoff --format text",
				"```",
				"",
				"## Author a project-local Skill",
				"",
				"Authored Skills live under `./skills/<name>/` and render to the configured target directories on sync.",
				"",
				"```bash",
				`auto skill create my-skill --description "Use when the user needs X. Prefer for Y."`,
				"auto skill lint",
				"auto skill sync",
				"```",
				"",
				"## Customize a Skill with variables",
				"",
				"Skills are templates, not static files. The author declares `customize:` variables in SKILL.md frontmatter; the installing project supplies values in `.auto/skills/skills.yaml`. Values are substituted on `sync`, and the `customize:` block is stripped from the rendered output — the agent only ever sees the finished Skill.",
				"",
				"### Authoring: declare the variables",
				"",
				"Reference a variable in the body as `{{ .var_name }}` and declare it under `customize:`:",
				"",
				"````markdown",
				"---",
				"name: deploy-checklist",
				"description: Pre-deploy verification checklist. Use when shipping to staging.",
				"customize:",
				"  team_name:",
				"    required: true",
				`    description: "Team name for the checklist header"`,
				"  staging_url:",
				`    default: "https://staging.example.com"`,
				`    description: "Staging environment URL to verify"`,
				"  extra_checks:",
				`    default: ""`,
				`    description: "Additional checklist items (markdown)"`,
				"---",
				"",
				"# {{ .team_name }} Deploy Checklist",
				"",
				"1. All tests pass on the target branch",
				"2. Staging at {{ .staging_url }} matches expected behavior",
				"{{ .extra_checks }}",
				"````",
				"",
				"Resolution rules — pick the declaration that matches how badly you need a value:",
				"",
				"- `required: true` and no `default`: sync FAILS until the project supplies a value. Reserve it for values with no sensible fallback — it makes the Skill uninstallable without configuration.",
				"- `default:` present: used whenever the project supplies nothing. Prefer this; it keeps the Skill installable with zero configuration.",
				"- Neither `required` nor `default`: resolves to the empty string, silently. Good for optional append-points.",
				"- A supplied value always wins over a `default`.",
				"",
				"Authoring constraints:",
				"",
				"- Every `{{ .var }}` used in the body MUST be declared under `customize:`. An undeclared placeholder is a hard error (`undeclared_placeholder`) and blocks `add` outright.",
				"- The grammar is deliberately tiny: `{{ .var }}` field access only. No pipes, conditionals, loops, or function calls — those are rejected as non-grammar constructs. To emit a literal brace pair, write `{{ \"{{\" }}`.",
				"- Substitution is raw and unescaped; values are inserted verbatim.",
				"- Side files (`references/`, `scripts/`, `assets/`) are copied verbatim and are never templated. Only SKILL.md is rendered.",
				"- Write a `description:` for every variable. `auto skill add` does NOT list a Skill's variables, so that description is the only discovery surface an installer has.",
				"",
				"### Installing: supply the values",
				"",
				"`auto skill add` writes the lock entry plus a `skills.yaml` stub, then renders. It does not scaffold or list the Skill's variables — read the upstream SKILL.md `customize:` block to see what it accepts.",
				"",
				"```bash",
				"auto skill add https://github.com/org/skills --skill deploy-checklist",
				"```",
				"",
				"If the Skill declares a required variable with no default, `add` exits non-zero and names one missing variable. The lock entry and `skills.yaml` stub are still written — only the render failed, so nothing lands in the target directories yet:",
				"",
				"```text",
				"Added deploy-checklist (commit fddeb668debe)",
				`sync error: render deploy-checklist: required_value_missing: required customize var "team_name"`,
				"  has no value and no default; supply skills.<skill>.replacements.team_name in skills.yaml",
				"```",
				"",
				"Fill the values in `.auto/skills/skills.yaml`, then sync:",
				"",
				"```yaml",
				"shared:",
				"  replacements:          # applied to every Skill",
				`    team_name: "Platform Team"`,
				"",
				"skills:",
				"  deploy-checklist:",
				"    version: latest",
				"    replacements:        # per-Skill; overrides shared for the same var",
				`      staging_url: "https://staging.platform.internal"`,
				"      extra_checks: |",
				"        4. Database migrations reviewed by DBA",
				"        5. Feature flags verified in the flag console",
				"```",
				"",
				"```bash",
				"auto skill sync",
				"auto skill describe deploy-checklist --format text   # shows the resolved replacements",
				"```",
				"",
				"`replacements:` is a named map (`var: value`); the var name must match the `customize:` key exactly. Only ONE missing variable is reported per run, so if a Skill has several required variables, fill the one named, re-run `auto skill sync`, and repeat until it renders.",
				"",
				"Note: `auto skill doctor` does not check for unresolved variables — it reports `ok: true` for a Skill that is locked but failed to render. Trust the `sync` exit code, not `doctor`, to confirm a customized Skill actually landed.",
				"",
				"### File-ref replacements",
				"",
				"A value can inline a file (or one markdown section of it) instead of being written out in skills.yaml:",
				"",
				"```yaml",
				"skills:",
				"  deploy-checklist:",
				"    replacements:",
				"      extra_checks:",
				`        file: "references/extra-checks.md"   # relative to the Skill's own directory`,
				`        section: "Additional Checks"         # scalar or list; omit for the whole file`,
				"        include_heading: false               # default false",
				"        strip_frontmatter: true              # default true",
				"```",
				"",
				"`file:` is required and must name exactly one literal path — no globs, no interpolation. `section`, `include_heading` and `strip_frontmatter` are the only other accepted keys. The path resolves inside the Skill's own tree and may not escape it, including via symlinks, so today a file-ref reaches author-shipped content rather than files in the consuming project. The resolved content hash is recorded in the manifest, so editing the referenced file re-renders the Skill on the next sync.",
				"",
				"## Keep vendored Skills current",
				"",
				"`auto skill update` is for Skill updates. The binary self-update command is `auto update`.",
				"",
				"```bash",
				"auto skill update --check        # preview upstream changes for floating specs",
				"auto skill update                # update lock.json and render changed Skills",
				"auto skill sync --locked         # reproduce locked commits after checkout/merge",
				"auto update                      # update the auto binary itself",
				"```",
				"",
				"## Trace slow runs",
				"",
				"`--trace` writes detailed phase and git/cache timing logs to stderr while leaving JSON stdout parseable.",
				"",
				"```bash",
				"auto skill sync --trace",
				"auto skill update --trace",
				"auto skill add <source> --trace",
				"```",
				"",
				"## Handle renamed Skills",
				"",
				"Run a full `auto skill sync` after an authored rename. A scoped `--target` sync is intentionally conservative and will not prune unrelated old target copies.",
				"",
				"```bash",
				"# Authored Skill rename: source changed in this repository.",
				"git mv skills/old-name skills/new-name",
				"$EDITOR skills/new-name/SKILL.md  # set frontmatter: name: new-name",
				"auto skill sync                    # full sync prunes managed old target copies",
				"",
				"# Vendored Skill rename: upstream changed the Skill name or path.",
				"auto skill add <source> --skill new-name",
				"auto skill remove old-name --vendored",
				"auto skill doctor",
				"```",
				"",
				"Pruning is receipt-gated and journaled: auto-skill deletes only target copies it previously rendered and that still match the local receipt. Modified or foreign target copies are reported, not deleted.",
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
				"- `list [--local|--vendored]`: list authored and vendored skills with origin and a stale flag (JSON default, `--format text`).",
				"- `describe <name>`: show a skill's provenance (source, ref, commit, skill_version, replacements).",
				"- `get <name> [--target <style>]`: print the full rendered SKILL.md (`--format text` for raw markdown).",
				"- `source list | describe <id>`: inspect upstream sources from the lock.",
				"- `target list`: list configured output targets with their on-disk path and managed-skill count.",
				"- `add <source>`: add skills from a local or remote source.",
				"- `sync`: render authored + vendored skills into each target.",
				"- `update [name...]`: float vendored skills to their latest upstream commits.",
				"- `remove <name> [--local|--vendored]`: remove a skill source and prune managed rendered copies.",
				"- `doctor`: verify setup and report issues in JSON.",
				"- `quickstart`: show a minimal happy-path workflow.",
				"",
				"Use persistent `--trace` with slow commands (`add`, `sync`, `update`) to emit detailed timing logs on stderr without changing JSON stdout.",
				"",
				"Binary self-update is `auto update` (the root command), not `auto skill update`.",
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

	// ── ownership section (OFFLINE — no network) ──────────────────────────────
	//
	// We surface, alongside the config checks, the same on-disk-tree-digest vs
	// receipt/skill_version comparison the sync prune pass uses (AC-9). Every
	// derived list is built from ownership.Classify, the single deletion-authority
	// gate, so a forged in-file metadata.auto_skill stamp can never fool it.
	//
	// Tolerant of an un-initialized project: a missing manifest/receipts/skills.yaml
	// just yields empty lists (ScanOwnership returns empty inputs). A genuine read
	// error from DesiredSet/ScanOwnership is reported as a failing config check
	// rather than panicking, so doctor still emits a payload.
	ownershipSection := map[string]any{
		"managed_orphans": []map[string]any{},
		"foreign":         []map[string]any{},
		"modified":        []map[string]any{},
		"unestablished":   []map[string]any{},
	}
	staleItems := []map[string]any{}
	actionableDrift := false

	desired, derr := sync.DesiredSet(env)
	var inputs ownership.Inputs
	var serr error
	if derr == nil {
		inputs, serr = sync.ScanOwnership(env, desired)
	}
	if scanErr := firstErr(derr, serr); scanErr != nil {
		checks = append(checks, map[string]any{
			"code":    "ownership_scan",
			"ok":      false,
			"message": "ownership scan failed: " + scanErr.Error(),
			"hint":    "ensure skills.yaml and lock.json are readable",
		})
	} else {
		verdicts := ownership.Classify(inputs)
		managedOrphans := ownershipItems(ownership.PruneEligible(verdicts))
		foreign := ownershipItems(ownership.Adoptable(verdicts))
		modified := ownershipItems(verdictsWithState(verdicts, ownership.StateModified))
		unestablished := ownershipItems(verdictsWithState(verdicts, ownership.StateManagedUnestablished))

		staleRefs, _ := skill.CheckStaleSkillRefs(env)
		staleItems = diagItems(staleRefs)

		ownershipSection = map[string]any{
			"managed_orphans": managedOrphans,
			"foreign":         foreign,
			"modified":        modified,
			"unestablished":   unestablished,
		}

		// Actionable-drift rule: managed_orphans (will be pruned next sync),
		// modified (locally edited managed skill), unestablished (manifest row with
		// no local receipt), and stale_skill_refs each demand a fix, so any of them
		// flips ok=false. foreign/adoptable is informational only — it is listed but
		// never flips ok by itself (run `adopt` to act on it). Note: AC-9's scenario
		// always pairs a foreign dir with a managed orphan, so doctor exits non-zero
		// there regardless.
		actionableDrift = len(managedOrphans) > 0 ||
			len(modified) > 0 ||
			len(unestablished) > 0 ||
			len(staleItems) > 0
	}

	configOK := true
	for _, check := range checks {
		if v, _ := check["ok"].(bool); !v {
			configOK = false
		}
	}
	slices.SortStableFunc(checks, func(a, b map[string]any) int {
		ac, _ := a["code"].(string)
		bc, _ := b["code"].(string)
		return strings.Compare(ac, bc)
	})

	return map[string]any{
		"ok":               configOK && !actionableDrift,
		"checks":           checks,
		"ownership":        ownershipSection,
		"stale_skill_refs": staleItems,
	}, nil
}

// ownershipItems maps ownership verdicts to small, deterministically ordered JSON
// objects for the doctor report. Classify already sorts verdicts by (target,
// name) and the filters preserve that order, so the slices are stable.
func ownershipItems(verdicts []ownership.DirStatus) []map[string]any {
	items := make([]map[string]any, 0, len(verdicts))
	for _, v := range verdicts {
		items = append(items, map[string]any{
			"target":           v.Target,
			"name":             v.Name,
			"on_disk_digest":   v.OnDiskDigest,
			"expected_version": v.ExpectedVersion,
		})
	}
	return items
}

// verdictsWithState filters verdicts to a single ownership state (ownership only
// exports PruneEligible/Adoptable; doctor also reports modified/unestablished).
func verdictsWithState(verdicts []ownership.DirStatus, state ownership.State) []ownership.DirStatus {
	var out []ownership.DirStatus
	for _, v := range verdicts {
		if v.State == state {
			out = append(out, v)
		}
	}
	return out
}

// diagItems maps stale-skill-ref diagnostics to JSON objects for the report.
func diagItems(diags []skill.Diagnostic) []map[string]any {
	items := make([]map[string]any, 0, len(diags))
	for _, d := range diags {
		items = append(items, map[string]any{
			"severity": string(d.Severity),
			"code":     d.Code,
			"path":     filepath.ToSlash(d.Path),
			"field":    d.Field,
			"message":  d.Message,
		})
	}
	return items
}

// firstErr returns the first non-nil error of its arguments.
func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// writeDoctorText renders the doctor report as a human-readable summary, mirroring
// the JSON payload: config checks, then the ownership counts/lists, then stale
// skill references.
func writeDoctorText(w io.Writer, report map[string]any) {
	if ok, _ := report["ok"].(bool); ok {
		fmt.Fprintln(w, "doctor: ok")
	} else {
		fmt.Fprintln(w, "doctor: issues found")
	}

	if checks, ok := report["checks"].([]map[string]any); ok {
		for _, c := range checks {
			mark := "x"
			if cok, _ := c["ok"].(bool); cok {
				mark = "✓"
			}
			fmt.Fprintf(w, "  [%s] %v: %v\n", mark, c["code"], c["message"])
		}
	}

	if own, ok := report["ownership"].(map[string]any); ok {
		fmt.Fprintln(w, "ownership:")
		writeOwnershipGroup(w, "managed orphans (prune next sync)", own["managed_orphans"])
		writeOwnershipGroup(w, "foreign (adoptable)", own["foreign"])
		writeOwnershipGroup(w, "modified (locally edited)", own["modified"])
		writeOwnershipGroup(w, "unestablished (no local receipt)", own["unestablished"])
	}

	if refs, ok := report["stale_skill_refs"].([]map[string]any); ok {
		fmt.Fprintf(w, "stale skill refs: %d\n", len(refs))
		for _, r := range refs {
			fmt.Fprintf(w, "  ! %v: %v\n", r["field"], r["message"])
		}
	}
}

// writeOwnershipGroup prints one ownership category with its count and members.
func writeOwnershipGroup(w io.Writer, label string, v any) {
	items, _ := v.([]map[string]any)
	fmt.Fprintf(w, "  %s: %d\n", label, len(items))
	for _, it := range items {
		fmt.Fprintf(w, "    - %v/%v\n", it["target"], it["name"])
	}
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
