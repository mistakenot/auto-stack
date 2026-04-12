package cli

import (
	"encoding/json"
	"fmt"

	"github.com/mistakenot/auto-search/internal/config"
	"github.com/mistakenot/auto-search/internal/indexdb"
	"github.com/spf13/cobra"
)

func newSkillsCmd() *cobra.Command {
	var index string

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

			usages, err := indexdb.ListSkillUsages(db)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			type skillEntry struct {
				SkillName string `json:"skillName"`
				Count     int    `json:"count"`
			}

			entries := make([]skillEntry, len(usages))
			for i, u := range usages {
				entries[i] = skillEntry{SkillName: u.SkillName, Count: u.Count}
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
	return cmd
}
