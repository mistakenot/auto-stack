package server_test

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestProjectListProxiesBackend covers AC-5: project.list returns
// backend-sourced entries, each carrying a host field (GR-F8).
func TestProjectListProxiesBackend(t *testing.T) {
	srv := newProxyServer(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	resp := rpcCall(ctx, t, c, 1, "project.list", map[string]string{})
	if resp["error"] != nil {
		t.Fatalf("project.list error: %v", resp["error"])
	}

	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("project.list result not an array: %T %v", resp["result"], resp["result"])
	}
	if len(result) != 1 {
		t.Fatalf("project.list returned %d entries, want 1: %v", len(result), result)
	}

	entry, ok := result[0].(map[string]any)
	if !ok {
		t.Fatalf("entry not a map: %v", result[0])
	}
	if entry["id"] != "alpha" {
		t.Errorf("id = %v, want alpha", entry["id"])
	}
	if entry["name"] != "Alpha" {
		t.Errorf("name = %v, want Alpha", entry["name"])
	}
	if entry["host"] != proxyHostID {
		t.Errorf("host = %v, want %q (each entry must carry host per GR-F8)", entry["host"], proxyHostID)
	}
}
