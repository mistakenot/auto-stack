package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mistakenot/auto-graph/internal/codegraph"
	"github.com/mistakenot/auto-graph/internal/contextpack"
	"github.com/spf13/cobra"
)

func newCodeContextCmd() *cobra.Command {
	var (
		tokenLimit int
		files      []string
		formatFlag string
		langFlag   string
	)

	cmd := &cobra.Command{
		Use:   "context <dir>",
		Short: "Build a context pack for seed files in a project directory",
		Long: `Build a context pack by selecting relevant files from the import graph
around the given seed files. Output is compact markdown (default) or JSON,
designed for LLM context windows.

The token limit controls the maximum rendered output size. Seed files are
mandatory and must fit within the budget; additional dependencies and
dependents are included by priority while budget allows.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodeContext(cmd, args[0], tokenLimit, files, formatFlag, langFlag)
		},
	}

	cmd.Flags().IntVar(&tokenLimit, "token-limit", 0, "maximum token budget for the rendered output (required)")
	cmd.Flags().StringArrayVar(&files, "file", nil, "seed file path (repeatable, at least one required)")
	cmd.Flags().StringVar(&formatFlag, "format", "markdown", "output format: markdown, json")
	cmd.Flags().StringVar(&langFlag, "lang", "", "language override (auto-detected if omitted)")

	_ = cmd.MarkFlagRequired("token-limit")

	return cmd
}

func runCodeContext(cmd *cobra.Command, dir string, tokenLimit int, files []string, formatFlag, langFlag string) error {
	// Validate required flags.
	if tokenLimit <= 0 {
		return &ExitError{Code: 1, Err: fmt.Errorf("--token-limit must be a positive integer")}
	}
	if len(files) == 0 {
		return &ExitError{Code: 1, Err: fmt.Errorf("at least one --file is required")}
	}

	// Validate format.
	switch formatFlag {
	case "markdown", "json":
		// valid
	default:
		return &ExitError{
			Code: 1,
			Err:  fmt.Errorf("unknown format %q; supported formats: markdown, json", formatFlag),
		}
	}

	// Resolve the project directory to an absolute path.
	projectRoot, err := filepath.Abs(dir)
	if err != nil {
		return &ExitError{Code: 1, Err: fmt.Errorf("resolving directory: %w", err)}
	}

	info, err := os.Stat(projectRoot)
	if err != nil || !info.IsDir() {
		return &ExitError{Code: 1, Err: fmt.Errorf("not a directory: %s", dir)}
	}

	// Detect language.
	lang := langFlag
	if lang == "" {
		detected, err := codegraph.DetectLanguage(projectRoot)
		if err != nil {
			return &ExitError{Code: 1, Err: err}
		}
		lang = detected
	}

	// Build the import graph.
	g, err := codegraph.Build(projectRoot, lang)
	if err != nil {
		return &ExitError{Code: 1, Err: err}
	}

	// Normalize and validate seed files.
	seeds, err := contextpack.NormalizeAndValidateSeeds(files, projectRoot, g)
	if err != nil {
		var valErrs *contextpack.ValidationErrors
		if errors.As(err, &valErrs) {
			// Print each validation error to stderr.
			w := cmd.ErrOrStderr()
			for _, ve := range valErrs.Errors {
				fmt.Fprintf(w, "Error [%s]: %s\n", ve.Code, ve.Message)
			}
			return &ExitError{Code: 1, Err: fmt.Errorf("seed file validation failed")}
		}
		return &ExitError{Code: 1, Err: err}
	}

	// Choose the format estimator.
	var estimator contextpack.FormatEstimator
	switch formatFlag {
	case "markdown":
		estimator = contextpack.MarkdownEstimator()
	case "json":
		estimator = contextpack.JSONEstimator()
	}

	// Build the context pack.
	pack, err := contextpack.Build(contextpack.BuildOptions{
		ProjectRoot: projectRoot,
		Seeds:       seeds,
		TokenLimit:  tokenLimit,
		Graph:       g,
		Estimator:   estimator,
	})
	if err != nil {
		var seedErr *contextpack.SeedBudgetExceededError
		if errors.As(err, &seedErr) {
			return &ExitError{
				Code: 1,
				Err: fmt.Errorf("seed files exceed token budget: need at least %d tokens, limit is %d",
					seedErr.MinimumBudget, seedErr.TokenLimit),
			}
		}
		return &ExitError{Code: 1, Err: err}
	}

	// Render output.
	w := cmd.OutOrStdout()
	switch formatFlag {
	case "markdown":
		fmt.Fprint(w, contextpack.RenderMarkdown(pack))
	case "json":
		output, err := contextpack.RenderJSON(pack)
		if err != nil {
			return &ExitError{Code: 1, Err: fmt.Errorf("rendering JSON: %w", err)}
		}
		fmt.Fprint(w, output)
	}

	return nil
}
