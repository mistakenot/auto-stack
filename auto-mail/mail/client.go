// Package mail is auto-mail's only exported domain API. The CLI, the hook and
// every future auto-* consumer go through this seam; the store itself lives
// under auto-mail/internal and is unimportable from any other module, which
// makes G11 a compile-time property rather than a convention (D-062-8).
//
// Delivery contract: at-least-once and unordered. Consumers must be idempotent
// on the mail id.
package mail

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors callers are expected to branch on.
var (
	// ErrNotImplemented marks a seam method whose implementation lands in a
	// later phase of this task. It exists so the interface can be complete
	// before every verb is.
	ErrNotImplemented = errors.New("not implemented in this phase")
	// ErrUnknownMail is returned when a mail id is not in the store.
	ErrUnknownMail = errors.New("unknown mail id")
	// ErrNoDelivery is returned when the caller holds no delivery row for an
	// otherwise-known mail id.
	ErrNoDelivery = errors.New("no delivery for this caller")
	// ErrInvalidAddress is returned when an address fails validation.
	ErrInvalidAddress = errors.New("invalid address")
)

// Binding is the opaque physical pair a Subscription is currently held by
// (G12). It never appears in an Address, nor in a stored envelope's to/from
// (G5) — it lives only on the bindings row.
type Binding struct {
	Manager string
	Target  string
	Session string
}

// SubscribeInput asks for a durable reader of an address.
type SubscribeInput struct {
	// Address is the virtual, free-form name to read.
	Address string
	// Name is an optional human label for the subscription.
	Name string
	// FromNow starts the cursor at the current high-water mark instead of the
	// beginning, so existing mail is not backfilled (D-10).
	FromNow bool
	// Binding is the caller's physical context, recorded once at subscribe.
	Binding Binding
}

// SubscribeResult is the payload `auto mail subscribe` prints.
type SubscribeResult struct {
	Address      string `json:"address"`
	Subscription string `json:"subscription"`
	Backfilled   int    `json:"backfilled"`
}

// SendInput posts one mail to an address.
type SendInput struct {
	// To is the destination address.
	To string
	// From is the sender's resolved absolute address. Empty means the client
	// resolves it on the documented ladder.
	From string
	// Body is a JSON object. `--message <text>` is sugar for {"message": text}.
	Body map[string]any
	// Binding is the caller's physical context, used to resolve From.
	Binding Binding
	// Cwd is the caller's working directory, used only by the last rung of the
	// From ladder to name the project. It is not a Binding: the binding's
	// target is a pane whenever one exists, and the project lookup needs the
	// directory itself.
	Cwd string
}

// SendResult is the payload `auto mail send` prints. subscriptions counts
// durable readers of the address; bound counts those with a binding row. The
// two are deliberately distinct and mean different things to a sender (G6).
type SendResult struct {
	ID            string `json:"id"`
	To            string `json:"to"`
	Subscriptions int    `json:"subscriptions"`
	Bound         int    `json:"bound"`
}

// ListInput scopes a read. With no filters it returns every unacked delivery
// for every subscription the caller is bound to.
type ListInput struct {
	Address      string
	Subscription string
	Binding      Binding
}

// Delivery is one unacked mail as a reader sees it.
type Delivery struct {
	ID     string         `json:"id"`
	From   string         `json:"from"`
	SentAt time.Time      `json:"sentAt"`
	Body   map[string]any `json:"body"`
}

// AckInput retires one delivery. Ack is always a separate explicit call —
// reading never retires mail (G3).
type AckInput struct {
	MailID  string
	Binding Binding
}

// AckResult reports the outcome. Acked means "this delivery is acked now";
// WonTransition answers "was it this call that transitioned it" (G13).
type AckResult struct {
	ID            string `json:"id"`
	Acked         bool   `json:"acked"`
	WonTransition bool   `json:"wonTransition"`
	// AckedAt and AckedBy describe the winning transition, so a loser can be
	// told who won it on stderr (D-062-7). Empty on a win.
	AckedAt time.Time `json:"-"`
	AckedBy string    `json:"-"`
}

// ResetResult reports what a reset removed. Wiping the store is a supported
// operation because this is alpha (G10).
type ResetResult struct {
	Removed []string `json:"removed"`
}

// Client is the only API into mail. T1 ships the direct-store implementation
// (D-11), so send/list/ack work with no daemon running; T3's RPC
// implementation becomes a second target for the same conformance suite.
type Client interface {
	// Subscribe creates or returns the subscription for an address, records
	// the caller's binding, and reports how many unacked mail items the cursor
	// backfilled.
	Subscribe(ctx context.Context, in SubscribeInput) (SubscribeResult, error)
	// Send appends alpha.mail.sent, inserts the immutable mail row and a
	// delivery row per bound subscription, and sets each pending flag.
	Send(ctx context.Context, in SendInput) (SendResult, error)
	// List materialises delivery rows lazily from each subscription's cursor
	// (D-10), stamps first read, and returns unacked mail. It never retires
	// anything (G3).
	List(ctx context.Context, in ListInput) ([]Delivery, error)
	// Ack is a compare-and-set on acked_at inside the append-and-project
	// transaction; it reports whether this call won the transition (G13).
	Ack(ctx context.Context, in AckInput) (AckResult, error)
	// Reset removes the store and the flag directory.
	Reset(ctx context.Context) (ResetResult, error)
	// Close releases the store handle.
	Close() error
}
