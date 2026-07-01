package cli

import (
	"fmt"
	"strings"

	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/sync"
	"github.com/spf13/cobra"
)

// newUpdateCmd is the skills update verb (D-3 reclaim): `auto skill update` floats
// vendored skills to their newest upstream commits and APPLIES the result —
// rewriting the lock and re-rendering each target via the native sync engine's
// write path. The binary self-update this name used to run is now reachable ONLY
// at the root `auto update` command; no auto-shared/update call remains under skill.
func newUpdateCmd(resolveEnv envResolver, resolveTrace traceResolver) *cobra.Command {
	var (
		check  bool
		format string
	)

	cmd := &cobra.Command{
		Use:   "update [name...]",
		Short: "Update vendored skills to their latest upstream commits",
		Long: "Float vendored (locked) skills to the newest upstream commit for their " +
			"version spec, rewrite the lock, and re-render every target. With no names " +
			"all floating skills are updated; names scope the run. --check resolves and " +
			"reports what is stale without writing anything (offline).\n\n" +
			"Binary self-update lives at `auto update`, not here.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := resolveFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			for _, name := range args {
				if err := skill.ValidateSkillName(name); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			}
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			// A named update must target skills that actually exist (in the lock
			// or authored). Otherwise the scoped sync silently no-ops and reports
			// success, misleading the caller into thinking the skill was updated
			// (M14). Validate membership before running.
			if len(args) > 0 {
				desired, derr := sync.DesiredSet(env)
				if derr != nil {
					return &ExitError{Code: 1, Err: derr}
				}
				var missing []string
				for _, name := range args {
					if !desired[name] {
						missing = append(missing, name)
					}
				}
				if len(missing) > 0 {
					return &ExitError{Code: 1, Err: fmt.Errorf(
						"unknown skill(s): %s; run `auto skill list` to see managed skills",
						strings.Join(missing, ", "))}
				}
			}

			// Route through the native sync engine's apply path: AutoUpdate floats
			// floating specs, rewrites the lock, and re-renders (skipped under
			// --check, which is an offline stale report). This makes the verb
			// actually advance the project rather than only planning.
			result, runErr := sync.Run(env, sync.Options{
				Targets:    args,
				AutoUpdate: true,
				Check:      check,
				Trace:      resolveTrace(cmd),
			})
			if result == nil {
				result = &sync.Result{}
			}

			if mode == "text" {
				writeUpdateText(cmd, result)
			} else {
				data, encErr := skill.EncodeJSON(result)
				if encErr != nil {
					return &ExitError{Code: 1, Err: encErr}
				}
				if _, wErr := cmd.OutOrStdout().Write(data); wErr != nil {
					return &ExitError{Code: 1, Err: wErr}
				}
			}

			// Diagnostics on stderr; a planning/fetch/render error (recorded in the
			// result even when the Go error is nil) is a non-zero exit.
			for _, w := range result.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
			for _, e := range result.Errors {
				fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", e)
			}
			if runErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", runErr)
				return &ExitError{Code: 1}
			}
			if result.ExitCode() != 0 {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "offline: report what is stale without writing")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json (default) or text")
	return cmd
}

// writeUpdateText prints a human-readable summary that makes it unambiguous
// whether anything actually changed: which skills advanced (with short
// before→after commits), how many were already current, and whether the lock
// moved. The silent "no upstream change" case prints an explicit confirmation
// including the number of skills checked, so the user is never left guessing.
func writeUpdateText(cmd *cobra.Command, result *sync.Result) {
	out := cmd.OutOrStdout()
	moves := planMoves(result.Plan)
	active := planActive(result.Plan)

	// --check is an offline stale report: nothing is written.
	if result.Check {
		if len(moves) == 0 && len(result.Stale) == 0 {
			fmt.Fprintf(out, "All %d skill(s) are up to date — nothing to update.\n", active)
			return
		}
		if len(moves) > 0 {
			fmt.Fprintf(out, "%d of %d skill(s) would update:\n", len(moves), active)
			for _, m := range moves {
				fmt.Fprintf(out, "  ! %s  %s\n", m.Name, m.arrow())
			}
		}
		for i := range result.Stale {
			s := &result.Stale[i]
			fmt.Fprintf(out, "  ! %s needs re-render (%s)\n", staleLabel(s), s.Reason)
		}
		fmt.Fprintln(out, "Run `auto skill update` to apply.")
		return
	}

	if len(moves) == 0 {
		// No commit advanced. A forced re-render (rare) still touches files, so
		// distinguish that from a genuine no-op rather than claim nothing happened.
		if len(result.Written) > 0 {
			fmt.Fprintf(out, "No upstream changes; re-rendered %d file(s) to match the lock (%d skill(s) checked).\n",
				len(result.Written), active)
			return
		}
		fmt.Fprintf(out, "All %d skill(s) already up to date — nothing to update.\n", active)
		return
	}

	fmt.Fprintf(out, "Updated %d of %d skill(s):\n", len(moves), active)
	for _, m := range moves {
		fmt.Fprintf(out, "  ✓ %s  %s\n", m.Name, m.arrow())
	}
	if unchanged := active - len(moves); unchanged > 0 {
		fmt.Fprintf(out, "Unchanged: %d (already at latest).\n", unchanged)
	}
	if result.LockRewritten {
		fmt.Fprintln(out, "Lock file updated.")
	}
}
