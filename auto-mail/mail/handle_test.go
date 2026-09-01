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
