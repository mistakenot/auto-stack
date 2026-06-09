package cli

import (
	"fmt"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/gitutil"
	"github.com/mistakenot/auto-reflect/internal/rules"
	"github.com/mistakenot/auto-reflect/internal/store"
	"github.com/spf13/cobra"
)

func newRebuildCmd(application *app.App) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Force a full refold of the playbook from the event log",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			repo, err := gitutil.DetectRepoLenient(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			playbook, conflicts, err := rules.Rebuild(repo.Root, store.PlaybookPath(repo.Root))
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			// Conflicts are diagnostics: stderr only, never on the JSON stdout.
			for _, c := range conflicts {
				fmt.Fprintf(cmd.ErrOrStderr(), "conflict: rule %s fields %v expected from_version %d but folded version was %d (last-writer-wins applied)\n",
					c.RuleID, c.Fields, c.Expected, c.Actual)
			}

			if outputFormat == "text" {
				fmt.Fprintf(cmd.OutOrStdout(), "Rebuilt playbook: %d rules, %d conflict(s)\n", len(playbook.Rules), len(conflicts))
				return nil
			}
			if err := writeJSON(cmd.OutOrStdout(), map[string]any{
				"rebuilt":        true,
				"rule_count":     len(playbook.Rules),
				"conflict_count": len(conflicts),
			}); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}
