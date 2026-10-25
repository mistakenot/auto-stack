// Package backendtest provides a scriptable fake backend.Backend for tests. It
// replaces the ad-hoc per-test fakes that implemented the old tmux-shaped
// interface, and lets a test drive a run's Status (running / exited N / lost)
// and inject errors from Start, Status and Stop without needing tmux.
package backendtest

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/mistakenot/auto-shared/backend"
)

// Fake is a scriptable backend.Backend. The zero value is usable: it keeps runs
// running (it writes no exit-code file) and records Start/Stop calls. Set
// ExitCode to make Start record a completion through the shared contract (an
// exit-code file in the run dir), or Lost to make Status report a vanished run.
type Fake struct {
	// NameVal is the registry name. Empty means "tmux" so it resolves as the
	// default backend for runs that set no backend field.
	NameVal string

	mu sync.Mutex
	// exitCode, when non-nil, is written to the run's exit-code file on Start,
	// so Status reports the run exited with that code.
	exitCode *int
	// lost makes Status report StatusLost when no exit was recorded.
	lost bool
	// startErr/statusErr/stopErr are injected per-call errors.
	startErr  error
	statusErr error
	stopErr   error
	// startBinding, when set, is returned by Start together with startErr
	// (used to model ErrStartUncertain, which carries a Binding).
	startBinding *backend.Binding

	starts []backend.StartSpec
	stops  []backend.Binding
}

// New returns a Fake that completes every run it starts with the given exit
// code (the common "run succeeds / fails with code N" case).
func New(exitCode int) *Fake {
	f := &Fake{}
	f.SetExit(exitCode)
	return f
}

// Name implements backend.Backend. Empty NameVal resolves to "tmux".
func (f *Fake) Name() string {
	if f.NameVal == "" {
		return backend.DefaultBackendName
	}
	return f.NameVal
}

// SetExit makes Start record a completion with the given exit code.
func (f *Fake) SetExit(code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := code
	f.exitCode = &c
	f.lost = false
}

// SetRunning makes Start leave the run running (no exit-code file written).
func (f *Fake) SetRunning() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exitCode = nil
	f.lost = false
}

// SetLost makes Status report the run as lost (no exit recorded, no session).
func (f *Fake) SetLost() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exitCode = nil
	f.lost = true
}

// SetStartErr injects a confirmed Start failure.
func (f *Fake) SetStartErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startErr = err
	f.startBinding = nil
}

// SetStartUncertain injects a Start failure that returns the given binding (to
// model ErrStartUncertain, which hands the caller a Binding to keep).
func (f *Fake) SetStartUncertain(err error, b backend.Binding) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startErr = err
	bb := b
	f.startBinding = &bb
}

// SetStatusErr injects a Status error.
func (f *Fake) SetStatusErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusErr = err
}

// SetStopErr injects a Stop error.
func (f *Fake) SetStopErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopErr = err
}

// Start implements backend.Backend.
func (f *Fake) Start(_ context.Context, spec *backend.StartSpec) (backend.Binding, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, *spec)
	if f.startErr != nil {
		if f.startBinding != nil {
			return *f.startBinding, f.startErr
		}
		return backend.Binding{}, f.startErr
	}
	if f.exitCode != nil && spec.RunDir != "" {
		path := filepath.Join(spec.RunDir, backend.ExitCodeFile)
		_ = os.WriteFile(path, []byte(strconv.Itoa(*f.exitCode)+"\n"), 0o644) //nolint:gosec // test artifact
	}
	return backend.Binding{Manager: f.Name(), Target: spec.Name}, nil
}

// Status implements backend.Backend. It checks the exit-code file first (so a
// test that writes one, or a Start that recorded one, reports exited), then the
// scripted lost/running state.
func (f *Fake) Status(_ context.Context, _ backend.Binding, runDir string) (backend.Status, error) {
	f.mu.Lock()
	lost := f.lost
	statusErr := f.statusErr
	f.mu.Unlock()
	if statusErr != nil {
		return backend.Status{}, statusErr
	}
	code, exited, err := backend.ReadExit(runDir)
	if err != nil {
		return backend.Status{}, err
	}
	if exited {
		return backend.Status{State: backend.StatusExited, ExitCode: code}, nil
	}
	if lost {
		return backend.Status{State: backend.StatusLost}, nil
	}
	return backend.Status{State: backend.StatusRunning}, nil
}

// Stop implements backend.Backend.
func (f *Fake) Stop(_ context.Context, binding backend.Binding) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops = append(f.stops, binding)
	return f.stopErr
}

// Starts returns a copy of the recorded Start specs.
func (f *Fake) Starts() []backend.StartSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]backend.StartSpec(nil), f.starts...)
}

// StartCount returns how many times Start was called.
func (f *Fake) StartCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.starts)
}

// StopCount returns how many times Stop was called.
func (f *Fake) StopCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.stops)
}
