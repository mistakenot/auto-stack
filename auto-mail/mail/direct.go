package mail

import (
	"context"
	"fmt"

	"github.com/mistakenot/auto-mail/internal/config"
	"github.com/mistakenot/auto-mail/internal/store"
	sharedconfig "github.com/mistakenot/auto-shared/config"
)

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
	st, err := store.Open(config.StorePathIn(home))
	if err != nil {
		return nil, err
	}
	if err := st.Migrate(context.Background()); err != nil {
		_ = st.Close()
		return nil, err
	}
	return &direct{home: home, store: st}, nil
}

// StorePath is the store file this client is bound to.
func (d *direct) StorePath() string { return d.store.Path() }

func (d *direct) Subscribe(_ context.Context, in SubscribeInput) (SubscribeResult, error) {
	return SubscribeResult{}, fmt.Errorf("subscribe %q: %w", in.Address, ErrNotImplemented)
}

func (d *direct) Send(_ context.Context, in SendInput) (SendResult, error) {
	return SendResult{}, fmt.Errorf("send to %q: %w", in.To, ErrNotImplemented)
}

// List is the one verb the walking skeleton implements end to end. Against a
// freshly migrated store there is nothing to materialise, so it returns an
// empty (never nil) slice — `auto mail list` prints `[]`.
func (d *direct) List(ctx context.Context, in ListInput) ([]Delivery, error) {
	rows, err := d.store.QueryContext(ctx, `
		SELECT m.id, m.envelope, m.body, m.sent_at
		FROM deliveries d
		JOIN mail m ON m.id = d.mail_id
		WHERE d.acked_at IS NULL
		ORDER BY m.id`)
	if err != nil {
		return nil, fmt.Errorf("list mail: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		// Nothing writes `deliveries` until the projection lands in the next
		// phase, so a row here means the caller is ahead of the implementation
		// — say so rather than returning a silently wrong empty list.
		return nil, fmt.Errorf("list mail: %w", ErrNotImplemented)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list mail: %w", err)
	}
	return make([]Delivery, 0), nil
}

func (d *direct) Ack(_ context.Context, in AckInput) (AckResult, error) {
	return AckResult{}, fmt.Errorf("ack %q: %w", in.MailID, ErrNotImplemented)
}

func (d *direct) Reset(_ context.Context) (ResetResult, error) {
	return ResetResult{}, fmt.Errorf("reset: %w", ErrNotImplemented)
}

func (d *direct) Close() error { return d.store.Close() }
