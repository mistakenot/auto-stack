package cli_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	sharedmodel "github.com/mistakenot/auto-shared/model"
	"github.com/parquet-go/parquet-go"

	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/miner"
)

// writeSessionsParquet writes AgentSession rows to <etlRoot>/sessions/data.parquet,
// matching the layout etlread.ReadSessions expects.
func writeSessionsParquet(t *testing.T, etlRoot string, sessions []sharedmodel.AgentSession) {
	t.Helper()
	dir := filepath.Join(etlRoot, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, "data.parquet"))
	if err != nil {
		t.Fatalf("create parquet: %v", err)
	}
	defer func() { _ = f.Close() }()
	w := parquet.NewGenericWriter[sharedmodel.AgentSession](f)
	if _, err := w.Write(sessions); err != nil {
		t.Fatalf("write parquet rows: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close parquet writer: %v", err)
	}
}

// appendMinedEvent appends a single session_mined event to the repo's event log.
func appendMinedEvent(t *testing.T, repo, sessionID string, status events.AckStatus, seq int) {
	t.Helper()
	payload := events.SessionMinedPayload{
		SessionID:    sessionID,
		MinerVersion: miner.Version,
		Status:       status,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	ev := events.Event{
		ID:            fmt.Sprintf("ev-%08x", seq),
		Type:          events.TypeSessionMined,
		SchemaVersion: 1,
		Seq:           seq,
		TS:            fmt.Sprintf("2026-01-01T00:00:%02dZ", seq),
		Host:          "test-host",
		Payload:       raw,
	}
	dir := filepath.Join(repo, ".auto", "reflect", "events")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir events: %v", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "fixture.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open events shard: %v", err)
	}
	defer func() { _ = f.Close() }()
	data, err := json.Marshal(&ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		t.Fatalf("write event: %v", err)
	}
}

// TestMinerStatusPendingMatchesStats is the F2 regression test: `miner status`
// and `stats` must report the SAME pending count over one fixture.
//
// Root cause this pins: the repo's origin remote here is the SSH form
// (git@github.com:example/auto-stack.git), while ETL session rows store the
// canonical https form. `stats` -> miner.PendingCount normalizes both through
// sharedgit.NormalizeRemoteURL, so the SSH remote and the https session rows
// resolve to the same scope key and the in-scope sessions are counted. The old
// hand-rolled `miner status` loop normalized with a weaker local helper that
// skipped NormalizeRemoteURL, so the SSH scope key never matched the https
// session rows and every in-scope session was dropped -> status.pending = 0
// while stats.pending_to_mine = 2. Collapsing both onto miner.PendingCount makes
// divergence impossible.
func TestMinerStatusPendingMatchesStats(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t) // origin = git@github.com:example/auto-stack.git (SSH form)
	writeFile(t, filepath.Join(repo, "README.md"), "seed\n")
	gitAddCommit(t, repo, "seed")

	if _, stderr, code := runCLIAt(t, repo, "init"); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, stderr)
	}

	// ETL sessions store the normalized https remote (as auto-etl writes it).
	const remote = "https://github.com/example/auto-stack"
	// s2/s3 carry a non-canonical .git-suffixed remote: NormalizeRemoteURL maps
	// it to the same scope key, but the old strip-only CLI helper does not — this
	// is the variance that produced the 390-vs-396 divergence in real ETL data.
	const remoteDotGit = "https://github.com/example/auto-stack.git"
	etlRoot := filepath.Join(home, ".auto", "etl", "output")
	writeSessionsParquet(t, etlRoot, []sharedmodel.AgentSession{
		{ID: "s1", Workspace: repo, GitRemote: remote, IsSubagent: false, LastMessageAt: 1000},
		{ID: "s2", Workspace: repo, GitRemote: remoteDotGit, IsSubagent: false, LastMessageAt: 2000},
		{ID: "s3", Workspace: repo, GitRemote: remoteDotGit, IsSubagent: false, LastMessageAt: 3000},
		{ID: "s4", Workspace: repo, GitRemote: remote, IsSubagent: false, LastMessageAt: 4000},
		// subagent must be ignored by both counters
		{ID: "sub", Workspace: repo, GitRemote: remote, IsSubagent: true, ParentSessionID: "s1", LastMessageAt: 1500},
		// other repo must be out of scope for both counters
		{ID: "other", Workspace: "/tmp/other", GitRemote: "https://github.com/example/other", IsSubagent: false, LastMessageAt: 5000},
	})

	// Partial coverage: s1 terminal (mined), s4 terminal (empty); s2 failed
	// (retryable -> pending), s3 never acked (-> pending). Pending = {s2, s3} = 2.
	appendMinedEvent(t, repo, "s1", events.AckMined, 1)
	appendMinedEvent(t, repo, "s4", events.AckEmpty, 2)
	appendMinedEvent(t, repo, "s2", events.AckFailed, 3)

	statusPending, coveragePresent := minerStatusPending(t, repo)
	statsPending := statsPendingToMine(t, repo)

	if statsPending == nil {
		t.Fatalf("stats did not report pending_to_mine (expected non-null with ETL present)")
	}
	if statusPending != *statsPending {
		t.Fatalf("F2 divergence: miner status pending=%d but stats pending_to_mine=%d", statusPending, *statsPending)
	}
	if statusPending != 2 {
		t.Fatalf("expected pending=2 (s2 failed + s3 unacked), got %d", statusPending)
	}
	// 023 contract: in-scope sessions exist, so coverage_pct must be a number, not null.
	if !coveragePresent {
		t.Fatalf("expected coverage_pct to be non-null when in-scope sessions exist")
	}
}

