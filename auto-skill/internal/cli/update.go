package cli

import (
	"fmt"

	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/sync"
	"github.com/spf13/cobra"
)

// newUpdateCmd is the skills update verb (D-3 reclaim): `auto skill update` floats
// vendored skills to their newest upstream commits and APPLIES the result —
// rewriting the lock and re-rendering each target via the native sync engine's
// write path. The binary self-update this name used to run is now reachable ONLY
// at the root `auto update` command; no auto-shared/update call remains under skill.
func newUpdateCmd(resolveEnv envResolver) *cobra.Command {
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

			// Route through the native sync engine's apply path: AutoUpdate floats
			// floating specs, rewrites the lock, and re-renders (skipped under
			// --check, which is an offline stale report). This makes the verb
			// actually advance the project rather than only planning.
			result, runErr := sync.Run(env, sync.Options{
				Targets:    args,
				AutoUpdate: true,
				Check:      check,
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

func writeUpdateText(cmd *cobra.Command, result *sync.Result) {
	out := cmd.OutOrStdout()
	if result.Check {
		if len(result.Stale) == 0 {
			fmt.Fprintln(out, "All skills are up to date.")
			return
		}
		for i := range result.Stale {
			s := &result.Stale[i]
			fmt.Fprintf(out, "- %s (%s): %s\n", s.Skill, s.Target, s.Reason)
		}
		return
	}
	if len(result.Written) == 0 {
		fmt.Fprintln(out, "All skills are up to date.")
		return
	}
	for _, w := range result.Written {
		fmt.Fprintf(out, "- updated %s\n", w)
	}
}
