package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistakenot/auto-shared/rpc"
	"github.com/mistakenot/auto-shared/transport"
	"github.com/mistakenot/auto-ui/internal/config"
)

// runDoctor executes the doctor command with an isolated HOME already set by the
// caller, returning the parsed checks, the raw stdout, and the command error.
func runDoctor(t *testing.T) ([]doctorCheck, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := newDoctorCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(nil)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetContext(context.Background())
	err := cmd.Execute()

	var checks []doctorCheck
	if out := strings.TrimSpace(stdout.String()); out != "" {
		if jerr := json.Unmarshal([]byte(out), &checks); jerr != nil {
			t.Fatalf("doctor stdout not JSON: %v\nstdout: %s", jerr, out)
		}
	}
	return checks, stdout.String(), err
}

func findCheck(checks []doctorCheck, name string) (doctorCheck, bool) {
	for _, c := range checks {
		if c.Check == name {
			return c, true
		}
	}
	return doctorCheck{}, false
}

// TestDoctorPerBackendHealth covers AC-9: with one reachable and one unreachable
// backend, doctor emits a backends_config check, a passing connectivity check
// (carrying the hostId) for the reachable backend, a failing connectivity check
// with a remediation hint for the unreachable one, and exits non-zero.
func TestDoctorPerBackendHealth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Valid UI settings so the ui_settings/port checks pass and don't short-circuit.
	if _, _, _, err := config.EnsureUISettings(); err != nil {
		t.Fatalf("EnsureUISettings: %v", err)
	}

	// One reachable backend (a real in-process fake autowatch over a unix socket).
	reachableURI := "unix://" + filepath.Join(t.TempDir(), "reachable.sock")
	stop := startFakeBackend(t, reachableURI, "doctor-host", 2)
	defer stop()

	// One unreachable backend: a unix socket path with nothing listening.
	unreachableURI := "unix://" + filepath.Join(t.TempDir(), "missing.sock")

	path, err := config.BackendsPath()
	if err != nil {
		t.Fatalf("BackendsPath: %v", err)
	}
	if err := config.SaveBackends(path, config.BackendsConfig{Backends: []config.Backend{
		{URI: reachableURI, Name: "reachable"},
		{URI: unreachableURI, Name: "down"},
	}}); err != nil {
		t.Fatalf("SaveBackends: %v", err)
	}

	checks, stdout, err := runDoctor(t)

	// Any failing check => non-zero exit.
	if err == nil {
		t.Fatalf("doctor returned nil, want non-zero exit (an unreachable backend fails)\nstdout: %s", stdout)
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code == 0 {
		t.Fatalf("doctor error = %v, want non-zero *ExitError", err)
	}

	// backends_config passes (two backends configured).
	if bc, ok := findCheck(checks, "backends_config"); !ok || bc.Status != "pass" {
		t.Fatalf("backends_config check = %+v (ok=%v), want pass", bc, ok)
	}

	// Reachable backend: pass connectivity check carrying the hostId.
	reach, ok := findCheck(checks, "backend:"+reachableURI)
	if !ok {
		t.Fatalf("missing connectivity check for reachable backend; checks: %+v", checks)
	}
	if reach.Status != "pass" {
		t.Fatalf("reachable backend check = %+v, want pass", reach)
	}
	if !strings.Contains(reach.Message, "doctor-host") {
		t.Errorf("reachable backend message %q, want it to include hostId doctor-host", reach.Message)
	}

	// Unreachable backend: fail with a remediation hint.
	down, ok := findCheck(checks, "backend:"+unreachableURI)
	if !ok {
		t.Fatalf("missing connectivity check for unreachable backend; checks: %+v", checks)
	}
	if down.Status != "fail" {
		t.Fatalf("unreachable backend check = %+v, want fail", down)
	}
	if down.Hint == "" {
		t.Errorf("unreachable backend check has no remediation hint: %+v", down)
	}
}

// startFakeRelayBackend stands up an in-process autowatch-like RPC server that
// answers daemon.status and project.list and, when relay is true, also answers
// bus.subscribe (as real autowatch does). With relay=false the backend is
// reachable but the relay probe degrades, mirroring a backend whose event relay
// is unavailable. It returns a stop function.
func startFakeRelayBackend(t *testing.T, uri, hostID string, relay bool) func() {
	t.Helper()
	lis, err := transport.Listen(uri)
	if err != nil {
		t.Fatalf("transport.Listen(%q): %v", uri, err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			peer := rpc.NewPeer(conn)
			peer.Register("daemon.status", func(_ context.Context, _ json.RawMessage) (any, error) {
				return map[string]any{"hostId": hostID, "version": "test"}, nil
			})
			peer.Register("project.list", func(_ context.Context, _ json.RawMessage) (any, error) {
				return []map[string]any{}, nil
			})
			if relay {
				peer.Register("bus.subscribe", func(_ context.Context, _ json.RawMessage) (any, error) {
					return map[string]any{"subscribed": true}, nil
				})
			}
			go func() { _ = peer.Serve(ctx) }()
		}
	}()

	return func() {
		cancel()
		_ = lis.Close()
	}
}

