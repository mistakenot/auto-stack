package mail

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/mistakenot/auto-mail/internal/config"
	"github.com/mistakenot/auto-mail/internal/store"
	sharedconfig "github.com/mistakenot/auto-shared/config"
)

// UnregisteredProject is the project component of a sender's from-address when
// the caller's working directory is not a registered project. It is a literal
// rather than an error: mail must work from anywhere, and a resolved address
// that says "I could not name this project" is more useful than a refusal.
const UnregisteredProject = "unregistered"

// AgentHandle is the agent component of the fallback from-address. T1 has no
// self-identification — an `auto mail send` process sees neither the hook
// payload's agent nor a session_id, and bridging that is what D-13 defers to
// T2 — so the fallback names the project and leaves the agent generic.
const AgentHandle = "agent"

// direct is the direct-store Client implementation D-11 binds the MVP CLI to:
// send/list/ack work with no daemon running, because a failing agent must be
// able to post mail with nothing else alive.
type direct struct {
	home  string
	store *store.Store
}

// NewDirect opens (creating if needed) the mail store under home and returns
// the direct-store client. An empty home resolves the user's home directory
// from the environment.
func NewDirect(home string) (Client, error) {
	if home == "" {
		resolved, err := sharedconfig.HomeDir()
		if err != nil {
			return nil, err
		}
		home = resolved
	}
	st, err := openStore(config.StorePathIn(home))
	if err != nil {
		return nil, err
	}
	if err := st.Migrate(context.Background()); err != nil {
		_ = st.Close()
		return nil, err
	}
	return &direct{home: home, store: st}, nil
}

// The T1 event vocabulary, re-exported at the seam. The log is the authority
// (D-1), so a caller that wants to assert on what actually happened — the
// conformance suite does — needs to name an event type without reaching past
// the seam into the store (G11). Every type carries the `alpha.` prefix G10
// requires.
const (
	// EventTypeSubscribed records a new durable reader of an address.
	EventTypeSubscribed = store.TypeSubscribed
	// EventTypeSent records one mail posted to an address.
	EventTypeSent = store.TypeSent
	// EventTypeRead records the first read of a delivery.
	EventTypeRead = store.TypeRead
	// EventTypeAcked records the one call that won a delivery's transition.
	EventTypeAcked = store.TypeAcked
)

// StorePath is the store file this client is bound to.
func (d *direct) StorePath() string { return d.store.Path() }

// CountEvents reports how many events of one type this client's log holds.
//
// It is not on Client: counting the log is an *observation* of an
// implementation, not part of the contract every implementation owes a caller,
// and T3's RPC client will answer it over the wire or not at all. The
// conformance suite treats it as an optional capability for exactly that
// reason — the log-level half of G4 is asserted where it can be, and skipped
// where it cannot, rather than being pushed into the seam to make one test
// convenient.
func (d *direct) CountEvents(ctx context.Context, eventType string) (int, error) {
	return d.store.CountEvents(ctx, eventType)
}

func (d *direct) Subscribe(ctx context.Context, in SubscribeInput) (SubscribeResult, error) {
	if err := ValidateAddress(in.Address); err != nil {
		return SubscribeResult{}, err
	}
	out, err := d.store.Subscribe(ctx, store.SubscribeParams{
		Address: in.Address,
		Name:    in.Name,
		FromNow: in.FromNow,
		Caller:  caller(in.Binding),
	})
	if err != nil {
		return SubscribeResult{}, fmt.Errorf("subscribe to %q: %w", in.Address, err)
	}
	return SubscribeResult{
		Address:      in.Address,
		Subscription: out.SubscriptionID,
		Backfilled:   out.Backfilled,
	}, nil
}

func (d *direct) Send(ctx context.Context, in SendInput) (SendResult, error) {
	if err := ValidateAddress(in.To); err != nil {
		return SendResult{}, err
	}
	from, err := d.resolveFrom(ctx, in)
	if err != nil {
		return SendResult{}, err
	}
	out, err := d.store.Send(ctx, store.SendParams{To: in.To, From: from, Body: in.Body})
	if err != nil {
		return SendResult{}, fmt.Errorf("send to %q: %w", in.To, err)
	}
	// The flag is raised after the commit, never inside it: it is a hint about
	// state, and a hint that outlived a rolled-back send would be drift with no
	// self-healing path. A flag that cannot be written is a missed nudge, not a
	// failed send — the mail is already durable, and the next send (or, from
	// T3, the reconcile tick) raises it again.
	for _, c := range out.Delivered {
		_ = setPending(d.home, Binding{Manager: c.Manager, Target: c.Target, Session: c.Session})
	}
	return SendResult{
		ID:            out.ID,
		To:            in.To,
		Subscriptions: out.Subscriptions,
		Bound:         out.Bound,
	}, nil
}

