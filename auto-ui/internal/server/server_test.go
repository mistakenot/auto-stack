package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mistakenot/auto-ui/internal/server"
)

// localReq builds a request whose RemoteAddr is loopback, matching real
// loopback-bound traffic so the loopback-only guard (see loopback_test.go)
// admits it. httptest.NewRequest defaults RemoteAddr to a non-loopback
// TEST-NET-1 address (192.0.2.1), which the guard correctly 403s.
func localReq(method, target string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = "127.0.0.1:55555"
	return req
}

// newTestFS returns an in-memory filesystem with an index.html that mimics the
// SPA shell, so the server tests do not depend on build tags or the real embed.
func newTestFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!doctype html><html><body><div id="app"></div></body></html>`),
		},
	}
}

// TestAPIHello covers AC-3 (server side): /api/hello returns JSON with a
// non-empty message and the mode that was passed into New.
func TestAPIHello(t *testing.T) {
	handler := server.New(newTestFS(), "test")

	req := localReq(http.MethodGet, "/api/hello")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	ct := rec.Header().Get("Content-Type")
	if mediaType, _, _ := strings.Cut(ct, ";"); strings.TrimSpace(mediaType) != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Message string `json:"message"`
		Mode    string `json:"mode"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Message == "" {
		t.Errorf("message is empty, want non-empty")
	}
	if body.Mode == "" {
		t.Errorf("mode is empty, want non-empty")
	}
	if body.Mode != "test" {
		t.Errorf("mode = %q, want %q", body.Mode, "test")
	}
}

// TestAPIHelloMethodNotAllowed asserts /api/hello rejects non-GET methods with
// 405 and an Allow: GET header.
func TestAPIHelloMethodNotAllowed(t *testing.T) {
	handler := server.New(newTestFS(), "test")

	req := localReq(http.MethodPost, "/api/hello")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
		t.Errorf("Allow = %q, want %q", allow, http.MethodGet)
	}
}

// TestServeIndex covers AC-4: GET / returns 200 with an HTML body containing
// the SPA mount point.
func TestServeIndex(t *testing.T) {
	handler := server.New(newTestFS(), "test")

	req := localReq(http.MethodGet, "/")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want it to contain text/html", ct)
	}

	bodyBytes, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(bodyBytes), `id="app"`) {
		t.Errorf("body does not contain %q", `id="app"`)
	}
}

// TestRPCRouteRemoved covers AC-5: the local ingest endpoint is gone. After 047
// hooks post to autowatch's hook-ingest, not auto-ui, so POST /api/rpc no longer
// routes to a handler and falls through to the SPA file server, which 404s the
// unknown path.
func TestRPCRouteRemoved(t *testing.T) {
	handler := server.New(newTestFS(), "test")

	req := localReq(http.MethodPost, "/api/rpc")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /api/rpc status = %d, want %d (route removed)", rec.Code, http.StatusNotFound)
	}
}

// TestMissingAsset asserts that a request for a non-existent file 404s.
func TestMissingAsset(t *testing.T) {
	handler := server.New(newTestFS(), "test")

	req := localReq(http.MethodGet, "/nope.js")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestCacheControlByMode enables the AC-2 dev edit→refresh loop: in disk (dev)
// mode static assets must be served with Cache-Control: no-store so a plain
// browser reload re-fetches edited files; in embed mode they are not.
func TestCacheControlByMode(t *testing.T) {
	tests := []struct {
		mode        string
		wantNoStore bool
	}{
		{mode: "disk", wantNoStore: true},
		{mode: "embed", wantNoStore: false},
	}
	for _, tt := range tests {
		handler := server.New(newTestFS(), tt.mode)
		req := localReq(http.MethodGet, "/")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		got := rec.Header().Get("Cache-Control")
		if tt.wantNoStore && got != "no-store" {
			t.Errorf("mode=%q Cache-Control = %q, want no-store", tt.mode, got)
		}
		if !tt.wantNoStore && got == "no-store" {
			t.Errorf("mode=%q Cache-Control = no-store, want it unset", tt.mode)
		}
	}
}
