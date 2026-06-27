package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-skill/internal/migrate"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/spf13/cobra"
)

// newMigrateCmd is the parent `migrate` command. It does no work itself — only
// the nested `vercel` subcommand translates a vercel skills-lock.json into the
// native lock.json + skills.yaml model. Running `migrate` with no subcommand
// shows help (cobra default).
func newMigrateCmd(resolveEnv envResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate from other skill managers into the native model",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newMigrateVercelCmd(resolveEnv))
	return cmd
}

// newMigrateVercelCmd translates a vercel-style skills-lock.json into additive
// .auto/skills/lock.json + skills.yaml entries (state "unresolved") and authored
// imports under ./skills/. It is a pure file transform: it never resolves commits
// or touches the network — run `auto skill sync` afterwards to resolve + render.
func newMigrateVercelCmd(resolveEnv envResolver) *cobra.Command {
	var (
		from   string
		dryRun bool
		format string
	)

	cmd := &cobra.Command{
		Use:   "vercel",
		Short: "Migrate a vercel skills-lock.json into the native lock + skills.yaml",
		Long: "Translate a vercel-style skills-lock.json into additive .auto/skills/lock.json " +
			"and skills.yaml entries. Migrated deps are written in the \"unresolved\" state " +
			"(no commit); run `auto skill sync` afterwards to resolve commits and render. " +
			"github/gitlab sources become lock entries; a local git repo becomes a " +
			"non-portable local lock entry; a non-git local directory is imported into " +
			"./skills/ as an authored skill; unsupported source types are skipped and listed. " +
			"Migration is additive — it never modifies the source skills-lock.json or " +
			"overwrites existing entries.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode, err := resolveFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			projectRoot := env.Root

			fromPath := from
			if !filepath.IsAbs(fromPath) {
				fromPath = filepath.Join(projectRoot, fromPath)
			}

			f, err := os.Open(fromPath)
			if err != nil {
				emitDiagnostics(cmd, []config.ValidationError{{
					Code:    migrate.CodeParseError,
					Path:    filepath.ToSlash(fromPath),
					Field:   "from",
					Message: fmt.Sprintf("cannot open vercel skills-lock.json: %v; check the --from path or re-run `npx skills` to regenerate it", err),
				}})
				return &ExitError{Code: 1}
			}
			defer func() { _ = f.Close() }()

			v, parseErrs := migrate.ParseVercelLock(f)
			if len(parseErrs) > 0 {
				emitDiagnostics(cmd, parseErrs)
				return &ExitError{Code: 1}
			}

			plan, err := migrate.Plan(v, projectRoot)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			res, err := plan.Apply(dryRun)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			// Payload first: JSON (default) to stdout, or a one-line text summary.
			if mode == "text" {
				fmt.Fprintf(cmd.OutOrStdout(),
					"migrated %d deps, skipped %d (unsupported); run `auto skill sync` to resolve commits and render.\n",
					len(res.Migrated), len(res.Skipped))
			} else {
				payload := map[string]any{
					"migrated": res.Migrated,
					"skipped":  res.Skipped,
					"imported": res.Imported,
					"failed":   res.Failed,
					"dry_run":  dryRun,
					"counts": map[string]int{
						"migrated": len(res.Migrated),
						"skipped":  len(res.Skipped),
						"imported": len(res.Imported),
					},
				}
				data, err := skill.EncodeJSON(payload)
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				if _, err := cmd.OutOrStdout().Write(data); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			}

			// Per-entry warnings (skipped/unsupported/already-present) to stderr so
			// stdout stays a clean payload.
			for _, s := range res.Skipped {
				fmt.Fprintf(cmd.ErrOrStderr(), "skipped %s (%s): %s\n", s.Name, s.Reason, s.Message)
			}

			// Valid results were printed first; exit non-zero when any entry was
			// skipped (AC-4 valid-results-then-nonzero).
			if res.Failed {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&from, "from", "./skills-lock.json", "path to the vercel skills-lock.json to migrate")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report the plan without writing lock.json, skills.yaml, or imports")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json (default) or text")
	return cmd
}

// emitDiagnostics writes structured validation errors to stderr as a JSON array,
// keeping stdout free of error output (G-json-default, G-schema-strict).
func emitDiagnostics(cmd *cobra.Command, errs []config.ValidationError) {
	data, err := skill.EncodeJSON(errs)
	if err != nil {
		for _, e := range errs {
			fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", e.Message)
		}
		return
	}
	_, _ = cmd.ErrOrStderr().Write(data)
}
