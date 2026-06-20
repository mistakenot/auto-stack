// Package conformance provides a reusable transport-agnostic conformance
// harness for the auto-shared/rpc layer. It defines seam interfaces (RPCClient,
// Observations, Fixture) and a Scenario runner that downstream tasks import to
// verify their RPC integrations produce identical behavior across transports.
//
// This is the ONE package that may import both rpc and transport — it is the
// single exception to the layering rule enforced by layering_test.go.
package conformance

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mistakenot/auto-shared/rpc"
)

// RPCClient is the minimal surface a Scenario drives. *rpc.Peer satisfies
// Call and Notify directly; a thin PeerClient adapter exposes OnNotify as
// a channel.
type RPCClient interface {
	// Call sends a request and blocks until the correlated response arrives.
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
	// Notify sends an id-less notification.
	Notify(method string, params any) error
	// Notifications returns a channel that receives inbound notifications
	// (requests with no id) from the remote peer.
	Notifications() <-chan rpc.Request
}

// Observations provides transport/impl-neutral observables for assertions.
type Observations interface {
	// DispatchCount returns the number of times a handler for the given
	// method was dispatched on the server side.
	DispatchCount(method string) int
}

// Fixture is a connected RPC environment. It decouples scenario assertions
// from how the environment is built, observed, and torn down.
type Fixture interface {
	// Client returns the RPCClient that scenarios use to send calls and
	// notifications.
	Client() RPCClient
	// Obs returns transport-neutral observables for the fixture.
	Obs() Observations
	// Close tears down the fixture, releasing all resources.
	Close() error
}

// FixtureFactory builds one Fixture for a test.
type FixtureFactory func(t testing.TB) Fixture

// Scenario is a single conformance test case. Written once, it asserts
// only through the Fixture interface so it is transport-agnostic.
type Scenario interface {
	// Name returns the scenario's human-readable name, used as the subtest name.
	Name() string
	// Run executes the scenario against the given fixture.
	Run(t testing.TB, f Fixture)
}

// RunAcrossFixtures runs a Scenario against each FixtureFactory as a subtest.
// This is the key function downstream tasks use to verify identical behavior
// across transports.
func RunAcrossFixtures(t testing.TB, s Scenario, factories ...FixtureFactory) {
	tt, ok := t.(*testing.T)
	if !ok {
		t.Fatal("RunAcrossFixtures requires *testing.T for subtests")
		return
	}
	for i, factory := range factories {
		f := factory // capture for closure
		name := factoryName(i)
		tt.Run(s.Name()+"/"+name, func(t *testing.T) {
			fix := f(t)
			defer fix.Close()
			s.Run(t, fix)
		})
	}
}

// factoryName returns a descriptive name for each factory index. The
// FakeFixtures() function returns them in a fixed order: pipe, unix, tcp.
func factoryName(i int) string {
	switch i {
	case 0:
		return "pipe"
	case 1:
		return "unix"
	case 2:
		return "tcp"
	default:
		return "unknown"
	}
}
