package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/sync"
	"github.com/spf13/cobra"
)

// newRemoveCmd drops a skill's source of truth (authored ./skills/<name>/ and/or
// the vendored lock + skills.yaml entry) and then re-runs the sync engine so its
// now-orphaned target copies are pruned under the same receipt-gated, journaled
// authority — target copies lacking a matching receipt (or locally modified) are
// reported, never deleted. The --local / --vendored selector is required only
// when the name exists as both; otherwise it is inferred.
//
// NOTE: this command is intentionally NOT registered in root.go here — phase 6
// wires it up alongside the doctor extension, hence the nolint:unused.
//
//nolint:unused // registered by phase 6 (root.go wire-up)
func newRemoveCmd(resolveEnv envResolver) *cobra.Command {
	var (
		local      bool
		vendored   bool
		textOutput bool
	)

	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a skill and prune its rendered target copies",
		Long: "Drop a skill's source of truth — the authored ./skills/<name>/ dir " +
			"(--local) and/or the vendored lock + skills.yaml entry (--vendored) — then " +
			"reconcile: the now-undesired skill is pruned from every target through the " +
			"same receipt-gated, journaled prune sync uses.\n\n" +
			"A selector is required only when the name exists as both a local and a " +
			"vendored skill; otherwise the single present source is inferred. Target " +
			"copies lacking a matching receipt, or locally modified, are reported — " +
			"never deleted.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if local && vendored {
				return &ExitError{Code: 1, Err: errors.New("invalid flags: --local and --vendored cannot be combined")}
			}

			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			sel := sync.SelUnset
			switch {
			case local:
				sel = sync.SelLocal
			case vendored:
				sel = sync.SelVendored
			}

			result, runErr := sync.Remove(env, args[0], sel)
			if runErr != nil {
				// Fail-fast usage error (unknown name / ambiguous / mismatched
				// selector): nothing was mutated.
				return &ExitError{Code: 1, Err: runErr}
			}

			// Payload first: JSON (default) on stdout, or a human summary with --text.
			if textOutput {
				writeRemoveText(cmd.OutOrStdout(), result)
			} else {
				data, encErr := skill.EncodeJSON(result)
				if encErr != nil {
					return &ExitError{Code: 1, Err: encErr}
				}
				if _, wErr := cmd.OutOrStdout().Write(data); wErr != nil {
					return &ExitError{Code: 1, Err: wErr}
				}
			}

			// Diagnostics → stderr so stdout stays a clean payload.
			for _, e := range result.Errors {
				fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", e)
			}
			if len(result.Errors) > 0 {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&local, "local", false, "remove the authored ./skills/<name>/ source")
	cmd.Flags().BoolVar(&vendored, "vendored", false, "remove the vendored (lock + skills.yaml) source")
	cmd.Flags().BoolVar(&textOutput, "text", false, "emit a human-readable summary instead of JSON")

	return cmd
}

// writeRemoveText prints a compact human-readable summary of a remove result.
//
//nolint:unused // used by newRemoveCmd, registered by phase 6 (root.go wire-up)
func writeRemoveText(w io.Writer, r sync.RemoveResult) {
	if len(r.Removed) > 0 {
		fmt.Fprintf(w, "removed %s (%v)\n", r.Name, r.Removed)
	}
	if len(r.Pruned) > 0 {
		fmt.Fprintf(w, "pruned: %d\n", len(r.Pruned))
		for _, p := range r.Pruned {
			fmt.Fprintf(w, "  - %s\n", p)
		}
	}
	if len(r.Reported) > 0 {
		fmt.Fprintf(w, "reported (not deleted — no receipt or locally modified): %d\n", len(r.Reported))
		for _, p := range r.Reported {
			fmt.Fprintf(w, "  ! %s\n", p)
		}
	}
	if len(r.Pruned) == 0 && len(r.Reported) == 0 {
		fmt.Fprintln(w, "no target copies to prune")
	}
}
