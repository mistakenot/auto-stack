package cli

import (
	"fmt"

	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/sync"
	"github.com/spf13/cobra"
)

// newUpdateCmd is the skills update verb (D-3 reclaim): `auto skill update` floats
// vendored skills to their newest upstream commits via the native update engine.
// The binary self-update that this name used to run is now reachable ONLY at the
// root `auto update` command — no auto-shared/update call remains under skill.
func newUpdateCmd(resolveEnv envResolver) *cobra.Command {
	var (
		check  bool
		format string
	)

	cmd := &cobra.Command{
		Use:   "update [name...]",
		Short: "Update vendored skills to their latest upstream commits",
		Long: "Float vendored (locked) skills to the newest upstream commit for their " +
			"version spec, then reconcile the lock. With no names every floating skill " +
			"is considered; names restrict the run. --check resolves upstream and reports " +
			"what would change without writing anything.\n\n" +
			"Binary self-update lives at `auto update`, not here.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := resolveFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			result, err := sync.Update(env, args, check)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if mode == "text" {
				out := cmd.OutOrStdout()
				if len(result.Changed) == 0 {
					fmt.Fprintln(out, "All skills are up to date.")
				}
				for i := range result.Changed {
					sp := &result.Changed[i]
					fmt.Fprintf(out, "- %s: %s → %s\n", sp.Name, sp.LockedCommit, sp.TargetCommit)
				}
				return nil
			}

			data, err := skill.EncodeJSON(result)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "resolve upstream and report changes without writing")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json (default) or text")
	return cmd
}
