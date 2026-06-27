package server_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mistakenot/auto-shared/rpc"
)

// TestDocListProxiesBackend covers AC-3: doc.list over /api/ws returns the
// backend's canned result verbatim, including meta on entries.
func TestDocListProxiesBackend(t *testing.T) {
	srv := newProxyServer(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "doc.list", map[string]string{"project": "alpha"})
	if resp["error"] != nil {
		t.Fatalf("doc.list error: %v", resp["error"])
	}

	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("doc.list result not an array: %T %v", resp["result"], resp["result"])
	}
	if len(result) != 2 {
		t.Fatalf("doc.list returned %d entries, want 2: %v", len(result), result)
	}

	entries := map[string]map[string]any{}
	for _, e := range result {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("entry not a map: %v", e)
		}
		p, _ := m["path"].(string)
		entries[p] = m
	}

	md := entries["docs/readme.md"]
	if md == nil || md["type"] != "markdown" {
		t.Errorf("docs/readme.md entry = %v, want type markdown", md)
	}

	html := entries["docs/tasks/042-test/plan.html"]
	if html == nil {
		t.Fatalf("missing html entry: %v", entries)
	}
	if html["type"] != "html" {
		t.Errorf("html entry type = %v, want html", html["type"])
	}
	meta, ok := html["meta"].(map[string]any)
	if !ok {
		t.Fatalf("html entry meta missing/not a map: %v", html["meta"])
	}
	if meta["status"] != "executing" {
		t.Errorf("meta.status = %v, want executing", meta["status"])
	}
	if meta["reviewState"] != "approved" {
		t.Errorf("meta.reviewState = %v, want approved", meta["reviewState"])
	}
	if meta["branch"] != "task/042-test" {
		t.Errorf("meta.branch = %v, want task/042-test", meta["branch"])
	}
}

// TestDocGetProxiesBackend covers AC-3: doc.get over /api/ws returns the
// backend's canned markdown verbatim.
func TestDocGetProxiesBackend(t *testing.T) {
	srv := newProxyServer(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "doc.get", map[string]string{
		"project": "alpha",
		"path":    "docs/readme.md",
	})
	if resp["error"] != nil {
		t.Fatalf("doc.get error: %v", resp["error"])
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("doc.get result not a map: %v", resp["result"])
	}
	if result["path"] != "docs/readme.md" {
		t.Errorf("path = %v, want docs/readme.md", result["path"])
	}
	if md, _ := result["markdown"].(string); md != "# Readme\nTop-level doc." {
		t.Errorf("markdown = %q, want backend content", md)
	}
}

// TestDocGetBackendErrorPropagates covers AC-8 (clean break): a backend that
// returns an rpc.Error for doc.get surfaces as a JSON-RPC error to the WS
// client — never local file contents.
func TestDocGetBackendErrorPropagates(t *testing.T) {
	srv := newProxyServer(t, map[string]rpc.Handler{
		"doc.get": func(_ context.Context, _ json.RawMessage) (any, error) {
			return nil, &rpc.Error{Code: -32004, Message: "doc not found"}
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "doc.get", map[string]string{
		"project": "alpha",
		"path":    "docs/secret.md",
	})

	if resp["result"] != nil {
		t.Fatalf("expected no result on backend error, got %v", resp["result"])
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %v", resp["error"])
	}
	if msg, _ := errObj["message"].(string); msg != "doc not found" {
		t.Errorf("error message = %q, want backend's %q", msg, "doc not found")
	}
	if code, _ := errObj["code"].(float64); int(code) != -32004 {
		t.Errorf("error code = %v, want -32004 (backend's code preserved)", errObj["code"])
	}
}
