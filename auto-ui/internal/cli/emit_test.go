package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"testing/fstest"

	"github.com/mistakenot/auto-shared/bus"
	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-ui/internal/app"
	"github.com/mistakenot/auto-ui/internal/server"
)

// emitTestRegistry returns a one-project fixture registry used by the emit e2e
// tests. The project id matches the --project value emit stamps on the envelope,
// so DeriveDocChanged resolves it and derives a doc.changed.
func emitTestRegistry() sharedconfig.ProjectsConfig {
	return sharedconfig.ProjectsConfig{
		Projects: []sharedconfig.ProjectRef{{
			ID:     "test-proj",
			Path:   "/fake/project",
			Remote: "https://github.com/test/repo.git",
		}},
	}
}

// startEmitServer stands up an httptest server backed by server.New with the
// debug buffer enabled and a fixture registry, and returns its bound TCP port.
func startEmitServer(t *testing.T) (*httptest.Server, int) {
	t.Helper()
	reg := emitTestRegistry()
	handler := server.New(fstest.MapFS{}, "test",
		server.WithRegistryProvider(func() sharedconfig.ProjectsConfig { return reg }),
		server.WithDebug(true),
	)
	srv := httptest.NewServer(handler)
	u, err := url.Parse(srv.URL)
	if err != nil {
		srv.Close()
		t.Fatalf("parse server url %q: %v", srv.URL, err)
	}
	// httptest binds 127.0.0.1, which matches the loopback address emit POSTs to.
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		srv.Close()
		t.Fatalf("parse server port %q: %v", u.Port(), err)
	}
	return srv, port
}

// TestEmitDerivesDocChanged runs `auto ui emit` against a debug-enabled server
// for a docs/**/*.md path and asserts the POST is accepted (no Origin) and that
// exactly one derived doc.changed appears in /api/debug/recent.
func TestEmitDerivesDocChanged(t *testing.T) {
	srv, port := startEmitServer(t)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	application := app.New(&stdout, &stderr)
	cmd := newEmitCmd(application)
	cmd.SetArgs([]string{
		"--project", "test-proj",
		"--path", "docs/tasks/test.md",
		"--worktree", "/fake/project",
		"--port", strconv.Itoa(port),
	})
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("emit Execute: %v\nstderr: %s", err, stderr.String())
	}

	// stdout must carry a parseable JSON payload reporting the accepted POST.
	var out struct {
		Emitted bool   `json:"emitted"`
		Status  int    `json:"status"`
		Type    string `json:"type"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("emit stdout not JSON: %v\nstdout: %s", err, stdout.String())
	}
	if !out.Emitted || out.Status != http.StatusNoContent {
		t.Fatalf("emit result = %+v, want emitted=true status=204", out)
	}
	if out.Type != "agent.tool.post" {
		t.Errorf("emit type = %q, want agent.tool.post", out.Type)
	}

	// The debug buffer should now hold the raw event and exactly one derived
	// doc.changed.
	events := getRecent(t, srv)
	var raw, derived int
	for _, e := range events {
		switch e.Type {
		case "agent.tool.post":
			raw++
		case "doc.changed":
			derived++
		}
	}
	if raw != 1 {
		t.Errorf("raw agent.tool.post count = %d, want 1: %+v", raw, events)
	}
	if derived != 1 {
		t.Errorf("derived doc.changed count = %d, want 1: %+v", derived, events)
	}
}

// TestEmitOriginRejected documents the Origin rule the emit command exists to
// sidestep: a request carrying an Origin header is rejected with 403, whereas
// emit itself never sets one. This POSTs directly (emit does not expose an Origin
// option) to the same server to assert the guard.
func TestEmitOriginRejected(t *testing.T) {
	srv, _ := startEmitServer(t)
	defer srv.Close()

	ev, err := bus.NewEvent("agent.tool.post", "auto/ui/emit", bus.ToolPost{
		Tool:  "Edit",
		Event: "PostToolUse",
		Paths: []bus.PathRef{{Rel: "docs/tasks/test.md", Abs: "/fake/project/docs/tasks/test.md"}},
	})
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	ev.Project = "test-proj"
	body, err := json.Marshal(ev.AsNotification())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/rpc", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST with Origin: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Origin-bearing POST status = %d, want 403", resp.StatusCode)
	}
}

// getRecent fetches and decodes the debug buffer's recorded events.
func getRecent(t *testing.T, srv *httptest.Server) []bus.Event {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/debug/recent")
	if err != nil {
		t.Fatalf("GET /api/debug/recent: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/debug/recent status = %d, want 200", resp.StatusCode)
	}
	var events []bus.Event
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	return events
}
