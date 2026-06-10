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
// deadline fires. Server-push pings and RPC responses share the connection, so
// tests filter the stream rather than assuming message order.
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

// TestWSServerPush asserts the server pushes a `ping` notification (id-less)
// within ~1.2s of connecting — the server->client push primitive (#3).
func TestWSServerPush(t *testing.T) {
	srv := httptest.NewServer(server.New(newTestFS(), "test"))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	msg := readUntil(ctx, t, c, func(m map[string]any) bool {
		// A notification has a method and no id.
		return m["method"] == "ping" && m["id"] == nil
	})
	params, ok := msg["params"].(map[string]any)
	if !ok || params["seq"] == nil {
		t.Fatalf("ping notification missing params.seq: %v", msg)
	}
}

// TestWSRequestResponse asserts a client `ping` RPC gets a correlated `pong`
// response carrying the same id and the echoed seq (primitives #1 + #2).
func TestWSRequestResponse(t *testing.T) {
	srv := httptest.NewServer(server.New(newTestFS(), "test"))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c := dialWS(ctx, t, srv.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	req := []byte(`{"jsonrpc":"2.0","id":99,"method":"ping","params":{"seq":7}}`)
	if err := c.Write(ctx, websocket.MessageText, req); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Skip any interleaved push notifications; wait for our id=99 response.
	resp := readUntil(ctx, t, c, func(m map[string]any) bool {
		id, ok := m["id"].(float64)
		return ok && id == 99
	})
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("response missing result: %v", resp)
	}
	if result["pong"] != true {
		t.Errorf("pong = %v, want true", result["pong"])
	}
	if seq, _ := result["seq"].(float64); seq != 7 {
		t.Errorf("echoed seq = %v, want 7", result["seq"])
	}
}
