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

## init

Create ~/.auto/mail/ and the alpha store, and establish host identity.

JSON fields: store, created, alpha.

## list

List unacked mail for every Subscription bound to this caller. Reading never
retires mail — ` + "`ack`" + ` is always a separate explicit call.

JSON: an array of {id, from, sentAt, body}; ` + "`[]`" + ` when there is nothing.

## docs

Print this reference.

## Alpha

The store is alpha: its filename carries the marker
(~/.auto/mail/alpha-store.db) and every event type is prefixed ` + "`alpha.`" + `.
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
