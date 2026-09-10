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

// ErrUnresolved is returned when no step of the identity chain applies. Phase 2
// completes the chain with the pane (TMUX_PANE / NTM_*) and bare-main (D-6)
// steps; until then the guard fails open on this error and the CLI reports it.
var ErrUnresolved = errors.New("lock: cannot resolve a Worker identity: not in a linked git worktree and " + WorkerEnv + " is not set (work in a worktree, or export " + WorkerEnv + "=<id>)")

// Repo is the git context a Worker acts in: the worktree root and the project
// id the store keys on.
type Repo struct {
	Root    string
	Project string
	Branch  string
}

// ResolveRepo resolves the worktree root, branch and project id for cwd the
// same way the hook adapter does: by normalized origin remote in the project
// registry (which follows worktrees), then by path, and finally — so an
// unregistered repo still locks consistently — the repo root path itself.
// Returns an error only when cwd is not inside a git worktree.
func ResolveRepo(cwd string) (Repo, error) {
	root, branch, _, err := sharedgit.Provenance(cwd)
	if err != nil || root == "" {
		return Repo{}, fmt.Errorf("lock: %s is not inside a git repository", cwd)
	}
	r := Repo{Root: root, Branch: branch}

	registry := loadRegistryQuietly()
	if remote, _ := sharedgit.OriginRemote(cwd); remote != "" {
		if p := registry.FindProjectByRemote(sharedgit.NormalizeRemoteURL(remote)); p != nil {
			r.Project = p.ID
		}
	}
	if r.Project == "" {
		if p := registry.FindProjectByPath(root); p != nil {
			r.Project = p.ID
		}
	}
	if r.Project == "" {
		r.Project = root
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
//  3. Shared/main checkout → TMUX_PANE / NTM_* pane (Phase 2).
//  4. Bare → block with remediation (D-6, Phase 2).
//
// identity is the config's Identity mode (auto|worktree|agent; "" = auto).
// payload is the hook payload when called from the guard (session_id is copied
// for display only — it can never key identity, see D-1) and nil from the CLI.
func ResolveWorker(cwd string, payload map[string]any, identity string) (Worker, error) {
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

	if id := strings.TrimSpace(os.Getenv(WorkerEnv)); id != "" {
		w.Kind = KindOverride
		w.WorkerID = id
		return w, nil
	}

	if identity == "" {
		identity = IdentityAuto
	}
	if identity != IdentityAgent {
		linked, err := isLinkedWorktree(repo.Root)
		if err != nil {
			return Worker{}, err
		}
		if linked {
			w.Kind = KindWorktree
			w.WorktreePath = repo.Root
			return w, nil
		}
	}

	// Steps 3 (pane) and 4 (bare) land in Phase 2.
	return Worker{}, ErrUnresolved
}

// isLinkedWorktree reports whether root is a linked worktree rather than the
// main one: `git worktree list --porcelain` lists the main worktree first, so
// linked ⇔ root differs from that first entry.
func isLinkedWorktree(root string) (bool, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("git worktree list: %s", strings.TrimSpace(stderr.String()))
	}
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		if main, ok := strings.CutPrefix(line, "worktree "); ok {
			return !samePath(main, root), nil
		}
	}
	return false, errors.New("git worktree list: no worktree entries")
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
