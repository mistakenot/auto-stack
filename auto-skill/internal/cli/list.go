package cli

import (
	"fmt"
	"strings"

	"github.com/mistakenot/auto-skill/internal/inspect"
	"github.com/mistakenot/auto-skill/internal/skill"
	"github.com/spf13/cobra"
)

// descTruncateWidth is the column at which `list`/`describe` truncate long
// free-text fields in the human (text) view. The cheap rungs carry ids +
// metadata; the truncated line points at the full-fidelity recover command.
const descTruncateWidth = 72

// resolveFormat validates the --format value, defaulting to json. Only "json" and
// "text" are accepted; anything else is a fail-fast usage error.
func resolveFormat(format string) (string, error) {
	switch format {
	case "", "json":
		return "json", nil
	case "text":
		return "text", nil
	default:
		return "", fmt.Errorf("invalid --format %q: expected json or text", format)
	}
}

// truncateField shortens s to descTruncateWidth runes, appending an ellipsis when
// it was cut. The bool reports whether truncation happened.
func truncateField(s string) (string, bool) {
	r := []rune(s)
	if len(r) <= descTruncateWidth {
		return s, false
	}
	return string(r[:descTruncateWidth-1]) + "…", true
}

func newListCmd(resolveEnv envResolver) *cobra.Command {
	var (
		local    bool
		vendored bool
		format   string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List authored and vendored skills with origin and a stale flag",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode, err := resolveFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			views, parseErrors, err := inspect.Inspect(env, inspect.Filter{Local: local, Vendored: vendored})
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if mode == "text" {
				writeListText(cmd, views)
			} else {
				data, err := skill.EncodeJSON(views)
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				if _, err := cmd.OutOrStdout().Write(data); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			}

			for _, pe := range parseErrors {
				fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", pe)
			}
			if len(parseErrors) > 0 {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&local, "local", false, "list only authored skills")
	cmd.Flags().BoolVar(&vendored, "vendored", false, "list only vendored (remote-sourced) skills")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json (default) or text")
	cmd.MarkFlagsMutuallyExclusive("local", "vendored")
	return cmd
}

func writeListText(cmd *cobra.Command, views []inspect.SkillView) {
	out := cmd.OutOrStdout()
	truncated := false
	for _, v := range views {
		desc, cut := truncateField(v.Description)
		if cut {
			truncated = true
		}
		stale := "stale=unknown"
		if v.Stale != nil {
			if *v.Stale {
				stale = "stale=true"
			} else {
				stale = "stale=false"
			}
		}
		shadow := ""
		if v.Shadowed {
			shadow = " (shadows vendored)"
		}
		fmt.Fprintf(out, "- %s [%s] %s%s: %s\n", v.Name, v.Origin, stale, shadow, desc)
	}
	if truncated {
		fmt.Fprintln(out, "# descriptions truncated — run 'auto skill get <name>' for the full SKILL.md")
	}
}

func newDescribeCmd(resolveEnv envResolver) *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "describe <name>",
		Short: "Show a skill's provenance (source, ref, commit, skill_version, replacements)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := resolveFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			prov, err := inspect.Describe(env, args[0])
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if mode == "text" {
				writeDescribeText(cmd, prov)
			} else {
				data, err := skill.EncodeJSON(prov)
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				if _, err := cmd.OutOrStdout().Write(data); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "json", "output format: json (default) or text")
	return cmd
}

func writeDescribeText(cmd *cobra.Command, prov inspect.Provenance) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "name:          %s\n", prov.Name)
	fmt.Fprintf(out, "origin:        %s\n", prov.Origin)
	if prov.Source != "" {
		fmt.Fprintf(out, "source:        %s\n", prov.Source)
	}
	if prov.URL != "" {
		fmt.Fprintf(out, "url:           %s\n", prov.URL)
	}
	if prov.Ref != "" {
		fmt.Fprintf(out, "ref:           %s\n", prov.Ref)
	}
	if prov.Commit != "" {
		fmt.Fprintf(out, "commit:        %s\n", prov.Commit)
	}
	if prov.VersionSpec != "" {
		fmt.Fprintf(out, "version_spec:  %s\n", prov.VersionSpec)
	}
	if prov.SkillVersion != "" {
		fmt.Fprintf(out, "skill_version: %s\n", prov.SkillVersion)
	}
	if len(prov.Replacements) > 0 {
		fmt.Fprintln(out, "replacements:")
		for k, v := range prov.Replacements {
			fmt.Fprintf(out, "  %s: %s\n", k, v)
		}
	}
}

func newGetCmd(resolveEnv envResolver) *cobra.Command {
	var (
		format string
		target string
	)

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Print a skill's full rendered SKILL.md",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := resolveFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			content, resolvedTarget, err := inspect.Get(env, args[0], target)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if mode == "text" {
				if _, err := cmd.OutOrStdout().Write(content); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				if !strings.HasSuffix(string(content), "\n") {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				return nil
			}

			payload := map[string]any{
				"name":    args[0],
				"target":  resolvedTarget,
				"content": string(content),
			}
			data, err := skill.EncodeJSON(payload)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if _, err := cmd.OutOrStdout().Write(data); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "json", "output format: json (default) or text (raw markdown)")
	cmd.Flags().StringVar(&target, "target", "", "read from a specific target style (default: first configured)")
	return cmd
}
