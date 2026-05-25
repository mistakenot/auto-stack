package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/mistakenot/auto-graph/internal/app"
	"github.com/mistakenot/auto-shared/version"
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
		Use:           "autograph",
		Short:         "Build and query code context graphs",
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
		newCodeCmd(),
	)

	return rootCmd
}
