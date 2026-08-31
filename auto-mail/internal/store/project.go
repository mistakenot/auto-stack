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
	// FromNow starts the cursor at the store's current high-water mark instead
	// of the beginning of the log, so existing mail is not backfilled (D-10).
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
		var cursor int64
		row := tx.QueryRowContext(ctx, `
			SELECT s.id, s.from_cursor
			FROM subscriptions s
			JOIN bindings b ON b.subscription_id = s.id
			WHERE s.address = ? AND b.manager = ? AND b.target = ?
			ORDER BY s.seq
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
			// A from-now cursor is the store's own high-water mark, read inside
			// this BEGIN IMMEDIATE transaction — never an id minted here.
			//
			// The tempting version of this is a fresh ULID: "every mail sent
			// after this instant carries a greater id". That holds only inside
			// one process. D-11 makes every send its own process against one
			// file, and two ULIDs drawn in the same millisecond by different
			// processes share a timestamp prefix but draw independent random
			// tails, so they can sort either way — and a mail that sorted below
			// such a cursor would be admitted by nothing, ever. The write lock
			// is what makes the mark true instead: any mail committed after
			// this transaction is appended after it, and therefore has a
			// strictly greater seq.
			if p.FromNow {
				mark, err := highWaterMark(ctx, tx)
				if err != nil {
					return err
				}
				cursor = mark
			}
			id, err := mintSubscriptionID(ctx, tx)
			if err != nil {
				return err
			}
			out.SubscriptionID = id
			// Appended before the projection rather than after it, because the
			// subscription's own seq *is* the log position of this event: the
			// row is a projection of the event down to its ordering (D-1).
			// It is appended only when the subscription is created — a
			// re-subscribe changes no subscription state, and the log records
			// state changes.
			seq, err := Append(ctx, tx, Event{
				Type:       TypeSubscribed,
				OccurredAt: now,
				Payload: map[string]any{
					"subscription": id,
					"address":      p.Address,
					"fromCursor":   cursor,
				},
			})
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO subscriptions (id, seq, address, name, from_cursor, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
				id, seq, p.Address, nullable(p.Name), cursor, formatTime(now)); err != nil {
				return fmt.Errorf("insert subscription for %q: %w", p.Address, err)
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO bindings (subscription_id, manager, target, session, last_seen) VALUES (?, ?, ?, ?, ?)`,
				id, p.Caller.Manager, p.Caller.Target, nullable(p.Caller.Session), formatTime(now)); err != nil {
				return fmt.Errorf("insert binding for %s: %w", id, err)
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

// highWaterMark is the store's "everything up to here" position: the greatest
// mail seq the log holds. It must be read inside a BEGIN IMMEDIATE transaction,
// which every caller here is — the write lock is what makes it a boundary
// rather than a sample, because it is what stops a concurrent sender from
// committing between the read and the write that stores it.
func highWaterMark(ctx context.Context, tx *sql.Tx) (int64, error) {
	var seq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) FROM mail`).Scan(&seq); err != nil {
		return 0, fmt.Errorf("read the mail high-water mark: %w", err)
	}
	return seq, nil
}

// countBackfill counts the mail this subscription's cursor admits and which has
// no delivery row yet — what its next list will materialise (D-10).
func countBackfill(ctx context.Context, tx *sql.Tx, subscriptionID, address string, cursor int64) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM mail m
		WHERE m.to_address = ?
		  AND m.seq > ?
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
	// Delivered is the binding of every bound subscription this mail was
	// actually delivered to. It is what the caller raises a pending flag on:
	// the flag is per binding, so the set has to come back from the same
	// transaction that decided which subscriptions the cursor admitted.
	Delivered []Caller
}

// Send appends alpha.mail.sent and inserts the immutable mail row. That is the
// whole write: **no delivery row is created here** (D-10). Delivery rows are
// materialised lazily on the read path, against each subscription's cursor,
// which is what lets a subscription created *after* a send still receive it
// (journey J2) — the two paths cannot disagree because there is only one.
//
// What Send does compute is the pair of counts the sender acts on, and the
// bindings whose pending flag this mail should raise. Those are reads, not
// materialisation: a flag is a hint about state, and the store stays the only
// authority on what is actually waiting.
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
		seq, err := Append(ctx, tx, Event{
			Type:       TypeSent,
			OccurredAt: now,
			Payload: map[string]any{
				"mail": out.ID,
				"to":   p.To,
				"from": p.From,
			},
		})
		if err != nil {
			return err
		}
		// The mail row carries the log position of its own sent event. That
		// number, not the id, is what every cursor comparison uses.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO mail (id, seq, to_address, envelope, body, sent_at) VALUES (?, ?, ?, ?, ?, ?)`,
			out.ID, seq, p.To, string(envelope), string(body), formatTime(now)); err != nil {
			return fmt.Errorf("insert mail %s: %w", out.ID, err)
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*), COUNT(b.subscription_id)
			FROM subscriptions s
			LEFT JOIN bindings b ON b.subscription_id = s.id
			WHERE s.address = ?`, p.To).Scan(&out.Subscriptions, &out.Bound); err != nil {
			return fmt.Errorf("count readers of %q: %w", p.To, err)
		}
		delivered, err := deliveredBindings(ctx, tx, p.To, seq)
		if err != nil {
			return err
		}
		out.Delivered = delivered
		return nil
	})
	if err != nil {
		return SendOutcome{}, err
	}
	return out, nil
}

// deliveredBindings returns the binding of every subscription whose cursor
// admits this mail — the bindings whose pending flag it should raise. The
// predicate is the same one the read path materialises with, so a --from-now
// subscription that will not receive the mail does not get its flag raised
// either. It reads; it materialises nothing (D-10).
func deliveredBindings(ctx context.Context, tx *sql.Tx, address string, seq int64) ([]Caller, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT b.manager, b.target, COALESCE(b.session, '')
		FROM bindings b
		JOIN subscriptions s ON s.id = b.subscription_id
		WHERE s.address = ? AND ? > s.from_cursor
		ORDER BY s.seq`, address, seq)
	if err != nil {
		return nil, fmt.Errorf("select bindings for %q: %w", address, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Caller
	for rows.Next() {
		var c Caller
		if err := rows.Scan(&c.Manager, &c.Target, &c.Session); err != nil {
			return nil, fmt.Errorf("scan binding for %q: %w", address, err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read bindings for %q: %w", address, err)
	}
	return out, nil
}

// PendingCount reports how much unretired mail this caller's bound
// subscriptions hold: unacked deliveries plus the mail their cursors admit but
// which no list has materialised yet.
//
// Both halves are needed because the two are the same thing at different ages —
// counting only delivery rows would report zero for a backlog nobody has listed
// yet, and clearing the pending flag on that answer would drop a nudge for mail
// that is genuinely waiting. It is a pure read: nothing is materialised and no
// read is stamped, because it exists only to decide whether the flag still
// means anything.
func (s *Store) PendingCount(ctx context.Context, c Caller) (int, error) {
	var unacked, admitted int
	err := s.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(*)
		     FROM deliveries d
		     JOIN bindings b ON b.subscription_id = d.subscription_id
		    WHERE d.acked_at IS NULL AND b.manager = ? AND b.target = ?),
		  (SELECT COUNT(*)
		     FROM mail m
		     JOIN subscriptions s ON s.address = m.to_address
		     JOIN bindings b ON b.subscription_id = s.id
		    WHERE b.manager = ? AND b.target = ? AND m.seq > s.from_cursor
		      AND NOT EXISTS (
		        SELECT 1 FROM deliveries d
		         WHERE d.subscription_id = s.id AND d.mail_id = m.id
		      ))`,
		c.Manager, c.Target, c.Manager, c.Target).Scan(&unacked, &admitted)
	if err != nil {
		return 0, fmt.Errorf("count pending mail for binding: %w", err)
	}
	return unacked + admitted, nil
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
			if err := materialise(ctx, tx, sub, now, ""); err != nil {
				return err
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
				if _, err := Append(ctx, tx, Event{
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
	cursor  int64
}

// materialise is the **only** place a delivery row is created (D-10). It inserts
// the rows this subscription's cursor admits and which do not exist yet, so a
// subscription created after a mail was sent still sees it, and a --from-now
// subscription never sees what preceded its mark.
//
// It lives on the read path on purpose: `send` writing delivery rows would make
// the sender pay O(subscriptions) for readers that may never read, would put
// half the reader projection inside the writer, and — being a second path to
// the same rows — could disagree with this one. onlyMail scopes the work to a
// single mail id when the caller already knows which one it wants (ack does).
func materialise(ctx context.Context, tx *sql.Tx, sub scopedSub, now time.Time, onlyMail string) error {
	query := `
		INSERT OR IGNORE INTO deliveries (subscription_id, mail_id, first_seen_at)
		SELECT ?, m.id, ? FROM mail m WHERE m.to_address = ? AND m.seq > ?`
	args := []any{sub.id, formatTime(now), sub.address, sub.cursor}
	if onlyMail != "" {
		query += ` AND m.id = ?`
		args = append(args, onlyMail)
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("materialise deliveries for %s: %w", sub.id, err)
	}
	return nil
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
	// Ordered by the log position the store assigned, not by the id a process
	// minted: "the order these subscriptions were created" is the log's answer.
	query += ` ORDER BY s.seq`

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
		ORDER BY m.seq`, subscriptionID)
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
		// Ack is a read-side operation — the caller is retiring its own copy —
		// so it materialises against its own cursors first, exactly as list
		// does. Without this, `send` then `ack` with no `list` between them
		// would fail on a mail the cursor plainly admits, purely because
		// nothing had happened to project it yet (D-10).
		subs, err := scopedSubscriptions(ctx, tx, ListParams{Caller: p.Caller})
		if err != nil {
			return err
		}
		for _, sub := range subs {
			if err := materialise(ctx, tx, sub, now, p.MailID); err != nil {
				return err
			}
		}

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
			if _, err := Append(ctx, tx, Event{
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

// AddressForBinding returns the address of the first subscription this caller
// created, which is rung 2 of the sender's from-address ladder.
//
// "First" is the log's ordering, not a comparison of two minted ids. The id is
// `sub_` plus a ULID prefix, and a prefix is timestamp bits: two subscriptions
// created in the same window by two processes carry no reliable order between
// them, so ordering by id would make this rung's answer depend on entropy.
// Ordering by the subscribed event's seq makes it depend on the store.
func (s *Store) AddressForBinding(ctx context.Context, c Caller) (string, bool, error) {
	var address string
	err := s.QueryRowContext(ctx, `
		SELECT s.address
		FROM subscriptions s
		JOIN bindings b ON b.subscription_id = s.id
		WHERE b.manager = ? AND b.target = ?
		ORDER BY s.seq
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
