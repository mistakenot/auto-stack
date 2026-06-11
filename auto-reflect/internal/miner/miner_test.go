package miner

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	sharedmodel "github.com/mistakenot/auto-shared/model"
	"github.com/parquet-go/parquet-go"

	"github.com/mistakenot/auto-reflect/internal/events"
)

// --- fixture helpers ---

func writeParquetFile[T any](t *testing.T, path string, rows []T) {
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

func writeEvent(t *testing.T, repoRoot string, ev *events.Event) {
	t.Helper()
	dir := filepath.Join(repoRoot, ".auto", "reflect", "events")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir events: %v", err)
	}
	path := filepath.Join(dir, "test.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open events: %v", err)
	}
	defer func() { _ = f.Close() }()
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		t.Fatalf("write event: %v", err)
	}
}

func makeSessionMinedEvent(t *testing.T, sessionID string, version int, status events.AckStatus, observations int, seq int, ts string) events.Event {
	t.Helper()
	payload := events.SessionMinedPayload{
		SessionID:    sessionID,
		MinerVersion: version,
		Status:       status,
		Observations: observations,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return events.Event{
		ID:            fmt.Sprintf("ev-%08x", seq),
		Type:          events.TypeSessionMined,
		SchemaVersion: 1,
		Seq:           seq,
		TS:            ts,
		Host:          "test-host",
		Payload:       raw,
	}
}

// --- FoldCoverage tests ---

func TestFoldCoverage_FailedStaysPending(t *testing.T) {
	evs := []events.Event{
		makeSessionMinedEvent(t, "s1", 1, events.AckFailed, 0, 1, "2026-01-01T00:00:00Z"),
	}
	cov := FoldCoverage(evs)
	state := cov["s1"]
	if state.MaxTerminalVersion != 0 {
		t.Errorf("MaxTerminalVersion = %d, want 0 (failed never makes terminal)", state.MaxTerminalVersion)
	}
	if state.AckCount != 1 {
		t.Errorf("AckCount = %d, want 1", state.AckCount)
	}
}

func TestFoldCoverage_MinedIsTerminal(t *testing.T) {
	evs := []events.Event{
		makeSessionMinedEvent(t, "s1", 1, events.AckMined, 3, 1, "2026-01-01T00:00:00Z"),
	}
	cov := FoldCoverage(evs)
	state := cov["s1"]
	if state.MaxTerminalVersion != 1 {
		t.Errorf("MaxTerminalVersion = %d, want 1", state.MaxTerminalVersion)
	}
	if state.LastStatus != events.AckMined {
		t.Errorf("LastStatus = %q, want %q", state.LastStatus, events.AckMined)
	}
	if state.LastObservations != 3 {
		t.Errorf("LastObservations = %d, want 3", state.LastObservations)
	}
}

func TestFoldCoverage_EmptyAndSkippedAreTerminal(t *testing.T) {
	evs := []events.Event{
		makeSessionMinedEvent(t, "s-empty", 1, events.AckEmpty, 0, 1, "2026-01-01T00:00:00Z"),
		makeSessionMinedEvent(t, "s-skip", 1, events.AckSkipped, 0, 2, "2026-01-01T00:00:01Z"),
	}
	cov := FoldCoverage(evs)
	if cov["s-empty"].MaxTerminalVersion != 1 {
		t.Errorf("empty: MaxTerminalVersion = %d, want 1", cov["s-empty"].MaxTerminalVersion)
	}
	if cov["s-skip"].MaxTerminalVersion != 1 {
		t.Errorf("skipped: MaxTerminalVersion = %d, want 1", cov["s-skip"].MaxTerminalVersion)
	}
}

func TestFoldCoverage_VersionBump(t *testing.T) {
	// Mined at v1, then should appear in queue if Version were bumped to 2
	evs := []events.Event{
		makeSessionMinedEvent(t, "s1", 1, events.AckMined, 2, 1, "2026-01-01T00:00:00Z"),
	}
	cov := FoldCoverage(evs)
	state := cov["s1"]
	// Terminal at v1
	if state.MaxTerminalVersion != 1 {
		t.Errorf("MaxTerminalVersion = %d, want 1", state.MaxTerminalVersion)
	}
	// If we compared against Version=2, this session would be pending
	if state.MaxTerminalVersion >= 2 {
		t.Error("session should not be terminal at version 2")
	}
}

func TestFoldCoverage_IgnoresNonMined(t *testing.T) {
	payload, err := json.Marshal(events.RuleCreatedPayload{RuleID: "r1"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	evs := []events.Event{
		{
			ID:            "ev-00000001",
			Type:          events.TypeRuleCreated,
			SchemaVersion: 1,
			Seq:           1,
			TS:            "2026-01-01T00:00:00Z",
			Host:          "test-host",
			Payload:       payload,
		},
	}
	cov := FoldCoverage(evs)
	if len(cov) != 0 {
		t.Errorf("expected empty coverage, got %d entries", len(cov))
	}
}

// --- Next tests ---

// setupFixture creates a temp etl root and repo root with sessions, messages, and optional events.
type fixture struct {
	RepoRoot string
	ETLRoot  string
}

func setupFixture(t *testing.T) fixture {
	t.Helper()
	repoRoot := t.TempDir()
	etlRoot := t.TempDir()

	// Initialize a git repo so DetectRepoLenient works
	initGitRepo(t, repoRoot, "https://github.com/example/repo.git")

	sessions := []sharedmodel.AgentSession{
		{
			ID:            "top-1",
			Workspace:     repoRoot,
			GitRemote:     "https://github.com/example/repo.git",
			IsSubagent:    false,
			LastMessageAt: 1000,
		},
		{
			ID:            "top-2",
			Workspace:     repoRoot,
			GitRemote:     "https://github.com/example/repo.git",
			IsSubagent:    false,
			LastMessageAt: 2000,
		},
		{
			ID:              "sub-1",
			Workspace:       repoRoot,
			GitRemote:       "https://github.com/example/repo.git",
			IsSubagent:      true,
			ParentSessionID: "top-1",
			LastMessageAt:   1500,
		},
		{
			ID:            "other-repo",
			Workspace:     "/tmp/other",
			GitRemote:     "https://github.com/example/other.git",
			IsSubagent:    false,
			LastMessageAt: 3000,
		},
	}
	writeParquetFile(t, filepath.Join(etlRoot, "sessions", "data.parquet"), sessions)

	messages := []sharedmodel.AgentMessage{
		// top-1: 2 user msgs, 1 with correction
		{ID: "m1", SessionID: "top-1", Role: "user", ContentTruncated: "hello world"},
		{ID: "m2", SessionID: "top-1", Role: "assistant", ContentTruncated: "hi"},
		{ID: "m3", SessionID: "top-1", Role: "user", ContentTruncated: "no, not that"},
		// top-2: 1 user msg, tool error
		{ID: "m4", SessionID: "top-2", Role: "user", ContentTruncated: "build it"},
		{ID: "m5", SessionID: "top-2", Role: "tool", ToolName: "Bash", IsError: true, ContentTruncated: "error: build failed"},
		// sub-1
		{ID: "m6", SessionID: "sub-1", Role: "user", ContentTruncated: "do the thing"},
		// other-repo
		{ID: "m7", SessionID: "other-repo", Role: "user", ContentTruncated: "hello"},
	}
	writeParquetFile(t, filepath.Join(etlRoot, "messages", "data.parquet"), messages)

	return fixture{RepoRoot: repoRoot, ETLRoot: etlRoot}
}

func initGitRepo(t *testing.T, dir, remoteURL string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "remote", "add", "origin", remoteURL},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git setup %v: %s: %v", args, out, err)
		}
	}
}

