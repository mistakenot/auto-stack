package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EObservationAddList drives observation capture through the built binary:
// init --project -> observation add -> observation list, then verifies the
// on-disk observation event carries the required payload fields plus provenance
// (session id, git hash) from the envelope.
func TestE2EObservationAddList(t *testing.T) {
	repo := initE2ERepo(t)
	writeE2EFile(t, filepath.Join(repo, "README.md"), "seed\n")
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	t.Setenv("AUTO_SESSION_ID", "e2e-obs")

	if stdout, stderr, err := runBinary(repo, "init", "--project"); err != nil {
		t.Fatalf("init failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	// Add an observation.
	addStdout, addStderr, err := runBinary(repo, "observation", "add",
		"--kind", "incident",
		"--subject", "build broke on a stale worktree",
		"--evidence-session", "sess-123",
		"--evidence-quote", "merge conflict in main",
		"--evidence-message", "msg-9",
		"--context", "ran build after pulling",
		"--suggested-generalization", "fetch+pull before branching",
		"--domain", "git",
		"--severity", "high",
	)
	if err != nil {
		t.Fatalf("observation add failed: %v\nstdout:\n%s\nstderr:\n%s", err, addStdout, addStderr)
	}
	var added struct {
		Created     bool `json:"created"`
		Observation struct {
			ObservationID string `json:"observation_id"`
		} `json:"observation"`
	}
	if jerr := json.Unmarshal([]byte(addStdout), &added); jerr != nil {
		t.Fatalf("observation add stdout not JSON: %v\nraw:\n%s", jerr, addStdout)
	}
	if !added.Created || !strings.HasPrefix(added.Observation.ObservationID, "ob-") {
		t.Fatalf("unexpected add response: %#v", added)
	}

	// List returns the observation we just added.
	listStdout, listStderr, err := runBinary(repo, "observation", "list")
	if err != nil {
		t.Fatalf("observation list failed: %v\nstderr:\n%s", err, listStderr)
	}
	var listed struct {
		Scope        string           `json:"scope"`
		Observations []map[string]any `json:"observations"`
	}
	if jerr := json.Unmarshal([]byte(listStdout), &listed); jerr != nil {
		t.Fatalf("observation list stdout not JSON: %v\nraw:\n%s", jerr, listStdout)
	}
	if len(listed.Observations) != 1 {
		t.Fatalf("expected 1 listed observation, got %d\nraw:\n%s", len(listed.Observations), listStdout)
	}
	requireFields(t, listed.Observations[0], "id", "ts", "observation_id", "kind", "subject", "severity")
	if listed.Observations[0]["observation_id"] != added.Observation.ObservationID {
		t.Fatalf("listed observation id mismatch: %#v", listed.Observations[0])
	}

	// The on-disk event carries the required payload fields + provenance.
	eventsDir := filepath.Join(repo, ".auto", "reflect", "events")
	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		t.Fatalf("read events dir %s: %v", eventsDir, err)
	}
	var shardFiles []string
	for _, e := range entries {
		if !e.IsDir() && shardNameShape.MatchString(e.Name()) {
			shardFiles = append(shardFiles, e.Name())
		}
	}
	if len(shardFiles) == 0 {
		t.Fatalf("expected an event shard under %s, found %v", eventsDir, namesOf(entries))
	}

	body := readShards(t, eventsDir, shardFiles)
	if !strings.Contains(body, `"observation"`) {
		t.Fatalf("event log missing observation event; body:\n%s", body)
	}

	// Decode the observation event and assert envelope provenance + payload fields.
	var found bool
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		if line == "" {
			continue
		}
		var env struct {
			Type      string `json:"type"`
			SessionID string `json:"session_id"`
			Git       struct {
				Hash string `json:"hash"`
			} `json:"git"`
			Payload struct {
				ObservationID string `json:"observation_id"`
				Kind          string `json:"kind"`
				Subject       string `json:"subject"`
				Severity      string `json:"severity"`
				Evidence      []struct {
					SessionID string `json:"session_id"`
				} `json:"evidence"`
			} `json:"payload"`
		}
		if jerr := json.Unmarshal([]byte(line), &env); jerr != nil {
			t.Fatalf("decode event line: %v\nline:\n%s", jerr, line)
		}
		if env.Type != "observation" {
			continue
		}
		found = true
		if env.SessionID != "e2e-obs" {
			t.Errorf("expected session provenance e2e-obs, got %q", env.SessionID)
		}
		if env.Git.Hash == "" {
			t.Errorf("expected non-empty git hash provenance on a committed repo")
		}
		if env.Payload.ObservationID != added.Observation.ObservationID {
			t.Errorf("payload observation_id mismatch: %q", env.Payload.ObservationID)
		}
		if env.Payload.Kind != "incident" || env.Payload.Severity != "high" {
			t.Errorf("unexpected payload kind/severity: %#v", env.Payload)
		}
		if len(env.Payload.Evidence) != 1 || env.Payload.Evidence[0].SessionID != "sess-123" {
			t.Errorf("unexpected payload evidence: %#v", env.Payload.Evidence)
		}
	}
	if !found {
		t.Fatalf("no observation event found in shard body:\n%s", body)
	}
}

