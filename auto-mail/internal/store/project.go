package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mistakenot/auto-mail/internal/ulid"
)

// Sentinel errors the client seam maps onto its own exported sentinels.
var (
	// ErrUnknownMail means no mail row carries that id.
	ErrUnknownMail = errors.New("unknown mail id")
	// ErrNoDelivery means the mail exists but the caller's subscriptions hold
	// no delivery row for it.
	ErrNoDelivery = errors.New("no delivery for this caller")
)

// Caller is the physical context an operation runs under: the opaque
// (manager, target) pair G12 mandates, plus the session it came from. It is
// stored **only** on the bindings row and never reaches an address or a stored
// envelope (G5).
type Caller struct {
	Manager string
	Target  string
	Session string
}

// SubscribeParams asks for a durable reader of an address.
type SubscribeParams struct {
	Address string
	Name    string
	// FromNow starts the cursor at the current high-water mark instead of the
	// beginning of the log, so existing mail is not backfilled (D-10).
	FromNow bool
	Caller  Caller
	Now     time.Time
}

// SubscribeOutcome is what the subscription looks like afterwards.
type SubscribeOutcome struct {
	SubscriptionID string
	// Backfilled counts the mail the cursor admits that has no delivery row
	// yet — the mail this subscription will see on its next list.
	Backfilled int
}

// Subscribe creates (or returns) the subscription for an address held by this
// caller, stamps its binding, and reports the backfill. One BEGIN IMMEDIATE
// transaction covers the append and every projection it implies (D-1).
//
// Subscription identity is (address, caller binding): two agents subscribing to
// one address get two subscriptions and therefore two independent delivery and
// ack states — that is broadcast (D-7). One agent subscribing twice gets the
// same subscription back rather than a second copy of every mail.
func (s *Store) Subscribe(ctx context.Context, p SubscribeParams) (SubscribeOutcome, error) {
	now := orNow(p.Now)
	var out SubscribeOutcome

	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		var cursor string
		row := tx.QueryRowContext(ctx, `
			SELECT s.id, s.from_cursor
			FROM subscriptions s
			JOIN bindings b ON b.subscription_id = s.id
			WHERE s.address = ? AND b.manager = ? AND b.target = ?
			ORDER BY s.id
			LIMIT 1`, p.Address, p.Caller.Manager, p.Caller.Target)
		switch err := row.Scan(&out.SubscriptionID, &cursor); {
		case err == nil:
			// Already subscribed. Re-stamp the binding — subscribe is the only
			// writer of a binding row in T1, because refreshing last_seen from
			// the hook would mean opening the store in the agent's hot path,
			// which G8 forbids. Heartbeat refresh arrives with T3.
			if _, err := tx.ExecContext(ctx,
				`UPDATE bindings SET manager = ?, target = ?, session = ?, last_seen = ?
				 WHERE subscription_id = ?`,
				p.Caller.Manager, p.Caller.Target, p.Caller.Session, formatTime(now), out.SubscriptionID); err != nil {
				return fmt.Errorf("refresh binding for %s: %w", out.SubscriptionID, err)
			}
		case errors.Is(err, sql.ErrNoRows):
			// A from-now cursor is a fresh ULID: every mail sent after this
			// instant carries a greater id, and every mail already stored
			// carries a smaller one, so the plain string comparison the cursor
			// is used with means exactly "from now on".
			if p.FromNow {
				cursor = ulid.NewAt(now)
			}
			id, err := mintSubscriptionID(ctx, tx)
			if err != nil {
				return err
			}
			out.SubscriptionID = id
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO subscriptions (id, address, name, from_cursor, created_at) VALUES (?, ?, ?, ?, ?)`,
				id, p.Address, nullable(p.Name), cursor, formatTime(now)); err != nil {
				return fmt.Errorf("insert subscription for %q: %w", p.Address, err)
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO bindings (subscription_id, manager, target, session, last_seen) VALUES (?, ?, ?, ?, ?)`,
				id, p.Caller.Manager, p.Caller.Target, nullable(p.Caller.Session), formatTime(now)); err != nil {
				return fmt.Errorf("insert binding for %s: %w", id, err)
			}
			// Appended only when the subscription is created: a re-subscribe
			// changes no subscription state, and the log records state changes.
			if err := Append(ctx, tx, Event{
				Type:       TypeSubscribed,
				OccurredAt: now,
				Payload: map[string]any{
					"subscription": id,
					"address":      p.Address,
					"fromCursor":   cursor,
				},
			}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("look up subscription for %q: %w", p.Address, err)
		}

		count, err := countBackfill(ctx, tx, out.SubscriptionID, p.Address, cursor)
		if err != nil {
			return err
		}
		out.Backfilled = count
		return nil
	})
	return out, err
}

