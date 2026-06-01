package cli

import (
	"encoding/json"
	"fmt"

	"github.com/mistakenot/auto-search/internal/cochange"
	"github.com/mistakenot/auto-search/internal/config"
	"github.com/mistakenot/auto-search/internal/search"
	"github.com/spf13/cobra"
)

func newCoChangeCmd() *cobra.Command {
	var repoID string
	var limit int
	var decayTau string
	var noDecay bool
	var input string
	var requestID string

	cmd := &cobra.Command{
		Use:   "co-change <path>",
		Short: "Find files temporally coupled to a given file in git history",
		Long: "Read commits/commit_files git parquet under the etl output root and return the " +
			"files that historically change together with the input file, ranked by directional " +
			"confidence weighted by lift, with a large-commit penalty and time decay.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inputPath := args[0]

			tauDays := 90.0
			if decayTau != "" {
				ms, err := search.ParseDurationMs(decayTau)
				if err != nil {
					return &ExitError{Code: 1, Err: fmt.Errorf("invalid --decay-tau: %w", err)}
				}
				tauDays = float64(ms) / float64(24*60*60*1000)
			}

			inputRoot := input
			if inputRoot == "" {
				defaultRoot, err := config.DefaultInputPath()
				if err != nil {
					return &ExitError{Code: 1, Err: err}
				}
				inputRoot = defaultRoot
			}

			result, err := cochange.Run(&cochange.Options{
				InputPath:      inputPath,
				RepoIDOverride: repoID,
				InputRoot:      inputRoot,
				TauDays:        tauDays,
				NoDecay:        noDecay,
				RequestID:      requestID,
			})
			if err != nil {
				// The typed cochange errors (ErrOutsideRepo, ErrNoOriginRemote,
				// ErrNoRepoMatch, ErrMissingParquet) already carry the failed
				// condition and a concrete remediation hint (AC-10), so they are
				// surfaced verbatim on stderr; stdout stays empty/parseable.
				return &ExitError{Code: 1, Err: err}
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		},
	}

	cmd.Flags().StringVar(&repoID, "repo-id", "", "explicit repo id (bypasses origin-remote matching)")
	cmd.Flags().IntVar(&limit, "limit", 50, "max related files to return (0 = no cap)")
	cmd.Flags().StringVar(&decayTau, "decay-tau", "90d", "time-decay constant (units m|h|d|w, e.g. 30d, 26w)")
	cmd.Flags().BoolVar(&noDecay, "no-decay", false, "disable time decay")
	cmd.Flags().StringVar(&input, "input", "", "etl output root (default: ~/.auto/etl/output)")
	cmd.Flags().StringVar(&requestID, "request-id", "", "request identifier to echo in _meta")
	return cmd
}
