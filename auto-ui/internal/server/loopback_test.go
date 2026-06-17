package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// okHandler is a sentinel inner handler: if the guard lets a request through,
// it answers 200 "ok" so tests can distinguish "served" from "403'd".
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
})

func TestLoopbackOnly(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		wantServed bool
	}{
		{name: "ipv4 loopback", remoteAddr: "127.0.0.1:55555", wantServed: true},
		{name: "ipv4 loopback range", remoteAddr: "127.0.0.5:1", wantServed: true},
		{name: "ipv6 loopback", remoteAddr: "[::1]:55555", wantServed: true},
		{name: "lan peer", remoteAddr: "192.168.1.20:40000", wantServed: false},
		{name: "tailnet peer direct", remoteAddr: "100.101.102.103:40000", wantServed: false},
		{name: "public peer", remoteAddr: "8.8.8.8:443", wantServed: false},
		{name: "empty remote addr", remoteAddr: "", wantServed: false},
		{
			// A non-loopback peer cannot smuggle itself past the guard by
			// claiming a loopback origin in forwarded headers.
			name:       "spoofed forwarded headers ignored",
			remoteAddr: "8.8.8.8:443",
			headers:    map[string]string{"X-Forwarded-For": "127.0.0.1", "X-Real-IP": "127.0.0.1"},
			wantServed: false,
		},
		{
			// Mirror of tailscale serve: the real peer is loopback (tailscaled
			// proxied to the backend); the tailnet client IP sits in the header
			// and must not change the allow decision.
			name:       "loopback peer with forwarded header allowed",
			remoteAddr: "127.0.0.1:55555",
			headers:    map[string]string{"X-Forwarded-For": "100.101.102.103"},
			wantServed: true,
		},
	}

	guarded := loopbackOnly(okHandler)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/hello", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, req)

			if tt.wantServed {
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200 (served)", rec.Code)
				}
				return
			}
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "localhost") {
				t.Errorf("403 body missing explanation, got: %q", body)
			}
		})
	}
}
