package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/feedback"
	"github.com/mistakenot/auto-reflect/internal/store"
	"github.com/mistakenot/auto-reflect/internal/timefilter"
	"github.com/spf13/cobra"
)

func newFeedbackCmd(application *app.App) *cobra.Command {
	feedbackCmd := &cobra.Command{
		Use:   "feedback",
		Short: "Capture and query feedback events",
	}

	feedbackCmd.AddCommand(newFeedbackAddCmd(application))
	feedbackCmd.AddCommand(newFeedbackListCmd(application))

	return feedbackCmd
}

func newFeedbackAddCmd(application *app.App) *cobra.Command {
	var kind string
	var file string
	var start int
	var end int
	var startSet bool
	var endSet bool
	var comment string
	var contextText string
	var format string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a feedback event",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			service := feedback.NewService()

			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			input := feedback.AddInput{
				Kind:    kind,
				File:    file,
				Comment: comment,
				Context: contextText,
			}
			if startSet {
				input.Start = &start
			}
			if endSet {
				input.End = &end
			}

			result, validationErrs, addErr := service.Add(application.CWD, &input)
			if addErr != nil {
				return &ExitError{Code: 1, Err: addErr}
			}
			if len(validationErrs) > 0 {
				writeValidationErrors(cmd.ErrOrStderr(), validationErrs)
				return &ExitError{Code: 1}
			}

			displayPath := store.DisplayPath(application.CWD, result.Path)
			if outputFormat == "text" {
				fmt.Fprintf(cmd.OutOrStdout(), "Recorded %s feedback as %s\n", result.Event.Kind, result.Event.ID)
				fmt.Fprintf(cmd.OutOrStdout(), "Path: %s\n", displayPath)
				return nil
			}

			payload := map[string]any{
				"created": true,
				"path":    displayPath,
				"event":   result.Event,
			}
			if err := writeJSON(cmd.OutOrStdout(), payload); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "feedback kind: helpful|harmful|missing")
	cmd.Flags().StringVar(&file, "file", "", "repo-relative file path")
	cmd.Flags().IntVar(&start, "start", 0, "start line (1-based)")
	cmd.Flags().IntVar(&end, "end", 0, "end line (1-based)")
	cmd.Flags().StringVar(&comment, "comment", "", "feedback comment")
	cmd.Flags().StringVar(&contextText, "context", "", "optional workflow context")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	_ = cmd.MarkFlagRequired("kind")
	_ = cmd.MarkFlagRequired("comment")

	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		startSet = cmd.Flags().Changed("start")
		endSet = cmd.Flags().Changed("end")
		return nil
	}

	return cmd
}

func newFeedbackListCmd(application *app.App) *cobra.Command {
	var kind string
	var fileFilter string
	var since string
	var after string
	var before string
	var limit int
	var format string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List feedback events",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			service := feedback.NewService()

			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if limit < 0 {
				return &ExitError{Code: 1, Err: errors.New("invalid --limit: Use --limit <n> where n >= 0")}
			}

			window, err := timefilter.Parse(time.Now().UTC(), since, after, before)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			result, validationErrs, listErr := service.List(application.CWD, feedback.ListInput{
				Kind:   strings.TrimSpace(kind),
				File:   strings.TrimSpace(fileFilter),
				After:  window.After,
				Before: window.Before,
				Limit:  limit,
			})
			if listErr != nil {
				return &ExitError{Code: 1, Err: listErr}
			}

			if outputFormat == "text" {
				if len(result.Events) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No feedback events found.")
				}
				for i := range result.Events {
					event := &result.Events[i]
					fmt.Fprintf(cmd.OutOrStdout(), "%d. [%s] %s %s\n", i+1, event.Kind, event.ID, event.Timestamp)
					fmt.Fprintf(cmd.OutOrStdout(), "   %s\n", event.Comment)
					if event.Subject.File != nil {
						fmt.Fprintf(cmd.OutOrStdout(), "   file: %s\n", *event.Subject.File)
					}
				}
			} else {
				payload := map[string]any{
					"events": result.Events,
				}
				if len(validationErrs) > 0 {
					payload["warnings"] = validationErrs
				}
				if err := writeJSON(cmd.OutOrStdout(), payload); err != nil {
					return &ExitError{Code: 1, Err: err}
				}
			}

			if len(validationErrs) > 0 {
				writeValidationErrors(cmd.ErrOrStderr(), validationErrs)
				return &ExitError{Code: 1}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "filter by feedback kind")
	cmd.Flags().StringVar(&fileFilter, "file", "", "filter by file path substring")
	cmd.Flags().StringVar(&since, "since", "", "relative time filter (e.g. 7d)")
	cmd.Flags().StringVar(&after, "after", "", "inclusive lower date bound (YYYY-MM-DD or RFC3339)")
	cmd.Flags().StringVar(&before, "before", "", "exclusive upper date bound (YYYY-MM-DD or RFC3339)")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum events to return (0 means all)")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")

	return cmd
}
