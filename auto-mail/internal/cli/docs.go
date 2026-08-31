package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const docsMarkdown = `# auto mail — command reference

A durable, addressed, at-least-once channel between agents on one host. JSON on
stdout by default; diagnostics go to stderr.

Vocabulary (docs/concepts/UBIQUITOUS_LANGUAGE.md): a **Mail** is sent to an
**Address**, a **Subscription** is a durable reader of an Address, a
**Delivery** is one Subscription's copy of one Mail plus its read/ack state, and
a **Binding** is the opaque physical target a Subscription is currently held by.
Nothing here is called a message — that word already names a Session exchange.

## Delivery contract

**At-least-once, unordered. Consumers must be idempotent on the mail id.**

Nothing here promises exactly-once delivery or ordering. The same mail id may be
handed to you more than once, and ids may arrive in an order that is not the
order they were sent in — so act on the id, and make acting on it twice mean the
same thing as acting on it once.

Reading never retires mail: ` + "`ack`" + ` is always a separate explicit call.

## init

Create ~/.auto/mail/ and the alpha store, and establish host identity.

JSON fields: store, created, alpha.

## subscribe

    auto mail subscribe <address> [--name <label>] [--from-now]

Become a durable reader of an Address and record this caller's Binding. By
default the cursor starts at the beginning, so mail sent before you subscribed
is backfilled; ` + "`--from-now`" + ` starts it at the current high-water mark instead.

JSON fields: address, subscription, backfilled.

## send

    auto mail send --to <address> (--message <text> | --body <json>) [--from <address>]

Post one Mail. It persists whether or not anybody is subscribed, and a later
subscriber still receives it.

JSON fields: id, to, subscriptions, bound. The two counts mean different things:
` + "`subscriptions`" + ` counts durable readers of the address, ` + "`bound`" + ` counts those with a
Binding row. In this alpha, ` + "`bound`" + ` means "a binding row exists", **not** "an
agent is alive right now" — nothing refreshes a binding after subscribe yet.

` + "`--message <text>`" + ` is sugar for a body of {"message": "<text>"}.

## list

    auto mail list [--address <address>] [--subscription <id>]

List unacked mail for every Subscription bound to this caller. Reading never
retires mail — ` + "`ack`" + ` is always a separate explicit call.

JSON: an array of {id, from, sentAt, body}; ` + "`[]`" + ` when there is nothing.

## ack

    auto mail ack <mail-id>

Retire one Mail. The payload answers two different questions: ` + "`acked`" + ` says the
delivery is acked now, ` + "`wonTransition`" + ` says whether it was *this* call that
transitioned it. Losing the race is a reported outcome, not a failure — exit
stays 0 and stderr names who won it. Exit is non-zero only for an unknown id, an
id you hold no delivery for, or a store error.

JSON fields: id, acked, wonTransition.

## reset

    auto mail reset [--yes]

Remove ~/.auto/mail/alpha-store.db and ~/.auto/mail/alpha-flags/ and report what
was removed. A store that still holds events is refused without ` + "`--yes`" + `.

JSON fields: removed.

## docs

Print this reference.

## Alpha

The store is alpha, and the marker is in the artifact rather than only in prose:
the filename is ~/.auto/mail/alpha-store.db and every event type is prefixed
` + "`alpha.`" + `.

**No upcasters, no migrations, no compatibility guarantee — the store may be
wiped on upgrade.** ` + "`auto mail reset`" + ` is a supported operation, not a
workaround. Nothing outside mail may depend on the store's shape; go through the
` + "`auto-mail/mail`" + ` client seam.

` + "`quickstart`" + ` and ` + "`doctor`" + ` are not here yet — they belong to epic 005's task T4,
which owns the adoption surface.
`

func newDocsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docs",
		Short: "Print the full auto mail command reference",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprint(cmd.OutOrStdout(), docsMarkdown)
			return nil
		},
	}
}
