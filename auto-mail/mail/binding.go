package mail

import (
	"path/filepath"

	"github.com/mistakenot/auto-shared/hooks"
)

// Binding managers. The pair is opaque by design (G12): a manager is a data
// value, not a type, so T3 adds `herdr` as another value plus an effector
// implementation rather than as a schema change.
const (
	// ManagerTmux targets a tmux pane by its rename-proof `%N` pane id.
	ManagerTmux = "tmux"
	// ManagerNTM targets a pane by the spawn batch and order NTM stamps.
	ManagerNTM = "ntm"
	// ManagerCwd is the fallback rung: the agent's resolved working directory.
	ManagerCwd = "cwd"
)

// BindingFor derives the caller's opaque (manager, target) pair from the same
// physical context the hook envelope already captures.
//
// Both `auto mail subscribe` and (from T3) the hook must compute the *same* pair
// from their own process, because that pair is the only join key they share:
// the subscribe process is a tool call and never sees the hook payload's
// session_id, and bridging that gap is exactly what D-13 defers to T2.
func BindingFor(cwd string) Binding {
	return BindingFromContext(hooks.CaptureContext(), cwd)
}

// BindingFromContext is BindingFor over an already-captured context map, which
// is what the hook has in hand. It walks the documented ladder (D-062-2):
//
//  1. tmux, targeting the `%N` pane id — stable across renames, and the handle
//     an effector addresses a pane by.
//  2. ntm, targeting the spawn batch and order, for a pane spawned by ntm with
//     no tmux server reachable.
//  3. cwd, targeting the resolved working directory.
//
// The cwd rung is a *fallback*, not the design: two agents sharing one checkout
// collide on a single binding, which is this host's normal state (see
// docs/postmortems/2026-06-28-shared-checkout-rogue-agent.md). It is what makes
// the harness scenario possible — a container has no tmux — and T3 replaces it
// for panes.
func BindingFromContext(captured map[string]string, cwd string) Binding {
	if pane := captured["tmux_pane_id"]; pane != "" {
		return Binding{Manager: ManagerTmux, Target: pane, Session: captured["tmux_session"]}
	}
	if batch := captured["NTM_SPAWN_BATCH_ID"]; batch != "" {
		target := batch
		if order := captured["NTM_SPAWN_ORDER"]; order != "" {
			target += "/" + order
		}
		return Binding{Manager: ManagerNTM, Target: target, Session: batch}
	}
	return Binding{Manager: ManagerCwd, Target: resolveDir(cwd)}
}

// resolveDir canonicalises a working directory so two spellings of one path
// (a relative cwd, a symlinked worktree) do not read as two agents. Resolution
// is best-effort: an unresolvable path is used as given rather than dropped,
// because a binding that is merely imprecise still beats none.
func resolveDir(cwd string) string {
	if cwd == "" {
		return ""
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	return filepath.Clean(cwd)
}
