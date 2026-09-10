// Package lock implements opt-in, hook-enforced serial-update locks: a project
// declares lockable Groups of files in .auto/lock/settings.json, a Worker takes
// a Lock on a Group before editing, and the PreToolUse guard denies edits to a
// Group's files by anyone but the holder. The store is host-global at
// ~/.auto/lock/locks.json.
//
// This package is a pure library — no cobra, no stdout — consumed by both the
// `auto lock` command and the `auto hooks fire` guard so the two always agree
// on config, identity and store semantics.
package lock

// Identity modes accepted by Config.Identity (D-1).
const (
	IdentityAuto     = "auto"
	IdentityWorktree = "worktree"
	IdentityAgent    = "agent"
)

// Holder kinds recorded on a Lock (D-1).
const (
	KindOverride = "override" // AUTO_LOCK_WORKER env / take --as
	KindWorktree = "worktree" // linked git worktree: branch + worktree_path
	KindAgent    = "agent"    // shared checkout: TMUX_PANE / NTM_* label
)

// Group is one lockable unit: a named set of repo-relative globs plus the
// human-facing reason the files must be edited serially (D-2, D-9).
type Group struct {
	Name        string   `json:"name"`
	Globs       []string `json:"globs"`
	Description string   `json:"description"`
}

// Config is the project-local opt-in at <repo>/.auto/lock/settings.json.
type Config struct {
	// Identity selects holder granularity: auto (default), worktree or agent.
	Identity string  `json:"identity,omitempty"`
	Groups   []Group `json:"groups"`
}

// Holder is the on-disk identity of the Worker that took a Lock. Kind selects
// which discriminator fields are meaningful; the rest are display-only.
type Holder struct {
	Kind         string `json:"kind"`
	Host         string `json:"host"`
	Branch       string `json:"branch,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	WorkerID     string `json:"worker_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
}

// Lock is one held Group: at most one per (Project, Group) in the store (D-8).
type Lock struct {
	Project string `json:"project"`
	Group   string `json:"group"`
	Holder  Holder `json:"holder"`
	Reason  string `json:"reason,omitempty"`
	PR      string `json:"pr,omitempty"`
	TakenAt string `json:"taken_at"`
}

// Audit actions recorded on the store (D-3).
const (
	AuditCleared   = "cleared"   // auto lock clear: merge-verified, or --force
	AuditReclaimed = "reclaimed" // holder liveness token gone (worktree path / tmux pane)
)

// AuditEntry records a clear / --force / liveness reclaim on the store
// (append-only). Holder is the Worker whose Lock was removed; By is whoever
// removed it (the clearing Worker, or just the host for a reclaim).
type AuditEntry struct {
	At      string `json:"at"`
	Action  string `json:"action"`
	Project string `json:"project"`
	Group   string `json:"group"`
	Holder  Holder `json:"holder"`
	By      Holder `json:"by"`
	Forced  bool   `json:"forced,omitempty"`
	PR      string `json:"pr,omitempty"`
	State   string `json:"state,omitempty"`
	Note    string `json:"note,omitempty"`
}

// Worker is the resolved in-memory identity of the process asking to take,
// release or edit: the project it is in plus the Holder it would be recorded as.
type Worker struct {
	Project string
	Holder
}

// Matches reports whether h is this Worker: same host, same kind, and the same
// kind-specific discriminator (worker_id for override/agent, branch and
// worktree_path for worktree). Project is compared by the caller, which looks
// locks up by (project, group) before asking.
func (w Worker) Matches(h Holder) bool {
	if w.Host != h.Host || w.Kind != h.Kind {
		return false
	}
	switch w.Kind {
	case KindWorktree:
		return w.Branch == h.Branch && w.WorktreePath == h.WorktreePath
	default:
		return w.WorkerID == h.WorkerID
	}
}

// Decision is the guard verdict; Reason becomes permissionDecisionReason.
type Decision struct {
	Deny   bool
	Reason string
}
