package conformance_test

import (
	"testing"

	"github.com/mistakenot/auto-mail/conformance"
	"github.com/mistakenot/auto-mail/mail"
)

// TestDirectClientConformance runs the whole suite against the direct-store
// client — the one implementation T1 ships (D-11).
//
// One target is the point rather than an admission: the suite is written
// against the interface, so when T3 registers mail.* on the daemon peer its
// RPC client becomes a second function in this file rather than a redesign
// (D-062-5).
func TestDirectClientConformance(t *testing.T) {
	conformance.RunSuite(t, func(t *testing.T) mail.Client {
		// A fresh home per case, so no case can see another's mailbox. The
		// direct client needs no daemon and no init step: it opens and migrates
		// the alpha store itself.
		client, err := mail.NewDirect(t.TempDir())
		if err != nil {
			t.Fatalf("open the direct client: %v", err)
		}
		t.Cleanup(func() { _ = client.Close() })
		return client
	})
}
