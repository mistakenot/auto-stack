package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mistakenot/auto-ui/internal/app"
	"github.com/mistakenot/auto-ui/internal/config"
)

// writeBackendsConfig writes a HOME-isolated backends.json (under ~/.auto/ui,
// which resolves inside the test's temp HOME) pointing serve at uri. HOME must
// already be redirected to a temp dir before calling.
func writeBackendsConfig(t *testing.T, uri string) {
	t.Helper()
	path, err := config.BackendsPath()
	if err != nil {
		t.Fatalf("backends path: %v", err)
	}
	if err := config.SaveBackends(path, config.BackendsConfig{
		Backends: []config.Backend{{URI: uri}},
	}); err != nil {
		t.Fatalf("save backends: %v", err)
	}
}

// waitForFile polls path until it has content or the deadline fires.
func waitForFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return b
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("ready file %s not written within deadline", path)
	return nil
}

// tryProjectList opens a WS connection, issues one project.list call, and
// returns the result entries — or nil if the backend is not yet connected (the
// proxy returns a JSON-RPC error until the manager's first reconcile completes).
// The caller retries on nil.
func tryProjectList(t *testing.T, wsURL string) []map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil) //nolint:bodyclose // resp.Body managed by Dial
	if err != nil {
		return nil
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"jsonrpc":"2.0","id":1,"method":"project.list","params":{}}`)); err != nil {
		return nil
	}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return nil
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil
		}
		id, ok := msg["id"].(float64)
		if !ok || id != 1 {
			continue // skip server-push pings
		}
		if msg["error"] != nil {
			return nil // backend not yet connected; caller retries
		}
		arr, ok := msg["result"].([]any)
		if !ok {
			return nil
		}
		var entries []map[string]any
		for _, e := range arr {
			if m, ok := e.(map[string]any); ok {
				entries = append(entries, m)
			}
		}
		return entries
	}
}

// pollProjectList retries tryProjectList until project.list returns the fake
// backend's host-tagged entry, or the deadline fires. serve's manager uses a 5s
// tick but reconciles once immediately on Run, so the first connection is
// usually in flight before the listener binds; the bounded poll keeps the test
// from racing that first reconcile.
func pollProjectList(t *testing.T, base, wantHost string) []map[string]any {
	t.Helper()
	wsURL := "ws" + base[len("http"):] + "/api/ws"
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entries := tryProjectList(t, wsURL)
		if len(entries) > 0 && entries[0]["host"] == wantHost {
			return entries
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("project.list did not return backend entry (host=%q) within deadline", wantHost)
	return nil
}

// TestServeReadyFileAndIsolation covers AC-5 (serve side): --port 0 binds an
// OS-assigned port, --ready-file records the real bound 127.0.0.1:NNNN address
// as JSON, and /api/hello answers on that port. Post clean-break (Phase 3),
// project.list is no longer served from a local registry: it is proxied from a
// configured autowatch backend. The test stands up a fake in-process backend,
// points a HOME-isolated backends.json at it, and asserts project.list returns
// the backend's host-tagged entry — proving the proxy path end to end.
func TestServeReadyFileAndIsolation(t *testing.T) {
	// Isolate from the host: a temp HOME means backends.json resolves under the
	// test's own ~/.auto/ui, and the default registry resolves to nothing.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AUTO_UI_PORT", "")
	t.Setenv("AUTO_PROJECTS_PATH", "")
	t.Setenv("AUTO_UI_DEBUG", "")

	const hostID = "test-host"
	uri := "unix://" + filepath.Join(t.TempDir(), "backend.sock")
	stopBackend := startFakeBackend(t, uri, hostID, 1)
	defer stopBackend()
	writeBackendsConfig(t, uri)

	readyFile := filepath.Join(t.TempDir(), "ready.json")

	application := app.New(&bytes.Buffer{}, &bytes.Buffer{})
	cmd := newServeCmd(application)
	cmd.SetArgs([]string{"--port", "0", "--ready-file", readyFile})

	// signal.NotifyContext derives from cmd.Context(); cancelling the parent
	// triggers the command's graceful shutdown, so the test can stop the server
	// without sending an OS signal.
	ctx, cancel := context.WithCancel(context.Background())
	cmd.SetContext(ctx)

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve returned error: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Errorf("serve did not shut down after cancel")
		}
	}()

	raw := waitForFile(t, readyFile)

	var ready struct {
		Addr string `json:"addr"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(raw), &ready); err != nil {
		t.Fatalf("ready file is not valid JSON %q: %v", raw, err)
	}
	host, portStr, err := net.SplitHostPort(ready.Addr)
	if err != nil {
		t.Fatalf("ready addr %q not host:port: %v", ready.Addr, err)
	}
	if host != "127.0.0.1" {
		t.Errorf("bound host = %q, want 127.0.0.1", host)
	}
	if portStr == "0" || portStr == "" {
		t.Errorf("bound port = %q, want a real OS-assigned port", portStr)
	}

	base := "http://" + ready.Addr

	// /api/hello answers 200 on the real bound port.
	helloCtx, helloCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer helloCancel()
	req, _ := http.NewRequestWithContext(helloCtx, http.MethodGet, base+"/api/hello", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/hello: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/api/hello status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// project.list (over WS) must return the backend's host-tagged entry, proving
	// the call was proxied to the configured autowatch backend.
	entries := pollProjectList(t, base, hostID)
	if len(entries) != 1 {
		t.Fatalf("project.list returned %d entries, want 1 (the backend entry): %v", len(entries), entries)
	}
	if entries[0]["host"] != hostID {
		t.Errorf("project.list host = %v, want %q (proves proxy path + backend identity)", entries[0]["host"], hostID)
	}
}

// TestServeFailsFastWithoutBackend covers AC-2 / GR-F6: with no backends.json
// (empty HOME), serve must fail fast with a non-zero ExitError and a remediation
// hint on Stderr, and must NOT bind a listener (no ready file written).
func TestServeFailsFastWithoutBackend(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // empty: no backends.json exists
	t.Setenv("AUTO_UI_PORT", "")
	t.Setenv("AUTO_PROJECTS_PATH", "")
	t.Setenv("AUTO_UI_DEBUG", "")

	readyFile := filepath.Join(t.TempDir(), "ready.json")

	application := app.New(&bytes.Buffer{}, &bytes.Buffer{})
	stderr := application.Stderr.(*bytes.Buffer)

	cmd := newServeCmd(application)
	// Suppress cobra's own error/usage print so the only Stderr writer is serve.
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--port", "0", "--ready-file", readyFile})

	// Bounded context so a regression that binds/serves can't hang the test.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd.SetContext(ctx)

	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()

	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not fail fast (hung) without a backend configured")
	}

	if err == nil {
		t.Fatal("serve returned nil, want a non-zero ExitError")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("serve error is %T, want *ExitError", err)
	}
	if exitErr.Code == 0 {
		t.Errorf("ExitError.Code = 0, want non-zero (fail-fast)")
	}
	if !strings.Contains(stderr.String(), "auto ui backends add") {
		t.Errorf("stderr = %q, want remediation hint mentioning 'auto ui backends add'", stderr.String())
	}
	if _, statErr := os.Stat(readyFile); statErr == nil {
		t.Errorf("ready file was written; serve must not bind a listener without a backend")
	}
}
