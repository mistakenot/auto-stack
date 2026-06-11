package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sharedmodel "github.com/mistakenot/auto-shared/model"
	"github.com/parquet-go/parquet-go"
)

// minerE2EFixture holds the temp dirs for a miner e2e test.
type minerE2EFixture struct {
	Repo    string // git repo root (CWD for the binary)
	Home    string // fake HOME dir; ETL lives at $HOME/.auto/etl/output
	ETLRoot string // shortcut to $HOME/.auto/etl/output
}

// setupMinerE2E creates a git repo, a fake HOME with ETL parquet fixtures, and
// sets HOME so the binary resolves the ETL output correctly.
func setupMinerE2E(t *testing.T) minerE2EFixture {
	t.Helper()

	home := t.TempDir()
	repo := initE2ERepo(t)

	// Seed commit so HEAD exists (required by init --project)
	writeE2EFile(t, filepath.Join(repo, "README.md"), "seed\n")
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	// Set HOME before running the binary so sharedconfig.AutoDir() resolves
	// to our temp dir, not the real HOME.
	t.Setenv("HOME", home)

	etlRoot := filepath.Join(home, ".auto", "etl", "output")

	// Create sessions parquet with 3 sessions:
	// - sess-a: same remote as repo, should be in scope
	// - sess-b: same remote as repo, should be in scope
	// - sess-c: different remote, out of default scope
	sessions := []sharedmodel.AgentSession{
		{
			ID:            "sess-a",
			Workspace:     repo,
			GitRemote:     "https://github.com/example/auto-stack",
			IsSubagent:    false,
			LastMessageAt: 1000,
		},
		{
			ID:            "sess-b",
			Workspace:     repo,
			GitRemote:     "https://github.com/example/auto-stack",
			IsSubagent:    false,
			LastMessageAt: 2000,
		},
		{
			ID:            "sess-c",
			Workspace:     "/tmp/other",
			GitRemote:     "https://github.com/example/other.git",
			IsSubagent:    false,
			LastMessageAt: 3000,
		},
	}
	writeParquetFixture(t, filepath.Join(etlRoot, "sessions", "data.parquet"), sessions)

	// Create messages parquet
	messages := []sharedmodel.AgentMessage{
		{ID: "m1", SessionID: "sess-a", Role: "user", ContentTruncated: "fix the build"},
		{ID: "m2", SessionID: "sess-a", Role: "assistant", ContentTruncated: "ok let me try"},
		{ID: "m3", SessionID: "sess-a", Role: "user", ContentTruncated: "no that's wrong, try again"},
		{ID: "m4", SessionID: "sess-b", Role: "user", ContentTruncated: "refactor the module"},
		{ID: "m5", SessionID: "sess-b", Role: "tool", ToolName: "Bash", IsError: true, ContentTruncated: "error: compile failed"},
		{ID: "m6", SessionID: "sess-c", Role: "user", ContentTruncated: "hello other repo"},
	}
	writeParquetFixture(t, filepath.Join(etlRoot, "messages", "data.parquet"), messages)

	// Also create .auto/reflect/events/ dir so events.ReadAll doesn't error
	eventsDir := filepath.Join(repo, ".auto", "reflect", "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("mkdir events: %v", err)
	}

	return minerE2EFixture{
		Repo:    repo,
		Home:    home,
		ETLRoot: etlRoot,
	}
}

