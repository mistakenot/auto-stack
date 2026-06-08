package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/mistakenot/auto-shared/version"
	"github.com/mistakenot/auto-ui/internal/app"
	"github.com/spf13/cobra"
)

type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func Execute(ctx context.Context, stdout, stderr io.Writer) int {
	application := app.New(stdout, stderr)
	rootCmd := NewRootCmd(application)
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			if exitErr.Err != nil && exitErr.Err.Error() != "" {
				fmt.Fprintln(stderr, exitErr.Err)
			}
			return exitErr.Code
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func NewRootCmd(application *app.App) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "ui",
		Short:         "Serve the auto-ui local web dashboard",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	rootCmd.Version = version.Version
	rootCmd.SetOut(application.Stdout)
	rootCmd.SetErr(application.Stderr)

	rootCmd.AddCommand(
		newInitCmd(),
		newDoctorCmd(),
		newQuickstartCmd(),
		newDocsCmd(),
		newUpdateCmd(),
		newServeCmd(application),
	)

	return rootCmd
}
