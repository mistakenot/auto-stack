package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-ui/internal/server"
)

// testRegistry returns a ProjectsConfig fixture with one registered project.
func testRegistry() config.ProjectsConfig {
	return config.ProjectsConfig{
		Projects: []config.ProjectRef{{
			ID:     "test-proj",
			Path:   "/fake/project",
			Remote: "https://github.com/test/repo.git",
		}},
	}
}

// validToolPostEvent builds a valid agent.tool.post bus.Event targeting a doc
// path in the test-proj project.
func validToolPostEvent(t *testing.T, relPath string) bus.Event {
	t.Helper()
	tp := bus.ToolPost{
		Tool:  "Edit",
		Event: "PostToolUse",
		Paths: []bus.PathRef{{Rel: relPath, Abs: "/fake/project/" + relPath}},
	}
	ev, err := bus.NewEvent("agent.tool.post", "auto/hooks/claude", tp)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	ev.Project = "test-proj"
	ev.Worktree = "/fake/project"
	ev.Branch = "main"
	return ev
}

// postRPC POSTs a JSON-RPC notification frame to the /api/rpc endpoint.
func postRPC(t *testing.T, srv *httptest.Server, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/rpc", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/rpc: %v", err)
	}
	return resp
}

// TestRPCIngestBroadcastAndDerive verifies that POSTing a valid agent.tool.post
// event to /api/rpc broadcasts it to a connected WebSocket client, and also
// derives and broadcasts a doc.changed event when the path matches docs/**/*.md.
func TestRPCIngestBroadcastAndDerive(t *testing.T) {
	reg := testRegistry()
	handler := server.New(newTestFS(), "test", server.WithRegistryProvider(func() config.ProjectsConfig {
		return reg
	}))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	// Wait for at least one ping to confirm the WS connection is established.
	readUntil(ctx, t, c, func(m map[string]any) bool {
		return m["method"] == "ping"
	})

	// POST a valid agent.tool.post event with a docs/ path.
	ev := validToolPostEvent(t, "docs/tasks/test.md")
	frame := bus.Notification{
		JSONRPC: "2.0",
		Method:  ev.Type,
		Params:  ev,
	}
	body, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}

	resp := postRPC(t, srv, body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	// The WS client should receive the raw agent.tool.post notification.
	rawMsg := readUntil(ctx, t, c, func(m map[string]any) bool {
		return m["method"] == "agent.tool.post"
	})
	params, ok := rawMsg["params"].(map[string]any)
	if !ok {
		t.Fatalf("agent.tool.post missing params: %v", rawMsg)
	}
	if params["type"] != "agent.tool.post" {
		t.Errorf("params.type = %v, want agent.tool.post", params["type"])
	}

	// Next, a doc.changed notification should arrive.
	docMsg := readUntil(ctx, t, c, func(m map[string]any) bool {
		return m["method"] == "doc.changed"
	})
	docParams, ok := docMsg["params"].(map[string]any)
	if !ok {
		t.Fatalf("doc.changed missing params: %v", docMsg)
	}
	if docParams["type"] != "doc.changed" {
		t.Errorf("params.type = %v, want doc.changed", docParams["type"])
	}

	// AC-1 (026): the client reads the changed path from params.data.path (the
	// full event envelope under params, data payload under data). Pin that shape
	// so the wire contract the explorer's liveness depends on can't silently regress.
	docData, ok := docParams["data"].(map[string]any)
	if !ok {
		t.Fatalf("doc.changed params.data missing or wrong type: %v", docParams["data"])
	}
	if docData["path"] != "docs/tasks/test.md" {
		t.Errorf("params.data.path = %v, want docs/tasks/test.md", docData["path"])
	}
}

// TestRPCIngestNonDocNoDerived verifies that a valid agent.tool.post with a
// non-docs path broadcasts the raw event but does NOT derive doc.changed.
func TestRPCIngestNonDocNoDerived(t *testing.T) {
	reg := testRegistry()
	handler := server.New(newTestFS(), "test", server.WithRegistryProvider(func() config.ProjectsConfig {
		return reg
	}))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	readUntil(ctx, t, c, func(m map[string]any) bool {
		return m["method"] == "ping"
	})

	// POST a valid event with a non-docs path.
	ev := validToolPostEvent(t, "src/main.go")
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

	// Should receive the raw event.
	readUntil(ctx, t, c, func(m map[string]any) bool {
		return m["method"] == "agent.tool.post"
	})

	// Should NOT receive doc.changed — wait a bit and confirm only pings arrive.
	shortCtx, shortCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer shortCancel()

	for {
		_, data, err := c.Read(shortCtx)
		if err != nil {
			break // timeout or close — expected
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg["method"] == "doc.changed" {
			t.Fatal("unexpected doc.changed received for non-docs path")
		}
	}
}

// TestRPCIngestMalformed verifies that a malformed POST body returns 400
// and the WS client receives nothing from it.
func TestRPCIngestMalformed(t *testing.T) {
	handler := server.New(newTestFS(), "test")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	readUntil(ctx, t, c, func(m map[string]any) bool {
		return m["method"] == "ping"
	})

	// POST garbage.
	resp := postRPC(t, srv, []byte(`{not valid json`))
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	// The WS client should receive only pings, not any broadcast.
	shortCtx, shortCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer shortCancel()

	for {
		_, data, err := c.Read(shortCtx)
		if err != nil {
			break
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		method, _ := msg["method"].(string)
		if method != "ping" {
			t.Fatalf("unexpected method %q received after malformed POST", method)
		}
	}
}

// TestRPCIngestInvalidEnvelope verifies that a JSON-parseable but invalid
// envelope (missing required fields) returns 400.
func TestRPCIngestInvalidEnvelope(t *testing.T) {
	handler := server.New(newTestFS(), "test")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Valid JSON but envelope has no required fields.
	body := []byte(`{"jsonrpc":"2.0","method":"agent.tool.post","params":{}}`)
	resp := postRPC(t, srv, body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestRPCIngestMethodNotAllowed verifies that GET /api/rpc returns 405.
func TestRPCIngestMethodNotAllowed(t *testing.T) {
	handler := server.New(newTestFS(), "test")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/rpc")
	if err != nil {
		t.Fatalf("GET /api/rpc: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}
