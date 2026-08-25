package mail

import "github.com/mistakenot/auto-mail/internal/store"

// This file exposes the package's internals to its own external test package
// (`package mail_test`) and to nothing else — it is a _test.go file, so none of
// it is compiled into the binary. The production surface stays exactly what
// the seam documents: HasPending, NudgeText, BindingFor, ValidateAddress.

// CountStoreOpens installs a counting wrapper over the package's only store
// opener and returns the count so far plus a restore function.
//
// It exists to make AC-10's central claim assertable rather than assumed: the
// hook's mail check must open the store on *neither* path, and the only way to
// prove a negative like that is to count the door being used.
func CountStoreOpens() (opens func() int, restore func()) {
	var n int
	prev := openStore
	openStore = func(path string) (*store.Store, error) {
		n++
		return prev(path)
	}
	return func() int { return n }, func() { openStore = prev }
}

// FlagPathFor is flagPath, exported for tests that need to plant or inspect a
// pending flag directly — a false-positive flag has no other way in, because
// nothing outside this package may write one (G11).
func FlagPathFor(home string, b Binding) string { return flagPath(home, b) }

// SetPendingFor raises a flag from a test without going through a send.
func SetPendingFor(home string, b Binding) error { return setPending(home, b) }
