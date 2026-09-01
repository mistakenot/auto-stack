package mail

import (
	"errors"
	"fmt"
	"strings"
)

// The reserved prefix and the one Handle that exists.
//
// A Handle is a relative alias for an Address, resolved at send time and never
// stored: the envelope always carries the address it resolved to (G5). `#` is
// reserved for the whole family — `#new`, `#children` and the rest are parked,
// not unclaimed — which is what makes adding the second one a change to this
// file and to nothing else (D-063-1).
//
// Reservation deliberately lives *above* ValidateAddress. Addresses stay
// free-form and validation stays permissive (D-9); it is the resolver, not the
// stored-address rule, that knows `#` means something.
const (
	// HandlePrefix marks a value as a relative Handle rather than an Address.
	HandlePrefix = "#"
	// HandleParent is the supervisor of an in-process Subagent.
	HandleParent = "#parent"
)

// ErrNotSubagent is returned when `#parent` is used by a process that is not an
// in-process Subagent — no marker is recorded for its Binding.
//
// The token is bare on purpose. Callers branch on it with errors.Is and never
// on a string; the remediation prose is attached at the wrap site by
// handleError, so the sentence a user reads can change without changing what a
// caller matches on.
var ErrNotSubagent = errors.New("not an in-process subagent")

// IsHandle reports whether a value is a relative Handle rather than an Address.
//
// It is checked *before* ValidateAddress, which is why that function stays
// permissive and unchanged: `#parent` is a perfectly valid address as far as
// validation is concerned, and it must remain so, or the stored-address rule
// would start depending on a resolver concern.
func IsHandle(s string) bool {
	return strings.HasPrefix(s, HandlePrefix)
}

// handleError attaches the user-facing remediation to a resolution failure.
//
// The two layers are separate for the reason T1's sentinels are: the token is
// what a caller branches on, the message is what a user reads. The message is
// built inside the mail package rather than at the CLI so T3's RPC client
// produces the same text against the same failure, and every message names the
// three things the project's error convention requires — what is wrong, what to
// do instead, and where to read more.
func handleError(handle string, err error) error {
	switch {
	case errors.Is(err, ErrNotSubagent):
		return fmt.Errorf("cannot resolve %q: %w — no active Subagent is recorded "+
			"for this agent. %s is available only inside an Agent/Task Subagent, whose "+
			"parentage the hooks carry. Send to an absolute address instead "+
			"(`--to auto-stack/supervisor`), or see `auto mail docs` under "+
			"\"relative handles\"", handle, ErrNotSubagent, handle)
	default:
		return err
	}
}
