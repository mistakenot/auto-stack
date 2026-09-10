package lock

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

// storeVersion is the schema version written to locks.json (D-8).
const storeVersion = 1

// storeFile is the on-disk shape of ~/.auto/lock/locks.json.
type storeFile struct {
	Version int          `json:"version"`
	Locks   []Lock       `json:"locks"`
	Audit   []AuditEntry `json:"audit"`
}

// Store is the host-global lock store: locks.json plus a flock sidecar in Dir.
// Every read-modify-write runs under a blocking exclusive flock on the sidecar
// and persists via an atomic rename, so concurrent Workers never lose an update
// or observe a torn file (D-8). Each caller opens its own handle; the type
// holds no state beyond the directory and the liveness probes.
//
// Liveness (D-3, AC-10): a Lock whose holder's liveness token is gone is
// reclaimed as a side effect of List and Take — a worktree holder whose
// worktree_path no longer exists on disk, or an agent holder whose tmux pane
// is absent from the tmux server. Only locks taken on this Host are probed;
// NTM-label and override holders carry no liveness token and are never
// reclaimed. The probes are fields so tests can simulate a dead holder and a
// present-but-idle one without a real worktree or tmux server; nil means the
// real probe.
type Store struct {
	Dir string

	// Host is the host id whose locks may be reclaimed; "" resolves to this
	// host's id at probe time.
	Host string
	// WorktreeExists reports whether a worktree holder's path is still on
	// disk. nil → os.Stat.
	WorktreeExists func(path string) bool
	// TmuxPanes lists every pane id (%N) in the tmux server. It must return
	// an error whenever the answer is uncertain (tmux missing, server down,
	// timeout) — an error means every pane holder is treated as live, so a
	// probe failure can never reclaim a lock. nil → `tmux list-panes -a`.
	TmuxPanes func() ([]string, error)
}

// tmuxQueryTimeout bounds the pane probe. List runs in the hook's critical
// path; the local tmux socket answers in well under a millisecond, but a
// wedged server must never stall the guard — and a timeout counts as
// uncertain, so it reclaims nothing.
const tmuxQueryTimeout = 200 * time.Millisecond

// listTmuxPanes is the real pane probe: `tmux list-panes -a -F '#{pane_id}'`.
func listTmuxPanes() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxQueryTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "list-panes", "-a", "-F", "#{pane_id}").Output()
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// worktreeExists is the real worktree probe.
func worktreeExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// OpenDefault returns the store at ~/.auto/lock.
func OpenDefault() (*Store, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return nil, err
	}
	return NewStore(filepath.Join(autoDir, "lock")), nil
}

// NewStore returns a store rooted at dir (tests point this at a temp dir).
func NewStore(dir string) *Store {
	return &Store{Dir: dir}
}

// Path returns the locks.json path.
func (s *Store) Path() string {
	return filepath.Join(s.Dir, "locks.json")
}

func (s *Store) lockPath() string {
	return filepath.Join(s.Dir, "locks.lock")
}

// HeldError is returned by Take when the Group is already held by a different
// Worker; Lock carries the current holder for the caller's message.
type HeldError struct {
	Lock Lock
}

func (e *HeldError) Error() string {
	return fmt.Sprintf("lock %q on project %q is held by %s", e.Lock.Group, e.Lock.Project, DescribeHolder(e.Lock.Holder))
}

// DescribeHolder renders a Holder for messages: the branch for a worktree
// holder, otherwise its worker id, plus the kind.
func DescribeHolder(h Holder) string {
	switch h.Kind {
	case KindWorktree:
		return "branch " + h.Branch
	case KindOverride, KindAgent:
		return "worker " + h.WorkerID
	default:
		if h.Branch != "" {
			return "branch " + h.Branch
		}
		return "worker " + h.WorkerID
	}
}

// withLock runs fn under a blocking exclusive flock on the sidecar file — the
// registry.withLock pattern. fn must do the whole read → mutate → write cycle
// inside the closure.
func (s *Store) withLock(fn func() error) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("create lock store dir: %w", err)
	}
	f, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock sidecar: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire lock sidecar: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}

// read loads locks.json; a missing file is an empty store. Must be called
// under withLock.
func (s *Store) read() (*storeFile, error) {
	data, err := os.ReadFile(s.Path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &storeFile{Version: storeVersion}, nil
		}
		return nil, fmt.Errorf("read lock store: %w", err)
	}
	var sf storeFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("parse lock store %s: %w", s.Path(), err)
	}
	if sf.Version == 0 {
		sf.Version = storeVersion
	}
	return &sf, nil
}

