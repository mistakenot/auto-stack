package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mistakenot/auto-shared/bus"
	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-shared/hooks"
)

const claudePostToolUse = `{
  "hook_event_name": "PostToolUse",
  "session_id": "sess-123",
  "cwd": "/repos/widgets/docs",
  "tool_name": "Edit",
  "tool_input": {"file_path": "/repos/widgets/docs/plan.md", "old_string": "a", "new_string": "b"}
}`

func TestBuildBusEventNormalizesAndMapsProject(t *testing.T) {
	registry := sharedconfig.ProjectsConfig{Projects: []sharedconfig.ProjectRef{
		{ID: "widgets", Path: "/repos/widgets"},
	}}
	ev := buildBusEvent("claude", []byte(claudePostToolUse), registry)

	if ev.Source != "auto/hooks/claude" {
		t.Errorf("source = %q, want auto/hooks/claude", ev.Source)
	}
	if ev.Type != "agent.tool.post" {
		t.Errorf("type = %q, want agent.tool.post", ev.Type)
	}
	if ev.Session != "sess-123" {
		t.Errorf("session = %q, want sess-123", ev.Session)
	}
	if ev.Time == "" {
		t.Error("expected time set")
	}
	if ev.SpecVersion != bus.SpecVersion {
		t.Errorf("specversion = %q, want %q", ev.SpecVersion, bus.SpecVersion)
	}

	// Validate the envelope is structurally valid.
	if errs := ev.Validate(); len(errs) > 0 {
		t.Fatalf("envelope validation failed: %v", errs)
	}

	// Decode the ToolPost data payload.
	tp, err := bus.DecodeData[bus.ToolPost](ev)
	if err != nil {
		t.Fatalf("decode ToolPost: %v", err)
	}
	if tp.Tool != "Edit" {
		t.Errorf("tool = %q, want Edit", tp.Tool)
	}
	if tp.Event != "PostToolUse" {
		t.Errorf("event = %q, want PostToolUse", tp.Event)
	}
	if tp.Raw == nil {
		t.Error("expected raw tool_input preserved")
	}
}

func TestBuildBusEventToleratesGarbage(t *testing.T) {
	ev := buildBusEvent("codex", []byte("not json at all"), sharedconfig.ProjectsConfig{})
	if ev.Source != "auto/hooks/codex" {
		t.Fatalf("expected source auto/hooks/codex for garbage input, got %q", ev.Source)
	}
	if ev.Time == "" {
		t.Fatal("expected time set even for garbage input")
	}
	if errs := ev.Validate(); len(errs) > 0 {
		t.Fatalf("even garbage input should produce a valid envelope: %v", errs)
	}
}

func TestMapEventType(t *testing.T) {
	tests := []struct {
		event, tool, want string
	}{
		{"PostToolUse", "Edit", "agent.tool.post"},
		{"PostToolUse", "", "agent.posttooluse"},
		{"SessionStart", "", "agent.session.start"},
		{"SessionEnd", "", "agent.session.end"},
		{"PreToolUse", "", "agent.tool.pre"},
		{"", "", "agent.unknown"},
		{"CustomEvent", "", "agent.customevent"},
	}
	for _, tt := range tests {
		got := mapEventType(tt.event, tt.tool)
		if got != tt.want {
			t.Errorf("mapEventType(%q, %q) = %q, want %q", tt.event, tt.tool, got, tt.want)
		}
	}
}

