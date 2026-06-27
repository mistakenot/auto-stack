package server_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mistakenot/auto-ui/internal/server"
)

// wsURL converts an httptest http(s):// base URL to a ws(s):// /api/ws URL.
func wsURL(base string) string {
	return "ws" + strings.TrimPrefix(base, "http") + "/api/ws"
}

// dialWS dials the test server's WebSocket endpoint, failing the test on error.
// The handshake response needs no explicit Body close: coder/websocket
// documents "You never need to close resp.Body yourself" (Dial manages it).
func dialWS(ctx context.Context, t *testing.T, base string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(ctx, wsURL(base), nil) //nolint:bodyclose // resp.Body is managed by websocket.Dial (see its docs)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

// readUntil reads messages until pred returns true for one, or the context
// deadline fires. Tests filter the stream rather than assuming message order.
func readUntil(ctx context.Context, t *testing.T, c *websocket.Conn, pred func(map[string]any) bool) map[string]any {
	t.Helper()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode %q: %v", data, err)
		}
		if pred(msg) {
			return msg
		}
	}
}

// TestWSRequestResponse asserts a client RPC call gets a correlated response.
func TestWSRequestResponse(t *testing.T) {
	srv := httptest.NewServer(server.New(newTestFS(), "test"))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	// Send an unknown method — should get a method-not-found error response with the same id.
	req := []byte(`{"jsonrpc":"2.0","id":99,"method":"nonexistent","params":{}}`)
	if err := c.Write(ctx, websocket.MessageText, req); err != nil {
		t.Fatalf("write: %v", err)
	}

	resp := readUntil(ctx, t, c, func(m map[string]any) bool {
		id, ok := m["id"].(float64)
		return ok && id == 99
	})
	if resp["error"] == nil {
		t.Fatalf("expected error response for unknown method, got: %v", resp)
	}
}
