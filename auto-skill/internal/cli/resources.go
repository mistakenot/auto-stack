package cli

import (
	"fmt"

	"github.com/mistakenot/auto-skill/internal/inspect"
	"github.com/spf13/cobra"
)

func newSourceCmd(resolveEnv envResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "source",
		Short: "Inspect upstream skill sources (deduped by repo)",
	}
	cmd.AddCommand(newSourceListCmd(resolveEnv), newSourceDescribeCmd(resolveEnv))
	return cmd
}

func newSourceListCmd(resolveEnv envResolver) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List upstream sources from the lock, with the skills each provides",
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
			sources, err := inspect.SourceList(env)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if mode == "text" {
				out := cmd.OutOrStdout()
				for _, s := range sources {
					fmt.Fprintf(out, "- %s @ %s: %v\n", s.ID, s.Commit, s.Skills)
				}
				return nil
			}
			return writeJSON(cmd, sources)
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "output format: json (default) or text")
	return cmd
}

func newSourceDescribeCmd(resolveEnv envResolver) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "describe <id>",
		Short: "Show one source's url/ref/commit and the skills it provides",
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
			src, err := inspect.SourceDescribe(env, args[0])
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if mode == "text" {
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "id:     %s\n", src.ID)
				fmt.Fprintf(out, "url:    %s\n", src.URL)
				fmt.Fprintf(out, "ref:    %s\n", src.Ref)
				fmt.Fprintf(out, "commit: %s\n", src.Commit)
				fmt.Fprintf(out, "skills: %v\n", src.Skills)
				return nil
			}
			return writeJSON(cmd, src)
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "output format: json (default) or text")
	return cmd
}

func newTargetCmd(resolveEnv envResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "target",
		Short: "Inspect configured output targets",
	}
	cmd.AddCommand(newTargetListCmd(resolveEnv))
	return cmd
}

func newTargetListCmd(resolveEnv envResolver) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured targets with their on-disk path and managed-skill count",
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
			targets, err := inspect.TargetList(env)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if mode == "text" {
				out := cmd.OutOrStdout()
				for _, t := range targets {
					fmt.Fprintf(out, "- %s: %s (%d managed)\n", t.Name, t.Path, t.ManagedCount)
				}
				return nil
			}
			return writeJSON(cmd, targets)
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "output format: json (default) or text")
	return cmd
}
