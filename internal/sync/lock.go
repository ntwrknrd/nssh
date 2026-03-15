package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// AcquireSourceLock acquires an advisory file lock for the named source.
// Returns an unlock function and nil on success.
// Returns an error if the lock is already held by another process.
func AcquireSourceLock(source string) (unlock func(), err error) {
	dir := syncLockDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}

	lockPath := filepath.Join(dir, source+".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	// Non-blocking exclusive lock
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("source %q is locked by another process", source)
	}

	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
