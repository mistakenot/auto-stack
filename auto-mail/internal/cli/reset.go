package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/mistakenot/auto-mail/internal/app"
	"github.com/mistakenot/auto-mail/internal/config"
	"github.com/mistakenot/auto-mail/mail"
	"github.com/spf13/cobra"
)

func newResetCmd(application *app.App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Wipe the alpha mail store and its pending flags",
		Long: "Remove ~/.auto/mail/alpha-store.db and ~/.auto/mail/alpha-flags/ and report " +
			"what was removed. The store is alpha: there are no upcasters and no migrations, " +
			"so starting again is the supported migration path rather than a workaround. " +
			"A store that still holds events is refused unless --yes is given.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReset(cmd, application, yes)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "wipe a store that still holds mail")
	return cmd
}

func runReset(cmd *cobra.Command, _ *app.App, yes bool) error {
	storePath, err := config.StorePath()
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	flagsDir, err := config.FlagsDir()
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}

	// Opening the client creates the store if it is absent, so a host with
	// nothing to reset is answered before anything is opened. Otherwise
	// `reset` on a fresh host would report removing a file it had just made.
	if !exists(storePath) && !exists(flagsDir) {
		if err := writeJSON(cmd.OutOrStdout(), mail.ResetResult{Removed: []string{}}); err != nil {
			return &ExitError{Code: 1, Err: err}
		}
		return nil
	}

	client, err := mail.NewDirect("")
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	defer func() { _ = client.Close() }()

	result, err := client.Reset(cmd.Context(), mail.ResetInput{Force: yes})
	switch {
	case errors.Is(err, mail.ErrStoreNotEmpty):
		return &ExitError{Code: 1, Err: fmt.Errorf(
			"%w — run `auto mail list` to see what is still waiting, then "+
				"`auto mail reset --yes` to wipe it anyway", err)}
	case err != nil:
		return &ExitError{Code: 1, Err: err}
	}

	if err := writeJSON(cmd.OutOrStdout(), result); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
