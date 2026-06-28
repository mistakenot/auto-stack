package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/etlread"
	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/gitutil"
	"github.com/mistakenot/auto-reflect/internal/miner"
	sharedconfig "github.com/mistakenot/auto-shared/config"
	sharedgit "github.com/mistakenot/auto-shared/git"
	"github.com/spf13/cobra"
)

func newMinerCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "miner",
		Short: "Work-queue and coverage tracker for session mining",
	}

	cmd.AddCommand(
		newMinerNextCmd(application),
		newMinerAckCmd(application),
		newMinerStatusCmd(application),
		newMinerDescribeCmd(application),
		newMinerSignalsCmd(application),
	)

	return cmd
}

// etlRoot returns the canonical ETL output directory (~/.auto/etl/output).
func etlRoot() (string, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return "", fmt.Errorf("resolve auto dir: %w", err)
	}
	return filepath.Join(autoDir, "etl", "output"), nil
}

// detectRepoRoot resolves the git repo root from application CWD.
func detectRepoRoot(cwd string) (string, error) {
	repo, err := gitutil.DetectRepoLenient(cwd)
	if err != nil {
		return "", err
	}
	return repo.Root, nil
}

// checkSource validates the ETL source state and returns an appropriate error
// if the source is missing or empty.
func checkSource(cmd *cobra.Command, root string) error {
	src, err := etlread.ResolveSource(root)
	if err != nil {
		return &ExitError{Code: 1, Err: fmt.Errorf("check ETL source: %w", err)}
	}
	switch src {
	case etlread.SourceMissing:
		fmt.Fprintf(cmd.ErrOrStderr(), "ETL output not found at %s; run `auto etl run`\n", root)
		return &ExitError{Code: 1}
	case etlread.SourceEmpty:
		fmt.Fprintf(cmd.ErrOrStderr(), "ETL output is empty at %s; run `auto etl run`\n", root)
		return &ExitError{Code: 1}
	}
	return nil
}

func newMinerNextCmd(application *app.App) *cobra.Command {
	var (
		limit            int
		all              bool
		includeSubagents bool
		format           string
	)

	cmd := &cobra.Command{
		Use:   "next",
		Short: "List unmined sessions ranked by friction signals",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			root, err := etlRoot()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if err := checkSource(cmd, root); err != nil {
				return err
			}

			repoRoot, err := detectRepoRoot(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			items, err := miner.Next(repoRoot, root, miner.NextOpts{
				Limit:            limit,
				All:              all,
				IncludeSubagents: includeSubagents,
			})
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if len(items) == 0 {
				if all {
					fmt.Fprintln(cmd.ErrOrStderr(), "all sessions have been mined at the current miner version")
				} else {
					fmt.Fprintln(cmd.ErrOrStderr(), "no unmined sessions for this workspace; try --all for cross-workspace results")
				}
			}

			if outputFormat == "text" {
				for i := range items {
					it := &items[i]
					fmt.Fprintf(cmd.OutOrStdout(), "%s score=%.2f msgs=%d errors=%d corrections=%.1f%% cwd=%s\n",
						it.SessionID, it.PriorityScore, it.MessageCount, it.Signals.ToolErrorCount,
						it.Signals.CorrectionDensity, it.CWD)
				}
				return nil
			}

			// JSON: always emit [] not null
			if items == nil {
				items = []miner.WorkItem{}
			}
			if err := writeJSON(cmd.OutOrStdout(), items); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "maximum items to return (0 means all)")
	cmd.Flags().BoolVar(&all, "all", false, "include sessions from all workspaces")
	cmd.Flags().BoolVar(&includeSubagents, "include-subagents", false, "include subagent session ids in each item")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}

func newMinerAckCmd(application *app.App) *cobra.Command {
	var (
		status       string
		observations int
	)

	cmd := &cobra.Command{
		Use:   "ack <session-id>",
		Short: "Record a mining outcome for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]

			ackStatus, err := parseAckStatus(status)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			root, err := etlRoot()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if err := checkSource(cmd, root); err != nil {
				return err
			}

			repoRoot, err := detectRepoRoot(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			// Compute signals and score for the session
			row, err := miner.Describe(repoRoot, root, sessionID)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("describe session %s: %w", sessionID, err)}
			}

			score := miner.Score(row.Signals)

			payload := events.SessionMinedPayload{
				SessionID:     sessionID,
				MinerVersion:  miner.Version,
				Status:        ackStatus,
				Observations:  observations,
				PriorityScore: score,
				Signals:       row.Signals,
			}

			stored, err := events.AppendEvent(application.CWD, events.TypeSessionMined, payload, events.AppendOptions{})
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if err := writeJSON(cmd.OutOrStdout(), stored); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "mined", "mining outcome: mined|empty|failed|skipped")
	cmd.Flags().IntVar(&observations, "observations", 0, "number of observations extracted")
	return cmd
}

