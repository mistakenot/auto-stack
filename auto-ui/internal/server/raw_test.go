package server_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/mistakenot/auto-shared/rpc"
)

// TestDocRawProxiesBackend covers AC-4: GET /api/doc/raw returns the
// base64-decoded raw bytes from the backend's doc.raw with Content-Type taken
// from the backend's contentType — byte-identical to the canned content.
func TestDocRawProxiesBackend(t *testing.T) {
	srv := newProxyServer(t, nil)

	resp, err := http.Get(srv.URL + "/api/doc/raw?project=alpha&path=docs/tasks/page.html")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != rawHTMLBody {
		t.Errorf("body = %q, want verbatim %q", string(body), rawHTMLBody)
	}
}

// TestDocRawDefaultContentType verifies an empty backend contentType falls back
// to text/html; charset=utf-8.
func TestDocRawDefaultContentType(t *testing.T) {
	const body = "<p>no content type</p>"
	srv := newProxyServer(t, map[string]rpc.Handler{
		"doc.raw": func(_ context.Context, _ json.RawMessage) (any, error) {
			return map[string]any{
				"path":          "docs/x.html",
				"contentType":   "",
				"contentBase64": base64.StdEncoding.EncodeToString([]byte(body)),
			}, nil
		},
	})

	resp, err := http.Get(srv.URL + "/api/doc/raw?project=alpha&path=docs/x.html")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want fallback text/html; charset=utf-8", ct)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != body {
		t.Errorf("body = %q, want %q", string(got), body)
	}
}

// TestDocRawMissingPath verifies a missing path param returns 400 — checked
// before resolving the backend.
func TestDocRawMissingPath(t *testing.T) {
	srv := newProxyServer(t, nil)

	resp, err := http.Get(srv.URL + "/api/doc/raw?project=alpha")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for missing path", resp.StatusCode)
	}
}

// TestDocRawRejectsNonGet verifies a non-GET method is rejected with 405.
func TestDocRawRejectsNonGet(t *testing.T) {
	srv := newProxyServer(t, nil)

	resp, err := http.Post(srv.URL+"/api/doc/raw?project=alpha&path=docs/tasks/page.html", "text/plain", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405 for POST", resp.StatusCode)
	}
}

// TestDocRawBackendErrorIs404 covers AC-8 (clean break): a backend error for
// doc.raw maps to 404 — never local bytes, never a leak.
func TestDocRawBackendErrorIs404(t *testing.T) {
	srv := newProxyServer(t, map[string]rpc.Handler{
		"doc.raw": func(_ context.Context, _ json.RawMessage) (any, error) {
			return nil, &rpc.Error{Code: -32004, Message: "doc not found"}
		},
	})

	resp, err := http.Get(srv.URL + "/api/doc/raw?project=alpha&path=docs/secret.html")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for backend error", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) == "<secret>" {
		t.Errorf("body leaked backend detail: %q", string(body))
	}
}
