package rpcserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/bus"
	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-shared/rpc/conformance"
	"github.com/mistakenot/auto-shared/transport"
	"github.com/mistakenot/auto-watch/internal/rpcmethods"
)

var emptyReg = func() config.ProjectsConfig {
	return config.ProjectsConfig{Projects: []config.ProjectRef{}}
}

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

func dialAndCall(t *testing.T, ctx context.Context, uri string) json.RawMessage {
	t.Helper()
	conn, err := transport.Dial(ctx, uri)
	if err != nil {
		t.Fatalf("transport.Dial(%s): %v", uri, err)
	}
	client := conformance.NewPeerClient(conn)
	go client.Peer().Serve(ctx)

	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	defer callCancel()
	raw, err := client.Call(callCtx, "daemon.status", nil)
	if err != nil {
		t.Fatalf("Call daemon.status: %v", err)
	}
	return raw
}

func TestServe_TCP_DaemonStatus(t *testing.T) {
	hub := bus.NewHub()
	h := rpcmethods.New("test-host", "0.1.0", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	// Give accept loop a moment to start.
	time.Sleep(20 * time.Millisecond)

	addr := "tcp://" + ln.Addr().String()
	raw := dialAndCall(t, ctx, addr)

	var result rpcmethods.StatusResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if result.HostID != "test-host" {
		t.Errorf("hostId = %q, want %q", result.HostID, "test-host")
	}
	if result.Version != "0.1.0" {
		t.Errorf("version = %q, want %q", result.Version, "0.1.0")
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Errorf("Serve returned error: %v", err)
	}
}

func TestServe_Unix_DaemonStatus(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	hub := bus.NewHub()
	h := rpcmethods.New("unix-host", "2.0.0", time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("unix://" + sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	time.Sleep(20 * time.Millisecond)

	raw := dialAndCall(t, ctx, "unix://"+sockPath)

	var result rpcmethods.StatusResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if result.HostID != "unix-host" {
		t.Errorf("hostId = %q, want %q", result.HostID, "unix-host")
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Errorf("Serve returned error: %v", err)
	}
}

func TestServe_ConcurrentConnections(t *testing.T) {
	hub := bus.NewHub()
	h := rpcmethods.New("test-host", "0.1.0", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	time.Sleep(20 * time.Millisecond)

	addr := "tcp://" + ln.Addr().String()

	var wg sync.WaitGroup
	results := make(chan string, 2)

	for range 2 {
		wg.Go(func() {
			conn, err := transport.Dial(ctx, addr)
			if err != nil {
				t.Errorf("Dial: %v", err)
				return
			}
			client := conformance.NewPeerClient(conn)
			go client.Peer().Serve(ctx)

			callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
			defer callCancel()
			raw, err := client.Call(callCtx, "daemon.status", nil)
			if err != nil {
				t.Errorf("Call: %v", err)
				return
			}
			var result rpcmethods.StatusResult
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Errorf("Unmarshal: %v", err)
				return
			}
			results <- result.HostID
		})
	}

	wg.Wait()
	close(results)

	count := 0
	for id := range results {
		count++
		if id != "test-host" {
			t.Errorf("hostId = %q, want %q", id, "test-host")
		}
	}
	if count != 2 {
		t.Errorf("got %d results, want 2", count)
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Errorf("Serve returned error: %v", err)
	}
}

func TestServe_Shutdown_ClosesListener(t *testing.T) {
	hub := bus.NewHub()
	h := rpcmethods.New("test-host", "0.1.0", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, false)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	time.Sleep(20 * time.Millisecond)

	// Connect a client.
	addr := "tcp://" + ln.Addr().String()
	conn, err := transport.Dial(ctx, addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	client := conformance.NewPeerClient(conn)
	clientErr := make(chan error, 1)
	go func() { clientErr <- client.Peer().Serve(ctx) }()

	// Verify the connection works.
	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	_, err = client.Call(callCtx, "daemon.status", nil)
	callCancel()
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	// Cancel context — should trigger clean shutdown.
	cancel()

	// Serve should return nil (clean shutdown).
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Serve returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return within timeout")
	}
}

func TestServe_CtlEvents_ConnectDisconnect(t *testing.T) {
	hub := bus.NewHub()
	sink := &collectSink{}
	unsub := hub.Subscribe(sink)
	defer unsub()

	h := rpcmethods.New("test-host", "0.1.0", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, true) // ctlEvents=true
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	time.Sleep(20 * time.Millisecond)

	// Dial and make a call.
	addr := "tcp://" + ln.Addr().String()
	clientCtx, clientCancel := context.WithCancel(ctx)
	conn, err := transport.Dial(clientCtx, addr)
	if err != nil {
		cancel()
		t.Fatalf("Dial: %v", err)
	}
	client := conformance.NewPeerClient(conn)
	clientDone := make(chan error, 1)
	go func() { clientDone <- client.Peer().Serve(clientCtx) }()

	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	_, err = client.Call(callCtx, "daemon.status", nil)
	callCancel()
	if err != nil {
		cancel()
		t.Fatalf("Call: %v", err)
	}

	// Wait for connect event to propagate.
	time.Sleep(50 * time.Millisecond)

	// Check for ctl.connect event.
	events := sink.snapshot()
	var hasConnect bool
	for _, ev := range events {
		if ev.Type == bus.TypeCtlConnect {
			hasConnect = true
		}
	}
	if !hasConnect {
		t.Error("expected ctl.connect event, got none")
	}

	// Close the client to trigger disconnect.
	clientCancel()
	<-clientDone

	// Wait for disconnect event.
	time.Sleep(100 * time.Millisecond)

	events = sink.snapshot()
	var hasDisconnect bool
	for _, ev := range events {
		if ev.Type == bus.TypeCtlDisconnect {
			hasDisconnect = true
		}
	}
	if !hasDisconnect {
		t.Error("expected ctl.disconnect event, got none")
	}

	cancel()
	<-errCh
}

func TestServe_CtlEventsFalse_NoLifecycleEvents(t *testing.T) {
	hub := bus.NewHub()
	sink := &collectSink{}
	unsub := hub.Subscribe(sink)
	defer unsub()

	h := rpcmethods.New("test-host", "0.1.0", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), hub, false, emptyReg)
	ln, err := transport.Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	srv := New(ln, h, hub, false) // ctlEvents=false
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()

	time.Sleep(20 * time.Millisecond)

	// Dial, call, close.
	addr := "tcp://" + ln.Addr().String()
	clientCtx, clientCancel := context.WithCancel(ctx)
	conn, err := transport.Dial(clientCtx, addr)
	if err != nil {
		cancel()
		t.Fatalf("Dial: %v", err)
	}
	client := conformance.NewPeerClient(conn)
	clientDone := make(chan error, 1)
	go func() { clientDone <- client.Peer().Serve(clientCtx) }()

	callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
	_, err = client.Call(callCtx, "daemon.status", nil)
	callCancel()
	if err != nil {
		cancel()
		t.Fatalf("Call: %v", err)
	}

	clientCancel()
	<-clientDone

	time.Sleep(100 * time.Millisecond)

	events := sink.snapshot()
	for _, ev := range events {
		if ev.Type == bus.TypeCtlConnect || ev.Type == bus.TypeCtlDisconnect {
			t.Errorf("unexpected lifecycle event %q with ctlEvents=false", ev.Type)
		}
	}

	cancel()
	<-errCh
}