// TestE2EObservationAuditTrail drives observation capture with the full audit
// trail: an originating task id plus per-evidence source provenance
// (file/line-range/commit), then verifies the stored event carries them.
func TestE2EObservationAuditTrail(t *testing.T) {
	repo := initE2ERepo(t)
	writeE2EFile(t, filepath.Join(repo, "README.md"), "seed\n")
	runCmd(t, repo, "git", "add", ".")
	runCmd(t, repo, "git", "commit", "-m", "seed")

	t.Setenv("AUTO_SESSION_ID", "e2e-obs-audit")

	if stdout, stderr, err := runBinary(repo, "init", "--project"); err != nil {
		t.Fatalf("init failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}

	addStdout, addStderr, err := runBinary(repo, "observation", "add",
		"--kind", "incident",
		"--subject", "build broke on a stale worktree",
		"--task-id", "049-reflect-audit-lineage-lint",
		"--evidence-session", "sess-123",
		"--evidence-file", "internal/app/build.go",
		"--evidence-line-range", "42-58",
		"--evidence-commit", "deadbeef",
		"--severity", "high",
	)
	if err != nil {
		t.Fatalf("observation add failed: %v\nstdout:\n%s\nstderr:\n%s", err, addStdout, addStderr)
	}
	var added struct {
		Created     bool `json:"created"`
		Observation struct {
			ObservationID string `json:"observation_id"`
		} `json:"observation"`
	}
	if jerr := json.Unmarshal([]byte(addStdout), &added); jerr != nil {
		t.Fatalf("observation add stdout not JSON: %v\nraw:\n%s", jerr, addStdout)
	}
	if !added.Created || !strings.HasPrefix(added.Observation.ObservationID, "ob-") {
		t.Fatalf("unexpected add response: %#v", added)
	}

	// Decode the stored event and assert the audit-trail fields.
	eventsDir := filepath.Join(repo, ".auto", "reflect", "events")
	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		t.Fatalf("read events dir %s: %v", eventsDir, err)
	}
	var shardFiles []string
	for _, e := range entries {
		if !e.IsDir() && shardNameShape.MatchString(e.Name()) {
			shardFiles = append(shardFiles, e.Name())
		}
	}
	if len(shardFiles) == 0 {
		t.Fatalf("expected an event shard under %s, found %v", eventsDir, namesOf(entries))
	}

	body := readShards(t, eventsDir, shardFiles)
	var found bool
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		if line == "" {
			continue
		}
		var env struct {
			Type    string `json:"type"`
			Payload struct {
				ObservationID string `json:"observation_id"`
				TaskID        string `json:"task_id"`
				Evidence      []struct {
					SessionID string `json:"session_id"`
					File      string `json:"file"`
					LineRange string `json:"line_range"`
					Commit    string `json:"commit"`
				} `json:"evidence"`
			} `json:"payload"`
		}
		if jerr := json.Unmarshal([]byte(line), &env); jerr != nil {
			t.Fatalf("decode event line: %v\nline:\n%s", jerr, line)
		}
		if env.Type != "observation" {
			continue
		}
		found = true
		if env.Payload.TaskID != "049-reflect-audit-lineage-lint" {
			t.Errorf("expected task_id provenance, got %q", env.Payload.TaskID)
		}
		if len(env.Payload.Evidence) != 1 {
			t.Fatalf("expected 1 evidence item, got %d", len(env.Payload.Evidence))
		}
		ev := env.Payload.Evidence[0]
		if ev.SessionID != "sess-123" || ev.File != "internal/app/build.go" || ev.LineRange != "42-58" || ev.Commit != "deadbeef" {
			t.Errorf("unexpected evidence provenance: %#v", ev)
		}
	}
	if !found {
		t.Fatalf("no observation event found in shard body:\n%s", body)
	}
}
