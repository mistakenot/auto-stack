package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/gitutil"
	"github.com/mistakenot/auto-reflect/internal/rules"
	"github.com/mistakenot/auto-reflect/internal/store"
	"github.com/spf13/cobra"
)

// doctorCheck is one structured health-check result. Mirrors
// auto-graph/internal/cli/doctor.go: status is one of pass|warn|fail and any
// fail makes the process exit non-zero. Hint carries a concrete remediation.
type doctorCheck struct {
	Check   string `json:"check"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func newDoctorCmd(application *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check auto reflect state health (state dir, events, snapshot freshness)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, err := gitutil.DetectRepoLenient(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			checks := runDoctorChecks(repo.Root)

			// stdout carries only the JSON payload; any failure exits non-zero.
			if err := writeJSON(cmd.OutOrStdout(), checks); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			for i := range checks {
				if checks[i].Status == "fail" {
					return &ExitError{Code: 1}
				}
			}
			return nil
		},
	}
}

// runDoctorChecks inspects the reflect state under repoRoot and returns one
// result per check. The order is fixed (state dir, events, snapshot, legacy
// feedback) so output is deterministic across runs.
func runDoctorChecks(repoRoot string) []doctorCheck {
	// EventsDir is repoRoot/.auto/reflect/events; its parent is the state dir.
	eventsDir := store.EventsDir(repoRoot)
	stateDir := filepath.Dir(eventsDir)
	playbookPath := store.PlaybookPath(repoRoot)

	return []doctorCheck{
		checkStateDir(stateDir),
		checkEvents(repoRoot, eventsDir),
		checkSnapshot(repoRoot, playbookPath),
		checkLegacyFeedback(stateDir),
	}
}

// checkStateDir confirms the .auto/reflect state dir exists. It is the root of
// all reflect state, so a missing dir means the repo was never initialized.
func checkStateDir(stateDir string) doctorCheck {
	if info, err := os.Stat(stateDir); err == nil && info.IsDir() {
		return doctorCheck{
			Check:   "state_dir",
			Status:  "pass",
			Message: "state dir present at " + stateDir,
		}
	}
	return doctorCheck{
		Check:   "state_dir",
		Status:  "fail",
		Message: "state dir " + stateDir + " is missing",
		Hint:    "run auto reflect init",
	}
}

// checkEvents confirms the events/ dir exists and every shard decodes. The
// events log is canonical (the playbook snapshot is derivable from it), so a
// missing or undecodable log is a hard failure.
func checkEvents(repoRoot, eventsDir string) doctorCheck {
	info, err := os.Stat(eventsDir)
	if err != nil || !info.IsDir() {
		return doctorCheck{
			Check:   "events",
			Status:  "fail",
			Message: "events directory " + eventsDir + " is missing",
			Hint:    "run auto reflect init",
		}
	}

	// ReadAll decodes every line of every shard; a decode error names the shard.
	if _, err := events.ReadAll(repoRoot); err != nil {
		return doctorCheck{
			Check:   "events",
			Status:  "fail",
			Message: "events log is not decodable: " + err.Error(),
			Hint:    "inspect the named shard in " + eventsDir + " for a corrupt line",
		}
	}

	shards, err := countShards(eventsDir)
	if err != nil {
		return doctorCheck{
			Check:   "events",
			Status:  "fail",
			Message: "cannot read events directory " + eventsDir + ": " + err.Error(),
			Hint:    "check filesystem permissions on " + eventsDir,
		}
	}
	return doctorCheck{
		Check:   "events",
		Status:  "pass",
		Message: fmt.Sprintf("%d shard(s), all decodable", shards),
	}
}

// checkSnapshot reports whether playbook.json is fresh relative to the folded
// event log. A missing snapshot warns (it is a disposable cache, rebuildable);
// a snapshot lagging the log's rule events warns to rebuild.
func checkSnapshot(repoRoot, playbookPath string) doctorCheck {
	if _, err := os.Stat(playbookPath); err != nil {
		return doctorCheck{
			Check:   "playbook_snapshot",
			Status:  "warn",
			Message: "playbook.json not found at " + playbookPath,
			Hint:    "run auto reflect rebuild",
		}
	}

	stale, foldedShards, err := snapshotStale(repoRoot, playbookPath)
	if err != nil {
		return doctorCheck{
			Check:   "playbook_snapshot",
			Status:  "fail",
			Message: "cannot evaluate playbook snapshot: " + err.Error(),
			Hint:    "run auto reflect rebuild",
		}
	}
	if stale {
		return doctorCheck{
			Check:   "playbook_snapshot",
			Status:  "warn",
			Message: "playbook.json is stale relative to the folded event log",
			Hint:    "run auto reflect rebuild",
		}
	}
	return doctorCheck{
		Check:   "playbook_snapshot",
		Status:  "pass",
		Message: fmt.Sprintf("snapshot fresh (folded through %d shard(s))", foldedShards),
	}
}

// checkLegacyFeedback flags a leftover legacy feedback.jsonl in the state dir.
// Feedback now lives in events/, so any such file (commonly a stale 0-byte
// stub) is dead weight and should be removed.
func checkLegacyFeedback(stateDir string) doctorCheck {
	legacy := filepath.Join(stateDir, "feedback.jsonl")
	info, err := os.Stat(legacy)
	if err != nil {
		return doctorCheck{
			Check:   "legacy_feedback",
			Status:  "pass",
			Message: "no legacy feedback.jsonl in state dir",
		}
	}
	detail := fmt.Sprintf("legacy feedback.jsonl present (%d bytes)", info.Size())
	if info.Size() == 0 {
		detail = "legacy 0-byte feedback.jsonl present"
	}
	return doctorCheck{
		Check:   "legacy_feedback",
		Status:  "warn",
		Message: detail + "; feedback is now stored in events/",
		Hint:    "remove " + legacy,
	}
}

// snapshotStale replicates rules.isStale (unexported) against exported parts:
// it compares the snapshot's folded_through per-shard high-water marks to the
// event log's per-shard rule-event seqs. A shard whose rule events advance past
// folded_through — or a snapshot schema mismatch — means the snapshot is stale.
// foldedShards is the number of shards the snapshot has folded, for reporting.
func snapshotStale(repoRoot, playbookPath string) (stale bool, foldedShards int, err error) {
	snapshot, err := rules.LoadSnapshot(playbookPath)
	if err != nil {
		return false, 0, err
	}
	foldedShards = len(snapshot.FoldedThrough)

	if snapshot.SchemaVersion != rules.SchemaVersion {
		return true, foldedShards, nil
	}

	sharded, err := events.ReadAllSharded(repoRoot)
	if err != nil {
		return false, foldedShards, err
	}

	current := make(map[string]int)
	for i := range sharded {
		se := &sharded[i]
		if !events.IsRuleEvent(se.Event.Type) {
			continue
		}
		if se.Event.Seq > current[se.Shard] {
			current[se.Shard] = se.Event.Seq
		}
	}
	for shard, seq := range current {
		if snapshot.FoldedThrough[shard] < seq {
			return true, foldedShards, nil
		}
	}
	return false, foldedShards, nil
}

func countShards(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			n++
		}
	}
	return n, nil
}
