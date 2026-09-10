package lock

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	sharedconfig "github.com/mistakenot/auto-shared/config"
	sharedgit "github.com/mistakenot/auto-shared/git"
)

// WorkerEnv is the env override that pins a Worker id (D-1 step 1). It is
// visible to both the Bash-run CLI and the hook subprocess, so both resolve the
// same key.
const WorkerEnv = "AUTO_LOCK_WORKER"

// Pane discriminators consulted on a shared checkout (D-1 step 3), in the
// order they are tried. TMUX_PANE (e.g. %3) is the stable, rename-proof pane
// handle; the NTM spawn batch + order pair identifies a pane ntm spawned when
// no tmux server is reachable — the same ladder auto-mail binds replies with.
const (
	tmuxPaneEnv     = "TMUX_PANE"
	ntmBatchEnv     = "NTM_SPAWN_BATCH_ID"
	ntmOrderEnv     = "NTM_SPAWN_ORDER"
	ntmWorkerPrefix = "ntm:"
)

// IdentityEnv lists every environment variable the identity chain reads, in
// precedence order. An empty value counts as unset. Tests clear these so a
// process running under tmux/ntm resolves the same as a bare one; doctor
// reports them.
var IdentityEnv = []string{WorkerEnv, tmuxPaneEnv, ntmBatchEnv, ntmOrderEnv}

// BareError is the D-6 outcome: the chain reached its end without a
// discriminator, so co-located agents cannot be told apart. It is the ONE
// identity failure the guard blocks on rather than failing open, because
// silently treating the checkout as a single holder could let two
// indistinguishable agents both believe they hold a Lock.
type BareError struct {
	// Root is the checkout that could not be identified.
	Root string
	// Identity is the mode in force; it decides whether a linked worktree
	// would have helped.
	Identity string
}

func (e *BareError) Error() string {
	return "lock: cannot identify this Worker: " + e.Detail() + " " + e.Remediation()
}

// Detail says what was missing, for messages that add their own framing.
func (e *BareError) Detail() string {
	if e.Identity == IdentityAgent {
		return fmt.Sprintf("identity is %q and there is no tmux pane, NTM label or %s to tell agents apart.", IdentityAgent, WorkerEnv)
	}
	return fmt.Sprintf("%s is the main checkout and there is no tmux pane, NTM label or %s to tell co-located agents apart.", e.Root, WorkerEnv)
}

// Remediation is the D-6 instruction: a worktree (when the mode honours one)
// or an explicit worker id.
func (e *BareError) Remediation() string {
	if e.Identity == IdentityAgent {
		return "Export " + WorkerEnv + "=<name> and retry."
	}
	return "Work in a linked git worktree, or export " + WorkerEnv + "=<name> and retry."
}

// Repo is the git context a Worker acts in: the worktree root the caller is
// in, the main worktree it belongs to, and the project id the store keys on.
type Repo struct {
	Root    string
	Main    string
	Project string
	Branch  string
}

// Linked reports whether the caller is in a linked worktree rather than the
// main one (D-1 step 2).
func (r Repo) Linked() bool {
	return !samePath(r.Main, r.Root)
}

// ResolveRepo resolves the worktree root, main worktree, branch and project id
// for cwd the same way the hook adapter does: by normalized origin remote in
// the project registry (which follows worktrees), then by path (the worktree,
// then its main checkout), and finally — so an unregistered repo still locks
// consistently — the main worktree path, which every linked worktree of the
// repo shares. Returns an error only when cwd is not inside a git worktree.
func ResolveRepo(cwd string) (Repo, error) {
	root, branch, _, err := sharedgit.Provenance(cwd)
	if err != nil || root == "" {
		return Repo{}, fmt.Errorf("lock: %s is not inside a git repository", cwd)
	}
	main, err := mainWorktree(root)
	if err != nil {
		return Repo{}, err
	}
	r := Repo{Root: root, Main: main, Branch: branch}

	registry := loadRegistryQuietly()
	if remote, _ := sharedgit.OriginRemote(cwd); remote != "" {
		if p := registry.FindProjectByRemote(sharedgit.NormalizeRemoteURL(remote)); p != nil {
			r.Project = p.ID
		}
	}
	for _, path := range []string{root, main} {
		if r.Project != "" {
			break
		}
		if p := registry.FindProjectByPath(path); p != nil {
			r.Project = p.ID
		}
	}
	if r.Project == "" {
		r.Project = main
	}
	return r, nil
}

