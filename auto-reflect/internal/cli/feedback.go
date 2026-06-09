package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/loop"
	"github.com/spf13/cobra"
)

func newFeedbackCmd(application *app.App) *cobra.Command {
	var (
		session string
		format  string
	)

	cmd := &cobra.Command{
		Use:   "feedback <json|->",
		Short: "Submit feedback closing the loop (JSON document or '-' for stdin)",
		Long: "Submit a feedback document to close the retrieval loop. " +
			"Pass the JSON as a positional argument, or '-' to read it from stdin. " +
			"The payload must rank exactly the outstanding feedback ids minted by `auto reflect select`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			raw, err := readFeedbackInput(cmd, args[0])
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			parsed, err := loop.ParseFeedback(raw)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			svc := loop.NewService(application.CWD)
			validationErrs, submitErr := svc.SubmitFeedback(parsed, strings.TrimSpace(session))
			if submitErr != nil {
				return &ExitError{Code: 1, Err: submitErr}
			}
			if len(validationErrs) > 0 {
				writeValidationErrors(cmd.ErrOrStderr(), validationErrs)
				fmt.Fprintln(cmd.ErrOrStderr(), "remediation: rank exactly the outstanding feedback ids; run `auto reflect gate check` to list them")
				return &ExitError{Code: 1}
			}

			if outputFormat == "text" {
				fmt.Fprintln(cmd.OutOrStdout(), "Feedback recorded; loop closed.")
				return nil
			}
			if err := writeJSON(cmd.OutOrStdout(), map[string]any{"recorded": true}); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&session, "session", "", "session id to scope feedback to (overrides env detection; closes another session's loop)")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}

// readFeedbackInput returns the raw feedback JSON, reading from stdin when the
// argument is "-".
func readFeedbackInput(cmd *cobra.Command, arg string) ([]byte, error) {
	if arg == "-" {
		in := cmd.InOrStdin()
		if in == nil {
			in = os.Stdin
		}
		data, err := io.ReadAll(in)
		if err != nil {
			return nil, fmt.Errorf("read feedback from stdin: %w", err)
		}
		if strings.TrimSpace(string(data)) == "" {
			return nil, errors.New("empty feedback on stdin: pipe a JSON document or pass it as a positional argument")
		}
		return data, nil
	}
	if strings.TrimSpace(arg) == "" {
		return nil, errors.New("empty feedback document: pass a JSON payload or '-' to read from stdin")
	}
	return []byte(arg), nil
}
