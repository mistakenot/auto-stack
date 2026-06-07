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

	req := httptest.NewRequest(http.MethodGet, "/api/hello", nil)
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

// TestServeIndex covers AC-4: GET / returns 200 with an HTML body containing
// the SPA mount point.
func TestServeIndex(t *testing.T) {
	handler := server.New(newTestFS(), "test")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
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

// TestMissingAsset asserts that a request for a non-existent file 404s.
func TestMissingAsset(t *testing.T) {
	handler := server.New(newTestFS(), "test")

	req := httptest.NewRequest(http.MethodGet, "/nope.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