// TestDoctorRelayProbe covers the Phase 4 relay check (AC-8): an otherwise-
// reachable backend that answers bus.subscribe passes its relay check; a
// reachable backend that does not answer bus.subscribe warns (with a remediation
// hint) but does not fail doctor (RPC proxying still works); and an unreachable
// backend gets no relay check at all (the probe is skipped once connectivity
// fails).
func TestDoctorRelayProbe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, _, _, err := config.EnsureUISettings(); err != nil {
		t.Fatalf("EnsureUISettings: %v", err)
	}

	relayURI := "unix://" + filepath.Join(t.TempDir(), "relay.sock")
	stopRelay := startFakeRelayBackend(t, relayURI, "relay-host", true)
	defer stopRelay()

	degradedURI := "unix://" + filepath.Join(t.TempDir(), "degraded.sock")
	stopDegraded := startFakeRelayBackend(t, degradedURI, "degraded-host", false)
	defer stopDegraded()

	unreachableURI := "unix://" + filepath.Join(t.TempDir(), "missing.sock")

	path, err := config.BackendsPath()
	if err != nil {
		t.Fatalf("BackendsPath: %v", err)
	}
	if err := config.SaveBackends(path, config.BackendsConfig{Backends: []config.Backend{
		{URI: relayURI, Name: "relay"},
		{URI: degradedURI, Name: "degraded"},
		{URI: unreachableURI, Name: "down"},
	}}); err != nil {
		t.Fatalf("SaveBackends: %v", err)
	}

	checks, stdout, err := runDoctor(t)

	// The unreachable backend fails connectivity, so doctor exits non-zero; the
	// relay warnings on their own must not cause a failing exit.
	if err == nil {
		t.Fatalf("doctor returned nil, want non-zero exit (an unreachable backend fails)\nstdout: %s", stdout)
	}

	// Reachable + relay-capable backend: relay check passes.
	relayCheck, ok := findCheck(checks, "relay:"+relayURI)
	if !ok {
		t.Fatalf("missing relay check for relay backend; checks: %+v", checks)
	}
	if relayCheck.Status != "pass" {
		t.Fatalf("relay backend relay check = %+v, want pass", relayCheck)
	}

	// Reachable but no bus.subscribe: relay check warns with a remediation hint.
	degradedCheck, ok := findCheck(checks, "relay:"+degradedURI)
	if !ok {
		t.Fatalf("missing relay check for degraded backend; checks: %+v", checks)
	}
	if degradedCheck.Status != "warn" {
		t.Fatalf("degraded backend relay check = %+v, want warn", degradedCheck)
	}
	if degradedCheck.Hint == "" {
		t.Errorf("degraded relay check has no remediation hint: %+v", degradedCheck)
	}

	// A relay warning must not be a failing check (doctor would exit non-zero
	// only on fail; relay-degraded keeps RPC proxying usable).
	if degradedCheck.Status == "fail" {
		t.Errorf("relay-degraded must warn, not fail: %+v", degradedCheck)
	}

	// Unreachable backend: connectivity fails and NO relay check is emitted
	// (the probe is skipped for an unreachable backend).
	if down, ok := findCheck(checks, "backend:"+unreachableURI); !ok || down.Status != "fail" {
		t.Fatalf("unreachable backend connectivity check = %+v (ok=%v), want fail", down, ok)
	}
	if rc, ok := findCheck(checks, "relay:"+unreachableURI); ok {
		t.Errorf("unreachable backend should have no relay check, got %+v", rc)
	}
}

// TestDoctorNoBackendsConfigured covers the GR-F6 prerequisite: with valid UI
// settings but no backends registered, doctor fails the backends_config check
// with the "auto ui backends add" remediation hint and exits non-zero.
func TestDoctorNoBackendsConfigured(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, _, _, err := config.EnsureUISettings(); err != nil {
		t.Fatalf("EnsureUISettings: %v", err)
	}

	checks, stdout, err := runDoctor(t)
	if err == nil {
		t.Fatalf("doctor returned nil, want non-zero exit when no backends configured\nstdout: %s", stdout)
	}
	bc, ok := findCheck(checks, "backends_config")
	if !ok || bc.Status != "fail" {
		t.Fatalf("backends_config check = %+v (ok=%v), want fail", bc, ok)
	}
	if !strings.Contains(bc.Hint, "auto ui backends add") {
		t.Errorf("backends_config hint = %q, want it to mention 'auto ui backends add'", bc.Hint)
	}
}