// TestMinerStatusCoveragePctNullWhenNoInScopeSessions verifies the F2 collapse
// preserves the 023 null-vs-zero contract: when there are zero in-scope
// sessions, coverage_pct is null (not 0), and pending agrees with stats (0).
func TestMinerStatusCoveragePctNullWhenNoInScopeSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "seed\n")
	gitAddCommit(t, repo, "seed")

	if _, stderr, code := runCLIAt(t, repo, "init"); code != 0 {
		t.Fatalf("init failed: code=%d stderr=%s", code, stderr)
	}

	etlRoot := filepath.Join(home, ".auto", "etl", "output")
	// Only an out-of-scope session exists -> zero in-scope sessions.
	writeSessionsParquet(t, etlRoot, []sharedmodel.AgentSession{
		{ID: "other", Workspace: "/tmp/other", GitRemote: "https://github.com/example/other", IsSubagent: false, LastMessageAt: 5000},
	})

	statusPending, coveragePresent := minerStatusPending(t, repo)
	statsPending := statsPendingToMine(t, repo)

	if statsPending == nil {
		t.Fatalf("stats did not report pending_to_mine")
	}
	if statusPending != *statsPending {
		t.Fatalf("F2 divergence: miner status pending=%d but stats pending_to_mine=%d", statusPending, *statsPending)
	}
	if statusPending != 0 {
		t.Fatalf("expected pending=0 with no in-scope sessions, got %d", statusPending)
	}
	if coveragePresent {
		t.Fatalf("expected coverage_pct to be null (not 0) when total_sessions==0")
	}
}

// minerStatusPending runs `miner status` and returns its pending count plus
// whether coverage_pct is non-null.
func minerStatusPending(t *testing.T, repo string) (pending int, coveragePresent bool) {
	t.Helper()
	stdout, stderr, code := runCLIAt(t, repo, "miner", "status")
	if code != 0 {
		t.Fatalf("miner status failed: code=%d stderr=%s", code, stderr)
	}
	var resp struct {
		Pending     int      `json:"pending"`
		CoveragePct *float64 `json:"coverage_pct"`
	}
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("decode miner status: %v\nraw:\n%s", err, stdout)
	}
	return resp.Pending, resp.CoveragePct != nil
}

// statsPendingToMine runs `stats` and returns its pending_to_mine field.
func statsPendingToMine(t *testing.T, repo string) *int {
	t.Helper()
	stdout, stderr, code := runCLIAt(t, repo, "stats")
	if code != 0 {
		t.Fatalf("stats failed: code=%d stderr=%s", code, stderr)
	}
	var resp struct {
		PendingToMine *int `json:"pending_to_mine"`
	}
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("decode stats: %v\nraw:\n%s", err, stdout)
	}
	return resp.PendingToMine
}
