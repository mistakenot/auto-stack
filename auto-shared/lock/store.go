package lock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
// holds no state beyond the directory.
type Store struct {
	Dir string
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

// List returns the locks for project, or every lock when project is "".
func (s *Store) List(project string) ([]Lock, error) {
	var out []Lock
	err := s.withLock(func() error {
		sf, err := s.read()
		if err != nil {
			return err
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