// write persists locks.json atomically. Must be called under withLock.
func (s *Store) write(sf *storeFile) error {
	if sf.Locks == nil {
		sf.Locks = []Lock{}
	}
	if sf.Audit == nil {
		sf.Audit = []AuditEntry{}
	}
	return sharedconfig.WriteJSONFileAtomic(s.Path(), sf)
}

// Take acquires group on project for w, recording w's full Holder (kind, host,
// branch, worktree path, worker id, session id for display) and reason. It is
// idempotent when w already holds it (the existing Lock is returned unchanged,
// never duplicated) and returns a *HeldError when a different Worker holds it.
func (s *Store) Take(project, group string, w Worker, reason string) (Lock, error) {
	var result Lock
	err := s.withLock(func() error {
		sf, err := s.read()
		if err != nil {
			return err
		}
		s.reclaim(sf)
		for i := range sf.Locks {
			l := &sf.Locks[i]
			if l.Project != project || l.Group != group {
				continue
			}
			if w.Matches(l.Holder) {
				result = *l
				return nil
			}
			return &HeldError{Lock: *l}
		}
		result = Lock{
			Project: project,
			Group:   group,
			Holder:  w.Holder,
			Reason:  reason,
			TakenAt: time.Now().UTC().Format(time.RFC3339),
		}
		sf.Locks = append(sf.Locks, result)
		return s.write(sf)
	})
	return result, err
}

// List returns the locks for project, or every lock when project is "". Locks
// whose holder is no longer live are reclaimed first (and persisted), so a
// dead holder never shows up as held — this is the lookup the guard uses.
func (s *Store) List(project string) ([]Lock, error) {
	var out []Lock
	err := s.withLock(func() error {
		sf, err := s.read()
		if err != nil {
			return err
		}
		if s.reclaim(sf) {
			if err := s.write(sf); err != nil {
				return err
			}
		}
		for i := range sf.Locks {
			if project == "" || sf.Locks[i].Project == project {
				out = append(out, sf.Locks[i])
			}
		}
		return nil
	})
	return out, err
}

// reclaim drops every lock on this host whose holder's liveness token is gone
// (D-3), appending a reclaimed audit entry per lock, and reports whether it
// changed sf. Must be called under withLock; the caller persists. The tmux
// server is asked at most once per pass, and only if a pane holder exists.
func (s *Store) reclaim(sf *storeFile) bool {
	host := s.Host
	if host == "" {
		host = sharedconfig.HostIDQuietly()
	}
	var panes []string
	panesKnown, panesAsked := false, false
	kept := sf.Locks[:0]
	changed := false
	for i := range sf.Locks {
		l := sf.Locks[i]
		reason := ""
		if l.Holder.Host == host {
			switch l.Holder.Kind {
			case KindWorktree:
				if l.Holder.WorktreePath != "" && !s.worktreeExists(l.Holder.WorktreePath) {
					reason = "worktree " + l.Holder.WorktreePath + " no longer exists"
				}
			case KindAgent:
				if strings.HasPrefix(l.Holder.WorkerID, "%") {
					if !panesAsked {
						panesAsked = true
						if p, err := s.tmuxPanes(); err == nil {
							panes, panesKnown = p, true
						}
					}
					if panesKnown && !slices.Contains(panes, l.Holder.WorkerID) {
						reason = "tmux pane " + l.Holder.WorkerID + " is gone"
					}
				}
			}
		}
		if reason == "" {
			kept = append(kept, l)
			continue
		}
		changed = true
		sf.Audit = append(sf.Audit, AuditEntry{
			At:      time.Now().UTC().Format(time.RFC3339),
			Action:  AuditReclaimed,
			Project: l.Project,
			Group:   l.Group,
			Holder:  l.Holder,
			By:      Holder{Host: host},
			Note:    reason,
		})
	}
	sf.Locks = kept
	return changed
}

func (s *Store) worktreeExists(path string) bool {
	if s.WorktreeExists != nil {
		return s.WorktreeExists(path)
	}
	return worktreeExists(path)
}

func (s *Store) tmuxPanes() ([]string, error) {
	if s.TmuxPanes != nil {
		return s.TmuxPanes()
	}
	return listTmuxPanes()
}

// Release frees the locks w holds on its project: the one named group, or —
// when group is "" — every one, which is what `auto lock release` runs at
// merge time. It returns how many were released; releasing nothing is not an
// error. Locks held by any other Worker, on this host or another, are never
// touched, and a Worker's locks on a different project are left alone too
// (an override id such as worker-a may be reused across projects).
func (s *Store) Release(w Worker, group string) (int, error) {
	released := 0
	err := s.withLock(func() error {
		sf, err := s.read()
		if err != nil {
			return err
		}
		kept := sf.Locks[:0]
		for i := range sf.Locks {
			l := &sf.Locks[i]
			mine := l.Project == w.Project && (group == "" || l.Group == group) && w.Matches(l.Holder)
			if mine {
				released++
				continue
			}
			kept = append(kept, *l)
		}
		if released == 0 {
			return nil
		}
		sf.Locks = kept
		return s.write(sf)
	})
	if err != nil {
		return 0, err
	}
	return released, nil
}

