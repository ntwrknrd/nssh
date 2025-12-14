package vault

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const (
	lockTimeout      = 5 * time.Second
	lockPollInterval = 100 * time.Millisecond
)

// lock acquires an exclusive directory-based lock.
// Returns an unlock function that must be called when done.
func (m *Manager) lock() (unlock func(), err error) {
	lockDir := m.credentialPath + ".lock"
	deadline := time.Now().Add(lockTimeout)

	for {
		// Try to create lock directory (atomic)
		if err := os.Mkdir(lockDir, 0700); err == nil {
			// Success - write lock info
			infoPath := filepath.Join(lockDir, ".lockinfo")
			info := fmt.Sprintf("pid=%d\ntime=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
			if err := os.WriteFile(infoPath, []byte(info), 0600); err != nil {
				slog.Debug("failed to write lock info", "path", infoPath, "err", err)
			}

			return func() {
				if err := os.RemoveAll(lockDir); err != nil {
					slog.Debug("failed to remove lock dir", "dir", lockDir, "err", err)
				}
			}, nil
		}

		if !os.IsExist(err) {
			return nil, fmt.Errorf("create lock: %w", err)
		}

		// Lock exists - check timeout
		if time.Now().After(deadline) {
			holder := readLockHolder(lockDir)
			if holder != "" {
				return nil, fmt.Errorf("credential store is busy (lock held by %s)", holder)
			}
			return nil, fmt.Errorf("credential store is busy (lock at %s)", lockDir)
		}

		time.Sleep(lockPollInterval)
	}
}

// readLockHolder attempts to read lock holder info.
func readLockHolder(lockDir string) string {
	infoPath := filepath.Join(lockDir, ".lockinfo")
	data, err := os.ReadFile(infoPath)
	if err != nil {
		return ""
	}
	return string(data)
}

// WithLock executes fn while holding the lock.
func (m *Manager) WithLock(fn func() error) error {
	unlock, err := m.lock()
	if err != nil {
		return err
	}
	defer unlock()
	return fn()
}
