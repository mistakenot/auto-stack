package bus

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// collectSink collects delivered events in a slice (thread-safe).
type collectSink struct {
	mu     sync.Mutex
	events []Event
}

func (s *collectSink) Deliver(ev Event) {
	s.mu.Lock()
	s.events = append(s.events, ev)
	s.mu.Unlock()
}

func (s *collectSink) got() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]Event, len(s.events))
	copy(cp, s.events)
	return cp
}

func TestHubFanOut(t *testing.T) {
	hub := NewHub()

	const n = 5
	sinks := make([]*collectSink, n)
	for i := range sinks {
		sinks[i] = &collectSink{}
		hub.Subscribe(sinks[i])
	}

	ev, _ := NewEvent("agent.tool.post", "test", nil)
	hub.Broadcast(ev)

	for i, s := range sinks {
		got := s.got()
		if len(got) != 1 {
			t.Errorf("sink %d: got %d events, want 1", i, len(got))
		}
		if len(got) > 0 && got[0].Type != "agent.tool.post" {
			t.Errorf("sink %d: type = %q, want agent.tool.post", i, got[0].Type)
		}
	}
}

func TestHubDeregister(t *testing.T) {
	hub := NewHub()

	s1 := &collectSink{}
	s2 := &collectSink{}
	cancel1 := hub.Subscribe(s1)
	hub.Subscribe(s2)

	// Deregister s1 before broadcast.
	cancel1()

	ev, _ := NewEvent("doc.changed", "test", nil)
	hub.Broadcast(ev)

	if len(s1.got()) != 0 {
		t.Errorf("deregistered sink received %d events, want 0", len(s1.got()))
	}
	if len(s2.got()) != 1 {
		t.Errorf("active sink received %d events, want 1", len(s2.got()))
	}
}

func TestHubDeregisterIdempotent(t *testing.T) {
	hub := NewHub()
	s := &collectSink{}
	cancel := hub.Subscribe(s)
	cancel()
	cancel() // second call should not panic
}

// slowSink simulates a sink that blocks for a while in Deliver.
type slowSink struct {
	delivered atomic.Int32
	delay     time.Duration
}

func (s *slowSink) Deliver(_ Event) {
	time.Sleep(s.delay)
	s.delivered.Add(1)
}

func TestHubSlowSinkNeverBlocks(t *testing.T) {
	hub := NewHub()

	slow := &slowSink{delay: 100 * time.Millisecond}
	fast := &collectSink{}
	hub.Subscribe(slow)
	hub.Subscribe(fast)

	// Broadcast should complete (fast sink is delivered synchronously).
	// The contract is that the hub calls Deliver synchronously per sink but
	// the auto-ui session Deliver uses a non-blocking send. For the hub
	// itself, we verify it completes even with a slow sink.
	done := make(chan struct{})
	go func() {
		ev, _ := NewEvent("agent.tool.post", "test", nil)
		hub.Broadcast(ev)
		close(done)
	}()

	select {
	case <-done:
		// Broadcast completed — verify fast sink got the event.
		if len(fast.got()) != 1 {
			t.Errorf("fast sink got %d events, want 1", len(fast.got()))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("broadcast blocked for >2s — slow sink is blocking the hub")
	}
}

func TestHubBroadcastToEmptyHub(t *testing.T) {
	hub := NewHub()
	ev, _ := NewEvent("agent.tool.post", "test", nil)
	// Should not panic.
	hub.Broadcast(ev)
}

func TestHubConcurrentSubscribeAndBroadcast(t *testing.T) {
	hub := NewHub()
	var wg sync.WaitGroup

	// Concurrent subscribers.
	for range 10 {
		wg.Go(func() {
			s := &collectSink{}
			cancel := hub.Subscribe(s)
			time.Sleep(time.Millisecond)
			cancel()
		})
	}

	// Concurrent broadcasts.
	for range 10 {
		wg.Go(func() {
			ev, _ := NewEvent("agent.tool.post", "test", nil)
			hub.Broadcast(ev)
		})
	}

	wg.Wait()
}
