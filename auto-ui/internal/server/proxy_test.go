package server_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-ui/internal/backend"
	uiconfig "github.com/mistakenot/auto-ui/internal/config"
	"github.com/mistakenot/auto-ui/internal/server"
)

// proxyHostID is the host id the fake backend reports from daemon.status.
const proxyHostID = "host-a"

// rawHTMLBody is the canned body the fake backend returns from doc.raw, base64
// in the wire payload and expected verbatim after decode.
const rawHTMLBody = "<!doctype html><h1>Page</h1><p>verbatim</p>"

// defaultBackendHandlers returns canned doc.list/doc.get/project.list/doc.raw
// handlers modelling a single autowatch backend. project.list entries carry a
// `host` field per GR-F8; doc.list entries carry `meta`.
func defaultBackendHandlers() map[string]rpc.Handler {
	return map[string]rpc.Handler{
		"doc.list": func(_ context.Context, _ json.RawMessage) (any, error) {
			return []any{
				map[string]any{"id": "docs/readme.md", "path": "docs/readme.md", "type": "markdown"},
				map[string]any{
					"id":   "docs/tasks/042-test/plan.html",
					"path": "docs/tasks/042-test/plan.html",
					"type": "html",
					"meta": map[string]any{
						"status":      "executing",
						"branch":      "task/042-test",
						"reviewState": "approved",
					},
				},
			}, nil
		},
		"doc.get": func(_ context.Context, _ json.RawMessage) (any, error) {
			return map[string]any{
				"path":     "docs/readme.md",
				"markdown": "# Readme\nTop-level doc.",
			}, nil
		},
		"project.list": func(_ context.Context, _ json.RawMessage) (any, error) {
			return []any{
				map[string]any{
					"id":     "alpha",
					"name":   "Alpha",
					"path":   "/home/u/alpha",
					"remote": "https://github.com/owner/alpha",
					"host":   proxyHostID,
				},
			}, nil
		},
		"doc.raw": func(_ context.Context, _ json.RawMessage) (any, error) {
			return map[string]any{
				"path":          "docs/tasks/page.html",
				"contentType":   "text/html; charset=utf-8",
				"contentBase64": base64.StdEncoding.EncodeToString([]byte(rawHTMLBody)),
			}, nil
		},
	}
}

// startFakeBackend wires sConn to an rpc.Peer serving daemon.status (returning
// proxyHostID) plus the supplied method handlers, and runs it until t cleanup.
func startFakeBackend(t *testing.T, sConn net.Conn, handlers map[string]rpc.Handler) {
	t.Helper()
	opts := []rpc.Option{
		rpc.WithHandler("daemon.status", func(_ context.Context, _ json.RawMessage) (any, error) {
			return map[string]any{
				"hostId":        proxyHostID,
				"version":       "test",
				"uptimeSeconds": 1,
				"pid":           1,
				"startedAt":     "2026-01-01T00:00:00Z",
			}, nil
		}),
	}
	for m, h := range handlers {
		opts = append(opts, rpc.WithHandler(m, h))
	}
	peer := rpc.NewPeer(sConn, opts...)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = peer.Serve(ctx) }()
}

// newProxyServer builds an httptest.Server fronted by a server wired to a
// backend.Manager that resolves to a single in-process fake backend. A nil
// overrides map uses the full default handler set; non-nil entries replace the
// matching default method.
func newProxyServer(t *testing.T, overrides map[string]rpc.Handler) *httptest.Server {
	t.Helper()

	handlers := defaultBackendHandlers()
	maps.Copy(handlers, overrides)

	const uri = "unix:///fake/proxy.sock"
	path := filepath.Join(t.TempDir(), "backends.json")
	if err := uiconfig.SaveBackends(path, uiconfig.BackendsConfig{
		Backends: []uiconfig.Backend{{URI: uri}},
	}); err != nil {
		t.Fatalf("SaveBackends: %v", err)
	}

	dial := func(_ context.Context, u string) (net.Conn, error) {
		if u != uri {
			t.Errorf("unexpected dial uri: %s", u)
		}
		sConn, cConn := net.Pipe()
		startFakeBackend(t, sConn, handlers)
		return cConn, nil
	}

	mgr := backend.NewManager(path, dial, 0)
	mgr.Reconcile(context.Background())

	// Wait until the backend's host id is learned so Resolve("") succeeds.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := mgr.Resolve(""); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake backend did not connect in time")
		}
		time.Sleep(5 * time.Millisecond)
	}

	srv := httptest.NewServer(server.New(newTestFS(), "test", server.WithBackendManager(mgr)))
	t.Cleanup(srv.Close)
	return srv
}

// rpcCall sends a JSON-RPC request over WS and reads the correlated response.
func rpcCall(ctx context.Context, t *testing.T, c *websocket.Conn, id int, method string, params any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  json.RawMessage(raw),
	}
	reqBytes, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal req: %v", err)
	}
	if err := c.Write(ctx, websocket.MessageText, reqBytes); err != nil {
		t.Fatalf("write %s: %v", method, err)
	}

	return readUntil(ctx, t, c, func(m map[string]any) bool {
		mid, ok := m["id"].(float64)
		return ok && mid == float64(id)
	})
}

// TestProxyRoutesToSingleBackend covers AC-6: with one backend connected, a call
// with no explicit host routes to it.
func TestProxyRoutesToSingleBackend(t *testing.T) {
	srv := newProxyServer(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "doc.list", map[string]string{})
	if resp["error"] != nil {
		t.Fatalf("doc.list error: %v", resp["error"])
	}
	if _, ok := resp["result"].([]any); !ok {
		t.Fatalf("doc.list result not an array: %T %v", resp["result"], resp["result"])
	}
}

// TestProxyUnknownHostErrors covers AC-6: an explicit unknown host yields a
// clean JSON-RPC error, never local data.
func TestProxyUnknownHostErrors(t *testing.T) {
	srv := newProxyServer(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "doc.list", map[string]string{"host": "nope"})
	if resp["error"] == nil {
		t.Fatalf("expected error for unknown host, got result %v", resp["result"])
	}
	errObj, _ := resp["error"].(map[string]any)
	if msg, _ := errObj["message"].(string); msg != "unknown host" {
		t.Errorf("error message = %q, want %q", msg, "unknown host")
	}
}

// TestProxyNoManagerErrors verifies that when no backend manager is configured,
// doc/project routes return a clear error instead of touching local data.
func TestProxyNoManagerErrors(t *testing.T) {
	srv := httptest.NewServer(server.New(newTestFS(), "test")) // no WithBackendManager
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "doc.list", map[string]string{})
	if resp["error"] == nil {
		t.Fatalf("expected error with no backend configured, got result %v", resp["result"])
	}
	errObj, _ := resp["error"].(map[string]any)
	if msg, _ := errObj["message"].(string); msg != "no backend configured" {
		t.Errorf("error message = %q, want %q", msg, "no backend configured")
	}
}
