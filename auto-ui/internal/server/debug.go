package server

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/mistakenot/auto-shared/bus"
)

// debugBufferCap is the maximum number of events retained by the debug buffer.
const debugBufferCap = 100

// debugBuffer is a fixed-capacity ring buffer of recorded bus events, used by
// the gated /api/debug/recent route to expose the last N events (raw + derived)
// that passed through the ingest handler. It is safe for concurrent use.
type debugBuffer struct {
	mu    sync.Mutex
	ring  [debugBufferCap]bus.Event
	count int // total events recorded (used for ordering)
}

// record appends an event to the ring, overwriting the oldest when full.
func (b *debugBuffer) record(ev bus.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ring[b.count%debugBufferCap] = ev
	b.count++
}

// recent returns the recorded events in insertion order (oldest first), at most
// debugBufferCap entries.
func (b *debugBuffer) recent() []bus.Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := min(b.count, debugBufferCap)
	out := make([]bus.Event, 0, n)
	start := b.count - n
	for i := start; i < b.count; i++ {
		out = append(out, b.ring[i%debugBufferCap])
	}
	return out
}

// handleDebugRecent returns a handler that serves the last N recorded events as
// JSON when enabled, and a 404 otherwise. The buffer must be non-nil when
// enabled is true.
func handleDebugRecent(b *debugBuffer, enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !enabled || b == nil {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(b.recent())
	}
}