func TestNext_ExcludesSubagents(t *testing.T) {
	f := setupFixture(t)
	items, err := Next(f.RepoRoot, f.ETLRoot, NextOpts{All: true})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	for _, item := range items {
		if item.SessionID == "sub-1" {
			t.Error("subagent sub-1 should not appear in Next results")
		}
	}
}

func TestNext_ScopesByRemote(t *testing.T) {
	f := setupFixture(t)
	// Default (non-All) should scope to current repo
	items, err := Next(f.RepoRoot, f.ETLRoot, NextOpts{})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	for _, item := range items {
		if item.SessionID == "other-repo" {
			t.Error("other-repo should be excluded by remote scope")
		}
	}
	// Should include top-1 and top-2
	ids := make(map[string]bool)
	for _, item := range items {
		ids[item.SessionID] = true
	}
	if !ids["top-1"] {
		t.Error("top-1 should be included in scoped results")
	}
	if !ids["top-2"] {
		t.Error("top-2 should be included in scoped results")
	}
}

func TestNext_AllWidensScope(t *testing.T) {
	f := setupFixture(t)
	items, err := Next(f.RepoRoot, f.ETLRoot, NextOpts{All: true})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	ids := make(map[string]bool)
	for _, item := range items {
		ids[item.SessionID] = true
	}
	if !ids["other-repo"] {
		t.Error("other-repo should be included when All=true")
	}
}