// mintSubscriptionID returns an id that is free in this transaction.
//
// The canonical `sub_01K9X2M4` form carries only the ULID's top 40 timestamp
// bits, so two subscriptions created within the same ~256ms window mint the
// identical string — two agents subscribing back to back is ordinary here, not
// exotic. Lengthening the prefix resolves it without a second source of ids,
// and terminates because the full ULID is unique.
func mintSubscriptionID(ctx context.Context, tx *sql.Tx) (string, error) {
	u := ulid.New()
	for chars := ulid.MinSubscriptionChars; chars <= ulid.Length; chars++ {
		candidate := ulid.SubscriptionIDFrom(u, chars)
		var taken int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM subscriptions WHERE id = ?`, candidate).Scan(&taken); err != nil {
			return "", fmt.Errorf("check subscription id %s: %w", candidate, err)
		}
		if taken == 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("mint subscription id: %q is already taken at full ULID length", u)
}

// countBackfill counts the mail this subscription's cursor admits and which has
// no delivery row yet — what its next list will materialise (D-10).
func countBackfill(ctx context.Context, tx *sql.Tx, subscriptionID, address, cursor string) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM mail m
		WHERE m.to_address = ?
		  AND m.id > ?
		  AND NOT EXISTS (
			SELECT 1 FROM deliveries d WHERE d.subscription_id = ? AND d.mail_id = m.id
		  )`, address, cursor, subscriptionID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count backfill for %s: %w", subscriptionID, err)
	}
	return count, nil
}

// SendParams posts one mail to an address.
type SendParams struct {
	To   string
	From string
	Body map[string]any
	Now  time.Time
}

// SendOutcome reports the id and the two counts a sender acts on. Subscriptions
// counts durable readers of the address; Bound counts those with a binding row.
// In T1 Bound means "a binding row exists", not "an agent is alive right now" —
// nothing refreshes last_seen until T3 (D-062-2).
type SendOutcome struct {
	ID            string
	Subscriptions int
	Bound         int
}

