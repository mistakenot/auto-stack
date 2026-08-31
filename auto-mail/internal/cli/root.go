package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/mistakenot/auto-mail/internal/app"
	"github.com/mistakenot/auto-shared/version"
	"github.com/spf13/cobra"
)

// ExitError carries a specific process exit code out of a command's RunE.
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

// ExitCode lets the merged `auto` dispatcher honor the declared exit code.
func (e *ExitError) ExitCode() int { return e.Code }

func (e *ExitError) Unwrap() error { return e.Err }

// Execute runs the standalone auto-mail binary.
func Execute(ctx context.Context, stdout, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	application := app.New(stdout, stderr, cwd)
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

// NewRootCmd builds the auto-mail command tree (mounted as `auto mail`).
func NewRootCmd(application *app.App) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "mail",
		Short:         "Durable, addressed mail between agents on one host (alpha)",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	rootCmd.Version = version.Version
	rootCmd.SetOut(application.Stdout)
	rootCmd.SetErr(application.Stderr)

	rootCmd.AddCommand(
		newInitCmd(application),
		newSubscribeCmd(application),
		newSendCmd(application),
		newListCmd(application),
		newAckCmd(application),
		newResetCmd(application),
		newDocsCmd(),
	)

	return rootCmd
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