// loadRegistryQuietly returns the host project registry, or an empty one when
// it is absent or unreadable — never an error (this runs in the hook hot path).
func loadRegistryQuietly() sharedconfig.ProjectsConfig {
	path, err := sharedconfig.ProjectsConfigPath()
	if err != nil {
		return sharedconfig.ProjectsConfig{}
	}
	cfg, err := sharedconfig.LoadProjects(path)
	if err != nil {
		return sharedconfig.ProjectsConfig{}
	}
	return cfg
}

// ResolveWorker resolves the Worker for cwd through the D-1 chain, first match
// wins:
//  1. AUTO_LOCK_WORKER env → kind override, worker_id = its value (in any mode).
//  2. Linked git worktree (mode auto or worktree) → kind worktree, keyed on
//     branch + worktree path.
//  3. Shared/main checkout (any mode; a linked worktree too in mode agent) →
//     kind agent, keyed on TMUX_PANE, else the NTM spawn batch/order label.
//  4. Bare → *BareError with the D-6 remediation.
//
// Modes auto and worktree coincide: both key a linked worktree on its branch
// and both still need a pane on the main checkout, because the D-1 invariant —
// never silently collapse indistinguishable agents — rules out treating the
// shared checkout as one holder. Mode agent skips step 2 so even worktree
// occupants are told apart per pane.
//
// identity is the config's Identity mode (auto|worktree|agent; "" = auto).
// payload is the hook payload when called from the guard (session_id is copied
// for display only — it can never key identity, see D-1) and nil from the CLI.
func ResolveWorker(cwd string, payload map[string]any, identity string) (Worker, error) {
	return ResolveWorkerAs(cwd, payload, identity, "")
}

// ResolveWorkerAs is ResolveWorker with an explicit override (take --as) that
// takes the place of AUTO_LOCK_WORKER for this call only; "" defers to the env.
func ResolveWorkerAs(cwd string, payload map[string]any, identity, override string) (Worker, error) {
	repo, err := ResolveRepo(cwd)
	if err != nil {
		return Worker{}, err
	}
	w := Worker{Project: repo.Project}
	w.Host = sharedconfig.HostIDQuietly()
	w.Branch = repo.Branch
	if payload != nil {
		if sid, ok := payload["session_id"].(string); ok {
			w.SessionID = sid
		}
	}

	if id := strings.TrimSpace(override); id != "" {
		w.Kind = KindOverride
		w.WorkerID = id
		return w, nil
	}
	if id := envValue(WorkerEnv); id != "" {
		w.Kind = KindOverride
		w.WorkerID = id
		return w, nil
	}

	if identity == "" {
		identity = IdentityAuto
	}
	if identity != IdentityAgent && repo.Linked() {
		w.Kind = KindWorktree
		w.WorktreePath = repo.Root
		return w, nil
	}

	if id := paneID(); id != "" {
		w.Kind = KindAgent
		w.WorkerID = id
		return w, nil
	}

	return Worker{}, &BareError{Root: repo.Root, Identity: identity}
}

// paneID returns the shared-checkout discriminator: the tmux pane id, else
// "ntm:<batch>/<order>" (or "ntm:<batch>" when no order is set), else "".
func paneID() string {
	if pane := envValue(tmuxPaneEnv); pane != "" {
		return pane
	}
	batch := envValue(ntmBatchEnv)
	if batch == "" {
		return ""
	}
	if order := envValue(ntmOrderEnv); order != "" {
		return ntmWorkerPrefix + batch + "/" + order
	}
	return ntmWorkerPrefix + batch
}

// envValue reads an identity variable, treating whitespace-only as unset so a
// test or wrapper that exports KEY= reads the same as one that never set it.
func envValue(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// mainWorktree returns the main worktree of the repo root belongs to: `git
// worktree list --porcelain` lists it first. It is the one path every linked
// worktree of a repo shares, and root ≠ main ⇔ root is a linked worktree.
func mainWorktree(root string) (string, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git worktree list: %s", strings.TrimSpace(stderr.String()))
	}
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		if main, ok := strings.CutPrefix(line, "worktree "); ok {
			return main, nil
		}
	}
	return "", errors.New("git worktree list: no worktree entries")
}

// samePath compares two paths after cleaning and resolving symlinks, so a
// temp-dir alias never masquerades as a different worktree.
func samePath(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = filepath.Clean(a)
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = filepath.Clean(b)
	}
	return ra == rb
}