// GHChecker reports the pull request for a branch: its number and state
// (OPEN, CLOSED or MERGED as gh spells them). It returns ErrNoPR when the
// branch has no PR at all. Clear takes it as an interface so the CLI can use
// the real gh-backed checker and tests can script every state.
type GHChecker interface {
	PRState(branch string) (number, state string, err error)
}

// ErrNoPR is returned by a GHChecker when the branch has no pull request.
var ErrNoPR = errors.New("no pull request found")

// PRStateMerged is the gh state that lets Clear release without --force.
const PRStateMerged = "MERGED"

// GHCLI is the gh-backed GHChecker: `gh pr list --head <branch> --state all`
// run in Dir (any directory inside the repo, so gh resolves the remote).
type GHCLI struct {
	Dir string
}

// PRState implements GHChecker via the gh CLI.
func (g GHCLI) PRState(branch string) (string, string, error) {
	cmd := exec.Command("gh", "pr", "list", "--head", branch, "--state", "all", "--limit", "1", "--json", "number,state")
	cmd.Dir = g.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("gh pr list --head %s: %s", branch, firstNonEmpty(strings.TrimSpace(stderr.String()), err.Error()))
	}
	var prs []struct {
		Number int    `json:"number"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &prs); err != nil {
		return "", "", fmt.Errorf("parse gh pr list output: %w", err)
	}
	if len(prs) == 0 {
		return "", "", ErrNoPR
	}
	return strconv.Itoa(prs[0].Number), prs[0].State, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ClearResult is what Clear removed and why it was allowed to.
type ClearResult struct {
	Lock   Lock   `json:"lock"`
	Forced bool   `json:"forced"`
	PR     string `json:"pr,omitempty"`
	State  string `json:"state,omitempty"`
}

// Clear removes another Worker's lock on group (D-3, AC-9). Without force it
// releases only when gh reports the holder branch's PR as MERGED; an open PR,
// no PR, a gh failure, or a holder with no branch to look up (agent/override
// kinds) all refuse with a message that says why and names --force. With
// force the lock is removed regardless; the PR state is still recorded when
// gh can supply it. Every release appends a cleared audit entry naming the
// holder, by (the clearing Worker, or just its host), forced, pr and state.
// A refusal never writes the store. Clearing an unheld group is an error.
func (s *Store) Clear(project, group string, by Holder, force bool, gh GHChecker) (ClearResult, error) {
	var result ClearResult
	err := s.withLock(func() error {
		sf, err := s.read()
		if err != nil {
			return err
		}
		idx := slices.IndexFunc(sf.Locks, func(l Lock) bool { return l.Project == project && l.Group == group })
		if idx < 0 {
			return fmt.Errorf("lock %q on project %q is not held — nothing to clear", group, project)
		}
		l := sf.Locks[idx]
		result = ClearResult{Lock: l, Forced: force}

		if l.Holder.Kind == KindWorktree && l.Holder.Branch != "" {
			pr, state, err := gh.PRState(l.Holder.Branch)
			switch {
			case err == nil:
				result.PR, result.State = pr, state
				if state != PRStateMerged && !force {
					return fmt.Errorf("holder branch %s PR #%s is still %s — not cleared (use --force if confirmed merged)", l.Holder.Branch, pr, state)
				}
			case errors.Is(err, ErrNoPR):
				if !force {
					return fmt.Errorf("no PR found for holder branch %s — not cleared (use --force once you have confirmed that work is done)", l.Holder.Branch)
				}
			default:
				if !force {
					return fmt.Errorf("cannot verify holder branch %s: %w — not cleared (use --force once you have confirmed its PR is merged)", l.Holder.Branch, err)
				}
			}
		} else if !force {
			return fmt.Errorf("holder %s (%s) has no branch, so there is no PR to verify — not cleared (use --force once you have confirmed it is done)", DescribeHolder(l.Holder), l.Holder.Kind)
		}

		sf.Locks = slices.Delete(sf.Locks, idx, idx+1)
		sf.Audit = append(sf.Audit, AuditEntry{
			At:      time.Now().UTC().Format(time.RFC3339),
			Action:  AuditCleared,
			Project: project,
			Group:   group,
			Holder:  l.Holder,
			By:      by,
			Forced:  force,
			PR:      result.PR,
			State:   result.State,
		})
		return s.write(sf)
	})
	return result, err
}
