package mail

import (
	"fmt"
	"strings"
	"unicode"
)

// MaxAddressLength bounds an address. The limit exists so a typo cannot write an
// unbounded string into every delivery row, not to shape the namespace.
const MaxAddressLength = 256

// ValidateAddress checks an address and explains any rejection.
//
// Addresses are **virtual and free-form** (D-9/G5): they name a channel, never a
// machine, a session or a pane, and nothing derives physical identity from one.
// Validation is therefore deliberately permissive — it rejects only what cannot
// round-trip or cannot have been meant:
//
//   - the empty string,
//   - leading or trailing whitespace (invisible, and two addresses that differ
//     only by it would silently fail to match),
//   - control characters,
//   - anything over MaxAddressLength.
//
// Everything else is accepted. In particular `/` is a permitted, ordinary
// character: `auto-web/bugs` is one address, it is **never** normalised or split
// into a hierarchy, and it keeps prefix filtering buildable later. The cost of
// being permissive is the typo case, which `send` mitigates by reporting a
// zero-subscription send on stderr rather than by narrowing the namespace.
func ValidateAddress(s string) error {
	if s == "" {
		return fmt.Errorf("%w: the address is empty — pass a name such as auto-web/bugs", ErrInvalidAddress)
	}
	if strings.TrimSpace(s) != s {
		return fmt.Errorf("%w: %q has leading or trailing whitespace — remove it; "+
			"addresses are compared exactly, so a padded one silently matches nothing", ErrInvalidAddress, s)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: %q contains the control character %q — "+
				"use printable characters; `/` is allowed and is never normalised away",
				ErrInvalidAddress, s, r)
		}
	}
	if len(s) > MaxAddressLength {
		return fmt.Errorf("%w: the address is %d bytes, over the %d-byte limit — "+
			"shorten it, or move the detail into the body", ErrInvalidAddress, len(s), MaxAddressLength)
	}
	return nil
}
