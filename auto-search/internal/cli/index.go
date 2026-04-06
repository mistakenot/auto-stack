package cli

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/mistakenot/auto-search/internal/config"
	"github.com/mistakenot/auto-search/internal/indexdb"
	"github.com/spf13/cobra"
)

func newIndexCmd() *cobra.Command {
	defaultInput := ""
	if path, err := config.DefaultInputPath(); err == nil {
		defaultInput = path
	}

	var name string
	var input string
	var key string

	cmd := &cobra.Command{
		Use:   "index",
		Short: "Build or update a local search index from autoetl parquet output",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(input) == "" {
				return &ExitError{
					Code: 1,
					Err:  errors.New("--input is required; run: autosearch init"),
				}
			}

			dbPath, err := config.IndexPath(name)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			stderr := cmd.ErrOrStderr()
			result, err := indexdb.IncrementalUpdate(dbPath, input, stderr)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			out := map[string]any{
				"index":              name,
				"path":               dbPath,
				"full_rebuild":       result.FullRebuild,
				"files_processed":    result.FilesProcessed,
				"partitions_skipped": result.PartitionsSkipped,
				"sessions_indexed":   result.SessionsIndexed,
				"messages_indexed":   result.MessagesIndexed,
				"sessions_skipped":   result.SessionsSkipped,
				"messages_skipped":   result.MessagesSkipped,
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		},
	}
	cmd.Flags().StringVar(&name, "name", config.DefaultIndexName, "named index to build")
	cmd.Flags().StringVar(&input, "input", defaultInput, "input parquet root to index")
	cmd.Flags().StringVar(&key, "key", "", "optional key path for remote inputs")
	return cmd
}
