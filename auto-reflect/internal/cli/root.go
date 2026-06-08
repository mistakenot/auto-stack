package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mistakenot/auto-reflect/internal/app"
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
	cmd := &cobra.Command{
		Use:           "reflect",
		Short:         "Persist and retrieve repository rules with feedback capture",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	cmd.Version = version.Version
	cmd.SetOut(application.Stdout)
	cmd.SetErr(application.Stderr)

	cmd.AddCommand(
		newInitCmd(application),
		newQuickstartCmd(),
		newRuleCmd(application),
		newLookupCmd(application),
		newFeedbackCmd(application),
	)

	return cmd
}

func writeJSON(w io.Writer, payload any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func normalizeFormat(raw string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(raw))
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "text" {
		return "", fmt.Errorf("invalid --format value %q: use --format json|text", raw)
	}
	return format, nil
}
