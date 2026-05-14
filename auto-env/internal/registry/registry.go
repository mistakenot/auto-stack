package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mistakenot/auto-shared/config"
)

type Entry struct {
	RepoRoot   string         `json:"repo_root"`
	Branch     string         `json:"branch"`
	BranchSlug string         `json:"branch_slug"`
	Slot       int            `json:"slot"`
	Ports      map[string]int `json:"ports"`
	Files      []string       `json:"files"`
	CreatedAt  time.Time      `json:"created_at"`
}

type Registry struct {
	Dir string
}

func Default() (*Registry, error) {
	autoDir, err := config.AutoDir()
	if err != nil {
		return nil, err
	}
	return &Registry{Dir: filepath.Join(autoDir, "env")}, nil
}

func (r *Registry) path() string {
	return filepath.Join(r.Dir, "environments.json")
}

func (r *Registry) lockPath() string {
	return filepath.Join(r.Dir, "environments.lock")
}

func (r *Registry) withLock(fn func() error) error {
	if err := os.MkdirAll(r.Dir, 0755); err != nil {
		return fmt.Errorf("create registry dir: %w", err)
	}
	f, err := os.OpenFile(r.lockPath(), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}

func (r *Registry) readEntries() ([]Entry, error) {
	data, err := os.ReadFile(r.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	return entries, nil
}

func (r *Registry) writeEntries(entries []Entry) error {
	return config.WriteJSONFile(r.path(), entries)
}

func (r *Registry) Add(entry *Entry) error {
	return r.withLock(func() error {
		entries, err := r.readEntries()
		if err != nil {
			return err
		}
		found := false
		for i, e := range entries {
			if e.RepoRoot == entry.RepoRoot {
				entries[i] = *entry
				found = true
				break
			}
		}
		if !found {
			entries = append(entries, *entry)
		}
		return r.writeEntries(entries)
	})
}

func (r *Registry) Remove(repoRoot string) error {
	return r.withLock(func() error {
		entries, err := r.readEntries()
		if err != nil {
			return err
		}
		filtered := entries[:0]
		for _, e := range entries {
			if e.RepoRoot != repoRoot {
				filtered = append(filtered, e)
			}
		}
		return r.writeEntries(filtered)
	})
}

func (r *Registry) List() ([]Entry, error) {
	var entries []Entry
	err := r.withLock(func() error {
		var e error
		entries, e = r.readEntries()
		return e
	})
	if entries == nil {
		entries = []Entry{}
	}
	return entries, err
}
