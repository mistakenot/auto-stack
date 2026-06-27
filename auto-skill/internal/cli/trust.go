package cli

import (
	"encoding/json"
	"fmt"

	"github.com/mistakenot/auto-skill/internal/trust"
	"github.com/spf13/cobra"
)

func newTrustCmd(resolveEnv envResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Manage trusted endpoints for skill fetching",
	}

	cmd.AddCommand(
		newTrustListCmd(resolveEnv),
		newTrustAddCmd(resolveEnv),
		newTrustRemoveCmd(resolveEnv),
	)

	return cmd
}

func newTrustListCmd(resolveEnv envResolver) *cobra.Command {
	var textOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List approved endpoints",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			store := trust.NewStore(env.TrustPath())
			tf, err := store.Load()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if textOutput {
				if len(tf.Endpoints) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "no trusted endpoints")
					return nil
				}
				for _, ep := range tf.Endpoints {
					fmt.Fprintln(cmd.OutOrStdout(), ep)
				}
				return nil
			}

			data, err := json.Marshal(tf)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
	cmd.Flags().BoolVar(&textOutput, "text", false, "human-readable output")
	return cmd
}

func newTrustAddCmd(resolveEnv envResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <endpoint>",
		Short: "Approve an endpoint for skill fetching",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			store := trust.NewStore(env.TrustPath())
			if err := store.Add(args[0]); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "approved: %s\n", args[0])
			return nil
		},
	}
	return cmd
}

func newTrustRemoveCmd(resolveEnv envResolver) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <endpoint>",
		Short: "Remove an endpoint from the approved list",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := resolveEnv()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			store := trust.NewStore(env.TrustPath())
			if err := store.Remove(args[0]); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "removed: %s\n", args[0])
			return nil
		},
	}
	return cmd
}
