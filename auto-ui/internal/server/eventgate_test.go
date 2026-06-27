package server

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mistakenot/auto-shared/bus"
)

// recordingSink is a bus.Sink that records every delivered event. Safe for
// concurrent delivery.
type recordingSink struct {
	mu     sync.Mutex
	events []bus.Event
}

func (s *recordingSink) Deliver(ev bus.Event) {
	s.mu.Lock()
	s.events = append(s.events, ev)
	s.mu.Unlock()
}

func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// newGateWithSink wires a real bus.Hub, a recording sink, and an eventGate.
func newGateWithSink(ttl time.Duration) (*eventGate, *recordingSink) {
	hub := bus.NewHub()
	sink := &recordingSink{}
	hub.Subscribe(sink)
	return newEventGate(hub, ttl), sink
}

func TestEventGateDedupWithinTTL(t *testing.T) {
	g, sink := newGateWithSink(time.Minute)

	ev := bus.Event{ID: "abc123"}
	g.Broadcast(ev)
	g.Broadcast(ev)

	if got := sink.count(); got != 1 {
		t.Fatalf("hub saw %d broadcasts, want 1 (duplicate id within ttl must be dropped)", got)
	}
}

func TestEventGateDistinctIDsPass(t *testing.T) {
	g, sink := newGateWithSink(time.Minute)

	g.Broadcast(bus.Event{ID: "a"})
	g.Broadcast(bus.Event{ID: "b"})

	if got := sink.count(); got != 2 {
		t.Fatalf("hub saw %d broadcasts, want 2 (distinct ids must both pass)", got)
	}
}

func TestEventGateExpiredNotSuppressed(t *testing.T) {
	ttl := 10 * time.Millisecond
	g, sink := newGateWithSink(ttl)

	ev := bus.Event{ID: "abc"}
	g.Broadcast(ev)
	// Bounded wait past the ttl, then re-broadcast: the id is now expired and
	// must not be suppressed.
	time.Sleep(ttl + 15*time.Millisecond)
	g.Broadcast(ev)

	if got := sink.count(); got != 2 {
		t.Fatalf("hub saw %d broadcasts, want 2 (id past ttl must not be suppressed)", got)
	}
}

func TestEventGateConcurrentBroadcastRaceClean(t *testing.T) {
	g, sink := newGateWithSink(time.Minute)

	const goroutines = 50
	const distinctIDs = 10
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g.Broadcast(bus.Event{ID: fmt.Sprintf("id-%d", i%distinctIDs)})
		}(i)
	}
	wg.Wait()

	// Each of the distinctIDs ids is deduped, so the hub sees between 1 and
	// distinctIDs broadcasts; the point of -race is to catch unsynchronized access.
	if got := sink.count(); got < 1 || got > distinctIDs {
		t.Fatalf("hub saw %d broadcasts, want between 1 and %d", got, distinctIDs)
	}
}
