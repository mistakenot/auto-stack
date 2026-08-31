package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mistakenot/auto-mail/internal/store"
)

// TestOpenMigrateIdempotent: opening a fresh store creates its parent
// directory, and migrating twice is a no-op — the alpha store has exactly one
// schema and `CREATE ... IF NOT EXISTS` must stay re-runnable (G10).
func TestOpenMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail", "alpha-store.db")

	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	for i := range 2 {
		if err := st.Migrate(ctx); err != nil {
			t.Fatalf("migrate #%d: %v", i+1, err)
		}
	}

	if got := st.Path(); got != path {
		t.Errorf("Path() = %q, want %q", got, path)
	}

	// Every table the final shape declares must exist after migration, so
	// phase 2 fills write paths rather than adding a migration.
	want := []string{"schema_migrations", "events", "mail", "subscriptions", "deliveries", "bindings"}
	rows, err := st.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer func() { _ = rows.Close() }()
	have := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	for _, table := range want {
		if !have[table] {
			t.Errorf("table %q missing after migrate; have %v", table, have)
		}
	}

	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestWithTxRollsBackOnError: a failing operation leaves no partial state, so
// the append-and-project pair phase 2 adds is genuinely atomic (D-1).
func TestWithTxRollsBackOnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alpha-store.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	boom := errors.New("boom")
	err = st.WithTx(ctx, func(tx *sql.Tx) error {
		if _, execErr := tx.ExecContext(ctx,
			`INSERT INTO events (id, type, version, occurred_at, payload) VALUES (?, ?, ?, ?, ?)`,
			"01TEST", "alpha.mail.sent", 1, "2026-08-25T10:14:02Z", "{}"); execErr != nil {
			return execErr
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("WithTx error = %v, want %v", err, boom)
	}

	var count int
	row := st.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 0 {
		t.Errorf("events after rollback = %d, want 0", count)
	}
}

// open is a migrated store on a fresh temp path.
func open(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "mail", "alpha-store.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

func caller(target string) store.Caller {
	return store.Caller{Manager: "cwd", Target: target}
}

// TestEventLogIsAppendOnly is G1 enforced by the database rather than by this
// package remembering it: the log's insert path is its only write path, and a
// handle that tries otherwise is aborted with a message naming the rail.
func TestEventLogIsAppendOnly(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	if err := st.WithTx(ctx, func(tx *sql.Tx) error {
		_, appendErr := store.Append(ctx, tx, store.Event{
			Type:    store.TypeSent,
			Payload: map[string]any{"mail": "01TEST"},
		})
		return appendErr
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	for _, tc := range []struct{ name, stmt string }{
		{"update", `UPDATE events SET type = 'alpha.mail.acked'`},
		{"delete", `DELETE FROM events`},
	} {
		err := st.WithTx(ctx, func(tx *sql.Tx) error {
			_, execErr := tx.ExecContext(ctx, tc.stmt)
			return execErr
		})
		if err == nil {
			t.Errorf("%s on events succeeded; the log must be append-only", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), "G1") {
			t.Errorf("%s on events failed with %v; the abort message must name the rail", tc.name, err)
		}
	}

	// The event survived both attempts, byte for byte.
	var typ string
	if err := st.QueryRowContext(ctx, `SELECT type FROM events`).Scan(&typ); err != nil {
		t.Fatalf("read event back: %v", err)
	}
	if typ != store.TypeSent {
		t.Errorf("event type = %q after the rejected writes, want %q", typ, store.TypeSent)
	}
}

// TestAppendRejectsAnEventOutsideTheAlphaNamespace: every T1 event type carries
// the alpha. prefix, so no consumer can match a bare `mail.sent` and read a
// compatibility promise into an alpha store (G10).
func TestAppendRejectsAnEventOutsideTheAlphaNamespace(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	err := st.WithTx(ctx, func(tx *sql.Tx) error {
		_, appendErr := store.Append(ctx, tx, store.Event{Type: "mail.sent"})
		return appendErr
	})
	if err == nil {
		t.Fatal("appending `mail.sent` succeeded; only alpha.mail.* is appendable")
	}

	// And the four constants are the complete T1 vocabulary.
	for _, typ := range []string{store.TypeSubscribed, store.TypeSent, store.TypeRead, store.TypeAcked} {
		if !strings.HasPrefix(typ, "alpha.mail.") {
			t.Errorf("event type %q is outside the alpha.mail.* namespace", typ)
		}
	}
}

// TestMailRowIsImmutable: a mail row is an immutable projection of its
// alpha.mail.sent event. Consumer state lives on deliveries, keyed by
// (subscription_id, mail_id), so nothing ever needs to update one (G1/G2).
func TestMailRowIsImmutable(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	sent, err := st.Send(ctx, store.SendParams{To: "auto-web/bugs", From: "auto-stack/reviewer"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	for _, tc := range []struct{ name, stmt string }{
		{"update", `UPDATE mail SET to_address = 'elsewhere'`},
		{"delete", `DELETE FROM mail`},
	} {
		err := st.WithTx(ctx, func(tx *sql.Tx) error {
			_, execErr := tx.ExecContext(ctx, tc.stmt)
			return execErr
		})
		if err == nil {
			t.Errorf("%s on mail succeeded; a mail row is immutable", tc.name)
		} else if !strings.Contains(err.Error(), "G1") {
			t.Errorf("%s on mail failed with %v; the abort message must name the rail", tc.name, err)
		}
	}

	var to string
	if err := st.QueryRowContext(ctx, `SELECT to_address FROM mail WHERE id = ?`, sent.ID).Scan(&to); err != nil {
		t.Fatalf("read mail back: %v", err)
	}
	if to != "auto-web/bugs" {
		t.Errorf("to_address = %q after the rejected writes, want %q", to, "auto-web/bugs")
	}
}

// TestNoConsumerStateOnTheMailRow is the structural half of G2: read_at and
// acked_at exist only on deliveries. A column on mail would make consumer state
// global to every reader, which is exactly what broadcast forbids.
func TestNoConsumerStateOnTheMailRow(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	columns := func(table string) map[string]bool {
		rows, err := st.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatalf("describe %s: %v", table, err)
		}
		defer func() { _ = rows.Close() }()
		out := map[string]bool{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatalf("scan column of %s: %v", table, err)
			}
			out[name] = true
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("read columns of %s: %v", table, err)
		}
		return out
	}

	mailColumns := columns("mail")
	for _, forbidden := range []string{"read_at", "acked_at", "acked_by"} {
		if mailColumns[forbidden] {
			t.Errorf("mail has a %q column; consumer state belongs on deliveries (G2)", forbidden)
		}
	}
	deliveryColumns := columns("deliveries")
	for _, required := range []string{"subscription_id", "mail_id", "read_at", "acked_at", "acked_by"} {
		if !deliveryColumns[required] {
			t.Errorf("deliveries has no %q column", required)
		}
	}
}

// TestBroadcastFanOut: two subscriptions on one address each get their own
// delivery and their own ack state, so acking one leaves the other unacked
// (D-7). The delivery join looks like pointless indirection with one reader —
// this is the case it exists for.
func TestBroadcastFanOut(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	first, err := st.Subscribe(ctx, store.SubscribeParams{Address: "auto-web/bugs", Caller: caller("/workspace/a")})
	if err != nil {
		t.Fatalf("subscribe a: %v", err)
	}
	second, err := st.Subscribe(ctx, store.SubscribeParams{Address: "auto-web/bugs", Caller: caller("/workspace/b")})
	if err != nil {
		t.Fatalf("subscribe b: %v", err)
	}
	if first.SubscriptionID == second.SubscriptionID {
		t.Fatalf("two agents on one address share subscription %q; that is not broadcast", first.SubscriptionID)
	}

	sent, err := st.Send(ctx, store.SendParams{To: "auto-web/bugs", From: "auto-stack/reviewer"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sent.Subscriptions != 2 || sent.Bound != 2 {
		t.Errorf("send reported subscriptions=%d bound=%d, want 2 and 2", sent.Subscriptions, sent.Bound)
	}

	acked, err := st.Ack(ctx, store.AckParams{MailID: sent.ID, Caller: caller("/workspace/a")})
	if err != nil {
		t.Fatalf("ack a: %v", err)
	}
	if !acked.Won {
		t.Error("the first ack did not win the transition")
	}

	listedA, err := st.List(ctx, store.ListParams{Caller: caller("/workspace/a")})
	if err != nil {
		t.Fatalf("list a: %v", err)
	}
	if len(listedA) != 0 {
		t.Errorf("agent a still sees %d mail after acking", len(listedA))
	}
	listedB, err := st.List(ctx, store.ListParams{Caller: caller("/workspace/b")})
	if err != nil {
		t.Fatalf("list b: %v", err)
	}
	if len(listedB) != 1 || listedB[0].ID != sent.ID {
		t.Errorf("agent b sees %+v; acking a's delivery must not retire b's", listedB)
	}
}

// TestCursorBackfillAndFromNow is D-10 and the J2 half of G6: a subscription
// created after a mail was sent still receives it, and --from-now is the
// explicit opt-out.
func TestCursorBackfillAndFromNow(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	sent, err := st.Send(ctx, store.SendParams{To: "auto-web/bugs", From: "auto-stack/reviewer"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sent.Subscriptions != 0 || sent.Bound != 0 {
		t.Errorf("send with nobody subscribed reported subscriptions=%d bound=%d, want 0 and 0",
			sent.Subscriptions, sent.Bound)
	}

	late, err := st.Subscribe(ctx, store.SubscribeParams{Address: "auto-web/bugs", Caller: caller("/workspace/late")})
	if err != nil {
		t.Fatalf("subscribe late: %v", err)
	}
	if late.Backfilled != 1 {
		t.Errorf("backfilled = %d, want 1 — a later subscriber must see the earlier mail", late.Backfilled)
	}
	listed, err := st.List(ctx, store.ListParams{Caller: caller("/workspace/late")})
	if err != nil {
		t.Fatalf("list late: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != sent.ID {
		t.Errorf("late subscriber sees %+v, want the earlier mail %s", listed, sent.ID)
	}

	fromNow, err := st.Subscribe(ctx, store.SubscribeParams{
		Address: "auto-web/bugs", FromNow: true, Caller: caller("/workspace/fromnow"),
	})
	if err != nil {
		t.Fatalf("subscribe --from-now: %v", err)
	}
	if fromNow.Backfilled != 0 {
		t.Errorf("--from-now backfilled = %d, want 0", fromNow.Backfilled)
	}
	listed, err = st.List(ctx, store.ListParams{Caller: caller("/workspace/fromnow")})
	if err != nil {
		t.Fatalf("list from-now: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("--from-now subscriber sees %+v, want nothing", listed)
	}
}

// TestListDoesNotRetireAndStampsReadOnce is G3 plus the read_at discipline:
// listing twice returns the same mail twice, and the second list appends no
// second alpha.mail.read event.
func TestListDoesNotRetireAndStampsReadOnce(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	if _, err := st.Subscribe(ctx, store.SubscribeParams{Address: "auto-web/bugs", Caller: caller("/workspace/b")}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	sent, err := st.Send(ctx, store.SendParams{To: "auto-web/bugs", From: "auto-stack/reviewer"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	for i := range 2 {
		listed, err := st.List(ctx, store.ListParams{Caller: caller("/workspace/b")})
		if err != nil {
			t.Fatalf("list #%d: %v", i+1, err)
		}
		if len(listed) != 1 || listed[0].ID != sent.ID {
			t.Fatalf("list #%d returned %+v, want the unacked mail; reading must not retire it", i+1, listed)
		}
	}

	if got := countEvents(t, st, store.TypeRead); got != 1 {
		t.Errorf("%s events = %d after two lists, want 1", store.TypeRead, got)
	}
	if got := countEvents(t, st, store.TypeAcked); got != 0 {
		t.Errorf("%s events = %d after listing, want 0 — only ack retires mail", store.TypeAcked, got)
	}
}

// TestAckTransitionsExactlyOnce: the affected-row count of an
// `acked_at IS NULL` update is the wonTransition answer, so a second ack is
// idempotent — it succeeds, appends no second event, and reports that it lost
// (G13/G4).
func TestAckTransitionsExactlyOnce(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	sub, err := st.Subscribe(ctx, store.SubscribeParams{Address: "auto-web/bugs", Caller: caller("/workspace/b")})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	sent, err := st.Send(ctx, store.SendParams{To: "auto-web/bugs", From: "auto-stack/reviewer"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	won, err := st.Ack(ctx, store.AckParams{MailID: sent.ID, Caller: caller("/workspace/b")})
	if err != nil {
		t.Fatalf("first ack: %v", err)
	}
	if !won.Won {
		t.Error("the first ack reported that it did not win")
	}
	if won.AckedBy != sub.SubscriptionID {
		t.Errorf("ackedBy = %q, want %q", won.AckedBy, sub.SubscriptionID)
	}

	lost, err := st.Ack(ctx, store.AckParams{MailID: sent.ID, Caller: caller("/workspace/b")})
	if err != nil {
		t.Fatalf("second ack: %v", err)
	}
	if lost.Won {
		t.Error("the second ack reported that it won; a delivery transitions exactly once")
	}
	if lost.AckedBy != sub.SubscriptionID || lost.AckedAt.IsZero() {
		t.Errorf("the loser was not told who won it: %+v", lost)
	}
	if got := countEvents(t, st, store.TypeAcked); got != 1 {
		t.Errorf("%s events = %d after two acks, want exactly 1", store.TypeAcked, got)
	}
}

// TestAckDistinguishesUnknownFromUndelivered: two different mistakes, two
// different errors — a caller can tell "no such mail" from "not yours".
func TestAckDistinguishesUnknownFromUndelivered(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	if _, err := st.Ack(ctx, store.AckParams{MailID: "01NOPE", Caller: caller("/workspace/b")}); !errors.Is(err, store.ErrUnknownMail) {
		t.Errorf("ack of an unknown id = %v, want ErrUnknownMail", err)
	}

	sent, err := st.Send(ctx, store.SendParams{To: "auto-web/bugs", From: "auto-stack/reviewer"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := st.Ack(ctx, store.AckParams{MailID: sent.ID, Caller: caller("/workspace/b")}); !errors.Is(err, store.ErrNoDelivery) {
		t.Errorf("ack of a mail the caller holds no delivery for = %v, want ErrNoDelivery", err)
	}
}

// TestResubscribeReturnsTheSameSubscription: one agent subscribing twice must
// not get a second copy of every mail. Two *different* agents do — that is
// TestBroadcastFanOut.
func TestResubscribeReturnsTheSameSubscription(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	first, err := st.Subscribe(ctx, store.SubscribeParams{Address: "auto-web/bugs", Caller: caller("/workspace/b")})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	second, err := st.Subscribe(ctx, store.SubscribeParams{Address: "auto-web/bugs", Caller: caller("/workspace/b")})
	if err != nil {
		t.Fatalf("re-subscribe: %v", err)
	}
	if first.SubscriptionID != second.SubscriptionID {
		t.Errorf("re-subscribe minted %q over %q", second.SubscriptionID, first.SubscriptionID)
	}
	if got := countEvents(t, st, store.TypeSubscribed); got != 1 {
		t.Errorf("%s events = %d after two subscribes, want 1 — a re-subscribe changes no state",
			store.TypeSubscribed, got)
	}
}

// TestSubscriptionIDsSurviveASameMillisecondBurst pins the collision the
// canonical `sub_01K9X2M4` form has by construction: eight characters carry only
// the top 40 bits of the timestamp, so subscriptions created inside one ~256ms
// window mint the identical short id. Two agents subscribing back to back is
// ordinary here, so the store lengthens the prefix rather than trusting it.
func TestSubscriptionIDsSurviveASameMillisecondBurst(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	seen := map[string]bool{}
	for i := range 8 {
		out, err := st.Subscribe(ctx, store.SubscribeParams{
			Address: fmt.Sprintf("auto-web/bugs-%d", i),
			Caller:  caller(fmt.Sprintf("/workspace/agent-%d", i)),
		})
		if err != nil {
			t.Fatalf("subscribe #%d: %v", i, err)
		}
		if seen[out.SubscriptionID] {
			t.Fatalf("subscription id %q was minted twice", out.SubscriptionID)
		}
		seen[out.SubscriptionID] = true
	}
}

// TestEveryEnvelopeCarriesAVersion (AC-11): the store is alpha and has no
// upcasters, so a stored shape must at least say which shape it is.
func TestEveryEnvelopeCarriesAVersion(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	sent, err := st.Send(ctx, store.SendParams{To: "auto-web/bugs", From: "auto-stack/reviewer"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	var envelope string
	if err := st.QueryRowContext(ctx, `SELECT envelope FROM mail WHERE id = ?`, sent.ID).Scan(&envelope); err != nil {
		t.Fatalf("read envelope: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(envelope), &decoded); err != nil {
		t.Fatalf("envelope is not JSON: %v", err)
	}
	if _, ok := decoded["version"]; !ok {
		t.Errorf("envelope %s carries no version integer", envelope)
	}

	var version int
	if err := st.QueryRowContext(ctx, `SELECT version FROM events WHERE type = ?`, store.TypeSent).Scan(&version); err != nil {
		t.Fatalf("read event version: %v", err)
	}
	if version != store.EventVersion {
		t.Errorf("event version = %d, want %d", version, store.EventVersion)
	}
}

// TestSubscriptionsAndBoundDiverge is AC-9's second half, and the reason a
// sender is handed two numbers instead of one: subscriptions counts durable
// readers of an address, bound counts those with a binding row. They answer
// different questions — "will anyone ever read this" versus "is anyone holding
// it right now" — and a sender that conflated them would read a liveness
// promise into a count that in T1 makes none (D-062-2).
//
// The unbound subscription is made by dropping the binding rather than by
// inserting a subscription by hand, because that is the shape T3 will produce:
// a reader whose pane is gone but whose subscription is still durable. Through
// the Client seam every subscription is bound at creation, which is why this
// case lives here rather than in the conformance suite.
func TestSubscriptionsAndBoundDiverge(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	sub, err := st.Subscribe(ctx, store.SubscribeParams{Address: "auto-web/bugs", Caller: caller("/workspace/b")})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := st.WithTx(ctx, func(tx *sql.Tx) error {
		_, execErr := tx.ExecContext(ctx, `DELETE FROM bindings WHERE subscription_id = ?`, sub.SubscriptionID)
		return execErr
	}); err != nil {
		t.Fatalf("drop the binding: %v", err)
	}

	sent, err := st.Send(ctx, store.SendParams{To: "auto-web/bugs", From: "auto-stack/reviewer"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sent.Subscriptions != 1 || sent.Bound != 0 {
		t.Errorf("send reported subscriptions=%d bound=%d, want 1 and 0 — the counts are distinct",
			sent.Subscriptions, sent.Bound)
	}
	if len(sent.Delivered) != 0 {
		t.Errorf("send raised a flag for %d bindings, want 0 — there is nowhere to raise one", len(sent.Delivered))
	}

	// Delivery does not depend on the binding: the subscription is a durable
	// reader, so the mail is waiting for whoever rebinds to it.
	//
	// "Waiting" is a claim about the cursor, not about a row. This assertion
	// used to demand a delivery row here, which was asserting the bug D-10
	// forbids — `send` had materialised one eagerly. What has to be true is
	// that the subscription's cursor admits the mail and the read path
	// projects it, which is what reading by subscription id shows.
	if rows := countRows(t, st, `SELECT COUNT(*) FROM deliveries`); rows != 0 {
		t.Errorf("send materialised %d delivery rows; D-10 puts every one of them on the read path", rows)
	}
	waiting, err := st.List(ctx, store.ListParams{SubscriptionID: sub.SubscriptionID})
	if err != nil {
		t.Fatalf("list the unbound subscription: %v", err)
	}
	if len(waiting) != 1 || waiting[0].ID != sent.ID {
		t.Errorf("an unbound subscription holds %+v, want %s — it is still a durable reader",
			waiting, sent.ID)
	}

	rebound, err := st.Subscribe(ctx, store.SubscribeParams{Address: "auto-web/bugs", Caller: caller("/workspace/b2")})
	if err != nil {
		t.Fatalf("resubscribe: %v", err)
	}
	if rebound.SubscriptionID == sub.SubscriptionID {
		t.Fatalf("a caller with no binding row reclaimed subscription %s", sub.SubscriptionID)
	}
	after, err := st.Send(ctx, store.SendParams{To: "auto-web/bugs", From: "auto-stack/reviewer"})
	if err != nil {
		t.Fatalf("second send: %v", err)
	}
	if after.Subscriptions != 2 || after.Bound != 1 {
		t.Errorf("send reported subscriptions=%d bound=%d, want 2 and 1", after.Subscriptions, after.Bound)
	}
}

// countRows answers a one-number question about the store's own tables. It is
// how a test asserts that something was *not* written — the absence of a
// projection has no seam-level observation, so it has to be read directly.
func countRows(t *testing.T, st *store.Store, query string, args ...any) int {
	t.Helper()
	var count int
	if err := st.QueryRowContext(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count rows (%s): %v", query, err)
	}
	return count
}

func countEvents(t *testing.T, st *store.Store, typ string) int {
	t.Helper()
	var count int
	if err := st.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM events WHERE type = ?`, typ).Scan(&count); err != nil {
		t.Fatalf("count %s events: %v", typ, err)
	}
	return count
}

// rowSnapshot is every column of every row of one table, rendered as text and
// ordered deterministically. It is the shape an immutability assertion needs:
// "the row did not change" is a claim about every column, and naming them
// individually would let a new column silently escape the check.
func rowSnapshot(t *testing.T, st *store.Store, table, orderBy string) []string {
	t.Helper()
	rows, err := st.QueryContext(context.Background(),
		fmt.Sprintf(`SELECT * FROM %s ORDER BY %s`, table, orderBy))
	if err != nil {
		t.Fatalf("snapshot %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns of %s: %v", table, err)
	}
	var out []string
	for rows.Next() {
		cells := make([]any, len(columns))
		for i := range cells {
			cells[i] = new(sql.NullString)
		}
		if err := rows.Scan(cells...); err != nil {
			t.Fatalf("scan %s: %v", table, err)
		}
		var b strings.Builder
		for i, name := range columns {
			cell := cells[i].(*sql.NullString)
			value := "<null>"
			if cell.Valid {
				value = cell.String
			}
			fmt.Fprintf(&b, "%s=%q ", name, value)
		}
		out = append(out, b.String())
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	return out
}

// TestListAndAckLeaveMailAndEventsByteIdentical is AC-3 in full: a whole
// list + ack cycle must leave every `mail` row and every previously-appended
// `events` row byte identical, and must record its state change as a **new**
// appended event rather than a rewrite of an old one (G1).
//
// The rows are compared column by column rather than by a spot check, so a
// column added later is covered without anybody remembering to add it here.
func TestListAndAckLeaveMailAndEventsByteIdentical(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	c := caller("/workspace/b")

	if _, err := st.Subscribe(ctx, store.SubscribeParams{Address: "auto-web/bugs", Caller: c}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	sent, err := st.Send(ctx, store.SendParams{To: "auto-web/bugs", From: "auto-stack/reviewer"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	mailBefore := rowSnapshot(t, st, "mail", "id")
	eventsBefore := rowSnapshot(t, st, "events", "seq")
	if len(mailBefore) != 1 {
		t.Fatalf("mail rows before = %d, want 1", len(mailBefore))
	}
	if len(eventsBefore) != 2 {
		t.Fatalf("events before = %d, want 2 (subscribed + sent)", len(eventsBefore))
	}

	if _, err := st.List(ctx, store.ListParams{Caller: c}); err != nil {
		t.Fatalf("list: %v", err)
	}
	acked, err := st.Ack(ctx, store.AckParams{MailID: sent.ID, Caller: c})
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	if !acked.Won {
		t.Fatal("the only ack did not win the transition")
	}

	if got := rowSnapshot(t, st, "mail", "id"); !slices.Equal(got, mailBefore) {
		t.Errorf("the mail row changed across list+ack:\nbefore %v\nafter  %v", mailBefore, got)
	}
	eventsAfter := rowSnapshot(t, st, "events", "seq")
	if len(eventsAfter) < len(eventsBefore) {
		t.Fatalf("events shrank from %d to %d; the log is append-only", len(eventsBefore), len(eventsAfter))
	}
	if !slices.Equal(eventsAfter[:len(eventsBefore)], eventsBefore) {
		t.Errorf("an existing event row changed across list+ack:\nbefore %v\nafter  %v", eventsBefore, eventsAfter[:len(eventsBefore)])
	}

	// The state change is a new row, and it is the ack.
	if got := countEvents(t, st, store.TypeAcked); got != 1 {
		t.Errorf("%s events = %d, want exactly 1 appended by the ack", store.TypeAcked, got)
	}
	appended := eventsAfter[len(eventsBefore):]
	if len(appended) == 0 {
		t.Fatal("list+ack appended no events at all; the ack must be recorded as a new event")
	}
	if !strings.Contains(appended[len(appended)-1], store.TypeAcked) {
		t.Errorf("the last appended event is %q, want an %s row", appended[len(appended)-1], store.TypeAcked)
	}
}

// TestAckTransitionsExactlyOnceUnderConcurrency is AC-6's concurrency half (G13): sixteen callers race for
// one delivery, exactly one wins the transition, exactly one alpha.mail.acked
// event exists afterwards, and nobody sees an error — which at the seam is
// what `acked: true` is, since AckResult sets it whenever Ack returns nil.
//
// Run under `-race` (auto-mail is in the Makefile's RACE_PROJECTS), so this
// asserts the Go-level discipline as well as the SQL-level one.
func TestAckTransitionsExactlyOnceUnderConcurrency(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	c := caller("/workspace/b")

	sub, err := st.Subscribe(ctx, store.SubscribeParams{Address: "auto-web/bugs", Caller: c})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	sent, err := st.Send(ctx, store.SendParams{To: "auto-web/bugs", From: "auto-stack/reviewer"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	const racers = 16
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		wins     int
		failures []error
	)
	start := make(chan struct{})
	for range racers {
		wg.Go(func() {
			<-start
			out, err := st.Ack(ctx, store.AckParams{MailID: sent.ID, Caller: c})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, err)
				return
			}
			if out.Won {
				wins++
			}
		})
	}
	close(start)
	wg.Wait()

	if len(failures) > 0 {
		t.Fatalf("%d of %d acks failed; every caller must see the delivery acked: %v",
			len(failures), racers, failures)
	}
	if wins != 1 {
		t.Errorf("%d of %d acks won the transition, want exactly 1", wins, racers)
	}
	if got := countEvents(t, st, store.TypeAcked); got != 1 {
		t.Errorf("%s events = %d after %d concurrent acks, want exactly 1", store.TypeAcked, got, racers)
	}

	var ackedAt, ackedBy sql.NullString
	if err := st.QueryRowContext(ctx,
		`SELECT acked_at, acked_by FROM deliveries WHERE subscription_id = ? AND mail_id = ?`,
		sub.SubscriptionID, sent.ID).Scan(&ackedAt, &ackedBy); err != nil {
		t.Fatalf("read the delivery back: %v", err)
	}
	if !ackedAt.Valid || ackedBy.String != sub.SubscriptionID {
		t.Errorf("delivery acked_at=%v acked_by=%v, want it acked by %s", ackedAt, ackedBy, sub.SubscriptionID)
	}
}

// TestConcurrentWritersLoseNoMail is the other half of D-11's concurrency
// concern: several *separate store handles* on one file — what several
// processes are, since each `auto mail send` opens its own — writing to one
// address at once. WAL plus busy_timeout plus one immediate transaction per
// write is the discipline being asserted, so the handles are deliberately not
// shared: a single *sql.DB would serialise this in Go and prove nothing about
// SQLite's locking.
func TestConcurrentWritersLoseNoMail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail", "alpha-store.db")
	ctx := context.Background()

	seed, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := seed.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	reader := caller("/workspace/b")
	sub, err := seed.Subscribe(ctx, store.SubscribeParams{Address: "auto-web/bugs", Caller: reader})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close the seed handle: %v", err)
	}

	const writers = 8
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		ids      []string
		failures []error
	)
	start := make(chan struct{})
	for i := range writers {
		wg.Go(func() {
			st, err := store.Open(path)
			if err != nil {
				mu.Lock()
				failures = append(failures, err)
				mu.Unlock()
				return
			}
			defer func() { _ = st.Close() }()
			<-start
			out, err := st.Send(ctx, store.SendParams{
				To:   "auto-web/bugs",
				From: "auto-stack/reviewer",
				Body: map[string]any{"message": fmt.Sprintf("writer %d", i)},
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, err)
				return
			}
			ids = append(ids, out.ID)
		})
	}
	close(start)
	wg.Wait()

	if len(failures) > 0 {
		t.Fatalf("%d of %d concurrent sends failed: %v", len(failures), writers, failures)
	}
	if len(ids) != writers {
		t.Fatalf("%d ids returned, want %d", len(ids), writers)
	}

	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Nothing was lost: one event, one mail row and (after a list materialises
	// them) one delivery row per send.
	if got := countEvents(t, st, store.TypeSent); got != writers {
		t.Errorf("%s events = %d, want %d — a concurrent writer's event was lost",
			store.TypeSent, got, writers)
	}
	listed, err := st.List(ctx, store.ListParams{Caller: reader})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != writers {
		t.Fatalf("list returned %d deliveries, want %d", len(listed), writers)
	}
	seen := map[string]bool{}
	for _, item := range listed {
		seen[item.ID] = true
	}
	for _, id := range ids {
		if !seen[id] {
			t.Errorf("mail %s was sent but never listed", id)
		}
	}

	// And the projection agrees with the log: one delivery row per mail, for
	// the one subscription, none of them acked.
	var deliveries int
	if err := st.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM deliveries WHERE subscription_id = ? AND acked_at IS NULL`,
		sub.SubscriptionID).Scan(&deliveries); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	if deliveries != writers {
		t.Errorf("unacked deliveries = %d, want %d", deliveries, writers)
	}
}

// TestSendMaterialisesNoDeliveries is D-10's writer half, and the regression
// rail for a `send` that used to insert a delivery row per subscription.
//
// The eager insert was a second path to rows the read path already owned, and
// two paths to one projection can disagree; it also made a send pay
// O(subscriptions) for readers that may never read, and put half the reader
// projection inside the writer. The decision that settles it is the same one
// that makes a late subscriber able to receive earlier mail (J2): if delivery
// is decided at send, a subscription created afterwards has already missed it.
func TestSendMaterialisesNoDeliveries(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	if _, err := st.Subscribe(ctx, store.SubscribeParams{
		Address: "auto-web/bugs", Caller: caller("/workspace/reader"),
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	sent, err := st.Send(ctx, store.SendParams{To: "auto-web/bugs", From: "auto-stack/reviewer"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if rows := countRows(t, st, `SELECT COUNT(*) FROM deliveries`); rows != 0 {
		t.Errorf("send wrote %d delivery rows; D-10 puts every one of them on the read path", rows)
	}
	// The mail is nonetheless waiting: the cursor admits it, which is what the
	// sender's own pending count reports and what the next read projects.
	pending, err := st.PendingCount(ctx, caller("/workspace/reader"))
	if err != nil {
		t.Fatalf("pending count: %v", err)
	}
	if pending != 1 {
		t.Errorf("pending = %d before any read, want 1 — a mail with no delivery row yet is "+
			"still waiting, and a flag cleared on that answer would drop a real nudge", pending)
	}

	listed, err := st.List(ctx, store.ListParams{Caller: caller("/workspace/reader")})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != sent.ID {
		t.Fatalf("list returned %+v, want %s", listed, sent.ID)
	}
	if rows := countRows(t, st, `SELECT COUNT(*) FROM deliveries`); rows != 1 {
		t.Errorf("deliveries = %d after the read, want 1 — the read path is what materialises them", rows)
	}
}

// TestFromAddressLadderFollowsTheLogNotTheID covers the same class of defect as
// the cursor, on rung 2 of the sender's from-address ladder.
//
// The rung answers "the first subscription this agent created", and it used to
// answer it with `ORDER BY s.id`. A subscription id is `sub_` plus a ULID
// prefix — timestamp bits, then entropy — so between two processes that is not
// a creation order at all. The row inserted by hand here is that case made
// concrete: a subscription the store recorded *second* whose id sorts *first*,
// which is what another process minting in the same window produces.
func TestFromAddressLadderFollowsTheLogNotTheID(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	c := caller("/workspace/sender")

	if _, err := st.Subscribe(ctx, store.SubscribeParams{Address: "auto-web/first", Caller: c}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// Recorded after it — a strictly greater seq — but carrying an id that
	// sorts below every id this store can mint.
	if err := st.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO subscriptions (id, seq, address, name, from_cursor, created_at)
			VALUES ('sub_00000000', (SELECT MAX(seq) + 1 FROM subscriptions), 'auto-web/later', NULL, 0,
			        (SELECT created_at FROM subscriptions ORDER BY seq LIMIT 1))`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO bindings (subscription_id, manager, target, session, last_seen)
			VALUES ('sub_00000000', ?, ?, NULL, (SELECT last_seen FROM bindings LIMIT 1))`,
			c.Manager, c.Target)
		return err
	}); err != nil {
		t.Fatalf("insert the later subscription: %v", err)
	}

	address, ok, err := st.AddressForBinding(ctx, c)
	if err != nil {
		t.Fatalf("resolve from-address: %v", err)
	}
	if !ok || address != "auto-web/first" {
		t.Errorf("the from-address ladder resolved %q (found=%v), want auto-web/first — "+
			"\"first created\" is the log's order, not a comparison of two minted ids", address, ok)
	}
}

// ── the cross-process cursor ────────────────────────────────────────────────

// Environment variables the child process below is driven by. They exist
// because the defect these tests cover cannot be reproduced inside one process:
// the ULID generator is monotonic through package-level state, so two ids minted
// in one process always sort in generation order. It is exactly that guarantee
// which does *not* cross a process boundary, and D-11 makes every `auto mail`
// verb its own process against one SQLite file.
const (
	childStoreEnv   = "AUTO_MAIL_TEST_CHILD_STORE"
	childAddressEnv = "AUTO_MAIL_TEST_CHILD_ADDRESS"
	childSendAtEnv  = "AUTO_MAIL_TEST_CHILD_SEND_AT"
)

// TestChildSend is not a test. It is the body of the child process the
// cross-process cases spawn — this binary re-executed with -test.run — and it
// skips itself whenever it is reached as an ordinary test.
//
// It matters that this is a real second process rather than a goroutine: the
// whole point is that the child's ULID entropy is drawn independently of the
// parent's, which is the condition a same-process test can never create.
func TestChildSend(t *testing.T) {
	path := os.Getenv(childStoreEnv)
	if path == "" {
		t.Skip("this is the child-process body of the cross-process cursor cases")
	}
	at, err := time.Parse(time.RFC3339Nano, os.Getenv(childSendAtEnv))
	if err != nil {
		t.Fatalf("child: parse %s: %v", childSendAtEnv, err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("child: open %s: %v", path, err)
	}
	defer func() { _ = st.Close() }()

	out, err := st.Send(context.Background(), store.SendParams{
		To:   os.Getenv(childAddressEnv),
		From: "auto-stack/child",
		Body: map[string]any{"message": "sent by another process"},
		Now:  at,
	})
	if err != nil {
		t.Fatalf("child: send: %v", err)
	}
	fmt.Printf("MAIL_ID=%s\n", out.ID)
}

// sendFromAnotherProcess re-executes this test binary as a child, has it post
// one mail to the same store file at the given instant, and returns the id it
// minted. It returns an error rather than failing the test, because leg 2 calls
// it from goroutines, where t.Fatalf is not usable.
func sendFromAnotherProcess(storePath, address string, at time.Time) (string, error) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestChildSend$", "-test.v")
	cmd.Env = append(os.Environ(),
		childStoreEnv+"="+storePath,
		childAddressEnv+"="+address,
		childSendAtEnv+"="+at.Format(time.RFC3339Nano),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("child send: %w\n%s", err, out)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if id, ok := strings.CutPrefix(strings.TrimSpace(line), "MAIL_ID="); ok {
			return id, nil
		}
	}
	return "", fmt.Errorf("child send printed no mail id:\n%s", out)
}

// mustSendFromAnotherProcess is sendFromAnotherProcess on the test goroutine.
func mustSendFromAnotherProcess(t *testing.T, storePath, address string, at time.Time) string {
	t.Helper()
	id, err := sendFromAnotherProcess(storePath, address, at)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return id
}

// TestFromNowCursorIsDecidedByTheStoreNotByAnID is the regression rail for the
// defect this cursor was rebuilt to close.
//
// A --from-now cursor used to be `ulid.NewAt(now)` — an id minted in the
// subscribing process — and admission was the string comparison
// `mail.id > from_cursor`. That is a claim about ULID ordering *between
// processes*, and ULID makes no such promise: the timestamp prefix is shared,
// the 80-bit tail is drawn independently, and two ids minted in the same
// millisecond by two processes sort either way. A mail that landed below the
// cursor was admitted by nothing — no delivery row, no pending count, no list
// entry — silently and permanently.
//
// D-11 makes that the ordinary configuration, not an exotic one: `send`,
// `list` and `ack` are each their own process against one file. So both legs
// here drive a real second process, and both assert the same invariant —
// **every mail committed after a --from-now subscribe is admitted by it**,
// whatever id the sending process happened to draw.
func TestFromNowCursorIsDecidedByTheStoreNotByAnID(t *testing.T) {
	const address = "auto-web/bugs"

	// Leg 1: the two processes disagree about the clock. This is the
	// deterministic case — the child's id carries a strictly smaller timestamp
	// prefix, so under the old cursor it sorted below and was lost every time,
	// with no dependence on entropy at all.
	t.Run("a-sender-whose-clock-trails-the-subscriber", func(t *testing.T) {
		st := open(t)
		ctx := context.Background()
		// A real clock, so the subscribing process's own id generator is at
		// this instant too. The child is handed an instant 5ms behind it, which
		// is a clock the two processes disagree about — ordinary between two
		// hosts, and reachable on one whenever a caller stamps its own time.
		at := time.Now().UTC()

		if _, err := st.Subscribe(ctx, store.SubscribeParams{
			Address: address, FromNow: true, Caller: caller("/workspace/reader"), Now: at,
		}); err != nil {
			t.Fatalf("subscribe --from-now: %v", err)
		}
		sent := mustSendFromAnotherProcess(t, st.Path(), address, at.Add(-5*time.Millisecond))

		listed, err := st.List(ctx, store.ListParams{Caller: caller("/workspace/reader")})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(listed) != 1 || listed[0].ID != sent {
			t.Fatalf("a --from-now subscriber holds %+v, want the mail %s another process "+
				"committed after it subscribed; \"from now\" is a position in the log, "+
				"not a comparison of two minted ids", listed, sent)
		}
	})

	// Leg 2: the two processes agree about the clock, to the millisecond. This
	// is the mechanism in its purest form — one timestamp prefix, sixteen
	// independently drawn 80-bit tails — and it is deliberately *not* the leg
	// the regression rests on. Against a cursor drawn from the same
	// distribution, all sixteen ids land above it about one run in seventeen,
	// so as a pre-fix failure it is likely, not certain; leg 1 is the one that
	// fails deterministically. What this leg adds is the real shape: sixteen
	// concurrent processes writing one SQLite file, asserting that every mail
	// committed after the subscribe arrives, with no coin flip left in the
	// answer.
	t.Run("senders-in-the-same-millisecond", func(t *testing.T) {
		const senders = 16
		st := open(t)
		ctx := context.Background()
		at := time.Now().UTC()

		if _, err := st.Subscribe(ctx, store.SubscribeParams{
			Address: address, FromNow: true, Caller: caller("/workspace/reader"), Now: at,
		}); err != nil {
			t.Fatalf("subscribe --from-now: %v", err)
		}

		var (
			wg   sync.WaitGroup
			each = make([]string, senders)
			errs = make([]error, senders)
		)
		for i := range senders {
			wg.Go(func() {
				each[i], errs[i] = sendFromAnotherProcess(st.Path(), address, at)
			})
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("%v", err)
			}
		}

		// How much of the adversarial ordering this run exercised, measured
		// between the children themselves so it assumes nothing about this
		// process's own id generator. One of their ids stands in for the cursor
		// the old code would have held; the count below it is how many mails
		// that cursor would have lost. It swings widely run to run because the
		// stand-in is itself a random draw, which is exactly why it is logged
		// and not asserted.
		below := 0
		for _, id := range each[1:] {
			if id < each[0] {
				below++
			}
		}
		t.Logf("%d of %d ids minted by other processes in this millisecond sort below "+
			"another process's id from the same millisecond; a --from-now cursor holding "+
			"that id would have lost every one of them, permanently",
			below, senders-1)

		listed, err := st.List(ctx, store.ListParams{Caller: caller("/workspace/reader")})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		seen := map[string]bool{}
		for _, item := range listed {
			seen[item.ID] = true
		}
		for _, id := range each {
			if !seen[id] {
				t.Errorf("mail %s was committed after the --from-now subscribe but is not "+
					"admitted by its cursor", id)
			}
		}
		if len(listed) != senders {
			t.Errorf("list returned %d mail, want %d", len(listed), senders)
		}
	})
}
