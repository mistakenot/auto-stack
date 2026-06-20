package rpcmethods

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-shared/rpc/conformance"
)

// collectSink implements bus.Sink and collects delivered events for assertions.
type collectSink struct {
	mu     sync.Mutex
	events []bus.Event
}

func (s *collectSink) Deliver(ev bus.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *collectSink) snapshot() []bus.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]bus.Event, len(s.events))
	copy(out, s.events)
	return out
}

// setup creates a net.Pipe-based RPC environment with Handlers on the server
// side and a conformance.PeerClient on the client side. Returns the client,
// the Handlers, and a cleanup func.
func setup(t *testing.T, ctlEvents bool) (*conformance.PeerClient, *Handlers, func()) {
	t.Helper()

	hub := bus.NewHub()
	h := New("test-host", "1.2.3", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, ctlEvents)

	sConn, cConn := net.Pipe()
	serverPeer := rpc.NewPeer(sConn)
	h.Register(serverPeer)

	client := conformance.NewPeerClient(cConn)

	ctx, cancel := context.WithCancel(context.Background())
	sErr := make(chan error, 1)
	cErr := make(chan error, 1)
	go func() { sErr <- serverPeer.Serve(ctx) }()
	go func() { cErr <- client.Peer().Serve(ctx) }()

	cleanup := func() {
		cancel()
		<-sErr
		<-cErr
	}
	return client, h, cleanup
}

func TestDaemonStatus_ReturnsExpectedFields(t *testing.T) {
	client, _, cleanup := setup(t, false)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	raw, err := client.Call(ctx, "daemon.status", nil)
	if err != nil {
		t.Fatalf("Call daemon.status: %v", err)
	}

	var result StatusResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("Unmarshal StatusResult: %v", err)
	}

	if result.HostID != "test-host" {
		t.Errorf("hostId = %q, want %q", result.HostID, "test-host")
	}
	if result.Version != "1.2.3" {
		t.Errorf("version = %q, want %q", result.Version, "1.2.3")
	}
	if result.PID <= 0 {
		t.Errorf("pid = %d, want > 0", result.PID)
	}
	if result.UptimeSeconds < 0 {
		t.Errorf("uptimeSeconds = %d, want >= 0", result.UptimeSeconds)
	}
	if result.StartedAt != "2025-01-01T00:00:00Z" {
		t.Errorf("startedAt = %q, want %q", result.StartedAt, "2025-01-01T00:00:00Z")
	}
}

func TestDaemonStatus_CtlEventsTrue_EmitsEvent(t *testing.T) {
	hub := bus.NewHub()
	sink := &collectSink{}
	unsub := hub.Subscribe(sink)
	defer unsub()

	h := New("test-host", "1.2.3", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, true)

	sConn, cConn := net.Pipe()
	serverPeer := rpc.NewPeer(sConn)
	h.Register(serverPeer)

	client := conformance.NewPeerClient(cConn)

	ctx, ctxCancel := context.WithCancel(context.Background())
	sErr := make(chan error, 1)
	cErr := make(chan error, 1)
	go func() { sErr <- serverPeer.Serve(ctx) }()
	go func() { cErr <- client.Peer().Serve(ctx) }()

	callCtx, callCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer callCancel()

	_, err := client.Call(callCtx, "daemon.status", nil)
	if err != nil {
		t.Fatalf("Call daemon.status: %v", err)
	}

	// Give the broadcast a moment to propagate (synchronous delivery,
	// but the handler runs in a goroutine).
	time.Sleep(50 * time.Millisecond)

	events := sink.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	ev := events[0]
	if ev.Type != bus.TypeCtlLogInfo {
		t.Errorf("event type = %q, want %q", ev.Type, bus.TypeCtlLogInfo)
	}

	var data bus.CtlLogEvent
	if err := json.Unmarshal(ev.Data, &data); err != nil {
		t.Fatalf("unmarshal ctl log data: %v", err)
	}
	if data.Op != "rpc.served" {
		t.Errorf("op = %q, want %q", data.Op, "rpc.served")
	}
	if data.Fields["method"] != "daemon.status" {
		t.Errorf("fields.method = %q, want %q", data.Fields["method"], "daemon.status")
	}

	ctxCancel()
	<-sErr
	<-cErr
}

func TestDaemonStatus_CtlEventsFalse_NoEvents(t *testing.T) {
	hub := bus.NewHub()
	sink := &collectSink{}
	unsub := hub.Subscribe(sink)
	defer unsub()

	h := New("test-host", "1.2.3", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false)

	sConn, cConn := net.Pipe()
	serverPeer := rpc.NewPeer(sConn)
	h.Register(serverPeer)

	client := conformance.NewPeerClient(cConn)

	ctx, ctxCancel := context.WithCancel(context.Background())
	sErr := make(chan error, 1)
	cErr := make(chan error, 1)
	go func() { sErr <- serverPeer.Serve(ctx) }()
	go func() { cErr <- client.Peer().Serve(ctx) }()

	callCtx, callCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer callCancel()

	_, err := client.Call(callCtx, "daemon.status", nil)
	if err != nil {
		t.Fatalf("Call daemon.status: %v", err)
	}

	// Allow time for any erroneous event to arrive.
	time.Sleep(50 * time.Millisecond)

	events := sink.snapshot()
	if len(events) != 0 {
		t.Errorf("expected 0 events with ctlEvents=false, got %d", len(events))
	}

	ctxCancel()
	<-sErr
	<-cErr
}

func TestDaemonStatus_ConcurrentCalls(t *testing.T) {
	client, h, cleanup := setup(t, false)
	defer cleanup()

	const n = 2
	var wg sync.WaitGroup
	errs := make(chan error, n)

	for range n {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := client.Call(ctx, "daemon.status", nil)
			if err != nil {
				errs <- err
			}
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent call failed: %v", err)
	}

	if got := h.DispatchCount("daemon.status"); got != n {
		t.Errorf("DispatchCount = %d, want %d", got, n)
	}
}

func TestDispatchCount_Increments(t *testing.T) {
	client, h, cleanup := setup(t, false)
	defer cleanup()

	if got := h.DispatchCount("daemon.status"); got != 0 {
		t.Fatalf("initial DispatchCount = %d, want 0", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 1; i <= 3; i++ {
		if _, err := client.Call(ctx, "daemon.status", nil); err != nil {
			t.Fatalf("Call %d: %v", i, err)
		}
		if got := h.DispatchCount("daemon.status"); got != i {
			t.Errorf("after %d calls, DispatchCount = %d, want %d", i, got, i)
		}
	}
}

func TestDispatchCount_UnknownMethod(t *testing.T) {
	h := New("test", "0.0.0", time.Now(), bus.NewHub(), false)
	if got := h.DispatchCount("nonexistent.method"); got != 0 {
		t.Errorf("DispatchCount for unknown method = %d, want 0", got)
	}
}