// minerStatus is the JSON shape for the status subcommand.
type minerStatus struct {
	TotalSessions    int                      `json:"total_sessions"`
	Mined            int                      `json:"mined"`
	Pending          int                      `json:"pending"`
	CoveragePct      *float64                 `json:"coverage_pct"`
	MinerVersion     int                      `json:"miner_version"`
	ByStatus         map[events.AckStatus]int `json:"by_status"`
	MeanObservations float64                  `json:"mean_observations"`
}

func newMinerStatusCmd(application *app.App) *cobra.Command {
	var (
		format string
		all    bool
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show mining coverage for this workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			root, err := etlRoot()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if err := checkSource(cmd, root); err != nil {
				return err
			}

			repoRoot, err := detectRepoRoot(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			// Get all sessions and events for coverage fold
			allSessions, err := etlread.ReadSessions(root)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("read sessions: %w", err)}
			}

			allEvents, err := events.ReadAll(repoRoot)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("read events: %w", err)}
			}

			coverage := miner.FoldCoverage(allEvents)

			// Pending is the single-sourced count: it comes from the SAME
			// miner.PendingCount that `reflect stats` (loop.Stats) calls, so the
			// two surfaces can never drift (task 052 F2). The session universe it
			// counts is documented on PendingCount: in-scope, top-level (non-
			// subagent) sessions not terminal at the current miner.Version
			// ("failed" acks stay retryable). checkSource already verified the
			// ETL source is OK, so src is OK here.
			// Pass `all` so the pending universe matches the total/mined loop below:
			// scoped to this repo by default, all-workspace under --all. Otherwise
			// total_sessions == pending + mined would break under --all (pending
			// stays repo-scoped while total/mined go all-workspace).
			pending, _, err := miner.PendingCount(repoRoot, root, all)
			if err != nil {
				return &ExitError{Code: 1, Err: fmt.Errorf("count pending: %w", err)}
			}

			// Scope filter for the total/mined/by_status breakdown. This mirrors
			// the scope logic in miner.PendingCount EXACTLY (same normalizeRemote,
			// same subagent + terminal-version filtering) so that, by construction,
			// total_sessions == pending + mined. Any drift here would re-introduce
			// the F2 inconsistency between the pending count and the coverage.
			var scopeRemote string
			var scopeWorkspace string
			if !all {
				repo, repoErr := gitutil.DetectRepoLenient(application.CWD)
				if repoErr == nil {
					scopeRemote = normalizeRemoteForScope(repo.Remote)
					if scopeRemote == "" {
						scopeWorkspace = repo.Root
					}
				}
			}

			totalSessions := 0
			byStatus := make(map[events.AckStatus]int)
			totalObservations := 0
			minedCount := 0

			for i := range allSessions {
				s := &allSessions[i]
				if s.IsSubagent {
					continue
				}

				// Scope filter
				if !all {
					if scopeRemote != "" {
						if normalizeRemoteForScope(s.GitRemote) != scopeRemote {
							continue
						}
					} else if scopeWorkspace != "" {
						if !strings.HasPrefix(s.Workspace, scopeWorkspace) {
							continue
						}
					}
				}

				totalSessions++

				state, ok := coverage[s.ID]
				if !ok || state.MaxTerminalVersion < miner.Version {
					// Not terminal at the current version: counted by PendingCount,
					// not part of the mined/by_status breakdown.
					continue
				}

				// Terminal at current version
				byStatus[state.LastStatus]++
				if state.LastStatus == events.AckMined {
					minedCount++
					totalObservations += state.LastObservations
				}
			}

			mined := 0
			for _, count := range byStatus {
				mined += count
			}

			var coveragePct *float64
			if totalSessions > 0 {
				pct := float64(mined) / float64(totalSessions) * 100
				coveragePct = &pct
			}

			var meanObs float64
			if minedCount > 0 {
				meanObs = float64(totalObservations) / float64(minedCount)
			}

			report := minerStatus{
				TotalSessions:    totalSessions,
				Mined:            mined,
				Pending:          pending,
				CoveragePct:      coveragePct,
				MinerVersion:     miner.Version,
				ByStatus:         byStatus,
				MeanObservations: meanObs,
			}

			if outputFormat == "text" {
				out := cmd.OutOrStdout()
				pctStr := "null"
				if coveragePct != nil {
					pctStr = fmt.Sprintf("%.1f%%", *coveragePct)
				}
				fmt.Fprintf(out, "total=%d mined=%d pending=%d coverage=%s version=%d mean_observations=%.1f\n",
					report.TotalSessions, report.Mined, report.Pending, pctStr, report.MinerVersion, report.MeanObservations)
				for status, count := range report.ByStatus {
					fmt.Fprintf(out, "  %s=%d\n", status, count)
				}
				return nil
			}

			if err := writeJSON(cmd.OutOrStdout(), report); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	cmd.Flags().BoolVar(&all, "all", false, "include sessions from all workspaces")
	return cmd
}

func newMinerDescribeCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "describe <session-id>",
		Short: "Show signals and ack history for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]

			root, err := etlRoot()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if err := checkSource(cmd, root); err != nil {
				return err
			}

			repoRoot, err := detectRepoRoot(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			row, err := miner.Describe(repoRoot, root, sessionID)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if err := writeJSON(cmd.OutOrStdout(), row); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	return cmd
}

func newMinerSignalsCmd(application *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "signals <session-id> [session-id...]",
		Short: "Show computed signals for one or more sessions",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := etlRoot()
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if err := checkSource(cmd, root); err != nil {
				return err
			}

			repoRoot, err := detectRepoRoot(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			rows, err := miner.SignalsFor(repoRoot, root, args)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			// Always emit [] not null
			if rows == nil {
				rows = []miner.SignalRow{}
			}
			if err := writeJSON(cmd.OutOrStdout(), rows); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	return cmd
}

// parseAckStatus validates the --status flag value.
func parseAckStatus(raw string) (events.AckStatus, error) {
	switch events.AckStatus(raw) {
	case events.AckMined:
		return events.AckMined, nil
	case events.AckEmpty:
		return events.AckEmpty, nil
	case events.AckFailed:
		return events.AckFailed, nil
	case events.AckSkipped:
		return events.AckSkipped, nil
	default:
		return "", fmt.Errorf("invalid --status %q: use mined|empty|failed|skipped", raw)
	}
}

// normalizeRemoteForScope produces a stable scope-comparison key from a remote
// URL. It MUST stay byte-identical to the miner package's unexported
// normalizeRemote (miner/miner.go): canonicalize via sharedgit.NormalizeRemoteURL
// (lowercases the host, converts ssh/scp forms to https, strips a trailing
// ".git" and embedded credentials), then strip the scheme for a scheme-agnostic
// key. The earlier strip-only version skipped NormalizeRemoteURL, so a
// non-canonical session remote (e.g. a ".git"-suffixed or ssh-form value that
// slipped through ETL) failed to match the canonical scope key and was dropped
// from `miner status` while miner.PendingCount still counted it — the task 052
// F2 divergence. Keeping this identical to miner.normalizeRemote keeps the
// status total/mined breakdown reconcilable with the single-sourced pending.
func normalizeRemoteForScope(raw string) string {
	n := sharedgit.NormalizeRemoteURL(raw)
	for _, prefix := range []string{"https://", "http://", "ssh://", "git://"} {
		if after, ok := strings.CutPrefix(n, prefix); ok {
			return after
		}
	}
	return n
}
