package writer

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockName is the lock file held for the duration of a write phase.
const LockName = ".write.lock"

// lockOutput takes an exclusive advisory lock over the whole output tree and
// returns the function that releases it.
//
// Writing a partition is read-merge-write, which is only safe against one writer
// at a time: two overlapping runs would each read the same partition, each merge
// their own rows into it, and the second rename would drop the first one's work.
// That matters here because `auto etl run` is on a */10 cron and can overlap a
// manual run trivially.
//
// The lock blocks rather than failing. A run that waits a few seconds for its
// turn is correct; a run that gives up has silently skipped an ingestion cycle,
// which is the class of quiet failure this package is trying to leave behind.
// flock is released by the kernel when the fd closes or the process dies, so a
// crashed run cannot strand it.
func lockOutput(outputDir string) (func(), error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(outputDir, LockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("flock %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
