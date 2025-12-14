//go:build linux || darwin

package software

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupTestStore creates a PassphraseStore with temp directories.
func setupTestStore(t *testing.T) (*PassphraseStore, string) {
	t.Helper()

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")

	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatalf("create state dir: %v", err)
	}

	cfg := Config{
		ConfigDir:        configDir,
		StateDir:         stateDir,
		ScryptWorkFactor: 14, // Minimum for fast tests
		Logger:           testNilLogger(),
	}

	store, err := NewPassphraseStore(cfg)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	return store, tmpDir
}

// setupTestStoreWithLogger creates a PassphraseStore with a custom logger.
//
//nolint:unused
func setupTestStoreWithLogger(t *testing.T, logger *slog.Logger) (*PassphraseStore, string) {
	t.Helper()

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")

	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatalf("create state dir: %v", err)
	}

	cfg := Config{
		ConfigDir:        configDir,
		StateDir:         stateDir,
		ScryptWorkFactor: 14, // Minimum for fast tests
		Logger:           logger,
	}

	store, err := NewPassphraseStore(cfg)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	return store, tmpDir
}

// testNilLogger returns a logger that discards output for tests.
func testNilLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mockTime returns a time function that returns fixedTime.
func mockTime(fixedTime time.Time) func() time.Time {
	return func() time.Time { return fixedTime }
}

// mockTimeAdvancing returns a time function that advances by step each call.
//
//nolint:unused
func mockTimeAdvancing(start time.Time, step time.Duration) func() time.Time {
	current := start
	return func() time.Time {
		t := current
		current = current.Add(step)
		return t
	}
}

// writeLockoutFile writes a raw lockout JSON file for tamper testing.
func writeLockoutFile(t *testing.T, stateDir string, content []byte) {
	t.Helper()
	path := filepath.Join(stateDir, "lockout.json")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("write lockout file: %v", err)
	}
}

// readLockoutFile reads the lockout JSON file.
func readLockoutFile(t *testing.T, stateDir string) []byte {
	t.Helper()
	path := filepath.Join(stateDir, "lockout.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read lockout file: %v", err)
	}
	return data
}

// deleteLockoutFile removes the lockout file.
func deleteLockoutFile(t *testing.T, stateDir string) {
	t.Helper()
	path := filepath.Join(stateDir, "lockout.json")
	_ = os.Remove(path)
}