// Send appends alpha.mail.sent, inserts the immutable mail row, and inserts a
// delivery row for every subscription on the address whose cursor admits it.
//
// Delivery does not depend on the subscription being bound: a subscription with
// no binding row still receives, and the sender is told the difference through
// the two counts (G6).
func (s *Store) Send(ctx context.Context, p SendParams) (SendOutcome, error) {
	now := orNow(p.Now)
	out := SendOutcome{ID: ulid.NewAt(now)}

	body, err := json.Marshal(orEmpty(p.Body))
	if err != nil {
		return SendOutcome{}, fmt.Errorf("marshal mail body: %w", err)
	}
	// The envelope carries a version integer so a future reader can tell which
	// shape it is looking at (G10/AC-11). It carries the virtual addresses and
	// nothing physical — no host id, no session id, no pane (G5).
	envelope, err := json.Marshal(map[string]any{
		"version": EventVersion,
		"to":      p.To,
		"from":    p.From,
		"sentAt":  formatTime(now),
	})
	if err != nil {
		return SendOutcome{}, fmt.Errorf("marshal mail envelope: %w", err)
	}

	err = s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := Append(ctx, tx, Event{
			Type:       TypeSent,
			OccurredAt: now,
			Payload: map[string]any{
				"mail": out.ID,
				"to":   p.To,
				"from": p.From,
			},
		}); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO mail (id, to_address, envelope, body, sent_at) VALUES (?, ?, ?, ?, ?)`,
			out.ID, p.To, string(envelope), string(body), formatTime(now)); err != nil {
			return fmt.Errorf("insert mail %s: %w", out.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO deliveries (subscription_id, mail_id, first_seen_at)
			SELECT s.id, ?, ? FROM subscriptions s WHERE s.address = ? AND ? > s.from_cursor`,
			out.ID, formatTime(now), p.To, out.ID); err != nil {
			return fmt.Errorf("materialise deliveries for %s: %w", out.ID, err)
		}
		return tx.QueryRowContext(ctx, `
			SELECT COUNT(*), COUNT(b.subscription_id)
			FROM subscriptions s
			LEFT JOIN bindings b ON b.subscription_id = s.id
			WHERE s.address = ?`, p.To).Scan(&out.Subscriptions, &out.Bound)
	})
	if err != nil {
		return SendOutcome{}, err
	}
	return out, nil
}

// ListParams scopes a read. With no filters it covers every subscription the
// caller is bound to, per the project convention that a list command with no
// filters returns everything.
type ListParams struct {
	Caller Caller
	// Address scopes to the caller's subscriptions on one address.
	Address string
	// SubscriptionID scopes to exactly one subscription, whoever holds it —
	// naming a subscription is explicit enough not to need the binding filter.
	SubscriptionID string
	Now            time.Time
}

// ListedMail is one unacked mail as a reader sees it.
type ListedMail struct {
	ID     string
	From   string
	SentAt time.Time
	Body   map[string]any
}

