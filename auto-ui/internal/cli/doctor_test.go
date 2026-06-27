package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

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
