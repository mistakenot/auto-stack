package mail

import (
	"errors"
	"fmt"
	"slices"
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

// The four ways a relative Handle is refused.
//
// Every token is bare on purpose, and they match the shape of T1's own
// sentinels in client.go. Callers branch on these with errors.Is and never on a
// string; the remediation prose is attached at the wrap site, so the sentence a
// user reads can change without changing what a caller matches on.
//
// Four rather than one because the fixes are four different things — become a
// Subagent, subscribe in the supervisor, spell the handle correctly, use an
// absolute address — and an agent reading stderr has to tell them apart without
// parsing (AC-3, AC-4, AC-10).
var (
	// ErrNotSubagent is returned when `#parent` is used by a process that is
	// not an in-process Subagent — no marker is recorded for its Binding.
	ErrNotSubagent = errors.New("not an in-process subagent")
	// ErrNoSupervisor is returned when the caller *is* a Subagent but no
	// Subscription is bound to its Binding, so the supervisor has no address to
	// resolve to. Distinct from ErrNotSubagent because the fix is the
	// supervisor's, not the caller's (AC-10).
	ErrNoSupervisor = errors.New("your supervisor holds no subscription")
	// ErrUnknownHandle is returned for a `#`-prefixed value that is not a
	// Handle that exists. It is what keeps `#nope` from quietly becoming a
	// channel no reader can ever subscribe to.
	ErrUnknownHandle = errors.New("not a known relative handle")
	// ErrHandleNotAllowed is returned when a known Handle is used in a position
	// that only takes an absolute address — `subscribe`, `list --address`,
	// `send --from` (D-063-2). It is wrapped at the CLI, which is the only
	// layer that knows which of the three positions was used.
	ErrHandleNotAllowed = errors.New("a relative handle is not allowed in this position")
)

// IsHandle reports whether a value is a relative Handle rather than an Address.
//
// It is checked *before* ValidateAddress, which is why that function stays
// permissive and unchanged: `#parent` is a perfectly valid address as far as
// validation is concerned, and it must remain so, or the stored-address rule
// would start depending on a resolver concern.
func IsHandle(s string) bool {
	return strings.HasPrefix(s, HandlePrefix)
}

// KnownHandles returns the Handles that exist, newest members last.
//
// It exists so the "`#nope` is not a handle" message lists them rather than
// spelling them out inline: adding `#children` should change this slice and no
// error string (D-063-1).
func KnownHandles() []string {
	return []string{HandleParent}
}

// ValidateHandle checks that a `#`-prefixed value names a Handle that exists.
//
// It is the address-namespace's other half, and deliberately shaped like
// ValidateAddress: one exported function, reused everywhere the question is
// asked, so the CLI's three rejected positions and the resolver all produce the
// same sentence for the same typo. Passing it a value that is not a Handle at
// all is a caller error — check IsHandle first — and is reported as one rather
// than silently accepted.
func ValidateHandle(s string) error {
	if !IsHandle(s) {
		return fmt.Errorf("%w: %q is an address, not a handle", ErrUnknownHandle, s)
	}
	if slices.Contains(KnownHandles(), s) {
		return nil
	}
	return describeHandleError(ErrUnknownHandle, s)
}

// describeHandleError attaches the user-facing remediation to a resolution
// failure.
//
// It returns an error rather than the string the API sketch imagined, and that
// is load-bearing rather than a liberty: the sentence is joined to its token
// with %w, so one value carries both layers. A string would force every caller
// to re-wrap — and a caller that reached for errors.New instead would silently
// break errors.Is, which is the entire contract these four sentinels exist to
// offer (AC-3, AC-10).
//
// The two layers are separate for the reason T1's sentinels are: the token is
// what a caller branches on, the message is what a user reads. All three
// messages are built inside the mail package rather than at the CLI so T3's RPC
// client produces the same text against the same failure, and every message
// names the three things the project's error convention requires — what is
// wrong, what to do instead, and where to read more.
//
// The fourth refusal, ErrHandleNotAllowed, is deliberately absent: only the CLI
// knows whether the offending value arrived as a `subscribe` argument, as
// `--address` or as `--from`, and a message that could not name the position
// would be the least useful of the four.
func describeHandleError(err error, handle string) error {
	switch {
	case errors.Is(err, ErrNotSubagent):
		return fmt.Errorf("cannot resolve %q: %w — no active Subagent is recorded "+
			"for this agent. %s is available only inside an Agent/Task Subagent, whose "+
			"parentage the hooks carry. Send to an absolute address instead "+
			"(`--to auto-stack/supervisor`), or see `auto mail docs` under "+
			"\"relative handles\"", handle, ErrNotSubagent, handle)
	case errors.Is(err, ErrNoSupervisor):
		return fmt.Errorf("cannot resolve %q: %w, so it has no address to be mailed "+
			"at. Run `auto mail subscribe <address>` in the supervisor first — it is "+
			"what binds an agent to an address. See `auto mail docs` under "+
			"\"relative handles\"", handle, ErrNoSupervisor)
	case errors.Is(err, ErrUnknownHandle):
		return fmt.Errorf("%q is %w. Known handles: %s; `#` is reserved for handles, "+
			"so it can never be used as an address — drop the `#` if you meant a "+
			"channel of that name. See `auto mail docs` under \"relative handles\"",
			handle, ErrUnknownHandle, strings.Join(KnownHandles(), ", "))
	default:
		return err
	}
}
