// Package vault provides age-encrypted credential management.
package vault

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/ntwrknrd/nssh/internal/session/mode"
)

// ErrAmbiguousMode is returned when both software and hardware keystores exist.
var ErrAmbiguousMode = errors.New("ambiguous state: both software and hardware keystores found")

// ErrNotInitialized is returned when no keystore is found.
var ErrNotInitialized = errors.New("not initialized: no keystore found")

// DetectSecurityMode returns the current security mode based on filesystem state.
// It checks for the presence of keystore files rather than reading config.
//
// Returns:
//   - agent.ModeSoftware if age.key.enc exists (and piv.json doesn't)
//   - agent.ModePIV if piv.json exists (and age.key.enc doesn't)
//   - ErrAmbiguousMode if both keystores exist
//   - ErrNotInitialized if neither keystore exists
func DetectSecurityMode(configDir string) (string, error) {
	softwareExists := fileExists(filepath.Join(configDir, "age.key.enc"))
	hardwareExists := fileExists(filepath.Join(configDir, "piv.json"))

	switch {
	case softwareExists && hardwareExists:
		return "", ErrAmbiguousMode
	case softwareExists:
		return string(mode.Software), nil
	case hardwareExists:
		return string(mode.PIV), nil
	default:
		return "", ErrNotInitialized
	}
}

// HasMultipleKeystores returns true if both software and hardware keystores exist.
// This indicates an ambiguous or partial state, typically from a failed mode switch.
func HasMultipleKeystores(configDir string) bool {
	softwareExists := fileExists(filepath.Join(configDir, "age.key.enc"))
	hardwareExists := fileExists(filepath.Join(configDir, "piv.json"))
	return softwareExists && hardwareExists
}

// IsInitialized returns true if at least one keystore exists.
func IsInitialized(configDir string) bool {
	softwareExists := fileExists(filepath.Join(configDir, "age.key.enc"))
	hardwareExists := fileExists(filepath.Join(configDir, "piv.json"))
	return softwareExists || hardwareExists
}

// fileExists returns true if the path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
