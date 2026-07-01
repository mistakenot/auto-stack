package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// WithFileLock runs fn while holding an exclusive advisory lock on
// "<path>.lock", serializing read-modify-write cycles on path across processes.
// WriteJSONFileAtomic makes each individual write atomic but does NOT serialize
// concurrent load→mutate→store sequences, so two `add` runs can otherwise race
// and lose one writer's update (H2). Callers must reload the target file INSIDE
// fn — the lock is only held for the duration of fn.
//
// The lock file lives beside path and is created if absent; its parent directory
// is created too. The lock is released (and fn's error returned) when fn returns.
func WithFileLock(path string, fn func() error) error {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return fmt.Errorf("create lock dir for %s: %w", path, err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open lock %s: %w", lockPath, err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire lock %s: %w", lockPath, err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}
