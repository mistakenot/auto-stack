package cli

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/gitutil"
	"github.com/mistakenot/auto-reflect/internal/timefilter"
	"github.com/spf13/cobra"
)

// gapRow is one readable feedback gap, projected from a feedback event whose
// payload carried a non-nil Gap. The top-level `id` is the feedback EVENT id
// (`ev-…`) — the `fb-` loop feedback id is not persisted on the event, so the
// envelope id is the only stable id available (resolved review thread, AC-1/AC-5).
type gapRow struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id,omitempty"`
	TS        string `json:"ts"`
	Report    string `json:"report"`
	Moment    string `json:"moment"`
}

func newGapCmd(application *app.App) *cobra.Command {
	gapCmd := &cobra.Command{
		Use:   "gap",
		Short: "Read feedback gaps captured during the retrieval loop (read-only)",
	}
	gapCmd.AddCommand(newGapListCmd(application))
	return gapCmd
}

func newGapListCmd(application *app.App) *cobra.Command {
	var (
		since  string
		after  string
		before string
		domain []string
		format string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List feedback gaps (newest-first), filtered by time",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			window, err := timefilter.Parse(time.Now().UTC(), since, after, before)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			// Feedback events carry no domain dimension (FeedbackPayload is
			// outcome/summary/rankings/gap; FeedbackGap is report/moment only).
			// Rather than silently returning [] for a filter that can never match,
			// fail fast with a remediation hint pointing at the domain-scoped surface.
			if len(normalizeFilterTags(domain)) > 0 {
				return &ExitError{Code: 1, Err: errors.New("--domain is not supported by `gap list`: feedback gaps carry no domain; use `auto reflect observation list --kind gap --domain <tag>` for domain-scoped gap observations")}
			}

			repo, err := gitutil.DetectRepoLenient(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			all, err := events.ReadAll(repo.Root)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			// Collect feedback events that actually reported a gap, preserving the
			// envelope so we can sort newest-first deterministically before projecting.
			matches := make([]events.Event, 0)
			for i := range all {
				e := all[i]
				if e.Type != events.TypeFeedback {
					continue
				}
				var p events.FeedbackPayload
				if decodeEventPayload(&e, &p) != nil {
					continue
				}
				if p.Gap == nil {
					continue
				}
				if !withinWindow(e.TS, window) {
					continue
				}
				matches = append(matches, e)
			}

			// Newest-first: descending by ts, then seq, then id — same total order
			// tie-break as `events list`, so the row stream is stable across shards.
			sort.SliceStable(matches, func(i, j int) bool {
				if matches[i].TS != matches[j].TS {
					return matches[i].TS > matches[j].TS
				}
				if matches[i].Seq != matches[j].Seq {
					return matches[i].Seq > matches[j].Seq
				}
				return matches[i].ID > matches[j].ID
			})

			rows := make([]gapRow, 0, len(matches))
			for i := range matches {
				e := matches[i]
				var p events.FeedbackPayload
				if decodeEventPayload(&e, &p) != nil || p.Gap == nil {
					continue
				}
				rows = append(rows, gapRow{
					ID:        e.ID,
					SessionID: e.SessionID,
					TS:        e.TS,
					Report:    p.Gap.Report,
					Moment:    p.Gap.Moment,
				})
			}

			if outputFormat == "text" {
				for i := range rows {
					printGapLine(cmd, &rows[i])
				}
				return nil
			}
			if err := writeJSON(cmd.OutOrStdout(), rows); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&since, "since", "", "relative window, e.g. 5m, 12h, 7d, 1w")
	cmd.Flags().StringVar(&after, "after", "", "absolute lower bound (YYYY-MM-DD or RFC3339)")
	cmd.Flags().StringVar(&before, "before", "", "absolute upper bound (YYYY-MM-DD or RFC3339)")
	cmd.Flags().StringSliceVar(&domain, "domain", nil, "filter by domain tag(s); unsupported — feedback gaps carry no domain (errors if set)")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}

func printGapLine(cmd *cobra.Command, row *gapRow) {
	session := row.SessionID
	if session == "" {
		session = "-"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s %s [%s] %s — %s\n", row.TS, row.ID, session, row.Report, row.Moment)
}