func TestPostBusEventDelivers(t *testing.T) {
	received := make(chan bus.Notification, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/rpc" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var notif bus.Notification
		_ = json.NewDecoder(r.Body).Decode(&notif)
		received <- notif
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	// Build a realistic event with paths.
	tp := bus.ToolPost{
		Tool:  "Edit",
		Event: "PostToolUse",
		Paths: []bus.PathRef{{Rel: "docs/plan.md", Abs: "/repos/widgets/docs/plan.md"}},
	}
	ev, _ := bus.NewEvent("agent.tool.post", "auto/hooks/claude", tp)
	ev.Project = "widgets"

	postBusEvent(port, ev)

	select {
	case notif := <-received:
		// Verify it's a valid JSON-RPC notification wrapping a bus.Event.
		if notif.JSONRPC != "2.0" {
			t.Errorf("jsonrpc = %q, want 2.0", notif.JSONRPC)
		}
		if notif.Method != "agent.tool.post" {
			t.Errorf("method = %q, want agent.tool.post", notif.Method)
		}
		if !strings.HasPrefix(notif.Params.Type, "agent.") {
			t.Errorf("event type = %q, want agent.* prefix", notif.Params.Type)
		}
		if notif.Params.Project != "widgets" {
			t.Errorf("project = %q, want widgets", notif.Params.Project)
		}
		if errs := notif.Params.Validate(); len(errs) > 0 {
			t.Errorf("received event fails validation: %v", errs)
		}

		// Verify paths in the data payload.
		tp2, err := bus.DecodeData[bus.ToolPost](notif.Params)
		if err != nil {
			t.Fatalf("decode ToolPost from received event: %v", err)
		}
		if len(tp2.Paths) != 1 {
			t.Fatalf("expected 1 path, got %d", len(tp2.Paths))
		}
		if tp2.Paths[0].Rel != "docs/plan.md" {
			t.Errorf("rel = %q, want docs/plan.md", tp2.Paths[0].Rel)
		}
		if tp2.Paths[0].Abs != "/repos/widgets/docs/plan.md" {
			t.Errorf("abs = %q, want /repos/widgets/docs/plan.md", tp2.Paths[0].Abs)
		}
	default:
		t.Fatal("server did not receive the bus event")
	}
}

// TestFireExitsZeroWhenUIDown verifies the command never errors at runtime, even
// when no UI server is listening — a hook must not break the agent.
func TestFireExitsZeroWhenUIDown(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no ui/settings.json → default port, nothing listening

	cmd := newHooksFireCmd()
	cmd.SetArgs([]string{"--agent", "claude"})
	cmd.SetIn(strings.NewReader(claudePostToolUse))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected exit 0 when UI is down, got error: %v", err)
	}
}

func TestFireRejectsInvalidAgent(t *testing.T) {
	cmd := newHooksFireCmd()
	cmd.SetArgs([]string{"--agent", "bogus"})
	cmd.SetIn(strings.NewReader("{}"))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid --agent")
	}
}

func TestFireWritesDurableLog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newHooksFireCmd()
	cmd.SetArgs([]string{"--agent", "claude"})
	cmd.SetIn(strings.NewReader(claudePostToolUse))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("fire returned error: %v", err)
	}

	// Find the log file.
	rawDir, err := hooks.RawDir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(rawDir)
	if err != nil {
		t.Fatalf("raw dir not created: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 log file, got %d", len(entries))
	}

	data, err := os.ReadFile(filepath.Join(rawDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}

	var env hooks.Envelope
	if err := json.Unmarshal(bytes.TrimSpace(data), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.Agent != "claude" {
		t.Errorf("agent = %q, want claude", env.Agent)
	}
	if env.CapturedAt == "" {
		t.Error("capturedAt should be set")
	}
	if env.HostID == "" {
		t.Error("hostId should be set")
	}
	// Verify payload is the verbatim input.
	var gotPayload map[string]any
	if err := json.Unmarshal(env.Payload, &gotPayload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if gotPayload["hook_event_name"] != "PostToolUse" {
		t.Errorf("payload hook_event_name = %v, want PostToolUse", gotPayload["hook_event_name"])
	}
}

func TestFireSwallowsLogFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Make the raw dir path a regular file so MkdirAll fails.
	hooksDir := filepath.Join(home, ".auto", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rawFile := filepath.Join(hooksDir, "raw")
	if err := os.WriteFile(rawFile, []byte("block"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up a test server to verify POST still fires.
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posted = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	// Write UI settings to point at the test server.
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	uiDir := filepath.Join(home, ".auto", "ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsJSON, _ := json.Marshal(map[string]int{"port": port})
	if err := os.WriteFile(filepath.Join(uiDir, "settings.json"), settingsJSON, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newHooksFireCmd()
	cmd.SetArgs([]string{"--agent", "claude"})
	cmd.SetIn(strings.NewReader(claudePostToolUse))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected exit 0 even with log failure, got: %v", err)
	}
	if !posted {
		t.Error("POST should still fire even when log append fails")
	}
}