func TestNext_NoDuplicates(t *testing.T) {
	f := setupFixture(t)
	items, err := Next(f.RepoRoot, f.ETLRoot, NextOpts{All: true})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	seen := make(map[string]int)
	for _, item := range items {
		seen[item.SessionID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("duplicate session ID %q appeared %d times", id, count)
		}
	}
}

func TestNext_FetchCmd(t *testing.T) {
	f := setupFixture(t)
	items, err := Next(f.RepoRoot, f.ETLRoot, NextOpts{All: true})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	for _, item := range items {
		want := "auto search session get " + item.SessionID
		if item.FetchCmd != want {
			t.Errorf("FetchCmd = %q, want %q", item.FetchCmd, want)
		}
	}
}

func TestNext_ExcludesTerminal(t *testing.T) {
	f := setupFixture(t)
	// Mark top-1 as mined at current Version
	ev := makeSessionMinedEvent(t, "top-1", Version, events.AckMined, 2, 1, "2026-01-01T00:00:00Z")
	writeEvent(t, f.RepoRoot, &ev)

	items, err := Next(f.RepoRoot, f.ETLRoot, NextOpts{All: true})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	for _, item := range items {
		if item.SessionID == "top-1" {
			t.Error("top-1 should be excluded — terminal at current version")
		}
	}
}

func TestNext_FailedStaysInQueue(t *testing.T) {
	f := setupFixture(t)
	// Mark top-1 as failed — should still appear
	ev := makeSessionMinedEvent(t, "top-1", Version, events.AckFailed, 0, 1, "2026-01-01T00:00:00Z")
	writeEvent(t, f.RepoRoot, &ev)

	items, err := Next(f.RepoRoot, f.ETLRoot, NextOpts{All: true})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	found := false
	for _, item := range items {
		if item.SessionID == "top-1" {
			found = true
			if !item.Remined {
				t.Error("top-1 should have Remined=true")
			}
			if item.PriorAck == nil {
				t.Error("top-1 should have non-nil PriorAck")
			} else {
				if item.PriorAck.Status != events.AckFailed {
					t.Errorf("PriorAck.Status = %q, want %q", item.PriorAck.Status, events.AckFailed)
				}
			}
		}
	}
	if !found {
		t.Error("top-1 should still be in queue after failed ack")
	}
}

func TestNext_Limit(t *testing.T) {
	f := setupFixture(t)
	items, err := Next(f.RepoRoot, f.ETLRoot, NextOpts{All: true, Limit: 1})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item with Limit=1, got %d", len(items))
	}
}

