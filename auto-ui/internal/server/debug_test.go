package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-ui/internal/server"
)

// TestDebugRecentEnabled verifies that, with WithDebug(true), POSTing a valid
// agent.tool.post for a docs/**/*.md path records both the raw event and the
// derived doc.changed event, retrievable via GET /api/debug/recent.
func TestDebugRecentEnabled(t *testing.T) {
	reg := testRegistry()
	handler := server.New(newTestFS(), "test",
		server.WithRegistryProvider(func() config.ProjectsConfig { return reg }),
		server.WithDebug(true),
	)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// POST a valid agent.tool.post with a docs/ markdown path.
	ev := validToolPostEvent(t, "docs/tasks/test.md")
	frame := bus.Notification{JSONRPC: "2.0", Method: ev.Type, Params: ev}
	body, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}

	resp := postRPC(t, srv, body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	// GET /api/debug/recent should return the raw event AND one derived doc.changed.
	getResp, err := http.Get(srv.URL + "/api/debug/recent")
	if err != nil {
		t.Fatalf("GET /api/debug/recent: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", getResp.StatusCode, http.StatusOK)
	}

	var events []bus.Event
	if err := json.NewDecoder(getResp.Body).Decode(&events); err != nil {
		t.Fatalf("decode events: %v", err)
	}

	var sawRaw, sawDerived bool
	for _, e := range events {
		switch e.Type {
		case "agent.tool.post":
			sawRaw = true
		case "doc.changed":
			sawDerived = true
		}
	}
	if !sawRaw {
		t.Errorf("recent events missing raw agent.tool.post: %+v", events)
	}
	if !sawDerived {
		t.Errorf("recent events missing derived doc.changed: %+v", events)
	}
	if len(events) != 2 {
		t.Errorf("recent events = %d, want 2 (raw + derived): %+v", len(events), events)
	}
}

// TestDebugRecentDisabled verifies that without WithDebug, GET /api/debug/recent
// returns 404.
func TestDebugRecentDisabled(t *testing.T) {
	handler := server.New(newTestFS(), "test")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/debug/recent")
	if err != nil {
		t.Fatalf("GET /api/debug/recent: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}
