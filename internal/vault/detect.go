// Package vault provides age-encrypted credential management.
package vault

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/ntwrknrd/nssh/internal/session/mode"
)

// ErrNotInitialized is returned when no keystore is found.
var ErrNotInitialized = errors.New("not initialized: no keystore found")

// DetectSecurityMode returns the current security mode based on filesystem state.
// It checks for the presence of the software keystore rather than reading config.
//
// Returns:
//   - agent.ModeSoftware if age.key.enc exists
//   - ErrNotInitialized if no software keystore exists
func DetectSecurityMode(configDir string) (string, error) {
	softwareExists := fileExists(filepath.Join(configDir, "age.key.enc"))
	if softwareExists {
		return string(mode.Software), nil
	}
	return "", ErrNotInitialized
}

// IsInitialized returns true if at least one keystore exists.
func IsInitialized(configDir string) bool {
	return fileExists(filepath.Join(configDir, "age.key.enc"))
}

// fileExists returns true if the path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
