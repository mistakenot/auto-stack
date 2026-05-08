package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mistakenot/auto-search/internal/config"
	"github.com/mistakenot/auto-search/internal/indexdb"
	"github.com/mistakenot/auto-search/internal/search"
	"github.com/spf13/cobra"
)

func newSkillsCmd() *cobra.Command {
	var index string
	var since string
	var after string
	var before string
	var cwd string

	cmd := &cobra.Command{
		Use:   "skills",
		Short: "List all skills used across indexed sessions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := config.IndexPath(index)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			db, err := indexdb.Open(dbPath)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("open index: %w; run: autosearch index", err)}
			}
			defer func() { _ = db.Close() }()

			tf, err := search.ParseTimeFilter(time.Now(), since, after, before)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			filter := &indexdb.SkillFilter{
				StartMs: tf.StartMs,
				EndMs:   tf.EndMs,
				CWD:     cwd,
			}

			usages, err := indexdb.ListSkillUsages(db, filter)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			type skillEntry struct {
				SkillName        string `json:"skillName"`
				Count            int    `json:"count"`
				DistinctSessions int    `json:"distinctSessions"`
			}

			entries := make([]skillEntry, len(usages))
			for i, u := range usages {
				entries[i] = skillEntry{
					SkillName:        u.SkillName,
					Count:            u.Count,
					DistinctSessions: u.DistinctSessions,
				}
			}

			out := map[string]any{
				"skills": entries,
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		},
	}
	cmd.Flags().StringVar(&index, "index", config.DefaultIndexName, "named index to query")
	cmd.Flags().StringVar(&since, "since", "", "relative time filter (e.g. 7d, 1w)")
	cmd.Flags().StringVar(&after, "after", "", "inclusive lower date bound (ISO 8601)")
	cmd.Flags().StringVar(&before, "before", "", "exclusive upper date bound (ISO 8601)")
	cmd.Flags().StringVar(&cwd, "cwd", "", "workspace path filter")
	return cmd
}
