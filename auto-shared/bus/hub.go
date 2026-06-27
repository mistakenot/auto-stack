package bus

import "sync"

// Sink is the interface a subscriber implements to receive broadcast events.
type Sink interface {
	Deliver(Event)
}

// sinkEntry is a registered subscriber in the hub.
type sinkEntry struct {
	sink Sink
}

// Hub broadcasts events to all registered sinks. It is safe for concurrent use.
type Hub struct {
	mu    sync.RWMutex
	sinks map[*sinkEntry]struct{}
}

// NewHub creates a new Hub ready for subscriptions.
func NewHub() *Hub {
	return &Hub{
		sinks: make(map[*sinkEntry]struct{}),
	}
}

// Subscribe registers a sink to receive broadcast events and returns a cancel
// function that deregisters it. The cancel function is safe to call multiple times.
func (h *Hub) Subscribe(s Sink) (cancel func()) {
	entry := &sinkEntry{sink: s}
	h.mu.Lock()
	h.sinks[entry] = struct{}{}
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		delete(h.sinks, entry)
		h.mu.Unlock()
	}
}

// SinkCount returns the number of currently registered sinks. Intended for
// tests and diagnostics.
func (h *Hub) SinkCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.sinks)
}

// Broadcast sends the event to every registered sink. It snapshots the sink set
// under RLock and calls Deliver outside the lock, so a slow sink cannot block
// registration/deregistration. Deliver is called synchronously per sink; if a
// sink's Deliver blocks, the broadcast to that sink blocks but other sinks
// proceed in order. The auto-ui session's Deliver uses a non-blocking channel
// send with drop-on-full, so the hub itself never blocks on a slow client.
func (h *Hub) Broadcast(ev Event) {
	h.mu.RLock()
	snapshot := make([]*sinkEntry, 0, len(h.sinks))
	for entry := range h.sinks {
		snapshot = append(snapshot, entry)
	}
	h.mu.RUnlock()

	for _, entry := range snapshot {
		entry.sink.Deliver(ev)
	}
}
