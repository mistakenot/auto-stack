package cli

import (
	"errors"
	"fmt"

	"github.com/mistakenot/auto-skill/internal/add"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/mistakenot/auto-skill/internal/sync"
	"github.com/mistakenot/auto-skill/internal/trace"
	"github.com/mistakenot/auto-skill/internal/transport"
	"github.com/mistakenot/auto-skill/internal/trust"
	"github.com/spf13/cobra"
)

func newAddCmd(resolveEnv envResolver, resolveTrace traceResolver) *cobra.Command {
	var (
		skills         []string
		paths          []string
		list           bool
		fullDepth      bool
		version        string
		as             string
		noSync         bool
		force          bool
		trustRequested bool
		textOutput     bool
		format         string
	)

	cmd := &cobra.Command{
		Use:   "add <source>",
		Short: "Add skills from a local or remote source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Fail-fast flag conflicts.
			if as != "" && (len(skills) > 1 || (len(skills) == 1 && skills[0] == "*")) {
				return &ExitError{Code: 1, Err: errors.New("invalid flags: --as cannot be combined with multiple --skill values or --skill '*'")}
			}

			wantText := textOutput || format == "text"
			if textOutput && format == "json" {
				return &ExitError{Code: 1, Err: errors.New("invalid flags: --text and --format json cannot be combined")}
			}

			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			tr := resolveTrace(cmd)
			opts := add.Options{
				Source:         args[0],
				Skills:         skills,
				Paths:          paths,
				List:           list,
				FullDepth:      fullDepth,
				Version:        version,
				As:             as,
				NoSync:         noSync,
				Force:          force,
				TrustRequested: trustRequested,
				Trace:          tr,
			}

			result, err := add.Run(env, opts)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if wantText {
				formatAddText(cmd, result)
			} else {
				data, err := skill.EncodeJSON(result)
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				if _, err := cmd.OutOrStdout().Write(data); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			}

			// Post-add auto-sync (T4): a plain `add` renders the freshly added
			// skills into every output target by default. Skipped for --list
			// (nothing was written) and --no-sync. The render is best-effort: it
			// never overrides add's own exit code (the add itself — lock +
			// skills.yaml or the authored copy — already succeeded). Its
			// diagnostics go to stderr; add's stdout payload (already written
			// above) is left untouched so it stays strictly parseable.
			if !noSync && !list {
				runPostAddSync(cmd, env, result, trustRequested, tr)
			}

			return nil
		},
	}

	cmd.Flags().StringArrayVar(&skills, "skill", nil, "exact-match skill filter (repeatable; '*' for all)")
	cmd.Flags().StringArrayVar(&paths, "path", nil, "scope discovery to specific paths (repeatable)")
	cmd.Flags().BoolVar(&list, "list", false, "preview discovered skills and exit without writing")
	cmd.Flags().BoolVar(&fullDepth, "full-depth", false, "force recursive scan of entire tree")
	cmd.Flags().StringVar(&version, "version", "", "version spec override (default: latest)")
	cmd.Flags().StringVar(&as, "as", "", "rename (single-skill only)")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "skip post-add sync (accepted; always true until T4)")
	cmd.Flags().BoolVar(&force, "force", false, "force overwrite on local import collision")
	cmd.Flags().BoolVar(&trustRequested, "trust-requested", false, "opt into advisory trust (CI/non-TTY)")
	cmd.Flags().BoolVar(&textOutput, "text", false, "emit human-readable text output")
	cmd.Flags().StringVar(&format, "format", "", "output format override (json or text)")

	return cmd
}

// runPostAddSync renders the just-added skills into the configured targets.
// It mirrors what `add` already decided about the source: a successful remote
// add has already approved (and cached) its endpoint, so the render needs no
// network. A local add bypasses the trust gate, so the source endpoint is
// approved here to keep the subsequent render symmetric — the user explicitly
// pointed `add` at that source. The render is best-effort: every outcome is
// reported on stderr, but add's exit code is owned by the add operation
// itself, never by this follow-on render.
func runPostAddSync(cmd *cobra.Command, env skill.Env, result add.Result, trustRequested bool, tr *trace.Logger) {
	// Make the just-added source trustable for the render step (idempotent for
	// an already-approved remote endpoint; best-effort — a non-approvable
	// source simply surfaces as a sync diagnostic below).
	if result.Source != "" {
		if ep, err := transport.Endpoint(result.Source); err == nil {
			_ = trust.NewStore(env.TrustPath()).Add(ep)
		}
	}

	syncRes, syncErr := sync.Run(env, sync.Options{TrustRequested: trustRequested, Trace: tr})
	if syncRes != nil {
		for _, w := range syncRes.Warnings {
			fmt.Fprintf(cmd.ErrOrStderr(), "sync warning: %s\n", w)
		}
		for _, e := range syncRes.Errors {
			fmt.Fprintf(cmd.ErrOrStderr(), "sync error: %s\n", e)
		}
	}
	if syncErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "sync error: %s\n", syncErr)
	}
}

// formatAddText writes human-readable add output to stdout.
func formatAddText(cmd *cobra.Command, result add.Result) {
	if len(result.Listed) > 0 {
		for _, l := range result.Listed {
			line := fmt.Sprintf("  %s  %s", l.Name, l.Subpath)
			if l.NeedsAs {
				line += "  [needs --as]"
			}
			fmt.Fprintln(cmd.OutOrStdout(), line)
		}
		return
	}

	for _, a := range result.Added {
		sha := a.Commit
		if len(sha) > 12 {
			sha = sha[:12]
		}
		if sha != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s (commit %s)\n", a.Name, sha)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s\n", a.Name)
		}
	}
}
