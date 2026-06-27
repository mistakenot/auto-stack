package trust

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/mistakenot/auto-skill/internal/transport"
)

func newTempStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "skills", "trust.json"))
}

func TestStoreRoundTrip(t *testing.T) {
	s := newTempStore(t)

	if err := s.Add("https://github.com"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	tf, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tf.Version != trustVersion {
		t.Errorf("version = %d, want %d", tf.Version, trustVersion)
	}

	ok, err := s.IsApproved("https://github.com")
	if err != nil {
		t.Fatalf("IsApproved: %v", err)
	}
	if !ok {
		t.Errorf("expected endpoint to be approved")
	}
}

func TestLoadMissingFileEmpty(t *testing.T) {
	s := newTempStore(t)
	tf, err := s.Load()
	if err != nil {
		t.Fatalf("Load missing file: %v", err)
	}
	if len(tf.Endpoints) != 0 {
		t.Errorf("expected empty endpoints, got %v", tf.Endpoints)
	}
	if tf.Version != trustVersion {
		t.Errorf("version = %d, want %d", tf.Version, trustVersion)
	}
}

func TestPortNormalization(t *testing.T) {
	s := newTempStore(t)
	if err := s.Add("https://github.com"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// The explicit default-port form must canonicalize to the same identity.
	ok, err := s.IsApproved("https://github.com:443")
	if err != nil {
		t.Fatalf("IsApproved: %v", err)
	}
	if !ok {
		t.Errorf("https://github.com:443 should match approved https://github.com")
	}

	tf, _ := s.Load()
	if len(tf.Endpoints) != 1 {
		t.Errorf("expected 1 endpoint after normalization, got %v", tf.Endpoints)
	}
}

func TestSchemeAndPortIsolation(t *testing.T) {
	s := newTempStore(t)
	if err := s.Add("https://github.com:443"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	for _, other := range []string{"git://github.com:9418", "ssh://github.com:22"} {
		ok, err := s.IsApproved(other)
		if err != nil {
			t.Fatalf("IsApproved(%s): %v", other, err)
		}
		if ok {
			t.Errorf("approving https should not authorize %s", other)
		}
	}
}

func TestLocalPathEndpoint(t *testing.T) {
	s := newTempStore(t)
	const local = "/home/user/repos"
	if err := s.Add(local); err != nil {
		t.Fatalf("Add local: %v", err)
	}
	ok, err := s.IsApproved(local)
	if err != nil {
		t.Fatalf("IsApproved local: %v", err)
	}
	if !ok {
		t.Errorf("local path endpoint should be approved")
	}
}

func TestIdempotentAdd(t *testing.T) {
	s := newTempStore(t)
	if err := s.Add("https://github.com"); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if err := s.Add("https://github.com:443"); err != nil {
		t.Fatalf("re-Add: %v", err)
	}
	tf, _ := s.Load()
	if len(tf.Endpoints) != 1 {
		t.Errorf("expected 1 endpoint after idempotent add, got %v", tf.Endpoints)
	}
}

func TestIdempotentRemove(t *testing.T) {
	s := newTempStore(t)
	// Removing from a non-existent store is a no-op success.
	if err := s.Remove("https://github.com"); err != nil {
		t.Fatalf("Remove absent (empty store): %v", err)
	}

	if err := s.Add("https://github.com"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Remove("https://github.com:443"); err != nil {
		t.Fatalf("Remove present: %v", err)
	}
	ok, _ := s.IsApproved("https://github.com")
	if ok {
		t.Errorf("endpoint should be removed")
	}
	// Removing again is still a no-op success.
	if err := s.Remove("https://github.com"); err != nil {
		t.Fatalf("Remove absent (after delete): %v", err)
	}
}

func TestAddRejectsCredentials(t *testing.T) {
	s := newTempStore(t)
	err := s.Add("https://user:pass@github.com/repo.git")
	if err == nil {
		t.Fatalf("expected credential-bearing endpoint to be rejected")
	}
	var te *transport.TransportError
	if !errors.As(err, &te) {
		t.Fatalf("expected TransportError, got %T: %v", err, err)
	}
	if te.Code != transport.CodeCredentialsInURL {
		t.Errorf("code = %q, want %q", te.Code, transport.CodeCredentialsInURL)
	}
}

func TestAddRejectsUnsupportedTransport(t *testing.T) {
	s := newTempStore(t)
	err := s.Add("ftp://example.com/repo")
	if err == nil {
		t.Fatalf("expected unsupported transport to be rejected")
	}
	var te *transport.TransportError
	if !errors.As(err, &te) {
		t.Fatalf("expected TransportError, got %T: %v", err, err)
	}
	if te.Code != transport.CodeUnsupportedTransport {
		t.Errorf("code = %q, want %q", te.Code, transport.CodeUnsupportedTransport)
	}
}

func TestAuthorizeApprovedPasses(t *testing.T) {
	s := newTempStore(t)
	if err := s.Add("https://github.com"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	g := &Gate{Store: s}
	if err := g.Authorize("https://github.com:443", nil, GateIO{}); err != nil {
		t.Errorf("Authorize approved endpoint: %v", err)
	}
}

func TestAuthorizeNonTTYFailsClosed(t *testing.T) {
	s := newTempStore(t)
	g := &Gate{Store: s}

	err := g.Authorize("https://github.com", []string{"https://github.com"}, GateIO{IsTTY: false, TrustRequested: false})
	if err == nil {
		t.Fatalf("expected fail-closed error in non-TTY without opt-in")
	}
	var nae *NotApprovedError
	if !errors.As(err, &nae) {
		t.Fatalf("expected NotApprovedError, got %T: %v", err, err)
	}
	if len(nae.Endpoints) != 1 || nae.Endpoints[0] != "https://github.com:443" {
		t.Errorf("expected canonical endpoint in error, got %v", nae.Endpoints)
	}

	// Nothing should have been recorded.
	if ok, _ := s.IsApproved("https://github.com"); ok {
		t.Errorf("fail-closed gate must not record approval")
	}
}

func TestAuthorizeTrustRequestedApprovesOnlyRequested(t *testing.T) {
	s := newTempStore(t)
	g := &Gate{Store: s}

	// Requested host gets approved and recorded.
	if err := g.Authorize("https://github.com", []string{"https://github.com"}, GateIO{TrustRequested: true}); err != nil {
		t.Fatalf("Authorize requested host: %v", err)
	}
	if ok, _ := s.IsApproved("https://github.com"); !ok {
		t.Errorf("requested host should have been recorded")
	}

	// A non-requested host must still fail closed even with TrustRequested.
	err := g.Authorize("https://evil.example.com", []string{"https://github.com"}, GateIO{TrustRequested: true})
	if err == nil {
		t.Fatalf("non-requested host should fail closed")
	}
	var nae *NotApprovedError
	if !errors.As(err, &nae) {
		t.Fatalf("expected NotApprovedError, got %T: %v", err, err)
	}
}

func TestAuthorizeTTYPromptRecordsOnYes(t *testing.T) {
	s := newTempStore(t)
	g := &Gate{Store: s, prompt: func(string) (bool, error) { return true, nil }}

	if err := g.Authorize("https://github.com", nil, GateIO{IsTTY: true}); err != nil {
		t.Fatalf("Authorize with yes prompt: %v", err)
	}
	if ok, _ := s.IsApproved("https://github.com"); !ok {
		t.Errorf("TTY yes should record approval")
	}
}

func TestAuthorizeTTYPromptFailsOnNo(t *testing.T) {
	s := newTempStore(t)
	g := &Gate{Store: s, prompt: func(string) (bool, error) { return false, nil }}

	err := g.Authorize("https://github.com", nil, GateIO{IsTTY: true})
	var nae *NotApprovedError
	if !errors.As(err, &nae) {
		t.Fatalf("expected NotApprovedError on no, got %T: %v", err, err)
	}
	if ok, _ := s.IsApproved("https://github.com"); ok {
		t.Errorf("TTY no must not record approval")
	}
}
