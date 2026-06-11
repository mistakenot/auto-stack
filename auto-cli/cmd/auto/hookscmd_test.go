package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

const claudePostToolUse = `{
  "hook_event_name": "PostToolUse",
  "session_id": "sess-123",
  "cwd": "/repos/widgets/docs",
  "tool_name": "Edit",
  "tool_input": {"file_path": "/repos/widgets/docs/plan.md", "old_string": "a", "new_string": "b"}
}`

func TestBuildHookEventNormalizesAndMapsProject(t *testing.T) {
	registry := sharedconfig.ProjectsConfig{Projects: []sharedconfig.ProjectRef{
		{ID: "widgets", Path: "/repos/widgets"},
	}}
	ev := buildHookEvent("claude", []byte(claudePostToolUse), registry)

	if ev.Agent != "claude" || ev.Event != "PostToolUse" || ev.Tool != "Edit" {
		t.Fatalf("unexpected normalization: %+v", ev)
	}
	if ev.SessionID != "sess-123" {
		t.Errorf("session id = %q", ev.SessionID)
	}
	if ev.Project != "widgets" {
		t.Errorf("expected cwd mapped to project 'widgets', got %q", ev.Project)
	}
	if len(ev.Paths) != 1 || ev.Paths[0] != "/repos/widgets/docs/plan.md" {
		t.Errorf("expected plan.md path extracted, got %v", ev.Paths)
	}
	if ev.Timestamp == "" {
		t.Errorf("expected timestamp set")
	}
}

func TestBuildHookEventToleratesGarbage(t *testing.T) {
	ev := buildHookEvent("codex", []byte("not json at all"), sharedconfig.ProjectsConfig{})
	if ev.Agent != "codex" || ev.Timestamp == "" {
		t.Fatalf("expected a usable event even for garbage input, got %+v", ev)
	}
}

func TestPostHookEventDelivers(t *testing.T) {
	received := make(chan HookEvent, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/hooks" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var ev HookEvent
		_ = json.NewDecoder(r.Body).Decode(&ev)
		received <- ev
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	postHookEvent(port, HookEvent{Agent: "claude", Project: "widgets", Tool: "Edit"})

	select {
	case ev := <-received:
		if ev.Project != "widgets" || ev.Tool != "Edit" {
			t.Fatalf("server received wrong event: %+v", ev)
		}
	default:
		t.Fatal("server did not receive the hook event")
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
