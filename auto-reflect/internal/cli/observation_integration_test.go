package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// observationAddResp mirrors the `observation add` JSON envelope.
type observationAddResp struct {
	Created     bool `json:"created"`
	Observation struct {
		ID            string   `json:"id"`
		TS            string   `json:"ts"`
		SessionID     string   `json:"session_id"`
		ObservationID string   `json:"observation_id"`
		Kind          string   `json:"kind"`
		Subject       string   `json:"subject"`
		Severity      string   `json:"severity"`
		Domain        []string `json:"domain"`
		Evidence      []struct {
			SessionID string `json:"session_id"`
			MessageID string `json:"message_id"`
			Quote     string `json:"quote"`
		} `json:"evidence"`
	} `json:"observation"`
}

type observationListResp struct {
	Scope        string `json:"scope"`
	Observations []struct {
		ID            string   `json:"id"`
		ObservationID string   `json:"observation_id"`
		Kind          string   `json:"kind"`
		Subject       string   `json:"subject"`
		Domain        []string `json:"domain"`
		TS            string   `json:"ts"`
	} `json:"observations"`
}

func addObservation(t *testing.T, repo string, args ...string) observationAddResp {
	t.Helper()
	full := append([]string{"observation", "add"}, args...)
	stdout, stderr, code := runCLIAt(t, repo, full...)
	if code != 0 {
		t.Fatalf("observation add failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var resp observationAddResp
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("decode observation add: %v\nraw:\n%s", err, stdout)
	}
	return resp
}

func TestObservationAddWritesEventOnDisk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AUTO_SESSION_ID", "obs-add")
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	resp := addObservation(t, repo,
		"--kind", "gap",
		"--subject", "docs are surfaced but rarely read",
		"--evidence-session", "s1",
		"--evidence-quote", "I never opened the doc",
		"--domain", "docs",
		"--severity", "high",
	)

	if !resp.Created {
		t.Fatalf("expected created=true, got %#v", resp)
	}
	if !strings.HasPrefix(resp.Observation.ObservationID, "ob-") {
		t.Fatalf("expected ob- id, got %q", resp.Observation.ObservationID)
	}
	if resp.Observation.Kind != "gap" || resp.Observation.Severity != "high" {
		t.Fatalf("unexpected kind/severity: %#v", resp.Observation)
	}
	if resp.Observation.SessionID != "obs-add" {
		t.Fatalf("expected session id from env, got %q", resp.Observation.SessionID)
	}
	if len(resp.Observation.Evidence) != 1 || resp.Observation.Evidence[0].SessionID != "s1" {
		t.Fatalf("unexpected evidence: %#v", resp.Observation.Evidence)
	}
	if resp.Observation.Evidence[0].Quote != "I never opened the doc" {
		t.Fatalf("quote not stored: %#v", resp.Observation.Evidence[0])
	}

	// Assert exactly one observation event landed on disk.
	got := readObservationEvents(t, repo)
	if len(got) != 1 {
		t.Fatalf("expected 1 observation event on disk, got %d", len(got))
	}
	if got[0]["observation_id"] != resp.Observation.ObservationID {
		t.Fatalf("on-disk observation_id mismatch: %#v", got[0])
	}
}

func TestObservationAddValidationFailures(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AUTO_SESSION_ID", "obs-fail")
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	cases := []struct {
		name     string
		args     []string
		wantErr  string
		exitZero bool
	}{
		{
			name:    "missing kind",
			args:    []string{"observation", "add", "--subject", "x", "--evidence-session", "s1"},
			wantErr: "kind",
		},
		{
			name:    "missing evidence session",
			args:    []string{"observation", "add", "--kind", "gap", "--subject", "x"},
			wantErr: "evidence session",
		},
		{
			name:    "bad severity",
			args:    []string{"observation", "add", "--kind", "gap", "--subject", "x", "--evidence-session", "s1", "--severity", "critical"},
			wantErr: "severity",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runCLIAt(t, repo, tc.args...)
			if code == 0 {
				t.Fatalf("expected non-zero exit\nstdout:\n%s", stdout)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("expected empty stdout on validation error, got:\n%s", stdout)
			}
			if !strings.Contains(stderr, tc.wantErr) {
				t.Fatalf("expected stderr to mention %q, got:\n%s", tc.wantErr, stderr)
			}
		})
	}

	// No events should have been written by any of the failing adds.
	if got := readObservationEvents(t, repo); len(got) != 0 {
		t.Fatalf("expected no observation events after failures, got %d", len(got))
	}
}

func TestObservationListFiltersAndOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AUTO_SESSION_ID", "obs-list")
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	first := addObservation(t, repo, "--kind", "gap", "--subject", "first gap", "--evidence-session", "s1", "--domain", "docs")
	second := addObservation(t, repo, "--kind", "correction", "--subject", "second correction", "--evidence-session", "s2", "--domain", "search")
	third := addObservation(t, repo, "--kind", "gap", "--subject", "third gap", "--evidence-session", "s3", "--domain", "docs")

	// Plain list: all three, newest-first (ts desc, id desc on ties).
	all := listObservations(t, repo)
	if len(all.Observations) != 3 {
		t.Fatalf("expected 3 observations, got %d", len(all.Observations))
	}
	ids := map[string]bool{
		first.Observation.ObservationID:  true,
		second.Observation.ObservationID: true,
		third.Observation.ObservationID:  true,
	}
	for _, o := range all.Observations {
		if !ids[o.ObservationID] {
			t.Fatalf("unexpected observation id %q", o.ObservationID)
		}
	}
	// Newest-first invariant: timestamps must be non-increasing.
	for i := 1; i < len(all.Observations); i++ {
		if all.Observations[i-1].TS < all.Observations[i].TS {
			t.Fatalf("list not newest-first: %s before %s", all.Observations[i-1].TS, all.Observations[i].TS)
		}
	}

	// --kind filter.
	gaps := listObservations(t, repo, "--kind", "gap")
	if len(gaps.Observations) != 2 {
		t.Fatalf("expected 2 gap observations, got %d", len(gaps.Observations))
	}
	for _, o := range gaps.Observations {
		if o.Kind != "gap" {
			t.Fatalf("kind filter leaked %q", o.Kind)
		}
	}

	// --domain filter (ANY-of).
	docs := listObservations(t, repo, "--domain", "docs")
	if len(docs.Observations) != 2 {
		t.Fatalf("expected 2 docs observations, got %d", len(docs.Observations))
	}

	// --limit caps the result.
	limited := listObservations(t, repo, "--limit", "1")
	if len(limited.Observations) != 1 {
		t.Fatalf("expected 1 observation with --limit 1, got %d", len(limited.Observations))
	}

	// --since 1h includes everything just written.
	recent := listObservations(t, repo, "--since", "1h")
	if len(recent.Observations) != 3 {
		t.Fatalf("expected 3 recent observations, got %d", len(recent.Observations))
	}

	// --unconsolidated returns all (no consolidation event type yet).
	unconsolidated := listObservations(t, repo, "--unconsolidated")
	if len(unconsolidated.Observations) != 3 {
		t.Fatalf("expected 3 unconsolidated observations, got %d", len(unconsolidated.Observations))
	}
}

func listObservations(t *testing.T, repo string, args ...string) observationListResp {
	t.Helper()
	full := append([]string{"observation", "list"}, args...)
	stdout, stderr, code := runCLIAt(t, repo, full...)
	if code != 0 {
		t.Fatalf("observation list failed: code=%d\nstderr:\n%s", code, stderr)
	}
	var resp observationListResp
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("decode observation list: %v\nraw:\n%s", err, stdout)
	}
	return resp
}

// readObservationEvents reads every event shard and returns the decoded payloads
// of all observation-typed events.
func readObservationEvents(t *testing.T, repo string) []map[string]any {
	t.Helper()
	eventsDir := filepath.Join(repo, ".auto", "reflect", "events")
	entries, err := os.ReadDir(eventsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read events dir: %v", err)
	}
	var out []map[string]any
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(eventsDir, e.Name()))
		if err != nil {
			t.Fatalf("read shard: %v", err)
		}
		for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
			if line == "" {
				continue
			}
			var env struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(line), &env); err != nil {
				t.Fatalf("decode event: %v\nline:\n%s", err, line)
			}
			if env.Type != "observation" {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal(env.Payload, &payload); err != nil {
				t.Fatalf("decode observation payload: %v", err)
			}
			out = append(out, payload)
		}
	}
	return out
}

// TestObservationDoesNotDirtyRuleProjection asserts that adding an observation
// leaves the rule projection (playbook) untouched: folded_through is unchanged
// and the rule list still has exactly the one rule we created.
func TestObservationDoesNotDirtyRuleProjection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AUTO_SESSION_ID", "obs-isolation")
	repo := initGitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "hello\n")
	gitAddCommit(t, repo, "seed")

	createTestRuleWith(t, repo,
		"--use-when", "writing go", "--content", "use gofmt",
		"--causal-note", "unformatted code", "--domain", "go", "--type", "soft")

	playbookPath := filepath.Join(repo, ".auto", "reflect", "playbook.json")
	before, err := os.ReadFile(playbookPath)
	if err != nil {
		t.Fatalf("read playbook before: %v", err)
	}

	addObservation(t, repo, "--kind", "gap", "--subject", "isolation check", "--evidence-session", "s1")

	after, err := os.ReadFile(playbookPath)
	if err != nil {
		t.Fatalf("read playbook after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("observation dirtied the rule projection\nbefore:\n%s\nafter:\n%s", before, after)
	}

	// rule list still reports exactly one rule.
	stdout, _, code := runCLIAt(t, repo, "rule", "list")
	if code != 0 {
		t.Fatalf("rule list failed")
	}
	var listed struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("decode rule list: %v\nraw:\n%s", err, stdout)
	}
	if len(listed.Rules) != 1 {
		t.Fatalf("expected 1 rule after adding observation, got %d", len(listed.Rules))
	}
}
