package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mistakenot/auto-mail/internal/ulid"
)

// The complete T1 event vocabulary. Every type carries the `alpha.` prefix, so
// no consumer can match a bare `mail.sent` and read a compatibility promise into
// an alpha store (G10 / D-2). These four are the whole set; adding a fifth means
// adding it here, because Append refuses anything outside the namespace.
const (
	// TypeSubscribed records a new durable reader of an address.
	TypeSubscribed = "alpha.mail.subscribed"
	// TypeSent records one mail posted to an address.
	TypeSent = "alpha.mail.sent"
	// TypeRead records the first read of a delivery. Not needed by T1 itself —
	// it is written now because D-8's escalation predicate ("unread past an age
	// threshold") is exactly this history, and retrofitting it later would mean
	// shipping an escalation policy with nothing to escalate against.
	TypeRead = "alpha.mail.read"
	// TypeAcked records the one call that won a delivery's transition (G13).
	TypeAcked = "alpha.mail.acked"
)

// EventVersion is the envelope version stamped on every appended event. The
// store is alpha and has no upcasters (G10); the field exists so a future reader
// can tell which shape it is looking at rather than guess.
const EventVersion = 1

// eventNamespace is the prefix every event type must carry.
const eventNamespace = "alpha.mail."

// Event is one entry in the append-only log. The log is the authority; `mail`,
// `subscriptions` and `deliveries` are projections of it maintained in the same
// transaction (D-1).
type Event struct {
	// ID is the event's own ULID, distinct from any mail id in the payload.
	ID string
	// Type is one of the alpha.mail.* constants above.
	Type string
	// Version defaults to EventVersion when zero.
	Version int
	// OccurredAt defaults to the append time when zero.
	OccurredAt time.Time
	// Payload is the event's data, stored as a JSON object.
	Payload map[string]any
}

// Append writes one event to the log. **It is the only writer of the `events`
// table** — everything else in this package projects from what it appends, and
// the BEFORE UPDATE / BEFORE DELETE triggers in the schema make that a property
// the database enforces rather than a rule this package remembers (G1).
func Append(ctx context.Context, tx *sql.Tx, ev Event) error {
	if !strings.HasPrefix(ev.Type, eventNamespace) {
		return fmt.Errorf("append event: type %q is outside the %s namespace", ev.Type, eventNamespace)
	}
	if ev.ID == "" {
		ev.ID = ulid.New()
	}
	if ev.Version == 0 {
		ev.Version = EventVersion
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	if ev.Payload == nil {
		ev.Payload = map[string]any{}
	}
	payload, err := json.Marshal(ev.Payload)
	if err != nil {
		return fmt.Errorf("marshal %s payload: %w", ev.Type, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events (id, type, version, occurred_at, payload) VALUES (?, ?, ?, ?, ?)`,
		ev.ID, ev.Type, ev.Version, formatTime(ev.OccurredAt), string(payload)); err != nil {
		return fmt.Errorf("append %s: %w", ev.Type, err)
	}
	return nil
}

// timeLayout is how every timestamp is stored. RFC 3339 in UTC sorts
// lexicographically the same way it sorts chronologically, so a text column is
// a correct index for "before"/"after" without a conversion on read.
const timeLayout = time.RFC3339Nano

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored timestamp %q: %w", s, err)
	}
	return t, nil
}