func TestNext_IncludeSubagents(t *testing.T) {
	f := setupFixture(t)
	items, err := Next(f.RepoRoot, f.ETLRoot, NextOpts{All: true, IncludeSubagents: true})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	found := false
	for _, item := range items {
		if item.SessionID == "top-1" && len(item.Subagents) > 0 {
			found = true
			if item.Subagents[0] != "sub-1" {
				t.Errorf("expected subagent sub-1, got %q", item.Subagents[0])
			}
		}
	}
	if !found {
		t.Error("top-1 should have subagent sub-1 when IncludeSubagents=true")
	}
}

func TestNext_SortedByScore(t *testing.T) {
	f := setupFixture(t)
	items, err := Next(f.RepoRoot, f.ETLRoot, NextOpts{All: true})
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	for i := 1; i < len(items); i++ {
		if items[i].PriorityScore > items[i-1].PriorityScore {
			t.Errorf("items not sorted: [%d].Score=%f > [%d].Score=%f",
				i, items[i].PriorityScore, i-1, items[i-1].PriorityScore)
		}
	}
}

// --- Describe / SignalsFor tests ---

func TestDescribe_ReturnsSignals(t *testing.T) {
	f := setupFixture(t)
	row, err := Describe(f.RepoRoot, f.ETLRoot, "top-1")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if row.SessionID != "top-1" {
		t.Errorf("SessionID = %q, want %q", row.SessionID, "top-1")
	}
	if row.Signals.MessageCount != 3 {
		t.Errorf("MessageCount = %d, want 3", row.Signals.MessageCount)
	}
}

func TestDescribe_SubagentSession(t *testing.T) {
	f := setupFixture(t)
	// Describe should work for subagent sessions too (not filtered by Next)
	row, err := Describe(f.RepoRoot, f.ETLRoot, "sub-1")
	if err != nil {
		t.Fatalf("Describe subagent: %v", err)
	}
	if row.SessionID != "sub-1" {
		t.Errorf("SessionID = %q, want %q", row.SessionID, "sub-1")
	}
}

func TestDescribe_WithAckHistory(t *testing.T) {
	f := setupFixture(t)
	ts := time.Now().UTC().Format(time.RFC3339)
	ev := makeSessionMinedEvent(t, "top-1", 1, events.AckMined, 2, 1, ts)
	writeEvent(t, f.RepoRoot, &ev)

	row, err := Describe(f.RepoRoot, f.ETLRoot, "top-1")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(row.AckHistory) != 1 {
		t.Fatalf("AckHistory length = %d, want 1", len(row.AckHistory))
	}
	if row.AckHistory[0].Status != events.AckMined {
		t.Errorf("AckHistory[0].Status = %q, want %q", row.AckHistory[0].Status, events.AckMined)
	}
	if row.AckHistory[0].Observations != 2 {
		t.Errorf("AckHistory[0].Observations = %d, want 2", row.AckHistory[0].Observations)
	}
}

func TestDescribe_NotFound(t *testing.T) {
	f := setupFixture(t)
	_, err := Describe(f.RepoRoot, f.ETLRoot, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestSignalsFor_MultipleSessions(t *testing.T) {
	f := setupFixture(t)
	rows, err := SignalsFor(f.RepoRoot, f.ETLRoot, []string{"top-1", "top-2", "sub-1"})
	if err != nil {
		t.Fatalf("SignalsFor: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	// Verify order matches input
	if rows[0].SessionID != "top-1" {
		t.Errorf("rows[0].SessionID = %q, want %q", rows[0].SessionID, "top-1")
	}
	if rows[1].SessionID != "top-2" {
		t.Errorf("rows[1].SessionID = %q, want %q", rows[1].SessionID, "top-2")
	}
	if rows[2].SessionID != "sub-1" {
		t.Errorf("rows[2].SessionID = %q, want %q", rows[2].SessionID, "sub-1")
	}
}

func TestSignalsFor_Dedupe(t *testing.T) {
	f := setupFixture(t)
	rows, err := SignalsFor(f.RepoRoot, f.ETLRoot, []string{"top-1", "top-1"})
	if err != nil {
		t.Fatalf("SignalsFor: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 row (deduped), got %d", len(rows))
	}
}