// settleFlag brings the caller's pending flag back in line with the store after
// a read or an ack: cleared once nothing is waiting, left alone otherwise.
//
// This is the self-healing half of D-062-3. A false-positive flag costs one
// wasted `auto mail list` — and that list is what removes it, so drift is
// bounded by a single wasted read rather than sticking until the next send.
// Failures are swallowed: an agent that has just read its mail must not be
// handed an error because a marker file would not unlink.
func (d *direct) settleFlag(ctx context.Context, b Binding) {
	pending, err := d.store.PendingCount(ctx, caller(b))
	if err != nil || pending > 0 {
		return
	}
	_ = clearPending(d.home, b)
}

// resolveFrom walks the three-rung ladder that gives a sender an absolute
// address to be replied to. The result is always resolved and absolute, never a
// relative handle — relative handles are T2's (G5/D-13).
//
//  1. An explicit --from wins.
//  2. Otherwise the address of a subscription bound to this caller, lowest
//     subscription id when there are several so the answer is deterministic.
//     This is C1's case: the sender subscribed to its own reply address first,
//     which is also what makes it reachable for a reply.
//  3. Otherwise `<projectId>/agent` from the project registry, or
//     `unregistered/agent` outside a registered project.
func (d *direct) resolveFrom(ctx context.Context, in SendInput) (string, error) {
	if in.From != "" {
		if err := ValidateAddress(in.From); err != nil {
			return "", err
		}
		return in.From, nil
	}
	if address, ok, err := d.store.AddressForBinding(ctx, caller(in.Binding)); err != nil {
		return "", err
	} else if ok {
		return address, nil
	}
	return d.projectAddress(in.Cwd), nil
}

// projectAddress is rung 3. A registry that cannot be read is treated exactly
// like an unregistered directory: `send` must not fail because the host's
// project list is missing or malformed.
func (d *direct) projectAddress(cwd string) string {
	project := UnregisteredProject
	if cwd != "" {
		registry, err := sharedconfig.LoadProjects(filepath.Join(d.home, ".auto", "projects.json"))
		if err == nil {
			if ref := registry.FindProjectByPath(resolveDir(cwd)); ref != nil && ref.ID != "" {
				project = ref.ID
			}
		}
	}
	return project + "/" + AgentHandle
}

// List materialises delivery rows lazily from each subscription's cursor,
// stamps the first read, and returns unacked mail. It never retires anything —
// ack is always a separate explicit call (G3).
func (d *direct) List(ctx context.Context, in ListInput) ([]Delivery, error) {
	if in.Address != "" {
		if err := ValidateAddress(in.Address); err != nil {
			return nil, err
		}
	}
	listed, err := d.store.List(ctx, store.ListParams{
		Caller:         caller(in.Binding),
		Address:        in.Address,
		SubscriptionID: in.Subscription,
	})
	if err != nil {
		return nil, fmt.Errorf("list mail: %w", err)
	}
	d.settleFlag(ctx, in.Binding)
	// Never nil: `auto mail list` must print `[]`, not `null`.
	out := make([]Delivery, 0, len(listed))
	for _, item := range listed {
		out = append(out, Delivery{
			ID:     item.ID,
			From:   item.From,
			SentAt: item.SentAt,
			Body:   item.Body,
		})
	}
	return out, nil
}

func (d *direct) Ack(ctx context.Context, in AckInput) (AckResult, error) {
	out, err := d.store.Ack(ctx, store.AckParams{MailID: in.MailID, Caller: caller(in.Binding)})
	switch {
	case errors.Is(err, store.ErrUnknownMail):
		return AckResult{}, fmt.Errorf("%w: %s", ErrUnknownMail, in.MailID)
	case errors.Is(err, store.ErrNoDelivery):
		return AckResult{}, fmt.Errorf("%w: %s", ErrNoDelivery, in.MailID)
	case err != nil:
		return AckResult{}, fmt.Errorf("ack %q: %w", in.MailID, err)
	}
	d.settleFlag(ctx, in.Binding)
	// Acked is true for winner and loser alike: it states that the delivery is
	// acked now, which is what a caller acts on. WonTransition answers the
	// separate question "was it me" (G13/D-062-7).
	return AckResult{
		ID:            in.MailID,
		Acked:         true,
		WonTransition: out.Won,
		AckedAt:       out.AckedAt,
		AckedBy:       out.AckedBy,
	}, nil
}

func (d *direct) Reset(_ context.Context) (ResetResult, error) {
	return ResetResult{}, fmt.Errorf("reset: %w", ErrNotImplemented)
}

func (d *direct) Close() error { return d.store.Close() }

// caller converts the public opaque pair into the store's. The conversion is
// the only place the two vocabularies meet, and it carries nothing but the
// pair — physical identity never travels further than the bindings row (G5).
func caller(b Binding) store.Caller {
	return store.Caller{Manager: b.Manager, Target: b.Target, Session: b.Session}
}
