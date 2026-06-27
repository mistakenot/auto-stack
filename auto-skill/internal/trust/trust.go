// Package trust implements the machine-local skill fetch trust store and the
// fail-closed authorization gate. Effective approval lives only in machine-local
// state (trust.json), never in the repo: a repo may *request* trusted hosts via
// skills.yaml, but a host is fetched from only after it is recorded here.
package trust

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-skill/internal/transport"
)

const trustVersion = 1

// TrustFile is the on-disk schema of the machine-local trust store.
type TrustFile struct {
	Version   int      `json:"version"`
	Endpoints []string `json:"endpoints"`
}

// Store is a handle to a machine-local trust.json file.
type Store struct {
	path string
}

// NewStore returns a Store backed by the trust.json at trustPath.
func NewStore(trustPath string) *Store {
	return &Store{path: trustPath}
}

func (s *Store) lockPath() string {
	return s.path + ".lock"
}

// Load reads the trust store. A missing file yields an empty store rather than
// an error so the gate can fail closed on its own terms.
func (s *Store) Load() (*TrustFile, error) {
	return s.read()
}

func (s *Store) read() (*TrustFile, error) {
	if _, err := os.Stat(s.path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &TrustFile{Version: trustVersion}, nil
		}
		return nil, err
	}
	var tf TrustFile
	if err := config.DecodeJSONFileStrict(s.path, &tf); err != nil {
		return nil, err
	}
	if tf.Version == 0 {
		tf.Version = trustVersion
	}
	return &tf, nil
}

// IsApproved reports whether endpoint, once canonicalized, is recorded in the
// store. Canonicalization is port- and scheme-aware so https://github.com and
// https://github.com:443 collapse to one identity while git:// and ssh:// to the
// same host stay distinct.
func (s *Store) IsApproved(endpoint string) (bool, error) {
	ep, err := transport.Endpoint(endpoint)
	if err != nil {
		return false, err
	}
	tf, err := s.read()
	if err != nil {
		return false, err
	}
	return slices.Contains(tf.Endpoints, ep), nil
}

// Add canonicalizes and records rawEndpoint. It rejects credential-bearing and
// unsupported-transport endpoints (via the transport primitive), is idempotent,
// and keeps the stored set deduped and sorted. The read-modify-write runs under
// an exclusive flock so concurrent writers do not lose updates.
func (s *Store) Add(rawEndpoint string) error {
	ep, err := transport.Endpoint(rawEndpoint)
	if err != nil {
		return err
	}
	return s.withLock(func() error {
		tf, err := s.read()
		if err != nil {
			return err
		}
		if slices.Contains(tf.Endpoints, ep) {
			return nil
		}
		tf.Endpoints = append(tf.Endpoints, ep)
		normalize(tf)
		return config.WriteJSONFileAtomic(s.path, tf)
	})
}

// Remove canonicalizes rawEndpoint and drops it from the store if present.
// Removing an absent endpoint is a no-op success.
func (s *Store) Remove(rawEndpoint string) error {
	ep, err := transport.Endpoint(rawEndpoint)
	if err != nil {
		return err
	}
	return s.withLock(func() error {
		tf, err := s.read()
		if err != nil {
			return err
		}
		idx := slices.Index(tf.Endpoints, ep)
		if idx < 0 {
			return nil
		}
		tf.Endpoints = slices.Delete(tf.Endpoints, idx, idx+1)
		normalize(tf)
		return config.WriteJSONFileAtomic(s.path, tf)
	})
}

func normalize(tf *TrustFile) {
	if tf.Version == 0 {
		tf.Version = trustVersion
	}
	slices.Sort(tf.Endpoints)
	tf.Endpoints = slices.Compact(tf.Endpoints)
}

func (s *Store) withLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create trust dir: %w", err)
	}
	f, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open trust lock: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire trust lock: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}

// NotApprovedError is returned when the gate fails closed. It names the exact
// endpoints that need approval so the caller can print actionable remediation.
type NotApprovedError struct {
	Endpoints []string
}

func (e *NotApprovedError) Error() string {
	joined := strings.Join(e.Endpoints, " ")
	return fmt.Sprintf(
		"endpoint not approved: %s; approve it on this machine with `auto skill trust add %s`",
		strings.Join(e.Endpoints, ", "), joined,
	)
}

// GateIO abstracts the I/O context for the trust gate.
type GateIO struct {
	IsTTY          bool
	TrustRequested bool // --trust-requested flag or AUTO_SKILL_TRUST_REQUESTED=1
}

// Gate authorizes fetches against a Store, recording new approvals as a
// side effect of an opt-in flag or an interactive yes.
type Gate struct {
	Store *Store
	// prompt is overridable for tests; nil falls back to a stdin prompt.
	prompt func(endpoint string) (bool, error)
}

// Authorize gates a fetch from endpoint. requestedHosts are the hosts the repo's
// skills.yaml asks to trust (advisory only). Resolution order:
//  1. already approved in machine state -> ok
//  2. TrustRequested set and endpoint is among requestedHosts -> approve + record
//  3. interactive TTY -> prompt, record on yes
//  4. otherwise -> fail closed
func (g *Gate) Authorize(endpoint string, requestedHosts []string, gio GateIO) error {
	ep, err := transport.Endpoint(endpoint)
	if err != nil {
		return err
	}

	approved, err := g.Store.IsApproved(ep)
	if err != nil {
		return err
	}
	if approved {
		return nil
	}

	if gio.TrustRequested && g.isRequested(ep, requestedHosts) {
		return g.Store.Add(ep)
	}

	if gio.IsTTY {
		ok, err := g.askApprove(ep)
		if err != nil {
			return err
		}
		if ok {
			return g.Store.Add(ep)
		}
	}

	return &NotApprovedError{Endpoints: []string{ep}}
}

func (g *Gate) isRequested(ep string, requestedHosts []string) bool {
	return slices.ContainsFunc(requestedHosts, func(h string) bool {
		canon, err := transport.Endpoint(h)
		if err != nil {
			return false
		}
		return canon == ep
	})
}

func (g *Gate) askApprove(endpoint string) (bool, error) {
	if g.prompt != nil {
		return g.prompt(endpoint)
	}
	return defaultPrompt(endpoint)
}

func defaultPrompt(endpoint string) (bool, error) {
	fmt.Fprintf(os.Stderr, "Approve fetching from %s? [y/N] ", endpoint)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
