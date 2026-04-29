package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/gitutil"
	"github.com/mistakenot/auto-reflect/internal/rules"
	"github.com/mistakenot/auto-reflect/internal/store"
	"github.com/spf13/cobra"
)

func newRuleCmd(application *app.App) *cobra.Command {
	ruleCmd := &cobra.Command{
		Use:   "rule",
		Short: "Manage repository rules",
	}

	var content string
	var category string
	var tags []string
	var format string

	ruleCmd.AddCommand(&cobra.Command{
		Use:   "create",
		Short: "Create a rule",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			repo, err := gitutil.DetectRepo(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			service := rules.NewService()
			result, validationErrs, createErr := service.Create(
				store.PlaybookPath(repo.Root),
				rules.CreateInput{Content: content, Category: category, Tags: tags},
			)
			if createErr != nil {
				return &ExitError{Code: 1, Err: createErr}
			}
			if len(validationErrs) > 0 {
				writeValidationErrors(cmd.ErrOrStderr(), validationErrs)
				return &ExitError{Code: 1}
			}

			displayPath := store.DisplayPath(application.CWD, result.Path)
			if outputFormat == "text" {
				fmt.Fprintf(cmd.OutOrStdout(), "Created rule %s\n", result.Created.ID)
				fmt.Fprintf(cmd.OutOrStdout(), "Category: %s\n", result.Created.Category)
				fmt.Fprintf(cmd.OutOrStdout(), "Content: %s\n", result.Created.Content)
				if len(result.Created.Tags) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "Tags: %s\n", strings.Join(result.Created.Tags, ", "))
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Path: %s\n", displayPath)
				return nil
			}

			payload := map[string]any{
				"created": true,
				"scope":   "repo",
				"path":    displayPath,
				"rule":    result.Created,
			}
			if err := writeJSON(cmd.OutOrStdout(), payload); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	})

	createCmd := ruleCmd.Commands()[0]
	createCmd.Flags().StringVar(&content, "content", "", "rule content")
	createCmd.Flags().StringVar(&category, "category", "", "rule category")
	createCmd.Flags().StringSliceVar(&tags, "tag", nil, "repeatable rule tag")
	createCmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	_ = createCmd.MarkFlagRequired("content")
	_ = createCmd.MarkFlagRequired("category")

	return ruleCmd
}

func newLookupCmd(application *app.App) *cobra.Command {
	var limit int
	var format string

	cmd := &cobra.Command{
		Use:   "lookup <query>",
		Short: "Look up rules",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if limit < 1 {
				return &ExitError{Code: 1, Err: errors.New("invalid --limit: Use --limit <n> where n >= 1")}
			}

			repo, err := gitutil.DetectRepo(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			service := rules.NewService()
			result, validationErrs, lookupErr := service.Lookup(store.PlaybookPath(repo.Root), args[0], limit)
			if lookupErr != nil {
				return &ExitError{Code: 1, Err: lookupErr}
			}

			if outputFormat == "text" {
				for i, rule := range result.Rules {
					fmt.Fprintf(cmd.OutOrStdout(), "%d. [%s] (%s) %.2f\n", i+1, rule.ID, rule.Category, rule.MatchScore)
					fmt.Fprintf(cmd.OutOrStdout(), "   %s\n", rule.Content)
					if len(rule.Tags) > 0 {
						fmt.Fprintf(cmd.OutOrStdout(), "   tags: %s\n", strings.Join(rule.Tags, ", "))
					}
				}
			} else {
				payload := map[string]any{
					"query":    result.Query,
					"keywords": result.Keywords,
					"scope":    "repo",
					"rules":    result.Rules,
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

	cmd.Flags().IntVar(&limit, "limit", 10, "maximum number of matches to return")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}
