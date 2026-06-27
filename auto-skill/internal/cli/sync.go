package cli

import (
	"fmt"
	"io"

	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/sync"
	"github.com/spf13/cobra"
)

// newSyncCmd renders the union of authored (./skills) and vendored (locked)
// skills into every configured output target using the native sync engine. The
// former Node-based shell-out is gone entirely — there is no longer any
// dependency on an external toolchain.
func newSyncCmd(resolveEnv envResolver) *cobra.Command {
	var (
		check      bool
		locked     bool
		noUpdate   bool
		targets    []string
		jobs       int
		textOutput bool
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Render local and vendored skills into agent configurations",
		Long: "Render the union of authored ./skills and vendored (locked) skills into " +
			"each configured target (e.g. .claude/skills, .agents/skills) using the native " +
			"sync engine.\n\n" +
			"By default floating specs (latest/branch:) float to HEAD per skills.yaml " +
			"auto_update, then render. --locked / --no-update reproduce the locked commit " +
			"without floating. --target scopes the run to named skills and implies --locked " +
			"(a scoped sync never advances the project-wide lock). --check is an offline " +
			"dry-run gate that writes nothing and exits non-zero when any target is stale.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			// --no-update is an alias/equivalent of --locked: both reproduce the
			// locked commit without floating.
			lockedEffective := locked || noUpdate

			opts := sync.Options{
				Check:    check,
				Locked:   lockedEffective,
				NoUpdate: noUpdate,
				Targets:  targets,
				Jobs:     jobs,
				// AutoUpdate (float-then-render) is driven by skills.yaml's
				// auto_update (default true, written by `init`); the engine ORs
				// opts.AutoUpdate with the parsed value. We deliberately do NOT
				// force it true here so that a project with auto_update:false
				// reproduces its locked commit (AC-11 precedence: --locked >
				// auto_update). --locked / --no-update / --target suppress
				// floating regardless.
			}

			result, runErr := sync.Run(env, opts)
			if result == nil {
				result = &sync.Result{}
			}

			// Emit the payload first. JSON (default): stdout carries only the
			// strictly parseable Result. Text: a human summary on stdout.
			if textOutput {
				writeSyncText(cmd.OutOrStdout(), result)
			} else {
				data, encErr := skill.EncodeJSON(result)
				if encErr != nil {
					return &ExitError{Code: 1, Err: encErr}
				}
				if _, wErr := cmd.OutOrStdout().Write(data); wErr != nil {
					return &ExitError{Code: 1, Err: wErr}
				}
			}

			// Diagnostics / advisory warnings / errors go to stderr so stdout
			// stays a clean payload. Token-budget overflow is an advisory
			// warning only — it never changes the exit code.
			for _, w := range result.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
			for _, e := range result.Errors {
				fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", e)
			}

			// Orchestration-level failure (unreadable config, recovery/commit
			// failure): print and exit non-zero.
			if runErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", runErr)
				return &ExitError{Code: 1}
			}

			// Per-skill/per-repo failures and a stale --check gate exit non-zero
			// (diagnostics already on stderr above).
			if result.ExitCode() != 0 {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "offline dry-run gate: plan only, write nothing, exit non-zero if any target is stale")
	cmd.Flags().BoolVar(&locked, "locked", false, "reproduce the locked commit without floating")
	cmd.Flags().BoolVar(&noUpdate, "no-update", false, "do not advance floating specs this run (alias of --locked)")
	cmd.Flags().StringArrayVar(&targets, "target", nil, "restrict to these skill names (repeatable; implies --locked)")
	cmd.Flags().IntVar(&jobs, "jobs", sync.DefaultJobs, "fetch + render worker-pool size")
	cmd.Flags().BoolVar(&textOutput, "text", false, "emit a human-readable summary instead of JSON")

	return cmd
}

// writeSyncText prints a compact human-readable summary of a sync result.
func writeSyncText(w io.Writer, r *sync.Result) {
	fmt.Fprintf(w, "mode: %s\n", r.Mode)
	if r.Recovered {
		fmt.Fprintln(w, "recovered a pending sync journal")
	}
	if r.Check {
		if len(r.Stale) == 0 {
			fmt.Fprintln(w, "up to date")
		} else {
			fmt.Fprintf(w, "stale: %d\n", len(r.Stale))
			for _, s := range r.Stale {
				if s.Target != "" {
					fmt.Fprintf(w, "  ! %s/%s (%s)\n", s.Target, s.Skill, s.Reason)
				} else {
					fmt.Fprintf(w, "  ! %s (%s)\n", s.Skill, s.Reason)
				}
			}
		}
		return
	}
	if len(r.Written) > 0 {
		fmt.Fprintf(w, "written: %d\n", len(r.Written))
		for _, p := range r.Written {
			fmt.Fprintf(w, "  + %s\n", p)
		}
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(w, "skipped (already up to date): %d\n", len(r.Skipped))
	}
	if r.ManifestWritten {
		fmt.Fprintln(w, "manifest written")
	}
	if r.LockRewritten {
		fmt.Fprintln(w, "lock rewritten")
	}
	if len(r.Written) == 0 && len(r.Skipped) == 0 {
		fmt.Fprintln(w, "no skills to render")
	}
}
