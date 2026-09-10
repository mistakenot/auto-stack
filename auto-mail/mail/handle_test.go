package mail_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/mistakenot/auto-mail/mail"
)

// TestIsHandleReservesThePrefix: `#` marks a relative Handle, and it does so on
// the whole family rather than on the one member that exists — which is what
// keeps adding `#children` a change to one file (D-063-1).
func TestIsHandleReservesThePrefix(t *testing.T) {
	handles := []string{"#parent", "#nope", "#", "#auto-web/bugs"}
	addresses := []string{"auto-web/bugs", "bugs", "parent", "a#b", "", "auto-stack/supervisor"}

	for _, s := range handles {
		if !mail.IsHandle(s) {
			t.Errorf("IsHandle(%q) = false, want true — `#` is reserved for the whole family", s)
		}
	}
	for _, s := range addresses {
		if mail.IsHandle(s) {
			t.Errorf("IsHandle(%q) = true, want false", s)
		}
	}
}

// TestValidateAddressStillAcceptsAHandle: reservation happens *above*
// validation, and this asserts the layering rather than trusting it.
//
// D-9 requires the stored-address namespace to stay free-form and permissive.
// Teaching ValidateAddress to reject `#` would make the rule about what may be
// *stored* depend on a resolver concern, and would be the natural "fix" for a
// future reader who has not read D-063-1 — so the layering is a test.
func TestValidateAddressStillAcceptsAHandle(t *testing.T) {
	if err := mail.ValidateAddress(mail.HandleParent); err != nil {
		t.Errorf("ValidateAddress(%q) = %v, want nil: `#` is reserved by the resolver, "+
			"not by validation (D-9/D-063-1)", mail.HandleParent, err)
	}
}

// TestErrNotSubagentIsABareBranchableToken: T1's "sentinel errors, then wrap"
// convention. The token is what errors.Is matches; the sentence a user reads is
// built at the wrap site. A sentinel carrying its own remediation would put the
// two on one string, and every caller would end up matching on prose.
func TestErrNotSubagentIsABareBranchableToken(t *testing.T) {
	text := mail.ErrNotSubagent.Error()
	if strings.Contains(text, "auto mail") || strings.Contains(text, "instead") {
		t.Errorf("ErrNotSubagent carries remediation prose (%q); the wrapper adds that", text)
	}
	if !errors.Is(mail.ErrNotSubagent, mail.ErrNotSubagent) {
		t.Errorf("ErrNotSubagent is not matchable with errors.Is")
	}
	if errors.Is(mail.ErrNotSubagent, mail.ErrInvalidAddress) {
		t.Errorf("ErrNotSubagent aliases ErrInvalidAddress; the two failures have different fixes")
	}
}

// TestEveryHandleSentinelIsABareBranchableToken generalises the case above to
// all four. They are the tokens a caller matches on, so none may carry
// remediation prose, and none may alias another — two failures with two
// different fixes that compare equal would make `errors.Is` a coin toss.
func TestEveryHandleSentinelIsABareBranchableToken(t *testing.T) {
	sentinels := map[string]error{
		"ErrNotSubagent":      mail.ErrNotSubagent,
		"ErrNoSupervisor":     mail.ErrNoSupervisor,
		"ErrUnknownHandle":    mail.ErrUnknownHandle,
		"ErrHandleNotAllowed": mail.ErrHandleNotAllowed,
	}
	for name, sentinel := range sentinels {
		text := sentinel.Error()
		for _, prose := range []string{"auto mail", "instead", "Run `", "for example"} {
			if strings.Contains(text, prose) {
				t.Errorf("%s carries remediation prose (%q); the wrapper adds that", name, text)
			}
		}
		for otherName, other := range sentinels {
			if otherName == name {
				continue
			}
			if errors.Is(sentinel, other) {
				t.Errorf("%s aliases %s; the two failures have different fixes", name, otherName)
			}
		}
	}
}

// TestKnownHandlesIsTheOneListEveryErrorReads: adding `#children` should be a
// change to this slice and to no error string (D-063-1), which is only true if
// nothing spells the list out inline.
func TestKnownHandlesIsTheOneListEveryErrorReads(t *testing.T) {
	known := mail.KnownHandles()
	if len(known) != 1 || known[0] != mail.HandleParent {
		t.Fatalf("KnownHandles() = %v, want exactly [%q] — the family is reserved, "+
			"but only one member exists", known, mail.HandleParent)
	}
	for _, h := range known {
		if !mail.IsHandle(h) {
			t.Errorf("KnownHandles() returned %q, which IsHandle does not recognise", h)
		}
		if err := mail.ValidateHandle(h); err != nil {
			t.Errorf("ValidateHandle(%q) = %v, want nil for a handle that exists", h, err)
		}
	}

	// A caller mutating the returned slice must not be able to invent a handle.
	known[0] = "#invented"
	if again := mail.KnownHandles(); again[0] != mail.HandleParent {
		t.Errorf("KnownHandles() returns shared state: after a caller wrote to it, "+
			"it answers %v", again)
	}
}

// TestValidateHandleRefusesAnUnrecognisedHandle: `#nope` is a typo, never a new
// channel. The message has to name the offending value *and* list what exists,
// because "that is not a handle" without the list leaves the caller guessing.
func TestValidateHandleRefusesAnUnrecognisedHandle(t *testing.T) {
	err := mail.ValidateHandle("#parnet")
	if !errors.Is(err, mail.ErrUnknownHandle) {
		t.Fatalf("ValidateHandle(\"#parnet\") = %v, want ErrUnknownHandle", err)
	}
	for _, want := range []string{"#parnet", mail.HandleParent, "auto mail docs"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %s", want, err)
		}
	}

	// An address is a caller error rather than a typo, and says so: reaching
	// ValidateHandle at all means IsHandle was not asked first.
	if err := mail.ValidateHandle("auto-web/bugs"); !errors.Is(err, mail.ErrUnknownHandle) {
		t.Errorf("ValidateHandle(%q) = %v, want ErrUnknownHandle", "auto-web/bugs", err)
	}
}