// List materialises any delivery rows the cursor admits, stamps the first read,
// and returns the unacked mail. It never retires anything — ack is always a
// separate explicit call (G3).
func (s *Store) List(ctx context.Context, p ListParams) ([]ListedMail, error) {
	now := orNow(p.Now)
	out := make([]ListedMail, 0)

	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		subs, err := scopedSubscriptions(ctx, tx, p)
		if err != nil {
			return err
		}
		for _, sub := range subs {
			// D-10: delivery rows are materialised lazily against the cursor,
			// so a subscription created after a mail was sent still sees it.
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO deliveries (subscription_id, mail_id, first_seen_at)
				SELECT ?, m.id, ? FROM mail m WHERE m.to_address = ? AND m.id > ?`,
				sub.id, formatTime(now), sub.address, sub.cursor); err != nil {
				return fmt.Errorf("materialise deliveries for %s: %w", sub.id, err)
			}

			listed, unread, err := unackedFor(ctx, tx, sub.id)
			if err != nil {
				return err
			}
			out = append(out, listed...)

			for _, mailID := range unread {
				// Guarded by `read_at IS NULL`, so repeated lists append no
				// further events. read_at is not needed by T1; it is written
				// now because D-8's escalation predicate reads exactly this
				// column, and T3 must not ship with no history to escalate on.
				res, err := tx.ExecContext(ctx,
					`UPDATE deliveries SET read_at = ? WHERE subscription_id = ? AND mail_id = ? AND read_at IS NULL`,
					formatTime(now), sub.id, mailID)
				if err != nil {
					return fmt.Errorf("stamp read for %s/%s: %w", sub.id, mailID, err)
				}
				affected, err := res.RowsAffected()
				if err != nil {
					return fmt.Errorf("stamp read for %s/%s: %w", sub.id, mailID, err)
				}
				if affected == 0 {
					continue
				}
				if err := Append(ctx, tx, Event{
					Type:       TypeRead,
					OccurredAt: now,
					Payload:    map[string]any{"subscription": sub.id, "mail": mailID},
				}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type scopedSub struct {
	id      string
	address string
	cursor  string
}

func scopedSubscriptions(ctx context.Context, tx *sql.Tx, p ListParams) ([]scopedSub, error) {
	query := `
		SELECT s.id, s.address, s.from_cursor
		FROM subscriptions s
		JOIN bindings b ON b.subscription_id = s.id
		WHERE b.manager = ? AND b.target = ?`
	args := []any{p.Caller.Manager, p.Caller.Target}
	if p.Address != "" {
		query += ` AND s.address = ?`
		args = append(args, p.Address)
	}
	if p.SubscriptionID != "" {
		// An explicitly named subscription is not filtered by the binding: the
		// caller has already said which one it means.
		query = `SELECT s.id, s.address, s.from_cursor FROM subscriptions s WHERE s.id = ?`
		args = []any{p.SubscriptionID}
	}
	query += ` ORDER BY s.id`

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select subscriptions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []scopedSub
	for rows.Next() {
		var sub scopedSub
		if err := rows.Scan(&sub.id, &sub.address, &sub.cursor); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read subscriptions: %w", err)
	}
	return out, nil
}

// unackedFor returns one subscription's unacked mail plus the ids that have not
// been read yet, so the caller can stamp them after the rows are closed.
func unackedFor(ctx context.Context, tx *sql.Tx, subscriptionID string) ([]ListedMail, []string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT m.id, m.envelope, m.body, m.sent_at, d.read_at
		FROM deliveries d
		JOIN mail m ON m.id = d.mail_id
		WHERE d.subscription_id = ? AND d.acked_at IS NULL
		ORDER BY m.id`, subscriptionID)
	if err != nil {
		return nil, nil, fmt.Errorf("select unacked mail for %s: %w", subscriptionID, err)
	}
	defer func() { _ = rows.Close() }()

	var listed []ListedMail
	var unread []string
	for rows.Next() {
		var (
			id, envelope, body, sentAt string
			readAt                     sql.NullString
		)
		if err := rows.Scan(&id, &envelope, &body, &sentAt, &readAt); err != nil {
			return nil, nil, fmt.Errorf("scan delivery for %s: %w", subscriptionID, err)
		}
		item, err := hydrate(id, envelope, body, sentAt)
		if err != nil {
			return nil, nil, err
		}
		listed = append(listed, item)
		if !readAt.Valid {
			unread = append(unread, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("read deliveries for %s: %w", subscriptionID, err)
	}
	return listed, unread, nil
}

func hydrate(id, envelope, body, sentAt string) (ListedMail, error) {
	var env struct {
		From string `json:"from"`
	}
	if err := json.Unmarshal([]byte(envelope), &env); err != nil {
		return ListedMail{}, fmt.Errorf("decode envelope of mail %s: %w", id, err)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		return ListedMail{}, fmt.Errorf("decode body of mail %s: %w", id, err)
	}
	at, err := parseTime(sentAt)
	if err != nil {
		return ListedMail{}, err
	}
	return ListedMail{ID: id, From: env.From, SentAt: at, Body: decoded}, nil
}

// AckParams retires one mail for every subscription the caller holds it under.
type AckParams struct {
	MailID string
	Caller Caller
	Now    time.Time
}

// AckOutcome answers two different questions. The delivery being acked is what
// a caller acts on; Won answers "was it this call that transitioned it" (G13).
type AckOutcome struct {
	Won     bool
	AckedAt time.Time
	AckedBy string
}

// Ack is a compare-and-set inside the append-and-project transaction: the
// affected-row count of an `acked_at IS NULL` update **is** the answer to
// whether this call won, which is why it can never disagree with the store.
// The alpha.mail.acked event is appended only on a win, so an already-acked
// delivery is idempotent rather than a second transition (G4/G13).
func (s *Store) Ack(ctx context.Context, p AckParams) (AckOutcome, error) {
	now := orNow(p.Now)
	var out AckOutcome

	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		deliveries, err := heldDeliveries(ctx, tx, p.MailID, p.Caller)
		if err != nil {
			return err
		}

		if len(deliveries) == 0 {
			var known int
			if err := tx.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM mail WHERE id = ?`, p.MailID).Scan(&known); err != nil {
				return fmt.Errorf("look up mail %s: %w", p.MailID, err)
			}
			if known == 0 {
				return fmt.Errorf("%w: %s", ErrUnknownMail, p.MailID)
			}
			return fmt.Errorf("%w: %s", ErrNoDelivery, p.MailID)
		}

		for _, d := range deliveries {
			res, err := tx.ExecContext(ctx, `
				UPDATE deliveries SET acked_at = ?, acked_by = ?
				WHERE subscription_id = ? AND mail_id = ? AND acked_at IS NULL`,
				formatTime(now), d.subscription, d.subscription, p.MailID)
			if err != nil {
				return fmt.Errorf("ack %s for %s: %w", p.MailID, d.subscription, err)
			}
			affected, err := res.RowsAffected()
			if err != nil {
				return fmt.Errorf("ack %s for %s: %w", p.MailID, d.subscription, err)
			}
			if affected == 0 {
				// Lost, or already acked. Carry who won it so the caller can be
				// told on stderr rather than left to guess (D-062-7).
				if !out.Won && d.ackedAt.Valid {
					if at, err := parseTime(d.ackedAt.String); err == nil {
						out.AckedAt = at
					}
					out.AckedBy = d.ackedBy.String
				}
				continue
			}
			out.Won = true
			out.AckedAt = now
			out.AckedBy = d.subscription
			if err := Append(ctx, tx, Event{
				Type:       TypeAcked,
				OccurredAt: now,
				Payload:    map[string]any{"subscription": d.subscription, "mail": p.MailID},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// heldDelivery is one of the caller's delivery rows for a mail, with whatever
// ack state it already carries.
type heldDelivery struct {
	subscription string
	ackedAt      sql.NullString
	ackedBy      sql.NullString
}

// heldDeliveries returns every delivery row for a mail that belongs to a
// subscription this caller is bound to. An empty result is the caller holding no
// delivery, which Ack distinguishes from the mail not existing at all.
func heldDeliveries(ctx context.Context, tx *sql.Tx, mailID string, c Caller) ([]heldDelivery, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT d.subscription_id, d.acked_at, d.acked_by
		FROM deliveries d
		JOIN bindings b ON b.subscription_id = d.subscription_id
		WHERE d.mail_id = ? AND b.manager = ? AND b.target = ?
		ORDER BY d.subscription_id`, mailID, c.Manager, c.Target)
	if err != nil {
		return nil, fmt.Errorf("select deliveries for %s: %w", mailID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []heldDelivery
	for rows.Next() {
		var h heldDelivery
		if err := rows.Scan(&h.subscription, &h.ackedAt, &h.ackedBy); err != nil {
			return nil, fmt.Errorf("scan delivery for %s: %w", mailID, err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read deliveries for %s: %w", mailID, err)
	}
	return out, nil
}

// AddressForBinding returns the address of the lowest-id subscription bound to
// this caller, which is rung 2 of the sender's from-address ladder. "Lowest id"
// is a deterministic tie-break rather than an arbitrary one: subscription ids
// are ULID-prefixed, so it means "the first one this agent created".
func (s *Store) AddressForBinding(ctx context.Context, c Caller) (string, bool, error) {
	var address string
	err := s.QueryRowContext(ctx, `
		SELECT s.address
		FROM subscriptions s
		JOIN bindings b ON b.subscription_id = s.id
		WHERE b.manager = ? AND b.target = ?
		ORDER BY s.id
		LIMIT 1`, c.Manager, c.Target).Scan(&address)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("resolve from-address for binding: %w", err)
	}
	return address, true, nil
}

func orNow(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t.UTC()
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// nullable stores an empty optional string as SQL NULL rather than "".
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
