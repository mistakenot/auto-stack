package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/mistakenot/auto-env/internal/app"
	"github.com/mistakenot/auto-shared/version"
	"github.com/spf13/cobra"
)

type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit %d", e.Code)
}

func Execute(ctx context.Context, stdout, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	application := app.New(stdout, stderr, cwd)
	rootCmd := newRootCmd(application)

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

func newRootCmd(application *app.App) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "autoenv",
		Short:         "Template-based config file generation with per-worktree port allocation",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	rootCmd.Version = version.Version
	rootCmd.SetOut(application.Stdout)
	rootCmd.SetErr(application.Stderr)

	rootCmd.AddCommand(
		newInitCmd(application),
		newUpCmd(application),
		newDownCmd(application),
		newStatusCmd(application),
		newQuickstartCmd(),
		newAgentsCmd(),
	)

	return rootCmd
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
