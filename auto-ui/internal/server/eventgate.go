package server

import (
	"sync"
	"time"

	"github.com/mistakenot/auto-shared/bus"
)

// maxSeenIDs caps the recently-seen id set so a long-running server cannot grow
// the dedup map without bound. When an insert would push the set to or past the
// cap, prune drops expired ids first; if the set is still at the cap it is
// cleared wholesale (a deliberately simple bound — the worst case re-admits a
// few ids that would otherwise have been suppressed, never a correctness issue).
const maxSeenIDs = 4096

// eventGate fronts a bus.Hub with id-based dedup over a sliding TTL window.
// Relayed (from backends) and locally-ingested copies of the same event share a
// deterministic id, so the gate forwards each id to the hub at most once per
// ttl. It is safe for concurrent use.
type eventGate struct {
	hub *bus.Hub
	ttl time.Duration

	mu   sync.Mutex
	seen map[string]time.Time
}

// newEventGate returns an eventGate fronting hub, deduping by event id over ttl.
func newEventGate(hub *bus.Hub, ttl time.Duration) *eventGate {
	return &eventGate{
		hub:  hub,
		ttl:  ttl,
		seen: make(map[string]time.Time),
	}
}

// Broadcast forwards ev to the hub unless an event with the same id was seen
// within ttl, in which case it is dropped. Expired ids are pruned and the set is
// capped on insert to bound memory. hub.Broadcast is called outside the lock so
// a slow sink cannot block other Broadcast callers from recording ids.
func (g *eventGate) Broadcast(ev bus.Event) {
	now := time.Now()

	g.mu.Lock()
	if seenAt, ok := g.seen[ev.ID]; ok && now.Sub(seenAt) < g.ttl {
		g.mu.Unlock()
		return
	}
	g.prune(now)
	g.seen[ev.ID] = now
	g.mu.Unlock()

	g.hub.Broadcast(ev)
}

// prune deletes expired ids and, if the set is still at the cap, clears it
// entirely. Callers must hold g.mu.
func (g *eventGate) prune(now time.Time) {
	for id, seenAt := range g.seen {
		if now.Sub(seenAt) >= g.ttl {
			delete(g.seen, id)
		}
	}
	if len(g.seen) >= maxSeenIDs {
		g.seen = make(map[string]time.Time)
	}
}
