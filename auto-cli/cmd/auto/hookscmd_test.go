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
	type captured struct {
		notif       bus.Notification
		path        string
		contentType string
		origin      string
	}
	received := make(chan captured, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		var notif bus.Notification
		_ = json.NewDecoder(r.Body).Decode(&notif)
		received <- captured{
			notif:       notif,
			path:        r.URL.Path,
			contentType: r.Header.Get("Content-Type"),
			origin:      r.Header.Get("Origin"),
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)

	// Build a realistic event with paths.
	tp := bus.ToolPost{
		Tool:  "Edit",
		Event: "PostToolUse",
		Paths: []bus.PathRef{{Rel: "docs/plan.md", Abs: "/repos/widgets/docs/plan.md"}},
	}
	ev, _ := bus.NewEvent("agent.tool.post", "auto/hooks/claude", tp)
	ev.Project = "widgets"

	// autowatch's HookIngest is mounted as the root handler — post to the bare host.
	postBusEvent(u.Host, ev)

	select {
	case got := <-received:
		// autowatch mounts HookIngest at the root path (no /api/rpc segment).
		if got.path != "/" {
			t.Errorf("post path = %q, want / (autowatch hook-ingest root)", got.path)
		}
		if got.contentType != "application/json" {
			t.Errorf("content-type = %q, want application/json", got.contentType)
		}
		// autowatch rejects browser-origin (CSRF) requests; the CLI must not send one.
		if got.origin != "" {
			t.Errorf("Origin = %q, want absent", got.origin)
		}
		notif := got.notif
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

// TestFireExitsZeroWhenDaemonDown verifies the command never errors at runtime,
// even when no autowatch daemon is listening — a hook must not break the agent.
// It also confirms the durable append (the canonical record) is still written
// when the best-effort live post is dropped.
func TestFireExitsZeroWhenDaemonDown(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// No daemon.pid.json → default addr; nothing listening there. Point at an
	// addr guaranteed to refuse so the post fails fast within the timeout.
	t.Setenv("AUTO_WATCH_HOOK_ADDR", "127.0.0.1:0")

	cmd := newHooksFireCmd()
	cmd.SetArgs([]string{"--agent", "claude"})
	cmd.SetIn(strings.NewReader(claudePostToolUse))
	var errBuf bytes.Buffer
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errBuf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected exit 0 when daemon is down, got error: %v", err)
	}
	// A dropped live post is silent — nothing should be emitted to the agent.
	if errBuf.Len() != 0 {
		t.Errorf("expected no error output when daemon down, got: %q", errBuf.String())
	}

	// The durable append must still have been written despite the dropped post.
	rawDir, err := hooks.RawDir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(rawDir)
	if err != nil {
		t.Fatalf("durable append not written when daemon down: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 durable log file, got %d", len(entries))
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

	// Point the hook at the test server standing in for autowatch hook-ingest.
	u, _ := url.Parse(srv.URL)
	t.Setenv("AUTO_WATCH_HOOK_ADDR", u.Host)

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

// TestWatchHookAddrPrecedence covers AC-2: watchHookAddr() resolves
// AUTO_WATCH_HOOK_ADDR before reading ~/.auto/watch/daemon.pid.json `.hookAddr`,
// falls back to the built-in default when both are absent, and degrades to the
// default for malformed/missing metadata. The env override lets an agent harness
// point hooks at an isolated daemon instance.
func TestWatchHookAddrPrecedence(t *testing.T) {
	// writePIDMeta writes a daemon.pid.json under home with the given hookAddr.
	writePIDMeta := func(t *testing.T, home string, body string) {
		t.Helper()
		watchDir := filepath.Join(home, ".auto", "watch")
		if err := os.MkdirAll(watchDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(watchDir, "daemon.pid.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("env wins over pid metadata and default", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		writePIDMeta(t, home, `{"hookAddr":"127.0.0.1:9999"}`)
		t.Setenv("AUTO_WATCH_HOOK_ADDR", "127.0.0.1:5555")
		if got := watchHookAddr(); got != "127.0.0.1:5555" {
			t.Errorf("watchHookAddr() = %q, want 127.0.0.1:5555 (env)", got)
		}
	})

	t.Run("pid metadata hookAddr used when env unset", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("AUTO_WATCH_HOOK_ADDR", "")
		writePIDMeta(t, home, `{"pid":42,"hookAddr":"127.0.0.1:7001","rpcAddr":"127.0.0.1:7002"}`)
		if got := watchHookAddr(); got != "127.0.0.1:7001" {
			t.Errorf("watchHookAddr() = %q, want 127.0.0.1:7001 (from daemon.pid.json)", got)
		}
	})

	t.Run("default when pid metadata absent", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("AUTO_WATCH_HOOK_ADDR", "")
		if got := watchHookAddr(); got != defaultWatchHookAddr {
			t.Errorf("watchHookAddr() = %q, want default %q", got, defaultWatchHookAddr)
		}
	})

	t.Run("default for malformed or empty metadata", func(t *testing.T) {
		t.Setenv("AUTO_WATCH_HOOK_ADDR", "")
		for _, body := range []string{`not json`, `{}`, `{"hookAddr":""}`, `{"rpcAddr":"127.0.0.1:1"}`} {
			home := t.TempDir()
			t.Setenv("HOME", home)
			writePIDMeta(t, home, body)
			if got := watchHookAddr(); got != defaultWatchHookAddr {
				t.Errorf("watchHookAddr() with metadata %q = %q, want default %q", body, got, defaultWatchHookAddr)
			}
		}
	})
}
