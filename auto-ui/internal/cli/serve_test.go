package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mistakenot/auto-ui/internal/app"
)

// writeProjectsFixture writes a projects.json registry to a temp file and
// returns its path. The single project lets the test prove --projects was the
// source of project.list (HOME is pointed at an empty dir, so the default
// registry resolves to nothing).
func writeProjectsFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	const body = `{"projects":[{"id":"fixture-proj","name":"Fixture","path":"/tmp/fixture","remote":"https://github.com/owner/fixture"}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
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

// TestServeReadyFileAndIsolation covers AC-5 (serve side): --port 0 binds an
// OS-assigned port, --ready-file records the real bound 127.0.0.1:NNNN address
// as JSON, /api/hello answers on that port, and --projects isolates the
// registry from ~/.auto (HOME is redirected to an empty dir, yet project.list
// returns the fixture entry).
func TestServeReadyFileAndIsolation(t *testing.T) {
	// Isolate from the host: an empty HOME means the default projects.json does
	// not exist, so any project.list result must come from --projects.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AUTO_UI_PORT", "")
	t.Setenv("AUTO_PROJECTS_PATH", "")
	t.Setenv("AUTO_UI_DEBUG", "")

	projects := writeProjectsFixture(t)
	readyFile := filepath.Join(t.TempDir(), "ready.json")

	application := app.New(&bytes.Buffer{}, &bytes.Buffer{})
	cmd := newServeCmd(application)
	cmd.SetArgs([]string{"--port", "0", "--ready-file", readyFile, "--projects", projects})

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

	// project.list (over WS) must return the --projects fixture, proving the
	// registry was loaded from --projects and not from the (empty) HOME default.
	wsCtx, wsCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer wsCancel()
	wsURL := "ws" + base[len("http"):] + "/api/ws"
	conn, _, err := websocket.Dial(wsCtx, wsURL, nil) //nolint:bodyclose // resp.Body managed by Dial
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := conn.Write(wsCtx, websocket.MessageText, []byte(`{"jsonrpc":"2.0","id":1,"method":"project.list","params":{}}`)); err != nil {
		t.Fatalf("write project.list: %v", err)
	}

	var entries []map[string]any
	for {
		_, data, err := conn.Read(wsCtx)
		if err != nil {
			t.Fatalf("read project.list: %v", err)
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode ws msg %q: %v", data, err)
		}
		id, ok := msg["id"].(float64)
		if !ok || id != 1 {
			continue // skip server-push pings
		}
		if msg["error"] != nil {
			t.Fatalf("project.list error: %v", msg["error"])
		}
		arr, ok := msg["result"].([]any)
		if !ok {
			t.Fatalf("project.list result not array: %v", msg["result"])
		}
		for _, e := range arr {
			if m, ok := e.(map[string]any); ok {
				entries = append(entries, m)
			}
		}
		break
	}

	if len(entries) != 1 {
		t.Fatalf("project.list returned %d entries, want 1 (the --projects fixture): %v", len(entries), entries)
	}
	if entries[0]["id"] != "fixture-proj" {
		t.Errorf("project.list id = %v, want fixture-proj (proves --projects isolation)", entries[0]["id"])
	}
}
