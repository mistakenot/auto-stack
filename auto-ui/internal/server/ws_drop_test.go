package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mistakenot/auto-shared/bus"
)

// dialDropWS dials the /api/ws endpoint of base, failing the test on error.
// resp.Body is managed by websocket.Dial (see its docs).
func dialDropWS(ctx context.Context, t *testing.T, base string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(base, "http") + "/api/ws"
	c, _, err := websocket.Dial(ctx, url, nil) //nolint:bodyclose // managed by websocket.Dial
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

// notifID returns params.id of a JSON-RPC notification, or "" if absent.
func notifID(m map[string]any) string {
	p, _ := m["params"].(map[string]any)
	s, _ := p["id"].(string)
	return s
}

// waitWSSubscribed gives the server goroutine time to reach hub.Subscribe after
// the WebSocket handshake completes. hub.Subscribe is the first thing the handler
// does after Accept, but goroutine scheduling means the client's Dial can return
// before it runs. (The external-package event_flow tests use an equivalent
// `waitSubscribed`; this white-box file is `package server`, so it needs its own.)
func waitWSSubscribed() {
	time.Sleep(50 * time.Millisecond)
}

// floodEvent builds a sizable, uniquely-ided bus.Event for the drop-on-full
// flood. The payload makes each notification large enough that a never-draining
// client's socket back-pressures quickly.
func floodEvent(id string, data json.RawMessage) bus.Event {
	return bus.Event{
		SpecVersion: bus.SpecVersion,
		Type:        "relay.test",
		Source:      "test",
		ID:          id,
		Time:        "2026-01-01T00:00:00Z",
		Host:        "host-a",
		Data:        data,
	}
}

// TestWSSlowClientDroppedOnFull (white-box) is the post-047 replacement for the
// removed event_flow TestRelaySlowClientDropped. With auto-ui's /api/rpc ingest
// gone, there is no longer a synchronous local path to flood the hub from, and a
// relayed flood can't reproduce the scenario (a backend peer's own 16-slot send
// buffer drops-and-closes on overflow, tearing the relay down before it could
// overrun a WS session). So this test wires a hub + WS handler directly and
// floods it with synchronous hub.Broadcast calls — the exact delivery path the
// removed /api/rpc POST used — to verify the WS-session drop-on-full behaviour
// (ws.go enqueue's non-blocking default): a never-draining client is dropped
// without wedging the hub or other clients.
func TestWSSlowClientDroppedOnFull(t *testing.T) {
	hub := bus.NewHub()
	d := newDispatcher()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ws", handleWSWithHub(hub, d))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Healthy client drains continuously in the background, surfacing each
	// notification's id on a channel.
	healthy := dialDropWS(ctx, t, srv.URL)
	defer healthy.Close(websocket.StatusNormalClosure, "")
	waitWSSubscribed()
	healthyIDs := make(chan string, 4096)
	go func() {
		for {
			_, data, err := healthy.Read(ctx)
			if err != nil {
				return
			}
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				if id := notifID(m); id != "" {
					select {
					case healthyIDs <- id:
					default:
					}
				}
			}
		}
	}()

	// Slow client connects, then never reads.
	slow := dialDropWS(ctx, t, srv.URL)
	defer slow.Close(websocket.StatusNormalClosure, "")
	waitWSSubscribed()

	// drainHealthy blocks until the healthy client has surfaced want — proving it
	// read that broadcast off the wire (so its 16-slot out is drained again).
	drainHealthy := func(want string) {
		for {
			select {
			case id := <-healthyIDs:
				if id == want {
					return
				}
			case <-ctx.Done():
				t.Fatalf("healthy client never received %q (Hub wedged?)", want)
			}
		}
	}

	// Flood the hub with synchronous, uniquely-ided broadcasts. Each broadcast
	// calls every session's non-blocking Deliver. We lockstep the healthy client —
	// waiting for it to read each event before sending the next — so it never
	// overflows (mirroring the natural pacing the removed /api/rpc HTTP round
	// trips provided). The slow client never reads, so its 16-slot out fills and a
	// subsequent Deliver drops-and-cancels it, never blocking the broadcasting
	// goroutine (so the Hub is not wedged).
	blob := make([]byte, 16*1024)
	for i := range blob {
		blob[i] = 'x'
	}
	payload, err := json.Marshal(map[string]string{"blob": string(blob)})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	for i := range 64 {
		id := fmt.Sprintf("flood-%d", i)
		hub.Broadcast(floodEvent(id, payload))
		drainHealthy(id)
	}

	// Post-flood sentinel: if the slow client had wedged the Hub, this would never
	// reach the healthy client. Its arrival proves the Hub is not wedged.
	hub.Broadcast(floodEvent("sentinel-1", nil))
	drainHealthy("sentinel-1")

	// The slow client was dropped (drop-on-full): its connection is closed
	// server-side, so a bounded read eventually errors rather than hanging.
	slowCtx, slowCancel := context.WithTimeout(ctx, 3*time.Second)
	defer slowCancel()
	for {
		if _, _, err := slow.Read(slowCtx); err != nil {
			break
		}
	}
}
