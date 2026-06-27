package server_test

import (
	"context"
	"encoding/json"
	"errors"
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

// TestBackendsListReportsHealth (AC-6): backends.list surfaces Manager.Health()
// over WS. A reachable backend is reported connected:true with its learned
// hostId + uri; a configured-but-unreachable backend (modelled by a failing dial
// — the deterministic stand-in for "dropped") is reported connected:false with a
// non-empty lastErr.
func TestBackendsListReportsHealth(t *testing.T) {
	const (
		uriOK   = "unix:///fake/ok.sock"
		uriDown = "unix:///fake/down.sock"
	)
	path := filepath.Join(t.TempDir(), "backends.json")
	if err := uiconfig.SaveBackends(path, uiconfig.BackendsConfig{
		Backends: []uiconfig.Backend{{URI: uriOK}, {URI: uriDown}},
	}); err != nil {
		t.Fatalf("SaveBackends: %v", err)
	}

	dial := func(_ context.Context, u string) (net.Conn, error) {
		if u == uriOK {
			sConn, cConn := net.Pipe()
			startFakeBackend(t, sConn, map[string]rpc.Handler{
				"bus.subscribe": func(_ context.Context, _ json.RawMessage) (any, error) {
					return map[string]string{"status": "subscribed"}, nil
				},
			})
			return cConn, nil
		}
		// uriDown: the dial fails, modelling an unreachable/dropped backend. The
		// Manager records it connected:false with a non-empty lastErr and retries
		// on a later tick.
		return nil, errors.New("connection refused")
	}

	mgr := backend.NewManager(path, dial, 0)
	mgr.Reconcile(context.Background())

	// The reachable backend connects (host id learned); Reconcile already recorded
	// the unreachable one as pending+lastErr before returning. Bounded wait, never
	// poll-to-settle.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := mgr.Resolve(proxyHostID); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reachable backend did not connect in time")
		}
		time.Sleep(5 * time.Millisecond)
	}

	srv := httptest.NewServer(server.New(newTestFS(), "test", server.WithBackendManager(mgr)))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "backends.list", map[string]string{})
	if resp["error"] != nil {
		t.Fatalf("backends.list error: %v", resp["error"])
	}
	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("backends.list result not an array: %T %v", resp["result"], resp["result"])
	}
	if len(result) != 2 {
		t.Fatalf("backends.list returned %d entries, want 2: %v", len(result), result)
	}

	byURI := map[string]map[string]any{}
	for _, e := range result {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("entry not a map: %v", e)
		}
		uri, _ := m["uri"].(string)
		byURI[uri] = m
	}

	okEntry := byURI[uriOK]
	if okEntry == nil {
		t.Fatalf("connected backend %q missing from health: %v", uriOK, result)
	}
	if okEntry["hostId"] != proxyHostID {
		t.Errorf("connected backend hostId = %v, want %q", okEntry["hostId"], proxyHostID)
	}
	if okEntry["connected"] != true {
		t.Errorf("connected backend connected = %v, want true", okEntry["connected"])
	}

	downEntry := byURI[uriDown]
	if downEntry == nil {
		t.Fatalf("dropped backend %q missing from health: %v", uriDown, result)
	}
	if downEntry["connected"] != false {
		t.Errorf("dropped backend connected = %v, want false", downEntry["connected"])
	}
	if le, _ := downEntry["lastErr"].(string); le == "" {
		t.Errorf("dropped backend lastErr empty, want a non-empty dial error")
	}
}
