package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/mistakenot/auto-skill/internal/adopt"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/spf13/cobra"
)

// newAdoptCmd pulls foreign (un-managed) skill dirs that were hand-dropped into
// an output target back into the project's authored ./skills/ source of truth
// via a staged filesystem move + `git add`. It is non-interactive and CI-safe:
// with no names and no --all it simply LISTS the adoptable candidates as JSON
// (JSON is the default; --text emits a human summary). We deliberately do NOT
// implement an interactive TTY picker — that would break JSON-default /
// fails-closed automation; choose a name, --all, or --from instead.
//
// NOTE: this command is intentionally NOT registered in root.go here — phase 6
// wires it up alongside the doctor extension, hence the nolint:unused.
//
//nolint:unused // registered by phase 6 (root.go wire-up)
func newAdoptCmd(resolveEnv envResolver) *cobra.Command {
	var (
		all        bool
		from       string
		force      bool
		yes        bool
		textOutput bool
	)

	cmd := &cobra.Command{
		Use:   "adopt [name...]",
		Short: "Adopt foreign skills from targets into ./skills/",
		Long: "Pull a foreign (un-managed) skill dir hand-dropped into an output target " +
			"back into ./skills/ via a staged filesystem move (copy → verify → remove → " +
			"git add). adopt does not re-render — the next `sync` does.\n\n" +
			"With no names and no --all it lists the adoptable foreign skills as JSON. " +
			"--all adopts every unambiguous candidate. When a name's copies differ across " +
			"targets it is a hard error; choose the source with --from <target>. An " +
			"existing ./skills/<name>/ is refused unless --force.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			result, runErr := adopt.Adopt(env, args, adopt.Options{
				All:   all,
				Force: force,
				Yes:   yes,
				From:  from,
			})
			if runErr != nil {
				return &ExitError{Code: 1, Err: runErr}
			}

			// Payload first: JSON (default) on stdout, or a human summary with --text.
			if textOutput {
				writeAdoptText(cmd.OutOrStdout(), result)
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

	cmd.Flags().BoolVar(&all, "all", false, "adopt every unambiguous adoptable candidate")
	cmd.Flags().StringVar(&from, "from", "", "target style to adopt from when copies diverge across targets")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing ./skills/<name>/")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "assume yes (non-interactive; accepted for symmetry)")
	cmd.Flags().BoolVar(&textOutput, "text", false, "emit a human-readable summary instead of JSON")

	return cmd
}

// writeAdoptText prints a compact human-readable summary of an adopt result.
//
//nolint:unused // used by newAdoptCmd, registered by phase 6 (root.go wire-up)
func writeAdoptText(w io.Writer, r adopt.Result) {
	if len(r.Candidates) > 0 {
		fmt.Fprintf(w, "adoptable: %d\n", len(r.Candidates))
		for _, c := range r.Candidates {
			targets := make([]string, 0, len(c.Copies))
			for _, cp := range c.Copies {
				targets = append(targets, cp.Target)
			}
			fmt.Fprintf(w, "  ? %s (in %s)\n", c.Name, strings.Join(targets, ", "))
		}
		return
	}
	if len(r.Adopted) > 0 {
		fmt.Fprintf(w, "adopted: %d\n", len(r.Adopted))
		for _, a := range r.Adopted {
			fmt.Fprintf(w, "  + %s ← %s (git add %s); run sync to render\n", a.Name, a.From, a.Dir)
		}
	}
	if len(r.Adopted) == 0 && len(r.Errors) == 0 {
		fmt.Fprintln(w, "nothing to adopt")
	}
}
