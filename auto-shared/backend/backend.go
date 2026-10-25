// Package backend is the stdlib-only execution port for auto-stack. The
// `auto watch` daemon (and, later, other callers) drive a run through a
// Backend adapter rather than talking to tmux directly: Start a run, ask its
// Status, Stop it. A run is addressed by an opaque Binding — the same
// (manager, target) pair auto-mail uses — so an ntm or herdr adapter can be
// added as one new file with no daemon changes.
//
// This package is deliberately stdlib-only: auto-shared/go.mod carries no
// `require` directive (see auto-shared/rpc/layering_test.go TestLayeringGate),
// so nothing here may import a third-party dependency.
package backend

import (
	"context"
	"errors"
	"sort"
)

// ErrStartUncertain is returned by Start when the adapter created a run but
// could not confirm whether the launched command is alive — for example a
// configuration step failed and the follow-up kill could not be confirmed. The
// returned Binding is still valid: the caller must record it, keep the run
// running, and let Status and the deadline own it from there, so a possibly
// live command is never re-dispatched.
var ErrStartUncertain = errors.New("backend start outcome uncertain")

// Binding is the opaque physical pair that says where a run executes: a manager
// (tmux, ntm, herdr) and a manager-specific target (a tmux session name, an ntm
// label, …). It is the same vocabulary auto-mail uses for a Subscription's
// Binding. Nothing physical beyond this pair is derivable from it.
type Binding struct {
	Manager string
	Target  string
}

// StartSpec is everything an adapter needs to launch one run.
type StartSpec struct {
	// Name is the manager-specific target name for the run (e.g. the tmux
	// session name). The returned Binding's Target echoes it.
	Name string
	// WorkDir is the working directory the launch script cd's into.
	WorkDir string
	// LaunchScript is the path to the wrapper script produced by
	// WriteLaunchScript. The adapter executes this; it owns the completion
	// contract (output log + atomic exit-code file).
	LaunchScript string
	// RunDir is the run's runtime directory. Status reads its exit-code file.
	RunDir string
	// Owner tags the run so a future reconcile can tell whose run a stray
	// session is (Q-3). Adapters that can tag (tmux's @auto_owner) should.
	Owner string
}

// StatusState is the liveness verdict for a run.
type StatusState int

const (
	// StatusRunning means the run's command is still executing.
	StatusRunning StatusState = iota
	// StatusExited means the launch wrapper recorded completion. ExitCode
	// carries the code, or is nil when the exit-code file was present but
	// empty or malformed ("exited, code unknown").
	StatusExited
	// StatusLost means no exit was recorded and the manager has no live
	// session for the run — the run vanished without completing.
	StatusLost
)

// Status is the result of asking a Backend about a run.
type Status struct {
	State StatusState
	// ExitCode is set only when State is StatusExited and the code is known.
	ExitCode *int
}

// Backend is the execution port. An adapter implements it over one manager.
type Backend interface {
	// Name is the manager name an adapter registers under (e.g. "tmux").
	Name() string
	// Start launches the run described by spec and returns its Binding. On a
	// confirmed failure it returns a non-nil error and the launched session (if
	// any) has been cleaned up. On an uncertain outcome it returns the Binding
	// together with ErrStartUncertain.
	Start(ctx context.Context, spec *StartSpec) (Binding, error)
	// Status reports the run's liveness. It checks the completion contract
	// (the exit-code file under runDir) first, and asks the manager about
	// session liveness only when no exit was recorded.
	Status(ctx context.Context, binding Binding, runDir string) (Status, error)
	// Stop terminates the run's session. Stopping an already-absent session is
	// not an error.
	Stop(ctx context.Context, binding Binding) error
}

// TextSender is an optional capability for adapters over interactive panes:
// sending a line of text to a live run. It is discovered by type assertion;
// only managers that can accept input (ntm, herdr) implement it. It is
// deliberately not named Sender, which collides with mail.Sender (caller
// identity).
type TextSender interface {
	SendText(ctx context.Context, binding Binding, text string) error
}

// Registry resolves a backend by name. The empty name resolves to the default
// backend, "tmux", so existing configs with no backend field keep working.
type Registry struct {
	byName map[string]Backend
}

// DefaultBackendName is the backend a run uses when none is specified.
const DefaultBackendName = "tmux"

// NewRegistry builds a registry from the given backends, keyed by Name().
func NewRegistry(backends ...Backend) *Registry {
	r := &Registry{byName: make(map[string]Backend, len(backends))}
	for _, b := range backends {
		if b != nil {
			r.byName[b.Name()] = b
		}
	}
	return r
}

// Get resolves a backend by name. An empty name resolves to DefaultBackendName.
// The bool reports whether a backend was found.
func (r *Registry) Get(name string) (Backend, bool) {
	if name == "" {
		name = DefaultBackendName
	}
	b, ok := r.byName[name]
	return b, ok
}

// Has reports whether a backend with the given name is registered. The empty
// name is treated as DefaultBackendName.
func (r *Registry) Has(name string) bool {
	_, ok := r.Get(name)
	return ok
}

// Names returns the registered backend names, sorted.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
