package hooks_test

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/hooks"
)

func TestLogPath(t *testing.T) {
	ts := time.Date(2026, 6, 15, 23, 59, 0, 0, time.UTC)
	p, err := hooks.LogPath(ts)
	if err != nil {
		t.Fatalf("LogPath: %v", err)
	}
	const wantSuffix = "hooks/raw/events-2026-06-15.jsonl"
	if len(p) < len(wantSuffix) || p[len(p)-len(wantSuffix):] != wantSuffix {
		t.Errorf("LogPath = %q, want suffix %q", p, wantSuffix)
	}
}

func TestAppendRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	env1 := hooks.Envelope{
		Agent:      "claude",
		CapturedAt: "2026-06-15T10:00:00Z",
		HostID:     "host-1",
		Cwd:        "/workspace",
		Project:    "myproject",
		Payload:    json.RawMessage(`{"hook_event_name":"PostToolUse","tool_name":"Read"}`),
	}
	env2 := hooks.Envelope{
		Agent:      "codex",
		CapturedAt: "2026-06-15T10:01:00Z",
		HostID:     "host-2",
		Payload:    json.RawMessage(`{"hook_event_name":"Stop","reason":"done"}`),
	}

	if err := hooks.Append(env1); err != nil {
		t.Fatalf("Append env1: %v", err)
	}
	if err := hooks.Append(env2); err != nil {
		t.Fatalf("Append env2: %v", err)
	}

	// Find the file that was written.
	logPath, err := hooks.LogPath(time.Now().UTC())
	if err != nil {
		t.Fatalf("LogPath: %v", err)
	}

	f, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}

	var got1, got2 hooks.Envelope
	if err := json.Unmarshal([]byte(lines[0]), &got1); err != nil {
		t.Fatalf("unmarshal line 1: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &got2); err != nil {
		t.Fatalf("unmarshal line 2: %v", err)
	}

	if string(got1.Payload) != string(env1.Payload) {
		t.Errorf("line 1 payload = %s, want %s", got1.Payload, env1.Payload)
	}
	if string(got2.Payload) != string(env2.Payload) {
		t.Errorf("line 2 payload = %s, want %s", got2.Payload, env2.Payload)
	}
	if got1.Agent != env1.Agent {
		t.Errorf("line 1 agent = %q, want %q", got1.Agent, env1.Agent)
	}
	if got2.Agent != env2.Agent {
		t.Errorf("line 2 agent = %q, want %q", got2.Agent, env2.Agent)
	}
}

func TestExtractEventName(t *testing.T) {
	got := hooks.ExtractEventName(map[string]any{"hook_event_name": "PostToolUse"})
	if got != "PostToolUse" {
		t.Errorf("ExtractEventName = %q, want %q", got, "PostToolUse")
	}
	if got := hooks.ExtractEventName(nil); got != "" {
		t.Errorf("ExtractEventName(nil) = %q, want empty", got)
	}
}

func TestExtractSessionID(t *testing.T) {
	got := hooks.ExtractSessionID(map[string]any{"session_id": "abc-123"})
	if got != "abc-123" {
		t.Errorf("ExtractSessionID = %q, want %q", got, "abc-123")
	}
	if got := hooks.ExtractSessionID(nil); got != "" {
		t.Errorf("ExtractSessionID(nil) = %q, want empty", got)
	}
}

func TestExtractTool(t *testing.T) {
	got := hooks.ExtractTool(map[string]any{"tool_name": "Read"})
	if got != "Read" {
		t.Errorf("ExtractTool = %q, want %q", got, "Read")
	}
	if got := hooks.ExtractTool(nil); got != "" {
		t.Errorf("ExtractTool(nil) = %q, want empty", got)
	}
}

func TestExtractPaths(t *testing.T) {
	payload := map[string]any{
		"tool_input": map[string]any{
			"file_path":     "/b/plan.md",
			"notebook_path": "/a/nb.ipynb",
		},
	}
	got := hooks.ExtractPaths(payload)
	want := []string{"/a/nb.ipynb", "/b/plan.md"}
	if len(got) != len(want) {
		t.Fatalf("ExtractPaths len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ExtractPaths[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// nil payload → nil
	if got := hooks.ExtractPaths(nil); got != nil {
		t.Errorf("ExtractPaths(nil) = %v, want nil", got)
	}

	// no tool_input → nil
	if got := hooks.ExtractPaths(map[string]any{"other": "value"}); got != nil {
		t.Errorf("ExtractPaths(no tool_input) = %v, want nil", got)
	}
}

func TestExtractPathsDedup(t *testing.T) {
	payload := map[string]any{
		"tool_input": map[string]any{
			"file_path": "/same/path.go",
			"path":      "/same/path.go",
		},
	}
	got := hooks.ExtractPaths(payload)
	if len(got) != 1 {
		t.Fatalf("ExtractPaths dedup len = %d, want 1; got %v", len(got), got)
	}
	if got[0] != "/same/path.go" {
		t.Errorf("ExtractPaths dedup[0] = %q, want %q", got[0], "/same/path.go")
	}
}