func writeParquetFixture[T any](t *testing.T, path string, rows []T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	w := parquet.NewGenericWriter[T](f)
	if _, err := w.Write(rows); err != nil {
		t.Fatalf("write rows: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
}

// TestE2E_MinerNext verifies that `miner next` returns ranked, in-scope sessions.
func TestE2E_MinerNext(t *testing.T) {
	fix := setupMinerE2E(t)

	stdout, stderr, err := runBinary(fix.Repo, "miner", "next")
	if err != nil {
		t.Fatalf("miner next failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	var items []map[string]any
	if jerr := json.Unmarshal([]byte(stdout), &items); jerr != nil {
		t.Fatalf("decode miner next json: %v\nraw:\n%s", jerr, stdout)
	}

	// Default scope: only sess-a and sess-b (same remote as repo)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d\nraw:\n%s", len(items), stdout)
	}

	// Each item must have the required fields
	for _, item := range items {
		requireFields(t, item, "session_id", "priority_score", "signals", "fetch_cmd")
	}

	// Items should be sorted by descending priority score
	if len(items) >= 2 {
		s0, _ := items[0]["priority_score"].(float64)
		s1, _ := items[1]["priority_score"].(float64)
		if s1 > s0 {
			t.Errorf("items not sorted by descending score: [0]=%f [1]=%f", s0, s1)
		}
	}
}

// TestE2E_MinerNextAll verifies --all widens scope to all workspaces.
func TestE2E_MinerNextAll(t *testing.T) {
	fix := setupMinerE2E(t)

	stdout, stderr, err := runBinary(fix.Repo, "miner", "next", "--all")
	if err != nil {
		t.Fatalf("miner next --all failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	var items []map[string]any
	if jerr := json.Unmarshal([]byte(stdout), &items); jerr != nil {
		t.Fatalf("decode json: %v\nraw:\n%s", jerr, stdout)
	}

	// --all: sess-a, sess-b, sess-c
	if len(items) != 3 {
		t.Fatalf("expected 3 items with --all, got %d\nraw:\n%s", len(items), stdout)
	}
}

// TestE2E_MinerAckThenNext verifies that acking a session excludes it from next.
func TestE2E_MinerAckThenNext(t *testing.T) {
	fix := setupMinerE2E(t)

	// Init the project so events can be written
	if stdout, stderr, err := runBinary(fix.Repo, "init", "--project"); err != nil {
		t.Fatalf("init --project failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	// Get initial list
	nextOut, _, err := runBinary(fix.Repo, "miner", "next")
	if err != nil {
		t.Fatalf("miner next failed: %v", err)
	}
	var before []map[string]any
	if jerr := json.Unmarshal([]byte(nextOut), &before); jerr != nil {
		t.Fatalf("decode json: %v\nraw:\n%s", jerr, nextOut)
	}
	if len(before) != 2 {
		t.Fatalf("expected 2 items before ack, got %d", len(before))
	}

	// Ack the first session
	firstID := before[0]["session_id"].(string)
	ackOut, ackStderr, ackErr := runBinary(fix.Repo, "miner", "ack", firstID, "--status", "mined", "--observations", "1")
	if ackErr != nil {
		t.Fatalf("miner ack failed: %v\nstdout:\n%s\nstderr:\n%s", ackErr, ackOut, ackStderr)
	}

	// Verify ack response is valid JSON
	var ackResp map[string]any
	if jerr := json.Unmarshal([]byte(ackOut), &ackResp); jerr != nil {
		t.Fatalf("ack response not JSON: %v\nraw:\n%s", jerr, ackOut)
	}

	// Next should now exclude the acked session
	nextOut2, _, err := runBinary(fix.Repo, "miner", "next")
	if err != nil {
		t.Fatalf("miner next after ack failed: %v", err)
	}
	var after []map[string]any
	if jerr := json.Unmarshal([]byte(nextOut2), &after); jerr != nil {
		t.Fatalf("decode json: %v\nraw:\n%s", jerr, nextOut2)
	}
	if len(after) != 1 {
		t.Fatalf("expected 1 item after ack, got %d\nraw:\n%s", len(after), nextOut2)
	}
	if after[0]["session_id"].(string) == firstID {
		t.Fatalf("acked session %s should not appear in next results", firstID)
	}
}

// TestE2E_MinerStatus verifies the status command shows coverage counts.
func TestE2E_MinerStatus(t *testing.T) {
	fix := setupMinerE2E(t)

	// Init the project
	if stdout, stderr, err := runBinary(fix.Repo, "init", "--project"); err != nil {
		t.Fatalf("init --project failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	// Before any acks, all sessions pending
	statusOut, statusStderr, err := runBinary(fix.Repo, "miner", "status")
	if err != nil {
		t.Fatalf("miner status failed: %v\nstderr:\n%s", err, statusStderr)
	}
	var status struct {
		TotalSessions int      `json:"total_sessions"`
		Mined         int      `json:"mined"`
		Pending       int      `json:"pending"`
		MinerVersion  int      `json:"miner_version"`
		CoveragePct   *float64 `json:"coverage_pct"`
	}
	if jerr := json.Unmarshal([]byte(statusOut), &status); jerr != nil {
		t.Fatalf("decode status json: %v\nraw:\n%s", jerr, statusOut)
	}
	if status.TotalSessions != 2 {
		t.Errorf("total_sessions = %d, want 2", status.TotalSessions)
	}
	if status.Pending != 2 {
		t.Errorf("pending = %d, want 2", status.Pending)
	}

	// Ack one, then check status
	nextOut, _, _ := runBinary(fix.Repo, "miner", "next")
	var items []map[string]any
	_ = json.Unmarshal([]byte(nextOut), &items)
	if len(items) > 0 {
		firstID := items[0]["session_id"].(string)
		runBinary(fix.Repo, "miner", "ack", firstID, "--status", "mined", "--observations", "2")
	}

	statusOut2, _, err := runBinary(fix.Repo, "miner", "status")
	if err != nil {
		t.Fatalf("miner status after ack failed: %v", err)
	}
	var status2 struct {
		Mined   int `json:"mined"`
		Pending int `json:"pending"`
	}
	if jerr := json.Unmarshal([]byte(statusOut2), &status2); jerr != nil {
		t.Fatalf("decode status2 json: %v\nraw:\n%s", jerr, statusOut2)
	}
	if status2.Mined != 1 {
		t.Errorf("after ack: mined = %d, want 1", status2.Mined)
	}
	if status2.Pending != 1 {
		t.Errorf("after ack: pending = %d, want 1", status2.Pending)
	}
}

// TestE2E_MinerDescribe verifies describe returns signals and ack history.
func TestE2E_MinerDescribe(t *testing.T) {
	fix := setupMinerE2E(t)

	// Init the project
	if stdout, stderr, err := runBinary(fix.Repo, "init", "--project"); err != nil {
		t.Fatalf("init --project failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	// Describe sess-a
	descOut, descStderr, err := runBinary(fix.Repo, "miner", "describe", "sess-a")
	if err != nil {
		t.Fatalf("miner describe failed: %v\nstdout:\n%s\nstderr:\n%s", err, descOut, descStderr)
	}

	var row struct {
		SessionID  string         `json:"session_id"`
		Signals    map[string]any `json:"signals"`
		AckHistory []any          `json:"ack_history"`
	}
	if jerr := json.Unmarshal([]byte(descOut), &row); jerr != nil {
		t.Fatalf("decode describe json: %v\nraw:\n%s", jerr, descOut)
	}
	if row.SessionID != "sess-a" {
		t.Errorf("session_id = %q, want %q", row.SessionID, "sess-a")
	}
	if row.Signals == nil {
		t.Error("signals should not be nil")
	}

	// Ack then describe again — ack history should be non-empty
	runBinary(fix.Repo, "miner", "ack", "sess-a", "--status", "mined", "--observations", "1")
	descOut2, _, err := runBinary(fix.Repo, "miner", "describe", "sess-a")
	if err != nil {
		t.Fatalf("miner describe after ack failed: %v", err)
	}
	var row2 struct {
		AckHistory []map[string]any `json:"ack_history"`
	}
	if jerr := json.Unmarshal([]byte(descOut2), &row2); jerr != nil {
		t.Fatalf("decode describe2 json: %v\nraw:\n%s", jerr, descOut2)
	}
	if len(row2.AckHistory) != 1 {
		t.Fatalf("expected 1 ack history entry, got %d", len(row2.AckHistory))
	}
}

// TestE2E_MinerSourceMissing verifies non-zero exit when ETL dir doesn't exist.
func TestE2E_MinerSourceMissing(t *testing.T) {
	repo := initE2ERepo(t)
	writeE2EFile(t, filepath.Join(repo, "README.md"), "seed\n")
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	// Set HOME to a temp dir with no ETL data
	noETLHome := t.TempDir()
	t.Setenv("HOME", noETLHome)

	_, stderr, err := runBinary(repo, "miner", "next")
	if err == nil {
		t.Fatal("expected non-zero exit when ETL source is missing")
	}
	if len(stderr) == 0 {
		t.Error("expected stderr message about missing ETL")
	}
}

// TestE2E_StatsIncludesPendingToMine verifies reflect stats includes the
// pending_to_mine field with graceful degradation.
func TestE2E_StatsIncludesPendingToMine(t *testing.T) {
	fix := setupMinerE2E(t)

	// Init the project so stats works
	if stdout, stderr, err := runBinary(fix.Repo, "init", "--project"); err != nil {
		t.Fatalf("init --project failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	// Create a rule so stats has something to report
	e2eCreateRule(t, fix.Repo,
		"--use-when", "testing miner stats",
		"--content", "verify pending_to_mine",
		"--causal-note", "e2e coverage",
		"--domain", "test",
		"--type", "soft",
	)

	// Stats should include pending_to_mine with a count
	statsOut, statsStderr, err := runBinary(fix.Repo, "stats")
	if err != nil {
		t.Fatalf("stats failed: %v\nstderr:\n%s", err, statsStderr)
	}

	var report struct {
		UnconsolidatedObservations int  `json:"unconsolidated_observations"`
		PendingToMine              *int `json:"pending_to_mine"`
	}
	if jerr := json.Unmarshal([]byte(statsOut), &report); jerr != nil {
		t.Fatalf("decode stats json: %v\nraw:\n%s", jerr, statsOut)
	}
	if report.PendingToMine == nil {
		t.Fatal("pending_to_mine should not be null when ETL data is present")
	}
	if *report.PendingToMine != 2 {
		t.Errorf("pending_to_mine = %d, want 2 (sess-a + sess-b in scope)", *report.PendingToMine)
	}

	// Text format should also include pending_to_mine
	textOut, _, err := runBinary(fix.Repo, "stats", "--format", "text")
	if err != nil {
		t.Fatalf("stats --format text failed: %v", err)
	}
	if !containsLine(textOut, "pending_to_mine=2") {
		t.Errorf("text output should contain 'pending_to_mine=2', got:\n%s", textOut)
	}
}

// TestE2E_StatsGracefulDegradation verifies that stats returns pending_to_mine=null
// when ETL data is not available, but does NOT error.
func TestE2E_StatsGracefulDegradation(t *testing.T) {
	repo := initE2ERepo(t)
	writeE2EFile(t, filepath.Join(repo, "README.md"), "seed\n")
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	// Set HOME to a dir with no ETL
	noETLHome := t.TempDir()
	t.Setenv("HOME", noETLHome)

	// Init the project
	if stdout, stderr, err := runBinary(repo, "init", "--project"); err != nil {
		t.Fatalf("init --project failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	// Stats must succeed even without ETL data
	statsOut, statsStderr, err := runBinary(repo, "stats")
	if err != nil {
		t.Fatalf("stats should not error when ETL is missing: %v\nstderr:\n%s", err, statsStderr)
	}

	var report struct {
		PendingToMine *int `json:"pending_to_mine"`
	}
	if jerr := json.Unmarshal([]byte(statsOut), &report); jerr != nil {
		t.Fatalf("decode stats json: %v\nraw:\n%s", jerr, statsOut)
	}
	if report.PendingToMine != nil {
		t.Errorf("pending_to_mine should be null when ETL is missing, got %d", *report.PendingToMine)
	}

	// Text format
	textOut, _, err := runBinary(repo, "stats", "--format", "text")
	if err != nil {
		t.Fatalf("stats --format text should not error: %v", err)
	}
	if !containsLine(textOut, "pending_to_mine=null") {
		t.Errorf("text output should contain 'pending_to_mine=null', got:\n%s", textOut)
	}
}

func containsLine(text, substr string) bool {
	for line := range splitLines(text) {
		if line == substr {
			return true
		}
	}
	return false
}

func splitLines(s string) func(func(string) bool) {
	return func(yield func(string) bool) {
		start := 0
		for i := range len(s) {
			if s[i] == '\n' {
				if !yield(s[start:i]) {
					return
				}
				start = i + 1
			}
		}
		if start < len(s) {
			yield(s[start:])
		}
	}
}
